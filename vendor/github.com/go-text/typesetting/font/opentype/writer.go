package opentype

import (
	"encoding/binary"
	"math"
)

// Table is one opentype binary table and its tag.
type Table struct {
	Content []byte
	Tag     Tag
}

// WriteTTF creates a single Truetype font file (.ttf) from the given [tables] slice,
// which must be sorted by Tag
func WriteTTF(tables []Table) []byte {
	introLength := uint32(otfHeaderSize + len(tables)*otfEntrySize)
	buffer := make([]byte, introLength)

	writeTTFHeader(len(tables), buffer)

	tableOffset := introLength // the actual content will start after the header + table directory
	for i, table := range tables {
		cs := checksum(table.Content)
		tableLength := uint32(len(table.Content))

		slice := buffer[otfHeaderSize+i*otfEntrySize:]
		binary.BigEndian.PutUint32(slice, uint32(table.Tag))
		binary.BigEndian.PutUint32(slice[4:], cs)
		binary.BigEndian.PutUint32(slice[8:], tableOffset)
		binary.BigEndian.PutUint32(slice[12:], tableLength)

		// update the offset
		tableOffset = tableOffset + tableLength
	}

	// append the actual table content :
	// allocate only once
	buffer = append(buffer, make([]byte, tableOffset-introLength)...)
	tableOffset = introLength
	for _, table := range tables {
		copy(buffer[tableOffset:], table.Content)
		tableOffset = tableOffset + uint32(len(table.Content))
	}

	return buffer
}

// out is assumed to have a length >= ttfHeaderSize
func writeTTFHeader(nTables int, out []byte) {
	log2 := math.Floor(math.Log2(float64(nTables)))
	// Maximum power of 2 less than or equal to numTables, times 16 ((2**floor(log2(numTables))) * 16, where “**” is an exponentiation operator).
	searchRange := math.Pow(2, log2) * 16
	// Log2 of the maximum power of 2 less than or equal to numTables (log2(searchRange/16), which is equal to floor(log2(numTables))).
	entrySelector := log2
	// numTables times 16, minus searchRange ((numTables * 16) - searchRange).
	rangeShift := nTables*16 - int(searchRange)

	binary.BigEndian.PutUint32(out[:], uint32(TrueType))
	binary.BigEndian.PutUint16(out[4:], uint16(nTables))
	binary.BigEndian.PutUint16(out[6:], uint16(searchRange))
	binary.BigEndian.PutUint16(out[8:], uint16(entrySelector))
	binary.BigEndian.PutUint16(out[10:], uint16(rangeShift))
}

func checksum(table []byte) uint32 {
	// "To accommodate data with a length that is not a multiple of four,
	// the above algorithm must be modified to treat the data as though
	// it contains zero padding to a length that is a multiple of four."
	if r := len(table) % 4; r != 0 {
		table = append(table, make([]byte, r)...)
	}

	var sum uint32
	for i := 0; i < len(table)/4; i++ {
		sum += binary.BigEndian.Uint32(table[i*4:])
	}

	return sum
}
