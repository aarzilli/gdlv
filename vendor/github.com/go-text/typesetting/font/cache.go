package font

type glyphExtents struct {
	valid   bool
	extents GlyphExtents
}

type extentsCache []glyphExtents

func (ec extentsCache) get(gid GID) (GlyphExtents, bool) {
	if int(gid) >= len(ec) {
		return GlyphExtents{}, false
	}
	ge := ec[gid]
	return ge.extents, ge.valid
}

func (ec extentsCache) set(gid GID, extents GlyphExtents) {
	if int(gid) >= len(ec) {
		return
	}
	ec[gid].valid = true
	ec[gid].extents = extents
}

func (ec extentsCache) reset() {
	for i := range ec {
		ec[i] = glyphExtents{}
	}
}

func (f *Face) GlyphExtents(glyph GID) (GlyphExtents, bool) {
	if e, ok := f.extentsCache.get(glyph); ok {
		return e, ok
	}
	e, ok := f.glyphExtentsRaw(glyph)
	if ok {
		f.extentsCache.set(glyph, e)
	}
	return e, ok
}
