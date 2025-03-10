package cff

import (
	"encoding/binary"
	"errors"
	"fmt"

	ps "github.com/go-text/typesetting/font/cff/interpreter"
	"github.com/go-text/typesetting/font/opentype/tables"
)

//go:generate ../../../../typesetting-utils/generators/binarygen/cmd/generator . _src.go

// CFF2 represents a parsed 'CFF2' Opentype table.
type CFF2 struct {
	fdSelect fdSelect // maybe nil if there is only one font dict

	// Charstrings contains the actual glyph definition.
	// It has a length of numGlyphs and is indexed by glyph ID.
	// See `LoadGlyph` for a way to intepret the glyph data.
	Charstrings [][]byte

	globalSubrs [][]byte

	// array of length 1 if fdSelect is nil
	// otherwise, it can be safely indexed by `fdSelect` output
	fonts []privateFonts

	VarStore tables.ItemVarStore // optional
}

type privateFonts struct {
	localSubrs     [][]byte
	defaultVSIndex int32
}

// ParseCFF2 parses 'src', which must be the content of a 'CFF2' Opentype table.
//
// See also https://learn.microsoft.com/en-us/typography/opentype/spec/cff2
func ParseCFF2(src []byte) (*CFF2, error) {
	if L := len(src); L < 5 {
		return nil, fmt.Errorf("reading header: EOF: expected length: 5, got %d", L)
	}
	var header header2
	header.mustParse(src)
	topDictEnd := int(header.headerSize) + int(header.topDictLength)
	if L := len(src); L < topDictEnd {
		return nil, fmt.Errorf("reading topDict: EOF: expected length: %d, got %d", topDictEnd, L)
	}
	topDictSrc := src[header.headerSize:topDictEnd]

	var (
		tp  topDict2
		psi ps.Machine
	)
	if err := psi.Run(topDictSrc, nil, nil, &tp); err != nil {
		return nil, fmt.Errorf("reading top dict: %s", err)
	}

	var (
		out CFF2
		err error
	)

	out.globalSubrs, err = parseIndex2(src, topDictEnd)
	if err != nil {
		return nil, err
	}

	// parse charstrings
	out.Charstrings, err = parseIndex2(src, int(tp.charStrings))
	if err != nil {
		return nil, err
	}

	fdIndex, err := parseIndex2(src, int(tp.fdArray))
	if err != nil {
		return nil, err
	}

	out.fonts = make([]privateFonts, len(fdIndex))
	// private dict reference
	for i, font := range fdIndex {
		var fd fontDict2
		err = psi.Run(font, nil, nil, &fd)
		if err != nil {
			return nil, fmt.Errorf("reading font dict: %s", err)
		}
		end := int(fd.privateDictOffset + fd.privateDictSize)
		if L := len(src); L < end {
			return nil, fmt.Errorf("reading private dict: EOF: expected length: %d, got %d", end, L)
		}
		// parse private dict
		var pd privateDict2
		err = psi.Run(src[fd.privateDictOffset:end], nil, nil, &pd)
		if err != nil {
			return nil, fmt.Errorf("reading private dict: %s", err)
		}

		out.fonts[i].defaultVSIndex = pd.vsindex
		// if required, parse the local subroutines
		if pd.subrsOffset != 0 {
			out.fonts[i].localSubrs, err = parseIndex2(src, int(pd.subrsOffset))
			if err != nil {
				return nil, err
			}
		}
	}

	if len(fdIndex) > 1 {
		// parse the fdSelect
		if L := len(src); L < int(tp.fdSelect) {
			return nil, fmt.Errorf("reading fdSelect: EOF: expected length: %d, got %d", tp.fdSelect, L)
		}
		out.fdSelect, _, err = parseFdSelect(src[tp.fdSelect:], len(out.Charstrings))
		if err != nil {
			return nil, err
		}

		// sanitize fdSelect outputs
		indexExtent := out.fdSelect.extent()
		if len(fdIndex) < indexExtent {
			return nil, fmt.Errorf("invalid number of font dicts: %d (for %d)", len(fdIndex), indexExtent)
		}
	}

	// parse variation store
	if tp.vstore != 0 {
		// See https://learn.microsoft.com/en-us/typography/opentype/spec/cff2#variationstore-data-contents
		if E, L := int(tp.vstore)+2, len(src); L < E {
			return nil, fmt.Errorf("reading variation store: EOF: expected length: %d, got %d", E, L)
		}
		size := int(binary.BigEndian.Uint16(src[tp.vstore:]))
		end := int(tp.vstore) + 2 + size
		if L := len(src); L < end {
			return nil, fmt.Errorf("reading variation store: EOF: expected length: %d, got %d", end, L)
		}
		vstore := src[tp.vstore+2 : end]
		out.VarStore, _, err = tables.ParseItemVarStore(vstore)
		if err != nil {
			return nil, err
		}
	}
	return &out, nil
}

func parseIndex2(src []byte, offset int) ([][]byte, error) {
	if L := len(src); L < offset+5 {
		return nil, fmt.Errorf("reading INDEX: EOF: expected length: %d, got %d", offset+5, L)
	}
	var is indexStart
	is.mustParse(src[offset:])
	out, _, err := parseIndexContent(src[offset+5:], is)
	return out, err
}

type topDict2 struct {
	charStrings int32 // offset
	fdArray     int32 // offset
	fdSelect    int32 // offset
	vstore      int32 // offset
}

func (tp *topDict2) Context() ps.Context { return ps.TopDict }

func (tp *topDict2) Apply(state *ps.Machine, op ps.Operator) error {
	switch op {
	case ps.Operator{Operator: 7, IsEscaped: true}: // FontMatrix
		// skip
		state.ArgStack.Clear()
		return nil
	case ps.Operator{Operator: 17, IsEscaped: false}: // CharStrings
		if state.ArgStack.Top < 1 {
			return fmt.Errorf("invalid number of arguments for operator %s in Top Dict", op)
		}
		tp.charStrings = int32(state.ArgStack.Pop())
	case ps.Operator{Operator: 36, IsEscaped: true}: // FDArray
		if state.ArgStack.Top < 1 {
			return fmt.Errorf("invalid number of arguments for operator %s in Top Dict", op)
		}
		tp.fdArray = int32(state.ArgStack.Pop())
	case ps.Operator{Operator: 37, IsEscaped: true}: // FDSelect
		if state.ArgStack.Top < 1 {
			return fmt.Errorf("invalid number of arguments for operator %s in Top Dict", op)
		}
		tp.fdSelect = int32(state.ArgStack.Pop())
	case ps.Operator{Operator: 24, IsEscaped: false}: // vstore
		if state.ArgStack.Top < 1 {
			return fmt.Errorf("invalid number of arguments for operator %s in Top Dict", op)
		}
		tp.vstore = int32(state.ArgStack.Pop())
	default:
		return fmt.Errorf("invalid operator %s in Top Dict", op)
	}
	return nil
}

type fontDict2 struct {
	privateDictSize   int32
	privateDictOffset int32
}

func (fd *fontDict2) Context() ps.Context { return ps.TopDict }

func (fd *fontDict2) Apply(state *ps.Machine, op ps.Operator) error {
	switch op {
	case ps.Operator{Operator: 18, IsEscaped: false}: // Private
		if state.ArgStack.Top < 2 {
			return fmt.Errorf("invalid number of arguments for operator %s in Font Dict", op)
		}
		fd.privateDictOffset = int32(state.ArgStack.Pop())
		fd.privateDictSize = int32(state.ArgStack.Pop())
		return nil
	default:
		return fmt.Errorf("invalid operator %s in Font Dict", op)
	}
}

// privateDict2 contains fields specific to the Private DICT context.
type privateDict2 struct {
	subrsOffset int32
	vsindex     int32 // 	itemVariationData index in the VariationStore structure table.
}

func (privateDict2) Context() ps.Context { return ps.PrivateDict }

// The Private DICT operators are defined by 5176.CFF.pdf Table 23 "Private
// DICT Operators".
func (priv *privateDict2) Apply(state *ps.Machine, op ps.Operator) error {
	if !op.IsEscaped { // 1-byte operators.
		switch op.Operator {
		case 6, 7, 8, 9: // "BlueValues" "OtherBlues" "FamilyBlues" "FamilyOtherBlues"
			return state.ArgStack.PopN(-2)
		case 10, 11: // "StdHW" "StdVW"
			return state.ArgStack.PopN(1)
		case 19: // "Subrs" pop 1
			if state.ArgStack.Top < 1 {
				return errors.New("invalid stack size for 'subrs' in private Dict charstring")
			}
			priv.subrsOffset = int32(state.ArgStack.Pop())
			return nil
		case 22: // "vsindex"
			if state.ArgStack.Top < 1 {
				return fmt.Errorf("invalid stack size for %s in private Dict", op)
			}
			priv.vsindex = int32(state.ArgStack.Pop())
			return nil
		case 23: // "blend"
			return nil
		}
	} else { // 2-byte operators. The first byte is the escape byte.
		switch op.Operator {
		case 9, 10, 11, 17, 18: // "BlueScale" "BlueShift" "BlueFuzz" "LanguageGroup" "ExpansionFactor"
			return state.ArgStack.PopN(1)
		case 12, 13: //  "StemSnapH"  "StemSnapV"
			return state.ArgStack.PopN(-2)
		}
	}
	return errors.New("invalid operand in private Dict charstring")
}
