package fontscan

import (
	"encoding/binary"
	"errors"
	"strings"

	"github.com/go-text/typesetting/language"
)

// LangID is a compact representation of a language
// this package has orthographic knowledge of.
type LangID = language.LangID

// NewLangID is [language.NewLangID].
//
// Deprecated: use [language.NewLangID] instead.
var NewLangID = language.NewLangID

// LangSet is a bit set for 512 languages
//
// It works as a map[LangID]bool, with the limitation
// that only the 9 low bits of a LangID are used.
// More precisely, the page of a LangID l is given by its 3 "higher" bits : 8-6
// and the bit position by its 6 lower bits : 5-0
type LangSet [8]uint64

// newLangsetFromCoverage compile the languages supported by the given
// rune coverage
func newLangsetFromCoverage(rs RuneSet) (out LangSet) {
	for id, runes := range languagesRunes {
		if rs.includes(runes) {
			out.Add(LangID(id))
		}
	}
	return out
}

func (ls LangSet) String() string {
	var chunks []string
	for pageN, page := range ls {
		for bit := 0; bit < 64; bit++ {
			if page&(1<<bit) != 0 {
				id := LangID(pageN<<6 | bit)
				chunks = append(chunks, string(id.Language()))
			}
		}
	}
	return "{" + strings.Join(chunks, "|") + "}"
}

func (ls *LangSet) Add(l LangID) {
	page := (l & 0b111111111 >> 6)
	bit := l & 0b111111
	ls[page] |= 1 << bit
}

func (ls LangSet) Contains(l LangID) bool {
	page := (l & 0b111111111 >> 6)
	bit := l & 0b111111
	return ls[page]&(1<<bit) != 0
}

const langSetSize = 8 * 8

func (ls LangSet) serialize() []byte {
	var buffer [langSetSize]byte
	for i, v := range ls {
		binary.BigEndian.PutUint64(buffer[i*8:], v)
	}
	return buffer[:]
}

// deserializeFrom reads the binary format produced by serializeTo
// it returns the number of bytes read from `data`
func (ls *LangSet) deserializeFrom(data []byte) (int, error) {
	if len(data) < langSetSize {
		return 0, errors.New("invalid lang set (EOF)")
	}
	for i := range ls {
		ls[i] = binary.BigEndian.Uint64(data[i*8:])
	}
	return langSetSize, nil
}
