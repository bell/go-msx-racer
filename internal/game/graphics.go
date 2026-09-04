package game

import "go-msx-racer/assets"

// This file ports the boot-time VRAM setup routines used by the attract
// sequence: clearing/initializing the first 16 background characters,
// loading the text font, and drawing the Konami splash logo. Each loads
// pre-decoded bytes from the assets package (see its doc comment) rather
// than reimplementing the RLE decompressor as part of the running game --
// the original code's "CALL $45E0 to decompress into VRAM" becomes a
// direct WriteVRAM of the already-decoded result.

// initFirstSixteenChars is ported from $489A-$48BA: blanks background
// pattern characters 0-15 (all-zero, i.e. invisible) and sets their color
// table entries to a plain ramp (character N's color byte = N, i.e.
// foreground = N>>4, background = N&0xF) across all 3 Graphic 2 pattern
// thirds. A safe default for character slots not otherwise in use yet.
func (c *Console) initFirstSixteenChars() {
	patBase := uint16(c.vdp().PatternTableBase())
	colBase := uint16(c.vdp().ColorTableBase())
	for third := uint16(0); third < 3; third++ {
		c.Sys.FillVRAM(patBase+third*0x800, 0, 128) // $489A-48A3: 16 chars * 8 bytes = 128
	}
	for ch := uint16(0); ch < 16; ch++ {
		val := byte(ch)
		for third := uint16(0); third < 3; third++ {
			c.Sys.FillVRAM(colBase+third*0x800+ch*8, val, 8)
		}
	}
}

// SetupFont is ported from $4773-$4787: runs initFirstSixteenChars, then
// loads the text font (assets.FontPattern) into background pattern
// characters 16 onward, and paints it a flat white-on-black (color byte
// $F0 per row) since the font doesn't have (or need) a per-row color
// table of its own.
func (c *Console) SetupFont() {
	c.initFirstSixteenChars() // $4773: CALL $489A

	patBase := uint16(c.vdp().PatternTableBase())
	colBase := uint16(c.vdp().ColorTableBase())
	const charBase = 16 * 8 // $2080 in the original == pattern base + $80

	for third := uint16(0); third < 3; third++ {
		c.Sys.WriteVRAM(patBase+third*0x800+charBase, assets.FontPattern) // $477C-$4787: CALL $45E0
	}
	for third := uint16(0); third < 3; third++ {
		c.Sys.FillVRAM(colBase+third*0x800+charBase, 0xF0, 0x160) // $4788-$478A(local)/$45CF: constant color fill
	}
}

// konamiLogoHoldCounter is $E00A: counts the remaining scroll steps for
// the Konami logo (see scrollKonamiLogoStep), from 14 down to 0.
func (c *Console) konamiLogoHoldCounter() *byte { return &c.RAM[0x00A] }

// konamiLogoNameAddr/setKonamiLogoNameAddr is $E00E-$E00F: the absolute
// VRAM name-table address the logo's top row is currently drawn at (a
// full address, not an offset -- the original hardcodes $3AAA as the
// starting point, relying on the name table always being at $3800 in
// this ROM, which the confirmed VDP register trace bears out).
func (c *Console) konamiLogoNameAddr() uint16 {
	return uint16(c.RAM[0x00E]) | uint16(c.RAM[0x00F])<<8
}
func (c *Console) setKonamiLogoNameAddr(addr uint16) {
	c.RAM[0x00E] = byte(addr)
	c.RAM[0x00F] = byte(addr >> 8)
}

// ShowKonamiLogo is ported from $48BB-$48DA: one-time setup for the
// "Konami®" boot splash -- sets the 14-step scroll countdown and the
// initial (pre-scroll) name-table position, decompresses the logo pattern,
// and fills its color -- then performs the first scroll step by falling
// straight into scrollKonamiLogoStep, exactly as the original does (there
// is no RET between $48DA and $48DD). See docs/ANALYSIS.md's dedicated
// section on this asset for the full trace and visual confirmation.
func (c *Console) ShowKonamiLogo() {
	*c.konamiLogoHoldCounter() = 14 // $48BB-$48BD
	c.setKonamiLogoNameAddr(0x3AAA) // $48C0-$48C3

	patBase := uint16(c.vdp().PatternTableBase())
	colBase := uint16(c.vdp().ColorTableBase())
	const charBase = 0x40 * 8 // $2200 in the original == pattern base + $200

	for third := uint16(0); third < 3; third++ {
		c.Sys.WriteVRAM(patBase+third*0x800+charBase, assets.KonamiLogoPattern) // $48C9-$48CF: CALL $45E0
	}
	for third := uint16(0); third < 3; third++ {
		c.Sys.FillVRAM(colBase+third*0x800+charBase, 0xF0, 0xD8) // $48D2-$48DA: constant color fill, white on black
	}

	c.scrollKonamiLogoStep() // falls through to $48DD in the original
}

// scrollKonamiLogoStep is ported from $48DD-$4900: this is the actual
// scroll animation, one step of it. It moves the logo's name-table
// position up by one row (32 bytes = one 8px row), redraws its 3 rows of
// tiles (3/11/12 wide, reflecting the logotype's varying letter heights --
// see docs/ANALYSIS.md) at the new position, clears the row just vacated
// below it, and decrements the hold counter -- returning true once that
// counter reaches zero, meaning the scroll is complete.
//
// Confirmed as a real per-frame scroll (not a one-shot draw, which is
// what an earlier version of this port assumed): $48DD has no RET before
// falling into the redraw code, so whoever CALLs $48DD -- stateAttractSequence's
// phase-1 branch, gated to every other frame -- runs this entire sequence
// each time, and the DEC (HL) on the counter at the end is exactly what
// that caller's subsequent "RET NZ" tests. 14 total steps (1 from
// ShowKonamiLogo's initial fall-through + 13 more from phase 1) move the
// logo from near the bottom of the screen up to roughly its vertical
// center, which is the "scrolls into the middle" effect this was missing.
func (c *Console) scrollKonamiLogoStep() (done bool) {
	addr := c.konamiLogoNameAddr() - 32 // $48DD-$48E4
	c.setKonamiLogoNameAddr(addr)

	// $48E7-$48F5: three calls to $4901, writing sequential character
	// codes (starting at $40) across three consecutive name-table rows.
	code := byte(0x40)
	rowAddr := addr
	for _, width := range [3]int{3, 11, 12} {
		for i := 0; i < width; i++ {
			c.Sys.WriteVRAM(rowAddr+uint16(i), []byte{code})
			code++
		}
		rowAddr += 32
	}
	c.Sys.FillVRAM(rowAddr, 0, 12) // $48F8-$48F9: clear the row vacated below

	counter := c.konamiLogoHoldCounter()
	*counter--
	return *counter == 0 // $48FF: DEC (HL) -- the flag "RET NZ" at the phase-1 call site tests
}

// showSoftwareCredit is ported from $40B6-$40BD: once the Konami logo
// finishes scrolling into place, draws a horizontal rule and the word
// "SOFTWARE" just below it -- the classic "KONAMI / -------- / SOFTWARE"
// boot credit screen.
//
// Decoded from the RLE stream at $46FC (reached via the $460B alternate
// entry, then a $80 mid-stream *destination* retarget -- seeing this
// stream is what caught $80's semantics being documented backwards; see
// internal/rle's package comment and docs/ANALYSIS.md's dedicated section):
//
//   - 12 copies of pattern character $5A at name-table address $394A. $5A
//     is the Konami logo's own 27th pattern tile -- the one tile of the 27
//     the wordmark itself doesn't use (see the Konami-logo section) -- and
//     it turns out to be a single solid horizontal line, i.e. this is
//     exactly a horizontal rule, reusing logo pattern data rather than
//     needing its own.
//   - The literal bytes $33,$2F,$26,$34,$37,$21,$32,$25 at name-table
//     address $396C. Run through the confirmed font character mapping
//     (see the font section), these decode to the letters S,O,F,T,W,A,R,E.
//
// The two writes line up (rule spans columns 10-21, "SOFTWARE" spans
// columns 12-19, both centered on column 15-16) directly below where
// ShowKonamiLogo's scroll settles -- confirming this reading rather than
// just asserting it.
func (c *Console) showSoftwareCredit() {
	c.Sys.WriteVRAM(0x394A, []byte{0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A})
	c.Sys.WriteVRAM(0x396C, []byte{0x33, 0x2F, 0x26, 0x34, 0x37, 0x21, 0x32, 0x25}) // "SOFTWARE"
}

// roadTitlePalette is the 4-entry two-tone (foreground, background) color
// table at $4112 (`40 70`/`60 90`/`A0 F0`/`60 90` -- blue/cyan, red/light
// red, yellow/white, red/light red again), indexed by paintRoadTitleColor's
// selector & 0x03.
var roadTitlePalette = [4][2]byte{
	{0x40, 0x70},
	{0x60, 0x90},
	{0xA0, 0xF0},
	{0x60, 0x90},
}

// showRoadTitle is ported from $49A6-$49BC: sets the VDP backdrop color
// to black, clears the name table, redraws the font (both already-known
// routines re-run here rather than skipped as "redundant", since nothing
// suggests they're conditional the second time), decompresses the
// "ROAD..." big-font title pattern into background pattern characters
// $C0-$E0 (masked dest $2600, offset $600 = char 192 * 8), and paints it
// via paintRoadTitleColor with a fixed selector of 1 -- confirmed by
// tracing $49BA's "LD A,01h" straight into the shared $40EF color-fill
// tail (see paintRoadTitleColor's own doc comment).
//
// $49A8's "CALL $46B1" was previously misread as an unclear MSX BIOS
// WRTPSG call and left unported. It's actually WRTVDP: $46B1 is "LD C,$07;
// JP $4038" and $4038 is itself just "JP $0047" -- and $0047 is confirmed
// to be WRTVDP (register in C, value in B) by InitVDPAndSoundEngine's own
// register-init loop, which calls the identical $0047 the same way. With
// $49A6's "LD B,$E0" feeding it, this writes VDP register 7 = $E0: text
// color 14 (unused outside Text mode), backdrop color **0 (black)** --
// switching the backdrop from whatever attract mode had it at (blue, via
// SetPowerOnDefaults/InitVDPAndSoundEngine) to black for the title
// screen. Caught by the discrepancy between this port's rendering and
// real hardware, after fixing color-index-0 transparency exposed the
// backdrop for the first time (see docs/ANALYSIS.md's correction note).
func (c *Console) showRoadTitle() {
	c.Sys.WriteVDPRegister(7, 0xE0) // $49A6-$49AA: LD B,$E0; CALL $46B1 (WRTVDP via $4038/$0047)

	c.clearNameTable() // $49AB: CALL $4584
	c.SetupFont()      // $49AE: CALL $4773

	patBase := uint16(c.vdp().PatternTableBase())
	const charBase = 0x600 // $2600 in the original == pattern base + $600

	for third := uint16(0); third < 3; third++ {
		c.Sys.WriteVRAM(patBase+third*0x800+charBase, assets.BigFontRoadTitlePattern) // $49B1-$49B7: CALL $45E0
	}

	c.paintRoadTitleColor(1) // $49BA-$49BC: LD A,01h; JP $40EF
}

// clearNameTable is ported from $4584-$458E: hides every sprite, then
// blanks the whole name table (all 24 rows x 32 columns) back to
// character 0.
//
// The sprite half is the leading $4467 call: $3B00 is this ROM's sprite
// attribute table base (R5=$76 -> $3B00, see msx.VDP.SpriteAttrTableBase),
// and $D0 in entry 0's Y byte is the TMS9918A's "no more sprites"
// terminator, so that one byte blanks all 32. This is a full screen wipe
// -- background and sprites -- not just a name-table fill.
//
// This is the ROM's general-purpose "wipe the screen" call, not something
// specific to the title screen: both of its confirmed call sites
// (showRoadTitle's $49AB, and stateAttractSequence's phase 0 at $411D) run
// it to clear away whatever the previous screen left behind.
func (c *Console) clearNameTable() {
	c.Sys.FillVRAM(0x3B00, 0xD0, 1) // $4584: CALL $4467 -- Y=$D0 in sprite-attribute entry 0 terminates the sprite list
	nameBase := uint16(c.vdp().NameTableBase())
	c.Sys.FillVRAM(nameBase, 0, 0x300) // $4587-$458E: LD HL,3800h; LD BC,0300h; XOR A; JP FILVRM
}

// paintRoadTitleColor is ported from $40EF-$410D: a shared two-part flat
// color fill for the "ROAD..." title tiles -- the first 22 tiles (176
// bytes / $B0) get one flat foreground/background color, the remaining 11
// tiles (88 bytes / $58) get another, both taken from the same entry of
// roadTitlePalette, selected by `selector & 0x03`. Reached two different
// ways in the original: showRoadTitle's tail-jump with a fixed selector of
// 1 (this function's only caller so far), and -- once phase 4 is ported --
// with the live $E004 countdown value as selector, which is presumably a
// color-cycling reveal effect. See docs/ANALYSIS.md's dedicated section on
// bigfont_pattern_road_4A25.bin for what's still unresolved (namely why
// the fill splits at exactly 22/11 tiles rather than matching any obvious
// letter boundary).
func (c *Console) paintRoadTitleColor(selector byte) {
	pair := roadTitlePalette[selector&0x03]
	colBase := uint16(c.vdp().ColorTableBase())
	for third := uint16(0); third < 3; third++ {
		c.Sys.FillVRAM(colBase+third*0x800+0x0600, pair[0], 0xB0) // $40FA-$4102: first 22 tiles
		c.Sys.FillVRAM(colBase+third*0x800+0x06B0, pair[1], 0x58) // $4103-$410D: remaining 11 tiles
	}
}

// roadFighterRevealBase/roadFighterRevealTable is a contiguous 114-byte
// ROM window ($4AFC-$4B6D) covering everything revealRoadFighterTitle's
// two scans touch across all 25 frames of phase 3's countdown --
// transcribed directly from the ROM (not hand-derived) to avoid a
// transcription error. The two scans start at different offsets within it
// ($4B0C, $4B2F) and, in the backward scan's case, walk toward and past
// its own nominal start into what looked like unrelated/undecoded bytes
// before this was traced -- see revealRoadFighterTitle's doc comment --
// so this is kept as one contiguous array rather than two independent
// per-scan tables.
const roadFighterRevealBase = 0x4AFC

var roadFighterRevealTable = assets.TitleRevealTable // extracted from $4AFC; see assets/assets.go

// attractMenuText is a contiguous 49-byte ROM window ($472A-$475A)
// holding four control-byte-delimited text runs consumed by two
// consecutive calls to the shared text-drawing routine at $45F0 -- see
// drawTextRun's doc comment for the format and drawAttractMenuText's for
// what it spells.
var attractMenuText = [49]byte{
	0x4A, 0x39, 0x1A, 0x2B, 0x2F, 0x2E, 0x21, 0x2D, 0x29, 0x00, 0x11, 0x19, 0x18, 0x15, 0xFE, 0xAB,
	0x39, 0x30, 0x2C, 0x21, 0x39, 0x00, 0x33, 0x25, 0x2C, 0x25, 0x23, 0x34, 0xFE, 0x0D, 0x3A, 0x2C,
	0x25, 0x36, 0x25, 0x2C, 0x00, 0x21, 0xFF, 0x4D, 0x3A, 0x2C, 0x25, 0x36, 0x25, 0x2C, 0x00, 0x22,
	0xFF,
}

// drawTextRun is ported from $45F0-$4606: a small text-drawing
// interpreter, structurally similar to but distinct from internal/rle's
// VRAM-blit format (same $FE/$FF control-byte flavor, different meaning,
// different code) -- confusing the two would repeat the mistake this
// project already made once with $80's semantics, so this gets its own
// name rather than reusing anything from internal/rle.
//
// Reads from `table` starting at `pos`: first a 2-byte little-endian
// absolute VRAM (name-table) address, then character codes written one
// at a time to that address (advancing by one each write) until a
// control byte appears -- $FE reads a fresh 2-byte destination and keeps
// going, $FF stops this call entirely. Returns the position right after
// whatever stopped it, so a second call (as $49C2 does) can continue
// where the first left off -- there's no separate "number of runs"
// prefix; each call simply runs until it hits $FF.
//
// Character codes reuse the font mapping confirmed elsewhere (font tile N
// lives at pattern char 16+N, per SetupFont, so a code of 0x10+N selects
// it): 0x00 is blank/space, 0x10-0x19 are digits, 0x21-0x3A are A-Z, and
// 0x1A -- the first font tile past the 10 digits, one of the "couple of
// icons" this project's font notes mention -- is confirmed by rendering
// the font at full size to be a © glyph.
//
// The off-by-one here (advancing past the 2 address bytes by exactly 2,
// not 3) was gotten wrong on the first attempt at a headless simulation
// and silently ate the first character after every retarget -- caught by
// noticing "PLAY SELECT" and "LEVEL A"/"LEVEL B" rendered as "LAY SELECT"
// /"EVEL A"/"EVEL B" until fixed; see the correction note in
// docs/ANALYSIS.md.
//
// `mask` models the real entry point's own C register, ANDed into every
// character byte right before the VRAM write ($4600: `AND C`) -- found
// while tracing a second, previously-unidentified entry point at $4607
// (`LD C,00h; JR $45F2`), which turned out to be this exact same
// interpreter, just with every character forced to 0 (blank) instead of
// its real value. $45F0 itself (the normal entry every other call site
// here uses) sets C=$FF, an AND that's a no-op -- so pass 0xFF to
// preserve that behavior, or 0x00 to erase whatever a prior 0xFF-masked
// call at the same destinations drew (see stateGetReadyWait's own phase
// 1 for the real use: blinking a "get ready" message on and off).
func (c *Console) drawTextRun(table []byte, pos int, mask byte) int {
	dest := uint16(table[pos]) | uint16(table[pos+1])<<8
	pos += 2
	for {
		b := table[pos]
		pos++
		if b == 0xFF {
			return pos
		}
		if b == 0xFE {
			dest = uint16(table[pos]) | uint16(table[pos+1])<<8
			pos += 2
			continue
		}
		c.Sys.WriteVRAM(dest, []byte{b & mask})
		dest++
	}
}

// drawAttractMenuText is ported from $49C2-$49CA: two calls to $45F0
// (drawTextRun) that -- via the $FE retargets embedded in attractMenuText
// -- draw four lines onto the name table: "©KONAMI 1985" at $394A (the
// same row showSoftwareCredit's rule/"SOFTWARE" occupied, which this
// overwrites), "PLAY SELECT" at $39AB, "LEVEL A" at $3A0D, and "LEVEL B"
// at $3A4D. Called every frame phase 3 runs, immediately before
// revealRoadFighterTitle -- idempotent (always writes the same bytes), so
// running it once per frame instead of once total makes no visible
// difference, matching $49C2's real structure exactly regardless.
//
// This was missing from the initial phase-3 port: revealRoadFighterTitle
// only covered $49CB onward (the reveal scan itself), skipping $49C2's
// own leading two CALLs entirely. Caught by the game actually being run
// and compared against the real attract screen, which is expected to show
// these four lines while "ROAD FIGHTER" reveals itself. Confirmed by
// rendering: "©KONAMI 1985" / "PLAY SELECT" / "LEVEL A" / "LEVEL B".
func (c *Console) drawAttractMenuText() {
	pos := c.drawTextRun(attractMenuText[:], 0, 0xFF)
	c.drawTextRun(attractMenuText[:], pos, 0xFF)
}

// levelSelectCursorTiles is the 2-tile finger-cursor graphic ($475B-
// $475C, pattern tiles 11-12 -- the two right after the © glyph in the
// font, terminated by the same $FF drawTextRun's data uses), confirmed
// via $5046's own "LD DE,$475B; JP $45F8" -- jumping directly into the
// inner write loop shared with drawTextRun/$45F0, skipping its usual
// "read a destination from the table" preamble because the caller
// ($5028, see drawLevelSelectCursor) sets HL itself instead.
var levelSelectCursorTiles = [2]byte{0x1B, 0x1C}

// drawLevelSelectCursor is ported from $5028-$5049: phase 3's blinking
// "finger" cursor next to whichever of "LEVEL A" ($3A0D) / "LEVEL B"
// ($3A4D) is currently selected via levelSelectFlag -- the finger sits 3
// columns before the text (2 tiles + 1 blank column), i.e. at $3A0A for
// LEVEL A or $3A4A for LEVEL B.
//
// Blinks by reusing $E004's own bit 3 as a clock -- the same countdown
// counter phase 3 is already decrementing for the reveal, so the blink
// rate is just whatever bit 3 happens to do as it counts down (roughly
// 8 frames on, 8 off), not a dedicated timer: draws the real tiles at the
// selected position when bit 3 is set, blanks them when clear, and always
// blanks the *other* (unselected) position outright, matching $5028's own
// "LD C,$FF or $00 before each of two draws" structure exactly (C=$00
// forces every write to 0 via the shared loop's "AND C" -- see
// drawTextRun's doc comment -- rather than needing a separate "erase"
// path).
//
// This is reached via $5028, which phase 3's ">=7" branch tail-jumps to
// -- previously assumed, without checking, to be an undifferentiated
// "large, self-play demo driver" not worth porting (see
// docs/ANALYSIS.md). That assumption was wrong: $5028 is small and
// self-contained, and RETurns right after this. Confirmed by tracing the
// exact CALL/RET/JP structure register by register: $5040's "CALL $5046"
// is a real call/return pair, and the second pass falls through into
// $5046-$5049 without a further CALL, so ITS eventual RET (on the data's
// $FF terminator) unwinds all the way back to $5028's own caller --
// execution never falls through into the much larger RAM-clearing code
// physically adjacent to it at $504C, which turns out to be a separate,
// still-untraced routine reached from elsewhere entirely, not from here.
//
// levelSelectFlag is set by stateExtra43C3 ($43C3, see its doc comment)
// in response to real input, so the cursor now genuinely moves and the
// choice it draws is real.
func (c *Console) drawLevelSelectCursor() {
	blinkOn := *c.attractSubCounter()&0x08 != 0 // $5028-$502F: BIT 3,(HL) on $E004
	c.drawLevelSelectCursorAt(blinkOn)
}

// drawLevelSelectCursorAt draws the cursor at its currently-selected
// position with the given visibility, skipping $5028's own blink-timer
// check -- this is the $5032 entry point into the same routine (reached
// via stateExtra43C3's "JP $5032" when the player confirms their
// selection with fire, forcing the cursor fully visible for that
// confirmation instant regardless of the blink phase).
func (c *Console) drawLevelSelectCursorAt(visible bool) {
	const levelACursor = 0x3A0A
	const levelBCursor = 0x3A4A

	selected, other := uint16(levelACursor), uint16(levelBCursor)
	if *c.levelSelectFlag() != 0 {
		selected, other = other, selected
	}

	c.drawCursorTiles(selected, visible)
	c.drawCursorTiles(other, false)
}

// drawCursorTiles writes (or, if !visible, blanks) the 2-tile finger
// cursor graphic at addr.
func (c *Console) drawCursorTiles(addr uint16, visible bool) {
	for _, b := range levelSelectCursorTiles {
		if !visible {
			b = 0
		}
		c.Sys.WriteVRAM(addr, []byte{b})
		addr++
	}
}

// revealRoadFighterTitle is ported from $49CB-$4A24 (the reveal scan
// itself; see drawAttractMenuText for $49C2-$49CA's leading two calls to
// $45F0, run immediately before this every frame): phase 3's per-frame
// "ROAD FIGHTER" title reveal, called once each frame while $E004's
// countdown (v, its value right after $40CF's own DEC) is still >= 7 --
// 25 frames total, v running from $1F down to $07.
//
// Two scans write into the name table each frame, both re-deriving their
// position fresh from v rather than carrying state across frames:
//
//   - "Backward" scan: row start $3840+(0x20-v) (drifts right, one column
//     per frame), reading from $4B0C toward LOWER addresses. This is
//     "ROAD" -- and more: despite $4B0C being the literal address the
//     original's own "LD DE,$4B0C" loads, tracing showed later frames'
//     backward walk runs straight past it into bytes this project's
//     disassembler had flagged as undecoded/unrelated ($4AFC-$4B0B) --
//     it's simply more of the same table, not a boundary.
//   - "Forward" scan: row start $38C0+v (drifts left, one column per
//     frame), reading from $4B2F toward HIGHER addresses. This is
//     "FIGHTER".
//
// Each scan does 3 outer passes (32 name-table bytes apart -- three
// 8px-tall rows, matching a 16px-tall run of these 16x16 characters plus
// one spare row) and, within each pass, an inner loop that runs
// (0x20-v) times, so it grows by one iteration per frame: read one table
// byte, and unless it's the $FF sentinel (which halts that pass's writes
// for the rest of this frame without moving anything), write it to the
// current name-table position, then step both the name-table position and
// the table read position by one, in the scan's own direction.
//
// Confirmed correct by a register-accurate simulation against the real
// ROM bytes across the full 25-frame window, rendering unambiguously as
// "ROAD FIGHTER" -- see docs/ANALYSIS.md's dedicated section on
// bigfont_pattern_road_4A25.bin for the full trace.
func (c *Console) revealRoadFighterTitle(v byte) {
	scan := func(rowStart uint16, deStart int, width int, dir int) {
		hl := int(rowStart)
		de := deStart
		inner := int(byte(0x20 - v)) // $49F1-$49F8
		for outer := 0; outer < 3; outer++ {
			rowHL, rowDE := hl, de
			for i := 0; i < inner; i++ {
				b := roadFighterRevealTable[rowDE-roadFighterRevealBase]
				if b == 0xFF { // $49F9-$49FB: sentinel -- no write, no movement
					continue
				}
				c.Sys.WriteVRAM(uint16(rowHL)&0x3FFF, []byte{b}) // $49FD-$4A06: SETWRT+OUT
				rowHL += dir
				rowDE += dir
			}
			hl += 0x20  // $4A19-$4A1B: next row down
			de += width // $4A1E-$4A1F: next table segment
		}
	}
	scan(0x3840+uint16(byte(0x20-v)), 0x4B0C, 0x11, -1) // $49CB-$49DC
	scan(0x38C0+uint16(v), 0x4B2F, 0x15, +1)            // $49DF-$49EC (falls through into the shared scan body)
}

// vdp is a small accessor for the concrete *msx.VDP behind c.Sys, used by
// the functions above to read table-base addresses. Graphics setup can
// only run against the real VDP backend (cmd/play), not an arbitrary
// msx.System -- there's nothing meaningful to read table addresses from
// otherwise.
func (c *Console) vdp() interface {
	NameTableBase() int
	ColorTableBase() int
	PatternTableBase() int
} {
	v, ok := c.Sys.(interface {
		NameTableBase() int
		ColorTableBase() int
		PatternTableBase() int
	})
	if !ok {
		panic("game: Console.Sys does not implement VDP table-base accessors (need *msx.VDP or equivalent)")
	}
	return v
}

// placeInAllThirds writes `data` at base+offset, base+offset+$800, and
// base+offset+$1000 -- the same "identical copy in all 3 Graphic 2
// pattern/color-table thirds" tripling every confirmed `CALL $45E0`
// placement in this file performs (see SetupFont/ShowKonamiLogo/
// showRoadTitle above, each of which is a single literal CALL in the
// listing but a 3-iteration loop here).
func (c *Console) placeInAllThirds(base, offset uint16, data []byte) {
	for third := uint16(0); third < 3; third++ {
		c.Sys.WriteVRAM(base+third*0x800+offset, data)
	}
}

// fillVRAMTriplicated is ported from $45CF-$45DF: FillVRAM's own
// "triple across the 3 Graphic 2 thirds" wrapper (CALL $0056/FILVRM,
// HL+=$800, repeat 3 times) -- the color-table equivalent of
// placeInAllThirds above.
func (c *Console) fillVRAMTriplicated(addr uint16, value byte, count uint16) {
	for third := uint16(0); third < 3; third++ {
		c.Sys.FillVRAM(addr+third*0x800, value, count)
	}
}

// mirrorByte is the Z80 bit-reversal loop at $466E (RR C / RLA, 8 times):
// reverses a byte's bit order (bit 0 <-> bit 7, etc.) -- for an 8x8 tile
// pattern row, this is exactly a horizontal pixel-order flip. Traced by
// hand-simulating the RR/RLA sequence against a sample byte and confirming
// it reproduces a textbook bit reversal, not assumed from the mnemonics
// alone.
func mirrorByte(b byte) byte {
	var out byte
	for i := 0; i < 8; i++ {
		out = (out << 1) | (b & 1)
		b >>= 1
	}
	return out
}

// mirrorPatternRows is ported from $4652 (with its $4679/$466E/$004A
// callees): reads `count` consecutive 8-byte tile patterns starting at
// `base+srcOffset`, bit-reverses each byte, and writes the result
// starting at `base+dstOffset` -- across all 3 pattern-table thirds (the
// routine's own outer "LD B,$03 ... DJNZ" loop). Used to generate a gauge
// bar's flipped-orientation pattern tiles at runtime rather than storing
// a second compressed copy in ROM. The corresponding *color* table
// entries, by contrast, genuinely are separate ROM-sourced assets in
// every confirmed use below -- color bytes don't mirror via simple bit
// reversal the way an 8x8 pattern row's pixels do.
func (c *Console) mirrorPatternRows(base, srcOffset, dstOffset uint16, count int) {
	for third := uint16(0); third < 3; third++ {
		src := base + third*0x800 + srcOffset
		dst := base + third*0x800 + dstOffset
		for i := 0; i < count*8; i++ {
			b := c.Sys.ReadVRAM(src + uint16(i))
			c.Sys.WriteVRAM(dst+uint16(i), []byte{mirrorByte(b)})
		}
	}
}

// mirrorSprite16 is ported from $4636-$4651 (sharing mirrorPatternRows'
// own $4679/$466E/$004A callees, but a distinct routine -- not a
// duplicate): horizontally flips `count` consecutive 32-byte 16x16
// hardware-sprite patterns (4 8x8 quadrants each, in this project's
// confirmed TL/BL/TR/BR order -- see internal/msxgfx's own doc comment)
// from src to dst, one full 16x16 sprite per iteration (both src and dst
// advance 32 bytes per count). Not ported by reasoning through the raw
// PUSH/POP-DE-and-BIT-4 stack trick by hand -- see this project's own
// standard for that class of intricate register/stack shuffling -- but
// by direct-call ROM tracing: filled the real call site's own source
// region ($1880, 192 bytes) with distinct per-byte markers, called $4636
// with its own real register values (HL=$1880, DE=$1950, C=6), and
// checked every output byte. Result: dst[i] = mirrorByte(src[i^16]) for
// every i in a 32-byte block, exactly -- i.e. the left half (bytes 0-15,
// TL+BL) and right half (bytes 16-31, TR+BR) swap position *and* each
// byte's bits reverse (a per-row horizontal pixel flip), producing a
// correct whole-sprite horizontal mirror. The real call site
// ($6934-$693E) uses this to generate the 3 missing "steering right"
// vehicle-sprite groups (selectors 5-7 of carSpritePatternTable, patterns
// 40-63) from the 3 "steering left" groups (selectors 1-3, patterns
// 16-39) already present in assets.SpritesVehiclesPattern -- see that
// asset's own doc comment. Before this was ported, those 3 groups were
// whatever VRAM already held (zero, i.e. a fully blank/invisible sprite),
// which is why the post-crash "spin" animation (case 7 of
// updateSteeringAndCarState, cycling carSpritePatternTable[0..7]) made
// the player's car flicker invisible on roughly 3 of every 8 frames.
func (c *Console) mirrorSprite16(src, dst uint16, count int) {
	for n := 0; n < count; n++ {
		s := src + uint16(n)*32
		d := dst + uint16(n)*32
		var buf [32]byte
		for i := uint16(0); i < 32; i++ {
			buf[i] = c.Sys.ReadVRAM(s + i)
		}
		for i := uint16(0); i < 32; i++ {
			c.Sys.WriteVRAM(d+(i^16), []byte{mirrorByte(buf[i])})
		}
	}
}

// initHUDAndTrackGraphics is ported from $68AA-$6A05, one of
// stateGetReadyWait's phase-3 TODOs (see state.go) -- reached once its
// "get ready" message wait ($4429) finishes, just before the race starts.
//
// Draws the SPEED/FUEL HUD panel, all three gauge-bar tile sets (each
// with a horizontally-mirrored companion: mirrorPatternRows generates the
// mirrored *pattern* at runtime, while the mirrored *color* comes from
// its own separate ROM asset -- except gauge bar 2, whose "mirrored"
// color is simply its normal color asset placed a second time, unchanged;
// ported faithfully, not smoothed into a fourth mirrored-color asset that
// doesn't actually exist in the ROM), plus a page of vehicle sprites and
// a second page of crash/debris/bonus-score sprites (320 + 800 bytes,
// decompressed via $460B's pointer-indirection -- see
// assets.SpritesVehiclesPattern's doc comment for how that indirection
// was decoded, and assets.SpritesCrashDebrisBonusPattern's for how the
// second page's actual content -- not more vehicles, despite looking
// like a natural continuation of the first sprite page -- was identified).
//
// Then dispatches on $E043 (the lap/stage counter -- see
// stateResultsPhase2's doc comment in state.go) for per-stage track
// scenery: stages 4 and 5 draw the diagonal UI border plus tree/bush and
// tree/road scenery (drawTrackBorderAndTreeScenery, fully traced); stage
// 2 additionally calls the per-stage terrain loader (loadStageTerrainScenery,
// $6A30, now traced) before placing the border directly; stages 0, 1, 3,
// and >=6 rely entirely on that same $6A30 loader, though the "stage == 0
// or >= 6" case's post-$6A30 nibble fixup (fixupGaugeBarBackgroundNibble,
// $6961-$6988) is separately confirmed and ported.
//
// $6A30 itself was found missing (left as a bare TODO with no call site
// wired up) after a user-supplied real-hardware screenshot showed normal
// roadside buildings/trees where this port showed fragments of the HUD
// panel/gauge-bar art instead -- see loadStageTerrainScenery's own doc
// comment for the trace and docs/ANALYSIS.md for the comparison.
// $6A06, called unconditionally partway through, is ported too --
// applyStageGaugeBar3ColorOverride, see its own doc comment.
func (c *Console) initHUDAndTrackGraphics() {
	patBase := uint16(c.vdp().PatternTableBase())
	colBase := uint16(c.vdp().ColorTableBase())

	c.placeInAllThirds(patBase, 0x200, assets.HudSpeedFuelPanelPattern) // $68AA-$68B2
	c.placeInAllThirds(colBase, 0x200, assets.HudSpeedFuelPanelColor)   // $68B3-$68BB

	c.placeInAllThirds(patBase, 0x680, assets.HudGaugeBars1Pattern)       // $68BC-$68C4
	c.placeInAllThirds(colBase, 0x680, assets.HudGaugeBars1Color)         // $68C5-$68CD
	c.mirrorPatternRows(patBase, 0x688, 0x6A8, 4)                         // $68CE-$68D8: CALL $4652
	c.placeInAllThirds(colBase, 0x6A8, assets.HudGaugeBars1MirroredColor) // $68D9-$68E1

	c.placeInAllThirds(patBase, 0x6C8, assets.HudGaugeBars2Pattern) // $68E2-$68EA
	c.placeInAllThirds(colBase, 0x6C8, assets.HudGaugeBars2Color)   // $68EB-$68F3
	c.mirrorPatternRows(patBase, 0x6C8, 0x708, 8)                   // $68F4-$68FE: CALL $4652
	c.placeInAllThirds(colBase, 0x708, assets.HudGaugeBars2Color)   // $68FF-$6907: same color asset, reused

	c.placeInAllThirds(patBase, 0x748, assets.HudGaugeBars3Pattern)       // $6908-$6910
	c.placeInAllThirds(colBase, 0x748, assets.HudGaugeBars3Color)         // $6911-$6919
	c.mirrorPatternRows(patBase, 0x760, 0x798, 6)                         // $691A-$6924: CALL $4652
	c.placeInAllThirds(colBase, 0x798, assets.HudGaugeBars3MirroredColor) // $6925-$692D

	c.Sys.WriteVRAM(0x1800, assets.SpritesVehiclesPattern)         // $692E-$6933: CALL $460B (pointer-indirected; see the asset's own doc comment)
	c.mirrorSprite16(0x1880, 0x1940, 6)                            // $6934-$693E: CALL $4636 -- see its own doc comment (dst is the true 32-byte-aligned base; the real $6934 call site loads DE=$1950, i.e. base+16, an addressing detail of its own PUSH/POP/BIT-4 stack trick that this direct byte-manipulation port doesn't need to replicate)
	c.Sys.WriteVRAM(0x1A00, assets.SpritesCrashDebrisBonusPattern) // $693F-$6944: CALL $460B
	c.applyStageGaugeBar3ColorOverride()                           // $6945: CALL $6A06 -- see its own doc comment

	switch stage := c.RAM[0x043]; {
	case stage == 4, stage == 5:
		c.drawTrackBorderAndTreeScenery() // $6995-$6A05

	case stage == 2:
		c.loadStageTerrainScenery()                                        // $6989: CALL $6A30
		c.placeInAllThirds(patBase, 0x588, assets.UIDiagonalBorderPattern) // $698C-$6992: CALL $45E0, direct to the "mirrored" border slot

	case stage == 1, stage == 3:
		c.loadStageTerrainScenery() // $6A30, reached via a tail-jump in
		// the original, so (unlike the default case below) no nibble
		// fixup runs afterward.

	default: // stage == 0 or stage >= 6
		c.loadStageTerrainScenery()       // $6A30
		c.fixupGaugeBarBackgroundNibble() // $6961-$6988
	}
}

// stageTerrainSources is the 6-entry ROM table at $6A51 ({patternSrcAddr,
// colorSrcAddr}, both fields little-endian in the original) loadStageTerrainScenery
// indexes by stage-1. Entries 3-4 (stages 4-5) are confirmed identical to
// each other and to SceneryTreeBushPattern/Color, which is already loaded a
// different way for those stages by drawTrackBorderAndTreeScenery -- see
// assets.StageTerrainPattern1's doc comment for the full trace.
//
// CORRECTED: this was a 5-entry table here, on the assumption that the
// table ended after stage 5. It doesn't -- reading the raw ROM at $6A51
// shows a 6th entry ($67D5 pattern / $6885 color), and only the *7th*
// 4-byte slot decodes as unrelated data ($2000/$00B0). Since the array
// was one entry short, loadStageTerrainScenery's own bounds check made
// stage 6 return without loading any terrain at all. Both of stage 6's
// streams are now extracted and confirmed genuine the same way the rest
// of this table was: $67D5 decodes 200 bytes and its control stream ends
// exactly at $6885 (its own color pointer), which in turn decodes 200
// bytes and ends exactly at $68AA -- initHUDAndTrackGraphics itself, i.e.
// the end of this whole data region, with no gap or slack anywhere in
// the chain.
var stageTerrainSources = [6]struct{ pattern, color []byte }{
	{assets.StageTerrainPattern1, assets.StageTerrainColor1},
	{assets.StageTerrainPattern2, assets.StageTerrainColor2},
	{assets.StageTerrainPattern3, assets.StageTerrainColor3},
	{assets.SceneryTreeBushPattern, assets.SceneryTreeBushColor},
	{assets.SceneryTreeBushPattern, assets.SceneryTreeBushColor},
	{assets.StageTerrainPattern6, assets.StageTerrainColor6},
}

// loadStageTerrainScenery is ported from $6A30-$6A4E: indexes
// stageTerrainSources by (E043-1) and decompresses the selected
// pattern/color pair to VRAM $2400 (pattern) and $0400 (color).
//
// CORRECTED: both of those were single writes here, on the stated
// reasoning that the original passes literal destination addresses
// "rather than looping across all 3 Graphic 2 thirds the way
// initHUDAndTrackGraphics's other loads are", so this only ever touched
// third 0. That was wrong, and the distinction it drew was fictional:
// the *literal addresses* are real ($6A40: LD HL,2400h; $6A4B: LD
// HL,0400h), but both are handed to `CALL $45E0` -- and $45E0 is itself
// the tripling routine ($45E0: LD B,03h ... CALL L4611; LD DE,0800h; ADD
// HL,DE; DJNZ), i.e. it decodes the same stream into HL, HL+$800 and
// HL+$1000. Every other confirmed `CALL $45E0` site in this file already
// models that correctly (see placeInAllThirds' own doc comment, which
// states the rule outright); this one call site simply didn't follow it.
//
// The visible effect was that each stage's own terrain art only ever
// reached third 0, i.e. name-table rows 0-7, leaving the roadside for
// the other two thirds -- most of the screen during real driving -- to
// show whatever unrelated art already occupied that character-code
// range. Reported from real gameplay as the roadside graphics looking
// the same regardless of which level you start, and settled by a
// real-hardware screenshot of stage 2's start showing its own beach/sea
// and palm-tree scenery running the full height of the screen.
//
// Stage values outside 1-6 (the DEC A wraparound the original's "stage
// == 0" case can reach) would read table bytes before/after $6A51 on
// real hardware; this port has no ROM bytes to read at runtime for that
// case (see assets' own doc comment on why: pre-decoded assets only), so
// out-of-range stage values are a documented no-op deviation rather than
// a guess at what garbage data might otherwise land in VRAM.
func (c *Console) loadStageTerrainScenery() {
	idx := int(c.RAM[0x043]) - 1
	if idx < 0 || idx >= len(stageTerrainSources) {
		return
	}
	src := stageTerrainSources[idx]
	c.placeInAllThirds(0x2400, 0, src.pattern) // $6A3C-$6A43: CALL $45E0 -- tripled by $45E0 itself
	c.placeInAllThirds(0x0400, 0, src.color)   // $6A47-$6A4E: JP $45E0 -- likewise
}

// drawTrackBorderAndTreeScenery is ported from $6995-$6A05: draws the UI
// diagonal border and tree/bush + tree/road scenery tiles, reused for
// stages 4 and 5 ($E043) by initHUDAndTrackGraphics. Stage 4 additionally
// re-paints two 8-tile ($38-byte) color-table runs to a flat $66 -- not
// confirmed why only stage 4 needs this and not 5.
func (c *Console) drawTrackBorderAndTreeScenery() {
	patBase := uint16(c.vdp().PatternTableBase())
	colBase := uint16(c.vdp().ColorTableBase())

	c.placeInAllThirds(patBase, 0x400, assets.UIDiagonalBorderPattern) // $6995-$699D
	c.mirrorPatternRows(patBase, 0x400, 0x588, 24)                     // $699E-$69A8: CALL $4652
	c.placeInAllThirds(colBase, 0x588, assets.UIDiagonalBorderColor)   // $69A9-$69B1

	c.placeInAllThirds(patBase, 0x400, assets.SceneryTreeBushPattern) // $69B2-$69BA (overwrites the border pattern at the same slot)
	c.mirrorPatternRows(patBase, 0x400, 0x468, 13)                    // $69BB-$69C5: CALL $4652
	c.placeInAllThirds(colBase, 0x400, assets.SceneryTreeBushColor)   // $69C6-$69CE
	c.placeInAllThirds(colBase, 0x468, assets.SceneryTreeBushColor)   // $69CF-$69D7: same asset, placed again

	c.placeInAllThirds(patBase, 0x4D0, assets.SceneryTreeRoadPattern) // $69D8-$69E0
	c.placeInAllThirds(colBase, 0x4D0, assets.SceneryTreeRoadColor)   // $69E1-$69E9

	if c.RAM[0x043] == 4 { // $69EA-$69EF: LD A,(E043h); CP 04h; RET NZ
		c.fillVRAMTriplicated(colBase+0x430, 0x66, 0x38) // $69F0-$69F8
		c.fillVRAMTriplicated(colBase+0x498, 0x66, 0x38) // $69FB-$6A03
	}
}

// fixupGaugeBarBackgroundNibble is ported from $6961-$6988: scans 256
// consecutive color-table bytes starting at colBase+$6C8 (the gauge bar 2
// area) and, for any whose low nibble is $F, replaces it with $7 -- then
// writes the (possibly adjusted) byte back via fillVRAMTriplicated
// (single byte, but across all 3 thirds). Reached only when $E043 (stage)
// is 0 or >=6, after loadStageTerrainScenery's own $6A30 per-stage
// terrain call -- see initHUDAndTrackGraphics.
func (c *Console) fixupGaugeBarBackgroundNibble() {
	colBase := uint16(c.vdp().ColorTableBase())
	addr := colBase + 0x6C8
	for i := uint16(0); i < 0x100; i++ {
		b := c.Sys.ReadVRAM(addr + i)
		if b&0x0F == 0x0F {
			b = (b & 0xF0) | 0x07
		}
		c.fillVRAMTriplicated(addr+i, b, 1)
	}
}

// stageGaugeBar3ColorOverride is the 7-byte ROM table at $6A69, indexed
// by the raw stage value (1-6; entry 0 is unused padding) -- confirmed
// via $6A0C's own "$403B: HL+=A" 1-byte-stride addressing, and via
// direct-call ROM tracing showing every output byte in the confirmed
// range ending in exactly this table's own values ORed with $0F.
var stageGaugeBar3ColorOverride = [7]byte{0x00, 0x20, 0xB0, 0x00, 0x60, 0x90, 0xF0}

// applyStageGaugeBar3ColorOverride is ported from $6A06-$6A2F
// (initHUDAndTrackGraphics' own final TODO, at its own $6945 call site).
// ORs a per-stage high-nibble byte (stageGaugeBar3ColorOverride[stage])
// onto the low nibble of each of 128 consecutive color-table bytes
// starting at colBase+$748 -- confirmed by address, not assumed: $6A13's
// own literal "LD HL,$0748" only makes sense as a VRAM address against
// colBase=$0 (this ROM's own confirmed color-table base, see
// docs/ANALYSIS.md's "VRAM memory map"), and $748 is exactly where
// HudGaugeBars3Color was placed a few lines above in this same function.
// The 128-byte span covers both that 88-byte asset and 40 bytes into the
// following HudGaugeBars3MirroredColor region (placed at colBase+$798,
// i.e. offset 80 into this same span) -- ported as one continuous sweep
// exactly matching the real ROM's own address range, not split along
// this port's own asset-extraction boundaries. Rewrites all 3 Graphic 2
// thirds via fillVRAMTriplicated, the same way fixupGaugeBarBackgroundNibble
// does for gauge bar 2's own color fixup, but unconditionally ORs in the
// stage's own override bits rather than conditionally swapping a
// sentinel nibble. Verified via direct-call ROM tracing for all 6 real
// stage values, loading the real HudGaugeBars3Color/MirroredColor assets
// exactly as production init does -- exact match.
func (c *Console) applyStageGaugeBar3ColorOverride() {
	colBase := uint16(c.vdp().ColorTableBase())
	addr := colBase + 0x748
	override := stageGaugeBar3ColorOverride[c.RAM[0x043]]
	for i := uint16(0); i < 0x80; i++ {
		b := (c.Sys.ReadVRAM(addr+i) & 0x0F) | override
		c.fillVRAMTriplicated(addr+i, b, 1)
	}
}

// hiscoreScoreText is the ROM data window $470D-$471D, one drawTextRun
// stream (a single retarget partway through, unlike drawAttractMenuText's
// four-string table): "HISCORE" at $3817, then "SCORE" at $3877.
var hiscoreScoreText = [18]byte{
	0x17, 0x38, 0x28, 0x29, 0x33, 0x23, 0x2F, 0x32, 0x25, 0xFE,
	0x77, 0x38, 0x33, 0x23, 0x2F, 0x32, 0x25, 0xFF,
}

// hudBorderStripTile1/2 are the two 4-byte literal name-table tile-code
// runs at $452D-$4530 and $4531-$4534, used by drawHUDBorderDecoration.
var hudBorderStripTile1 = [4]byte{0x70, 0x01, 0x60, 0x71}
var hudBorderStripTile2 = [4]byte{0x70, 0x73, 0x01, 0x71}

// drawPackedBCDDigits is ported from $4535-$4560: a packed-BCD digit
// renderer with leading-zero blanking. Reads `count` bytes starting at
// `src`, walking *backward* (each byte holds 2 BCD digits, high nibble
// first), and writes `count*2` digit tiles starting at `dest`, walking
// forward. A digit tile is font character $10+nibble (digits 0-9 live at
// pattern characters $10-$19, confirmed by SetupFont); a leading zero
// (any zero digit before the first nonzero one) is blanked to character 0
// (the always-invisible character SetupFont reserves) instead -- except
// the very last digit, which is always shown even if it's a zero using
// the same nibble-swap idiom `$4548`/`$4556` use to fold a $10-offset
// zero back down to plain zero via the AND-mask.
//
// Confirmed against $44D0's own use of it (src=$E03E, count=3): $E03C-
// $E03E is a 3-byte packed-BCD counter (6 decimal digits) capped at
// 999999 elsewhere ($449C-$44A0's "LD BC,$9999" overflow clamp) --
// consistent with a score/high-score display, not yet confirmed against
// any other reader.
func (c *Console) drawPackedBCDDigits(dest uint16, src uint16, count byte) {
	seenNonzero := false
	for i := byte(0); i < count; i++ {
		b := c.RAM[src-0xE000]
		last := i == count-1

		hi := b >> 4
		if hi != 0 {
			seenNonzero = true
		}
		tile := hi + 0x10
		if !seenNonzero && !last {
			tile = 0
		}
		c.Sys.WriteVRAM(dest, []byte{tile})
		dest++

		lo := b & 0x0F
		if lo != 0 {
			seenNonzero = true
		}
		tile = lo + 0x10
		if !seenNonzero && !last {
			tile = 0
		}
		c.Sys.WriteVRAM(dest, []byte{tile})
		dest++

		src--
	}
}

// countdownDigitOffsetTable is the 9-byte ROM table at $4241, indexed
// directly by the current "3,2,1" countdown value ($E004). Counter
// values 8 down to 2 are all reachable here (see drawCountdownDigit's
// own doc comment for why -- a stale earlier comment on this file's own
// caller claimed only 4-2 were reachable; that was wrong and has been
// corrected), so every entry except index 0 (never reached: the counter
// starts at 9, and this code only runs after the first decrement,
// during which it becomes 8) is live.
var countdownDigitOffsetTable = [9]byte{0x00, 0x60, 0x60, 0x40, 0x40, 0x20, 0x20, 0x00, 0x00}

// drawCountdownDigit is ported from $4217-$423E: called every 32nd frame
// from stateGetReadyWait phase 9, right after the countdown counter
// ($E004) is decremented and its beep (if any) triggered -- and only
// when that decrement didn't just trigger triggerRaceStart. The counter
// is seeded at 9 by phase 8's own setGameState(9) call (not 5 by phase
// 7, as an earlier pass at this code assumed without checking -- no code
// anywhere sets it to 5), so the values reaching this function are 8
// down to 2 (1 exits early via triggerRaceStart before this ever runs).
//
// Picks a 2-tile-wide graphic (character codes page/page+1) based on the
// counter's parity and whether it's exactly 2: odd selects $46,
// even-and-not-2 selects $44, and exactly 2 selects $48. Combined with
// countdownDigitOffsetTable's own destination offsets, this doesn't draw
// three digit numerals the way an earlier pass at this guessed (before
// actually tracing it) -- it lights up ONE of 4 name-table rows (1, 2,
// 3, then 4) as the count ticks down, alternating between the $44/$45
// and $46/$47 tile pairs twice at each of rows 1-3 (once per counter
// value) before finally showing $48/$49 once at row 4 for the last
// count before the race starts. That's a row-by-row "starting lights"
// indicator, not digit sprites -- matching a real starting-grid
// countdown light sequence far better than "digit graphics" ever did,
// and matching what a user-supplied real-hardware screenshot showed at
// this exact spot (previously undrawn in this port -- see
// docs/ANALYSIS.md's "flags"/countdown-signal write-up). All the
// character-code pairs involved live inside the same $40-$7F
// HUD-panel/gauge-bar range other roadside decoration reuses (see
// docs/ANALYSIS.md) -- rendering that sheet shows each pair as a
// visually distinct rounded/oval shape, consistent with a lit-vs-unlit
// signal light, though the exact lit/unlit assignment isn't
// independently confirmed beyond the mechanism.
//
// The destination is $3826 + countdownDigitOffsetTable[counter], column
// 6 -- i.e. name-table rows 1-4, columns 6-7. Those cells are the lamps
// of the starting-signal gantry initTrackLayoutData's static layout
// already put in the road buffer: rows 0-5, columns 5-8, with a $42,$42
// cap on row 0, $40/$46/$47/$41 (left post, two lamp tiles, right post)
// on rows 1-4, and a $43,$43 foot on row 5. So this doesn't draw onto
// blank scenery -- it re-lights four rows of an existing structure.
//
// CORRECTED: an earlier version of this comment said the lamp cells
// start out holding $42 "for every countdown-inactive frame before this
// first fires". They don't -- they start out as $46,$47 (one of the two
// pairs this function alternates between), and $42 is only the cap tile
// on the row above. Caught by dumping the name table at get-ready
// phase 7 while working on the level-select cheat; see
// docs/ANALYSIS.md open-issues item 7 for the full geometry.
func (c *Console) drawCountdownDigit() {
	counter := *c.attractSubCounter()

	var page byte
	switch {
	case counter&1 != 0:
		page = 0x46
	case counter == 2:
		page = 0x48
	default:
		page = 0x44
	}

	addr := 0x3826 + uint16(countdownDigitOffsetTable[counter])
	c.Sys.WriteVRAM(addr, []byte{page})
	c.Sys.WriteVRAM(addr+1, []byte{page + 1})
}

// drawHUDBorderDecoration is ported from $44F4-$452A plus its
// fall-through tail at $740B-$740D: a small sequence of solid-color
// fills and repeated-tile copies forming a decorative border strip and a
// handful of icon tiles just right of the HUD panel -- all reusing
// character indices that fall within the HUD panel pattern range
// initHUDAndTrackGraphics already loads ($40-$7F), plus $5A-$5F which
// overlap the Konami logo's own unused 27th-tile range (also already
// loaded by ShowKonamiLogo, and safe to reuse here since the logo isn't
// shown during gameplay). Ported for its confirmed mechanical effect
// (exact addresses, tile codes, and fill counts, traced instruction by
// instruction including a non-obvious H-register carry through the
// 8-row loop at $450E-$4519); what the individual tiles actually depict
// isn't independently confirmed.
//
// $740B turned out to be a two-line tail, not the "separate substantial
// routine" an earlier pass at this code assumed from its address alone:
// `CALL $73AD` (the speed gauge's own entry point, see gameplay.go),
// then `JP $7444` (the FUEL gauge's own entry point) -- i.e. this draws
// an initial reading for *both* gauges right after the border
// decoration, at whatever value initPlayerCarSpritesAndFuel already set
// ($E04F=0 -- unlit -- and $E083=$D0 -- near-full).
//
// CORRECTED: an earlier pass at this port never actually read $740E's
// own raw bytes and instead assumed, from the address alone, that it
// matched the *other* known "$44D9" tail-jump pattern this same file
// uses elsewhere (GameplayTick's own ending) -- shipping
// redrawScoreAndHiscoreDigits() here instead of the real target. Found
// while investigating a user report that the fuel gauge isn't visible
// until shortly after a race starts (only the speed gauge, whose own
// $740B call was translated correctly, showed up immediately): $740E is
// `C3 44 74` -- `JP $7444`, unambiguously drawFuelGaugeBar's own entry,
// not $44D9. The old code happened to look harmless (score digits just
// got redrawn twice: once here, once for real moments later from
// drawScoreDisplay's own end) rather than crashing or looking obviously
// wrong, which is exactly why it went unnoticed until a user compared
// against expected behavior. Confirmed by tracing the real ROM running
// on a real Z80 core: at race start, before any dynamic gauge-redraw
// trigger has fired, VRAM already shows a full-looking fuel bar,
// traceable to exactly this call.
//
// This does NOT redraw or protect the border strip in any way -- worth
// noting because the border strip's own last row ($39F9, row 15) and
// the speed gauge's topmost row overlap whenever speed is high enough
// to fill all 8 gauge rows ($E04F >= $E0). That overlap is confirmed
// present in the ROM's own code on both sides (this routine's addresses
// and the gauge's were each independently traced and verified
// byte-for-byte), so it's a genuine interaction, not a bug introduced
// by this port -- but nothing found so far explains whether/how the
// original refreshes the border strip afterward, so it's flagged here
// rather than silently accepted.
func (c *Console) drawHUDBorderDecoration() {
	c.Sys.FillVRAM(0x38B9, 0x6F, 4)                 // $44F4-$44FC
	c.Sys.WriteVRAM(0x38D9, hudBorderStripTile1[:]) // $44FF-$4507

	addr := uint16(0x38F9) // $450A
	for i := 0; i < 8; i++ {
		c.Sys.WriteVRAM(addr, hudBorderStripTile2[:]) // $450E-$4519, one row per iteration
		addr += 0x20
	}

	c.Sys.FillVRAM(addr, 0x72, 4) // $451B-$4521: addr is now $39F9

	c.drawTextRun(hudBorderTailText[:], 0, 0xFF) // $4524-$452A
	c.drawSpeedGaugeBar()                        // $740B: CALL $73AD
	c.drawFuelGaugeBar()                         // $740E: JP $7444
}

// hudBorderTailText is the ROM data window $476A-$4772: a single
// drawTextRun stream placing 6 tile codes ($5A-$5F) at $3AF8.
var hudBorderTailText = [9]byte{0xF8, 0x3A, 0x5A, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0xFF}

// redrawScoreAndHiscoreDigits is ported from $44D9-$44E8: draws both
// packed-BCD digit groups drawScoreDisplay's own labels sit above --
// hiscore ($E03C-$E03E, 3 bytes) at $3838, and current score ($E03F-$E041,
// 3 bytes -- confirmed distinct from hiscore by clearRaceRAMBlock zeroing
// $E03F onward at the start of every "get ready" sequence while leaving
// $E03C-$E03E alone, exactly matching "current score resets each race,
// hiscore persists") at $3898. A prior pass at drawScoreDisplay only
// ported the first of these two -- easy to miss since they're two
// sequential calls into the same shared digit-drawing tail ($4535) inside
// one Z80 function, not two separately-named routines. This shared tail
// is also the target of two "JP $44D9" tail-jumps elsewhere
// (GameplayTick's own end and drawHUDBorderDecoration's $740E) and of a
// genuine `CALL $44D9` from updateSteeringAndCarState's state 3 ($6B5F) --
// all three now use this helper instead of leaving TODOs.
func (c *Console) redrawScoreAndHiscoreDigits() {
	c.drawPackedBCDDigits(0x3838, 0xE03E, 3) // $44D9-$44DF
	c.drawPackedBCDDigits(0x3898, 0xE041, 3) // $44E2-$44E8
}

// drawScoreDisplay is ported from $44D0-$44E8, one of stateGetReadyWait
// phase 3's TODOs (see state.go): draws drawHUDBorderDecoration's
// decorative tiles, then "HISCORE"/"SCORE" labels, then both digit groups
// (see redrawScoreAndHiscoreDigits).
func (c *Console) drawScoreDisplay() {
	c.drawHUDBorderDecoration()                 // $44D0: CALL $44F4
	c.drawTextRun(hiscoreScoreText[:], 0, 0xFF) // $44D3-$44D6
	c.redrawScoreAndHiscoreDigits()             // $44D9-$44E8
}

// stageLabelText is the ROM data window $471F-$4729: a single
// drawTextRun stream placing "STAGE" (with 3 trailing spaces, blank/code
// $00) at $3969, roughly the middle of the screen.
var stageLabelText = [11]byte{
	0x69, 0x39, 0x33, 0x34, 0x21, 0x27, 0x25, 0x00, 0x00, 0x00, 0xFF,
}

// drawStageNumber is ported from $42ED-$44E9 (via the shared $44ED digit
// entry, itself falling into drawPackedBCDDigits' $4535): draws
// "STAGE   " at $3969, then backs the destination up 2 characters
// ($3969 + 8 chars - 2 = $396F) and overwrites the last 2 (blank)
// characters with the current stage number ($E043, one packed-BCD byte /
// 2 digits) -- "STAGE 01", "STAGE 02", etc. Not yet wired to anything
// that clears the screen first (see stateGetReadyWait phase 3's own doc
// comment on $504C) -- on real hardware the middle of the screen is the
// track/road view by this point, not the still-showing title, but this
// port doesn't draw that yet (GameplayTick's own callees, mostly
// unported), so currently this renders overlapping whatever's still on
// screen from the title/menu.
func (c *Console) drawStageNumber() {
	c.drawTextRun(stageLabelText[:], 0, 0xFF) // $42ED-$42F0
	c.drawPackedBCDDigits(0x396F, 0xE043, 1)  // $42F3-$42F5: DEC L; DEC L; JP $44ED
}

// stageMapRows is the 105-byte ROM block at $76EC-$7754: the stage-map
// cut scene's own 5-column by 21-row picture, one row of 5 tile codes
// per line below.
//
// CORRECTED: this was a single 5-byte value (`stageMapStrip`) that
// drawStageMapScreen wrote to all 21 rows, on the reading that $7467's
// call is a "copy these fixed bytes to each row" repeat. It isn't. That
// call ($4575) preserves only BC across its row loop, and the copier it
// calls ($45A7) advances its source pointer as it goes ($45B5: INC DE)
// -- so each row consumes the *next* 5 bytes and the picture is 21*5 =
// 105 bytes. The old 5-byte constant was simply this block's first row,
// which is why the map rendered as that one row repeated 21 times, as
// reported from real gameplay. The block ends at exactly $7755, which is
// fuelPickupTick's own address -- the same tight-packing boundary that
// confirms the rest of this ROM's data tables.
var stageMapRows = assets.StageMapRows // extracted from $76EC; see assets/assets.go

// stageMapStateReset is the 8-byte ROM literal at $7475-$747C, copied
// into $E04C-$E053 by drawStageMapScreen -- resets several gameplay
// state bytes for the upcoming stage, including $E04F (the "speed" value
// engineSoundPitchStep and the acceleration logic at $6CA1 read) back to
// 0. The other bytes' individual roles aren't confirmed.
var stageMapStateReset = [8]byte{0xA8, 0x00, 0x63, 0x00, 0x00, 0x00, 0x00, 0x0A}

// drawStageMapScreen is ported from $744C-$7474, called from
// stateResultsPhase2 phase 1 ($4381) right before returning to attract
// mode. Places the stage-map terrain tileset (assets.MapTerrainTilesetPattern/
// Color -- already identified, see their own doc comment, as a course-
// overview screen) into VRAM, paints its 5-tile-wide by 21-row-tall
// picture at $382B one row at a time from stageMapRows (see that
// block's own doc comment for why this is a walk through 105 bytes and
// not, as an earlier pass here had it, one 5-byte row repeated), and
// resets 8 bytes of gameplay state.
func (c *Console) drawStageMapScreen() {
	patBase := uint16(c.vdp().PatternTableBase())
	colBase := uint16(c.vdp().ColorTableBase())
	c.placeInAllThirds(patBase, 0x400, assets.MapTerrainTilesetPattern) // $744C-$7454
	c.placeInAllThirds(colBase, 0x400, assets.MapTerrainTilesetColor)   // $7455-$745D

	addr := uint16(0x382B) // $745E-$7466: HL=$382B, DE=$76EC, BC=$1505 (B=21 rows, C=5 bytes)
	for i := 0; i < 21; i++ {
		c.Sys.WriteVRAM(addr, stageMapRows[i*5:i*5+5]) // $7467 (via $4575/$45A7)
		addr += 0x20
	}

	copy(c.RAM[0x04C:0x054], stageMapStateReset[:]) // $746A-$7473
}
