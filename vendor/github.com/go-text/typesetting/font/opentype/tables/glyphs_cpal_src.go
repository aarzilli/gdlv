package tables

// https://learn.microsoft.com/en-us/typography/opentype/spec/cpal
//
// For now, only the CPAL version 0 is supported.
type CPAL struct {
	Version            uint16        //	Table version number
	NumPaletteEntries  uint16        //	Number of palette entries in each palette.
	numPalettes        uint16        //	Number of palettes in the table.
	numColorRecords    uint16        //	Total number of color records, combined for all palettes.
	ColorRecordsArray  []ColorRecord `arrayCount:"ComputedField-numColorRecords" offsetSize:"Offset32"` // Offset from the beginning of CPAL table to the first ColorRecord.
	ColorRecordIndices []uint16      `arrayCount:"ComputedField-numPalettes"`                           // [numPalettes] Index of each palette’s first color record in the combined color record array.
}

type ColorRecord struct {
	Blue  uint8 // Blue value (B0).
	Green uint8 // Green value (B1).
	Red   uint8 // Red value (B2).
	Alpha uint8 // Alpha value (B3).
}
