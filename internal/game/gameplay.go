package game

import "go-msx-racer/assets"

// This file ports the main per-frame gameplay update, $5224, and its
// callees. $5224 turned out NOT to be a single self-contained physics
// routine -- the working hypothesis before it was actually traced -- it's
// a dispatcher for essentially the entire gameplay engine: road/traffic
// physics, scrolling, scoring, and collision, fanning out into at least
// 14 separate subsystems and a web of interconnected RAM state ($E049,
// $E078, $E0B8, $E03B, and more) that would each need their own careful,
// dedicated tracing pass -- see docs/ANALYSIS.md's "What's not yet
// ported". Only the dispatcher, three small self-contained callees, and
// two screen-drawing routines found while assessing the rest
// (drawSpeedGaugeBar and drawFuelGaugeBar, sharing drawGaugeBar's code
// and reached via scrollSpeedAndDrawGauge/updateLaneFlagAndFuel/
// fuelPickupTick) are ported here; the remainder are honest TODO stubs
// citing their real addresses and whatever RAM they're confirmed to
// touch, not guessed implementations.

// GameplayTick is ported from $5224-$524E: the per-frame gameplay update,
// called once by stateGetReady and repeatedly by statePlaying. Dispatches,
// in this exact confirmed order, to 14 subsystems covering road/traffic
// physics, scrolling, scoring, and collision -- most of which remain
// unported; porting them is a multi-session undertaking on the scale of
// the sound engine's note interpreter, not something to guess at. Ends
// with what used to be an unconfirmed tail-jump to $44D9, now ported as
// redrawScoreAndHiscoreDigits.
func (c *Console) GameplayTick() {
	c.blitSpriteAttributeTable(0x34)   // $5224: CALL $526B
	c.scrollTrafficSprite()            // $5227: CALL $597D
	c.runObjectSlots()                 // $522A: CALL $70A8 -- see its own doc comment; only some object types ported so far
	c.advanceApproachingObjectMarker() // $522D: CALL $5434 -- see its own doc comment
	c.fuelPickupTick()                 // $5230: CALL $7755 -- dispatches on $E0B8; also draws the fuel gauge bar on refuel, see its own doc comment
	if c.updateSteeringAndCarState() { // $5233: CALL $6A70 -- see its own doc comment for the bool's meaning
		return
	}
	c.advanceScrollSpeedCounter()  // $5236: CALL $7374 -- see its own doc comment
	c.scrollSpeedAndDrawGauge()    // $5239: CALL $738C -- also draws the speed gauge bar, see its own doc comment
	c.checkDistanceMilestone()     // $523C: CALL $5251
	c.checkPlayerObjectProximity() // $523F: CALL $6E4A -- see its own doc comment
	if c.updateRoadAndScore() {    // $5242: CALL $5501 -- see its own doc comment for the bool's meaning
		return
	}
	c.updateLaneFlagAndFuel()       // $5245: CALL $7411 -- also draws the fuel gauge bar on drain, see its own doc comment
	c.accelerateCarSpeed()          // $5248: CALL $6CA1
	c.spawnStagedTrafficObject()    // $524B: CALL $6F25 -- see its own doc comment
	c.redrawScoreAndHiscoreDigits() // $524E: JP $44D9
}

// blitSpriteAttributeTable is ported from $526B-$527C (and its alternate
// entry $5270, called from $527D with count=$78 instead of $34): copies
// `count` bytes from a RAM staging buffer at $E10E to VRAM $3B00. That
// destination is not "one byte past the name table" as an earlier pass at
// this code assumed from address arithmetic alone -- it's exactly
// msx.VDP.SpriteAttrTableBase() for this ROM's real boot-time register
// value (R5=$76, giving base $3B00, confirmed by that multiplication
// landing on the identical address two independent things already pointed
// at: this function's own destination, and a direct $D0 byte write found
// elsewhere in the ROM at that same address -- the real TMS9918A "hide
// this sprite" Y value, see countdownGetReadyMessage). So $E10E-onward is
// the sprite-attribute-table staging buffer: 4 bytes per hardware sprite
// (Y, X, pattern, color), blitted to the real SAT every frame this runs.
// `count`=$34 (13 entries) vs $78 (30 entries) most likely reflects how
// many of the table's 32 possible sprites are in active use for a given
// call site, not a meaningful behavioral difference. See
// armPlayerCarSprites for the two entries (index 1 and 2) confirmed to be
// the player's own car, and docs/ANALYSIS.md's sprite write-up for what's
// confirmed about entry 0 ($E10E itself, see $597D/scrollTrafficSprite)
// and the remaining, still-unconfirmed entries.
func (c *Console) blitSpriteAttributeTable(count byte) {
	const src = 0x10E
	const dst = uint16(0x3B00)
	for i := 0; i < int(count); i++ {
		c.Sys.WriteVRAM(dst+uint16(i), []byte{c.RAM[src+i]})
	}
}

// scrollTrafficSprite is ported from $597D-$5997: GameplayTick's own
// $5227 dispatch (stateResultsPhase2's $4377 reaches the same address,
// and is wired to this same function). Derives sprite-attribute-table entry 0's row
// ($E10E, see blitSpriteAttributeTable) from the 16-bit distance counter
// $E078 (see checkDistanceMilestone) -- $0x6C minus (distance*4)'s high
// byte, wrapping like the original's byte SUB+NEG -- so this entry's
// on-screen row shifts as the player's own distance traveled advances,
// unlike the player's own row-pair ($E04B/$E04C, see accelerateCarSpeed)
// which only changes with speed. Every 16th frame, also toggles that same
// entry's color byte ($E111) by XOR 6, a blink effect. What entry 0
// represents isn't confirmed -- a traffic/opponent car (scrolling with
// relative distance) is the leading guess, but not the player's own car,
// which is entries 1-2 (armPlayerCarSprites).
func (c *Console) scrollTrafficSprite() {
	distance := uint16(c.RAM[0x078]) | uint16(c.RAM[0x079])<<8
	scaled := byte(distance >> 6) // $597D-$5982: HL<<=2 (ADD HL,HL x2); A=H
	c.RAM[0x10E] = 0x6C - scaled  // $5983-$598A: SUB $6C; NEG
	if c.RAM[0x003]&0x0F != 0 {   // $598D-$5992: only every 16th frame
		return
	}
	c.RAM[0x111] ^= 6 // $5993-$5996
}

// checkDistanceMilestone is ported from $5251-$526A: once the 16-bit
// distance counter at $E078 reaches or exceeds $0F12, and bit 6 of $E002
// is set, triggers sound id $92 -- then (regardless of that bit) sets
// $E049 to 3. $E002 bit 6 is the same bit RunGameStateMachine's own
// stack-trick branch tests (see its doc comment). $E049 gates several of
// GameplayTick's still-unported callees ($7374, $6E4A, $5501, all "DEC A;
// RET Z" on it being exactly 1) and is zeroed by $504C during gameplay
// init (see stateGetReady) -- plausibly a race-phase flag, with this
// being the first confirmed write that moves it elsewhere, but that
// reading isn't confirmed, only the mechanism is.
func (c *Console) checkDistanceMilestone() {
	distance := uint16(c.RAM[0x078]) | uint16(c.RAM[0x079])<<8
	if distance < 0x0F12 { // $5254-$525A: LD BC,$0F12; SBC HL,BC; RET C
		return
	}
	if c.RAM[0x002]&0x40 != 0 { // $525B-$525E: LD A,(E002h); BIT 6,A
		c.TriggerSound(0x92) // $5260-$5262: LD A,92h; CALL NZ,$4B6E
	}
	c.RAM[0x049] = 3 // $5265-$5267: LD A,03h; LD (E049h),A
}

// gaugeBarTiles are the 5 name-table tile groups (3 bytes each) at
// $73FC-$740A, selecting how "full" one row of a gauge bar looks: index
// 0-2 are increasingly-full partial rows, index 3 is a fully-lit row,
// index 4 is unlit. drawGaugeBar indexes this by (value>>3)&3 for the
// one partial row, and by a fixed 3/4 for full/unlit rows. Shared by both
// the speed and fuel gauges (see $73B2/$7444's own doc comments) -- the
// original reuses this exact code for both, distinguished only by which
// value is loaded into A and which column the caller starts at.
var gaugeBarTiles = [5][3]byte{
	{0x6C, 0x6D, 0x6E},
	{0x69, 0x6A, 0x6B},
	{0x66, 0x67, 0x68},
	{0x63, 0x64, 0x65},
	{0x61, 0x04, 0x62},
}

// drawGaugeBar is ported from $73B2-$73FA, the shared tail both the
// speed gauge ($73AD, entering with A=$E04F, column $D8) and the fuel
// gauge ($7444, entering with A=$E083, column $DB -- exactly 3 columns,
// one gauge-width, to the right) jump into. Renders `value` as a
// 3-tile-wide vertical bar gauge growing upward from name-table row 22
// (`$3A00 | col`) -- top 3 bits of `value` select how many rows are
// fully lit, the next 2 bits select the one partially-lit row above
// them, and the remaining rows above that are unlit, 32 discrete fill
// levels in all.
//
// An earlier pass at this port drew a fixed 8 rows for every value,
// reaching name-table row 15. The original never does: every one of its
// three drawing loops (full rows via repeated "SUB $20" until it borrows,
// the unlit fill, and the value==0 all-unlit case) shares the same
// boundary check at $73ED-$73F0 ("CP H; RET Z" once the address's high
// byte reaches $39, i.e. row 15) and stops the instant it would touch
// that row -- confirmed by hand-tracing every value band (0, a
// mid-range value, and the real $D7 speed/fuel cap all stop after
// exactly 7 rows, rows 16-22; only a value of $E0 or above, unreachable
// given that $D7 cap, would ever reach row 15 through the full-rows count
// alone). So the gauge is genuinely 7 rows tall in practice, not 8 --
// row 15 belongs to other HUD graphics above it, and drawing into it
// (what the fixed-8-rows version did) visibly overwrote them at high
// gauge values, exactly the bug a real-hardware comparison caught.
func (c *Console) drawGaugeBar(value byte, col uint16) {
	addr := uint16(0x3A00) | col

	if value == 0 { // $73B4-$73B5: OR A; JR Z,$73F1
		c.drawGaugeBarUnlitFrom(addr)
		return
	}

	fullRows := int(value >> 5) // $73B8-$73BC, $73BA/$73D2's loop
	for i := 0; i < fullRows; i++ {
		c.Sys.WriteVRAM(addr, gaugeBarTiles[3][:])
		addr -= 0x20
	}

	subnotch := (value >> 3) & 0x03 // $73D4-$73DC
	c.Sys.WriteVRAM(addr, gaugeBarTiles[subnotch][:])
	addr -= 0x20

	if addr>>8 == 0x39 { // $73ED-$73F0: reached row 15 -- stop, don't touch it
		return
	}
	c.drawGaugeBarUnlitFrom(addr)
}

// drawGaugeBarUnlitFrom is the shared unlit-fill loop at $73E9-$73FA:
// draws unlit tiles upward from `addr` (inclusive), stopping the instant
// the address would reach name-table row 15 ($39xx) -- see drawGaugeBar's
// own doc comment for why the gauge never actually draws into that row.
func (c *Console) drawGaugeBarUnlitFrom(addr uint16) {
	for addr>>8 != 0x39 {
		c.Sys.WriteVRAM(addr, gaugeBarTiles[4][:])
		addr -= 0x20
	}
}

// drawSpeedGaugeBar is ported from $73AD-$73B0 plus the shared
// $73B2-$73FA tail (see drawGaugeBar): renders $E04F (the "speed" value
// also read by engineSoundPitchStep and the acceleration logic at
// $6CA1) at column $D8. Reached both by falling through from
// scrollSpeedAndDrawGauge (every frame) and, when $E04F==0 exactly, via
// its own early branch straight to an all-unlit gauge ($73B4-$73B5,
// inside drawGaugeBar).
func (c *Console) drawSpeedGaugeBar() {
	c.drawGaugeBar(c.RAM[0x04F], 0xD8)
}

// drawFuelGaugeBar is ported from $7444-$7449 plus the shared
// $73B2-$73FA tail (see drawGaugeBar): renders $E083 at column $DB,
// immediately right of the speed gauge. $E083 is confirmed to be a
// genuine depleting fuel level, not just a crash-adjacent flag as
// earlier TODOs in this file guessed from its threshold checks alone:
// $6AF4 (part of the player-car state machine dispatched from $6A70)
// drains it by $14 every 8 frames, updateLaneFlagAndFuel below drains it
// by 1 on specific frames, and fuelPickupTick refills it by $20 (capped
// at $D7) when driving over a fuel pickup -- and running it all the way
// down is what $6B05-$6B58's "EMPTY" message (drawn at the car's own
// screen position, not this HUD gauge) responds to.
//
// CORRECTED: an earlier pass at this port concluded updateLaneFlagAndFuel's
// own trigger (bit 5 of $E057 vs. bit 5 of $E084) could never fire, since
// $E057 has no write site anywhere in this port's own disassembled ~73%
// of the ROM via direct addressing -- and shipped a STOPGAP, explicitly
// labeled as not ROM-faithful, that called this once at race start just
// so the gauge would be visible at all (stateGetReadyWait's own phase 3).
// That conclusion was right about the disassembly coverage but wrong
// about the trigger being dead: found by tracing real writes to $E057 on
// the actual ROM running on a real Z80 core (the same technique used for
// the road-curve and VRAM-sharing investigations elsewhere in this
// project) -- $E057 *is* written, by $7374 ($5236's own GameplayTick
// dispatch, previously an untraced TODO), via a 16-bit `LD (E056h),HL`
// that touches $E057 as HL's high byte. See advanceScrollSpeedCounter's
// own doc comment for the full trace; now ported, so
// updateLaneFlagAndFuel's edge-detection genuinely fires (confirmed:
// fuel visibly drains roughly every 20 real frames in this port now,
// matching the real ROM under the same test). The stopgap call in
// stateGetReadyWait's phase 3 was removed as part of that fix.
//
// FOLLOW-UP CORRECTION: removing the stopgap immediately surfaced a
// second, real gap -- the gauge was then invisible for the first ~20
// frames of every race (blank until the first natural drain), which a
// user reported directly ("not shown when starting game, appears soon
// after"). The real ROM does NOT leave it blank: this function has a
// genuine third, initial call site after all, at drawHUDBorderDecoration's
// own tail ($740E: `JP $7444`) -- previously mistranslated as a call to
// redrawScoreAndHiscoreDigits() instead (see drawHUDBorderDecoration's
// own doc comment in graphics.go for the full story). With that fixed,
// this now runs once at race start (via drawScoreDisplay ->
// drawHUDBorderDecoration), matching the real ROM's own VRAM content
// byte-for-byte at the moment gameplay begins.
func (c *Console) drawFuelGaugeBar() {
	c.drawGaugeBar(c.RAM[0x083], 0xDB)
}

// advanceScrollSpeedCounter is ported from $7374-$738B, GameplayTick's
// own $5236 dispatch (previously an untraced TODO). Returns immediately
// if $E049==1 (matching checkDistanceMilestone's own doc comment on what
// that value gates); otherwise adds a level-dependent step -- $01FF for
// level B ($E03B nonzero), $0180 for level A -- to the 16-bit counter at
// $E056/$E057 (`LD BC,01FFh` then, only for level A, `LD C,80h`
// overwriting just the low byte, giving exactly $0180).
//
// Found while investigating a user report that the fuel gauge never
// visually decreases: this is the real write site for $E057, whose bit
// 5 flipping is what updateLaneFlagAndFuel's own edge-detection reacts
// to in order to drain fuel and redraw the gauge -- an earlier pass at
// this port had exhaustively grepped the disassembled ~73% of the ROM
// for a direct write to $E057 and found none, concluding the trigger
// was permanently dead and shipping a doc-labeled stopgap instead (see
// drawFuelGaugeBar's own corrected doc comment). The real write is a
// 16-bit `LD (E056h),HL` at $7388 that touches $E057 as HL's high byte
// -- invisible to a literal 8-bit-address grep. Found by tracing actual
// writes to $E057 on the real ROM running on a real Z80 core rather than
// reading further by hand, the same technique used for the road-curve
// and VRAM-sharing investigations elsewhere in this project.
func (c *Console) advanceScrollSpeedCounter() {
	if c.RAM[0x049] == 1 {
		return
	}
	step := uint16(0x01FF)
	if c.RAM[0x03B] == 0 { // level A
		step = 0x0180
	}
	sum := uint16(c.RAM[0x056]) | uint16(c.RAM[0x057])<<8
	sum += step
	c.RAM[0x056] = byte(sum)
	c.RAM[0x057] = byte(sum >> 8)
}

// scrollSpeedAndDrawGauge is ported from $738C-$73FA, called
// unconditionally every frame by GameplayTick at $5239. Accumulates
// $E04F (speed) into an 8-bit sub-counter at $E077, carrying into the
// 16-bit distance counter $E078 on overflow (see checkDistanceMilestone's
// doc comment for what reads $E078), stashes the current speed into
// $E051 unless $E049==5 (not confirmed why that value is special, though
// $E049==5 is also what $7411/updateLaneFlagAndFuel sets on one of its
// own branches), then draws the speed gauge bar.
//
// $7374 (GameplayTick's separate $5236 dispatch) is NOT a fallthrough
// into this routine despite being physically adjacent and superficially
// similar (both gate on/touch $E049) -- it ends in its own RET at $738B
// before this routine's own code even starts, confirmed by the raw
// bytes, not assumed from proximity (the same mistake this project
// already corrected once for $5028/$504C).
func (c *Console) scrollSpeedAndDrawGauge() {
	speed := c.RAM[0x04F]
	sum := uint16(c.RAM[0x077]) + uint16(speed)
	c.RAM[0x077] = byte(sum)
	if sum > 0xFF { // $7394: JR NC -- carry means the 8-bit add overflowed
		distance := uint16(c.RAM[0x078]) | uint16(c.RAM[0x079])<<8
		distance++
		c.RAM[0x078] = byte(distance)
		c.RAM[0x079] = byte(distance >> 8)
	}

	if c.RAM[0x049] != 5 { // $73A0-$73A8: CP 05h; JR Z (skip the write)
		c.RAM[0x051] = speed
	}

	c.drawSpeedGaugeBar() // $73AD onward
}

// updateLaneFlagAndFuel is ported from $7411-$7441, GameplayTick's own
// $5245 dispatch. Edge-detects a difference between bit 5 of $E057 and
// bit 5 of $E084 (semantics not confirmed -- $E057/$E084 read like a
// per-lane or per-object bitmask pair, given $7411's own doc comment
// before this trace; possibly "did the car's lane assignment change")
// and, only when they differ: acknowledges it by toggling $E084's bit 5,
// drains $E083 (fuel) by 1 -- or, if fuel was already 0, sets $E049 to 2
// instead of going negative -- redraws the fuel gauge, and, once fuel
// drops below $10, beeps a low-fuel warning (sound id 2) unless $E028
// (a channel-record byte, see triggerSoundRaw's slot table) is already
// nonzero.
func (c *Console) updateLaneFlagAndFuel() {
	bit := c.RAM[0x057] & 0x20
	if c.RAM[0x084]&0x20 == bit { // $7411-$741E
		return
	}
	c.RAM[0x084] ^= 0x20 // $741F-$7421

	if c.RAM[0x083] == 0 { // $7422-$7425
		c.RAM[0x049] = 2 // $742C-$742F
	} else {
		c.RAM[0x083]--         // $7427
		if c.RAM[0x083] == 0 { // $7428-$742A
			c.RAM[0x049] = 2 // $742C-$742F
		}
	}

	c.drawFuelGaugeBar() // $7431: CALL $7444

	if c.RAM[0x083] >= 0x10 { // $7434-$7439
		return
	}
	if c.RAM[0x028] != 0 { // $743A-$743E
		return
	}
	c.TriggerSound(2) // $743F-$7441
}

// fuelPickupScoreTable is the 5-byte ROM table at $7791, indexed by
// min($E0B9, 4) -- the packed-BCD "tens/hundreds" score bonus awarded
// for successive fuel pickups within one refuel sequence (0, 3, 5, 8,
// then 16 points for the 4th and every one after).
var fuelPickupScoreTable = [5]byte{0x00, 0x03, 0x05, 0x08, 0x10}

// fuelPickupTick is ported from $7755-$77C7 (GameplayTick's own $5230
// dispatch), a small dispatcher on $E0B8 (0 = idle, RET; 1 = refueling;
// 2 = a sparkle/twinkle animation playing out after the refuel).
//
// $E0B8==1: refills $E083 (fuel) by $20, capped at $D7 (the same cap the
// speed gauge's own $D7-ish scale tops out around, via the acceleration
// logic at $6CA1), redraws the fuel gauge, advances $E0B8 to 2, bumps a
// separate pickup counter ($E0B9), arms $E0BA=$60 (a countdown for the
// sparkle animation below), and awards a score bonus via
// fuelPickupScoreTable indexed by $E0B9 (clamped to 4) -- confirmed via
// addToScore's own real disassembly that its "hi" (packed-BCD hundreds)
// parameter is unconditionally zeroed at its own entry ($447F: `LD
// C,00h`) regardless of whatever the caller passes, so this project's
// one existing addToScore call site and this new one both only ever
// need to pass 0 there; not a register-threading risk as an earlier
// pass at this function assumed.
//
// $E0B8==2: counts $E0BA down every call; while nonzero, updates a
// decorative sprite-attribute-table entry ($E13A-$E13D, the same
// staging-buffer family blitSpriteAttributeTable copies to VRAM) to
// float just above the player's own car ($E112/$E113, player row/col,
// confirmed via armPlayerCarSprites) and cycle through 4 sprite pattern
// frames as $E0B9 (the same pickup counter from above) increases -- a
// sparkle/twinkle effect on the spot where fuel was just collected.
// Once $E0BA reaches 0, resets $E0B8 to 0 (idle) and hides that sprite
// entry ($E13A=$E0, an off-screen Y value).
func (c *Console) fuelPickupTick() {
	switch c.RAM[0x0B8] {
	case 1:
		fuel := int(c.RAM[0x083]) + 0x20 // $775F-$7765
		if fuel > 0xD7 {                 // $7767-$776A
			fuel = 0xD7
		}
		c.RAM[0x083] = byte(fuel)
		c.drawFuelGaugeBar() // $776C: CALL $7444
		c.RAM[0x0B8]++       // $776F-$7772
		c.RAM[0x0B9]++       // $7773-$7774
		c.RAM[0x0BA] = 0x60  // $7775-$7776
		idx := c.RAM[0x0B9]  // $7778-$777E: clamp to 4
		if idx > 4 {
			idx = 4
		}
		c.addToScore(fuelPickupScoreTable[idx], 0) // $7780-$7789: CALL $403B then $447F
		c.TriggerSound(3)                          // $778C-$778E

	case 2:
		c.RAM[0x0BA]--         // $7796-$7799
		if c.RAM[0x0BA] == 0 { // $779A: JR Z,$77BE
			c.RAM[0x0B8] = 0    // $77BE-$77C0
			c.RAM[0x13A] = 0xE0 // $77C2-$77C7
			return
		}
		c.RAM[0x13A] = c.RAM[0x112] - 0x10 // $779C-$77A5
		c.RAM[0x13B] = c.RAM[0x113]        // $77A7-$77A9: LDI
		idx := c.RAM[0x0B9] - 1            // $77AA-$77B2: clamp to 3
		if idx >= 4 {
			idx = 3
		}
		c.RAM[0x13C] = idx*4 + 0x94 // $77B4-$77B8
		c.RAM[0x13D] = 6            // $77BA-$77BC
	}
}

// pixelToNameTableAddr is ported from $4561-$4574: converts a (row, col)
// pair -- each an 8-bit value whose top 5 bits select a tile position --
// into a name-table VRAM address ($3800 + (row>>3)*32 + (col>>3)).
// Confirmed by literally simulating the original's odd interleaved
// rotate sequence (4 solo `RRA`s on the row byte, then 3 rounds of
// `RR H`/`RRA` crossing bits between the two registers) against this
// closed-form formula across a full sweep of inputs and both possible
// incoming carry states (the original never clears carry first, but the
// carry-dependent bits all get discarded by the final `AND $03`, so the
// result is carry-independent) -- not derived by inspection alone.
func pixelToNameTableAddr(row, col byte) uint16 {
	return 0x3800 + uint16(row>>3)*32 + uint16(col>>3)
}

// fuelEmptyMessage is the ROM data window $6B59-$6B5D: "EMPTY", drawn at
// the car's own screen position when it runs out of fuel.
var fuelEmptyMessage = [5]byte{0x25, 0x2D, 0x30, 0x34, 0x39}

// drawFuelEmptyMessage is ported from $6B1C-$6B58: draws "EMPTY" at the
// car's current screen position (converted from $E04C/$E04E via
// pixelToNameTableAddr, clamped to name-table column 18 or less so the
// message can't be drawn under the HUD panel), plays a sound (id $1B,
// reusing the startup-jingle ID -- confirmed literal, not a copy/paste
// mistake in this port) once the requesting channel is free, and
// transitions to $E049=6.
func (c *Console) drawFuelEmptyMessage() {
	if c.RAM[0x028] != 0 { // $6B1C-$6B20
		return
	}
	c.TriggerSound(0x1B) // $6B21-$6B23

	row := c.RAM[0x04C] - 0x10 // $6B26-$6B2F
	col := c.RAM[0x04E] - 0x08 // $6B26-$6B33
	addr := pixelToNameTableAddr(row, col)

	if addr&0x1F >= 0x12 { // $6B37-$6B3C
		addr = (addr & 0xFFE0) | 0x12 // $6B3E-$6B43
	}

	c.Sys.WriteVRAM(addr, fuelEmptyMessage[:]) // $6B44-$6B4A
	c.RAM[0x004] = 0x80                        // $6B4D-$6B4F
	c.RAM[0x049] = 6                           // $6B52-$6B54
}

// carSpritePatternTable is the 11-byte ROM table at $6E3F, indexed by the
// per-state car-sprite-pattern selector byte $E053 -- see
// armPlayerCarSprites. All 11 values are multiples of 8, consistent with
// selecting between groups of hardware sprite patterns (32 bytes/pattern)
// -- plausibly one group per steering angle (straight, turning left/right
// by varying amounts), though which specific value means which angle
// isn't confirmed.
//
// This table, and the two functions below, were originally named around
// an "engine sound" guess (the only thing updateSteeringAndCarState was
// otherwise known to touch) -- corrected once tracing $6CA1 confirmed
// $E10E onward is the sprite-attribute-table staging buffer blitSpriteAttributeTable
// blits to VRAM's real SAT every frame (see its own doc comment, and
// docs/ANALYSIS.md's sprite write-up), and $E112/$E116 -- exactly where
// this table's values land -- are two of that buffer's 4-byte sprite
// records.
var carSpritePatternTable = [11]byte{0x00, 0x10, 0x18, 0x20, 0x08, 0x38, 0x30, 0x28, 0x40, 0x48, 0x68}

// armPlayerCarSprites is ported from $6E1D-$6E3E: writes the player car's
// own 2-sprite pair -- sprite-attribute-table entries 1 and 2 ($E112 and
// $E116 in the RAM staging buffer blitSpriteAttributeTable copies to VRAM's
// real SAT, entry 0 being $E10E, see docs/ANALYSIS.md) -- confirming the
// real hardware's cars genuinely are two stacked/paired 16x16 sprites,
// not one. Each entry gets (Y, X, pattern) = ($E04C, $E04E, tableValue):
// $E04C/$E04E are the car's own row/column position (per
// drawFuelEmptyMessage's use of those same two bytes -- $E04D is skipped,
// being the fixed-point fraction byte of a separate $E04D/$E04E pair, see
// updateCarPositionFromSteering), and tableValue is
// carSpritePatternTable[$E053] for the first sprite, +4 for the second --
// i.e. the two sprites making up one car are always 4 pattern slots apart
// in the sprite pattern table. Neither entry's color byte (offset 3) is
// touched here -- see initPlayerCarSpritesAndFuel for where the two
// colors are confirmed to come from.
func (c *Console) armPlayerCarSprites() {
	a := carSpritePatternTable[c.RAM[0x053]]
	posC, posB := c.RAM[0x04C], c.RAM[0x04E]
	c.RAM[0x112], c.RAM[0x113], c.RAM[0x114] = posC, posB, a
	c.RAM[0x116], c.RAM[0x117], c.RAM[0x118] = posC, posB, a+4
}

// setCarSpritePattern is the shared tail at $6AB4/$6AB5 -- $6AB4 (used
// when the caller wants the selector forced to 0, i.e. "facing straight
// ahead") is just "XOR A" falling into $6AB5 (used when the caller has
// already loaded a specific selector into A); both then store into $E053
// and re-arm armPlayerCarSprites.
func (c *Console) setCarSpritePattern(selector byte) {
	c.RAM[0x053] = selector
	c.armPlayerCarSprites()
}

// clearCarBehaviorState is the shared tail at $6BDB-$6BDF: resets
// car-behavior state to 0 (normal driving) and the car's own sprite
// pattern to 0, via setCarSpritePattern. Used by updateSteeringAndCarState's
// own case 4 ending and by $6E4A (checkPlayerObjectProximity).
func (c *Console) clearCarBehaviorState() {
	c.RAM[0x049] = 0 // $6BDB-$6BDC
	c.setCarSpritePattern(0)
}

// applySignedPositionOffset is ported from $6E0B-$6E1C: adds a magnitude
// `e`, doubled and optionally negated, to the 16-bit fixed-point car
// position at $E04D (low/fraction byte) and $E04E (high byte -- the same
// byte drawFuelEmptyMessage reads directly as a plain pixel column,
// consistent with $E04E being this pair's integer part and $E04D a
// sub-pixel fraction accumulated for smooth motion at less than 1
// pixel/frame). Shared by updateCarPositionFromSteering's own tail and by
// state 5's direct call.
func (c *Console) applySignedPositionOffset(e byte, negate bool) {
	offset := int16(e)
	if negate {
		offset = -offset
	}
	offset *= 2 // $6E14-$6E16: SLA E; RL D
	pos := int16(uint16(c.RAM[0x04D]) | uint16(c.RAM[0x04E])<<8)
	pos += offset
	c.RAM[0x04D] = byte(pos)
	c.RAM[0x04E] = byte(pos >> 8)
}

// updateCarPositionFromSteering is ported from $6DEC-$6E1C: derives a
// steering magnitude from the last recorded speed ($E051, halved plus a
// fixed +8 floor unless speed is exactly 0), picks a direction from the
// edge-triggered input state ($E009 bits 2-3, refined by the sticky
// last-direction latch $E048 in the rare case both bits read set at
// once), and applies it via applySignedPositionOffset. Called from
// updateNormalDriving (state 0) whenever fresh steering input is latched.
func (c *Console) updateCarPositionFromSteering() {
	speed := c.RAM[0x051]
	var e byte
	if speed != 0 {
		e = (speed >> 1) + 8
	}
	input := c.RAM[0x009]
	masked := input & 0x0C
	if masked == 0x0C { // $6DFE-$6E02: both edge-triggered bits set at once
		masked = c.RAM[0x048] & input
	}
	c.applySignedPositionOffset(e, masked&0x04 != 0) // $6E08-$6E0D: RRA x3 extracts bit 2 into carry, confirmed carry-independent the same way pixelToNameTableAddr's rotate chain is
}

// nudgeCarPositionTowardSpeed is ported from $6BE2-$6C04: sets the engine
// sound selector to 7 (or 1, when $E054 is set), then adds ($E051-$0C) --
// as a plain byte subtraction, wrapping like the original's SUB rather
// than clamped to zero -- to the car's fixed-point position, negated when
// $E054 is set. Unlike updateCarPositionFromSteering's tail, this jumps
// straight to the shared add ($6E18) without doubling first. Called
// directly by states 4 and 7; $E054's role isn't confirmed (possibly a
// "which side of the road" or "AI-controlled" flag, given it also swaps
// which of two engine-sound selectors gets used here and in state 3).
func (c *Console) nudgeCarPositionTowardSpeed() {
	selector := byte(7)
	offset := int16(c.RAM[0x051] - 0x0C)
	if c.RAM[0x054] != 0 {
		selector = 1
		offset = -offset
	}
	c.RAM[0x053] = selector
	pos := int16(uint16(c.RAM[0x04D]) | uint16(c.RAM[0x04E])<<8)
	pos += offset
	c.RAM[0x04D] = byte(pos)
	c.RAM[0x04E] = byte(pos >> 8)
}

// drainFuelAndResetPickup is ported from $6AF4-$6B02: drains $E083 (fuel)
// by $14, floored at 0, resets the fuel-pickup dispatcher ($E0B8) to idle,
// then redraws the speed gauge and both score digit groups -- the
// original reaches these via a chain of tail-jumps ($452A -> $740B ->
// $740E -> $44D9) rather than genuine calls, but the net effect once it
// eventually returns is the same as calling them directly. Called from
// updateSteeringAndCarState's state 1 once its own ~11-frame sub-counter
// ($E04A) counts all the way down.
func (c *Console) drainFuelAndResetPickup() {
	fuel := int(c.RAM[0x083]) - 0x14
	if fuel < 0 {
		fuel = 0
	}
	c.RAM[0x083] = byte(fuel)
	c.RAM[0x0B8] = 0
	c.drawSpeedGaugeBar()
	c.redrawScoreAndHiscoreDigits()
}

// updateNormalDriving is ported from $6A90-$6AB8: state 0 ($E049==0) of
// updateSteeringAndCarState's dispatch -- "normal driving," reached both
// via the dispatch table and as a genuine subroutine call from state 2
// ($6B0C, once the fuel-empty deceleration below has room to keep
// driving). First checks for an off-track collision/crash
// (checkOffTrackCollisionAndCrash, $6CD1); if that signals "abort," skips
// everything else here and propagates the signal up (see its own doc
// comment). Otherwise applies a steering-driven position update when both
// the player's edge-triggered input and a scaled/shifted copy of $E055
// agree a direction is held (exact relationship between $E055 and the
// input bits not confirmed), re-arms the continuous engine-pitch sound
// (id 1) whenever channel 0 has gone idle and $E002 bit 6 is set, then
// resets the engine sound selector to 0. The bool return mirrors
// updateSteeringAndCarState's own "abort the caller" convention.
func (c *Console) updateNormalDriving() bool {
	if c.checkOffTrackCollisionAndCrash() { // $6A90: CALL $6CD1
		return true
	}
	m1 := (c.RAM[0x055] << 2) & 0x0C
	m2 := c.RAM[0x009] & 0x0C
	if m1&m2 != 0 { // $6AA1: CALL NZ,$6DEC
		c.updateCarPositionFromSteering()
	}
	if c.RAM[0x002]&0x40 != 0 { // $6AA4-$6AA9: BIT 6,A; JR Z (skip if clear)
		if c.RAM[0x010] == 0 { // $6AAB-$6AAF: channel 0's chOffActive
			c.TriggerSound(1) // $6AB1: CALL Z,$4B6E
		}
	}
	c.setCarSpritePattern(0) // $6AB4-$6AB8
	return false
}

// updateSteeringAndCarState is ported from $6A70-$6C5A (mostly): GameplayTick's
// own $5233 dispatch and the routine `statePlaying`'s DJNZ-taken branch
// tail-jumps to via $527D on the ~255 frames out of 256 that don't call
// GameplayTick directly (see $527D's own doc comment, `advanceCarBehavior`).
//
// First, unconditionally: latches bits 2-3 of $E008 (raw combined input)
// into $E048, but only when nonzero -- a "sticky" last-active-direction
// value, used by updateCarPositionFromSteering above. Then dispatches on
// $E049 (the same byte checkDistanceMilestone/the fuel-drain routines
// write) via the 8-entry jump table at $6A80.
//
// The return value models a stack trick several of the 8 states use:
// their very first instruction is "POP HL", discarding the return address
// this function's own caller pushed. Any RET reached afterward then
// returns not to that immediate caller, but to *its* caller instead --
// i.e. it also aborts the rest of whatever GameplayTick/advanceCarBehavior
// was about to do this frame. States 1, 3, and 6 do this (unconditionally,
// on every exit path); the others (0, 2, 4, 5, 7) behave like an ordinary
// function. `true` means "the caller should return immediately too,"
// mirroring how decrementAttractSubCounter's own doc comment describes
// modeling the same kind of discarded-return-address trick as a plain
// early `return` at the call site instead of replicating the stack
// mechanics literally.
func (c *Console) updateSteeringAndCarState() bool {
	if input := c.RAM[0x008] & 0x0C; input != 0 { // $6A70-$6A7A
		c.RAM[0x048] = input
	}

	switch c.RAM[0x049] { // $6A7B-$6A7D: dispatch via the $6A80 table
	case 0:
		return c.updateNormalDriving()

	case 1:
		if c.RAM[0x003]&0x07 != 0 { // $6ABC-$6AC1: only every 8th frame
			return true
		}
		c.RAM[0x04A]--
		switch {
		case c.RAM[0x04A] == 0: // $6AC6: JR Z,$6ADF
			c.drainFuelAndResetPickup()
			c.RAM[0x049] = 6 // $6AE2-$6AE4
			c.RAM[0x004] = 0x10
			if c.RAM[0x083] != 0 { // $6AEC-$6AF0: fuel unexpectedly nonzero
				// The original performs a further, deeper stack unwind
				// here (an extra discarded return address beyond this
				// function's own) that can't cleanly be modeled without
				// restructuring the whole call chain -- approximated as
				// the same single-level abort every other exit from this
				// state uses. Rare in practice: $E083 is expected to
				// already be 0 whenever $E04A's countdown reaches zero.
				return true
			}
			c.drawFuelEmptyMessage() // $6B1C onward, shared with state 2
			return true
		case c.RAM[0x04A] == 11: // $6AC9-$6ACD
			c.TriggerSound(0x4F) // $6AD7-$6AD9
			c.setCarSpritePattern(8)
			return true
		case c.RAM[0x04A] == 9: // $6ACF-$6AD1
			c.setCarSpritePattern(9)
			return true
		default:
			return true // $6AD1: RET NZ
		}

	case 2:
		if c.RAM[0x083] != 0 { // $6B05-$6B09: JP NZ,$6C2C
			c.RAM[0x049] = 0 // $6C2C-$6C2F
			return false
		}
		if c.updateNormalDriving() { // $6B0C: CALL $6A90
			return true
		}
		decremented := c.RAM[0x04F] - 1 // $6B0F-$6B13: DEC A
		if decremented >= 0xF0 {        // $6B14-$6B16: CP $F0; JR NC,$6B1C -- taken once speed wraps past 0
			c.drawFuelEmptyMessage()
			return false
		}
		c.RAM[0x04F] = decremented // $6B18: LD (HL),A
		c.scrollRoadBackward()     // $6B19: JP $6C73
		return false

	case 3:
		c.redrawScoreAndHiscoreDigits() // $6B5F: CALL $44D9
		c.RAM[0x0EE] = 0x80
		c.RAM[0x0FE] = 0x80
		c.setCarSpritePattern(0) // $6B6A: CALL $6AB4
		c.RAM[0x04F] = 0         // $6B6D-$6B6E: speed=0
		delta := c.RAM[0x04C] - 0xC8
		if delta >= 8 { // $6B77-$6B79: CP 08h; JR C (delta<8 skips this)
			c.RAM[0x04C] -= 4
			return true
		}
		if c.RAM[0x0E6] == 0 && c.RAM[0x0F6] == 0 { // $6B80-$6B89
			*c.gameOverFlag() = 1 // $6B8A-$6B8C
		}
		return true

	case 4:
		c.RAM[0x0B1]--
		if c.RAM[0x0B1] == 0 { // $6B93-$6B94
			val := byte(7)
			if c.RAM[0x054] != 0 { // $6B96-$6B9E
				val = 1
			}
			c.RAM[0x0B1] = val
			c.RAM[0x049] = 7         // $6BA1-$6BA6
			if c.RAM[0x04F] < 0xA0 { // $6BA7-$6BAC
				c.RAM[0x049] = 0 // $6BAD: cancel the transition, speed too low
			}
			return false
		}
		c.nudgeCarPositionTowardSpeed()         // $6BB0: CALL $6BE2
		if c.checkOffTrackCollisionAndCrash() { // $6BB3: CALL $6CD1 -- same target and stack depth as updateNormalDriving's own $6A90 call (case 4 is entered via the same discarding trampoline as case 0, see this function's own doc comment), so its "double-abort" return-true is modeled identically here
			return true
		}
		c.armPlayerCarSprites()                               // $6BB6: CALL $6E1D
		if c.RAM[0x002]&0x40 != 0 && c.RAM[0x003]&0x03 == 0 { // $6BB9-$6BC7
			c.TriggerSound(0x0D)
		}
		if c.RAM[0x054] != 0 { // $6BCA-$6BDA
			if c.RAM[0x008]&0x04 == 0 {
				return false
			}
		} else {
			if c.RAM[0x008]&0x08 == 0 {
				return false
			}
		}
		c.clearCarBehaviorState() // $6BDB-$6BDF
		return false

	case 5:
		c.RAM[0x004]--
		if c.RAM[0x004] == 0 { // $6C09: JR Z,L6C24 -- shared tail with state 2's $6C2C
			if c.RAM[0x083] == 0 {
				c.RAM[0x049] = 2
			} else {
				c.RAM[0x049] = 0
			}
			return false
		}
		speed := c.RAM[0x051]
		if speed != 0 { // $6C0F-$6C12
			speed--
			c.RAM[0x051] = speed
		}
		e := speed >> 1                        // $6C17: SRL A
		negate := c.RAM[0x055]&1 != 0          // $6C1A-$6C1D: RRA's carry-out is bit 0 of $E055
		c.applySignedPositionOffset(e, negate) // $6C1E: CALL $6E0B
		return false

	case 6:
		c.decrementAttractSubCounter() // $6C32: CALL $440C (return value unused)
		*c.crashFlag() = 0             // $6C35-$6C38
		return true                    // $6C39: RET -- double-return, see doc comment

	case 7:
		c.nudgeCarPositionTowardSpeed() // $6C3A: CALL $6BE2
		if c.RAM[0x003]&0x03 == 0 {     // $6C3D-$6C45: every 4th frame
			c.TriggerSound(0x0B)
			c.RAM[0x0B1]--
			if c.RAM[0x054] != 0 { // $6C51-$6C56
				c.RAM[0x0B1] += 2
			}
		}
		c.setCarSpritePattern(c.RAM[0x0B1] & 0x07) // $6C57-$6C5A
		// $6C5D-$6C64: CALL $6DC1 directly -- unlike updateNormalDriving's
		// own $6CD1 call, this skips the $E055 stash and the $6D09
		// crash/bounce distinction entirely: any off-track detection here
		// goes straight to the same "stop and start recovering" sequence
		// checkOffTrackCollisionAndCrash's own $E04F>=$A0 branch uses.
		if _, _, unsafe := c.checkOffTrackCollision(0xE04C); unsafe {
			c.triggerOffTrackRecovery() // $6F18-$6AFF, shared with checkPlayerObjectProximity
			// This was modeled as a double-discarded-return-address
			// abort (matching checkOffTrackCollisionAndCrash's own,
			// separate $6D04 mechanism), but tracing checkPlayerObjectProximity's
			// own reach of this exact same tail (see its doc comment)
			// worked out that call site's stack depth precisely and
			// found it resolves to a normal, single-level return, not a
			// double-discard -- which raises a real question about
			// whether "return true" here is actually correct too, since
			// this call site (`CALL $6DC1` at $6C60, unconsumed when the
			// unsafe branch is taken) looks structurally similar. Left
			// as `return true` since this exact behavior is
			// already-tested, working code; flagged here as an open
			// question deserving its own focused re-verification rather
			// than a change made in passing while working on something
			// else.
			return true
		}
		return false
	}
	return false
}

// advanceCarBehavior is ported from $527D-$528F (partially): the
// dispatch chain `statePlaying`'s own DJNZ-taken branch tail-jumps to on
// every frame it does NOT call GameplayTick directly -- roughly 255 out
// of every 256 frames, given `statePlaying` decrements $E001 from 0 down
// to 0 before it next reaches zero (see statePlaying's own doc comment).
// This means this routine, not GameplayTick, is what actually runs on
// nearly every frame of real gameplay -- and it happens to call two
// callees this session already ported for GameplayTick's own sake:
// `$5270` (blitSpriteAttributeTable, count $78) and `$738C`
// (scrollSpeedAndDrawGauge, which draws the speed gauge). Wiring this up
// is therefore also what makes the speed/fuel gauges update essentially
// every frame instead of once every ~256, which was the honest state
// this port was in before this pass.
//
// `$5501` (a large, per-stage scroll/lane dispatcher -- only its stage
// 0/1 entry is ported so far, see updateRoadAndScore) still has 5 of its
// 6 stage-specific entries and 4 of its own callees as TODO. `$6CA1`
// (acceleration/collision) is now ported, see accelerateCarSpeed. The
// tail-jump to $528F is now ported too, see scrollTrafficSpritesAndWrap
// -- an earlier, shallower pass at this file mischaracterized it as
// "no VRAM writes found," which undersold it: it's the actual
// continuous-scroll mechanism for the decorative traffic sprites
// initTrafficSpriteSlots sets up, and since updateRoadAndScore's own
// abort condition is confirmed (see its own doc comment) to never fire
// in practice, this runs essentially every frame of real gameplay.
func (c *Console) advanceCarBehavior() {
	c.blitSpriteAttributeTable(0x78) // $527D: CALL $5270
	if c.updateSteeringAndCarState() {
		return // $5280: CALL $6A70 -- see its own doc comment for the bool's meaning
	}
	c.accelerateCarSpeed()      // $5283: CALL $6CA1
	c.scrollSpeedAndDrawGauge() // $5286: CALL $738C
	if c.updateRoadAndScore() { // $5289: CALL $5501 -- see its own doc comment for the bool's meaning
		return
	}
	c.scrollTrafficSpritesAndWrap() // $528C: JP $528F -- see its own doc comment
}

// scrollTrafficSpritesAndWrap is ported from $528F-$52B7
// (advanceCarBehavior's own $528C tail-jump target): walks the 11
// 2-sprite traffic-sprite pairs initTrafficSpriteSlots sets up ($E11A
// through $E171, 4 bytes/sprite, 2 sprites/pair -- confirmed by
// tracing the walk's own address arithmetic, not assumed from the
// range alone), decrementing each pair's shared Y position by 6 every
// time this runs. Since this runs essentially every frame of real
// gameplay (see advanceCarBehavior's own doc comment), this is the
// actual mechanism that makes the decorative traffic sprites visibly
// scroll down the screen -- the missing piece behind their "frozen
// clutter" appearance this project had already documented and, before
// this, not found.
//
// Once a pair's Y falls within [$C0,$E0) (near the bottom of the
// screen), its decrement is skipped for that pass -- but the walk
// itself still needs to advance by 8 bytes that time (matching the
// decrement branch), landing on the *next* pair's first sprite, not the
// current pair's second sprite: $52B1-$52B3 (`LD A,04h; CALL $403B`)
// does exactly that extra +4 in the original.
//
// CORRECTED: an earlier pass at this port dropped that extra advance,
// reasoning (wrongly) that re-checking the stuck pair's own second
// sprite on the next iteration was harmless since both sprites of a
// pair share one Y value. It isn't harmless: every "remaining" tick
// after the first stuck (non-last) pair ends up checking and writing 4
// bytes off from where the original does, so once any pair other than
// the very last gets stuck, every subsequent pair that frame is
// misaligned -- and since alignment is never reset between frames, the
// corruption persists and compounds. This was reported from real
// gameplay as "3 cars stuck, sprites never move" -- the affected pairs
// weren't even necessarily the ones whose own Y had reached the stuck
// band; several froze at Y values well outside [$C0,$E0) purely because
// the walk had drifted off-address by the time it reached them.
//
// On the *last* of the 11 iterations, landing in the near-bottom band
// instead increments $E001 (the same "phase" byte this port already
// models via advanceAttractPhase) -- ported literally even though it's
// a surprising thing for gameplay code to touch, since it's an exact,
// unambiguous translation of a real `JP $416A`.
//
// What resets a pair's Y back to the top once it actually reaches the
// wrap band is still NOT resolved by this function (it only ever
// decrements, or skips decrementing, never increments) -- if that reset
// exists at all, it's presumably among $70A8/$70D9's own still-unported
// pieces (positionTrafficSprite, already ported, does write fresh
// positions into the first 2 of these 11 pairs' own addresses when a
// real traffic object spawns there -- see trafficSpriteEntryAddr -- but
// nothing resets the other 9). Left as an open question rather than
// guessed at; with the alignment bug fixed, whichever pairs' Y
// genuinely reaches the wrap band should now correctly (if
// unglamorously) freeze there alone, matching what the ROM's own
// control flow actually specifies, rather than dragging other pairs
// down with them.
func (c *Console) scrollTrafficSpritesAndWrap() {
	addr := uint16(0xE116)
	remaining := 11
	for {
		addr += 4 // $5294-$5297
		y := c.readRAM(addr)
		if y-0xC0 < 0x20 { // $5299-$529D
			if remaining == 1 { // $52AB-$52AE
				c.advanceAttractPhase()
				return
			}
			addr += 4 // $52B1-$52B3: LD A,04h; CALL $403B -- skip to the pair's second sprite, same as the decrement branch below does, just without writing anything. Missing in an earlier pass at this port: every pair after the first "stuck" one then got checked/written 4 bytes off from where it should have been, corrupting the rest of that frame's pass and (compounding every subsequent frame) freezing sprites whose own Y was never actually in the "stuck" band -- exactly the "3 cars stuck" symptom a user reported from real gameplay, not a rare edge case.
		} else {
			newY := y - 6 // $529F-$52A2
			c.writeRAM(addr, newY)
			addr += 4              // $52A3-$52A6
			c.writeRAM(addr, newY) // $52A7
		}
		remaining--
		if remaining == 0 { // $52A8: DJNZ
			return
		}
	}
}

// carRowPosition/setCarRowPosition read and write a 16-bit fixed-point car
// row position at $E04B (low/fraction byte) and $E04C (high/integer byte)
// -- confirmed by $6CA1/$6C73's own "LD HL,(E04Bh)" 16-bit load, and
// mirroring the already-confirmed $E04D(fraction)/$E04E(integer) column
// pair (see applySignedPositionOffset) exactly: same split, same byte
// order, just the other screen axis. $E04C alone is also read as a plain
// 8-bit "row" elsewhere (drawFuelEmptyMessage, armPlayerCarSprites) --
// consistent with it being this pair's own integer part, the one that
// actually matters for on-screen position, the same way $E04E doubles as
// both the column pair's integer byte and a plain pixel column.
func (c *Console) carRowPosition() int16 {
	return int16(uint16(c.RAM[0x04B]) | uint16(c.RAM[0x04C])<<8)
}

func (c *Console) setCarRowPosition(pos int16) {
	c.RAM[0x04B] = byte(pos)
	c.RAM[0x04C] = byte(uint16(pos) >> 8)
}

// speedInAccelerationBand is ported from $6C93-$6CA0: reports whether
// `speed` falls in one of two "active" bands ($18-$4F, $7E-$FF) as
// opposed to the two "inactive" bands ($00-$17, $50-$7D) -- gates whether
// accelerateCarSpeed/scrollRoadBackward advance the car's row position
// each call they run. Why these specific alternating bands matter isn't
// confirmed.
func speedInAccelerationBand(speed byte) bool {
	switch {
	case speed < 0x18:
		return false
	case speed < 0x50:
		return true
	case speed < 0x7E:
		return false
	default:
		return true
	}
}

// carBehaviorManagesOwnSpeed is ported from $6C89-$6C92: reports whether
// $E049 is one of the three car-behavior states (1: countdown; 2:
// fuel-empty; 7: lane-change, see updateSteeringAndCarState) that already
// manage their own speed changes elsewhere, so accelerateCarSpeed and
// decelerateCarSpeed should leave $E04F alone.
func (c *Console) carBehaviorManagesOwnSpeed() bool {
	switch c.RAM[0x049] {
	case 1, 2, 7:
		return true
	default:
		return false
	}
}

// scrollRoadBackward is ported from $6C73-$6C87: the shared tail of
// decelerateCarSpeed ($6C67) and updateSteeringAndCarState's state 2
// "still has fuel" branch (which jumps directly here, skipping
// decelerateCarSpeed's own gate/decrement above). Clears $E038 (an
// "accelerating" flag accelerateCarSpeed's own path also sets when it
// runs), then -- unless the car's row position has already reached $F8 --
// decrements it by $48 in the same alternating speed bands
// speedInAccelerationBand selects, mirroring accelerateCarSpeed's own
// increment in the opposite direction. Always ends by re-arming the
// player's sprite pair, since $E04C (the row byte) just changed.
func (c *Console) scrollRoadBackward() {
	c.RAM[0x038] = 0          // $6C73-$6C74
	if c.RAM[0x04C] >= 0xF8 { // $6C7A-$6C7D: LD A,H; CP $F8; JP NC,$6E1D
		c.armPlayerCarSprites()
		return
	}
	if !speedInAccelerationBand(c.RAM[0x04F]) { // $6C80-$6C83: CALL $6C93; RET NC
		return
	}
	c.setCarRowPosition(c.carRowPosition() - 0x48) // $6C84-$6CCB (shared with accelerateCarSpeed's own $6CCA)
	c.armPlayerCarSprites()                        // $6CCE: JP $6E1D
}

// decelerateCarSpeed is ported from $6C67-$6C72 plus the shared
// scrollRoadBackward tail: while $E049 doesn't already manage its own
// speed (carBehaviorManagesOwnSpeed) and speed is nonzero, decrements it
// by 1 and scrolls the road backward to match.
func (c *Console) decelerateCarSpeed() {
	if c.carBehaviorManagesOwnSpeed() { // $6C67-$6C6A
		return
	}
	if c.RAM[0x04F] == 0 { // $6C6E-$6C70
		return
	}
	c.RAM[0x04F]-- // $6C71-$6C72
	c.scrollRoadBackward()
}

// accelerateCarSpeed is ported from $6CA1-$6CCE: GameplayTick's own $5248
// dispatch, also called directly by advanceCarBehavior ($5283). While
// $E049 doesn't already manage its own speed (carBehaviorManagesOwnSpeed):
// if the accelerate input isn't held ($E009 bit 4, a level-triggered
// state, not edge-triggered), defers entirely to decelerateCarSpeed.
// Otherwise increments speed (capped at $D7, the same cap
// fuelPickupTick's own refill logic uses), sets $E038=1, and -- unless the
// row position has already reached $9A -- advances it by $48 in the same
// alternating speed bands scrollRoadBackward checks, then re-arms the
// player's sprite pair.
func (c *Console) accelerateCarSpeed() {
	if c.carBehaviorManagesOwnSpeed() { // $6CA1-$6CA4
		return
	}
	if c.RAM[0x009]&0x10 == 0 { // $6CA5-$6CAA
		c.decelerateCarSpeed()
		return
	}
	if c.RAM[0x04F] >= 0xD7 { // $6CAF-$6CB2
		return
	}
	c.RAM[0x04F]++            // $6CB3-$6CB4
	c.RAM[0x038] = 1          // $6CB5-$6CB7
	if c.RAM[0x04C] >= 0x9A { // $6CBD-$6CC0: LD A,H; CP $9A; JP NC,$6E1D
		c.armPlayerCarSprites()
		return
	}
	if !speedInAccelerationBand(c.RAM[0x04F]) { // $6CC3-$6CC6: CALL $6C93; RET NC
		return
	}
	c.setCarRowPosition(c.carRowPosition() + 0x48) // $6CC7-$6CCB
	c.armPlayerCarSprites()                        // $6CCE: JP $6E1D
}

// carInitTable49 is the 11-byte ROM table at $5219, copied to
// $E049-$E053 by initPlayerCarSpritesAndFuel.
var carInitTable49 = [11]byte{0x00, 0x00, 0x00, 0x80, 0x00, 0x52, 0x00, 0x00, 0x00, 0x00, 0x00}

// carInitTable10E is the 12-byte ROM table at $5185, copied to
// $E10E-$E119 by initPlayerCarSpritesAndFuel -- 3 sprite-attribute-table
// entries' worth (Y, X, pattern, color each, see blitSpriteAttributeTable):
// entry 0 ($E10E, hidden at boot -- Y=$E0 is off the 192-row display) and
// the player's own 2-sprite pair ($E112/$E116, Y=X=$80/$52, matching
// carInitTable49's own $E04C/$E04E values exactly -- confirming these two
// entries really are the player's car -- colors $01/$06 (black, dark
// red)).
var carInitTable10E = [12]byte{0xE0, 0xD0, 0x6C, 0x06, 0x80, 0x52, 0x00, 0x01, 0x80, 0x52, 0x00, 0x06}

// resetCarPositionTables is ported from $51FF-$5218: copies carInitTable49
// into $E049-$E053 (the car's own behavior state and row/column) and
// carInitTable10E into $E10E-$E119 (3 sprite-attribute-table entries,
// including the player's own 2-sprite pair). A real, separate ROM
// subroutine, confirmed by two independent call sites -- $504C's own
// $51FF call (below, part of initPlayerCarSpritesAndFuel) and $51C5's
// (see recoverFromNonFatalCrash) -- not a synthetic Go-side factoring.
func (c *Console) resetCarPositionTables() {
	copy(c.RAM[0x049:0x054], carInitTable49[:])
	copy(c.RAM[0x10E:0x11A], carInitTable10E[:])
}

// clearObjectSlotsAndStaging is ported from $51E5-$51F2: zero-fills the
// 69-byte range $E0C1-$E105, covering both real $70A8 traffic-object slots
// and both level-stream staging slots. A real, separate ROM subroutine,
// confirmed by two independent call sites -- initPlayerCarSpritesAndFuel's
// own $5059 call and recoverFromNonFatalCrash's $51E5 call -- not a
// synthetic Go-side factoring, the same standard resetCarPositionTables
// above already follows.
func (c *Console) clearObjectSlotsAndStaging() {
	for i := uint16(0); i <= 0x44; i++ {
		c.writeRAM(0xE0C1+i, 0)
	}
}

// initPlayerCarSpritesAndFuel is ported from $504C-$5099, $50A7-$50B9,
// $5068-$507E, and $5081-$5086 -- the specific parts of the much larger
// $504C gameplay-init routine confirmed to matter for the player's car
// sprites, starting fuel, the track-boundary lookup table
// checkOffTrackCollision reads, the road-strip read pointer
// scrollAndDrawRoadStrip advances, the level-data stream's own initial
// position and first meta-tile stage (see advanceLevelStream), the road
// "feature" injection stream armRoadFeatureStream primes (found while
// investigating a "road never scrolls" report -- see
// docs/ANALYSIS.md), and $E0B2 (below).
//
// CORRECTED: an earlier pass at this port skipped $504C's own two leading
// zero-fills -- the self-referential 847-byte fill covering $E048-$E396
// ($504C-$5057) and the CALL $51E5 fill of $E0C1-$E105 (now
// clearObjectSlotsAndStaging) -- reasoning that every byte either range
// touches is either already zero (from clearRaceRAMBlock, run earlier in
// the same state transition) or gets unconditionally overwritten later in
// this same function. That reasoning only holds for the fresh-race-start
// call site (stateGetReadyWait phase 3), which always runs after
// clearRaceRAMBlock. It's false for the *other* confirmed call site --
// stateResultsPhase2's own phase 4, reached on every stage-to-stage
// transition within the same race -- which calls this function directly,
// with no clearRaceRAMBlock in between. $E078/$E079 (the 16-bit distance
// counter checkDistanceMilestone compares against $0F12 to trigger the
// finish sequence) falls inside the skipped $E048-$E396 range and has no
// other reset anywhere in this port, so it survived unchanged from
// whatever value it held the instant the *previous* stage finished --
// already past $0F12. Found from a user report: starting stage 3 via
// cmd/play's level-select cheat, finishing it normally, then having stage
// 4 instantly re-trigger the finish sequence on its very first frame, and
// every stage after that doing the same. Reproduced headlessly (calling
// this function twice with $043 bumped in between, no clearRaceRAMBlock,
// matching the real transition) before fixing: the distance counter did
// carry over and did immediately re-trigger checkDistanceMilestone.
// Fixed by porting both zero-fills for real instead of trying to prove
// them redundant -- removes the whole class of "which bytes actually get
// overwritten before the *next* call" bug, not just this one instance.
//
// $5068-$507B sets $E0B2 to one of two fixed ROM addresses ($50BE, a
// single $FF byte, or $50DC) depending on the lap counter ($E042) and
// current stage ($E043==6) -- consumed by the still-unported $54AF
// (dereferences it against $E079), so the two addresses themselves
// aren't meaningful to this port yet; stored as literal 16-bit values
// for whenever $54AF is traced, not dereferenced by anything here.
//
// Copies carInitTable49 into $E049-$E053 (the car's row/column position
// and steering/sprite-pattern-selector state, reset to their per-race
// defaults) and carInitTable10E into $E10E-$E119. The original also
// CALLs $6E1D (armPlayerCarSprites) between these two copies, but every
// byte it writes falls inside the second copy's own destination range
// and is immediately overwritten by it, so that call has no observable
// effect here and is skipped. Also sets fuel ($E083) to $D0, near its
// $D7 cap (see fuelPickupTick/drawFuelGaugeBar) -- confirmed from
// $504C's own body immediately after its $51FF call, $5089-$508D: LD
// A,$D0; LD (E083h),A -- fills $E098-$E0AF (24 bytes) with a flat $20,
// the per-row-band track-boundary table checkOffTrackCollision indexes
// into (real per-row boundary values are presumably written elsewhere by
// track-layout code this port hasn't traced; this flat default is what
// $504C itself sets before anything else can override it) -- and sets
// $E086=4 (a transient default, immediately superseded by
// injectRoadFeature's own first reload once gameplay starts -- see its
// doc comment) and $E046=$E186 ($50B6-$50B9), the starting position of
// scrollAndDrawRoadStrip's own read pointer.
// UNCONFIRMED DEVIATION: also clears $E00D (gameOverFlag), which is
// *outside* $504C's own confirmed $E048-$E396 zero-fill and has no other
// write anywhere in the ROM (confirmed exhaustively -- a raw byte-level
// scan of the whole 16KB ROM for the operand pair `0D E0`, the same
// technique that found $E0AD's real write site elsewhere in this file,
// turns up exactly the 5 references already known: 2 reads and 3
// writes, none of them a clear reachable from this call site). Found
// from a second user report on the same $504C investigation above: with
// that fix alone, stage 2 started but "finished" again almost instantly
// with the car never moving, and headless tracing showed this cascading
// stage-by-stage all the way to the wrap, entirely without driving --
// stateResultsPhase2's own phase 4 sets gameOverFlag to the just-advanced
// lap counter (a real, confirmed write, see its own doc comment) and nothing
// in the traced, per-lap "resume driving" path (stateCrashSequence's
// phase 5, taken here since $E002 bit 6 -- "game in progress" -- stays
// set for the whole race) ever clears it before statePlaying's own
// GameplayTick-gated check reads it again -- which happens almost
// immediately, since (also confirmed, see scrollTrafficSpritesAndWrap)
// countdownGetReadyMessage stamps all 11 decorative traffic-sprite Y
// positions to $C8 every frame during stateCrashSequence's own phase-0
// wait, and $C8 already sits inside scrollTrafficSpritesAndWrap's own
// "reached the bottom" band -- so the very first time it runs in the new
// stage, it immediately advances $E001, and statePlaying's very next
// GameplayTick call reads the still-stale gameOverFlag and bounces
// straight back into the results sequence. Whatever avoids this on real
// hardware isn't visible in the disassembly alone; clearing it here (the
// one place in this port already responsible for resetting per-stage
// state) is a deliberate, effect-driven fix for a reproducible,
// game-breaking bug, not a guess -- flagged as unconfirmed against the
// literal ROM rather than presented as traced.
func (c *Console) initPlayerCarSpritesAndFuel() {
	for i := 0x048; i <= 0x396; i++ { // $504C-$5057: LD HL,E048h; LD DE,E049h; LD BC,034Eh; LD(HL),0; LDIR
		c.RAM[i] = 0
	}
	c.clearObjectSlotsAndStaging() // $5059: CALL L51E5
	*c.gameOverFlag() = 0          // see UNCONFIRMED DEVIATION above

	c.RAM[0x0B2], c.RAM[0x0B3] = 0xBE, 0x50 // $5068-$507B: default -- see doc comment above
	if c.RAM[0x042] != 0 || c.RAM[0x043] == 6 {
		c.RAM[0x0B2], c.RAM[0x0B3] = 0xDC, 0x50
	}
	c.armRoadFeatureStream() // $507E: CALL $50BF -- see its own doc comment
	c.RAM[0x07D] = 8         // $5081-$5083 (role not confirmed -- consumed by the still-unported $590E/$56F7)
	c.resetCarPositionTables()
	c.RAM[0x083] = 0xD0
	for i := 0; i <= 0x17; i++ { // $505C-$5066: LD HL,E098h; LD DE,E099h; LD(HL),20h; LD C,17h; LDIR
		c.RAM[0x098+i] = 0x20
	}
	c.RAM[0x089] = stageRoadRowFillTable[c.RAM[0x043]-1] // $508E-$5099 -- fillRoadRowDefault's own fill byte, found missing while chasing a "blank margins on freshly-scrolled rows" report: this was read every qualifying frame but never written anywhere in this port, so it stayed at cold-boot 0 (an invisible tile) instead of the real per-stage default

	stage := c.RAM[0x043] - 1                // $509C-$50A6
	streamStart := 0x537D + uint16(stage)*16 // $50A7-$50AB
	c.RAM[0x0BD] = byte(streamStart)
	c.RAM[0x0BE] = byte(streamStart >> 8)
	c.advanceLevelStream() // $50AE: CALL $7020 -- see its own doc comment
	c.RAM[0x086] = 4       // $50B1-$50B3 (role not confirmed)
	c.RAM[0x046] = 0x86    // $50B6-$50B9: LD DE,E186h; LD(E046h),DE -- the road-strip read pointer scrollAndDrawRoadStrip advances
	c.RAM[0x047] = 0xE1
}

// recoverFromNonFatalCrash is ported from $51C5-$51D7 -- reached only via
// stateCrashTrigger's own $432B, which this port previously left as an
// untraced `CALL $51C5` TODO. Runs once a crashed car still has fuel
// remaining, right before stateCrashTrigger resumes normal driving.
//
// Resets the car's own behavior state, row/column, and sprite-attribute
// entries to their per-race defaults (resetCarPositionTables, $51FF --
// the same subroutine initPlayerCarSpritesAndFuel calls at race start).
// Then overwrites the column ($E04E) from $E0AD ($51F3: doubled, masked
// to an 8-pixel tile boundary, offset by $20).
//
// CORRECTED: an earlier pass here found no literal "LD (E0ADh),..."
// anywhere in the ROM (confirmed again by scanning the raw ROM bytes
// directly for the operand pair `AD E0`, not just the disassembled
// listing -- only the one read site at $51F3 turned up) and concluded
// $E0AD's own write site didn't exist yet in the decoded ROM, making
// this always resolve to a fixed column ($20, its cold-start default).
// That conclusion was stale: $E0AD is the 22nd byte of the 23-byte
// $E098-$E0AE track-boundary table advanceBoundaryHistoryTable (already
// ported, called every frame via injectRoadCurve) shifts one position
// per call -- so it *is* written, continuously, just via that shift
// loop's own computed addressing (`c.RAM[addr+1] = c.RAM[addr]`) rather
// than a literal immediate operand a byte-pattern search could find.
// Verified end-to-end: drove real sessions of varying length and
// steering before forcing a non-fatal crash, and the recovered column
// genuinely differs run to run (confirmed $60/$50/$40/$48 across four
// different drive histories, not stuck at $20) -- it does encode real
// recent-curve history, exactly as the read site's own context suggests,
// not a dead fallback.
//
// Re-arms
// the player's own sprite pair (armPlayerCarSprites, $6E1D) to reflect
// that new column -- unlike initPlayerCarSpritesAndFuel's own call to
// the same function, this one is *not* redundant, since $E04E just
// changed after resetCarPositionTables already ran.
//
// Then hides all 11 decorative traffic-sprite pairs by flat-filling
// $E11A-$E14A (49 bytes, $51D8-$51E3: LD(HL),C8h then LDIR, the same
// "seed one byte, LDIR-propagate" idiom initPlayerCarSpritesAndFuel's own
// $E098 fill already uses) with $C8 -- the same off-screen Y value
// blitSpriteAttributeTable's own doc comment identifies -- and clears the
// object-slot/staging area by flat-filling $E0C1-$E105 (69 bytes,
// $51E5-$51F0) with 0, covering both real $70A8 slots and both staging
// slots. Zeroes $E0B9 (role not confirmed), then restarts the level
// stream from its current position (advanceLevelStream, $51D5: JP $7020
// -- a genuine tail-jump in the original, reached here via CALL so it
// still returns normally to stateCrashTrigger once advanceLevelStream's
// own RET fires).
//
// Found while diagnosing a session-discovered soft-lock: without this,
// $E049 (car behavior state, left at 6 by drawFuelEmptyMessage's own
// crash-recovery path) never gets reset back to 0, so the game oscillates
// between states 5 and 6 forever instead of resuming normal driving --
// see docs/ANALYSIS.md's "Crash recovery gets stuck" section.
func (c *Console) recoverFromNonFatalCrash() {
	c.resetCarPositionTables()                   // $51C5: CALL $51FF
	c.RAM[0x04E] = (c.RAM[0x0AD]<<1)&0xF8 + 0x20 // $51C8: CALL $51F3
	c.armPlayerCarSprites()                      // $51CB: CALL $6E1D

	for i := uint16(0); i <= 0x30; i++ { // $51D8-$51E3: LD(HL),C8h; LDIR, BC=$30
		c.writeRAM(0xE11A+i, 0xC8)
	}
	c.clearObjectSlotsAndStaging() // $51E5: CALL L51E5
	c.RAM[0x0B9] = 0               // $51D1-$51D4
	c.advanceLevelStream()         // $51D5: JP $7020
}

// roadFeatureEntry is one entry of the per-stage road-feature table
// injectRoadFeature reads from (the ROM's own $E075-pointed-to array):
// hold is how many consecutive injectRoadFeature calls (see its own doc
// comment for the exact cadence) keep re-injecting from this entry
// before the level-stream cursor ($E073) advances to the next byte;
// length is how many bytes get copied per injection; data is exactly
// hold*length bytes, consumed length-at-a-time, advancing through it
// linearly (never wrapping within one entry). Confirmed for real, not
// guessed: decoding hold*length bytes from each of the 8 entries in the
// ROM lands EXACTLY on the next entry's own 2-byte header with zero gap
// or overlap, for all 8 entries in a row.
type roadFeatureEntry struct {
	hold, length byte
	data         []byte
}

// roadFeatureTableStage145 is the 8-entry table at ROM $7970 (pointed to
// by the 8 2-byte pointers at $7960), used by stages 1, 4, and 5 -- all
// three share the identical $E073-stream-start ($78E5) and $E075-table
// ($7960) values in the per-stage seed table at $5161, confirmed by
// reading that table directly: entries 0, 3, and 4 (stages 1, 4, 5)
// have identical field-2/field-3 values, differing only in field 1 (a
// separate pointer, copied into $E07A/$E07B, consumed by the
// still-unported $56F7). Stages 2/3 (sharing a different table at
// $7AAA/$7BAA/$7ACD) and stage 6 (its own, $7F6B/$7ED1) are now
// extracted too -- see roadFeatureTableStage23/roadFeatureTableStage6
// and the roadFeatureSets selector below.
//
// Every byte in every entry's data is a name-table character code in the
// $80+ range this session's stage-terrain work (loadStageTerrainScenery,
// see docs/ANALYSIS.md) already loads real pattern/color data for --
// confirming this table really is what feeds new roadside terrain tiles
// into the scrolling view over time, matching a real-hardware-derived
// hint from the user describing exactly this mechanism ("reads a few
// bytes ahead... injects them onto the top row... just before it shifts
// down").
var roadFeatureTableStage145 = [8]roadFeatureEntry{
	{7, 7, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x87, 0x80, 0x87, 0x8C, 0x8B, 0x8A, 0x80, 0x87, 0x80, 0x87, 0x85, 0x86, 0x89, 0xA0, 0xA0, 0x80, 0x87, 0x83, 0x84, 0x89, 0xA0, 0xA0, 0x80, 0x87, 0x81, 0x82, 0x88, 0x80, 0x87, 0x80, 0x87, 0xA0, 0xA0, 0xA0, 0x80, 0x87, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x87}},
	{7, 6, []byte{0x08, 0x96, 0x97, 0x98, 0x99, 0x08, 0x08, 0x93, 0x94, 0x95, 0x93, 0x08, 0x08, 0x8F, 0x90, 0x91, 0x92, 0x08, 0x08, 0x96, 0x97, 0x98, 0x99, 0x08, 0x08, 0x93, 0x94, 0x95, 0x93, 0x08, 0x08, 0x8F, 0x90, 0x91, 0x92, 0x08, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD}},
	{8, 8, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x87, 0x80, 0x87, 0xA0, 0xBB, 0xBC, 0xA0, 0x80, 0x87, 0x80, 0x87, 0xA0, 0xB3, 0xB4, 0xA0, 0x80, 0x87, 0xA0, 0xA0, 0xA0, 0xB1, 0xB2, 0xA0, 0x80, 0x87, 0xA0, 0xA0, 0x8C, 0x8B, 0x8B, 0x8A, 0x80, 0x87, 0xA0, 0xA0, 0x9D, 0x9E, 0x9F, 0x89, 0x80, 0x87, 0x80, 0x87, 0x9A, 0x9B, 0x9C, 0x88, 0x80, 0x87, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x87}},
	{8, 3, []byte{0x8D, 0x8E, 0xA0, 0xA0, 0x8D, 0x8E, 0xA0, 0xA0, 0xA0, 0x8D, 0x8E, 0xA0, 0xA0, 0x8D, 0x8E, 0xA0, 0x8D, 0x8E, 0x8D, 0x8E, 0xA0, 0xA0, 0x8D, 0x8E}},
	{8, 3, []byte{0x8D, 0x8E, 0xA0, 0xA0, 0x8D, 0x8E, 0xA0, 0xA0, 0xA0, 0x8D, 0x8E, 0xA0, 0xA0, 0x8D, 0x8E, 0xA0, 0x8D, 0x8E, 0x8D, 0x8E, 0xA0, 0xA0, 0x8D, 0x8E}}, // identical to entry 3 -- both pointers really are $7A11 in ROM
	{8, 6, []byte{0xA0, 0xA0, 0xA0, 0xA0, 0xAA, 0xAB, 0xAC, 0xAD, 0xAE, 0xAF, 0xB0, 0xA0, 0xA0, 0xA0, 0xA0, 0xA0, 0xA0, 0xA0, 0xA0, 0xA0, 0xAA, 0xAB, 0xA0, 0xA0, 0xA0, 0xA0, 0xAA, 0xAB, 0x8D, 0x8E, 0xB7, 0xB8, 0xBA, 0xA0, 0xA0, 0xA0, 0xB5, 0xB6, 0xB9, 0xA0, 0x8D, 0x8E, 0xA0, 0xA0, 0xA0, 0xA0, 0x8D, 0x8E}},
	{6, 6, []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x87, 0x80, 0x8C, 0x8B, 0x8A, 0x80, 0x87, 0xA7, 0xA8, 0xA9, 0x89, 0x80, 0x87, 0xA4, 0xA5, 0xA6, 0x88, 0x80, 0x87, 0x80, 0x87, 0xA0, 0xA0, 0x80, 0x87, 0x80, 0x80, 0x80, 0x80, 0x80, 0x87}},
	{7, 4, []byte{0xA0, 0xAA, 0xAB, 0xA0, 0xA0, 0xA0, 0xAA, 0xAB, 0xA1, 0xA2, 0xA0, 0xA0, 0xA0, 0xA0, 0xAA, 0xAB, 0xA0, 0xA1, 0xA2, 0xA0, 0xA0, 0xA0, 0xAA, 0xAB, 0xA0, 0xAA, 0xAB, 0xA0}},
}

// roadFeatureStreamStage145 is the ROM byte stream at $78E5-$78F0 (stages
// 1/4/5), read one byte at a time by injectRoadFeature via the $E073
// cursor. Each byte's low nibble selects one of roadFeatureTableStage145's
// 8 entries; its high nibble selects where within the 22-byte road-strip
// staging row ($E058) the entry's data lands.
//
// CORRECTED: this was 12 bytes, on the reading that the $FF at $78F1
// terminates the stream. It doesn't -- $FF is a *gap* marker meaning "no
// feature at this step" ($58C8-$58CA skips the entry reload and sets the
// hold counter to 8; $58EF-$58F1 then returns without injecting), and
// $E073 advances unconditionally either way ($58C1-$58C5). The stream
// really is 123 bytes with 11 gap markers scattered through it, running
// from $78E5 to exactly $795F -- the byte before this group's own entry
// pointer table at $7960, the same tight-packing boundary that confirms
// the rest of this data. Stopping at the first $FF meant features were
// injected for only about the first ten seconds of a race and then never
// again, which is what a user reported as stage 2 showing no palm trees.
//
// This port stops injecting once the cursor passes the extracted run.
// Real hardware has no bound and would keep walking into whatever
// follows -- for stages 2/3 and 6 that is adjacent, unrelated data whose
// low nibbles would over-index the entry pointer table. Each stream here
// is extracted exactly as far as that stays self-consistent (every byte
// either $FF or selecting an in-range entry), which is a deliberate,
// documented bound rather than a claim about what the ROM does past it.
var roadFeatureStreamStage145 = assets.RoadFeatureStreamStage145 // extracted from $78E5

// roadFeatureStreamBase145 is the ROM address roadFeatureStreamStage145's
// first byte occupies -- $E073 is stored as this same literal address
// (not a 0-based Go index) to match how the original code compares it,
// so injectRoadFeature converts with a subtraction, the same convention
// trackRecordData/stageMetaTileIDs already use.
const roadFeatureStreamBase145 uint16 = 0x78E5

// roadFeatureStreamStage23/roadFeatureTableStage23 are stages 2 and 3's
// own equivalents (stream at $7BAA, entry table at $7ACD -- the pair
// roadFeatureStageRecords already carried for both those stages, which
// share one identical record). Six entries, not eight: the stream's own
// low nibbles only ever select 0-5, and the six records chain
// end-to-end from $7ADB and finish at exactly $7BAA -- the stream's own
// first byte -- with no gap, the same tight-packing check that confirmed
// the stage 1/4/5 table. (The two bytes at $7AD9-$7ADA, between the six
// pointers and the first record, read as $0000; most likely an unused
// 7th pointer slot, but nothing selects it, so it isn't modelled.)
//
// The background byte running through this data is $C5 -- exactly
// stageRoadRowFillTable's own stage-2 entry, an independent
// cross-check that these bytes really are stage 2's roadside content.
var roadFeatureStreamStage23 = assets.RoadFeatureStreamStage23 // extracted from $7BAA

const roadFeatureStreamBase23 uint16 = 0x7BAA

var roadFeatureTableStage23 = [6]roadFeatureEntry{
	{8, 4, []byte{0x84, 0x85, 0x86, 0xC5, 0x82, 0x83, 0xC5, 0xC5, 0xC5, 0x84, 0x85, 0x86, 0xC5, 0x82, 0x83, 0xC5, 0xC5, 0x84, 0x85, 0x86, 0xC5, 0x82, 0x83, 0xC5, 0x84, 0x85, 0x86, 0xC5, 0x82, 0x83, 0xC5, 0xC5}},
	{7, 5, []byte{0xAE, 0xAE, 0xAE, 0xAE, 0xC5, 0xC5, 0xA0, 0x9F, 0x9E, 0xB0, 0xC5, 0xA9, 0xAA, 0x9D, 0xB0, 0xAB, 0xA8, 0xA7, 0x9D, 0xB0, 0xAB, 0xA6, 0xA7, 0x9D, 0xB0, 0xC5, 0xA4, 0xA5, 0x9C, 0xB0, 0xAE, 0xAE, 0xAE, 0xAE, 0xC5}},
	{7, 4, []byte{0xC5, 0xC5, 0xAC, 0xAD, 0xC5, 0xC5, 0xAC, 0xAD, 0xA0, 0x9F, 0x9F, 0x9E, 0x07, 0xA1, 0xA3, 0x9D, 0x07, 0xA1, 0xA2, 0x9D, 0x07, 0xA1, 0xA3, 0x9D, 0x07, 0xA1, 0xA2, 0x9C}},
	{8, 6, []byte{0x84, 0x85, 0x86, 0x84, 0x85, 0x86, 0x82, 0x83, 0xC5, 0x82, 0x83, 0xC5, 0x84, 0x85, 0x86, 0x84, 0x85, 0x86, 0x82, 0x83, 0xC5, 0x82, 0x83, 0xC5, 0xC5, 0x84, 0x85, 0x86, 0xC5, 0xC5, 0xC5, 0x82, 0x83, 0xC5, 0xC5, 0xC5, 0x84, 0x85, 0x86, 0xC5, 0xC5, 0xC5, 0x82, 0x83, 0xC5, 0xC5, 0xC5, 0xC5}},
	{8, 5, []byte{0xC5, 0xC5, 0x8A, 0x8C, 0x8E, 0xC5, 0xC5, 0x89, 0x8B, 0x8D, 0x87, 0x88, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0x94, 0x95, 0x8E, 0xC5, 0xC5, 0x92, 0x93, 0x8D, 0x94, 0x95, 0x8E, 0xC5, 0xC5, 0x92, 0x93, 0x8D, 0xC5, 0xC5}},
	{4, 3, []byte{0x8F, 0x90, 0x91, 0xC5, 0xC5, 0xC5, 0xC5, 0x80, 0x81, 0x80, 0x81, 0xC5}},
}

// roadFeatureStreamStage6/roadFeatureTableStage6 are stage 6's own pair
// (stream at $7F6B, entry table at $7ED1). Four entries -- the stream
// selects only 1 and 3, but 0-3 are what the table's own tight chain
// covers: the four records run from $7ED9 (exactly table+8, no gap
// before them either) and finish at exactly $7F6B, the stream's own
// first byte.
//
// Entry 2 ends with $2B $2F $2E $21 $2D $29, which under this ROM's own
// confirmed font mapping ($21-$3A = A-Z) reads "KONAMI" -- a roadside
// billboard, and about as unambiguous a confirmation that this data was
// extracted correctly as this table could offer.
var roadFeatureStreamStage6 = assets.RoadFeatureStreamStage6 // extracted from $7F6B

const roadFeatureStreamBase6 uint16 = 0x7F6B

var roadFeatureTableStage6 = [4]roadFeatureEntry{
	{8, 5, []byte{0x0F, 0x83, 0x84, 0x0F, 0x0F, 0x0F, 0x81, 0x82, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x83, 0x84, 0x83, 0x84, 0x0F, 0x81, 0x82, 0x81, 0x82, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x83, 0x84, 0x0F, 0x83, 0x84, 0x81, 0x82, 0x0F, 0x81, 0x82, 0x0F, 0x0F}},
	{5, 6, []byte{0x0F, 0x0F, 0x87, 0x88, 0x89, 0x0F, 0x0F, 0x0F, 0x85, 0x86, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x0F, 0x2B, 0x2F, 0x2E, 0x21, 0x2D, 0x29}},
	{7, 4, []byte{0x98, 0x97, 0x97, 0x96, 0x91, 0x92, 0x93, 0x95, 0x8D, 0x8E, 0x90, 0x95, 0x8D, 0x8E, 0x8F, 0x95, 0x8A, 0x8B, 0x8C, 0x94, 0x0F, 0x87, 0x88, 0x89, 0x0F, 0x85, 0x86, 0x0F}},
	{8, 5, []byte{0x0F, 0x87, 0x88, 0x89, 0x0F, 0x0F, 0x85, 0x86, 0x0F, 0x0F, 0x87, 0x88, 0x87, 0x88, 0x89, 0x85, 0x86, 0x85, 0x86, 0x0F, 0x0F, 0x87, 0x88, 0x89, 0x0F, 0x0F, 0x85, 0x86, 0x0F, 0x0F, 0x87, 0x88, 0x87, 0x88, 0x89, 0x85, 0x86, 0x85, 0x86, 0x0F}},
}

// roadFeatureSet bundles one stage's own injectRoadFeature inputs. The
// three distinct sets are shared exactly the way roadFeatureStageRecords'
// own streamBase/entryTableBase columns already say they are: stages 1,
// 4 and 5 share one ($78E5/$7960), stages 2 and 3 share another
// ($7BAA/$7ACD), and stage 6 has its own ($7F6B/$7ED1).
type roadFeatureSet struct {
	base    uint16
	stream  []byte
	entries []roadFeatureEntry
}

var roadFeatureSets = [6]roadFeatureSet{
	{roadFeatureStreamBase145, roadFeatureStreamStage145, roadFeatureTableStage145[:]}, // stage 1
	{roadFeatureStreamBase23, roadFeatureStreamStage23, roadFeatureTableStage23[:]},    // stage 2
	{roadFeatureStreamBase23, roadFeatureStreamStage23, roadFeatureTableStage23[:]},    // stage 3
	{roadFeatureStreamBase145, roadFeatureStreamStage145, roadFeatureTableStage145[:]}, // stage 4
	{roadFeatureStreamBase145, roadFeatureStreamStage145, roadFeatureTableStage145[:]}, // stage 5
	{roadFeatureStreamBase6, roadFeatureStreamStage6, roadFeatureTableStage6[:]},       // stage 6
}

// stage45SharedInitTable is the 12-byte ROM table at $5116, copied
// verbatim into $E08A-$E095 for stages 4 and 5 by armRoadFeatureStream's
// own $510A entry point -- three 2-byte pointers ($7E7C, $7E57, $7DC5)
// interleaved with two other 2-byte fields ($0101, $013F). Stage 5
// (armRoadFeatureStream's own $5122 entry) runs this exact same copy
// first, then overrides two of these fields with its own values -- see
// armRoadFeatureStream's own doc comment.
var stage45SharedInitTable = [12]byte{0x7C, 0x7E, 0x01, 0x01, 0x00, 0x00, 0x57, 0x7E, 0x3F, 0x01, 0xC5, 0x7D}

// stage2And4FeatureStream is the 96-byte (24x4-byte-chunk) ROM stream at
// $7DC5, terminated by a literal $FF at $7E25 (confirmed by scanning the
// real ROM bytes for it, not assumed from a round address) -- read 4
// bytes at a time into $E058, wrapping back to the start on the
// terminator. Used by updateRoadAndScore's own stage 2 handler ($5545,
// via $E08A) and shared with stage 4/5's own $E094 field (both
// initialized to this same $7DC5 address by armRoadFeatureStream), a
// distinct, simpler mechanism from injectRoadFeature's own
// hold/length/entry-table format -- this one has no per-entry structure
// at all, just a flat repeating byte stream. Content not independently
// interpreted beyond "some kind of road-strip tile data" (name-table
// character codes in roughly the $B0-$C7 range, plausible scenery/road
// tiles by the same reasoning as roadFeatureTableStage145's own content,
// but not cross-checked against a rendered screen the way that one was).
//
// CORRECTED: this array was originally transcribed with only 92 of the
// real 96 bytes, with the last 32 wrong (a copy/paste slip duplicating
// an earlier row instead of the real ROM content from offset 64
// onward). Stage 2's own consumption (4 bytes every OTHER qualifying
// frame, gated by updateRoadAndScore's own outer rate limiter) never
// happened to run enough calls in this port's own verification to reach
// the wrong tail; stage 5's own real-ROM verification (unconditional, 4
// bytes every call, no gating) did, at its 17th call, and a direct
// byte-for-byte re-dump against the ROM found and fixed this. A reminder
// that "verified for N calls" only covers the bytes N calls actually
// reach -- see docs/ANALYSIS.md.
var stage2And4FeatureStream = [96]byte{
	0xC6, 0xC6, 0xB9, 0xB1, 0xC6, 0xC6, 0xBF, 0xB6, 0xC6, 0xBB, 0xB2, 0xC5, 0xC2, 0xBF, 0xB6, 0xC5,
	0xC6, 0xBD, 0xB5, 0xC5, 0xC4, 0xBB, 0xB4, 0xC5, 0xC6, 0xC6, 0xBC, 0xC5, 0xC6, 0xC2, 0xBA, 0xB5,
	0xC2, 0xC6, 0xBB, 0xB4, 0xC6, 0xC3, 0xB8, 0xB3, 0xC6, 0xC4, 0xB7, 0xB2, 0xC6, 0xC6, 0xB9, 0xB1,
	0xC6, 0xC1, 0xBF, 0xB6, 0xC6, 0xBF, 0xB2, 0xC5, 0xC1, 0xC7, 0xB1, 0xC5, 0xC6, 0xBC, 0xB4, 0xC5,
	0xC4, 0xB9, 0xB3, 0xC5, 0xC1, 0xBF, 0xB2, 0xC5, 0xC6, 0xBE, 0xB1, 0xC5, 0xC6, 0xBD, 0xB4, 0xC5,
	0xC6, 0xC2, 0xBC, 0xC5, 0xC6, 0xC6, 0xB9, 0xB5, 0xC6, 0xC3, 0xB8, 0xB4, 0xC6, 0xC1, 0xB9, 0xB3,
}

var stage2And4FeatureStreamBase uint16 = 0x7DC5

// roadFeatureStageRecords is the 36-byte (6 stages x 6 bytes) ROM table
// at $5161, read by armRoadFeatureStream's own $5132 half. Each stage's
// record is 3 fields: field1 (a ROM pointer -- $E071/$E072 end up at
// field1+2, and field1FirstWord is the confirmed 2 bytes stored AT
// field1 itself, low byte -> $E07A, high byte -> $E07B), streamBase
// ($E073/$E074, injectRoadFeature's own stream cursor), and
// entryTableBase ($E075/$E076, injectRoadFeature's own entry-table
// pointer). All 18 values independently confirmed by direct-calling
// $50BF against the real ROM once for each stage 1-6 and reading back
// every register it touches -- not derived from the table's own raw
// bytes by hand, which is exactly the kind of arithmetic this project's
// conventions call for verifying rather than trusting a manual read of.
// Stages 2 and 3 share one identical record (confirmed: their own 6 ROM
// bytes are byte-for-byte the same).
var roadFeatureStageRecords = [6]struct {
	field1          uint16
	field1FirstWord uint16
	streamBase      uint16
	entryTableBase  uint16
}{
	{0x78C4, 0x1910, 0x78E5, 0x7960}, // stage 1
	{0x7AAA, 0x4910, 0x7BAA, 0x7ACD}, // stage 2
	{0x7AAA, 0x4910, 0x7BAA, 0x7ACD}, // stage 3
	{0x7E83, 0x2910, 0x78E5, 0x7960}, // stage 4
	{0x7E60, 0x3110, 0x78E5, 0x7960}, // stage 5
	{0x7EA8, 0x3110, 0x7F6B, 0x7ED1}, // stage 6
}

// armRoadFeatureStream is ported from $50BF/$5132 -- and, previously
// undersold as "a trivial one-instruction wrapper," $50BF's own full
// 4-instruction body: $5132 (below) THEN a 6-entry per-stage jump table
// at $50C9, only found while tracing updateRoadAndScore's stages 2-6,
// which read state ($E08A/$E090/$E092/$E07C/$E0B2, or, for stage 3, a
// whole different road-curve table) that only this second half ever
// initializes. Called unconditionally from initPlayerCarSpritesAndFuel
// ($507E) and again mid-race whenever injectRoadCurve's own phase stream
// runs out and wraps ($56FE: CALL L50BF, see injectRoadCurve's own doc
// comment) -- the exact same re-arm, not a separate mechanism.
//
// $5132 copies a 6-byte per-stage record from ROM ($5161, indexed by
// (stage-1)*6) into $E071-$E076 -- three 2-byte fields -- then reads 2
// bytes from the record's own first field (as a pointer) into
// $E07A/$E07B, advances $E071 past those 2 bytes, and sets a literal ROM
// address ($5998, already known as scrollTrafficSprite's own entry point
// -- role in this context not confirmed) into $E06F. $E073/$E075 come
// from the record's 2nd/3rd fields respectively -- $E075 is a pointer to
// a roadFeatureTableStage145-equivalent entry table, confirmed by
// injectRoadFeature's own $58D0 (`LD HL,(E075h)`), correcting an earlier
// doc comment here that guessed it was consumed by injectRoadCurve
// instead (that function never reads $E075 at all -- confirmed by an
// exhaustive search of the whole disassembly for that address, which
// turns up exactly the one site in injectRoadFeature).
//
// CORRECTED: an earlier pass at this port modeled $E07A/$E07B/$E071 with
// one hardcoded value shared across stages 1, 4, and 5, on the strength
// of a doc comment here claiming their own field-1 pointer targets'
// "first 2 bytes are identical" across those three stages. Directly
// reading the real ROM at all three targets ($78C4/$7E83/$7E60) during
// this session's stages-2-6 investigation disproved that: only the
// FIRST of the 2 bytes (which becomes $E07A) is identical ($10) -- the
// second ($E07B) genuinely differs per stage ($19/$29/$31), and $E071
// itself (each stage's own field-1 pointer, plus 2) obviously differs
// too. Since $E07B is a live countdown `refreshRoadCurvePhase`
// decrements and checks against zero, this meant stages 4 and 5 were
// reading the wrong phase-countdown seed (stage 1's, 25, instead of
// their own 41/49) and the wrong $E071 read cursor (stage 1's own table
// address) -- silently wrong road-curve timing on any stage past the
// first, never caught because this project's own testing hadn't reached
// stage 4/5 before now. Fixed by generalizing to the real per-stage
// $5161 record (roadFeatureStageRecords, all 6 entries independently
// confirmed by direct-calling $50BF against the real ROM for each stage
// and comparing every resulting register), the same rigor already
// applied throughout this file's other arithmetic-heavy translations.
//
// This port only has roadFeatureTableStage145's own ROM data extracted
// (stages 1, 4, 5 -- see its doc comment) -- stages 2/3 share one
// not-yet-extracted entry table of their own and stage 6 has a third --
// so this only sets up real $E073 (the injectRoadFeature stream cursor)
// for stages 1, 4, 5; for 2, 3, and 6, $E073 is deliberately left at 0
// (injectRoadFeature's own no-op sentinel) rather than reading
// not-yet-extracted ROM data, even though $5132 itself would compute
// real (if currently unusable) values for them too -- $E07A/$E07B/$E071/
// $E075 are still set correctly for every stage regardless, matching
// $5132's own unconditional behavior (harmless for 2/3/6 today, since
// injectRoadFeature's own $E073==0 guard means nothing ever reads them,
// but correct and ready for whenever their entry-table data gets
// extracted). $E086 is a transient default (see
// initPlayerCarSpritesAndFuel) genuinely overwritten by
// injectRoadFeature's own first reload, so it's not set here even though
// the original's own $50B1-$50B3 (unrelated call site) happens to run
// later in the same init sequence regardless of stage.
//
// The $50C9 jump table (confirmed via its own literal 12-byte pointer
// list) sets up state each of stages 2-6's own updateRoadAndScore
// handler reads. Stage 1 has no entry (a plain RET). Stage 2 arms
// $E08A at the start of stage2And4FeatureStream. Stage 3 arms $E090
// (its own $56CC/$56CE-driven stream, not yet extracted -- see
// docs/ANALYSIS.md), sets a flag at $E07C, and -- notably -- overrides
// $E06F/$E070 to a completely different road-curve-array base ($7C54)
// than the $5998 default injectRoadCurve otherwise uses; $E08A is also
// set to the byte value 1 here (not a pointer), but stage 3's own
// handler never reads $E08A, so this looks like a harmless side effect
// of the original reusing the same A register rather than something
// load-bearing. Stages 4 and 5 share one 12-byte copy
// (stage45SharedInitTable) into $E08A-$E095; stage 5 then overrides two
// of those fields with its own $E08A/$E092 values. Stage 6 arms $E090
// with its own stream base ($7FE4, not yet extracted).
func (c *Console) armRoadFeatureStream() {
	rec := roadFeatureStageRecords[c.RAM[0x043]-1]
	c.RAM[0x07A] = byte(rec.field1FirstWord) // $514D-$5155: LDIR, 2 bytes from (field1)
	c.RAM[0x07B] = byte(rec.field1FirstWord >> 8)
	c.RAM[0x071] = byte(rec.field1 + 2) // $5157: (E071h) left pointing 2 bytes past the pair just consumed
	c.RAM[0x072] = byte((rec.field1 + 2) >> 8)
	c.RAM[0x075] = byte(rec.entryTableBase) // injectRoadFeature's own $58D0 pointer
	c.RAM[0x076] = byte(rec.entryTableBase >> 8)

	// $E073: injectRoadFeature's own stream cursor, armed for every stage
	// now that all three per-stage-group feature sets are extracted (see
	// roadFeatureSets). This used to be zeroed for stages 2/3/6 as a
	// "not yet supported" sentinel, which left those stages with no
	// roadside features injected at all for a whole race.
	c.RAM[0x073] = byte(rec.streamBase)
	c.RAM[0x074] = byte(rec.streamBase >> 8)
	c.RAM[0x07F] = 1 // $514A: the first-ever countdown -- see injectRoadFeature's own doc comment for why 1, not the later reload value of 8
	c.RAM[0x06F] = 0x98
	c.RAM[0x070] = 0x59

	switch c.RAM[0x043] { // $50C2-$50C6: CALL $4045 on (E043-1) -- the $50C9 jump table
	case 1:
		// $50E7: RET -- no per-stage init for stage 1.
	case 2:
		c.RAM[0x08A] = byte(stage2And4FeatureStreamBase) // $50E8-$50EB
		c.RAM[0x08B] = byte(stage2And4FeatureStreamBase >> 8)
	case 3:
		c.RAM[0x090] = 0x85 // $50EF-$50F2
		c.RAM[0x091] = 0x7C
		c.RAM[0x06F] = 0x54 // $50F5-$50F8 -- overrides the $5998 default set above: stage 3 reads its road curve from a different ROM table entirely
		c.RAM[0x070] = 0x7C
		c.RAM[0x07C] = 1    // $50FB-$50FD
		c.RAM[0x08A] = 1    // $5100: see doc comment above -- not a pointer, not read by stage 3's own handler
		c.RAM[0x0B2] = 0xA1 // $5103-$5106
		c.RAM[0x0B3] = 0x7A
	case 4, 5:
		copy(c.RAM[0x08A:0x096], stage45SharedInitTable[:]) // $510A-$5115
		c.stage5E094Cursor = stage2And4FeatureStream[:]     // $E094/$E095 = $7DC5 from the same table -- only stage 5's own handler reads it, but the real ROM arms it for both, so this port does too
		if c.RAM[0x043] == 5 {
			c.RAM[0x08A] = 0x8A // $5125-$5128
			c.RAM[0x08B] = 0x7D
			c.RAM[0x092] = 0x0E // $512B-$512E
			c.RAM[0x093] = 0x01
		}
	case 6:
		c.RAM[0x090] = 0xE4 // $50D5-$50D8
		c.RAM[0x091] = 0x7F
	}
}

// injectRoadFeature is ported from $58AF-$590D: one of updateRoadAndScore's
// stage 0/1 callees, previously skipped as a TODO under the (now known
// wrong, see updateRoadAndScore's own doc comment) assumption that the
// whole branch was dead code.
//
// Runs on the same cadence as its caller (roughly every other real
// frame). $E07F counts down from 8 to 0; when it hits 0, this "reloads":
// $E07F resets to 8, the $E073 cursor advances one byte through
// roadFeatureStreamStage145, and (unless the new byte is past the
// stream's end) that byte's low nibble selects a fresh
// roadFeatureTableStage145 entry, whose hold/length/data become the new
// $E086/$E085/$E087-equivalent state. On every OTHER call (not a
// reload), $E086 (the entry's remaining "hold" count) decrements, and
// once it reaches 1, this returns immediately without injecting -- which
// is what makes each entry inject exactly `hold` times per reload cycle,
// not 8.
//
// Every call that doesn't return early (both the reload path and the
// "still holding" path) falls into the shared tail: re-reads the
// CURRENT (not advanced further) stream byte's high nibble to compute a
// destination offset into the 22-byte road-strip staging row ($E058),
// then copies the entry's own `length`-byte chunk starting at the
// current read cursor into that offset, advancing the read cursor by
// `length` for next time. Over one entry's `hold` calls, this walks
// linearly through the entry's entire `hold*length`-byte data run,
// length bytes at a time, always writing to the same destination offset
// (a gradual reveal/update at one fixed screen position, not a moving
// one) -- confirmed by the exact byte-for-byte ROM layout, not guessed.
//
// CORRECTED: this used to no-op entirely for stages 2, 3 and 6, because
// only stage 1/4/5's stream and entry table had been extracted and
// armRoadFeatureStream left $E073 at 0 as a "not yet supported"
// sentinel for the others. That was an honest gap rather than a wrong
// guess, but its effect was severe and easy to miss: those three stages
// got no roadside features injected at all for an entire race, so their
// scrolling roadside stayed whatever fillRoadRowDefault's flat per-stage
// fill byte painted, with none of the trees, signs or scenery the
// feature stream is what places. Reported from real gameplay as stage 2
// showing no palm trees. All three per-stage-group sets are now
// extracted (see roadFeatureSets) and this dispatches on the current
// stage.
func (c *Console) injectRoadFeature() {
	stage := int(c.RAM[0x043]) - 1
	if stage < 0 || stage >= len(roadFeatureSets) {
		return // no ROM data to read for an out-of-range stage -- see armRoadFeatureStream
	}
	set := roadFeatureSets[stage]

	streamPos := uint16(c.RAM[0x073]) | uint16(c.RAM[0x074])<<8
	if streamPos == 0 {
		return // never armed (shouldn't happen once armRoadFeatureStream has run)
	}

	c.RAM[0x07F]--
	if c.RAM[0x07F] == 0 { // $58AF-$58B3: reload
		c.RAM[0x07F] = 8
		streamPos++
		c.RAM[0x073] = byte(streamPos)
		c.RAM[0x074] = byte(streamPos >> 8)

		idx := int(streamPos - set.base)
		// $58C8-$58CA: LD A,(HL); INC A; JR Z -- a $FF byte means "no
		// feature at this step", NOT end-of-stream: the entry selection
		// below is skipped, the hold counter is reset to 8, and the
		// cursor still advances (it already did, above).
		if idx < len(set.stream) && set.stream[idx] != 0xFF {
			entry := set.entries[set.stream[idx]&0x0F]
			c.RAM[0x086] = entry.hold
			c.RAM[0x085] = entry.length
			c.roadFeatureCursor = entry.data
		} else {
			c.RAM[0x086] = 8 // $58E7-$58E9
		}
	} else {
		if c.RAM[0x086] == 1 { // $58B5-$58BB
			return
		}
		c.RAM[0x086]-- // $58BC
	}

	// $58EC-$5908: shared tail, both paths.
	idx := int(streamPos - set.base)
	if idx >= len(set.stream) {
		return // past this port's extracted stream -- see the doc comment's own note on where the real ROM would keep walking
	}
	b := set.stream[idx]
	if b == 0xFF {
		return // $58EF-$58F1: LD A,(HL); INC A; RET Z -- a gap step injects nothing
	}
	destOffset := (b >> 4) * 2
	length := int(c.RAM[0x085])
	if length == 0 || len(c.roadFeatureCursor) < length {
		return // exhausted this entry's data -- see doc comment; not expected to happen given the ROM's own exact byte counts, but guarded rather than panicking
	}
	for i := 0; i < length; i++ {
		c.RAM[0x058+int(destOffset)+i] = c.roadFeatureCursor[i]
	}
	c.roadFeatureCursor = c.roadFeatureCursor[length:]
}

// stageRoadRowFillTable is the 6-byte ROM table at $5191, indexed by
// stage-1, that initPlayerCarSpritesAndFuel's own $508E-$5099 reads once
// at race start into $E089 -- fillRoadRowDefault's fill byte. Stage 1 is
// $A0 (grass, matching every other confirmed "off-track" tile
// reference); stages 4/5 are $06/$09, both inside the blank/invisible
// character range ($00-$0F, see initFirstSixteenChars) -- plausible if
// those stages' own margins are meant to show through to the backdrop
// rather than a textured fill, not independently confirmed.
var stageRoadRowFillTable = [6]byte{0xA0, 0xC5, 0xAB, 0x06, 0x09, 0x80}

// fillRoadRowDefault is ported from $592A-$5939: updateRoadAndScore's
// own $551D callee, run once per qualifying frame before the per-stage
// dispatch (i.e. before injectRoadFeature gets a chance to punch
// specific offsets full of real content). Fills the entire 22-byte
// road-strip staging row ($E058-$E06D) with one flat byte read from
// $E089 -- stageRoadRowFillTable's own per-stage default, set once at
// race start.
//
// CORRECTED: the doc comment here previously claimed $E089 was set by
// "$5132's own per-stage table", which was never actually checked and
// was wrong -- $5132 doesn't touch $E089 at all. The real write site
// ($508E-$5099, part of initPlayerCarSpritesAndFuel's larger body) was
// simply never ported, so $E089 stayed at its cold-boot value of 0 (an
// invisible tile) for the entire time this function has existed in this
// port. That's exactly why every row scrollAndDrawRoadStrip composes
// during actual driving had blank margins on both sides of the road,
// while the road itself (populated separately by injectRoadCurve) and
// the original static paint (which gets its own grass fill directly
// from initTrackLayoutData, not through this path) both looked correct
// -- found from a user's own screenshot of half a screen's worth of
// freshly-scrolled rows, comparing them against the still-correct
// original static rows above.
func (c *Console) fillRoadRowDefault() {
	fill := c.RAM[0x089]
	for i := 0; i < 22; i++ {
		c.RAM[0x058+i] = fill
	}
}

// checkpointGoalText is the 16-byte ROM data window at $596D used by
// injectCheckpointText: the first 11 bytes spell "CHECK POINT" (font
// character mapping confirmed elsewhere, e.g. showSoftwareCredit's own
// "SOFTWARE" decode); the last 5 spell "GOAL" plus one trailing byte
// ($3B, one past 'Z' in the confirmed A-Z run -- not a letter, role not
// confirmed, ported faithfully rather than guessed at or dropped).
var checkpointGoalText = [16]byte{0x23, 0x28, 0x25, 0x23, 0x2B, 0x00, 0x30, 0x2F, 0x29, 0x2E, 0x34, 0x27, 0x2F, 0x21, 0x2C, 0x3B}

// injectCheckpointText is ported from $5944-$596C: updateRoadAndScore's
// own $553A callee (stage 0/1 branch). Once, at the exact moment the
// distance counter ($E078) equals $0F09, writes "CHECK POINT" into the
// road-strip circular buffer at the position 28 bytes past the current
// read pointer ($E046) -- i.e. into a row that hasn't scrolled into view
// yet, the same "write ahead of where the view currently is" pattern
// injectRoadFeature uses via $E058, except this writes directly into the
// buffer itself rather than through the staging row. On stage 6 only,
// also appends "GOAL" (checkpointGoalText's own last 5 bytes) 3 columns
// after "CHECK POINT" ends.
func (c *Console) injectCheckpointText() {
	distance := uint16(c.RAM[0x078]) | uint16(c.RAM[0x079])<<8
	if distance != 0x0F09 {
		return
	}
	ptr := uint16(c.RAM[0x046]) | uint16(c.RAM[0x047])<<8
	dst := ptr + 28
	for i := 0; i < 11; i++ { // "CHECK POINT"
		c.writeRAM(dst+uint16(i), checkpointGoalText[i])
	}
	if c.RAM[0x043] == 6 {
		dst += 11 + 3 // $5967-$5969: INC DE x3
		for i := 0; i < 5; i++ {
			c.writeRAM(dst+uint16(i), checkpointGoalText[11+i])
		}
	}
}

// milestoneSignEntryTable is the 8-byte ROM table at $54F9 that
// milestoneSignTable145 (below) indexes into, 4 bytes at a time: two
// 2-tile-code pairs per entry, injected across 2 consecutive
// injectMilestoneSign calls (the first call copies bytes 0-1, the
// second bytes 2-3, since $E0B5 auto-advances by 2 after each LDIR).
// All 4 pairs fall inside the same $40-$7F HUD-panel/gauge-bar
// character range other roadside decoration reuses (see
// docs/ANALYSIS.md) -- presumably small distance-milestone road signs.
var milestoneSignEntryTable = [8]byte{0x76, 0x77, 0x74, 0x75, 0x7A, 0x7B, 0x78, 0x79}

// milestoneSignTable145 is the ROM table at $50DC ({distanceHighByte,
// entryIndex} pairs, both single bytes) -- one of the two fixed
// addresses initPlayerCarSpritesAndFuel's own $E0B2 conditional can
// select. The *other* address, $50BE, is a single literal $FF byte --
// since $E079 (distance's own high byte) would need to reach $FF
// (roughly 65280 distance units) to ever match it, that's a sentinel
// meaning "never trigger", not a real table -- so this port only models
// $50DC's own real 5 entries, and injectMilestoneSign treats $E0B2==$50BE
// as its own permanently-inert case rather than encoding a table that's
// designed to never be read. Confirmed to be exactly 5 entries (10
// bytes) by finding real Z80 code (not further table data) starting
// immediately after them at $50E6.
var milestoneSignTable145 = [5][2]byte{{0x01, 0x00}, {0x04, 0x00}, {0x06, 0x01}, {0x07, 0x01}, {0x0E, 0x00}}

// injectMilestoneSign is ported from $54AF-$54F8: one of
// updateRoadAndScore's stage 0/1 callees. Watches $E079 (the distance
// counter's own high byte) for a match against milestoneSignTable145's
// current entry (advancing through the table as each entry matches, one
// entry per race -- never rewound); on a match, arms a 2-call injection
// of the matched entry's 4-byte icon pair (from milestoneSignEntryTable)
// into the $E058 staging row, at a destination offset derived from the
// track-boundary table's own current value ($E098, the same table
// advanceBoundaryHistoryTable now keeps live -- see its own doc
// comment).
//
// $E0B2 is set once, at race start, by initPlayerCarSpritesAndFuel's own
// $E0B2 conditional (lap counter and stage-6 dependent, see its doc
// comment) to either $50BE (a permanent no-op sentinel, see
// milestoneSignTable145's own doc comment) or $50DC (this table). This
// port only has $50DC's real table extracted; $50BE needs no table at
// all since it never matches.
func (c *Console) injectMilestoneSign() {
	if c.RAM[0x0B4] == 0 { // $54B2-$54B4: not currently mid-injection
		ptr := uint16(c.RAM[0x0B2]) | uint16(c.RAM[0x0B3])<<8
		if ptr < 0x50DC {
			return // $E0B2 == $50BE's own sentinel, or an unsupported stage -- see doc comment
		}
		idx := int(ptr - 0x50DC)
		if idx+1 >= len(milestoneSignTable145)*2 {
			return // past this port's own extracted 5 entries
		}
		entry := milestoneSignTable145[idx/2]
		if c.RAM[0x079] != entry[0] { // $54B9-$54BD
			return
		}
		c.RAM[0x0B2] = byte(ptr + 2) // $54BE-$54C1: advance past this entry
		c.RAM[0x0B3] = byte((ptr + 2) >> 8)
		src := 0x54F9 + uint16(entry[1])*4 // $54C4-$54C9: LD HL,54F9h; (byte)*4; HL+=
		c.RAM[0x0B5] = byte(src)
		c.RAM[0x0B6] = byte(src >> 8)
		c.RAM[0x0B4] = 2 // $54CF-$54D1: arm the 2-call injection

		lookup := c.RAM[0x098] >> 2 // $54D4-$54D9: RRA x2; AND 3Fh
		c.RAM[0x0B7] = (lookup & 0x3F) + 2
	}

	src := uint16(c.RAM[0x0B5]) | uint16(c.RAM[0x0B6])<<8
	dst := 0xE058 + uint16(c.RAM[0x0B7])
	srcIdx := int(src) - 0x54F9
	if srcIdx >= 0 && srcIdx+1 < len(milestoneSignEntryTable) {
		c.writeRAM(dst, milestoneSignEntryTable[srcIdx])
		c.writeRAM(dst+1, milestoneSignEntryTable[srcIdx+1])
	}
	src += 2
	c.RAM[0x0B5] = byte(src)
	c.RAM[0x0B6] = byte(src >> 8)
	c.RAM[0x0B4]--
}

// advanceBoundaryHistoryTable is ported from $590E-$5929: called
// unconditionally from $56F7 (the still-only-partially-traced $5501
// callee between injectRoadFeature and injectCheckpointText -- see its
// own doc comment for what's not yet ported). Shifts the 23-byte
// track-boundary table checkOffTrackCollision reads from ($E098-$E0AE,
// see initPlayerCarSpritesAndFuel's own doc comment for its flat $20
// cold-start default) up by one byte -- $E098's old value moves to
// $E099, ..., $E0AE's old value moves to $E0AF, the table's own last
// byte -- then writes a brand new value into the now-vacant $E098 from
// (($E07D<<2) | ($E07A&3)).
//
// This is a genuinely important find, not a minor detail: it means the
// per-row boundary table this port's own checkOffTrackCollision already
// reads every frame has, until now, never actually changed after its
// one-time flat-$20 initialization -- off-track detection has been
// checking a static table the entire time this port has run, regardless
// of how the road curves. Whether $E07A/$E07D actually encode "the
// current curve" in a way that makes the resulting boundary values
// meaningful isn't independently confirmed yet (neither register's own
// broader role is fully traced), so this is ported for its confirmed
// mechanical effect, not a confirmed claim that curves now affect
// collision correctly end to end.
func (c *Console) advanceBoundaryHistoryTable() {
	for addr := 0x0AE; addr > 0x098; addr-- {
		c.RAM[addr+1] = c.RAM[addr]
	}
	c.RAM[0x099] = c.RAM[0x098]
	c.RAM[0x098] = (c.RAM[0x07D] << 2) | (c.RAM[0x07A] & 0x03)
}

// roadCurveArray is the ROM table at $5998: a single shared stream of
// road-boundary tile codes ($D0/$D1, the confirmed safe-road range,
// mixed with $D9-$EC-ish boundary-transition values). The 5 entries in
// roadCurveTableStarts are just different starting offsets into this
// SAME array, not 5 separate tables -- confirmed directly: $59C1,
// $5A19, $5A1B, and $59C3 all fall within $5998's own span. Read one
// byte at a time via injectRoadCurve's own chased cursor ($E06F), this
// is what feeds fine-grained curve shape into the road-strip staging
// row ($E058) alongside injectRoadFeature's own coarser terrain
// features.
//
// CORRECTED: an earlier extraction stopped after 199 bytes -- long
// enough that roadCurveHandler5768's own wraparound (the $FF sentinel
// at index 40) and the first several curve phases all looked complete,
// but too short for roadCurveHandler57A7's own reads, which can index
// up to 9 bytes before $E06F for its variable-length run and legitimately
// reach past byte 198. Found by direct comparison against the real ROM
// running on a real Z80 core (github.com/remogatto/z80, throwaway
// investigation tooling): this port's own injectRoadCurve output
// diverged from the real ROM exactly when a nibble-3/4 call's own
// $E081 grew large enough to read past this array's old end -- this
// port's own bounds guard (roadCurveByte returning ok=false past the
// end, a deliberate safety net for exactly this "extraction is
// incomplete" scenario, see its own doc comment) silently skipped the
// write instead of crashing, which looked like "this byte just isn't
// changing" rather than "reading past the end of known data" until
// checked against the real ROM's own bytes at those addresses.
// Extended to 217 bytes: the real ROM's data continues in the same
// tile-code pattern (still every value falling in the confirmed
// $D0-$F8 boundary/transition range) through byte 216, then changes
// character entirely at byte 217 (unrelated small integers, not
// tile-shaped) -- a natural, non-arbitrary stopping point, not just
// "extended until the test passed."
var roadCurveArray = assets.RoadCurveArray // extracted from $5998; see assets/assets.go

const roadCurveArrayBase uint16 = 0x5998

// roadCurveByte reads one byte from roadCurveArray at a real (as the
// original code would compute it) address, returning ok=false if it
// falls outside the bytes this port has extracted -- a documented
// safety guard (this port has no ROM loaded at runtime to read further
// data from, see assets' own doc comment), not a guess at what real
// hardware would do walking further into adjacent ROM bytes.
func roadCurveByte(addr uint16) (b byte, ok bool) {
	idx := int(addr) - int(roadCurveArrayBase)
	if idx < 0 || idx >= len(roadCurveArray) {
		return 0, false
	}
	return roadCurveArray[idx], true
}

// roadCurveTableStarts is the 5-entry pointer table at $593A, each entry
// just a different starting offset into roadCurveArray (see its own doc
// comment) -- indexed by ($E07A & $0F), the same low nibble
// injectRoadCurve's own caller (advanceBoundaryHistoryTable's sibling in
// $56F7) dispatches on.
var roadCurveTableStarts = [5]uint16{0, 41, 129, 131, 43}

// roadCurvePhase8Table is the 8-byte ROM table at $5808, indexed by
// ($E07B & 7) -- used only by the $57A7 handler (nibble 3/4). Unlike
// roadCurveArray, entries here are read directly as single bytes, not
// chased as pointers.
var roadCurvePhase8Table = [8]byte{0xE7, 0xE7, 0xE6, 0xE7, 0xE7, 0xE7, 0xE6, 0xE7}

// roadCurveDeltaTable is the 4-byte ROM table at $58AB, indexed by
// (high nibble of $E07A)*2 -- sets $E081 for the $57A7 handler's own use
// (its role there, "how many bytes to copy from roadCurveArray", is
// confirmed mechanically; why these specific 4 values isn't).
var roadCurveDeltaTable = [4]byte{4, 7, 6, 9}

// roadCurvePhaseStreams holds each stage's own real curve-phase-stream
// ROM bytes, indexed the same way roadFeatureStageRecords is (stage-1)
// -- each one starting at that same stage's own field1 address
// (roadFeatureStageRecords[i].field1) and running through its own real
// $FE terminator plus one trailing (unused, but needed so the final
// 2-byte read doesn't go out of bounds) byte -- read 2 bytes at a time
// into $E07A/$E07B whenever the current phase's own hold counter
// ($E07B) reaches 0.
//
// CORRECTED: an earlier pass at this port only ever extracted and
// verified stage 1's own stream (originally named/documented as if it
// were shared by "stages 1/4/5", on the mistaken assumption -- already
// corrected elsewhere in armRoadFeatureStream's own doc comment for the
// *seed* values $E07A/$E07B/$E071 -- that those stages' streams were
// identical). They aren't: roadFeatureStageRecords' own field1FirstWord
// column already proved stages 4 and 5 have distinct high bytes ($29,
// $31 vs stage 1's $19), meaning distinct ROM data at distinct
// addresses ($7E83, $7E60 vs $78C4). Since refreshRoadCurvePhase kept
// indexing that one stage-1 array/base regardless of which stage was
// actually running, every stage except 1 computed a wildly
// out-of-bounds index on its very first call and silently never
// injected any curve data at all -- from frame 1, not "after a while"
// like stage 1's own now-fixed bug, but the same visible symptom (a
// road row rendering as flat, uncurved grass, reported this time on
// stage 2). Fixed by extracting and independently verifying (each
// stream's own real $FE terminator confirmed to land pair-aligned, the
// same check that validated stage 1's) all 6 stages' real streams from
// the ROM directly, and generalizing refreshRoadCurvePhase to select
// the current stage's own base/stream instead of a single hardcoded
// pair. Stages 2 and 3 share identical bytes, matching
// roadFeatureStageRecords' own already-confirmed identical record for
// those two stages.
var roadCurvePhaseStreams = [6][]byte{
	assets.RoadCurvePhaseStage1,  // $78C4
	assets.RoadCurvePhaseStage23, // $7AAA -- stages 2 and 3 share one stream
	assets.RoadCurvePhaseStage23,
	assets.RoadCurvePhaseStage4, // $7E83
	assets.RoadCurvePhaseStage5, // $7E60
	assets.RoadCurvePhaseStage6, // $7EA8
}

// refreshRoadCurvePhase is ported from $585F-$58A8 (also reached, with a
// small twist, from $5852 -- see injectRoadCurve's own doc comment):
// reads the next 2 bytes from the phase stream ($E071) into
// $E07A/$E07B, points $E06F at the roadCurveArray entry
// roadCurveTableStarts[$E07A&$0F] selects, and -- only when the low
// nibble is 3 or 4 (the $57A7 handler's own range) -- also derives
// $E081 from the high nibble via roadCurveDeltaTable.
//
// $585F's own body has NO early-return path on real hardware: both of
// its own exits ($5890 `JP NC,L56F7` and $58A8 `JP L56F7`) jump straight
// back to injectRoadCurve's own top -- a genuine loop-back with the
// call stack untouched (confirmed via $4045's documented tail-call
// semantics: it pops and discards its own return address rather than
// preserving it, so `JP L56F7` from deep in this function is not
// "return to whoever dispatched here", it's "restart injectRoadCurve's
// nibble dispatch from scratch"). This return value models exactly
// that: true means "loop back", always the real behavior on real
// hardware. It's false only for this port's own synthetic safety guard
// (the phase stream has run past its own extracted end, see
// roadCurvePhaseStreams' doc comment) -- a case that should no
// longer happen in practice now that injectRoadCurve's own $FE re-arm
// check (see its doc comment) keeps the stream cycling indefinitely.
//
// CORRECTED: this function previously also returned false whenever the
// freshly-read nibble was 5 or greater -- reached for real once the
// stream's own $FE terminator byte is read as a nibble-14 "low nibble"
// (see injectRoadCurve's own doc comment for why $FE is a real,
// load-bearing sentinel, not inert). On real hardware there's no such
// guard: $5882-5886 unconditionally loads a new $E06F from
// $593A+nibble*2 regardless of nibble's value, reading well past
// roadCurveTableStarts' own 5 confirmed entries into unrelated ROM
// data for nibble 14. That computed value is provably never used --
// injectRoadCurve's own leading $E07A==$FE check re-arms the whole
// stream (via armRoadFeatureStream) on the very next loop pass, before
// any handler ever dispatches on this stale state -- so this port
// simply leaves $E06F unchanged for that one transitional call instead
// of computing a value from ROM data it doesn't have extracted, and
// returns true (matching the real "always loop back" behavior) instead
// of stopping. Found the same way as the two roadCurveHandler5810Tail/
// roadCurveArray bugs above: a direct byte-for-byte comparison against
// the real ROM (this time run far enough, ~1000 calls, to reach the
// stream's own natural wraparound) diverged at the exact call where the
// real ROM's own $E071/$E07A/$E07B visibly reset to their initial
// per-stage values and this port's own injectRoadCurve instead stopped
// injecting entirely for the rest of the race -- matching a user report
// of a whole road row turning to blank grass after driving for a while.
func (c *Console) refreshRoadCurvePhase() bool {
	stream := roadCurvePhaseStreams[c.RAM[0x043]-1]
	base := roadFeatureStageRecords[c.RAM[0x043]-1].field1
	pos := uint16(c.RAM[0x071]) | uint16(c.RAM[0x072])<<8
	idx := int(pos) - int(base)
	if idx < 0 || idx+1 >= len(stream) {
		return false
	}
	newA, newB := stream[idx], stream[idx+1]
	pos += 2
	c.RAM[0x071] = byte(pos)
	c.RAM[0x072] = byte(pos >> 8)
	c.RAM[0x07A] = newA
	c.RAM[0x07B] = newB

	nibble := newA & 0x0F
	if nibble == 2 {
		c.RAM[0x07D]--
	}
	if int(nibble) < len(roadCurveTableStarts) {
		c.RAM[0x06F], c.RAM[0x070] = lo16(roadCurveArrayBase+roadCurveTableStarts[nibble]), hi16(roadCurveArrayBase+roadCurveTableStarts[nibble])
	}

	if byte(nibble-3) < 2 { // $588A-5890: SUB 3h; CP 2h; JP NC -- falls through (doesn't jump away) only when nibble is 3 or 4
		// $5893-589D: EX AF,AF' stashes A (=nibble-3, 0 or 1) across computing
		// the high nibble into B, then ADD A,B combines them -- NOT hi*2, a
		// mistake an earlier pass at this (and this same session's own
		// verification simulation) both made on the first attempt, caught by
		// re-deriving the EX AF,AF' register flow a second time by hand.
		hi := (newA >> 4) & 0x0F
		idx := (nibble - 3) + hi
		if int(idx) < len(roadCurveDeltaTable) {
			c.RAM[0x081] = roadCurveDeltaTable[idx]
		}
	}
	return true
}

func lo16(v uint16) byte { return byte(v) }
func hi16(v uint16) byte { return byte(v >> 8) }

// injectRoadCurve is ported from $56F7 in full (not just its
// nibble-dispatched tail, an earlier pass at this port's own
// mischaracterization -- see the CORRECTED note below): calls
// advanceBoundaryHistoryTable ($5701: CALL $590E), then dispatches on
// ($E07A&$0F) via the confirmed table at $570C to one of three
// handlers sharing a common "copy one byte via a direct index, then a
// variable-length run via a computed skip" shape: $5768 (nibble 0),
// $5810 falling through to $5852 on its own hold-counter expiry
// (nibbles 1-2), and $57A7 (nibbles 3-4) -- all writing into the $E058
// staging row alongside injectRoadFeature and fillRoadRowDefault.
//
// CORRECTED: an earlier pass at this port modeled each handler as an
// independent function that, on its own internal countdown reaching
// zero, called refreshRoadCurvePhase and then CONTINUED with that SAME
// handler's own remaining body (its own copy-length/skip-distance
// logic) using the freshly refreshed $E06F/$E07A/$E07D. That's wrong:
// $585F/refreshRoadCurvePhase's own body has no early-return path on
// real hardware -- every route through it (a plain fallthrough, plus
// $5768's, $5852's, and $57A7's own separate calls into it) ends by
// jumping straight back to $56F7's own top (`JP L56F7`/`JP NC,L56F7`),
// re-running advanceBoundaryHistoryTable and re-dispatching on the
// NEWLY refreshed nibble from scratch -- which can easily route to a
// COMPLETELY DIFFERENT handler than the one that just expired. This is
// confirmed, not inferred: $4045 (the trampoline $5709's own `CALL
// $4045` uses to dispatch into these handlers) pops and discards its
// own return address rather than preserving it (see docs/ANALYSIS.md's
// disassembler notes), so `JP L56F7` is genuinely "restart the
// dispatch", not "return to whoever called injectRoadCurve" -- the
// call stack is never touched by any of this. Using the old (wrong)
// handler's own leftover arithmetic on state set up for a different
// handler is exactly the kind of thing that would corrupt curve tiles
// without crashing -- found while investigating a user report of
// broken road-shoulder/mid-road tiles specifically during curves (the
// only time refreshRoadCurvePhase's countdown-expiry path actually
// fires). Each handler now returns a bool: true means "this call ended
// via the JP-back-to-$56F7 path, loop and re-dispatch"; false means
// "reached a natural RET, stop and return to the caller"
// (updateRoadAndScore, which then calls injectCheckpointText/
// injectMilestoneSign/scrollAndDrawRoadStrip exactly once, matching
// $553A/$553D/$5540 -- all three are separately, independently CALLed
// after $56F7's own CALL at $5537 returns, not chained through it).
//
// Verified against a standalone register-flow simulation of this
// corrected model (the cascading ADD/SUB/NEG arithmetic computing each
// handler's copy length and skip distance is exactly the kind of thing
// this project has gotten wrong by hand before) rather than hand-traced
// -- running it a couple thousand iterations from stage 1's own real
// seed data produces stable, in-bounds writes into the 22-byte staging
// row using real $D0/$D1-range road tile codes throughout the range
// covered by this port's own extracted 16-pair phase stream prefix (see
// roadCurvePhaseStreams' own doc comment for what happens once
// that's exhausted -- a separate, already-documented limitation, not
// something this fix introduces or fixes).
//
// CORRECTED: $E07A==$FE re-arming the phase stream from scratch ($5709's
// own leading check, `CP FEh; CALL $50BF`) was previously modeled as
// inert, on the reasoning that nothing in the confirmed ROM control flow
// ever writes literal $FE to $E07A. That reasoning missed the real
// source: $FE is roadCurvePhaseStreams' own terminator byte
// (confirmed directly in the ROM at $78E4, immediately followed by
// unrelated data at $78E5 -- see that array's own doc comment), and
// refreshRoadCurvePhase reads it as an ordinary stream byte once the
// stream's own 16 real pairs are exhausted, setting $E07A=$FE for real.
// Without this re-arm, that's where this port used to stop injecting
// curve data entirely for the rest of the race -- every subsequent
// updateRoadAndScore call would still run fillRoadRowDefault (a flat
// grass fill) but never punch real road tiles into it, which is exactly
// what a user-reported "one whole row turns to blank grass after
// driving for a while, crash at speed / driveable slowly" bug looks
// like. Fixed by checking for $E07A==$FE at the top of every pass
// (including loop-backs, matching $56F7's own position for this check,
// before advanceBoundaryHistoryTable) and calling armRoadFeatureStream
// (the same $50BF/$5132 this port already uses for the initial
// race-start arm) to reset the whole per-stage curve/feature state back
// to its starting values. Verified against a direct byte-for-byte
// comparison against the real ROM extended far enough (3000 calls) to
// reach this wraparound for real: the real ROM's own $E06F/$E07A/$E07B/
// $E071 visibly reset to their initial per-stage values at the same
// call this port's fix now reproduces exactly.
func (c *Console) injectRoadCurve() {
	for {
		if c.RAM[0x07A] == 0xFE { // $56F7-$56FE: CP FEh; CALL L50BF
			c.armRoadFeatureStream()
		}
		c.advanceBoundaryHistoryTable() // $5701: CALL $590E -- runs on every pass, including loop-backs and re-arms
		nibble := c.RAM[0x07A] & 0x0F
		var loopBack bool
		switch {
		case nibble == 0:
			loopBack = c.roadCurveHandler5768()
		case nibble == 1 || nibble == 2:
			loopBack = c.roadCurveHandler5810()
		default:
			loopBack = c.roadCurveHandler57A7()
		}
		if !loopBack {
			return
		}
	}
}

// roadCurveSharedTail is ported from $5786-$57A6 (also reached via a
// direct CALL from $5810's own tail at $584B): given the cursor left
// just past the single byte roadCurveHandler5768/5810 already copied,
// adjusts it by a skip distance derived from ($E07A>>4)&3 (0, 3, or 6
// bytes), then copies a further (that same value + 1)*3 bytes -- i.e.
// runs of 3, 6, or 9 bytes -- from there into $E058 right after the
// first byte, advancing $E06F to just past what was read.
func (c *Console) roadCurveSharedTail(hl, de uint16) {
	hiNibble := (c.RAM[0x07A] >> 4) & 0x03
	skip := byte(2 - hiNibble) // $578F-5794: SUB 2h; NEG
	hl += uint16(skip) * 3
	count := int(hiNibble+1) * 3
	for i := 0; i < count; i++ {
		if b, ok := roadCurveByte(hl + uint16(i)); ok {
			c.writeRAM(de+uint16(i), b)
		}
	}
	hl += uint16(count)
	c.RAM[0x06F], c.RAM[0x070] = lo16(hl), hi16(hl)
}

// roadCurveHandler5768 is ported from $5768-57A6 (dispatched when
// ($E07A&$0F)==0). Counts $E07B down to 0; on expiry, per injectRoadCurve's
// own doc comment, jumps away to refreshRoadCurvePhase and never returns
// here -- modeled by returning refreshRoadCurvePhase's own result
// directly rather than falling through into this handler's own body
// with refreshed state. Otherwise wraps $E06F back to roadCurveArray's
// own start on hitting that array's $FF sentinel byte (present once, at
// $5998+40 -- see roadCurveArray), copies one byte at the current
// cursor into $E058+$E07D, then hands off to roadCurveSharedTail for
// the variable-length follow-up run, returning false (natural RET at
// $57A6).
func (c *Console) roadCurveHandler5768() bool {
	c.RAM[0x07B]--
	if c.RAM[0x07B] == 0 { // $576C: JP Z,L585F -- a jump away, not a call; never falls back into this function
		return c.refreshRoadCurvePhase()
	}
	hl := uint16(c.RAM[0x06F]) | uint16(c.RAM[0x070])<<8
	if b, ok := roadCurveByte(hl); ok && b == 0xFF {
		hl = roadCurveArrayBase
		c.RAM[0x06F], c.RAM[0x070] = lo16(hl), hi16(hl)
	}
	de := 0xE058 + uint16(c.RAM[0x07D])
	if b, ok := roadCurveByte(hl); ok {
		c.writeRAM(de, b)
	}
	c.roadCurveSharedTail(hl+1, de+1)
	return false
}

// roadCurveHandler5810Tail is ported from $583E-5851, the tail $5810's
// own body falls into.
//
// CORRECTED: an earlier pass at this port modeled this as copying only
// ONE literal byte before handing off to roadCurveSharedTail, by analogy
// with roadCurveHandler5768's own body (which really does only write
// one). That's wrong for this handler specifically: `$584B: CALL L5784`
// does not call roadCurveSharedTail's own entry ($5786) -- it calls
// $5784, which is itself the middle of $5768's body: a bare `LDI`
// instruction. Reached this way, that LDI copies a SECOND literal byte
// (using HL/DE already advanced by this function's own LDI at $5849)
// before falling straight through into $5786's shared arithmetic. So
// $5810Tail writes two literal bytes where $5768 writes one, and must
// hand off to roadCurveSharedTail with the cursor advanced by 2, not 1.
//
// Found by direct comparison against the real ROM running on a real Z80
// core (github.com/remogatto/z80, throwaway investigation tooling): from
// stage 1's own real seed data, this port's own injectRoadCurve output
// matched the real ROM's exactly for the first 24 calls (the entire
// nibble-0/$5768 stretch), then diverged at the very first nibble-1
// dispatch into this function -- this port wrote $D1 at $E058 offset 8
// where the real ROM wrote $E7, and $E06F ended up one byte short
// ($59CB vs the real $59CC). Both discrepancies vanish with this fix
// (verified by re-running the same comparison to completion).
func (c *Console) roadCurveHandler5810Tail() {
	hl := uint16(c.RAM[0x06F]) | uint16(c.RAM[0x070])<<8
	de := 0xE058 + uint16(c.RAM[0x07D])
	if b, ok := roadCurveByte(hl); ok {
		c.writeRAM(de, b)
	}
	if b, ok := roadCurveByte(hl + 1); ok {
		c.writeRAM(de+1, b)
	}
	c.roadCurveSharedTail(hl+2, de+2)
}

// roadCurveHandler5810 is ported from $5810-583B (dispatched when
// ($E07A&$0F) is 1 or 2). Counts $E07B down to 0 (falling into
// roadCurveHandler5852 on expiry, $5810's own "wrap" path -- distinct
// from $5768's); every 8th count (($E07B&7)==0) additionally nudges
// $E07D and re-seeds $E06F from roadCurveTableStarts before the shared
// tail runs. Returns roadCurveHandler5852's own result on expiry (see
// its doc comment for why -- it's a genuine loop-back, not a
// subroutine call this function's own tail resumes after); false
// (natural RET) on every other path.
func (c *Console) roadCurveHandler5810() bool {
	c.RAM[0x07B]--
	if c.RAM[0x07B] == 0 { // $5814: JR Z,L5852
		return c.roadCurveHandler5852()
	}
	if c.RAM[0x07B]&0x07 != 0 {
		c.roadCurveHandler5810Tail()
		return false
	}
	nibble := c.RAM[0x07A] & 0x0F
	c.RAM[0x07D]++
	if nibble != 2 {
		c.RAM[0x07D] -= 2
	}
	c.RAM[0x06F], c.RAM[0x070] = lo16(roadCurveArrayBase+roadCurveTableStarts[nibble]), hi16(roadCurveArrayBase+roadCurveTableStarts[nibble])
	c.roadCurveHandler5810Tail()
	return false
}

// roadCurveHandler5852 is ported from $5852-585E (the version of
// $585F's own re-arm reached from $5810's own expiry, as opposed to
// $57A7's -- see refreshRoadCurvePhase's doc comment for the shared
// part): bumps $E07D unless the current low nibble is exactly 1, then
// falls straight into $585F/refreshRoadCurvePhase, which -- per
// injectRoadCurve's own CORRECTED doc comment -- always ends by
// jumping back to $56F7's top on success, never by returning here to
// continue into roadCurveHandler5810Tail (an earlier pass at this port
// did exactly that, incorrectly). So this simply returns
// refreshRoadCurvePhase's own result.
func (c *Console) roadCurveHandler5852() bool {
	if c.RAM[0x07A]&0x0F != 1 {
		c.RAM[0x07D]++
	}
	return c.refreshRoadCurvePhase()
}

// roadCurveHandler57A7 is ported from $57A7-5807 (dispatched when
// ($E07A&$0F) is 3 or 4). Copies one byte directly from
// roadCurvePhase8Table (indexed by $E07B&7, not chased through $E06F)
// into $E058+$E07D, then a further $E081 bytes from roadCurveArray
// starting (9-$E081) bytes past $E06F. Counts $E07B down separately;
// hitting its own "negative" wraparound re-arms the phase stream via
// refreshRoadCurvePhase and returns its result directly (a genuine
// loop-back to $56F7's top, per injectRoadCurve's own CORRECTED doc
// comment -- $57DA's `JP M,L585F` never returns here); every 8th count
// otherwise nudges $E081 and re-seeds $E06F the same way $5810 nudges
// $E07D, then returns false (natural RET at $5807, or the early RETs
// at $57E0/$57E3).
func (c *Console) roadCurveHandler57A7() bool {
	de := 0xE058 + uint16(c.RAM[0x07D])
	c.writeRAM(de, roadCurvePhase8Table[c.RAM[0x07B]&0x07])

	hl := uint16(c.RAM[0x06F]) | uint16(c.RAM[0x070])<<8
	count := int(c.RAM[0x081])
	if count > 9 { // $57C3-57C5: SUB 9h; NEG -- would underflow if $E081 ever drifts above 9 via the nudge below; the ROM's own roadCurveDeltaTable never exceeds 9, so this is a safety guard, not a confirmed real case
		count = 9
	}
	start := hl + uint16(9-count)
	for i := 0; i < count; i++ {
		if b, ok := roadCurveByte(start + uint16(i)); ok {
			c.writeRAM(de+1+uint16(i), b)
		}
	}
	hl = start + uint16(count) + 2
	c.RAM[0x06F], c.RAM[0x070] = lo16(hl), hi16(hl)

	c.RAM[0x07B]--
	if c.RAM[0x07B]&0x80 != 0 { // $57DA: JP M,L585F -- negative; a jump away, never returns here
		return c.refreshRoadCurvePhase()
	}
	if c.RAM[0x07B]&0x07 != 0 || c.RAM[0x07B] == 0 { // $57E0/$57E3: RET NZ / RET Z
		return false
	}
	nibble := c.RAM[0x07A] & 0x0F
	c.RAM[0x081]++
	if nibble != 3 {
		c.RAM[0x081] -= 2
	}
	c.RAM[0x06F], c.RAM[0x070] = lo16(roadCurveArrayBase+roadCurveTableStarts[nibble]), hi16(roadCurveArrayBase+roadCurveTableStarts[nibble])
	return false // $5807: RET
}

// spritePatternColorPairs is the 4-byte ROM table at $51C1 that
// fillSpriteRecordBatch reads from. It's re-read from the start on every
// one of the outer loop's passes rather than advanced through -- $51A8's
// own "LD HL,$51C1" sits inside the outer DJNZ's loop body, not before
// it, the same "replay the same source every pass" idiom $783F's own
// outer loop uses (confirmed the same way, by tracing exactly which
// instructions the DJNZ target range actually covers, after an initial
// reading of this as "a big table the loop advances through" turned out
// to run past the end of the actual 4-byte table into the next
// function's own code bytes).
var spritePatternColorPairs = [4]byte{0x00, 0x01, 0x04, 0x04}

// fillSpriteRecordBatch replicates one of $5197's two invocations of the
// shared worker at $51A5: for `passes` outer iterations, writes 2
// sprite-attribute-table records (Y, X, pattern, color) starting at dst
// -- X fixed at x for the whole batch, Y starting at $08 and advancing by
// $18 each pass, pattern/color always the same two pairs from
// spritePatternColorPairs regardless of pass. Advances dst by 8 bytes (2
// records) per pass.
func (c *Console) fillSpriteRecordBatch(dst uint16, x byte, passes int) {
	y := byte(0x08)
	for p := 0; p < passes; p++ {
		for i := 0; i < 2; i++ {
			c.writeRAM(dst, y)
			c.writeRAM(dst+1, x)
			c.writeRAM(dst+2, spritePatternColorPairs[i*2])
			c.writeRAM(dst+3, spritePatternColorPairs[i*2+1])
			dst += 4
		}
		y += 0x18
	}
}

// initTrafficSpriteSlots is ported from $5197-$51C0: called once from
// stateGetReadyWait phase 7 ($41CD), right after drawRoadStrip paints the
// initial road. Fills sprite-attribute-table staging entries 3-24 (22 of
// the 30 entries blitSpriteAttributeTable's own $78-count call copies to
// VRAM -- entries 0-2 are the traffic/opponent-car slot and the player's
// own 2-sprite pair, already set up by initPlayerCarSpritesAndFuel) with
// two back-to-back batches: entries 3-12 at column X=$51, entries 13-24
// at X=$65, both cycling the same 5 (or 6) Y positions $08/$20/$38/$50/
// $68(/$80) spaced 24px apart and alternating between two fixed
// pattern/color pairs. The destination range and exact per-record layout
// are confirmed by tracing the loop precisely (register-flow simulated,
// not hand-counted, after the mistake noted on spritePatternColorPairs);
// what these 22 sprites actually depict -- extra traffic cars, roadside
// decoration -- is not independently confirmed, only their mechanical
// placement.
func (c *Console) initTrafficSpriteSlots() {
	c.fillSpriteRecordBatch(0xE11A, 0x51, 5) // $5197-$519D: DE=E11Ah, BC=$0551
	c.fillSpriteRecordBatch(0xE142, 0x65, 6) // $51A0-$51A5 (fallthrough): E=$42 (D unchanged), BC=$0665
}

// clampRowBand mirrors a repeated idiom -- $6DC4-$6DCE (used by
// checkOffTrackCollision, bandWidth=$10) and $6D92-$6D9C (used by
// checkTrackBoundaryTile, bandWidth=$18) -- that special-cases row values
// near the bottom of the screen (>=$C0) into one of two fixed sentinels
// depending on how far past $C0 they are, rather than using the row value
// directly. Plausibly related to how the track visually wraps/repeats
// near the bottom of the display, not independently confirmed.
func clampRowBand(value, bandWidth byte) byte {
	if value < 0xC0 {
		return value
	}
	if value-0xC0 >= bandWidth {
		return 0
	}
	return 0xB8
}

// checkTrackBoundaryTile is ported from $6D8B-$6DC0, shared by
// boundaryCheckFar ($6D2A) and boundaryCheckNear ($6D56): looks up the
// background tile 4 rows ahead of a given (row, column) pair -- rowAddr
// points at the row byte, with the column always at rowAddr+2, both
// confirmed by $6DC1's own literal register use (see checkOffTrackCollision)
// -- clamped near the bottom of the screen via clampRowBand, and
// `colOffset` pixels right of the column, via the confirmed
// pixelToNameTableAddr address conversion. The original reads the tile
// via SETRD+IN on the VDP data port rather than calling RDVRM directly,
// but the observable effect -- read one byte from a given VRAM address --
// is identical, so this uses c.Sys.ReadVRAM. Returns the raw tile value
// and whether it falls outside the $CF-$D8 "safe" band; tile IDs aren't
// independently cross-referenced against the extracted scenery/border
// tilesets, so what specifically makes a tile "safe" here isn't confirmed
// beyond the mechanism.
func (c *Console) checkTrackBoundaryTile(rowAddr uint16, colOffset byte) (tile byte, unsafe bool) {
	row := clampRowBand(c.readRAM(rowAddr)+4, 0x18)
	col := c.readRAM(rowAddr+2) + colOffset
	addr := pixelToNameTableAddr(row, col)
	tile = c.Sys.ReadVRAM(addr)
	return tile, tile < 0xCF || tile >= 0xD9
}

// boundaryColumnCheckFar is the shared sub-check at $6D3B-$6D49, used by
// boundaryCheckFar both for a genuinely-read tile in [$D9,$E1) and (via
// $6D4D-$6D52) for a substituted value when the tile is exactly $EC.
func (c *Console) boundaryColumnCheckFar(rowAddr uint16, value byte) (resultB byte, unsafe bool) {
	col := c.readRAM(rowAddr + 2)
	masked := (col + 0x0C) & 0xF8
	pre := col - masked + value // byte-wrapping throughout, matching $6D3B-$6D44
	if pre < 0xD4 {             // $6D45-$6D47: SUB $D4; JR C -- borrow means safe
		return 0, false
	}
	return 1, true // $6D49
}

// boundaryCheckFar is ported from $6D2A-$6D54: one of the two track-edge
// checks checkOffTrackCollision dispatches to, using a 12px column
// look-ahead offset.
func (c *Console) boundaryCheckFar(rowAddr uint16) (tile, resultB byte, unsafe bool) {
	tile, unsafe = c.checkTrackBoundaryTile(rowAddr, 0x0C)
	if !unsafe {
		return tile, 0, false
	}
	switch {
	case tile < 0xCF: // $6D31-$6D33
		return tile, 1, true // $6D49
	case tile < 0xE1: // $6D35-$6D39: tile in [$D9,$E1)
		resultB, unsafe = c.boundaryColumnCheckFar(rowAddr, tile)
		return tile, resultB, unsafe
	case tile == 0xEC: // $6D4D-$6D52: substitutes $E0 for the tile value
		resultB, unsafe = c.boundaryColumnCheckFar(rowAddr, 0xE0)
		return tile, resultB, unsafe
	default:
		return tile, 1, true // $6D49
	}
}

// boundaryColumnCheckNear is the shared sub-check at $6D67-$6D78, used by
// boundaryCheckNear both for a genuinely-read tile in [$E1,$E9) and (via
// $6D7C-$6D81) for a substituted value when the tile is exactly $F3.
func (c *Console) boundaryColumnCheckNear(rowAddr uint16, value byte) (resultB byte, unsafe bool) {
	col := c.readRAM(rowAddr + 2)
	masked := (col + 0x04) & 0xF8
	lowNibble := value & 0x0F
	pre := (col + 0x04) - masked // byte-wrapping; $6D67-$6D75
	if pre >= lowNibble {        // $6D76-$6D77: SUB B; RET NC -- no borrow means safe
		return lowNibble, false
	}
	return 2, true // $6D78
}

// boundaryCheckNear is ported from $6D56-$6D83: the other of the two
// track-edge checks checkOffTrackCollision dispatches to, using a 4px
// column look-ahead offset.
func (c *Console) boundaryCheckNear(rowAddr uint16) (tile, resultB byte, unsafe bool) {
	tile, unsafe = c.checkTrackBoundaryTile(rowAddr, 0x04)
	if !unsafe {
		return tile, 0, false
	}
	switch {
	case tile >= 0xE1 && tile < 0xE9: // $6D5D-$6D65
		resultB, unsafe = c.boundaryColumnCheckNear(rowAddr, tile)
		return tile, resultB, unsafe
	case tile == 0xF3: // $6D7C-$6D81: substitutes $E1 for the tile value
		resultB, unsafe = c.boundaryColumnCheckNear(rowAddr, 0xE1)
		return tile, resultB, unsafe
	default:
		return tile, 2, true // $6D78
	}
}

// checkOffTrackCollision is ported from $6DC1-$6DE9: looks up a per-row
// boundary column from the $E098 RAM table (see
// initPlayerCarSpritesAndFuel for its confirmed flat default) indexed by
// the row at rowAddr, and compares it to the column at rowAddr+2 to
// decide which of the two track-edge checks to run. rowAddr is a real
// parameter, not always $E04C -- the original takes it via HL (`$6DC1:
// PUSH HL; LD A,(HL)`, with the column read via the same HL+2 later,
// `$6DE2: INC HL; INC HL; CP (HL)`), and $70A8's own type-2 object
// handler calls this with HL pointing at an object slot field instead of
// the player's own position (confirmed while porting that handler, since
// an earlier version of this function hardcoded $E04C/$E04E, which was
// only ever proven correct for its two originally-traced call sites).
// Called directly by state 7 ($6C60) and via checkOffTrackCollisionAndCrash
// (state 0's own $6CD1), both with rowAddr=$E04C, as well as $70A8's own
// type-2 handler with rowAddr pointing at the relevant object slot field.
func (c *Console) checkOffTrackCollision(rowAddr uint16) (tile, resultB byte, unsafe bool) {
	band := clampRowBand(c.readRAM(rowAddr), 0x10)
	tableIndex := (band >> 3) & 0x1F
	lookup := c.RAM[0x098+int(tableIndex)]
	boundary := (lookup*2)&0xF8 + 0x20
	if boundary >= c.readRAM(rowAddr+2) { // $6DE6: JP NC,$6D56
		return c.boundaryCheckNear(rowAddr)
	}
	return c.boundaryCheckFar(rowAddr) // $6DE9: JP $6D2A
}

// checkCrashBoundaryTile is ported from $6D09-$6D29: given the raw tile
// value checkOffTrackCollision read, classifies it into 3 bands. tile>=$7C
// is not crash-relevant at all -- report true so
// checkOffTrackCollisionAndCrash continues to its own recovery handling.
// tile in [$74,$78) is a genuine crash: sets $E049=4 (stateCrashSequence)
// and $E0B1=$20. tile in [$78,$7C) is a "bounce": resets speed to $20 and
// redraws the HUD (the same speed-gauge-plus-score-digits tail $6AF4's
// own ending uses). Both of the latter report false -- already fully
// handled, no further action needed this call.
func (c *Console) checkCrashBoundaryTile(tile byte) (continueToRecovery bool) {
	// $6D09-$6D0F: SUB $74 wraps for tile<$74 (byte arithmetic, matching
	// Go's byte type here) -- e.g. tile=$60 computes as $60-$74=$F8, NOT a
	// small number, so it correctly falls through to "not crash-relevant"
	// below along with tile>=$7C. An earlier version of this translation
	// checked "tile>=$7C"/"tile<$78" directly and got this wrong: it
	// treated every tile below $78 as crash-relevant, including ones far
	// below the real $74-$7B band, confirmed wrong by testing tile=$60
	// (which should reach the same "continue to recovery" path as $7C+)
	// and instead incorrectly triggered a crash.
	diff := tile - 0x74
	if diff >= 0x08 { // $6D0C-$6D0F: CP $08; CCF; RET C
		return true
	}
	if diff < 0x04 { // $6D10-$6D1F: tile in [$74,$78)
		c.RAM[0x049] = 4
		c.RAM[0x0B1] = 0x20
		return false
	}
	// diff in [$04,$08): tile in [$78,$7C)
	c.RAM[0x04F] = 0x20 // $6D20-$6D29 (via $6B02: JP $452A)
	c.drawSpeedGaugeBar()
	c.redrawScoreAndHiscoreDigits()
	return false
}

// checkOffTrackCollisionAndCrash is ported from $6CD1-$6D06 plus its own
// $6D09 sub-call: called every frame from updateNormalDriving (state 0)
// once its own steering/sprite update is done. Runs checkOffTrackCollision
// and stashes $E055 (always $FF first; overwritten with the check's own
// `resultB` if the tile was unsafe -- purpose of $E055 itself not
// independently confirmed). If safe, returns immediately. If unsafe,
// checkCrashBoundaryTile may already fully resolve it (a genuine crash or
// a "bounce"); otherwise this sets $E049=1 (the countdown/recovery state,
// $E04A primed to $0C) and, if speed is still high ($E04F>=$A0), resets
// speed and the fuel-pickup dispatcher and redraws the HUD -- otherwise
// sets up a slower recovery ($E049=5, a fixed reference speed, timers
// reset).
//
// The return value mirrors updateSteeringAndCarState's own "abort the
// caller" bool, one level further down: the original's $6D04
// ("POP HL;POP HL;JP $6F1E") discards BOTH this function's own return
// address (back to updateNormalDriving) AND updateNormalDriving's own
// (back to updateSteeringAndCarState's case 0), so the net effect --
// once every level up the call chain propagates this bool through its
// own return -- is exactly the same "stop GameplayTick/advanceCarBehavior
// early this frame" signal, just triggered two levels deeper than the
// single-POP states already documented there.
func (c *Console) checkOffTrackCollisionAndCrash() bool {
	tile, resultB, unsafe := c.checkOffTrackCollision(0xE04C)
	c.RAM[0x055] = 0xFF // $6CD7-$6CDA
	if !unsafe {
		return false // $6CDC: RET NC
	}
	c.RAM[0x055] = resultB // $6CDD

	if !c.checkCrashBoundaryTile(tile) {
		return false // crash or bounce already fully handled
	}

	c.RAM[0x049] = 1 // $6CE2-$6CE5: LD HL,$0C01; LD(E049h),HL
	c.RAM[0x04A] = 0x0C
	if c.RAM[0x04F] >= 0xA0 { // $6CE8-$6CED
		c.RAM[0x04F] = 0 // $6F1E-$6F1F
		c.RAM[0x0B8] = 0 // $6AFE-$6AFF
		c.drawSpeedGaugeBar()
		c.redrawScoreAndHiscoreDigits()
		return true // $6D04-$6D06's own double-abort, see doc comment above
	}
	c.RAM[0x049] = 5    // $6CF0-$6CF2
	c.RAM[0x051] = 0xBC // $6CF5-$6CF7
	c.RAM[0x004] = 0x18 // $6CFA-$6CFC
	c.RAM[0x053] = 0    // $6CFF-$6D00
	return false
}

// addToScore is ported from $447F-$44A4: adds a 2-byte packed-BCD amount
// (`mid`:`lo`) to the current score ($E03F-$E041, see
// redrawScoreAndHiscoreDigits) using real BCD arithmetic (Z80 DAA after
// each byte), gated on $E002 bit 6 (the same bit RunGameStateMachine's
// own stack-trick branch and checkDistanceMilestone both test).
//
// The two exits matter and an earlier pass here only modeled one. On
// overflow (score past six digits) it pegs the *hiscore* bytes
// $E03C-$E03E at $999999 and returns. Otherwise -- the ordinary case --
// $4497's `JR NC` falls into $44A7, the hiscore compare-and-copy (see
// updateHiscore). An earlier version of this comment called the hiscore
// cap "surprising" and possibly an original bug, on the assumption that
// the routine ended at $44A4; with the fall-through traced it is
// consistent, just terse.
//
// CORRECTED: an earlier pass at this port modeled this with a third,
// caller-supplied `hi` parameter (added to $E041, the highest digit
// pair). Real disassembly shows $447F's own first instruction is `LD
// C,$00` -- the register `ADC A,C` later reads for that third byte is
// unconditionally zeroed right there, discarding whatever the caller
// passed before this function even looks at its own gate ($E002 bit 6).
// So the original itself has no way to add anything but a carry to
// $E041 from any call site; a `hi` parameter here could never have any
// observable effect, and keeping one around risked a future caller
// passing a nonzero value that the real ROM never would. Found while
// porting fuelPickupTick's own second addToScore call site and
// double-checking the first one's own assumption instead of reusing it
// uncritically.
func (c *Console) addToScore(mid, lo byte) {
	if c.RAM[0x002]&0x40 == 0 { // $4481-$4485: ADD A,A; RET P
		return
	}
	var c1, c2, c3 bool
	c.RAM[0x03F], c1 = bcdAddCarry(c.RAM[0x03F], lo, false) // $4489-$448C
	c.RAM[0x040], c2 = bcdAddCarry(c.RAM[0x040], mid, c1)   // $448E-$4491
	c.RAM[0x041], c3 = bcdAddCarry(c.RAM[0x041], 0, c2)     // $4493-$4496: ADC A,C -- C is unconditionally $00 (see doc comment)
	if c3 {
		// $4499-$44A3: LD BC,$9999; LD(E03Ch),BC; LD(E03Dh),BC -- two
		// overlapping 16-bit stores (each writes BC's low byte to the
		// named address, high byte to the next one), so all three bytes
		// $E03C-$E03E end up $99, not just two. Those are the *hiscore*
		// bytes: a score that overflows six digits pegs the hiscore at
		// $999999 and stops there, rather than comparing.
		c.RAM[0x03C] = 0x99
		c.RAM[0x03D] = 0x99
		c.RAM[0x03E] = 0x99
		return
	}

	// $4497: JR NC,$44A7 -- the ordinary, no-overflow path falls straight
	// into the hiscore update. Missing this branch is why the hiscore
	// never changed: every other piece (the digits, the label, the RAM
	// layout) was already correct, and nothing ever wrote $E03C-$E03E.
	c.updateHiscore(c.RAM[0x041])
}

// updateHiscore is ported from $44A7-$44CF, reached only by falling out
// of addToScore's own no-overflow path ($4497). `scoreTop` is the score's
// top BCD byte ($E041) as addToScore just wrote it -- the original still
// has it in A, which is why this reads like it takes an argument.
//
// Two things happen here. First ($44A8-$44B6): if scoreTop has reached
// whatever $E044 currently holds, $E044 is advanced by 5 in BCD,
// saturating at $FF instead of wrapping. $E044 is written here and read
// here, and nowhere else in the ROM -- no consumer for it turned up in
// any of the disassembled code -- so what it is *for* is unconfirmed;
// ported for its mechanical effect, in the same spirit as the other
// write-only bytes this port keeps faithfully (see docs/ANALYSIS.md).
// A per-10000-points extra-life or bonus counter would fit the shape,
// but that is a guess and is not recorded as anything more.
//
// Second ($44B7-$44CF): compares the 3-byte hiscore ($E03C-$E03E) against
// the 3-byte score ($E03F-$E041) from the most significant byte down, and
// copies score over hiscore if the score is higher. The original does the
// copy with a single LDDR whose source and destination both walk
// downward, so it is a straight 3-byte copy preserving byte order, not a
// reversal. Equal scores fall out of the compare loop and copy too --
// harmless, since the bytes already match.
func (c *Console) updateHiscore(scoreTop byte) {
	if scoreTop >= c.RAM[0x044] { // $44AB-$44AC: CP (HL); JR C -- skip when below
		v, carry := bcdAddCarry(c.RAM[0x044], 0x05, false) // $44AE-$44B1: ADD A,05h; DAA
		if carry {                                         // $44B2-$44B4: JR NC; LD A,FFh
			v = 0xFF
		}
		c.RAM[0x044] = v // $44B6
	}

	// $44B7-$44C4: 3-byte compare, most significant first. SUB sets carry
	// when the hiscore byte is the smaller one.
	for i := 0; i < 3; i++ {
		hi := c.RAM[0x03E-i]
		sc := c.RAM[0x041-i]
		if hi < sc { // $44BF: JR C -- new high score
			break
		}
		if hi > sc { // $44C1: RET NZ -- existing hiscore still wins
			return
		}
	}

	// $44C6-$44CF: LD BC,3; LD E,$3E; LD L,$41; LDDR
	c.RAM[0x03E] = c.RAM[0x041]
	c.RAM[0x03D] = c.RAM[0x040]
	c.RAM[0x03C] = c.RAM[0x03F]
}

// bcdAddCarry models the Z80's "ADD/ADC then DAA" idiom for one
// packed-BCD byte (two decimal digits per byte): ordinary binary addition
// followed by a decimal adjustment, matching real DAA behavior for the
// addition case.
func bcdAddCarry(a, b byte, carryIn bool) (result byte, carryOut bool) {
	sum := int(a) + int(b)
	if carryIn {
		sum++
	}
	lo := sum & 0x0F
	hi := (sum >> 4) & 0x0F
	if lo > 9 {
		lo -= 10
		hi++
	}
	if hi > 9 {
		hi -= 10
		carryOut = true
	}
	return byte(hi<<4 | lo), carryOut
}

// scrollAndDrawRoadStrip is ported from $5716-$5767: called from $5501's
// per-stage dispatch (the stage 0/1 entry, $5534, calls it last, after
// injectRoadFeature and its as-yet-untraced siblings $56F7/$5944/$54AF
// have all had a chance to write into $E058). Each call, decrements a
// 16-bit read pointer ($E046) by 22, wrapping from $E170 back to $E380
// -- both addresses inside the level/object-layout RAM region
// initTrackLayoutData populates from ROM -- then projects a long stream
// of bytes read from that same pointer directly onto the visible name
// table: 22 columns (columns 1-22, row 0 at $3801) by 24 rows, reading
// 22*24=528 consecutive bytes -- the pointer keeps advancing across all
// 24 rows, it does not re-read the same 22 bytes each row -- with a
// second, independent wraparound (resetting to $E186 if the read
// pointer would land exactly on $E396) checked once per row, not per
// byte. $E170-$E380 is exactly 528 bytes wide, i.e. one full pass
// through this loop consumes exactly one full lap of whatever circular
// buffer that region holds -- strong structural confirmation this is
// the intended mechanism, not a guess.
//
// CORRECTED (found while tracing injectRoadFeature -- see
// docs/ANALYSIS.md): the $5732-$5738 step was ported with its LDIR copy
// direction backwards. The real instructions are `LD HL,E058h; LD
// BC,0016h; LDIR` with DE already holding the just-decremented $E046
// pointer -- Z80 LDIR copies **(HL)->(DE)**, i.e. **from** $E058 **into**
// the circular buffer at the newly-exposed position, not the other way
// around. This is the "inject the freshly composed row into the row
// that's about to scroll into view" step the user's own real-hardware
// description predicted ("injects them onto the top row of the screen
// buffer just before it shifts down") -- with the direction backwards,
// this port was instead overwriting $E058 with old buffer content right
// after injectRoadFeature (and friends) had just written new content
// into it, silently discarding every injection before paintRoadStrip
// (which reads straight from the buffer, not from $E058) ever ran.
func (c *Console) scrollAndDrawRoadStrip() {
	ptr := uint16(c.RAM[0x046]) | uint16(c.RAM[0x047])<<8
	ptr -= 22 // $5719-$571C: wraps like the original's 16-bit ADD of $FFEA
	if ptr == 0xE170 {
		ptr = 0xE380
	}
	c.RAM[0x046] = byte(ptr)
	c.RAM[0x047] = byte(ptr >> 8)

	for i := 0; i < 22; i++ { // $5732-$5738: LD HL,E058h; LD BC,0016h; LDIR -- (HL)->(DE), E058 into the buffer
		c.writeRAM(ptr+uint16(i), c.RAM[0x058+i])
	}

	c.paintRoadStrip(ptr)
}

// paintRoadStrip is the 24-row x 22-column name-table projection shared by
// scrollAndDrawRoadStrip ($574D-$5762's own loop body) and drawRoadStrip
// ($573A's identical loop): starting from src, reads 22 consecutive bytes
// per row into the visible name table from $3801, wrapping the read
// pointer from $E396 back to $E186 once per row (not per byte). The
// original's two versions differ only in how they reach the VDP -- $5716
// via a WRTVRM-equivalent, $573A via raw SETWRT+OUT port I/O -- which is
// an observably identical effect, so both are modeled with the one
// msx.System.WriteVRAM call here.
func (c *Console) paintRoadStrip(src uint16) {
	addr := uint16(0x3801)
	for row := 0; row < 24; row++ {
		var buf [22]byte
		for i := 0; i < 22; i++ { // $574D-$5751
			buf[i] = c.readRAM(src)
			src++
		}
		c.Sys.WriteVRAM(addr, buf[:])
		if src == 0xE396 { // $5753-$575D: wraparound check, once per row
			src = 0xE186
		}
		addr += 0x20 // $5760-$5762
	}
}

// drawRoadStrip is ported from $573A-$5767: called once from
// stateGetReadyWait phase 6 (right after initTrackLayoutData populates the
// road data) and again from stateCrashTrigger ($4337, when a crash leads
// straight into a new race rather than through stateCrashSequence). Unlike
// scrollAndDrawRoadStrip, this does not decrement/wrap the $E046 pointer
// first -- it's a one-shot "paint whatever's currently there" call, not a
// per-frame scroll step.
func (c *Console) drawRoadStrip() {
	ptr := uint16(c.RAM[0x046]) | uint16(c.RAM[0x047])<<8
	c.paintRoadStrip(ptr)
}

// trackRecordData is the raw 105-byte ROM table at $785B-$78C3 that
// initTrackLayoutData's record-copier ($783F) reads from. A "record" is a
// 2-byte header (repeat count, chunk length) followed by exactly
// chunk-length data bytes; several of initTrackLayoutData's calls reuse
// the same offset into this table (reading the identical header+data
// twice, at different destinations) rather than each having their own
// private copy -- confirmed by literally simulating $77D5-$783C's
// register flow instruction by instruction (a standalone scratch
// simulation, not hand arithmetic), not inferred from the shape alone.
var trackRecordData = assets.TrackRecordData // extracted from $785B; see assets/assets.go

// applyTrackRecord replicates one $783F call: reads a repeat-count/chunk-
// length header from trackRecordData at romAddr (an absolute $78xx
// address into the table above), then writes `recB` DIFFERENT
// consecutive recC-byte chunks -- recB*recC data bytes total, not recC
// bytes repeated recB times -- into RAM starting at dst, advancing dst
// by 22 (one road-strip row) after each chunk. Repeats this whole
// recB-chunk pass `width` times, re-reading from the record's own start
// each pass without resetting dst in between -- matching $783F's own
// outer "DJNZ"-free repeat-via-DEC-A loop, which restores the *source*
// pointer every pass (confirmed via its own "PUSH HL"/"POP HL" bracketing
// the whole DJNZ loop) but never resets the destination.
//
// CORRECTED: an earlier pass at this ported the inner DJNZ loop as
// "repeat the same recC-byte chunk recB times", missing that $783F's own
// LDIR (`$784B`) advances its source pointer HL by recC as a normal side
// effect of the copy -- carried unnoticed into every one of the 10
// romAddr values this function is called with, including two that are
// visibly wrong as a result: the "START" record ($7893, recB=2) was
// showing the word doubled across 2 rows, when the real second row is 5
// copies of tile $4F (an underline, not repeated text) -- resolving a
// "START appears twice" discrepancy against a real-hardware screenshot
// that an earlier session had (correctly, on its own terms) concluded
// was a faithful, unfixable ROM-data duplication; and the flag/marker
// record ($78B2, recB=5) was showing 5 identical flags in one column,
// when the real 5 rows are {$A0,$4B},{$A0,$A0},{$4B,$A0},{$A0,$A0},
// {$A0,$4B} -- flags on rows 1/3/5 only, with row 3's shifted one column
// left -- caught from a user's own side-by-side comparison against real
// hardware, not found by this project's own testing. Found by directly
// re-decoding all 10 confirmed romAddr values against the raw ROM file
// under the corrected model and confirming every single one produces
// varied, sensible-looking rows instead of uniform repeats -- not
// theoretical, checked byte-for-byte.
func (c *Console) applyTrackRecord(dst uint16, romAddr uint16, width int) {
	i := int(romAddr) - 0x785B
	recB := int(trackRecordData[i])
	recC := int(trackRecordData[i+1])
	for w := 0; w < width; w++ {
		src := i + 2
		for r := 0; r < recB; r++ {
			for j := 0; j < recC; j++ {
				c.writeRAM(dst+uint16(j), trackRecordData[src+j])
			}
			src += recC
			dst += 22
		}
	}
}

// initTrackLayoutData is ported from $77C8-$785A: called from
// stateGetReadyWait phase 6 ($41C2), right before drawRoadStrip paints the
// result. This is the routine scrollAndDrawRoadStrip's own doc comment
// flagged as missing -- without it, the $E170-$E380 circular buffer that
// routine reads from stays zeroed (a blank road) rather than holding real
// track-curve data.
//
// First, a backward LDDR fill covers $E186-$E3AB (550 bytes) with $A0 --
// almost certainly a "grass/off-track" filler tile, given the confirmed
// on-track tile range is $CF-$D8 (see checkTrackBoundaryTile) and none of
// the record data below is anywhere near that. Then a sequence of
// applyTrackRecord calls punches actual track-surface stripes ($D0/$D1,
// squarely inside that confirmed safe range) into specific rows within
// that filled region, each only touching `chunk length` of the 22 columns
// per row and leaving the rest at the $A0 baseline -- consistent with a
// road that doesn't span the full width every row, i.e. an actual curve.
// The middle destination addresses ($E1B3, $E236, $E206, $E338) aren't
// independently set by the caller -- they're derived from carrying over
// the low byte of a previous call's own final destination pointer, the
// same register-reuse idiom $783F's own multi-outer-pass loop uses
// internally, confirmed the same way (direct register-flow simulation,
// not guessed). Two short mid-sequence blocks ($77F8-$7803, $7822-$782A)
// are plain direct pokes/LDIR fills of a single repeated byte ($4E, with
// a trailing $56 on the first one) rather than table-driven records --
// their semantic purpose (lane markers? a finish-line tile?) isn't
// confirmed, only their mechanical effect.
//
// One whole call ($77EE-$77F5: applyTrackRecord(0xE236, 0x78A5, 15)) was
// missing from an earlier pass at this function -- caught by a
// real-hardware screenshot showing continuous roadside scenery where
// this port showed plain grass for most of the left margin, then
// confirmed for real with a fresh, from-scratch register-flow simulation
// of the entire $77C8-$783C sequence (not a hand-patch of the missing
// site alone), which reproduced all 9 already-ported calls' exact
// (romAddr, dst, width) triples byte-for-byte and additionally surfaced
// this 10th one sitting between the 3rd and 5th. It writes the record at
// $78A5 (`$4C,$4C,$4D,$4D,$4D`, the same tile-code family as the
// still-unexplained $4C/$4D reuse on the road's right edge -- see
// docs/ANALYSIS.md) to column 1 (the road's *left* edge) for 15 rows
// (rows 8-22) -- i.e. this is very likely the left-side counterpart of
// that same roadside barrier graphic, previously entirely absent rather
// than merely misrendered.
func (c *Console) initTrackLayoutData() {
	for addr := uint16(0xE186); addr <= 0xE3AB; addr++ { // $77C8-$77D3
		c.writeRAM(addr, 0xA0)
	}

	c.applyTrackRecord(0xE18E, 0x7875, 6)  // $77D5-$77DC
	c.applyTrackRecord(0xE198, 0x78B2, 1)  // $77DF-$77E5
	c.applyTrackRecord(0xE1B3, 0x78B2, 1)  // $77E8-$77EB
	c.applyTrackRecord(0xE236, 0x78A5, 15) // $77EE-$77F5 -- see doc comment above

	for i := uint16(0); i < 5; i++ { // $77F8-$7803: direct fill, not a record -- dst derived from the call above's own leftover DE ($E380), not independent
		c.writeRAM(0xE380+i, 0x4E)
	}
	c.writeRAM(0xE385, 0x56)

	c.applyTrackRecord(0xE251, 0x78BE, 14) // $7804-$780C
	c.applyTrackRecord(0xE18A, 0x785B, 1)  // $780F-$7815
	c.applyTrackRecord(0xE206, 0x789F, 13) // $7818-$781F

	for i := uint16(0); i < 3; i++ { // $7822-$782A: direct fill, not a record
		c.writeRAM(0xE325+i, 0x4E)
	}

	c.applyTrackRecord(0xE331, 0x7893, 1) // $782C-$7831
	c.applyTrackRecord(0xE367, 0x78AC, 1) // $7834-$7839
	c.applyTrackRecord(0xE338, 0x78AC, 1) // $783C: falls through into $783F directly
}

// updateRoadAndScore is ported from $5501-$5544 (the stage 0/1 path
// only): GameplayTick's own $5242 dispatch (also reached directly from
// advanceCarBehavior's own $5289).
//
// CORRECTED: an earlier pass at this misread the entry gate as "only
// runs during car-behavior state 1 (off-track recovery)". The real
// instructions are `LD A,(E049h); DEC A; RET Z` -- that returns early
// (does nothing) exactly when E049 WAS 1, and falls through to the real
// body for every OTHER state, including 0 (normal driving). So this is
// backwards from the original doc comment: it runs on every normal
// driving frame and specifically sits out the off-track-recovery state,
// not the other way around. Caught by tracing the raw disassembly again
// while investigating a real "road never scrolls" report -- see
// docs/ANALYSIS.md's road-scroll write-up for the full history of what
// this bug caused (an exhaustive, and until now seemingly conclusive,
// proof that scrollAndDrawRoadStrip's only call site was unreachable).
//
// Every other frame (gated on whether the distance counter's own low
// bit, $E078, has flipped since the last time $E07E was updated to
// match it), adds 3 points to the score (addToScore) and re-stages
// $E058 from a per-stage fill byte ($E089, role not confirmed) before
// dispatching on the current stage ($E043). The stage 0/1 entry
// ($5534) ends in scrollAndDrawRoadStrip, which is what actually paints
// the road every qualifying frame -- there is no separate rate limiter
// gating *that* call specifically; how fast the road visibly advances is
// governed entirely by how often this whole function's own outer gate
// (the $E078 bit-flip check above) passes, which in turn is governed by
// how fast $E078 itself increments, i.e. current speed
// (accelerateCarSpeed) -- already correctly wired, not a gap.
// injectRoadFeature ($58AF), injectRoadCurve ($56F7, including its own
// advanceBoundaryHistoryTable prefix), injectCheckpointText ($5944), and
// injectMilestoneSign ($54AF) are all now fully ported -- see their own
// doc comments.
//
// Stage 2 ($5545) is now ported too (stage2RoadFeatureStep): after the
// same injectRoadFeature call stage 0/1 makes, it additionally reads a
// flat 4-byte-at-a-time ROM stream (stage2And4FeatureStream, via the
// $E08A cursor armRoadFeatureStream now arms) into $E058, then rejoins
// the shared tail from injectRoadCurve onward.
//
// Stage 3 ($5563) is now ported too (stage3RoadFeatureStep): renders the
// full 22-byte staging row from a $E090-pointed 16-bit pattern word
// (renderRoadPatternBits, shared with stage 6), then overwrites its
// middle 15 bytes from one of two 48-byte ROM streams (stage3StreamA/B)
// selected by a flip-flop, using $E06F -- the same field injectRoadCurve's
// own default $5998 table normally occupies, confirmed still safe here
// since stage 3's own armRoadFeatureStream override replaces it for the
// entire stage, and injectRoadCurve is never reached from stage 3's own
// dispatch branch. Every piece of this (both new ROM streams, the word
// stream, the whole reload state machine) independently verified against
// the real ROM via a 30-call direct-call trace matching this port's own
// output exactly.
//
// Stage 6 ($56AC) is now ported too (stage6RoadFeatureStep) -- much
// simpler than stage 3's, since it doesn't touch $E06F at all: just
// renderRoadPatternBits (reused as-is) plus its own pattern-word stream
// (stage6PatternWordStream, $7FE4) and $E090 wraparound, then rejoins
// the shared tail from its very start (`$56C9: JP $5534`, i.e. stage 6
// gets injectRoadFeature/injectRoadCurve too, unlike stages 2 and 3
// which rejoin partway through). Verified against the real ROM the same
// way, isolating the prefix's own effect from the rest of the shared
// tail by stopping a direct-call trace right at the `JP $5534` boundary.
//
// Stages 4 and 5's own shared prefix ($5600) is ported as
// stage45RoadFeatureStep: a nested pair of countdowns ($E08D, then $E08C
// once $E08D reaches 0) gates when a new one of 4 fixed-format ROM
// entries (roadStrip7ByteEntries, from the $569C table -- each a "count"
// byte plus count*7 data bytes, packed back-to-back with zero gap,
// confirmed exactly) gets selected, versus just continuing to read 7
// more bytes from whichever entry is already active.
//
// Stage 4 ($55E9, stage4RoadFeatureStep) reuses the same 7 bytes
// $5600 just returned: adds $0D to each and writes them into
// $E068-$E06E in reverse order, then rejoins the shared tail from
// injectRoadCurve. Stage 5 ($5642, stage5RoadFeatureStep) calls
// injectRoadCurve and $5600 as ordinary subroutines (not tail-jumps),
// then runs a second, independent entry-selector mechanism: $E094
// tracks a cursor into one of 4 *different* ROM entries
// (road56A4Entries, from the $56A4 table -- no leading "count" byte
// this time, and reload cadence driven separately by $E092/$E090
// instead of the entry's own data), and finally a register-shadow
// (`EXX`, modeled here with an ordinary local instead) peek of $E090's
// current byte decides whether 4 more bytes get discarded or copied,
// reversed, into $E06B-$E06E.
//
// All six of $E043's possible dispatch values (0-1 shared, 2-6
// individually) are now ported, completing $5501 -- confirmed at least
// as large as $6A70/$6CA1/$6CD1 combined, a genuinely multi-session-scale
// routine. Every stage's own new mechanism was verified against the real
// ROM via direct-call traces isolating each prefix's own effect (stopping
// right at its own tail-jump boundary) and matching this port's output
// byte-for-byte -- which is what caught two real bugs along the way: a
// same-call $E08A advance in stage 4/5's shared prefix that neither
// reading the disassembly by hand nor a first real-ROM run anticipated,
// and (found via stage 5's own heavier, ungated consumption finally
// reaching bytes a lighter stage-2-only test never had cause to) a
// transcription error in stage2And4FeatureStream itself -- see its own
// doc comment.
//
// The bool return mirrors updateSteeringAndCarState's own "abort the
// caller" convention: the stage 0/1 entry's own ending ("POP HL; RET")
// discards its immediate caller's return address the same way states 1,
// 3, and 6 do there.
func (c *Console) updateRoadAndScore() bool {
	if c.RAM[0x049] == 1 { // $5501-$5505: DEC A; RET Z (on E049-1)
		return false
	}
	bit := c.RAM[0x078] & 1
	if c.RAM[0x07E]&1 == bit { // $5508-$5513
		return false
	}
	c.RAM[0x07E] ^= 1  // $5514-$5516
	c.addToScore(0, 3) // $5517-$551A: LD DE,$0003; CALL $447F
	return c.runRoadFeatureDispatch()
}

// runRoadFeatureDispatch is ported from $551D-$5544ff, the shared tail
// updateRoadAndScore's own preamble falls into -- and a genuine, separate
// entry point in its own right: stateCrashSequence's phase 2 reaches it
// directly too ($428A-$428E, via a doubled return-address push whose net
// effect is just an ordinary call -- the pushed address is a bare `RET`,
// so landing on it and immediately returning is observably identical to
// returning straight to the original caller). This is why the road keeps
// visibly scrolling during the post-lap "crash sequence" animation: it's
// the exact same per-frame road/feature update as normal driving, just
// invoked directly every frame instead of through updateRoadAndScore's
// own $E078-rate-limited entry.
func (c *Console) runRoadFeatureDispatch() bool {
	c.fillRoadRowDefault() // $551D: CALL $592A -- see its own doc comment

	switch c.RAM[0x043] { // $5520-$5523: dispatch via $4045 on the stage number
	case 0, 1:
		c.injectRoadFeature()      // $5534: CALL $58AF -- see its own doc comment
		c.injectRoadCurve()        // $5537: CALL $56F7 -- see its own doc comment; includes advanceBoundaryHistoryTable
		c.injectCheckpointText()   // $553A: CALL $5944 -- see its own doc comment
		c.injectMilestoneSign()    // $553D: CALL $54AF -- see its own doc comment
		c.scrollAndDrawRoadStrip() // $5540: CALL $5716
		return true                // $5543-$5544: POP HL; RET
	case 2:
		c.injectRoadFeature()     // $5545: CALL $58AF -- same as stage 0/1's own first call
		c.stage2RoadFeatureStep() // $5548-$555D -- see its own doc comment
		c.injectRoadCurve()       // $5560: JP $5537 -- rejoins stage 0/1's shared tail
		c.injectCheckpointText()
		c.injectMilestoneSign()
		c.scrollAndDrawRoadStrip()
		return true
	case 3:
		c.stage3RoadFeatureStep() // $5563-$55E7 -- see its own doc comment; ends with a JP into $553A
		c.injectCheckpointText()
		c.injectMilestoneSign()
		c.scrollAndDrawRoadStrip()
		return true
	case 4:
		c.stage4RoadFeatureStep() // $55E9-$55FD -- see its own doc comment; ends with a JP into $5537
		c.injectRoadCurve()
		c.injectCheckpointText()
		c.injectMilestoneSign()
		c.scrollAndDrawRoadStrip()
		return true
	case 5:
		c.stage5RoadFeatureStep() // $5642-$5699 -- see its own doc comment; ends with a JP into $553A
		c.injectCheckpointText()
		c.injectMilestoneSign()
		c.scrollAndDrawRoadStrip()
		return true
	case 6:
		c.stage6RoadFeatureStep() // $56AC-$56C6 -- see its own doc comment; ends with a JP into $5534
		c.injectRoadFeature()
		c.injectRoadCurve()
		c.injectCheckpointText()
		c.injectMilestoneSign()
		c.scrollAndDrawRoadStrip()
		return true
	}
	return false
}

// stage2RoadFeatureStep is ported from $5545-$5560's own middle section
// (the part beyond the shared injectRoadFeature call, see
// updateRoadAndScore): reads the next 4 bytes from
// stage2And4FeatureStream via the $E08A cursor into $E058 (the road-strip
// staging row's own first 4 bytes), wrapping back to the stream's start
// on its terminator. The terminator check happens BEFORE the copy, using
// whichever byte the cursor currently points at, so the very first 4
// bytes copied right after a wrap come from the fresh restart, not the
// terminator itself -- confirmed by the exact instruction order ($554B:
// LD A,(HL) / INC A / JR NZ, all before the LDIR at $555B).
func (c *Console) stage2RoadFeatureStep() {
	ptr := uint16(c.RAM[0x08A]) | uint16(c.RAM[0x08B])<<8
	if int(ptr-stage2And4FeatureStreamBase) >= len(stage2And4FeatureStream) {
		ptr = stage2And4FeatureStreamBase
	}
	copy(c.RAM[0x058:0x05C], stage2And4FeatureStream[ptr-stage2And4FeatureStreamBase:])
	ptr += 4
	c.RAM[0x08A] = byte(ptr)
	c.RAM[0x08B] = byte(ptr >> 8)
}

// stage3PatternWordStream is the 6-word (12-byte) ROM stream at $7C85,
// terminated by a real $FF at $7C91 (confirmed by scanning the actual
// ROM bytes). Each 16-bit word feeds renderRoadPatternBits, one bit at a
// time from the top, via the $E090 cursor.
var stage3PatternWordStream = [6]uint16{0x9240, 0x4920, 0x0000, 0x2490, 0x0000, 0x0000}

var stage3PatternWordStreamBase uint16 = 0x7C85

// stage6PatternWordStream is stage 6's own equivalent of
// stage3PatternWordStream: a 6-word (12-byte) ROM stream at $7FE4,
// terminated by a real $FF at $7FF0 (confirmed the same way, by scanning
// the actual ROM bytes).
var stage6PatternWordStream = [6]uint16{0x0000, 0x0000, 0x2220, 0x0000, 0x0000, 0x8880}

var stage6PatternWordStreamBase uint16 = 0x7FE4

// patternWordAt looks up the 16-bit pattern word at a $E090 pointer
// value, dispatching to whichever of stage3PatternWordStream/
// stage6PatternWordStream actually contains it -- the two stages share
// renderRoadPatternBits, but each arms $E090 with its own stream
// (armRoadFeatureStream), and the two ROM ranges don't overlap.
func patternWordAt(ptr uint16) uint16 {
	if idx := int(ptr - stage3PatternWordStreamBase); idx >= 0 && idx < 2*len(stage3PatternWordStream) {
		return stage3PatternWordStream[idx/2]
	}
	return stage6PatternWordStream[(ptr-stage6PatternWordStreamBase)/2]
}

// stage3StreamA and stage3StreamB are the two 48-byte ROM streams at
// $7C23 and $7C54 respectively, each terminated by a real $FF
// immediately past its own 48th byte ($7C53 and $7C84 -- confirmed by
// scanning the actual ROM bytes, and matching the "rewind by $30"
// mechanic stage3RoadFeatureStep's own reload logic uses: hitting the
// terminator rewinds $E06F by exactly 48 bytes, landing back on the
// stream's own start). Fed 4 bytes at a time (twice per 8-byte "reload"
// consume, see stage3RoadFeatureStep) into the road-strip staging row.
var stage3StreamA = [48]byte{
	0x94, 0x95, 0x96, 0x97, 0x98, 0xA9, 0x96, 0xAA, 0x90, 0x91, 0x92, 0x93, 0x99, 0xA6, 0xA7, 0xA8,
	0x8E, 0x8B, 0xAB, 0x8F, 0x98, 0xA4, 0xA2, 0xA5, 0x8A, 0x8B, 0x8C, 0x8D, 0x99, 0xA1, 0xA2, 0xA3,
	0x86, 0x87, 0x88, 0x89, 0x98, 0x9E, 0x9F, 0xA0, 0x82, 0x83, 0x84, 0x85, 0x99, 0x9C, 0x84, 0x9D,
}
var stage3StreamB = [48]byte{
	0x04, 0x04, 0x04, 0x80, 0x98, 0x9A, 0x9B, 0x04, 0x04, 0x9B, 0x04, 0x81, 0x99, 0x01, 0x04, 0x04,
	0x04, 0x04, 0x04, 0x80, 0x98, 0x9A, 0x04, 0x04, 0x04, 0x9B, 0x04, 0x81, 0x99, 0x01, 0x9B, 0x04,
	0x9B, 0x04, 0x04, 0x80, 0x98, 0x9A, 0x04, 0x9B, 0x04, 0x04, 0x9B, 0x81, 0x99, 0x01, 0x04, 0x04,
}

var stage3StreamABase uint16 = 0x7C23
var stage3StreamBBase uint16 = 0x7C54

// stage3StreamAt classifies a $E06F pointer value as belonging to
// stage3StreamA or stage3StreamB (the two streams sit back to back in
// ROM, A below B, so a simple address comparison tells them apart) and
// returns the corresponding Go slice plus the pointer's own offset into
// it. atEnd reports whether that offset has reached the stream's real
// $FF terminator -- checked via Go slice bounds rather than dereferencing
// the ROM address directly (this port has no general ROM-image accessor;
// only the specific bytes extracted into these Go arrays), the same
// convention stage2RoadFeatureStep's own bounds check already uses.
func stage3StreamAt(addr uint16) (stream []byte, offset int, atEnd bool) {
	if addr < stage3StreamBBase {
		return stage3StreamA[:], int(addr - stage3StreamABase), int(addr-stage3StreamABase) >= len(stage3StreamA)
	}
	return stage3StreamB[:], int(addr - stage3StreamBBase), int(addr-stage3StreamBBase) >= len(stage3StreamB)
}

// renderRoadPatternBits is ported from $56CC/$56CE (two entry points
// into one routine: $56CC sets B=$0C=12 then falls into $56CE; stage 6
// calls $56CE directly with its own B=$0A=10, see stage6RoadFeatureStep).
// Reads a 16-bit "pattern word" from the address stored at $E090/$E091
// (a pointer, not the word itself -- $E090 walks a stream of such
// pointers' *targets* two bytes at a time, see stage3RoadFeatureStep and
// stage6RoadFeatureStep, each of which arms its own stream), via
// patternWordAt (which dispatches to whichever of
// stage3PatternWordStream/stage6PatternWordStream actually contains the
// current pointer), then for
// `count` iterations shifts it left one bit and writes one tile per
// iteration into the road-strip staging row starting at `dst`: carry set
// (the bit shifted out was 1) writes $80, clear writes $0F -- except on
// stage 3, which uses $9B/$04 instead (confirmed: $E043==3 is checked
// fresh on every iteration of the original's own loop, not hoisted
// outside it, though the practical effect is identical either way since
// the stage doesn't change mid-loop). The word itself is re-read fresh
// from $E090 on each call (not persisted across calls), so calling this
// twice with the same $E090 value -- as stage 3 does, once for 12 bits
// then again for 10 -- produces the same bit sequence twice, not a
// continuation into the word's own remaining low bits.
func (c *Console) renderRoadPatternBits(dst int, count int) {
	ptr := uint16(c.RAM[0x090]) | uint16(c.RAM[0x091])<<8
	word := patternWordAt(ptr)
	tileA, tileB := byte(0x80), byte(0x0F)
	if c.RAM[0x043] == 3 {
		tileA, tileB = 0x9B, 0x04
	}
	for i := 0; i < count; i++ {
		tile := tileB
		if word&0x8000 != 0 {
			tile = tileA
		}
		c.RAM[dst] = tile
		dst++
		word <<= 1
	}
}

// stage3RoadFeatureStep is ported from $5563-$55E7, updateRoadAndScore's
// stage 3 handler.
//
// Renders the full 22-byte road-strip staging row from the current
// $E090-pointed pattern word (renderRoadPatternBits, 12 bits into
// $E058-$E063 then 10 more into $E064-$E06D), then advances the $E090
// cursor by 2 bytes (through stage3PatternWordStream), wrapping back to
// its own start on a real $FF terminator.
//
// Then peeks the byte at the CURRENT $E06F pointer ($5583-$5585: `LD
// A,(HL); INC A; JR NZ,$55B3`). CORRECTED from an earlier draft of this
// port that read this peek as independent of what follows: it isn't --
// when the peeked byte is NOT the $FF terminator, this jumps straight to
// the 8-byte consume tail below, skipping the rewind AND the reload
// countdown entirely for that call. Only on a terminator hit does
// execution fall through into rewinding $E06F by 48 bytes ($5587-$558B:
// `LD BC,$FFD0; ADD HL,BC` -- confirmed as -$30 by the two's-complement
// arithmetic, landing exactly back at whichever of stage3StreamA/B's own
// 48-byte start $E06F was already in, a modulo-48 wrap with no
// separately stored base address needed) *and* running the reload
// countdown at $E08A. Verified against the real ROM: a direct-call trace
// showed $E08A/$E07C staying constant for 6 calls in a row (each stream
// is exactly 48 bytes, consumed 8 bytes/call) before changing exactly on
// the 7th, matching this gated model and ruling out the earlier draft's
// "decrements and maybe reloads every single call" reading.
//
// Once $E08A reaches 0: toggles a flip-flop at $E07C's own bit 0, then
// picks a fresh $E06F/$E08A/$E08B based on the flip-flop's NEW value --
// 0 selects stage3StreamA with a 1-call hold (E08A=1, so the very next
// terminator hit, one full 48-byte pass later, immediately reloads
// again), 1 selects stage3StreamB with a 16-call hold (E08A=$10, i.e.
// roughly 2-3 full 48-byte passes through stream B before flipping back).
// If $E08A *hasn't* reached 0 yet (the common case, mid-pass through
// whichever stream is current), $E08B is unconditionally reset to 6
// instead ($55E4-$55E5) -- confirmed instruction, role not independently
// confirmed beyond "written here," since nothing in this handler reads
// $E08B back.
//
// Either way, consumes 8 bytes from $E06F's current position: 4 bytes
// into $E05C-$E05F, then a flat fill of $E060-$E066 (7 bytes) with $D0
// (the same "seed one byte, LDIR-propagate" idiom used elsewhere in this
// file), then 4 more stream bytes into $E067-$E06A -- overwriting the
// middle of the pattern-rendered row from above, leaving only its first
// 4 and last 3 bytes showing the original bit-rendered pattern. Advances
// $E06F by 8 (not 15 -- the $D0 fill doesn't consume stream bytes).
//
// Finally increments $E08C (a free-running counter) and, on 2 out of
// every 4 values of its own low 2 bits, overwrites $E063 with $D1 --
// plausibly a blinking warning light or similar (Road Fighter's real
// content makes a railroad-crossing gate a strong guess for what this
// whole mechanism draws, given the alternating bar pattern and this
// on/off blink, but that's inference from shape, not confirmed against
// a rendered screen the way roadFeatureTableStage145's own content was).
func (c *Console) stage3RoadFeatureStep() {
	c.renderRoadPatternBits(0x058, 12) // $5563-$5569
	c.renderRoadPatternBits(0x064, 10) // $5569-$556D

	ptr := uint16(c.RAM[0x090]) | uint16(c.RAM[0x091])<<8
	ptr += 2 // $556E-$5573
	c.RAM[0x090] = byte(ptr)
	c.RAM[0x091] = byte(ptr >> 8)
	if int(ptr-stage3PatternWordStreamBase) >= 2*len(stage3PatternWordStream) { // $5576-$557D
		ptr = stage3PatternWordStreamBase
		c.RAM[0x090] = byte(ptr)
		c.RAM[0x091] = byte(ptr >> 8)
	}

	e06f := uint16(c.RAM[0x06F]) | uint16(c.RAM[0x070])<<8
	if _, _, atEnd := stage3StreamAt(e06f); atEnd { // $5583-$5585: peek; only NOT jumping straight to the consume tail below when the terminator is hit
		e06f -= 0x30 // $5587-$558B: rewind

		c.RAM[0x08A]-- // $558E-$5591 -- the reload countdown only ever runs on a terminator-hit call, gated together with the rewind above, not independently every call
		if c.RAM[0x08A] != 0 {
			c.RAM[0x08B] = 6 // $55E4-$55E5
			c.RAM[0x06F] = byte(e06f)
			c.RAM[0x070] = byte(e06f >> 8)
		} else {
			flag := (c.RAM[0x07C] & 1) ^ 1 // $5597-$559C
			c.RAM[0x07C] = flag
			if flag == 0 { // $55A3-$55AB
				e06f = stage3StreamABase
				c.RAM[0x08A], c.RAM[0x08B] = 1, 6
			} else {
				e06f = stage3StreamBBase
				c.RAM[0x08A], c.RAM[0x08B] = 0x10, 6
			}
			c.RAM[0x06F] = byte(e06f)
			c.RAM[0x070] = byte(e06f >> 8)
		}
	}

	stream, off, _ := stage3StreamAt(e06f)
	copy(c.RAM[0x05C:0x060], stream[off:])    // $55B3-$55BC
	for i := uint16(0x060); i <= 0x066; i++ { // $55BE-$55C6: seed+LDIR flat fill
		c.RAM[i] = 0xD0
	}
	copy(c.RAM[0x067:0x06B], stream[off+4:]) // $55C8-$55CB
	e06f += 8                                // $55CD
	c.RAM[0x06F] = byte(e06f)
	c.RAM[0x070] = byte(e06f >> 8)

	c.RAM[0x08C]++          // $55D0-$55D3
	if c.RAM[0x08C]&3 < 2 { // $55D5-$55D9
		c.RAM[0x063] = 0xD1 // $55DC-$55DF
	}
}

// stage6RoadFeatureStep is ported from $56AC-$56C6, updateRoadAndScore's
// stage 6 handler -- much simpler than stage 3's, since it doesn't touch
// $E06F at all: renders the full 22-byte staging row from the current
// $E090-pointed pattern word (renderRoadPatternBits, shared with stage
// 3, 12 bits then 10 more -- see its own doc comment), advances the
// $E090 cursor by 2 bytes through stage6PatternWordStream, and wraps
// back to the stream's own start on its real $FF terminator. Ends by
// rejoining the shared tail from its very start (`$56C9: JP $5534`),
// i.e. stage 6 gets injectRoadFeature/injectRoadCurve too, unlike
// stages 2 and 3 which rejoin partway through.
func (c *Console) stage6RoadFeatureStep() {
	c.renderRoadPatternBits(0x058, 12) // $56AC-$56B2
	c.renderRoadPatternBits(0x064, 10) // $56B2-$56B7

	ptr := uint16(c.RAM[0x090]) | uint16(c.RAM[0x091])<<8
	ptr += 2 // $56B7-$56BC
	c.RAM[0x090] = byte(ptr)
	c.RAM[0x091] = byte(ptr >> 8)
	if int(ptr-stage6PatternWordStreamBase) >= 2*len(stage6PatternWordStream) { // $56BF-$56C6
		ptr = stage6PatternWordStreamBase
		c.RAM[0x090] = byte(ptr)
		c.RAM[0x091] = byte(ptr >> 8)
	}
}

// roadStrip7ByteEntry is one entry of the 4-entry ROM table at $569C,
// each a "count" byte followed by count*7 data bytes, packed back-to-back
// in ROM with zero gap -- confirmed exactly: each entry's own byte
// length (1+count*7) lands precisely on the next entry's own address,
// for all 4 entries in a row, the same standard of proof this project
// already applied to roadFeatureTableStage145.
type roadStrip7ByteEntry struct {
	count byte
	data  []byte
}

var roadStrip7ByteEntries = [4]roadStrip7ByteEntry{
	{0x02, []byte{0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0x09, 0x09, 0xA1, 0xA3, 0xA4, 0xA5, 0xA7, 0x09, 0x09}},
	{0x02, []byte{0xAB, 0xAC, 0xAD, 0xAE, 0xAF, 0x09, 0x09, 0xA9, 0xA3, 0xA4, 0xA5, 0xAA, 0x09, 0x09}},
	{0x1D, assets.RoadStripEntry3}, // extracted from $7CB1; see assets/assets.go
	{0x02, []byte{0x9A, 0xA3, 0xA4, 0xA5, 0xA0, 0x09, 0x09, 0x9B, 0x9C, 0x9D, 0x9E, 0x9F, 0x09, 0x09}},
}

// stage45SelectorStreamA and stage45SelectorStreamB are stage 4 and 5's
// own per-stage byte streams (armRoadFeatureStream arms $E08A/$E08B with
// $7E7C for stage 4, $7D8A for stage 5 -- see stage45SharedInitTable).
// Each byte's top 6 bits (>>2) become a new $E08C reload count and its
// low 2 bits select one of roadStrip7ByteEntries above; consumed one
// byte at a time, but only once BOTH $E08D and $E08C have separately
// counted down to 0 (see stage45RoadFeatureStep).
//
// UNCONFIRMED EXTENT: unlike every other stream this session extracted
// (stage2/3/6's, all bounded by a real $FF terminator byte, confirmed by
// scanning for it), nothing in $5600's own disassembly checks for a
// terminator here -- the advance is a plain `INC HL` with no bounds
// check at all. Only a 64-byte prefix of each stream is extracted below,
// enough to cover any realistic test: stage 4's own stream opens with
// seven $FE bytes in a row, each selecting the 204-byte/29-count entry
// with a $3F/63-cycle hold -- 63*29 = 1827 qualifying calls before even
// the FIRST advance, let alone the seven in a row -- so in practice
// stage 4 will essentially never be observed selecting anything but that
// one entry. Stage 5's own stream is far more active (holds as short as
// 2 calls), so it exercises the advance path within a normal test run,
// and is what this port's own real-ROM verification actually used.
var stage45SelectorStreamA = [64]byte{
	0xFE, 0xFE, 0xFE, 0xFE, 0xFE, 0xFE, 0xFE, 0x10, 0x29, 0x11, 0x09, 0x13, 0x18, 0x20, 0x31, 0x24,
	0x18, 0x10, 0x19, 0x13, 0x18, 0x20, 0xFB, 0x20, 0x6F, 0x24, 0x18, 0x10, 0x19, 0x12, 0x11, 0x10,
	0x49, 0x11, 0x09, 0x13, 0x18, 0x20, 0xC1, 0x24, 0x18, 0x10, 0x19, 0xFE, 0x10, 0x31, 0x13, 0x18,
	0x20, 0x61, 0x21, 0x29, 0x24, 0x18, 0x10, 0x21, 0x13, 0x18, 0x20, 0x19, 0x22, 0x29, 0x20, 0x51,
}
var stage45SelectorStreamABase uint16 = 0x7E7C

var stage45SelectorStreamB = [64]byte{
	0x09, 0x3E, 0x05, 0x58, 0x07, 0x16, 0x05, 0x4C, 0x07, 0x2A, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5,
	0xC5, 0xC5, 0xB5, 0xC5, 0xC5, 0xC5, 0xB4, 0xC5, 0xC5, 0xC5, 0xB3, 0xC5, 0xC5, 0xC5, 0xC7, 0xB5,
	0xC5, 0xC5, 0xBD, 0xB4, 0xC5, 0xC5, 0xBB, 0xC7, 0xB5, 0xC5, 0xC6, 0xBC, 0xB4, 0xC5, 0xC2, 0xBB,
	0xB3, 0xC5, 0xC6, 0xC6, 0xBC, 0xB5, 0xC6, 0xC4, 0xBA, 0xB4, 0xFF, 0xC6, 0xC6, 0xB9, 0xB1, 0xC6,
}
var stage45SelectorStreamBBase uint16 = 0x7D8A

// stage45SelectorByteAt reads one byte from whichever of
// stage45SelectorStreamA/B applies for the current stage, given a
// pointer value read back from $E08A/$E08B. Panics if the pointer has
// walked past the extracted 64-byte prefix -- see the "UNCONFIRMED
// EXTENT" note on those streams; this is a deliberate loud failure
// rather than silently returning wrong data, since reaching this point
// would mean this port's own extracted prefix genuinely wasn't long
// enough for real play, worth learning about immediately rather than
// papering over.
func stage45SelectorByteAt(stage byte, ptr uint16) byte {
	if stage == 4 {
		return stage45SelectorStreamA[ptr-stage45SelectorStreamABase]
	}
	return stage45SelectorStreamB[ptr-stage45SelectorStreamBBase]
}

// stage45RoadFeatureStep is ported from $5600-$5641, the prefix stages 4
// and 5 share (called from each one's own updateRoadAndScore handler
// before their own further stage-specific logic -- see
// docs/ANALYSIS.md).
//
// Every call, unconditionally: decrements a countdown at $E08D. If it's
// still nonzero, skips straight to copying the next 7 bytes from the
// current entry (stage45DataCursor, modeling $E08E/$E08F -- see its own
// doc comment on the Console struct) into $E058-$E05E.
//
// Once $E08D reaches 0: decrements a second, independent countdown at
// $E08C. If THAT also reaches 0: advances the $E08A/$E08B stream cursor
// by one byte and reads the new byte's top 6 bits into $E08C as a fresh
// reload count (stage45SelectorByteAt). Either way (whether or not
// $E08A just advanced), reads the CURRENT byte at $E08A/$E08B again and
// uses its low 2 bits to select one of the 4 roadStrip7ByteEntries:
// $E08D is set to that entry's own count, stage45DataCursor is pointed
// at its data, and (falling through to the same tail as the "$E08D
// still nonzero" path) the first 7 bytes are copied into the staging
// row. This can re-select the SAME entry it was already on (if $E08A
// didn't just advance) -- restarting that entry's own data from the
// beginning, not continuing from wherever a previous pass left off.
//
// Returns the 7 bytes just copied: stage 4's own handler ($55E9) reuses
// them directly rather than re-reading $E058 (the original does this by
// simply not overwriting HL, which $5600's own LDIR conveniently leaves
// pointing just past the 7 bytes it copied -- HL survives a Z80 CALL/RET
// with no explicit return-value passing needed; this port makes that
// implicit reuse explicit instead).
func (c *Console) stage45RoadFeatureStep() []byte {
	c.RAM[0x08D]--         // $5600-$5603
	if c.RAM[0x08D] != 0 { // $5604
		return c.stage45CopySevenBytes()
	}

	c.RAM[0x08C]--         // $5606-$5607
	if c.RAM[0x08C] == 0 { // $5608
		ptr := uint16(c.RAM[0x08A]) | uint16(c.RAM[0x08B])<<8
		ptr++ // $560A-$560E
		c.RAM[0x08A] = byte(ptr)
		c.RAM[0x08B] = byte(ptr >> 8)
		b := stage45SelectorByteAt(c.RAM[0x043], ptr)
		c.RAM[0x08C] = (b >> 2) & 0x3F // $5611-$5619
	}

	ptr := uint16(c.RAM[0x08A]) | uint16(c.RAM[0x08B])<<8 // $561A-$561D
	b := stage45SelectorByteAt(c.RAM[0x043], ptr)
	entry := roadStrip7ByteEntries[b&0x03] // $561E-$5627
	c.RAM[0x08D] = entry.count             // $562B-$562E
	c.stage45DataCursor = entry.data       // $5630
	return c.stage45CopySevenBytes()
}

// stage45CopySevenBytes is $5633-$5641, the tail every path through
// stage45RoadFeatureStep shares: copies 7 bytes from stage45DataCursor
// into $E058-$E05E, advancing the cursor.
func (c *Console) stage45CopySevenBytes() []byte {
	if len(c.stage45DataCursor) < 7 { // guarded rather than panicking -- see injectRoadFeature's own equivalent guard
		return nil
	}
	chunk := c.stage45DataCursor[:7]
	copy(c.RAM[0x058:0x05F], chunk)
	c.stage45DataCursor = c.stage45DataCursor[7:]
	return chunk
}

// stage4RoadFeatureStep is ported from $55E9-$55FD, updateRoadAndScore's
// stage 4 handler ($E043==4 -- not to be confused with the entirely
// different $5642 this port calls stage5RoadFeatureStep, since this
// project's own stage numbering follows $E043's literal value, and the
// dispatch table's entries for $E043==4 and ==5 point at $55E9 and
// $5642 respectively).
//
// Runs stage45RoadFeatureStep, then reuses the SAME 7 bytes it just
// returned (rather than re-reading them from ROM): adds $0D to each and
// writes them into $E068-$E06E in reverse order -- a mirrored, tile-ID-
// shifted second copy of the same 7-byte chunk, plausibly the other side
// of a symmetrical road feature (matching this session's railroad-
// crossing-gate guess for the broader $56xx family, though this specific
// detail isn't independently confirmed against a rendered screen).
func (c *Console) stage4RoadFeatureStep() {
	chunk := c.stage45RoadFeatureStep() // $55E9: CALL $5600
	for i, b := range chunk {           // $55EC-$55FB: rewind, then re-read+offset+$0D each, written $E06E downward
		c.RAM[0x06E-i] = b + 0x0D
	}
}

// road56A4EntryA and road56A4EntryC are 2 of the 4 entries in the ROM
// table at $56A4 (stage 5's own equivalent of $569C, read by
// stage5RoadFeatureStep): 48-byte streams at $7D94 and $7E26
// respectively, each terminated by a real $FF exactly 48 bytes in
// (confirmed by scanning the actual ROM bytes, same as every other
// stream this session extracted this way). The table's other 2 slots
// (index 0 and index 3) share the SAME address ($7D94), and index 1
// ($7DC5) is the same 96-byte stream already extracted as
// stage2And4FeatureStream -- reused directly rather than duplicated.
var road56A4EntryA = [48]byte{
	0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xC5, 0xB5, 0xC5, 0xC5, 0xC5, 0xB4, 0xC5, 0xC5, 0xC5,
	0xB3, 0xC5, 0xC5, 0xC5, 0xC7, 0xB5, 0xC5, 0xC5, 0xBD, 0xB4, 0xC5, 0xC5, 0xBB, 0xC7, 0xB5, 0xC5,
	0xC6, 0xBC, 0xB4, 0xC5, 0xC2, 0xBB, 0xB3, 0xC5, 0xC6, 0xC6, 0xBC, 0xB5, 0xC6, 0xC4, 0xBA, 0xB4,
}
var road56A4EntryC = [48]byte{
	0xC6, 0xC6, 0xB7, 0xB2, 0xC6, 0xC3, 0xBF, 0xB1, 0xC1, 0xBF, 0xC7, 0xB6, 0xC6, 0xBD, 0xB2, 0xC5,
	0xC6, 0xBF, 0xB1, 0xC5, 0xBF, 0xC7, 0xB6, 0xC5, 0xC7, 0xB2, 0xC5, 0xC5, 0xC7, 0xB1, 0xC5, 0xC5,
	0xC7, 0xB6, 0xC5, 0xC5, 0xB2, 0xC5, 0xC5, 0xC5, 0xBF, 0xC5, 0xC5, 0xC5, 0xB6, 0xC5, 0xC5, 0xC5,
}

var road56A4Entries = [4][]byte{road56A4EntryA[:], stage2And4FeatureStream[:], road56A4EntryC[:], road56A4EntryA[:]}

// stage5SelectorStream is stage 5's own per-stage selector-byte stream
// (armRoadFeatureStream arms $E090/$E091 with $7E57 for both stages 4
// and 5, but only stage 5's own handler ever reads it). Same role and
// same "unconfirmed extent" caveat as stage45SelectorStreamA/B: only a
// 32-byte prefix is extracted, since nothing in $5642's own disassembly
// checks bounds on $E090's advance either, and its own reload count
// ($E092, seeded to $3F=63 by stage45SharedInitTable) makes reaching
// this stream's own second byte take at least 63 full passes through
// whichever $56A4 entry is current -- essentially unreachable in a
// normal test, let alone real play.
var stage5SelectorStream = [32]byte{
	0x39, 0x06, 0x33, 0x04, 0x11, 0x06, 0x2B, 0x04, 0x25, 0x10, 0x31, 0x13, 0x18, 0x20, 0xAD, 0x20,
	0x65, 0x22, 0x21, 0x20, 0x59, 0x21, 0x21, 0x20, 0x81, 0x22, 0x19, 0x20, 0x41, 0x21, 0x19, 0x20,
}
var stage5SelectorStreamBase uint16 = 0x7E57

// stage5RoadFeatureStep is ported from $5642-$5699, updateRoadAndScore's
// stage 5 handler ($E043==5). Unlike every stage before it, its first
// two calls are real subroutine CALLs that return here rather than tail
// jumps: injectRoadCurve, then stage45RoadFeatureStep (the shared prefix
// stages 4 and 5 both use, see its own doc comment -- its return value
// isn't used here, unlike stage 4's).
//
// Then a second, independent entry-selector mechanism, parallel to (but
// materially different from) $5600's own: $E094 (stage5E094Cursor)
// tracks a read cursor into one of the 4 road56A4Entries; when it's
// exhausted (the real ROM peeks *(E094) for a literal $FF -- this port
// checks the Go cursor's own length instead, the same convention
// stage2/3/6's streams already use), decrements a reload count at $E092
// and, once THAT also reaches 0, advances $E090 (stage5SelectorStream)
// by one byte to compute a fresh $E092 from its top 6 bits -- then
// either way, reads the CURRENT $E090 byte's low 2 bits to select a
// fresh road56A4Entries entry.
//
// Finally, regardless of whether a reload just happened: re-reads the
// CURRENT $E090 byte (a plain peek in the original, done via a
// register-shadow `EXX` so it doesn't disturb the main `HL`/entry
// cursor -- modeled here with an ordinary local variable instead, same
// observable effect) and branches on its own low 2 bits: exactly 3
// means "skip 4 bytes with no visible effect" (advances
// stage5E094Cursor only); anything else copies those same 4 bytes,
// reversed, into $E06B-$E06E.
func (c *Console) stage5RoadFeatureStep() {
	c.injectRoadCurve()        // $5642: CALL $56F7
	c.stage45RoadFeatureStep() // $5645: CALL $5600

	if len(c.stage5E094Cursor) == 0 { // $5648-$564D: peek *(E094) for the terminator
		c.RAM[0x092]--         // $564F-$5652
		if c.RAM[0x092] == 0 { // $5653-$5656
			ptr := uint16(c.RAM[0x090]) | uint16(c.RAM[0x091])<<8
			ptr++ // $5658-$5659
			c.RAM[0x090] = byte(ptr)
			c.RAM[0x091] = byte(ptr >> 8)
			b := stage5SelectorStream[ptr-stage5SelectorStreamBase] // $565C-$565D
			c.RAM[0x092] = (b >> 2) & 0x3F                          // $565E-$5662
		}
		ptr := uint16(c.RAM[0x090]) | uint16(c.RAM[0x091])<<8
		b := stage5SelectorStream[ptr-stage5SelectorStreamBase] // $5665
		c.stage5E094Cursor = road56A4Entries[b&0x03]            // $5666-$5673
	}

	ptr := uint16(c.RAM[0x090]) | uint16(c.RAM[0x091])<<8
	peek := stage5SelectorStream[ptr-stage5SelectorStreamBase] // $5676-$567F: EXX-based peek, doesn't disturb $E094's own cursor
	if len(c.stage5E094Cursor) < 4 {                           // guarded rather than panicking -- see injectRoadFeature's own equivalent guard
		return
	}
	if peek&0x03 == 3 { // $567D/$5680: CP 03h; JR NZ -- NOT jumping (i.e. ==3) falls into the advance-only path
		c.stage5E094Cursor = c.stage5E094Cursor[4:] // $5682-$5688
		return
	}
	for i := 0; i < 4; i++ { // $568B-$5694
		c.RAM[0x06E-i] = c.stage5E094Cursor[i]
	}
	c.stage5E094Cursor = c.stage5E094Cursor[4:] // $5696
}

// This section ports $70A8, GameplayTick's own $522A callee -- see
// docs/ANALYSIS.md's dedicated "$70A8 scoping notes" for the full trace
// this is derived from. Despite an earlier "massive 8-way object
// dispatcher" characterization (before anyone had actually walked its
// call tree), it turns out to loop over exactly 2 small object slots,
// with an 8-way type dispatch that collapses to a handful of distinct
// handler bodies. Ported incrementally, type by type, in the order the
// scoping notes recommend (cheapest/most-confirmed first); each
// unported type is an honest TODO citing its real address.

// objectSlotBase returns the RAM base address of one of $70A8's two
// object slots (idx 0 or 1), 16 bytes each starting at $E0E5 --
// confirmed via $71D1-$71DA's own explicit "E0E4==0 ? $E0E5 : $E0F5"
// branch elsewhere in this same system.
func objectSlotBase(idx byte) uint16 {
	return 0xE0E5 + uint16(idx)*16
}

// setObjectType sets an object slot's own type byte (offset 1) --
// modeling the shared tail at $719C-$71A1 when reached directly (i.e.
// not through $716D's own fuller use of the same tail, which also
// touches slot offsets 8-9 first; that fuller path isn't ported yet,
// see $70D9's own TODO in runObjectSlots).
func (c *Console) setObjectType(base uint16, newType byte) {
	c.writeRAM(base+1, newType)
}

// addToSlot16 replicates $72E7-$72F5: adds a 16-bit delta (Z80
// wraparound semantics, hence uint16 rather than a signed type) to the
// existing little-endian 16-bit value stored at slot bytes
// [fieldHi-1, fieldHi] (low byte first), writing the sum back in place.
func (c *Console) addToSlot16(base uint16, fieldHi byte, delta uint16) {
	old := uint16(c.readRAM(base+uint16(fieldHi)-1)) | uint16(c.readRAM(base+uint16(fieldHi)))<<8
	sum := old + delta
	c.writeRAM(base+uint16(fieldHi), byte(sum>>8))
	c.writeRAM(base+uint16(fieldHi)-1, byte(sum))
}

// handleObjectType2 is ported from $71A2-$71C5 (object type 2 -- see
// docs/ANALYSIS.md's scoping notes for how the type-to-address mapping
// was verified via $403B's own confirmed "table + 2*type" addressing,
// correcting an earlier, wrong-by-one guess at the mapping made before
// that was checked). Runs checkOffTrackCollision against the slot's own
// position (field 6, not the player's $E04C -- see that function's own
// doc comment) rather than the player's; if unsafe, demotes the slot
// back to type 1. Otherwise counts down slot field $0F, demoting to
// type 1 once it hits zero; while still counting down, accumulates a
// $E0100/$FF00 (i.e. +1 or -1 in 8.8 fixed point, direction from slot
// field $0E) delta into the 16-bit value at slot fields 7-8, and always
// accumulates a fixed -1 into the 16-bit value at slot fields 5-6 --
// plausibly a position/velocity pair for whatever this object is, not
// independently confirmed beyond the mechanism.
func (c *Console) handleObjectType2(base uint16) {
	if _, _, unsafe := c.checkOffTrackCollision(base + 6); unsafe { // $71A2-$71AA
		c.setObjectType(base, 1) // $71C2-$71C4: LD A,1; JR $719C
		return
	}
	remaining := c.readRAM(base+0x0F) - 1 // $71AC-$71B1
	c.writeRAM(base+0x0F, remaining)
	if remaining == 0 { // $71B2: JR Z,$71C2
		c.setObjectType(base, 1)
		return
	}
	deltaY := uint16(0x0100) // $71B4-$71B9: CALL $7145
	if c.readRAM(base+0x0E) == 1 {
		deltaY = 0xFF00
	}
	c.addToSlot16(base, 8, deltaY)
	c.addToSlot16(base, 6, 0xFF00) // $71BC-$71BF: LD DE,$FF00; JP $7168
}

// handleObjectType3 is ported from $71C6-$7208 (object type 3). Only
// runs once the object's own field 9 reaches $D7 or more (a countdown
// or distance gate, not confirmed beyond the comparison itself); then
// compares its own position (fields 6/8, the same row/column pair
// checkOffTrackCollision reads) against the *other* slot's -- `other`
// is genuinely an inverted lookup (idx 0 selects the other slot's own
// base, $E0F5; idx 1 selects $E0E5), confirmed directly from $71D1-
// $71DA's own idx==0 branch, not the same arithmetic objectSlotBase
// itself uses -- and, if both a column-proximity and a row-offset test
// pass (both are chained-subtraction-then-add checks, ported as exact
// byte arithmetic rather than reasoned about algebraically, the same
// standard this project applies to every Z80 SUB/ADD/RET-NC chain),
// sets a direction flag and a 24-frame countdown (fields $0E/$0F -- the
// same two fields handleObjectType2 also uses) and transitions the
// object to type 7. One honest surprise from testing this rather than
// reasoning about it: two objects at the exact same row report "safe"
// on the row check, not "unsafe" -- this looks backwards for a naive
// proximity read, but the exact chained constants ($28, $08, $10) are
// ported faithfully regardless; this is very plausibly detecting a
// specific relative-offset window (e.g. "far enough ahead to trigger a
// pass") rather than raw closeness, not a translation error.
func (c *Console) handleObjectType3(idx byte, base uint16) {
	if c.readRAM(base+9) < 0xD7 { // $71C6-$71CF: CP $D7; RET C
		return
	}
	other := objectSlotBase(1 - idx) // $71D1-$71DF
	curCol, otherCol := c.readRAM(base+8), c.readRAM(other+8)
	preCol := curCol - otherCol - 0x10 // $71E0-$71E3
	if preCol < 0xE0 {                 // $71E5-$71E7: ADD A,$20; RET NC
		return
	}
	curRow, otherRow := c.readRAM(base+6), c.readRAM(other+6)
	preRow := curRow - 0x28 - otherRow - 0x08 // $71EC-$71F0
	if preRow < 0xF0 {                        // $71F2-$71F4: ADD A,$10; RET NC
		return
	}
	direction := byte(0)
	if otherCol < curCol { // $71F5-$71FB
		direction = 1
	}
	c.writeRAM(base+0x0E, direction) // $7201
	c.writeRAM(base+0x0F, 0x18)      // $7203
	c.setObjectType(base, 7)         // $7205-$7207: LD A,7; JR $719C
}

// writeApproachingObjectRecord is ported from $53DD-$5433: called from
// $70A8's own type 1/4 handler (not yet ported), unconditionally, as
// part of that handler's own body -- so the gates here are this
// function's own, not a caller-side check. Gated on the overall
// distance counter ($E078) being under $0F00, on slot field *0* (not
// the type field at offset 1 -- a separate, unconfirmed sub-state this
// object's type 1/4 handler apparently tracks) equaling 7, and on the
// player not being in off-track recovery ($E049==1). If the object's
// own row falls within a specific offset window relative to the
// player's own row ($E04C) -- ported as exact chained-subtraction byte
// arithmetic rather than reasoned about algebraically; confirmed by
// sweeping it that, like handleObjectType3's own row check, the *same*
// row does not trigger this, only a specific band above it -- and a
// separate "claimed" record at $E105 isn't already active, writes an
// 8-byte-ish record there derived from the object's
// row/column and the player's own column. $5434 is this record's
// consumer -- traced far enough to see it touches $E106/$E108/$E10A/
// $E10C and more beyond that, clearly larger than this write side, and
// left as its own TODO rather than guessed at. Field semantics beyond
// the mechanism (e.g. what $0x90/$0x03 mean) aren't confirmed.
func (c *Console) writeApproachingObjectRecord(base uint16) {
	distance := uint16(c.RAM[0x078]) | uint16(c.RAM[0x079])<<8
	if distance >= 0x0F00 { // $53DD-$53E6: SBC HL,$0F00; RET NC
		return
	}
	if c.readRAM(base) != 7 { // $53E7-$53ED: slot field 0, not the type field
		return
	}
	if c.RAM[0x049] == 1 { // $53EE-$53F2: DEC A; RET Z
		return
	}
	row := c.readRAM(base + 6)
	pre := c.RAM[0x04C] - 0x10 - row - 0xB8 // $53F8-$5400
	if pre < 0xE0 {                         // $5400-$5402: ADD A,$20; RET NC
		return
	}
	rowPlus30 := row + 0x30                                  // $5403-$5406
	colOffset := c.readRAM(base+8) + 4                       // $5407-$540C
	delta := (uint16(c.RAM[0x04E]) - uint16(colOffset)) << 2 // $540D-$541A

	if c.RAM[0x105] == 1 { // $541B-$5420: DEC A; RET Z
		return
	}
	c.RAM[0x105] = 1                // $5421
	c.RAM[0x107] = rowPlus30        // $5423-$5425
	c.RAM[0x109] = colOffset        // $5426-$5428
	c.RAM[0x10A] = 0x90             // $5429-$542A
	c.RAM[0x10B] = 0x03             // $542C-$542D
	c.RAM[0x10C] = byte(delta)      // $542F-$5430
	c.RAM[0x10D] = byte(delta >> 8) // $5431-$5432
}

// advanceApproachingObjectMarker is ported from $5434-$54AE (GameplayTick's
// own $522D callee, previously untraced -- and previously guessed to be
// something like "reads $E105 (track/scenery pointer?)"): the per-frame
// update for the single "approaching object" record writeApproachingObjectRecord
// arms above. That record turns out to be a scrolling scenery/hazard
// marker, not passive decoration: $E107/$E109 are the *integer* row/column
// of two independent 16-bit fixed-point positions (low bytes $E106/$E108),
// advanced each frame by the fixed per-frame deltas $E10A-$E10D
// writeApproachingObjectRecord sets once, and drawn as sprite-attribute-
// table entry 12 (staging address $E13E, the last of the 13 entries
// blitSpriteAttributeTable's own count=$34 call copies to VRAM every
// frame) -- but getting too close to it in *both* row and column forces
// a genuine off-track crash.
//
// Does nothing while the record is inactive ($E105==0). Once the row has
// scrolled to $C0 or beyond (off the bottom of the play area), retires
// the marker via retireApproachingObjectMarker and returns.
//
// Otherwise, every 8th frame while the game is active ($E002 bit 6 set),
// triggers a proximity-warning sound (id 4). Then checks the player's
// own position ($E04C/$E04E) against the marker's current row/column:
// confirmed via direct-call ROM tracing (not reasoned from the raw
// chained-subtraction bytes alone) to be two independent narrow-band
// checks -- row within 24 of the marker, then (only if the row check
// also matched) column within 16 -- not a combined distance. If either
// check misses, the marker just advances normally: its position fields
// accumulate their own per-frame deltas (via addToSlot16, the same
// 16-bit low/high accumulator helper the $70A8 object system uses), and
// its sprite entry is redrawn from the updated integer row/column, with
// a pattern that cycles with the frame counter ($E003) and a fixed
// color index. If BOTH checks hit -- the player has collided with the
// marker -- it's retired and triggerOffTrackRecovery fires (the same
// off-track/crash tail case 7 of updateSteeringAndCarState and
// checkPlayerObjectProximity already share) -- a plain call-then-return
// in the original ($54A9: CALL $549E; $54AC: JP $6F18, a tail-jump that
// still returns normally to $5434's own caller once triggerOffTrackRecovery
// itself RETs, since neither instruction is a CALL that leaves an extra
// frame behind), not a stack trick needing a bool return here.
func (c *Console) advanceApproachingObjectMarker() {
	if c.RAM[0x105] == 0 { // $5434-$5439
		return
	}
	if c.RAM[0x107] >= 0xC0 { // $543A-$543F
		c.retireApproachingObjectMarker()
		return
	}
	if c.RAM[0x002]&0x40 != 0 && c.RAM[0x003]&0x07 == 0 { // $5441-$544E
		c.TriggerSound(4) // $5450-$5452
	}

	deltaRow := c.RAM[0x04C] - c.RAM[0x107] - 8 // $5456-$545C
	if deltaRow >= 0xE8 {                       // $545E: ADD A,$18; JR NC
		deltaCol := c.RAM[0x04E] - c.RAM[0x109] - 8 // $5462-$5468
		if deltaCol >= 0xF0 {                       // $546A: JR C -- both bands hit: collision
			c.retireApproachingObjectMarker()
			c.triggerOffTrackRecovery() // $54AC: JP $6F18
			return
		}
	}

	rowDelta := uint16(c.RAM[0x10A]) | uint16(c.RAM[0x10B])<<8
	colDelta := uint16(c.RAM[0x10C]) | uint16(c.RAM[0x10D])<<8
	c.addToSlot16(0xE105, 2, rowDelta)          // $546C-$5476: row (E106/E107) += rowDelta
	c.addToSlot16(0xE105, 4, colDelta)          // $5479-$5483: col (E108/E109) += colDelta
	c.RAM[0x13E] = c.RAM[0x107]                 // $5486-$548D: sprite Y = updated row
	c.RAM[0x13F] = c.RAM[0x109]                 // sprite X = updated col
	c.RAM[0x140] = (c.RAM[0x003] & 0x0C) + 0x6C // $5491-$5498: pattern, cycling with the frame counter
	c.RAM[0x141] = 8                            // $549A-$549C: color
}

// retireApproachingObjectMarker is ported from $549E-$54A8: deactivates
// the $E105 "approaching object" record and hides its sprite entry at
// the $E0 off-screen sentinel (the same one this project's other
// retirement paths use, e.g. finishObjectSlot -- not the $D0
// list-terminator value).
func (c *Console) retireApproachingObjectMarker() {
	c.RAM[0x105] = 0 // $549E-$54A1
	c.RAM[0x13E] = 0xE0
}

// checkApproachingObjectSound is ported from $72B8-$72E6, one of
// activateStagedObjectType's own callees. Gated on the overall game
// being "active" ($E002 bit 6 set) and $E049 being neither 1
// (off-track recovery) nor 3 (the "arriving at finish" state, see
// checkDistanceMilestone's own doc comment) -- both DEC-A-based
// cascading checks against the SAME loaded value, ported as two direct
// comparisons rather than replicating the register-level cascade. Only
// proceeds if the slot's own field 0 (not field 1, the type -- a
// separate byte this whole $70D9 family reads) equals 1. If the object
// (field 6, its own row) is within a fixed band of the player's row
// ($E04C) and the player's own speed ($E04F) is at least $90, triggers
// sound id 9 -- a proximity-warning honk for an approaching object.
func (c *Console) checkApproachingObjectSound(base uint16) {
	if c.RAM[0x002]&0x40 == 0 { // $72B8-$72BD
		return
	}
	if c.RAM[0x049] == 1 || c.RAM[0x049] == 3 { // $72BE-$72C5
		return
	}
	if c.readRAM(base) != 1 { // $72C6-$72CB
		return
	}
	delta := c.RAM[0x04C] - 0x34 - c.readRAM(base+6) - 8 // $72D1-$72D7
	if delta < 0xF8 {                                    // $72D9-$72DB: ADD A,08h; RET NC
		return
	}
	if c.RAM[0x04F] < 0x90 { // $72DC-$72E1
		return
	}
	c.TriggerSound(9) // $72E2-$72E4
}

// resolveApproachingObjectDirection is ported from $716D-$71A1, one of
// activateStagedObjectType's own callees. Only proceeds when the slot's
// own field 0 equals 3. Checks the player's own row ($E04C) against the
// object's own row (field 6) within a fixed band ($40 below, $20
// above); if not close enough, returns without transitioning. If close:
// derives a direction flag by comparing the player's own column
// ($E04E) against field 8's current value (not independently confirmed
// what sets that value before this read -- plausibly a target column
// from the object's own staged spawn data), writes the direction to
// field $0E and $20 to field $0F (the same two fields handleObjectType2/3
// use for direction/hold-countdown), and transitions the object to type
// 5 via the same $719C-$71A1 shared tail setObjectType already models
// a simpler version of.
func (c *Console) resolveApproachingObjectDirection(base uint16) {
	if c.readRAM(base) != 3 { // $7170-$7173
		return
	}
	objRow := c.readRAM(base + 6)
	lowerBound := c.RAM[0x04C] - 0x40 // $7178-$717B
	if lowerBound < objRow {          // $717D-$717E: CP (HL); RET C
		return
	}
	upperBound := objRow + 0x20  // $7180-$7181
	if upperBound < lowerBound { // $7183-$7184: CP B; RET C
		return
	}
	direction := byte(1)
	if c.RAM[0x04E] < c.readRAM(base+8) { // $7187-$7193: CP (HL); JR NC
		direction = 0
	}
	c.writeRAM(base+0x0E, direction) // $7196
	c.writeRAM(base+0x0F, 0x20)      // $7198
	c.setObjectType(base, 5)         // $719A-$71A1
}

// handleObjectType57 is ported from $7209-$7226 (object types 5 and 7,
// dispatched from the same table entry -- see resolveApproachingObjectDirection
// above and handleObjectType3 below for where objects transition into
// these two types). Counts down slot field $0F; once decrementing it
// goes negative (byte result >= $80, matching the original's own "DEC
// (HL); JP M"), demotes the object back to type 1 (see setObjectType).
// Otherwise pushes the slot's own 16-bit position (fields 7-8, via
// addToSlot16) by a fixed magnitude -- $00B8 for type 5, $00A0 for type
// 7 -- negated when the slot's own direction flag (field $0E) is zero
// (matching $7146's shared "JR NZ" branch, the same tail
// pushAwayFromTrackBoundary reuses for a different sign source).
// Finally falls through into checkSiblingProximityAndResolve, exactly
// like activateStagedObjectType's own tail does.
func (c *Console) handleObjectType57(idx byte, base uint16, objType byte) {
	countdown := c.readRAM(base+0x0F) - 1 // $7212: DEC (HL)
	c.writeRAM(base+0x0F, countdown)
	if countdown >= 0x80 { // $7213: JP M,L71C2
		c.setObjectType(base, 1)
		return
	}
	delta := uint16(0x00B8) // $7218-$721D
	if objType == 7 {
		delta = 0x00A0 // $721F
	}
	if c.readRAM(base+0x0E) == 0 { // $7221-$7222: OR A
		delta = -delta // $7146: JR NZ skipped -- negate path
	}
	c.addToSlot16(base, 8, delta) // $7223: CALL L7146 (tail of pushAwayFromTrackBoundary)
	c.resolveAndPushObject(idx, base)
}

// resolveAndPushObject is the shared $70E5-$7122 tail: sibling
// resolution followed by the three positional pushes, ending in the
// curve-boundary tail's own JP into $7145. Both of its entries are real:
// activateStagedObjectType falls into it, and handleObjectType57 reaches
// it via $7226's `JP L70E5`.
//
// CORRECTED: that `JP L70E5` was modelled as a call to
// checkSiblingProximityAndResolve alone, on the reading that the jump
// targets that one routine. It doesn't -- $70E5 is a `CALL L72FE`
// *inside* activateStagedObjectType's own body, so control continues
// through $70E8, $70EB and the $70EE tail before returning. Types 5 and
// 7 were therefore running sibling resolution and nothing else.
//
// That is exactly the state a maneuvering car is in: kind 3 (the magenta
// car) enters type 5 via resolveApproachingObjectDirection, and type 5
// moves it sideways $B8 per frame for up to $20 frames. With the three
// pushes skipped for the whole of that window, nothing pushed it back
// on-road (pushAwayFromTrackBoundary), nothing corrected its position
// against the player's speed (pushByRelativeSpeed), and nothing applied
// the curve (pushAwayFromCurveBoundary) -- so it could swerve straight
// off the track and stay there, hang alongside the player, and ignore
// the road's curvature. Reported from real gameplay, most visibly on
// stage 5 where two kind-3 cars maneuver at once and both take their
// direction from the player's own column, so they move together and the
// outer one leaves the road.
func (c *Console) resolveAndPushObject(idx byte, base uint16) {
	c.checkSiblingProximityAndResolve(idx, base) // $70E5: CALL $72FE
	c.pushAwayFromTrackBoundary(base)            // $70E8: CALL $7123
	c.pushByRelativeSpeed(base)                  // $70EB: CALL $7154
	c.pushAwayFromCurveBoundary(base)            // $70EE-$7122: inline tail, ends in JP $7145
}

// checkSiblingProximityAndResolve is ported from $72FE-$7351, one of
// activateStagedObjectType's own callees. Only proceeds when the slot's
// own field 1 (the type) is nonzero and field 9 is at least $D7 --
// otherwise, plausibly, not yet "settled" enough for this to matter.
// Checks the row and column distance between this object and the
// *other* $70A8 slot (idx 1-idx, matching handleObjectType3's own
// inverted-lookup convention); if both are within their own fixed
// bands, derives a direction flag from which one leads in column, and
// calls resolveObjectProximity on the OTHER slot with that direction --
// transitioning the sibling into evasive type-2 behavior.
//
// $733D-$7351's own tail (previously an honest gap, since it depends on
// resolveObjectProximity's own real Z80 return value/flags in a way this
// port's earlier void-returning translation of that function couldn't
// expose) additionally transitions *this* object to type 2 as well --
// confirmed via direct-call ROM tracing across all 3 of
// resolveObjectProximity's own outcomes (sibling inactive: skip
// entirely; sibling field 0 is 6 or 7: still transitions, with a
// distinctly different direction value; normal success: both objects
// transition, with complementary directions) that `apply` is exactly
// the right gate and `outcome` feeds this object's own direction as
// `(outcome-1)^1`. This object's own countdown ($0F) is 16 frames, a
// frame longer than the sibling's own 12.
func (c *Console) checkSiblingProximityAndResolve(idx byte, base uint16) {
	if c.readRAM(base+1) == 0 { // $7301-$7304
		return
	}
	if c.readRAM(base+9) < 0xD7 { // $7305-$730C
		return
	}
	otherBase := objectSlotBase(1 - idx)
	rowDelta := c.readRAM(otherBase+6) - c.readRAM(base+6) - 0x10 // $731B-$731F
	if rowDelta < 0xE0 {                                          // $7321: ADD A,20h; RET NC
		return
	}
	curCol, otherCol := c.readRAM(base+8), c.readRAM(otherBase+8)
	colDelta := otherCol - curCol - 0x0A // $7326-$732A
	if colDelta < 0xEC {                 // matches ADD A,14h; RET NC
		return
	}
	direction := byte(0)
	if otherCol < curCol { // $732D-$7333: SUB(HL) borrows when otherCol<curCol
		direction = 1
	}
	outcome, apply := c.resolveObjectProximity(otherBase, direction) // $7334-$733C
	if !apply {                                                      // $733C: RET Z
		return
	}
	c.setObjectType(base, 2)             // $733E-$7342
	c.writeRAM(base+0x0E, (outcome-1)^1) // $7344-$734D
	c.writeRAM(base+0x0F, 0x10)          // $734E-$734F
}

// pushAwayFromTrackBoundary is ported from $7123-$7151's own leading
// gate, one of activateStagedObjectType's own callees. Only proceeds
// while the overall distance counter ($E078) is before the checkpoint
// threshold ($0F09, the same one injectCheckpointText uses) and
// checkOffTrackCollision (against the object's own row, field 6)
// reports unsafe. Clears field $0F, then pushes the object's own
// 16-bit position (fields 7-8, via addToSlot16) sideways by a fixed
// amount -- negated when checkOffTrackCollision's own "which side"
// result (resultB) is 1 -- steering the object back toward the safe
// side of whichever boundary it crossed.
func (c *Console) pushAwayFromTrackBoundary(base uint16) {
	distance := uint16(c.RAM[0x078]) | uint16(c.RAM[0x079])<<8
	if distance >= 0x0F09 { // $7123-$712D
		return
	}
	_, resultB, unsafe := c.checkOffTrackCollision(base + 6) // $712E-$7136
	if !unsafe {
		return
	}
	c.writeRAM(base+0x0F, 0) // $7137-$713E
	delta := uint16(0x02F0)  // $7140-$7142
	if resultB == 1 {        // $7144-$714E: negate when resultB==1
		delta = -delta
	}
	c.addToSlot16(base, 8, delta) // $714F-$7151
}

// pushByRelativeSpeed is ported from $7154-$716A, one of
// activateStagedObjectType's own callees. Computes the player's own
// speed ($E04F) minus a 16-bit target value at the object's own fields
// 9-10, scales it by 8, and pushes the object's own position (fields
// 5-6, via addToSlot16) by that amount -- a relative-speed-driven
// approach/recede effect.
func (c *Console) pushByRelativeSpeed(base uint16) {
	target := uint16(c.readRAM(base+9)) | uint16(c.readRAM(base+10))<<8
	delta := (uint16(c.RAM[0x04F]) - target) << 3
	c.addToSlot16(base, 6, delta) // $7168-$716A
}

// pushAwayFromCurveBoundary is ported from $70EE-$7151 (the inline tail
// activateStagedObjectType's own body falls into after its last
// CALLed callee), sharing the same $7144-714F negate-and-push tail
// pushAwayFromTrackBoundary uses, fed by a different, boundary-table-
// derived flag instead of checkOffTrackCollision's own resultB.
// Computes a "band" from the object's own row (clamped near the bottom
// of the screen, the same clampRowBand shape checkOffTrackCollision
// uses) and looks up the per-row boundary table ($E098, see
// advanceBoundaryHistoryTable) at that band; if the table entry's own
// low 2 bits are 0, does nothing further. Otherwise computes a
// speed-relative delta (player speed minus field 9, doubled) and pushes
// the object's own position (fields 7-8) by it, negated when the
// boundary entry's low bits equal 1.
func (c *Console) pushAwayFromCurveBoundary(base uint16) {
	objRow := c.readRAM(base + 6)
	band := byte(0)
	if objRow-0xC0 >= 0x40 { // $70F3-$70F9
		band = (objRow >> 3) & 0x1F // $70FB-$70FF
	}
	masked := c.RAM[0x098+int(band)] & 0x03 // $7102-$710F
	if masked == 0 {                        // $7110: JP Z,L7289
		return
	}
	speed, field9 := c.RAM[0x04F], c.readRAM(base+9)
	diff := speed - field9 // $7113-$7117
	if speed < field9 {    // $7117-$7119: negate on borrow
		diff = -diff
	}
	// $711B-$711D: ADD A,A; LD E,A; LD D,00h -- the double happens in A,
	// an 8-bit register, so it wraps at $FF *before* being zero-extended
	// into DE. Widening first (uint16(diff)*2, what this line used to do)
	// lets delta reach $1FE, i.e. nearly two whole column units in one
	// frame, where the original can never exceed $FF -- less than one.
	// Only diverges once |speed - field9| >= $80, which is common early
	// in a race, and the visible result was a traffic car taking a sudden
	// sideways jump across the road and back. Found from a user report
	// about the magenta car (kind 3, the only kind that actively
	// maneuvers relative to the player -- see
	// resolveApproachingObjectDirection, which gates on field 0 == 3),
	// though the bug is not kind-specific: that car is just the one whose
	// own lateral movement makes an extra shove obvious.
	delta := uint16(diff * 2)
	if masked == 1 { // $711F-$7145: A=masked; DEC A; JR NZ -- negate when masked==1
		delta = -delta
	}
	c.addToSlot16(base, 8, delta) // $714F-$7151
}

// activateStagedObjectType is ported from $70D9-$7151: runObjectSlots'
// own type 1/4 handler, the confirmed root cause of a user report that
// road objects (traffic cars, oil, gas spill) never appear -- every
// object this port's own spawn pipeline creates starts in exactly this
// dispatch case (type 1, hardcoded by stageObjectRecord), and until
// this was ported, nothing here ever transitioned it onward to one of
// the drawable types (2, 3, 5, 6, 7).
//
// Reuses three already-ported functions as plain subroutines, exactly
// as the original's own CALL chain does -- handleObjectType3 and
// writeApproachingObjectRecord don't gate on field 1 (the type) at all,
// only on their own field 0/field 9 conditions, so calling them here
// (type 1/4) is faithful, not a guess: the original genuinely reuses
// the same routine from two different dispatch cases.
//
// NOT PORTED: checkSiblingProximityAndResolve's own $733D-$7351 tail
// (see its own doc comment) -- an honest, narrowly-scoped gap, not
// guessed at.
func (c *Console) activateStagedObjectType(idx byte, base uint16) {
	c.checkApproachingObjectSound(base)       // $70D9: CALL $72B8
	c.handleObjectType3(idx, base)            // $70DC: CALL $71C6
	c.writeApproachingObjectRecord(base)      // $70DF: CALL $53DD
	c.resolveApproachingObjectDirection(base) // $70E2: CALL $716D
	c.resolveAndPushObject(idx, base)         // $70E5-$7122, shared with handleObjectType57's own JP into it
}

// runObjectSlots is ported from $70A8-$72AD: loops over exactly 2
// object slots (see objectSlotBase), dispatching each on its own type
// byte (slot offset 1, values 0-7) via the $4045-trampolined 8-entry
// jump table at $70C9-$70D8. $E0BF (the original's own "current slot
// base" cache) and $E0E4 (the loop index) aren't modeled as separate
// RAM reads here -- Go passes idx/base directly to each handler
// instead -- but $E0E4 is still written to RAM for fidelity, since at
// least one sibling routine ($6E4A, not yet ported) reads it directly.
//
// The 8 table entries collapse to fewer distinct handler bodies: type 0
// does nothing (fast-pathed before ever reaching the table, matching
// the original's own "AND A; JP Z" skip, and -- unlike every other
// type -- never reaching the shared draw/retire tail below either);
// types 1 and 4 share $70D9; type 2 is $71A2 (handleObjectType2,
// ported); type 3 is $71C6 (handleObjectType3, ported); types 5 and 7
// share $7209; type 6 is $728A.
//
// Originally positionTrafficSprite (the $728A tail) was only called for
// type 6, on the assumption it was that type's own dedicated handler.
// Direct-call ROM tracing (a user report that road objects never
// visually appeared led to re-deriving this from scratch) instead
// showed it's the shared continuation *every* dispatch case's own RET
// falls into: $70C2-$70C6 pushes return address $728A onto the stack
// *before* running the $4045 trampoline, so whichever handler eventually
// executes a plain RET (types 1-5,7, all of whose call chains bottom out
// in one) lands there too, and type 6's table entry simply points at
// $728A directly instead of at a RET-terminated handler. Confirmed
// empirically: a type-1 object (the type every real spawn starts as)
// already produces real sprite-attribute writes the moment this tail
// runs after its dispatch, with no type-6 transition involved at all.
// See docs/ANALYSIS.md's "$70A8 scoping notes" for the full derivation
// and the direct-call trace that established it.
func (c *Console) runObjectSlots() {
	for idx := byte(0); idx < 2; idx++ {
		c.RAM[0x0E4] = idx
		base := objectSlotBase(idx)
		objType := c.readRAM(base + 1)
		switch objType {
		case 0:
			continue // no-op, matches the original's own fast-path skip -- bypasses the shared tail below entirely, unlike every other case
		case 1, 4:
			c.activateStagedObjectType(idx, base) // $70D9
		case 2:
			c.handleObjectType2(base)
		case 3:
			c.handleObjectType3(idx, base)
		case 5, 7:
			c.handleObjectType57(idx, base, objType) // $7209
		case 6:
			// no separate body: type 6's own dispatch-table entry points
			// straight at the shared tail below (see this function's own
			// doc comment), so there's nothing case-specific to do here.
		}
		c.positionTrafficSprite(idx, base) // $728A-$7289: CALL $7229's own leading "always draw" half
		c.finishObjectSlot(idx, base)      // $728D-$72A3: retire near the bottom of the screen
	}
}

// trafficSpriteEntryAddr returns the RAM staging-buffer address of the
// first sprite of a 2-sprite pair for object index idx: $E11A + idx*8
// -- confirmed via $736B/$403B's own combined addressing (idx tripled
// via 3 ADD A,A, i.e. idx*8, added to a base of $E11A), used by both
// halves of checkPlayerObjectProximity below. $E11A is the same
// sprite-attribute-table staging area initTrafficSpriteSlots ($5197)
// populates (entries 3 onward), and idx here lines up with that
// function's own entry numbering shifted by 3 and halved (idx 0 -> the
// $E11A/$E11E pair, i.e. entries 3-4).
func trafficSpriteEntryAddr(idx byte) uint16 {
	return 0xE11A + uint16(idx)*8
}

// resolveObjectProximity is ported from $7352-$736A: a shared helper for
// checkPlayerObjectProximity (called with A already holding a direction
// flag) and checkSiblingProximityAndResolve's own $7339 call, which
// additionally uses the real Z80 return value (A, and whether the
// callee's own path even reaches a meaningful one) that
// checkPlayerObjectProximity's own void call site doesn't need -- see
// checkSiblingProximityAndResolve's own doc comment for how that value
// feeds its own further tail. If the object's own field 0 is 6 or 7,
// does nothing to it (aborts before the type-2 transition) but still
// reports the "would-have-applied" outcome via `apply=true`; if its type
// is already 0, `apply` is false (a real difference confirmed by direct-
// call ROM tracing -- these two abort paths carry different Z80 flag
// states out through the real RET, which is exactly why
// checkSiblingProximityAndResolve's own tail was previously left
// unported rather than guessed at). Otherwise transitions it to type 2
// (see handleObjectType2) with the given direction and a 12-frame
// countdown -- shorter than handleObjectType3's own 24-frame source for
// the same type.
func (c *Console) resolveObjectProximity(base uint16, direction byte) (outcome byte, apply bool) {
	if v := c.readRAM(base) - 6; v < 2 { // $7352-$735A: RET C
		return v, true
	}
	if c.readRAM(base+1) == 0 { // $735B-$735D: RET Z
		return 0, false
	}
	c.setObjectType(base, 2)         // $735E-$735F
	c.writeRAM(base+0x0E, direction) // $7361-$7365
	c.writeRAM(base+0x0F, 0x0C)      // $7366-$7367
	return direction + 1, true       // $7369: INC A
}

// checkPlayerObjectProximity is ported from $6E4A-$6F17 (GameplayTick's
// own $523F callee, previously a TODO). Only runs when the player isn't
// already in off-track recovery ($E049==1). Loops over 3 iterations
// using the identical $E0E5+idx*16 slot-base arithmetic $70A8 itself
// uses -- idx 0 and 1 land on the two real object slots, and idx 2
// lands on $E105 exactly ($E0E5+2*16), the same "approaching object"
// record writeApproachingObjectRecord/$5434 (see advanceApproachingObjectMarker)
// manage, processed here via the same generic logic as a real slot. For
// each iteration, if the corresponding slot/record's own field 1 (for
// $E105, this is $E106) is nonzero, checks the player's own position
// against a 2-sprite pair in the staging buffer at trafficSpriteEntryAddr(idx)
// -- the same traffic sprites initTrafficSpriteSlots sets up -- and if
// close enough in both row and column (both chained-subtraction checks,
// ported as exact byte arithmetic per this project's own convention),
// stashes a direction flag ($E054).
//
// $6EA7-$6EBA (previously missed entirely -- the earlier porting pass's
// own doc comment jumped straight from $6EA4 to $6EBB without
// acknowledging the 20 bytes between them, not a deliberately-flagged
// gap): once the off-track check above has passed, unconditionally
// reverts the object's own type to 1 (field 1) and sets field 2 to 5,
// then sets field 12 (offset $0C) to 3 or $FD depending on the same
// direction flag computed above (matching $6EB3's own "LD A,C" -- C
// already holds that value from the $6E84 "LD C,$00"/"INC C" pair, not
// a separate register). The type=1 write is then legitimately
// overwritten again moments later by resolveObjectProximity's own
// type=2 transition below (confirmed via direct-call ROM tracing that
// this is the real, intended final sequence, not a bug in either
// write). Field 12's role isn't independently confirmed -- for idx 2 it
// aliases into the sprite-attribute staging area ($E105+$0C=$E111, one
// of the entries blitSpriteAttributeTable copies to VRAM), an overlap
// this project's docs already note as a known quirk of that pseudo-slot,
// but for idx 0/1 it's a plain, non-aliased field within the object's
// own 16-byte slot.
//
// If the slot/record's own field 0 is 6 or 7, jumps into the same
// $6F18 tail already reachable from case 7 (see
// checkOffTrackCollisionAndCrash's own doc comment) -- i.e. this object
// crossing into that sub-state is treated exactly like the player going
// off-track. Traced precisely how that tail's own final RET behaves for
// *this* call site specifically (no CALL frames are left unconsumed
// between $6E4A's own entry and this jump, unlike case 7's own path,
// which leaves its own "CALL $6DC1" frame on the stack at the
// equivalent point) and it resolves to a perfectly normal, single-level
// return here -- so this is modeled as an ordinary write-then-return,
// not a bool "abort the caller" signal the way case 7's own path is.
// This traced difference incidentally raised a real question about
// whether case 7's own "return true" is actually correct (working
// through its exact stack depth suggests its own path might also
// resolve to a normal return rather than a double-discard) -- flagged
// as an open question rather than changed here, since that's
// pre-existing, tested code and this observation deserves its own
// focused re-verification, not a change made in passing.
//
// Otherwise: if the player's own car-behavior state is 7, clears it to
// 0 (a plain, normal return, see clearCarBehaviorState); else
// transitions the slot via resolveObjectProximity (with the direction
// flag flipped) and unconditionally forces a real crash ($E049=4,
// stateCrashSequence) and re-arms the player's own sprites. If the
// slot/record's own field 0 is exactly 8, additionally decrements
// $E0E3 (role not confirmed), clears the object entirely (type=0),
// hides its 2-sprite pair (Y=$E0, off-screen -- the real TMS9918A
// off-screen sentinel this project's sprite code already uses
// elsewhere, not the $D0 list-terminator value), and arms the
// fuel-pickup dispatcher ($E0B8=1).
//
// One check ($6E94-$6E99: `LD A,(E0E4h); CP 3; JP Z,$6F18`) tests the
// loop index against 3 mid-body, but the loop itself only ever runs the
// body with index 0-2 (the exit test, which compares *after*
// incrementing, is what reaches 3) -- ported literally since it's an
// exact, unambiguous translation, but it appears structurally
// unreachable from this function's own traced entry point; not
// confirmed to matter in practice.
func (c *Console) checkPlayerObjectProximity() {
	if c.RAM[0x049] == 1 { // $6E4A-$6E4E
		return
	}
	for idx := byte(0); idx < 3; idx++ {
		c.RAM[0x0E4] = idx
		base := 0xE0E5 + uint16(idx)*16 // $6E4F-$6E62: also objectSlotBase(idx) for idx<2; idx=2 lands on $E105
		if c.readRAM(base+1) == 0 {     // $6E6A-$6E6D
			continue
		}
		spriteAddr := trafficSpriteEntryAddr(idx) // $6E70-$6E71: CALL $736B
		spriteY := c.readRAM(spriteAddr)
		preRow := c.RAM[0x04C] - spriteY - 0x10 // $6E74-$6E7A
		if preRow < 0xE0 {                      // ADD A,$20; JP NC,$6F0A
			continue
		}
		spriteX := c.readRAM(spriteAddr + 1)
		direction := byte(0)
		if c.RAM[0x04E] < spriteX { // $6E80-$6E88
			direction = 1
		}
		preCol := c.RAM[0x04E] - spriteX - 0x0A // $6E89-$6E8B
		if preCol < 0xEA {                      // ADD A,$16; JP NC,$6F0A
			continue
		}
		c.RAM[0x054] = direction // $6E90-$6E91

		if idx == 3 { // $6E94-$6E99 -- structurally unreachable, see doc comment
			c.triggerOffTrackRecovery()
			return
		}
		if v := c.readRAM(base) - 6; v < 2 { // $6E9C-$6EA4
			c.triggerOffTrackRecovery()
			return
		}

		c.writeRAM(base+1, 1) // $6EA7-$6EA8: revert the object's own type to 1
		c.writeRAM(base+2, 5) // $6EAA-$6EAB
		if direction != 0 {   // $6EB3-$6EB7: LD A,C; AND A; JR NZ -- C already holds this same direction (see the $6E84 "LD C,00h"/"INC C" pair above)
			c.writeRAM(base+0x0C, 3) // $6EB5
		} else {
			c.writeRAM(base+0x0C, 0xFD) // $6EB9
		}

		if c.RAM[0x049] == 7 { // $6EBB-$6EC1
			c.RAM[0x049] = 0
			return
		}
		c.resolveObjectProximity(base, direction^1) // $6EC3-$6ECC: XOR 1
		c.RAM[0x0B1] = 0x20                         // $6ECF-$6ED1
		c.RAM[0x049] = 4                            // $6ED4-$6ED6
		c.armPlayerCarSprites()                     // $6ED9

		if c.readRAM(base) != 8 { // $6EDC-$6EE2
			return
		}
		c.RAM[0x0E3]--                 // $6EE3-$6EE6
		c.clearCarBehaviorState()      // $6EE7: CALL $6BDB
		c.setObjectType(base, 0)       // $6EEA-$6EEE
		c.writeRAM(spriteAddr, 0xE0)   // $6EF0-$6EFC
		c.writeRAM(spriteAddr+4, 0xE0) // $6EFE-$6F02
		c.RAM[0x0B8] = 1               // $6F04-$6F07
		return
	}
}

// triggerOffTrackRecovery is the shared tail at $6F18-$6F22 (also
// reached from case 7, see checkOffTrackCollisionAndCrash's own doc
// comment): starts the off-track-recovery countdown and redraws the
// HUD, the same way a genuine off-track detection does.
func (c *Console) triggerOffTrackRecovery() {
	c.RAM[0x049] = 1 // $6F18-$6F1B
	c.RAM[0x04A] = 0x0C
	c.RAM[0x04F] = 0 // $6F1E-$6F1F
	c.RAM[0x0B8] = 0 // $6AFE-$6AFF
	c.drawSpeedGaugeBar()
	c.redrawScoreAndHiscoreDigits()
}

// writeSpriteEntryYXPattern is ported from $7282-$7289: writes Y=y,
// X=x, pattern=pattern into a raw 4-byte sprite-attribute-table staging
// entry at addr. The 4th byte (color) is deliberately left untouched --
// the original only advances HL past it ("INC HL; INC HL" after writing
// the pattern byte), it never writes there.
func (c *Console) writeSpriteEntryYXPattern(addr uint16, y, x, pattern byte) {
	c.writeRAM(addr, y)
	c.writeRAM(addr+1, x)
	c.writeRAM(addr+2, pattern)
}

// trafficSpritePatternByType is the 9-byte ROM table at $72AF, indexed
// by an object slot's own field 0 -- confirmed via $7275-$727C's own
// "$4040: DE+=A" indexed read.
var trafficSpritePatternByType = [9]byte{0x7C, 0x00, 0x00, 0x84, 0x8C, 0x8C, 0x50, 0x50, 0x60}

// positionTrafficSprite is ported from $724B-$7289, plus the leading
// "always draw" half of its own caller $7229 (the CALL L724B at $7229
// itself). Despite its name (kept for history), this is NOT type-6-
// specific: direct-call ROM tracing (see docs/ANALYSIS.md's "$70A8
// scoping notes") confirms it's the shared tail every dispatch case's
// own RET falls into, via the return address $70A8 pre-pushes before
// its own $4045-trampolined type dispatch ($70C2-$70C6) -- type 6 is
// simply entered directly here instead of via a RET, since the dispatch
// table's own type-6 entry points straight at this address. runObjectSlots
// now calls this (and finishObjectSlot below) after every non-empty
// slot's own dispatch, regardless of type.
//
// Reads an object slot's own row/column (fields 6/8) and, for field-0
// values 2 or 4 (the latter only every 4th frame, $E003&3==0), bumps the
// row by 1 for this frame's draw only -- a transient visual nudge, not
// written back to field 6 -- distinct from the position accumulator
// handleObjectType2/handleObjectType3 already manage. If the resulting
// row has reached $D0 (the real hardware sprite-list-terminator value),
// stops without writing anything further. Otherwise writes Y/X/pattern
// (pattern looked up from trafficSpritePatternByType by the slot's own
// field 0) into the object's own 2-sprite staging entry at
// trafficSpriteEntryAddr(idx), TWICE: once with the looked-up pattern,
// once more 4 bytes later with pattern+4 (the second half of the
// 2-sprite pair) -- confirmed via direct-call tracing showing both
// halves written (e.g. field0=3 produces pattern bytes $84 then $88).
// Deliberately not touching the 4th (color) byte of either half,
// matching writeSpriteEntryYXPattern's own literal write count.
//
// $7234-$7248 -- an additional write to a second, fixed sprite pair at
// $E132 for objects whose own field 0 is 6 or 7 -- is also now fully
// ported (see the trailing block below). The gate itself was resolved
// first: $724B's own body brackets its "LD A,(HL)" (reading field 0)
// with two "EX AF,AF'" (at $724E and $7251), a pure save/restore trick
// to protect the real A register across that one load -- the *net
// effect*, confirmed by reading both instructions together rather than
// in isolation, is that shadow AF ends up holding field 0, not some
// externally-supplied state. $7229's own "EX AF,AF'; CP 6; CP 7" (right
// after `CALL $724B` returns) is exactly reading that back -- i.e. the
// gate is simply `field0==6 || field0==7`, the same field this function
// already reads.
//
// The pattern index this block writes (`$7239: ADD A,4`) uses whatever
// the *real* A register held at $7229's own entry -- previously flagged
// as depending on which of several structurally different call paths
// reached this shared continuation. Resolved by a full single-step trace
// of a real, reachable object (field0=6, type=1, driven through
// activateStagedObjectType's own normal chain -- type 6/7 themselves
// turn out to never actually get set anywhere in the traced object
// system, but field 0 is independent of dispatch type and this shared
// continuation runs every frame regardless of type, so field0=6/7 alone
// is enough): the value is always exactly `pattern+4` -- $724B's own
// last computed value, left over from its own second sprite-pair write
// just before it returns, completely independent of which dispatch case
// called it. Confirmed stable across 20 consecutive frames and with
// field0=7 as well. In the one path where $724B *doesn't* reach that
// write (the `row==$D0` early return below), A is instead left holding
// $D0 itself (confirmed the same way) -- but since that row also feeds
// into this block's own `row+$10`, the resulting sprite pair lands at
// row $E0, off the bottom of the visible screen, making the exact
// pattern moot in that specific case.
func (c *Console) positionTrafficSprite(idx byte, base uint16) (row byte) {
	field0 := c.readRAM(base)
	row = c.readRAM(base + 6)
	if field0 == 2 || (field0 == 4 && c.RAM[0x003]&3 == 0) { // $7257-$7267
		row++
	}
	shadowBase := row // $7271-$7274: RET Z leaves A=row when the main draw below is skipped
	if row != 0xD0 {
		col := c.readRAM(base + 8)
		pattern := trafficSpritePatternByType[field0]
		addr := trafficSpriteEntryAddr(idx)
		c.writeSpriteEntryYXPattern(addr, row, col, pattern)     // $7282-$7289
		c.writeSpriteEntryYXPattern(addr+4, row, col, pattern+4) // $7280-$7289 (fallthrough repeat)
		shadowBase = pattern + 4                                 // $724B's own leftover A -- see doc comment above
	}

	if field0 == 6 || field0 == 7 { // $722D-$7233
		row2 := row + 0x10 // $7234-$7237
		col := c.readRAM(base + 8)
		c.writeSpriteEntryYXPattern(0xE132, row2, col, shadowBase+4) // $723B-$723E, $7280-$7289
		c.writeRAM(0xE135, 0x01)                                     // $7244-$7248
		c.writeSpriteEntryYXPattern(0xE136, row2, col, shadowBase+8) // second half of the same double-write
		c.writeRAM(0xE139, 0x08)                                     // $7241-$7242
	}
	return row
}

// finishObjectSlot is ported from $728D-$72A3, the tail that follows
// positionTrafficSprite in the same shared per-slot continuation (see
// its own doc comment). Re-reads the object's own persistent row field
// (field 6, unaffected by positionTrafficSprite's own transient draw-only
// increment) and, if it has reached the band [$C3,$CF) -- the object is
// being retired near the bottom of the screen -- forces that field to
// $DF (an off-screen sentinel row, not the $D0 list-terminator value),
// clears the object's type to 0 via setObjectType (removing it), and
// redraws once more at the new position so the sprite pair doesn't
// linger at its last visible row for one extra frame.
func (c *Console) finishObjectSlot(idx byte, base uint16) {
	if c.readRAM(base+6)-0xC3 < 0x0C { // $7292-$7296
		c.writeRAM(base+6, 0xDF)           // $72A4
		c.setObjectType(base, 0)           // $72A6-$72A9
		c.positionTrafficSprite(idx, base) // $72AA: CALL L7229 -- redraw at the retired position
	}
}

// stageMetaTileIDs is the 96-byte ROM array at $537D: exactly 6 stages,
// 16 bytes each -- confirmed precisely, not assumed: the byte right
// after ($53DD) is exactly where writeApproachingObjectRecord's own
// already-ported code begins ($53DD: LD HL,(E078h) = bytes 2A 78 E0),
// with no gap or slack. Each stage's own 16 bytes are a sequence of
// meta-tile IDs (small integers, 1-15) padded with $FF once real data
// runs out for that stage.
var stageMetaTileIDs = [96]byte{
	0x01, 0x02, 0x04, 0x05, 0x02, 0x03, 0x04, 0x08, 0x02, 0x06, 0x05, 0x04, 0x02, 0x01, 0x03, 0xFF,
	0x01, 0x02, 0x08, 0x04, 0x05, 0x01, 0x0F, 0x08, 0x01, 0x08, 0x05, 0x0B, 0x0F, 0xFF, 0xFF, 0xFF,
	0x02, 0x08, 0x06, 0x0F, 0x05, 0x0A, 0x03, 0x02, 0x0A, 0x08, 0x07, 0x05, 0x08, 0x06, 0x0A, 0xFF,
	0x01, 0x02, 0x09, 0x08, 0x05, 0x0A, 0x02, 0x0A, 0x08, 0x09, 0x05, 0x0A, 0x03, 0x0A, 0x08, 0xFF,
	0x0F, 0x0A, 0x06, 0x0B, 0x0F, 0x0B, 0x05, 0x0B, 0x08, 0x06, 0x0F, 0x0A, 0x06, 0x0A, 0xFF, 0xFF,
	0x01, 0x04, 0x08, 0x0F, 0x08, 0x0F, 0x05, 0x0A, 0x0E, 0x08, 0x0F, 0x02, 0x0A, 0x0F, 0x0A, 0xFF,
}

// metaTileDefinitions is the 90-byte ROM table at $5311: exactly 15
// meta-tile definitions, 6 bytes each -- confirmed precisely: the next
// 18 bytes after this table are metaTileColors, a separate,
// already-identified, exactly-9-entry table, with no gap. Indexed by
// (meta-tile ID - 1), or -2 for ID $0A specifically on Level A
// (aliasing it to ID 9's own definition).
var metaTileDefinitions = [15][6]byte{
	{0x00, 0x04, 0x20, 0x00, 0x08, 0x28},
	{0x00, 0x04, 0x10, 0x01, 0x10, 0x28},
	{0x00, 0x04, 0x18, 0x00, 0x08, 0x18},
	{0x00, 0x04, 0x28, 0x00, 0x08, 0x18},
	{0x00, 0x04, 0x18, 0x08, 0x08, 0x28},
	{0x00, 0x04, 0x28, 0x01, 0x08, 0x20},
	{0x02, 0x04, 0x28, 0x00, 0x08, 0x20},
	{0x00, 0x04, 0x28, 0x03, 0x08, 0x20},
	{0x06, 0x04, 0x18, 0x00, 0x08, 0x30},
	{0x00, 0x04, 0x30, 0x07, 0x08, 0x1C},
	{0x02, 0x04, 0x28, 0x03, 0x08, 0x20},
	{0x04, 0x04, 0x28, 0x02, 0x08, 0x20},
	{0x04, 0x04, 0x28, 0x05, 0x08, 0x20},
	{0x05, 0x04, 0x18, 0x05, 0x08, 0x28},
	{0x03, 0x04, 0x28, 0x03, 0x08, 0x20},
}

// metaTileColors is the 18-byte ROM table at $536B: exactly 9 entries,
// 2 bytes each -- indexed not by meta-tile ID but by a staged object
// record's own byte 0 (confirmed self-consistent: every one of
// metaTileDefinitions' own byte-0 values is in 0..8, exactly fitting
// this table).
var metaTileColors = [9][2]byte{
	{0x01, 0x0C}, {0x01, 0x04}, {0x01, 0x04}, {0x01, 0x0D}, {0x0B, 0x01},
	{0x0B, 0x01}, {0x01, 0x08}, {0x01, 0x08}, {0x0F, 0x0D},
}

// stageObjectRecord is the shared loop body at $705F-$70A5 within
// advanceLevelStream (confirmed, after an initial hand-tracing error,
// via a standalone register-flow simulation -- the same technique
// already used for $77C8 and $5197 -- that it runs twice with clean,
// unstaggered addressing: once building $E0C3 from def[0:3], once
// building $E0D3 from def[3:6]): expands 3 bytes of a meta-tile
// definition into one 16-byte staging record at dst. def[0] goes to
// offset 0 unchanged; a fixed $01 goes to offset 1; def[1] (doubled on
// Level A, $706C-$7070) goes to offset 3; def[2] goes to offset 4;
// offset 9 gets $80 normally, $6C if the current race stage is 3,
// further overridden to $E4 if def[0] is 2 or 5; offset 10 gets a
// fixed $00. Every other offset (2, 5-8, 11-15) is deliberately left
// untouched, exactly matching the original -- whatever was already
// there (stale data from a previous spawn cycle, or zero from
// ColdBoot).
func (c *Console) stageObjectRecord(dst uint16, def [3]byte) {
	c.writeRAM(dst, def[0]) // $7060-$7062
	c.writeRAM(dst+1, 0x01) // $7064-$7066
	b1 := def[1]
	if c.RAM[0x03B] == 0 { // $706C-$7070: Level A doubles it
		b1 *= 2
	}
	c.writeRAM(dst+3, b1)     // $7071
	c.writeRAM(dst+4, def[2]) // $7074

	override := byte(0x80) // $7082 (default)
	if c.RAM[0x043] == 3 { // $707D-$7086
		override = 0x6C
	}
	if def[0] == 2 || def[0] == 5 { // $7088-$7091
		override = 0xE4
	}
	c.writeRAM(dst+9, override) // net effect of $707B-$7093
	c.writeRAM(dst+10, 0x00)    // $7094-$7095
}

// advanceLevelStream is ported from $7020-$70A7: advances the level-
// data read-ahead cursor ($E0BD) by 1 byte through the current race
// stage's own 16-byte slice of stageMetaTileIDs, wrapping back to that
// slice's own start whenever the byte read is $FF (confirmed via this
// literal wrap-on-terminator check, not inferred) -- $E0BD is the
// "virtual Y-coordinate"/read-ahead cursor into the level stream. That
// byte (a meta-tile ID) selects one of the 15 metaTileDefinitions,
// which gets expanded via stageObjectRecord into *both* of the 2
// 16-byte staging slots ($E0C3 from the definition's own bytes 0-2,
// $E0D3 from bytes 3-5), and $E0C1 is reset to 2 (spawnStagedTrafficObject's
// own "how many staged objects are left to place" counter). Called
// once at race start/restart (from initPlayerCarSpritesAndFuel's own
// larger $504C context and from stateCrashTrigger) and again, in a
// continuous loop with spawnStagedTrafficObject, whenever that counter
// reaches zero.
func (c *Console) advanceLevelStream() {
	pos := uint16(c.RAM[0x0BD]) | uint16(c.RAM[0x0BE])<<8
	pos++
	c.RAM[0x0BD] = byte(pos)
	c.RAM[0x0BE] = byte(pos >> 8)

	if stageMetaTileIDs[pos-0x537D] == 0xFF { // $7027-$7029
		stage := c.RAM[0x043] - 1
		pos = 0x537D + uint16(stage)*16 // $702B-$703A
		c.RAM[0x0BD] = byte(pos)
		c.RAM[0x0BE] = byte(pos >> 8)
	}

	id := stageMetaTileIDs[pos-0x537D]   // $703D-$7041
	idx := id - 1                        // $7049: 1-indexed -> 0-indexed
	if c.RAM[0x03B] == 0 && id == 0x0A { // $7042-$7048: Level A aliases ID $0A
		idx = id - 2
	}
	def := metaTileDefinitions[idx] // $704E-$7054

	c.RAM[0x0C1] = 2 // $7055-$7058
	c.RAM[0x0C2] = 0 // $705A-$705B
	c.stageObjectRecord(0xE0C3, [3]byte{def[0], def[1], def[2]})
	c.stageObjectRecord(0xE0D3, [3]byte{def[3], def[4], def[5]})
}

// activateStagedObject is $6F67-$701F's own "this staging slot is
// ready to place" path, factored out of spawnStagedTrafficObject for
// clarity -- see that function's own doc comment for the full
// behavior. Returns restaged=true if $E0C1 reached zero and control
// fell through into advanceLevelStream, matching $701D's own "JP
// NZ,$6F5D" (only *not* taken -- falling through to $7020 instead --
// when the counter is exhausted); the caller should stop entirely in
// that case, the same way $7020's own eventual RET ends the whole
// per-frame call.
func (c *Console) activateStagedObject(index byte, slotBase uint16) (restaged bool) {
	// $6F67-$6F7C: refresh the *other* staging slot's own field 3 --
	// the index-to-address mapping is inverted from the naive
	// expectation (index 0 touches $E0D6, index 1 touches $E0C6);
	// ported exactly as traced, not "corrected."
	otherFieldAddr := uint16(0xE0C6)
	if index == 0 {
		otherFieldAddr = 0xE0D6
	}
	val := byte(0x0C)
	if c.RAM[0x03B] != 0 { // Level B
		val = 0x07
	}
	c.writeRAM(otherFieldAddr, val)

	// $6F7D-$6F96: search $70A8's own 2 object slots for an empty one.
	found := false
	var targetIdx byte
	for targetIdx = 0; targetIdx < 2; targetIdx++ {
		if c.readRAM(objectSlotBase(targetIdx)+1) == 0 {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	c.RAM[0x0B0] = targetIdx // $6F98-$6F99

	// $6F9D-$6FC6: search for an on-track spawn column, starting at
	// row 0 and nudging the column left by 8 until safe. The original
	// loop is genuinely unbounded -- on real hardware, with real road
	// data always present by the time an object can spawn (spawning is
	// gated on speed, well after the one-shot road paint at race
	// start), some column always is safe. Ported with an explicit cap
	// at 32 attempts (every distinct byte-wrapped column value `col -=
	// 8` can ever produce, so further attempts would only repeat
	// values already tried) as a deliberate, documented deviation from
	// literal translation: this project's own road-strip is confirmed
	// static (see the road-scroll dead-code finding elsewhere in
	// ANALYSIS.md), so a given static frame's own row 0 is not
	// guaranteed to have any safe column at all, and an actual infinite
	// loop would hang the whole program rather than just misplace one
	// sprite.
	lane := c.readRAM(slotBase + 4)
	col := ((c.RAM[0x098] >> 2) & 0x1F << 3) + lane // $6FA3-$6FAD
	for attempt := 0; attempt < 32; attempt++ {
		c.writeRAM(slotBase+6, 0)
		c.writeRAM(slotBase+8, col)
		if _, _, unsafe := c.checkOffTrackCollision(slotBase + 6); !unsafe {
			break
		}
		col -= 8 // $6FBE-$6FC1
	}

	// $6FC8-$6FEA: pick the final spawn row.
	d := c.readRAM(slotBase)
	if c.RAM[0x03B] == 0 { // $6FCC-$6FD2: Level A forces d=0 for this check only
		d = 0
	}
	row := byte(0xF0)
	if (d == 2 || d == 5) && c.RAM[0x000] != 6 { // $6FD8-$6FEA
		row = 0xBF
	}
	c.writeRAM(slotBase+6, row)

	// $6FEC-$6FF4: the actual spawn -- copy the whole 16-byte staged
	// record into the found object slot.
	target := objectSlotBase(targetIdx)
	for i := uint16(0); i < 16; i++ {
		c.writeRAM(target+i, c.readRAM(slotBase+i))
	}
	c.writeRAM(slotBase+1, 0) // $6FF9-$6FFB: mark this staging slot consumed

	// $6FFD-$7017: sprite colors from metaTileColors, keyed by the
	// staged record's own (un-Level-adjusted) byte 0.
	colors := metaTileColors[c.readRAM(slotBase)]
	spriteAddr := trafficSpriteEntryAddr(targetIdx)
	c.writeRAM(spriteAddr+3, colors[0])
	c.writeRAM(spriteAddr+7, colors[1])

	c.RAM[0x0C1]-- // $7019-$701C
	if c.RAM[0x0C1] != 0 {
		return false
	}
	c.advanceLevelStream() // $701D falls through to $7020
	return true
}

// spawnStagedTrafficObject is ported from $6F25-$701F (GameplayTick's
// own $524B callee, previously "compares $E04F to $B8, touches $E0C1.
// Not traced."): the per-frame consumer of the 2 staging slots
// advanceLevelStream populates. Gated on speed >=$B8 and on $E0C1!=0
// (nothing staged). For each of the 2 staging slots ($E0C3, $E0D3): if
// the slot looks empty (field 1==0), skip it; if it's staged but not
// yet claimed (field 3==0), attempt to spawn it now (activateStagedObject);
// otherwise run a per-frame countdown on it (field 3--, gated to odd
// frames below a speed threshold, $6F4F-$6F5C).
func (c *Console) spawnStagedTrafficObject() {
	if c.RAM[0x04F] < 0xB8 { // $6F25-$6F2A
		return
	}
	if c.RAM[0x0C1] == 0 { // $6F2B-$6F30
		return
	}
	c.RAM[0x0C2] = 0 // $6F31-$6F32

	for {
		index := c.RAM[0x0C2]
		slotBase := 0xE0C3 + uint16(index)*16
		c.RAM[0x0BF] = byte(slotBase)
		c.RAM[0x0C0] = byte(slotBase >> 8)

		if c.readRAM(slotBase+1) != 0 { // $6F44-$6F47
			if c.readRAM(slotBase+3) == 0 { // $6F49-$6F4D
				if c.activateStagedObject(index, slotBase) {
					return // fell through into advanceLevelStream, which itself RETs
				}
			} else if c.RAM[0x04F] >= 0xC8 || c.RAM[0x003]&1 != 0 { // $6F4F-$6F5A
				c.writeRAM(slotBase+3, c.readRAM(slotBase+3)-1) // $6F5C
			}
		}

		c.RAM[0x0C2]++         // $6F5D-$6F60
		if c.RAM[0x0C2] == 2 { // $6F61-$6F64
			return
		}
	}
}
