package imageboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"hash/fnv"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/geoip"
)

// Poster IDs and country flags, on the boards that have them.

// posterID is a poster's ID in a thread: eight characters of a keyed hash
// of their address, the board and the thread, as 4chan's: the same all
// through a thread, another in the next, and nothing of the address to be
// had from it.
func (s *Server) posterID(addr, dir string, thread int64) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("id\x00" + addr + "\x00" + dir + "\x00" + strconv.FormatInt(thread, 10)))
	return base64.StdEncoding.EncodeToString(m.Sum(nil)[:6])
}

// idColors are how many colours IDs are shown in (idc-0 ... in chan.css).
const idColors = 24

// idClass is an ID's colour's class: the same ID, the same colour.
func idClass(id string) string {
	h := fnv.New32a()
	h.Write([]byte(id))
	return "idc-" + strconv.Itoa(int(h.Sum32()%idColors))
}

// geo is the country database, read again when its file changes (looked
// at once a minute at most).
type geo struct {
	mu      sync.Mutex
	reader  atomic.Pointer[geoip.Reader]
	stamp   time.Time // the file's, when read
	checked time.Time
}

// country is an address's country (code, name), if the boards have a
// database and it says.
func (s *Server) country(addr string) (string, string) {
	ip := net.ParseIP(addr)
	if s.S.GeoIP == "" || ip == nil {
		return "", ""
	}
	g := &s.geo
	g.mu.Lock()
	if time.Since(g.checked) > time.Minute {
		g.checked = time.Now()
		if fi, err := os.Stat(s.S.GeoIP); err == nil && !fi.ModTime().Equal(g.stamp) {
			r, err := geoip.Open(s.S.GeoIP)
			if err != nil {
				s.B.Log.Printf("chan: the country database: %v", err)
			} else {
				g.reader.Store(r)
				g.stamp = fi.ModTime()
			}
		}
	}
	g.mu.Unlock()
	code, name, ok := g.reader.Load().Country(ip)
	if !ok {
		return "", ""
	}
	return code, name
}

// flagOf is a code's flag, for the templates.
func flagOf(code string) string { return geoip.Flag(strings.ToUpper(code)) }
