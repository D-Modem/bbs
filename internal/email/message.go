package email

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset" // the charsets mail comes in
	gomail "github.com/emersion/go-message/mail"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/store"
)

// fromAddress is who a mail kept here is from, as an address.
func (s *Service) fromAddress(m *store.Message) *mail.Address {
	switch {
	case m.FromAddr != "":
		return &mail.Address{Name: m.From, Address: m.FromAddr}
	case m.FromID > 0:
		return &mail.Address{Name: m.From, Address: s.AddressOf(m.From)}
	}
	if name, a, ok := bbs.SplitNetmail(m.From); ok {
		return &mail.Address{Name: name, Address: fidoAddress(name, a)}
	}
	return &mail.Address{Name: m.From, Address: "postmaster@" + s.Set.Domain}
}

// toField is who a mail kept here is to, as To says it.
func (s *Service) toField(m *store.Message) string {
	if m.ToAddr != "" {
		if l, err := mail.ParseAddressList(m.ToAddr); err == nil {
			out := make([]string, len(l))
			for i, a := range l {
				out[i] = a.String()
			}
			return strings.Join(out, ", ")
		}
	}
	if name, a, ok := bbs.SplitNetmail(m.To); ok {
		return (&mail.Address{Name: name, Address: fidoAddress(name, a)}).String()
	}
	return s.Mailbox(m.To)
}

// messageID is a mail's Message-ID: its own, or one made of its id.
func (s *Service) messageID(m *store.Message) string {
	if m.MessageID != "" {
		return m.MessageID
	}
	return fmt.Sprintf("<mail-%d@%s>", m.ID, s.Set.Hostname)
}

// newMessageID is a new Message-ID of this server's.
func (s *Service) newMessageID() string {
	var b [12]byte
	rand.Read(b[:])
	return fmt.Sprintf("<%d.%s@%s>", time.Now().Unix(), hex.EncodeToString(b[:]), s.Set.Hostname)
}

// Render is a mail as a mail program has it: as it came, or, for one
// written here, made of it (the same each time: its size does not change).
func (s *Service) Render(m *store.Message) []byte {
	if m.Raw != nil {
		return m.Raw
	}
	var b bytes.Buffer
	field := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	field("From", s.fromAddress(m).String())
	field("To", s.toField(m))
	field("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	field("Date", m.Created.Format(time.RFC1123Z))
	field("Message-ID", s.messageID(m))
	if m.ReplyTo > 0 {
		field("In-Reply-To", fmt.Sprintf("<mail-%d@%s>", m.ReplyTo, s.Set.Hostname))
	}
	writeTextBody(&b, m.Body)
	return b.Bytes()
}

// writeTextBody writes the MIME fields of a plain text body, and the body:
// 8bit, or quoted-printable if a line is too long for it.
func writeTextBody(b *bytes.Buffer, body string) {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), "\n"), "\n")
	qp := false
	for _, l := range lines {
		qp = qp || len(l) > 900
	}
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n")
	if !qp {
		b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		for _, l := range lines {
			b.WriteString(strings.TrimRight(l, "\r") + "\r\n")
		}
		return
	}
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	w := quotedprintable.NewWriter(b)
	w.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
	w.Close()
}

// parsed is what the BBS shows of a mail that came as RFC 5322.
type parsed struct {
	fromName, fromAddr string
	to                 []*mail.Address
	cc                 []*mail.Address
	subject            string
	date               time.Time
	messageID          string
	inReplyTo          string
	text               string // the body for the terminals
}

// readLimit is the most of a part read for its text.
const readLimit = 1 << 20

var (
	htmlBreaks = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/tr|/h[1-6])[^>]*>`)
	htmlDrop   = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)\s*>`)
	htmlTags   = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRuns  = regexp.MustCompile(`\n{3,}`)
)

// htmlText is HTML as plain text, roughly.
func htmlText(h string) string {
	h = htmlDrop.ReplaceAllString(h, "")
	h = htmlBreaks.ReplaceAllString(h, "\n")
	h = htmlTags.ReplaceAllString(h, "")
	h = html.UnescapeString(h)
	var lines []string
	for _, l := range strings.Split(h, "\n") {
		lines = append(lines, strings.Join(strings.Fields(l), " "))
	}
	return strings.TrimSpace(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// parse reads a mail: its header and its text (the first plain text part,
// else the first HTML one as text), its attachments listed after it.
func parse(raw []byte) (*parsed, error) {
	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil && mr == nil {
		return nil, err
	}
	p := &parsed{}
	h := mr.Header
	if l, err := h.AddressList("From"); err == nil && len(l) > 0 {
		p.fromName, p.fromAddr = l[0].Name, l[0].Address
	} else if v, _ := h.Text("From"); v != "" {
		p.fromName = v
	}
	if l, err := h.AddressList("To"); err == nil {
		p.to = convert(l)
	}
	if l, err := h.AddressList("Cc"); err == nil {
		p.cc = convert(l)
	}
	p.subject, _ = h.Subject()
	p.date, _ = h.Date()
	p.messageID = h.Get("Message-ID")
	p.inReplyTo = h.Get("In-Reply-To")
	var plain, htm string
	var attached []string
	for {
		part, err := mr.NextPart()
		if err != nil {
			break // io.EOF, or a part that cannot be read: what was read is kept
		}
		switch ph := part.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := ph.ContentType()
			switch {
			case ct == "text/plain" && plain == "", ct == "" && plain == "":
				b, _ := io.ReadAll(io.LimitReader(part.Body, readLimit))
				plain = string(b)
			case ct == "text/html" && htm == "":
				b, _ := io.ReadAll(io.LimitReader(part.Body, readLimit))
				htm = string(b)
			default:
				n, _ := io.Copy(io.Discard, part.Body)
				attached = append(attached, fmt.Sprintf("(%s, %s)", ct, size(n)))
			}
		case *gomail.AttachmentHeader:
			name, _ := ph.Filename()
			if name == "" {
				name = "(unnamed)"
			}
			n, _ := io.Copy(io.Discard, part.Body)
			attached = append(attached, fmt.Sprintf("%s (%s)", name, size(n)))
		}
	}
	p.text = plain
	if strings.TrimSpace(p.text) == "" && htm != "" {
		p.text = htmlText(htm)
	}
	if len(attached) > 0 {
		p.text = strings.TrimRight(p.text, "\r\n") + "\n\n[Attached, for a mail program to open: " + strings.Join(attached, ", ") + "]"
	}
	return p, nil
}

func convert(l []*gomail.Address) []*mail.Address {
	out := make([]*mail.Address, len(l))
	for i, a := range l {
		out[i] = &mail.Address{Name: a.Name, Address: a.Address}
	}
	return out
}

// size is a size in bytes for people.
func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", (n+512)>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

// terminalBody is a mail's text as the terminals keep it: lines wrapped
// and cleaned, within the BBS's limits.
func terminalBody(text string) string {
	body := bbs.CleanBody(bbs.WrapBody(text))
	if body == "" {
		body = "(empty)"
	}
	return body
}

// addressList is addresses as a header says them.
func addressList(l []*mail.Address) string {
	out := make([]string, len(l))
	for i, a := range l {
		out[i] = a.String()
	}
	return strings.Join(out, ", ")
}

// headerEnd is where a raw mail's header ends (its blank line's start), or
// -1.
func headerEnd(raw []byte) int {
	if bytes.HasPrefix(raw, []byte("\r\n")) || bytes.HasPrefix(raw, []byte("\n")) {
		return 0
	}
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return i + 2
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return i + 1
	}
	return -1
}

// headerFields is a raw header's fields, unfolded, in order: name and
// value.
func headerFields(raw []byte) [][2]string {
	end := headerEnd(raw)
	if end < 0 {
		end = len(raw)
	}
	var out [][2]string
	for _, line := range strings.Split(strings.ReplaceAll(string(raw[:end]), "\r\n", "\n"), "\n") {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(out) > 0 {
			out[len(out)-1][1] += " " + strings.TrimSpace(line)
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out = append(out, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
	}
	return out
}

// withoutFields is a raw mail with the header fields of those names (in
// any case) taken out, folded lines and all.
func withoutFields(raw []byte, names ...string) []byte {
	end := headerEnd(raw)
	if end < 0 {
		return raw
	}
	drop := map[string]bool{}
	for _, n := range names {
		drop[strings.ToLower(n)] = true
	}
	var out bytes.Buffer
	skipping := false
	for _, line := range bytes.SplitAfter(raw[:end], []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if !skipping {
				out.Write(line)
			}
			continue
		}
		k, _, _ := bytes.Cut(line, []byte(":"))
		skipping = drop[strings.ToLower(string(bytes.TrimSpace(k)))]
		if !skipping {
			out.Write(line)
		}
	}
	out.Write(raw[end:])
	return out.Bytes()
}

// crlf is a raw mail with its lines ended by CRLF.
func crlf(raw []byte) []byte {
	if !bytes.Contains(raw, []byte("\n")) {
		return raw
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(raw, []byte("\n"), []byte("\r\n"))
}
