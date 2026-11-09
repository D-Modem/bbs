package email

// The matching here follows go-imap's imapmemserver (MIT licence,
// Copyright (c) 2013 The Go-IMAP Authors), on mail read from the store.

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	gomessage "github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"

	"github.com/D-Modem/bbs/internal/store"
)

// matcher matches one mail against search criteria, reading it only if
// they need it.
type matcher struct {
	is     *imapSession
	e      *store.BoxEntry
	raw    []byte
	loaded bool
}

func (mm *matcher) load() []byte {
	if !mm.loaded {
		mm.raw, _ = mm.is.raw(mm.e)
		mm.loaded = true
	}
	return mm.raw
}

func (mm *matcher) entity() *gomessage.Entity {
	e, _ := gomessage.Read(bytes.NewReader(mm.load()))
	if e == nil {
		e, _ = gomessage.New(gomessage.Header{}, bytes.NewReader(nil))
	}
	return e
}

func (mm *matcher) match(seq uint32, c *imap.SearchCriteria) bool {
	for _, s := range c.SeqNum {
		if seq == 0 || !s.Contains(seq) {
			return false
		}
	}
	for _, s := range c.UID {
		if !s.Contains(imap.UID(mm.e.UID)) {
			return false
		}
	}
	if !matchDate(mm.e.Created, c.Since, c.Before) {
		return false
	}
	for _, f := range c.Flag {
		if !hasFlag(mm.e, f) {
			return false
		}
	}
	for _, f := range c.NotFlag {
		if hasFlag(mm.e, f) {
			return false
		}
	}
	if c.Larger != 0 || c.Smaller != 0 {
		n := mm.is.size(mm.e)
		if c.Larger != 0 && n <= c.Larger || c.Smaller != 0 && n >= c.Smaller {
			return false
		}
	}
	if len(c.Header) > 0 || !c.SentSince.IsZero() || !c.SentBefore.IsZero() {
		h := gomail.Header{Header: mm.entity().Header}
		for _, f := range c.Header {
			if !matchFields(h.FieldsByKey(f.Key), f.Value) {
				return false
			}
		}
		if !c.SentSince.IsZero() || !c.SentBefore.IsZero() {
			t, err := h.Date()
			if err != nil || !matchDate(t, c.SentSince, c.SentBefore) {
				return false
			}
		}
	}
	for _, t := range c.Text {
		if !matchEntity(mm.entity(), t, true) {
			return false
		}
	}
	for _, b := range c.Body {
		if !matchEntity(mm.entity(), b, false) {
			return false
		}
	}
	for i := range c.Not {
		if mm.match(seq, &c.Not[i]) {
			return false
		}
	}
	for i := range c.Or {
		if !mm.match(seq, &c.Or[i][0]) && !mm.match(seq, &c.Or[i][1]) {
			return false
		}
	}
	return true
}

// matchDate compares dates without their zones, as RFC 3501 says.
func matchDate(t, since, before time.Time) bool {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if !since.IsZero() && t.Before(since) {
		return false
	}
	if !before.IsZero() && !t.Before(before) {
		return false
	}
	return true
}

func matchFields(fields gomessage.HeaderFields, pattern string) bool {
	if pattern == "" {
		return fields.Len() > 0
	}
	pattern = strings.ToLower(pattern)
	for fields.Next() {
		v, _ := fields.Text()
		if strings.Contains(strings.ToLower(v), pattern) {
			return true
		}
	}
	return false
}

func matchEntity(e *gomessage.Entity, pattern string, header bool) bool {
	if pattern == "" {
		return true
	}
	if header && matchFields(e.Header.Fields(), pattern) {
		return true
	}
	if mr := e.MultipartReader(); mr != nil {
		for {
			part, err := mr.NextPart()
			if err != nil {
				return false
			}
			if matchEntity(part, pattern, header) {
				return true
			}
		}
	}
	t, _, err := e.Header.ContentType()
	if err != nil && t == "" {
		t = "text/plain"
	}
	if !strings.HasPrefix(t, "text/") && !strings.HasPrefix(t, "message/") {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(e.Body, readLimit))
	if err != nil {
		return false
	}
	return bytes.Contains(bytes.ToLower(b), bytes.ToLower([]byte(pattern)))
}
