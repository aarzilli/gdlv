package fontscan

import (
	"fmt"
	"os"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// Location identifies where a font.Face is stored.
type Location = font.FontID

// Footprint is a condensed summary of the main information
// about a font, serving as a lightweight surrogate
// for the original font file.
type Footprint struct {
	// Location stores the adress of the font resource.
	Location Location

	// Family is the general nature of the font, like
	// "Arial"
	// Note that, for performance reason, we store the
	// normalized version of the family name.
	Family string

	// Runes is the set of runes supported by the font.
	Runes RuneSet

	// Scripts is the set of scripts deduced from [Runes]
	Scripts ScriptSet

	// Langs is the set of languages deduced from [Runes]
	Langs LangSet

	// Aspect precises the visual characteristics
	// of the font among a family, like "Bold Italic"
	Aspect font.Aspect

	// isUserProvided is set to true for fonts add manually to
	// a FontMap
	// User fonts will always be tried if no other fonts match,
	// and will have priority among font with same family name.
	//
	// This field is not serialized in the index, since it is always false
	// for system fonts.
	isUserProvided bool
}

func newFootprintFromFont(f *font.Font, location Location, md font.Description) (out Footprint) {
	out.Runes, out.Scripts, _ = newCoveragesFromCmap(f.Cmap, nil)
	out.Langs = newLangsetFromCoverage(out.Runes)
	out.Family = font.NormalizeFamily(md.Family)
	out.Aspect = md.Aspect
	out.Location = location
	out.isUserProvided = true
	return out
}

func newFootprintFromLoader(ld *ot.Loader, isUserProvided bool, buffer scanBuffer) (out Footprint, _ scanBuffer, err error) {
	raw := buffer.tableBuffer

	// since raw is shared, special car must be taken in the parsing order

	raw, _ = ld.RawTableTo(ot.MustNewTag("OS/2"), raw)
	fp := tables.FPNone
	if os2, _, err := tables.ParseOs2(raw); err != nil {
		fp = os2.FontPage()
	}

	// we can use the buffer since ProcessCmap do not keep any reference on
	// the input slice
	raw, err = ld.RawTableTo(ot.MustNewTag("cmap"), raw)
	if err != nil {
		return Footprint{}, buffer, err
	}
	tb, _, err := tables.ParseCmap(raw)
	if err != nil {
		return Footprint{}, buffer, err
	}
	cmap, _, err := font.ProcessCmap(tb, fp)
	if err != nil {
		return Footprint{}, buffer, err
	}

	out.Runes, out.Scripts, buffer.cmapBuffer = newCoveragesFromCmap(cmap, buffer.cmapBuffer) // ... and build the corresponding rune set

	out.Langs = newLangsetFromCoverage(out.Runes)

	desc, raw := font.Describe(ld, raw)
	out.Family = font.NormalizeFamily(desc.Family)
	out.Aspect = desc.Aspect
	out.isUserProvided = isUserProvided

	buffer.tableBuffer = raw

	return out, buffer, nil
}

// loadFromDisk assume the footprint location refers to the file system
func (fp *Footprint) loadFromDisk() (*font.Face, error) {
	location := fp.Location

	file, err := os.Open(location.File)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	loaders, err := ot.NewLoaders(file)
	if err != nil {
		return nil, err
	}

	if index := int(location.Index); len(loaders) <= index {
		// this should only happen if the font file as changed
		// since the last scan (very unlikely)
		return nil, fmt.Errorf("invalid font index in collection: %d >= %d", index, len(loaders))
	}

	ft, err := font.NewFont(loaders[location.Index])
	if err != nil {
		return nil, fmt.Errorf("reading font at %s: %s", location.File, err)
	}

	return font.NewFace(ft), nil
}
