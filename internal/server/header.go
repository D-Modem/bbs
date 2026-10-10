package server

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
)

// The session header, as modemmenu (server.go) and app_dmodem write it:
// KEY=value lines, then an empty line, then the caller's bytes.

const (
	headerMaxLines = 64
	headerMaxBytes = 8192
)

// ReadHeader reads a session header from c, a byte at a time so that
// nothing of the caller's is read with it.
func ReadHeader(c net.Conn, timeout time.Duration) (map[string]string, error) {
	c.SetReadDeadline(time.Now().Add(timeout))
	defer c.SetReadDeadline(time.Time{})
	h := map[string]string{}
	var line []byte
	b := make([]byte, 1)
	for total := 0; ; total++ {
		if total >= headerMaxBytes {
			return nil, errors.New("header: too long")
		}
		if _, err := c.Read(b); err != nil {
			return nil, fmt.Errorf("header: %w", err)
		}
		if b[0] != '\n' {
			line = append(line, b[0])
			continue
		}
		s := strings.TrimSuffix(string(line), "\r")
		line = line[:0]
		if s == "" {
			return h, nil
		}
		if len(h) >= headerMaxLines {
			return nil, errors.New("header: too many lines")
		}
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("header: no = in %q", bbs.Clean(s, 40))
		}
		h[strings.ToUpper(strings.TrimSpace(k))] = v
	}
}

// pick is the first of the keys the header has.
func pick(h map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := h[k]; ok {
			return bbs.Clean(v, 64)
		}
	}
	return ""
}

// applyHeader puts what a header says of the call into c; with trust its
// MODEM_USER is the caller, logged in already.
func applyHeader(c *bbs.Caller, h map[string]string, trust bool) {
	c.Rate = pick(h, "DMODEM_RATE", "MODEM_RATE")
	c.CallerID = pick(h, "DMODEM_CALLERID", "MODEM_CALLERID")
	c.Dialed = pick(h, "DMODEM_DIALED", "MODEM_DIALED")
	c.Channel = pick(h, "DMODEM_CHANNEL", "MODEM_CHANNEL")
	if c.Rate != "" || c.Channel != "" {
		c.Via = "modem"
	}
	if c.CallerID != "" {
		c.Remote = c.CallerID
	}
	if trust {
		c.TrustedUser = strings.TrimSpace(pick(h, "MODEM_USER"))
	}
}
