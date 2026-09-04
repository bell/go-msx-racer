package game

// This file ports the input-reading pipeline: $46C3 (joystick/keyboard
// sampling, combined into one byte) and $46B6-$46C2 (UpdateInputAndTimers,
// which turns that into raw + edge-triggered state in RAM). Both were
// previously stubbed pending identification of the BIOS calls they use
// (WRTPSG $0093, RDPSG $0096, SNSMAT $0141) -- now confirmed, so both are
// fully ported.
//
// cmd/play's own updateKeyboardMatrix feeds real host keyboard state into
// msx.VDP's KeyboardMatrix (see its own doc comment for the exact key
// mapping), so this pipeline observes genuine player input there --
// PSG register 14 (joystick port A) still always reads as "nothing
// pressed" (no joystick support), but the keyboard path this game
// actually needs is live.

// rrca is the Z80 RRCA instruction: rotate A right circularly (bit 0 into
// bit 7, everything else shifts down one).
func rrca(a byte) byte { return (a >> 1) | (a << 7) }

// readCombinedInput is ported from $46C3-$46FB: reads joystick port A
// (PSG register 14, after configuring PSG register 15 via WRTPSG) and two
// keyboard matrix rows (7 and 8) via SNSMAT, merging all of it into a
// single byte the rest of the input pipeline treats uniformly regardless
// of whether the player is using a joystick or the keyboard.
//
// Translated as an exact, mechanical instruction-by-instruction mirror
// (register renames only, no algebraic simplification of the bit
// shuffling) rather than worked out by hand which physical key/joystick
// line ends up at which final bit -- reasoning through several chained
// RRCA rotations by hand is exactly the kind of place a subtle
// off-by-one has bitten this project before. The mechanism is confirmed
// correct because it's a literal translation; which bit means "up" vs
// "fire" in the result is not independently confirmed.
func (c *Console) readCombinedInput() byte {
	c.Sys.WritePSGRegister(0x0F, 0x8F) // $46C3-$46C7: LD E,$8F; LD A,$0F; CALL $0093 (WRTPSG)

	c.Sys.DisableInterrupts()
	joystick := c.Sys.ReadPSGRegister(0x0E) // $46CA-$46CD: LD A,$0E; CALL $0096 (RDPSG)
	c.Sys.EnableInterrupts()
	joystick = ^joystick & 0x3F // $46D1-$46D2: CPL; AND $3F

	row7 := c.Sys.SenseKeyboardMatrixRow(0x07) // $46D5-$46D7
	r7 := rrca(^row7) & 0x20                   // $46DA-$46DC: CPL; RRCA; AND $20

	row8 := c.Sys.SenseKeyboardMatrixRow(0x08) // $46DF-$46E1
	cpl8 := ^row8                              // $46E4: CPL

	r8x2 := rrca(rrca(cpl8))       // $46E5-$46E6: RRCA; RRCA
	combined := (r8x2 & 0x04) | r7 // $46E8-$46EA: AND $04; OR E

	r8x4 := rrca(rrca(r8x2)) // $46ED-$46EE: RRCA; RRCA
	combined |= r8x4 & 0x18  // $46F0-$46F2: AND $18; OR C

	r8x5 := rrca(r8x4)      // $46F5: RRCA
	combined |= r8x5 & 0x03 // $46F6-$46F8: AND $03; OR C

	return joystick | combined // $46F9-$46FA: POP BC; OR B
}

// updateEdgeTriggeredPair mirrors the shared tail at $46BC-$46C2: given
// the RAM offset holding the "raw, level-triggered" state, writes
// newValue there and returns (and stores one byte earlier, at
// offset-1) the edge-triggered "just pressed" bits -- bits set in
// newValue that weren't set in the previous call's value. Two call
// sites reuse this exact tail with different RAM pairs: $46B6
// (UpdateInputAndTimers, offset $009/$008) and $43C3
// (stateExtra43C3, offset $03A/$039) -- the latter samples input
// completely independently of the former, since it runs as a
// "continuation" after whichever state handler's own RET fires and
// needs edge-detection unaffected by whatever $E008/$E009 already
// reflect this same frame.
func (c *Console) updateEdgeTriggeredPair(offset int, newValue byte) byte {
	old := c.RAM[offset]
	c.RAM[offset] = newValue
	edge := (newValue ^ old) & newValue
	c.RAM[offset-1] = edge
	return edge
}

// UpdateInputAndTimers is ported from $46B6-$46C2: reads the combined
// joystick+keyboard byte (readCombinedInput) into $E009 (the raw,
// level-triggered input state), and computes $E008 as the edge-triggered
// "just pressed this frame" bits via updateEdgeTriggeredPair.
func (c *Console) UpdateInputAndTimers() {
	c.updateEdgeTriggeredPair(0x009, c.readCombinedInput()) // $46B6-$46C2
}
