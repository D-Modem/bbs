package geoip

import (
	"encoding/binary"
	"encoding/csv"
	"errors"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
)

// csvDB is a CSV's ranges, kept small: each place once, and of each
// range only where it starts (the ranges run on one from another; a gap
// is a range of no place, place 0).  IPv4 and IPv6 apart, IPv4 in 4
// bytes.  DB-IP's city list, 7.8 million ranges, takes some 120 MB.
type csvDB struct {
	places  []Location // [0]: none
	v4      []uint32
	v4place []uint32
	v6      [][2]uint64
	v6place []uint32
}

// CSV layouts read, by addresses or by their numbers, quoted or not
// (lines that are not ranges are passed over):
//
//	start,end,CC                                     DB-IP's country
//	start,end,continent,CC,region,city,lat,lon       DB-IP's city
//	"from","to","CC","Country"[,"Region","City",...] IP2Location's
func readCSV(in io.Reader) (*csvDB, error) {
	r := csv.NewReader(in)
	r.FieldsPerRecord, r.LazyQuotes, r.ReuseRecord = -1, true, true
	db := &csvDB{places: []Location{{}}}
	index := map[Location]uint32{}
	var v4 []range4
	var v6 []range6
	for {
		f, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				continue
			}
			return nil, err
		}
		if len(f) < 3 {
			continue
		}
		start, ok1 := address(strings.TrimSpace(f[0]))
		end, ok2 := address(strings.TrimSpace(f[1]))
		if !ok1 || !ok2 {
			continue
		}
		var l Location
		field := func(i int) string {
			if i >= len(f) {
				return ""
			}
			s := strings.TrimSpace(f[i])
			if s == "-" {
				return ""
			}
			return s
		}
		if dbipCity(f) {
			l = Location{Code: field(3), Region: field(4), City: field(5)}
		} else {
			l = Location{Code: field(2), Name: field(3)}
			if len(f) >= 6 {
				l.Region, l.City = field(4), field(5)
			}
		}
		l.Code = strings.ToUpper(l.Code)
		if len(l.Code) != 2 || unknown[l.Code] {
			continue // no place: left a gap
		}
		p, ok := index[l]
		if !ok {
			l = Location{strings.Clone(l.Code), strings.Clone(l.Name), strings.Clone(l.Region), strings.Clone(l.City)}
			p = uint32(len(db.places))
			db.places = append(db.places, l)
			index[l] = p
		}
		if s4, e4, ok := four(start, end); ok {
			v4 = append(v4, range4{s4, e4, p})
		} else {
			v6 = append(v6, range6{split(start), split(end), p})
		}
	}
	if len(db.places) == 1 {
		return nil, errors.New("not a MaxMind DB, nor a CSV of ranges (start,end,country)")
	}
	db.build4(v4)
	db.build6(v6)
	return db, nil
}

type range4 struct {
	start, end uint32
	place      uint32
}

type range6 struct {
	start, end [2]uint64
	place      uint32
}

// four is a range of IPv4 addresses (IPv4-mapped) as numbers.
func four(start, end [16]byte) (uint32, uint32, bool) {
	s, e := net.IP(start[:]).To4(), net.IP(end[:]).To4()
	if s == nil || e == nil {
		return 0, 0, false
	}
	return binary.BigEndian.Uint32(s), binary.BigEndian.Uint32(e), true
}

// split is a 16-byte address as two big-endian halves, high first.
func split(a [16]byte) [2]uint64 {
	return [2]uint64{binary.BigEndian.Uint64(a[:8]), binary.BigEndian.Uint64(a[8:])}
}

// less6 says whether address a is before b.
func less6(a, b [2]uint64) bool { return a[0] < b[0] || a[0] == b[0] && a[1] < b[1] }

// next6 is the address after a ([^0, ^0]'s: itself, the last).
func next6(a [2]uint64) [2]uint64 {
	if a[1] == ^uint64(0) {
		if a[0] == ^uint64(0) {
			return a
		}
		return [2]uint64{a[0] + 1, 0}
	}
	return [2]uint64{a[0], a[1] + 1}
}

// build4 sets db's IPv4 starts from rs, sorted by start: an overlapping
// range is cut to begin after the one before (one wholly inside it is
// dropped), a gap gets a start of place 0, and so does the address after
// the last range.
func (db *csvDB) build4(rs []range4) {
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].start < rs[j].start })
	var next uint64 // the address after the last range
	for _, r := range rs {
		if uint64(r.start) < next {
			if uint64(r.end) < next {
				continue // inside the last
			}
			r.start = uint32(next)
		}
		if uint64(r.start) > next {
			db.v4, db.v4place = append(db.v4, uint32(next)), append(db.v4place, 0)
		}
		db.v4, db.v4place = append(db.v4, r.start), append(db.v4place, r.place)
		next = uint64(r.end) + 1
	}
	if next <= 0xffffffff {
		db.v4, db.v4place = append(db.v4, uint32(next)), append(db.v4place, 0)
	}
}

// build6 is build4 for IPv6: db's IPv6 starts from rs, overlaps cut, gaps
// and the rest after the last range place 0 (none once a range reaches
// the very last address).
func (db *csvDB) build6(rs []range6) {
	sort.SliceStable(rs, func(i, j int) bool { return less6(rs[i].start, rs[j].start) })
	var next [2]uint64
	last := false // the very last address is in a range
	for _, r := range rs {
		if last {
			break
		}
		if less6(r.start, next) {
			if less6(r.end, next) {
				continue
			}
			r.start = next
		}
		if less6(next, r.start) {
			db.v6, db.v6place = append(db.v6, next), append(db.v6place, 0)
		}
		db.v6, db.v6place = append(db.v6, r.start), append(db.v6place, r.place)
		next = next6(r.end)
		last = next == r.end
	}
	if !last {
		db.v6, db.v6place = append(db.v6, next), append(db.v6place, 0)
	}
}

// lookup is ip's place: that of the last start at or before it (IPv4 in
// IPv6 looked up as IPv4); Location{} before the first, or in a gap.
func (db *csvDB) lookup(ip net.IP) Location {
	ip = embedded(ip)
	if v4 := ip.To4(); v4 != nil {
		a := binary.BigEndian.Uint32(v4)
		i := sort.Search(len(db.v4), func(i int) bool { return db.v4[i] > a })
		if i == 0 {
			return Location{}
		}
		return db.places[db.v4place[i-1]]
	}
	var b [16]byte
	copy(b[:], ip.To16())
	a := split(b)
	i := sort.Search(len(db.v6), func(i int) bool { return less6(a, db.v6[i]) })
	if i == 0 {
		return Location{}
	}
	return db.places[db.v6place[i-1]]
}

// continents are DB-IP's continent codes.
var continents = map[string]bool{"AF": true, "AN": true, "AS": true, "EU": true, "NA": true, "OC": true, "SA": true, "ZZ": true}

// dbipCity says whether a line is DB-IP's city layout: a continent, the
// country, region, city, latitude and longitude.
func dbipCity(f []string) bool {
	if len(f) != 8 || !continents[f[2]] || len(f[3]) != 2 {
		return false
	}
	_, err1 := strconv.ParseFloat(f[6], 64)
	_, err2 := strconv.ParseFloat(f[7], 64)
	return err1 == nil && err2 == nil
}
