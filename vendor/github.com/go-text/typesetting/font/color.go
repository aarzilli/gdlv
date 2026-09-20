package font

import (
	"errors"
	"fmt"

	"github.com/go-text/typesetting/font/opentype/tables"
)

// Support for COLR and CPAL tables

// CPAL is the 'CPAL' table,
// with [numPalettes]x[numPaletteEntries] colors.
// CPAL[0] is the default palette
type CPAL [][]tables.ColorRecord

func newCPAL(table tables.CPAL) (CPAL, error) {
	numPalettes := len(table.ColorRecordIndices)
	numColors := len(table.ColorRecordsArray)

	// "The first palette, palette index 0, is the default palette.
	// A minimum of one palette must be provided in the CPAL table if the table is present.
	// Palettes must have a minimum of one color record. An empty CPAL table,
	// with no palettes and no color records is not permitted."
	if numPalettes == 0 {
		return nil, errors.New("empty CPAL table")
	}
	out := make(CPAL, numPalettes)
	for i, startIndex := range table.ColorRecordIndices {
		endIndex := int(startIndex) + int(table.NumPaletteEntries)
		if endIndex > numColors {
			return nil, fmt.Errorf("invalid CPAL table (expected at least %d colors, got %d)", endIndex, numColors)
		}
		out[i] = table.ColorRecordsArray[startIndex:endIndex]
	}
	return out, nil
}
