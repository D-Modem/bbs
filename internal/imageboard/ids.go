package imageboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"hash/fnv"
	"net"
	"strconv"
	"strings"

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

// country is an address's country (code, name), if the boards have a
// database and it says.
func (s *Server) country(addr string) (string, string) {
	if s.S.GeoIP == "" {
		return "", ""
	}
	s.geoOnce.Do(func() {
		s.geo = &geoip.File{Path: s.S.GeoIP, Log: func(err error) { s.B.Log.Printf("chan: the country database: %v", err) }}
	})
	code, name, _ := s.geo.Country(net.ParseIP(addr))
	return code, name
}

// flagOf is a code's flag, for the templates.
func flagOf(code string) string { return geoip.Flag(strings.ToUpper(code)) }
