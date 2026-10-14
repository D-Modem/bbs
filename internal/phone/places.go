package phone

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// places.gz is libphonenumber's geocoding, in English: the place a number
// is in by its first digits (the calling code's and on), "1201" New
// Jersey, "1201200" Jersey City, NJ, "4930" Berlin.
//
//go:embed places.gz
var placesGz []byte

var (
	placesOnce sync.Once
	placeKeys  []uint64 // the prefixes: digits*32 + length, in order
	placeIdx   []uint32 // and their descriptions
	placeDesc  []string
	placeMax   int // the longest prefix
)

func key(p string) (uint64, bool) {
	if len(p) == 0 || len(p) > 18 {
		return 0, false
	}
	n, err := strconv.ParseUint(p, 10, 64)
	return n*32 + uint64(len(p)), err == nil
}

func loadPlaces() {
	placesOnce.Do(func() {
		z, err := gzip.NewReader(bytes.NewReader(placesGz))
		if err != nil {
			panic("phone: places.gz: " + err.Error())
		}
		s := bufio.NewScanner(z)
		s.Buffer(make([]byte, 64*1024), 1<<20)
		descs := true
		prev := ""
		for s.Scan() {
			l := s.Text()
			switch {
			case descs && strings.HasPrefix(l, "#"):
			case descs && l == "":
				descs = false
			case descs:
				placeDesc = append(placeDesc, l)
			default:
				c, rest, _ := strings.Cut(l, ",")
				digits, idx, _ := strings.Cut(rest, " ")
				n, _ := strconv.Atoi(c)
				i, _ := strconv.Atoi(idx)
				p := prev[:n] + digits
				prev = p
				if k, ok := key(p); ok && i < len(placeDesc) {
					placeKeys = append(placeKeys, k)
					placeIdx = append(placeIdx, uint32(i))
					placeMax = max(placeMax, len(p))
				}
			}
		}
		if err := s.Err(); err != nil {
			panic("phone: places.gz: " + err.Error())
		}
		sort.Sort(byKey{})
	})
}

type byKey struct{}

func (byKey) Len() int           { return len(placeKeys) }
func (byKey) Less(i, j int) bool { return placeKeys[i] < placeKeys[j] }
func (byKey) Swap(i, j int) {
	placeKeys[i], placeKeys[j] = placeKeys[j], placeKeys[i]
	placeIdx[i], placeIdx[j] = placeIdx[j], placeIdx[i]
}

// placeOf is where a number (its calling code and on) is, by its longest
// known prefix ("": not known), and the prefix's length.
func placeOf(digits string) (string, int) {
	loadPlaces()
	for l := min(len(digits), placeMax); l > 0; l-- {
		k, ok := key(digits[:l])
		if !ok {
			continue
		}
		if i := sort.Search(len(placeKeys), func(i int) bool { return placeKeys[i] >= k }); i < len(placeKeys) && placeKeys[i] == k {
			return placeDesc[placeIdx[i]], l
		}
	}
	return "", 0
}
