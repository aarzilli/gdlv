package harfbuzz

import (
	"fmt"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
	ucd "github.com/go-text/typesetting/internal/unicodedata"
)

// ported from harfbuzz/src/hb-ot-layout-gsubgpos.hh Copyright © 2007,2008,2009,2010  Red Hat, Inc. 2010,2012  Google, Inc.  Behdad Esfahbod

// GSUB or GPOS lookup
type layoutLookup interface {
	// accumulate the subtables coverage into the diggest
	collectCoverage(*setDigest)
	// walk the subtables to add them to the context
	dispatchSubtables(*getSubtablesContext)

	// walk the subtables and apply the sub/pos
	dispatchApply(ctx *otApplyContext) bool

	Props() uint32
	isReverse() bool
}

/*
 * GSUB/GPOS Common
 */

const ignoreFlags = otIgnoreBaseGlyphs | otIgnoreLigatures | otIgnoreMarks

// use a digest to speedup match
type otLayoutLookupAccelerator struct {
	lookup    layoutLookup
	subtables getSubtablesContext
	digest    setDigest
}

func (ac *otLayoutLookupAccelerator) init(lookup layoutLookup) {
	ac.lookup = lookup
	ac.digest = setDigest{}
	lookup.collectCoverage(&ac.digest)
	ac.subtables = nil
	lookup.dispatchSubtables(&ac.subtables)
}

// apply the subtables and stops at the first success.
func (ac *otLayoutLookupAccelerator) apply(c *otApplyContext) bool {
	for _, table := range ac.subtables {
		if table.apply(c) {
			return true
		}
	}
	return false
}

// represents one layout subtable, with its own coverage
type applicable struct {
	objApply func(c *otApplyContext) bool

	digest setDigest
}

func newGSUBApplicable(table tables.GSUBLookup) applicable {
	ap := applicable{objApply: func(c *otApplyContext) bool { return c.applyGSUB(table) }}
	ap.digest.collectCoverage(table.Cov())
	return ap
}

func newGPOSApplicable(table tables.GPOSLookup) applicable {
	ap := applicable{objApply: func(c *otApplyContext) bool { return c.applyGPOS(table) }}
	ap.digest.collectCoverage(table.Cov())
	return ap
}

func (ap applicable) apply(c *otApplyContext) bool {
	return ap.digest.mayHave(gID(c.buffer.cur(0).Glyph)) && ap.objApply(c)
}

type getSubtablesContext []applicable

// one for GSUB, one for GPOS (known at compile time)
type otProxyMeta struct {
	recurseFunc recurseFunc
	tableIndex  uint8 // 0 for GSUB, 1 for GPOS
	inplace     bool
}

var (
	proxyGSUB = otProxyMeta{tableIndex: 0, inplace: false, recurseFunc: applyRecurseGSUB}
	proxyGPOS = otProxyMeta{tableIndex: 1, inplace: true, recurseFunc: applyRecurseGPOS}
)

type otProxy struct {
	otProxyMeta
	accels []otLayoutLookupAccelerator
}

type wouldApplyContext struct {
	glyphs      []GID
	indices     []uint16 // see get1N
	zeroContext bool
}

// `value` interpretation is dictated by the context
type matcherFunc = func(gid gID, value uint16) bool

// interprets `value` as a Glyph
func matchGlyph(gid gID, value uint16) bool { return gid == gID(value) }

// interprets `value` as a Class
func matchClass(class tables.ClassDef) matcherFunc {
	return func(gid gID, value uint16) bool {
		c, _ := class.Class(gid)
		return uint16(c) == value
	}
}

// interprets `value` as an index in coverage array
func matchCoverage(covs []tables.Coverage) matcherFunc {
	return func(gid gID, value uint16) bool {
		_, covered := covs[value].Index(gid)
		return covered
	}
}

const (
	no uint8 = iota
	yes
	maybe
)

type otApplyContextMatcher struct {
	matchFunc    matcherFunc
	lookupProps  uint32
	mask         GlyphMask
	ignoreZWNJ   bool
	ignoreZWJ    bool
	ignoreHidden bool
	perSyllable  bool
	syllable     uint8
}

func (m *otApplyContextMatcher) init(c *otApplyContext, contextMatch bool) {
	m.matchFunc = nil
	m.lookupProps = c.lookupProps
	/* Ignore ZWNJ if we are matching GPOS, or matching GSUB context and asked to. */
	m.ignoreZWNJ = c.tableIndex == 1 || (contextMatch && c.autoZWNJ)
	/* Ignore ZWJ if we are matching context, or asked to. */
	m.ignoreZWJ = contextMatch || c.autoZWJ
	/* Ignore hidden glyphs (like CGJ) during GPOS. */
	m.ignoreHidden = c.tableIndex == 1
	if contextMatch {
		m.mask = math.MaxUint32
	} else {
		m.mask = c.lookupMask
	}
	// Per syllable matching is only for GSUB.
	m.perSyllable = c.tableIndex == 0 && c.perSyllable
	m.syllable = 0
}

func (m otApplyContextMatcher) mayMatch(info *GlyphInfo, glyphData []uint16) uint8 {
	if info.Mask&m.mask == 0 || (m.perSyllable && m.syllable != 0 && m.syllable != info.syllable) {
		return no
	}

	if m.matchFunc != nil {
		if m.matchFunc(gID(info.Glyph), glyphData[0]) {
			return yes
		}
		return no
	}

	return maybe
}

func (m otApplyContextMatcher) maySkip(c *otApplyContext, info *GlyphInfo) uint8 {
	if !c.checkGlyphProperty(info, m.lookupProps) {
		return yes
	}

	if info.isDefaultIgnorable() &&
		(m.ignoreZWNJ || !info.isZwnj()) &&
		(m.ignoreZWJ || !info.isZwj()) &&
		(m.ignoreHidden || !info.isHidden()) {
		return maybe
	}

	return no
}

type skippingIterator struct {
	c       *otApplyContext
	matcher otApplyContextMatcher

	matchGlyphDataArray []uint16
	matchGlyphDataStart int // start as index in matchGlyphDataArray

	idx int
	end int
}

func (it *skippingIterator) init(c *otApplyContext, contextMatch bool) {
	it.c = c
	it.end = len(c.buffer.Info)
	it.matchGlyphDataArray = it.matchGlyphDataArray[:0]
	it.matchGlyphDataStart = 0
	it.matcher.init(c, contextMatch)
}

func (it *skippingIterator) setMatchFunc(matchFunc matcherFunc, glyphData []uint16) {
	it.matcher.matchFunc = matchFunc
	it.matchGlyphDataArray = glyphData
	it.matchGlyphDataStart = 0
}

func (it *skippingIterator) reset(startIndex int) {
	// For GSUB forward iterator
	it.idx = startIndex
	it.end = len(it.c.buffer.Info)
	it.matcher.syllable = it.c.buffer.cur(0).syllable
}

func (it *skippingIterator) resetBack(startIndex int) {
	// For GSUB backward iterator
	it.idx = startIndex
	it.matcher.syllable = it.c.buffer.cur(0).syllable
}

func (it *skippingIterator) resetFast(startIndex int) {
	// Doesn't set end or syllable. Used by GPOS which doesn't care / change.
	it.idx = startIndex
}

func (it *skippingIterator) maySkip(info *GlyphInfo) uint8 { return it.matcher.maySkip(it.c, info) }

type matchRes uint8

const (
	match matchRes = iota
	notMatch
	skip
)

func (it *skippingIterator) match(info *GlyphInfo) matchRes {
	skipR := it.matcher.maySkip(it.c, info)
	if skipR == yes {
		return skip
	}

	matchR := it.matcher.mayMatch(info, it.matchGlyphDataArray[it.matchGlyphDataStart:])
	if matchR == yes || (matchR == maybe && skipR == no) {
		return match
	}

	if skipR == no {
		return notMatch
	}

	return skip
}

func (it *skippingIterator) next() (_ bool, unsafeTo int) {
	stop := it.end - 1
	for it.idx < stop {
		it.idx++
		info := &it.c.buffer.Info[it.idx]
		switch it.match(info) {
		case match:
			if len(it.matchGlyphDataArray) != 0 {
				it.matchGlyphDataStart++
			}
			return true, 0
		case notMatch:
			return false, it.idx + 1
		case skip:
			continue
		}
	}
	return false, it.end
}

func (it *skippingIterator) prev() (_ bool, unsafeFrom int) {
	stop := 0
	L := len(it.c.buffer.outInfo)
	for it.idx > stop {
		it.idx--
		var info *GlyphInfo
		if it.idx < L {
			info = &it.c.buffer.outInfo[it.idx]
		} else {
			// we are in "position mode" : outInfo is not used anymore
			// in the C implementation, outInfo and info now are sharing the same storage
			info = &it.c.buffer.Info[it.idx]
		}

		switch it.match(info) {
		case match:
			if len(it.matchGlyphDataArray) != 0 {
				it.matchGlyphDataStart++
			}
			return true, 0
		case notMatch:
			return false, max(1, it.idx) - 1
		case skip:
			continue
		}
	}
	return false, 0
}

type recurseFunc = func(c *otApplyContext, lookupIndex uint16) bool

type otApplyContext struct {
	font   *Font
	buffer *Buffer

	recurseFunc recurseFunc
	gdef        tables.GDEF
	varStore    tables.ItemVarStore
	indices     []uint16 // see get1N()

	iterContext skippingIterator
	iterInput   skippingIterator

	nestingLevelLeft int
	tableIndex       uint8 // 0 for GSUB, 1 for GPOS
	lookupMask       GlyphMask
	lookupProps      uint32
	randomState      uint32
	lookupIndex      uint16
	direction        Direction

	hasGlyphClasses bool
	autoZWNJ        bool
	autoZWJ         bool
	perSyllable     bool
	newSyllables    uint8 // 0xFF for undefined
	random          bool

	lastBase      int // GPOS uses
	lastBaseUntil int // GPOS uses

	matchPositions []int
}

func (c *otApplyContext) reset(tableIndex uint8, font *Font, buffer *Buffer) {
	c.font = font
	c.buffer = buffer

	c.recurseFunc = nil
	c.gdef = font.face.GDEF
	c.varStore = c.gdef.ItemVarStore
	c.indices = c.indices[:0]

	c.nestingLevelLeft = maxNestingLevel
	c.tableIndex = tableIndex
	c.lookupMask = 1
	c.lookupProps = 0
	c.randomState = 1
	c.lookupIndex = 0
	c.direction = buffer.Props.Direction

	c.hasGlyphClasses = c.gdef.GlyphClassDef != nil
	c.autoZWNJ = true
	c.autoZWJ = true
	c.perSyllable = false
	c.newSyllables = 0xFF
	c.random = false

	c.lastBase = -1
	c.lastBaseUntil = 0

	ensureLen(&c.matchPositions, 8)

	// iterContext
	// iterInput
	c.initIters()
}

func (c *otApplyContext) initIters() {
	c.iterInput.init(c, false)
	c.iterContext.init(c, true)
}

func (c *otApplyContext) setLookupMask(mask GlyphMask) {
	c.lookupMask = mask
	c.initIters()
}

func (c *otApplyContext) setLookupProps(lookupProps uint32) {
	c.lookupProps = lookupProps
	c.initIters()
}

func (c *otApplyContext) applyRecurseLookup(lookupIndex uint16, l layoutLookup) bool {
	savedLookupProps := c.lookupProps
	savedLookupIndex := c.lookupIndex

	c.lookupIndex = lookupIndex
	c.setLookupProps(l.Props())

	savedMatchPositions := c.matchPositions
	var stackMatchPositions [8]int
	c.matchPositions = stackMatchPositions[:]

	ret := l.dispatchApply(c)

	c.lookupIndex = savedLookupIndex
	c.setLookupProps(savedLookupProps)

	c.matchPositions = savedMatchPositions

	return ret
}

func (c *otApplyContext) substituteLookup(accel *otLayoutLookupAccelerator) {
	c.applyString(proxyGSUB, accel)
}

func (c *otApplyContext) checkGlyphProperty(info *GlyphInfo, matchProps uint32) bool {
	glyphProps := info.glyphProps

	/* Not covered, if, for example, glyph class is ligature and
	 * matchProps includes LookupFlags::IgnoreLigatures */
	if (glyphProps & uint16(matchProps) & ignoreFlags) != 0 {
		return false
	}

	if glyphProps&tables.GPMark != 0 {
		return c.matchPropertiesMark(info, glyphProps, matchProps)
	}

	return true
}

func (c *otApplyContext) matchPropertiesMark(info *GlyphInfo, glyphProps uint16, matchProps uint32) bool {
	/* If using mark filtering sets, the high uint16 of
	 * matchProps has the set index. */
	if uint16(matchProps)&font.UseMarkFilteringSet != 0 {
		_, has := c.gdef.MarkGlyphSetsDef.Coverages[matchProps>>16].Index(gID(info.Glyph))
		return has
	}

	/* The second byte of matchProps has the meaning
	 * "ignore marks of attachment type different than
	 * the attachment type specified." */
	if uint16(matchProps)&otMarkAttachmentType != 0 {
		return uint16(matchProps)&otMarkAttachmentType == (glyphProps & otMarkAttachmentType)
	}

	return true
}

func (c *otApplyContext) setGlyphClass(glyphIndex GID) {
	c.setGlyphClassExt(glyphIndex, 0, false, false)
}

func (c *otApplyContext) setGlyphClassExt(glyphIndex_ GID, classGuess uint16, ligature, component bool) {
	glyphIndex := gID(glyphIndex_)

	c.buffer.digest.add(glyphIndex)

	if c.newSyllables != 0xFF {
		c.buffer.cur(0).syllable = c.newSyllables
	}

	props := c.buffer.cur(0).glyphProps | substituted
	if ligature {
		props |= ligated
		// In the only place that the MULTIPLIED bit is used, Uniscribe
		// seems to only care about the "last" transformation between
		// Ligature and Multiple substitutions.  Ie. if you ligate, expand,
		// and ligate again, it forgives the multiplication and acts as
		// if only ligation happened.  As such, clear MULTIPLIED bit.
		props &= ^multiplied
	}
	if component {
		props |= multiplied
	}
	if c.hasGlyphClasses {
		props &= preserve
		c.buffer.cur(0).glyphProps = props | c.gdef.GlyphProps(glyphIndex)
	} else if classGuess != 0 {
		props &= preserve
		c.buffer.cur(0).glyphProps = props | classGuess
	} else {
		c.buffer.cur(0).glyphProps = props
	}
}

func (c *otApplyContext) replaceGlyph(glyphIndex GID) {
	c.setGlyphClass(glyphIndex)
	c.buffer.replaceGlyphIndex(glyphIndex)
}

func (c *otApplyContext) randomNumber() uint32 {
	/* http://www.cplusplus.com/reference/random/minstd_rand/ */
	c.randomState = c.randomState * 48271 % 2147483647
	return c.randomState
}

func (c *otApplyContext) applyRuleSet(ruleSet tables.SequenceRuleSet, match matcherFunc) bool {
	for _, rule := range ruleSet.SeqRule {
		// the first which match is applied
		applied := c.contextApplyLookup(rule.InputSequence, rule.SeqLookupRecords, match)
		if applied {
			return true
		}
	}
	return false
}

func (c *otApplyContext) applyChainRuleSet(ruleSet tables.ChainedClassSequenceRuleSet, match [3]matcherFunc) bool {
	for i, rule := range ruleSet.ChainedSeqRules {

		if debugMode {
			fmt.Println("APPLY - chain rule number", i)
		}

		b := c.chainContextApplyLookup(rule.BacktrackSequence, rule.InputSequence, rule.LookaheadSequence, rule.SeqLookupRecords, match)
		if b { // stop at the first application
			return true
		}
	}
	return false
}

// `input` starts with second glyph (`inputCount` = len(input)+1)
func (c *otApplyContext) contextApplyLookup(input []uint16, lookupRecord []tables.SequenceLookupRecord, lookupContext matcherFunc) bool {
	if len(input)+1 > maxContextLength {
		return false
	}
	hasMatch, matchEnd, _ := c.matchInput(input, lookupContext)
	if hasMatch {
		c.buffer.unsafeToBreak(c.buffer.idx, matchEnd)
		c.applyLookup(len(input)+1, lookupRecord, matchEnd)
		return true
	} else {
		c.buffer.unsafeToConcat(c.buffer.idx, matchEnd)
		return false
	}
}

//	`input` starts with second glyph (`inputCount` = len(input)+1)
//
// lookupsContexts : backtrack, input, lookahead
func (c *otApplyContext) chainContextApplyLookup(backtrack, input, lookahead []uint16,
	lookupRecord []tables.SequenceLookupRecord, lookupContexts [3]matcherFunc,
) bool {
	if len(input)+1 > maxContextLength {
		return false
	}

	hasMatch, matchEnd, _ := c.matchInput(input, lookupContexts[1])
	endIndex := matchEnd
	if !(hasMatch && endIndex != 0) {
		c.buffer.unsafeToConcat(c.buffer.idx, endIndex)
		return false
	}

	hasMatch, endIndex = c.matchLookahead(lookahead, lookupContexts[2], matchEnd)
	if !hasMatch {
		c.buffer.unsafeToConcat(c.buffer.idx, endIndex)
		return false
	}

	hasMatch, startIndex := c.matchBacktrack(backtrack, lookupContexts[0])
	if !hasMatch {
		c.buffer.unsafeToConcatFromOutbuffer(startIndex, endIndex)
		return false
	}

	c.buffer.unsafeToBreakFromOutbuffer(startIndex, endIndex)
	c.applyLookup(len(input)+1, lookupRecord, matchEnd)
	return true
}

func (c *wouldApplyContext) wouldApplyLookupContext1(data tables.SequenceContextFormat1, index int) bool {
	if index >= len(data.SeqRuleSet) { // index is not sanitized in tt.Parse
		return false
	}
	ruleSet := data.SeqRuleSet[index]
	return c.wouldApplyRuleSet(ruleSet, matchGlyph)
}

func (c *wouldApplyContext) wouldApplyLookupContext2(data tables.SequenceContextFormat2, _ int, glyphID GID) bool {
	class, _ := data.ClassDef.Class(gID(glyphID))
	ruleSet := data.ClassSeqRuleSet[class]
	return c.wouldApplyRuleSet(ruleSet, matchClass(data.ClassDef))
}

func (c *wouldApplyContext) wouldApplyLookupContext3(data tables.SequenceContextFormat3, _ int) bool {
	covIndices := get1N(&c.indices, 1, len(data.Coverages))
	return c.wouldMatchInput(covIndices, matchCoverage(data.Coverages))
}

func (c *wouldApplyContext) wouldApplyRuleSet(ruleSet tables.SequenceRuleSet, match matcherFunc) bool {
	for _, rule := range ruleSet.SeqRule {
		if c.wouldMatchInput(rule.InputSequence, match) {
			return true
		}
	}
	return false
}

func (c *wouldApplyContext) wouldApplyChainRuleSet(ruleSet tables.ChainedSequenceRuleSet, inputMatch matcherFunc) bool {
	for _, rule := range ruleSet.ChainedSeqRules {
		if c.wouldApplyChainLookup(rule.BacktrackSequence, rule.InputSequence, rule.LookaheadSequence, inputMatch) {
			return true
		}
	}
	return false
}

func (c *wouldApplyContext) wouldApplyLookupChainedContext1(data tables.ChainedSequenceContextFormat1, index int) bool {
	if index >= len(data.ChainedSeqRuleSet) { // index is not sanitized in tt.Parse
		return false
	}
	ruleSet := data.ChainedSeqRuleSet[index]
	return c.wouldApplyChainRuleSet(ruleSet, matchGlyph)
}

func (c *wouldApplyContext) wouldApplyLookupChainedContext2(data tables.ChainedSequenceContextFormat2, _ int, glyphID GID) bool {
	class, _ := data.InputClassDef.Class(gID(glyphID))
	ruleSet := data.ChainedClassSeqRuleSet[class]
	return c.wouldApplyChainRuleSet(ruleSet, matchClass(data.InputClassDef))
}

func (c *wouldApplyContext) wouldApplyLookupChainedContext3(data tables.ChainedSequenceContextFormat3, _ int) bool {
	lB, lI, lL := len(data.BacktrackCoverages), len(data.InputCoverages), len(data.LookaheadCoverages)
	return c.wouldApplyChainLookup(get1N(&c.indices, 0, lB), get1N(&c.indices, 1, lI), get1N(&c.indices, 0, lL),
		matchCoverage(data.InputCoverages))
}

// `input` starts with second glyph (`inputCount` = len(input)+1)
// only the input lookupsContext is needed
func (c *wouldApplyContext) wouldApplyChainLookup(backtrack, input, lookahead []uint16, inputLookupContext matcherFunc) bool {
	contextOk := true
	if c.zeroContext {
		contextOk = len(backtrack) == 0 && len(lookahead) == 0
	}
	return contextOk && c.wouldMatchInput(input, inputLookupContext)
}

// `input` starts with second glyph (`count` = len(input)+1)
func (c *wouldApplyContext) wouldMatchInput(input []uint16, matchFunc matcherFunc) bool {
	if len(c.glyphs) != len(input)+1 {
		return false
	}

	for i, glyph := range input {
		if !matchFunc(gID(c.glyphs[i+1]), glyph) {
			return false
		}
	}

	return true
}

// `input` starts with second glyph (`inputCount` = len(input)+1)
func (c *otApplyContext) matchInput(input []uint16, matchFunc matcherFunc) (_ bool, endPosition int, totalComponentCount uint8) {
	buffer := c.buffer
	count := len(input) + 1
	if count == 1 {
		endPosition = buffer.idx + 1
		c.matchPositions[0] = buffer.idx
		totalComponentCount = buffer.cur(0).getLigNumComps()
		return true, endPosition, totalComponentCount
	}

	if count > maxContextLength {
		return false, 0, 0
	}
	skippyIter := &c.iterInput
	skippyIter.reset(buffer.idx)
	skippyIter.setMatchFunc(matchFunc, input)

	/*
	* This is perhaps the trickiest part of OpenType...  Remarks:
	*
	* - If all components of the ligature were marks, we call this a mark ligature.
	*
	* - If there is no GDEF, and the ligature is NOT a mark ligature, we categorize
	*   it as a ligature glyph.
	*
	* - Ligatures cannot be formed across glyphs attached to different components
	*   of previous ligatures.  Eg. the sequence is LAM,SHADDA,LAM,FATHA,HEH, and
	*   LAM,LAM,HEH form a ligature, leaving SHADDA,FATHA next to eachother.
	*   However, it would be wrong to ligate that SHADDA,FATHA sequence.
	*   There are a couple of exceptions to this:
	*
	*   o If a ligature tries ligating with marks that belong to it itself, go ahead,
	*     assuming that the font designer knows what they are doing (otherwise it can
	*     break Indic stuff when a matra wants to ligate with a conjunct,
	*
	*   o If two marks want to ligate and they belong to different components of the
	*     same ligature glyph, and said ligature glyph is to be ignored according to
	*     mark-filtering rules, then allow.
	*     https://github.com/harfbuzz/harfbuzz/issues/545
	 */

	firstLigID := buffer.cur(0).getLigID()
	firstLigComp := buffer.cur(0).getLigComp()

	const (
		ligbaseNotChecked = iota
		ligbaseMayNotSkip
		ligbaseMaySkip
	)
	ligbase := ligbaseNotChecked
	for i := 1; i < count; i++ {
		if ok, unsafeTo := skippyIter.next(); !ok {
			return false, unsafeTo, 0
		}

		ensureLen(&c.matchPositions, i+1)

		c.matchPositions[i] = skippyIter.idx

		thisLigID := buffer.Info[skippyIter.idx].getLigID()
		thisLigComp := buffer.Info[skippyIter.idx].getLigComp()
		if firstLigID != 0 && firstLigComp != 0 {
			/* If first component was attached to a previous ligature component,
			* all subsequent components should be attached to the same ligature
			* component, otherwise we shouldn't ligate them... */
			if firstLigID != thisLigID || firstLigComp != thisLigComp {
				/* ...unless, we are attached to a base ligature and that base
				 * ligature is ignorable. */
				if ligbase == ligbaseNotChecked {
					found := false
					out := buffer.outInfo
					j := len(out)
					for j != 0 && out[j-1].getLigID() == firstLigID {
						if out[j-1].getLigComp() == 0 {
							j--
							found = true
							break
						}
						j--
					}

					if found && skippyIter.maySkip(&out[j]) == yes {
						ligbase = ligbaseMaySkip
					} else {
						ligbase = ligbaseMayNotSkip
					}
				}

				if ligbase == ligbaseMayNotSkip {
					return false, 0, 0
				}
			}
		} else {
			/* If first component was NOT attached to a previous ligature component,
			* all subsequent components should also NOT be attached to any ligature
			* component, unless they are attached to the first component itself! */
			if thisLigID != 0 && thisLigComp != 0 && (thisLigID != firstLigID) {
				return false, 0, 0
			}
		}

		totalComponentCount += buffer.Info[skippyIter.idx].getLigNumComps()
	}

	endPosition = skippyIter.idx + 1
	totalComponentCount += buffer.cur(0).getLigNumComps()
	c.matchPositions[0] = buffer.idx

	return true, endPosition, totalComponentCount
}

// `count` and `c.matchPositions` include the first glyph
func (c *otApplyContext) ligateInput(count, matchEnd int, ligGlyph gID, totalComponentCount uint8) {
	buffer := c.buffer

	buffer.mergeClusters(buffer.idx, matchEnd)

	/* - If a base and one or more marks ligate, consider that as a base, NOT
	*   ligature, such that all following marks can still attach to it.
	*   https://github.com/harfbuzz/harfbuzz/issues/1109
	*
	* - If all components of the ligature were marks, we call this a mark ligature.
	*   If it *is* a mark ligature, we don't allocate a new ligature id, and leave
	*   the ligature to keep its old ligature id.  This will allow it to attach to
	*   a base ligature in GPOS.  Eg. if the sequence is: LAM,LAM,SHADDA,FATHA,HEH,
	*   and LAM,LAM,HEH for a ligature, they will leave SHADDA and FATHA with a
	*   ligature id and component value of 2.  Then if SHADDA,FATHA form a ligature
	*   later, we don't want them to lose their ligature id/component, otherwise
	*   GPOS will fail to correctly position the mark ligature on top of the
	*   LAM,LAM,HEH ligature.  See:
	*     https://bugzilla.gnome.org/show_bug.cgi?id=676343
	*
	* - If a ligature is formed of components that some of which are also ligatures
	*   themselves, and those ligature components had marks attached to *their*
	*   components, we have to attach the marks to the new ligature component
	*   positions!  Now *that*'s tricky!  And these marks may be following the
	*   last component of the whole sequence, so we should loop forward looking
	*   for them and update them.
	*
	*   Eg. the sequence is LAM,LAM,SHADDA,FATHA,HEH, and the font first forms a
	*   'calt' ligature of LAM,HEH, leaving the SHADDA and FATHA with a ligature
	*   id and component == 1.  Now, during 'liga', the LAM and the LAM-HEH ligature
	*   form a LAM-LAM-HEH ligature.  We need to reassign the SHADDA and FATHA to
	*   the new ligature with a component value of 2.
	*
	*   This in fact happened to a font...  See:
	*   https://bugzilla.gnome.org/show_bug.cgi?id=437633
	 */

	isBaseLigature := buffer.Info[c.matchPositions[0]].isBaseGlyph()
	isMarkLigature := buffer.Info[c.matchPositions[0]].isMark()
	for i := 1; i < count; i++ {
		if !buffer.Info[c.matchPositions[i]].isMark() {
			isBaseLigature = false
			isMarkLigature = false
			break
		}
	}
	isLigature := !isBaseLigature && !isMarkLigature

	klass, ligID := uint16(0), uint8(0)
	if isLigature {
		klass = tables.GPLigature
		ligID = buffer.allocateLigID()
	}
	lastLigID := buffer.cur(0).getLigID()
	lastNumComponents := buffer.cur(0).getLigNumComps()
	componentsSoFar := lastNumComponents

	if isLigature {
		buffer.cur(0).setLigPropsForLigature(ligID, totalComponentCount)
		if buffer.cur(0).unicode.generalCategory() == ucd.Mn {
			buffer.cur(0).setGeneralCategory(ucd.Lo)
		}
	}

	// ReplaceGlyph_with_ligature
	c.setGlyphClassExt(GID(ligGlyph), klass, true, false)
	buffer.replaceGlyphIndex(GID(ligGlyph))

	for i := 1; i < count; i++ {
		for buffer.idx < c.matchPositions[i] {
			if isLigature {
				thisComp := buffer.cur(0).getLigComp()
				if thisComp == 0 {
					thisComp = lastNumComponents
				}
				newLigComp := componentsSoFar - lastNumComponents +
					min8(thisComp, lastNumComponents)
				buffer.cur(0).setLigPropsForMark(ligID, newLigComp)
			}
			buffer.nextGlyph()
		}

		lastLigID = buffer.cur(0).getLigID()
		lastNumComponents = buffer.cur(0).getLigNumComps()
		componentsSoFar += lastNumComponents

		/* Skip the base glyph */
		buffer.skipGlyph()
	}

	if !isMarkLigature && lastLigID != 0 {
		/* Re-adjust components for any marks following. */
		for i := buffer.idx; i < len(buffer.Info); i++ {
			if lastLigID != buffer.Info[i].getLigID() {
				break
			}

			thisComp := buffer.Info[i].getLigComp()
			if thisComp == 0 {
				break
			}

			newLigComp := componentsSoFar - lastNumComponents +
				min8(thisComp, lastNumComponents)
			buffer.Info[i].setLigPropsForMark(ligID, newLigComp)
		}
	}
}

func (c *otApplyContext) recurse(subLookupIndex uint16) bool {
	if c.nestingLevelLeft == 0 || c.recurseFunc == nil || c.buffer.maxOps <= 0 {
		if c.buffer.maxOps <= 0 {
			c.buffer.maxOps--
			return false
		}
		c.buffer.maxOps--
	}

	c.nestingLevelLeft--
	ret := c.recurseFunc(c, subLookupIndex)
	c.nestingLevelLeft++
	return ret
}

func (c *otApplyContext) matchBacktrack(backtrack []uint16, matchFunc matcherFunc) (_ bool, matchStart int) {
	if len(backtrack) == 0 {
		return true, c.buffer.backtrackLen()
	}

	skippyIter := &c.iterContext
	skippyIter.resetBack(c.buffer.backtrackLen())
	skippyIter.setMatchFunc(matchFunc, backtrack)

	for i := 0; i < len(backtrack); i++ {
		if ok, unsafeFrom := skippyIter.prev(); !ok {
			return false, unsafeFrom
		}
	}

	return true, skippyIter.idx
}

func (c *otApplyContext) matchLookahead(lookahead []uint16, matchFunc matcherFunc, startIndex int) (_ bool, endIndex int) {
	if len(lookahead) == 0 {
		return true, startIndex
	}

	skippyIter := &c.iterContext
	skippyIter.reset(startIndex - 1)
	skippyIter.setMatchFunc(matchFunc, lookahead)

	for i := 0; i < len(lookahead); i++ {
		if ok, unsafeTo := skippyIter.next(); !ok {
			return false, unsafeTo
		}
	}

	return true, skippyIter.idx + 1
}

// `count` and `matchPositions` include the first glyph
// `lookupRecord` is in design order
func (c *otApplyContext) applyLookup(count int,
	lookupRecord []tables.SequenceLookupRecord, matchLength int,
) {
	buffer := c.buffer
	var end int

	/* All positions are distance from beginning of *output* buffer.
	* Adjust. */
	{
		bl := buffer.backtrackLen()
		end = bl + matchLength - buffer.idx

		delta := bl - buffer.idx
		/* Convert positions to new indexing. */
		for j := 0; j < count; j++ {
			c.matchPositions[j] += delta
		}
	}

	for _, lk := range lookupRecord {
		idx := int(lk.SequenceIndex)
		if idx >= count { // invalid, ignored
			continue
		}

		origLen := buffer.backtrackLen() + buffer.lookaheadLen()

		// This can happen if earlier recursed lookups deleted many entries.
		if c.matchPositions[idx] >= origLen {
			continue
		}

		buffer.moveTo(c.matchPositions[idx])

		if buffer.maxOps <= 0 {
			break
		}

		if debugMode {
			fmt.Printf("\t\tAPPLY nested lookup %d\n", lk.LookupListIndex)
		}

		if !c.recurse(lk.LookupListIndex) {
			continue
		}

		newLen := buffer.backtrackLen() + buffer.lookaheadLen()
		delta := newLen - origLen

		if delta == 0 {
			continue
		}

		// Recursed lookup changed buffer len. Adjust.
		//
		// TODO:
		//
		// Right now, if buffer length increased by n, we assume n new glyphs
		// were added right after the current position, and if buffer length
		// was decreased by n, we assume n match positions after the current
		// one where removed.  The former (buffer length increased) case is
		// fine, but the decrease case can be improved in at least two ways,
		// both of which are significant:
		//
		//   - If recursed-to lookup is MultipleSubst and buffer length
		//     decreased, then it's current match position that was deleted,
		//     NOT the one after it.
		//
		//   - If buffer length was decreased by n, it does not necessarily
		//     mean that n match positions where removed, as there recursed-to
		//     lookup might had a different LookupFlag.  Here's a constructed
		//     case of that:
		//     https://github.com/harfbuzz/harfbuzz/discussions/3538
		//
		// It should be possible to construct tests for both of these cases.
		//
		end += delta
		if end < int(c.matchPositions[idx]) {
			// End might end up being smaller than match_positions[idx] if the recursed
			// lookup ended up removing many items.
			// Just never rewind end beyond start of current position, since that is
			// not possible in the recursed lookup.  Also adjust delta as such.
			//
			// https://bugs.chromium.org/p/chromium/issues/detail?id=659496
			// https://github.com/harfbuzz/harfbuzz/issues/1611
			//
			delta += c.matchPositions[idx] - end
			end = c.matchPositions[idx]
		}

		next := idx + 1 // next now is the position after the recursed lookup.

		if delta > 0 {
			if delta+count > maxContextLength {
				break
			}
			ensureLen(&c.matchPositions, delta+count)
		} else {
			/* NOTE: delta is negative. */
			delta = max(delta, int(next)-int(count))
			next -= delta
		}

		/* Shift! */
		copy(c.matchPositions[next+delta:], c.matchPositions[next:count])
		next += delta
		count += delta

		/* Fill in new entries. */
		for j := idx + 1; j < next; j++ {
			c.matchPositions[j] = c.matchPositions[j-1] + 1
		}

		/* And fixup the rest. */
		for ; next < count; next++ {
			c.matchPositions[next] += delta
		}

	}

	buffer.moveTo(end)
}

func (c *otApplyContext) applyLookupContext1(data tables.SequenceContextFormat1, index int) bool {
	if index >= len(data.SeqRuleSet) { // index is not sanitized in tt.Parse
		return false
	}
	ruleSet := data.SeqRuleSet[index]
	return c.applyRuleSet(ruleSet, matchGlyph)
}

func (c *otApplyContext) applyLookupContext2(data tables.SequenceContextFormat2, _ int, glyphID GID) bool {
	class, _ := data.ClassDef.Class(gID(glyphID))
	var ruleSet tables.SequenceRuleSet
	if int(class) < len(data.ClassSeqRuleSet) {
		ruleSet = data.ClassSeqRuleSet[class]
	}
	return c.applyRuleSet(ruleSet, matchClass(data.ClassDef))
}

// return a slice containing [start, start+1, ..., end-1],
// using `indices` as an internal buffer to avoid allocations
// these indices are used to refer to coverage
func get1N(indices *[]uint16, start, end int) []uint16 {
	if end > cap(*indices) {
		*indices = make([]uint16, end)
		for i := range *indices {
			(*indices)[i] = uint16(i)
		}
	}
	return (*indices)[start:end]
}

func (c *otApplyContext) applyLookupContext3(data tables.SequenceContextFormat3, _ int) bool {
	covIndices := get1N(&c.indices, 1, len(data.Coverages))
	return c.contextApplyLookup(covIndices, data.SeqLookupRecords, matchCoverage(data.Coverages))
}

func (c *otApplyContext) applyLookupChainedContext1(data tables.ChainedSequenceContextFormat1, index int) bool {
	if index >= len(data.ChainedSeqRuleSet) { // index is not sanitized in tt.Parse
		return false
	}
	ruleSet := data.ChainedSeqRuleSet[index]
	return c.applyChainRuleSet(ruleSet, [3]matcherFunc{matchGlyph, matchGlyph, matchGlyph})
}

func (c *otApplyContext) applyLookupChainedContext2(data tables.ChainedSequenceContextFormat2, _ int, glyphID GID) bool {
	class, _ := data.InputClassDef.Class(gID(glyphID))
	var ruleSet tables.ChainedClassSequenceRuleSet
	if int(class) < len(data.ChainedClassSeqRuleSet) {
		ruleSet = data.ChainedClassSeqRuleSet[class]
	}
	return c.applyChainRuleSet(ruleSet, [3]matcherFunc{
		matchClass(data.BacktrackClassDef), matchClass(data.InputClassDef), matchClass(data.LookaheadClassDef),
	})
}

func (c *otApplyContext) applyLookupChainedContext3(data tables.ChainedSequenceContextFormat3, _ int) bool {
	lB, lI, lL := len(data.BacktrackCoverages), len(data.InputCoverages), len(data.LookaheadCoverages)
	return c.chainContextApplyLookup(get1N(&c.indices, 0, lB), get1N(&c.indices, 1, lI), get1N(&c.indices, 0, lL),
		data.SeqLookupRecords, [3]matcherFunc{
			matchCoverage(data.BacktrackCoverages), matchCoverage(data.InputCoverages), matchCoverage(data.LookaheadCoverages),
		})
}

// grows the slice if required; and copy the current contents
func ensureLen(slice *[]int, L int) {
	if L > len(*slice) {
		if L <= cap(*slice) {
			*slice = (*slice)[:L]
		} else {
			newSlice := make([]int, L)
			copy(newSlice, *slice)
			*slice = newSlice
		}
	}
}
