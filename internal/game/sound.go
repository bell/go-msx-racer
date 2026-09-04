package game

import "go-msx-racer/assets"

// This file ports the PSG (AY-3-8910) sound engine. It is a hand-written,
// priority-based music/SFX player: up to 3 simultaneous channels, each
// advancing through a compact note-command bytecode stream stored in the
// ROM (see assets.SoundData and docs/ANALYSIS.md). Confidence is high for
// the trigger/priority logic (InitVDPAndSoundEngine, TriggerSound) since
// every register and branch was traced against the disassembly;
// TickSoundChannels captures the confirmed 3-channel, 11-byte-record loop
// structure from $4BFF. advanceSoundChannel ($4CBC, $4D00-$4DD5) is now
// ported for the two "note is already playing" paths -- the compact
// nibble-packed command format was worked out by tracing every AND/CP/BIT
// mask against confirmed AY-3-8910 register semantics (tone-period pairs,
// mixer bits, volume registers), not guessed from shape. engineSoundPitchStep
// ($4C25-$4CB9) is also now ported: the separate per-frame pitch-resolution
// path reached only when a channel's stored sound ID is exactly 1 -- see
// TickSoundChannels' doc comment for why that's a special, continuously-
// recomputed sound (confirmed to be pitch-related, not just plausible,
// once traced) rather than a "just triggered" case, which is what an
// earlier pass at this code assumed. installSoundRequest's repeatCount>1
// multi-install loop is also ported -- see its own doc comment for the
// stride-length correction (soundChannelRecordSize, not a literal 7)
// that was needed to get real multi-voice chords playing.

// vdpInitRegisters holds VDP registers 0-7 as written during boot, ported
// from the 8-byte table at $46A9-$46B0 (also saved verbatim as
// assets/vdp_init_registers.bin). Selects MSX Screen 2 (Graphic 1) mode.
var vdpInitRegisters = [8]byte{0x02, 0xE2, 0x0E, 0x7F, 0x07, 0x76, 0x03, 0xE4}

// soundChannelRecordSize is the stride between the three sound-channel
// state records in work RAM, confirmed by "LD DE,000Bh; ADD IX,DE" in the
// per-frame tick loop at $4C11-$4C20.
const soundChannelRecordSize = 0x0B

// soundChannelBase is $E010, the first of 3 channel records (ported from
// "LD IX,E010h" at $4C07).
const soundChannelBase = 0xE010

// Fields within a channel record, relative to its base address, as used by
// the note player at $4D00-$4DD5 (register indirect via IX+n there).
//
// Two of these turned out to have misleading names from an earlier,
// less-informed pass, corrected here once advanceSoundChannel's body was
// actually traced:
//   - offset $01 ("chOffNoteDuration" below, formerly "chOffActive2") is
//     not a second boolean flag -- it's a note-length *template*, set from
//     a $2X command's low nibble and reloaded into offset $00 at the start
//     of every new note ($4D18-$4D1B).
//   - offset $02 ("chOffSoundID") is what TickSoundChannels actually tests
//     for its "$4C12: LD A,(IX+02h)" gate -- not offset $01, which an
//     earlier version of this port used by mistake (see TickSoundChannels'
//     own doc comment for the fix and what the real gate means). It is
//     both the sound ID *and* the interpreter's activity flag: $4D2F sets
//     it to 0 to silence the channel, and everywhere else it holds the
//     nonzero id last passed to TriggerSound. Bit 7 of that id additionally
//     selects which of the two note-command formats advanceSoundChannel
//     decodes ($4CD9): clear selects a compact "raw tone period" format,
//     set selects a "note name via the pitch table" format -- a
//     deliberate reuse of the request's own high bit as a format
//     selector, not a separate runtime-toggled state.
const (
	chOffActive       = 0x00 // frames remaining in the current note/step; reloaded from chOffNoteDuration, see $4CCA
	chOffNoteDuration = 0x01 // note-length template, set by a $2X command ($4CE9), reloaded into chOffActive at each new note
	chOffSoundID      = 0x02 // see doc comment above: id, active-flag, and note-format selector all at once
	chOffScriptLo     = 0x03 // current read pointer into the note-command bytecode (low byte)
	chOffScriptHi     = 0x04 // current read pointer (high byte)
	chOffTranspose    = 0x05 // octave transpose count for the pitch-table format, set by an $EX command ($4D7F); each unit doubles the looked-up period (one octave)
	chOffAltCounter   = 0x06 // alternate note-off countdown, set by an $FX command ($4D74); substituted into chOffDuration when the primary one hits exactly 12 ($4DA4-$4DA9)
	chOffDuration     = 0x07 // secondary countdown consumed by the post-note "sustain/tremolo" branch at $4D35; going negative there ends that effect
	chOffLoopCount    = 0x08 // reloaded to chOffNoteDuration+2 at each new note ($4D20); decremented by the sustain/tremolo branch
	chOffLoopLimit    = 0x09 // zeroed when a note stream ends ($4D26); not otherwise read by anything ported so far -- possibly consumed by the still-unported $4C25 path
	chOffPitchIndex   = 0x0A // per-repeat transpose amount for the pitch-table format's accumulation loop ($4D8C), NOT the final pitch-table index itself (that comes from the note byte's high nibble, $4D9E) despite the name -- kept unrenamed pending a clearer replacement
)

// soundBaseAddr is $4DD6, the start of assets.SoundData -- see
// soundROM/soundScriptPointer below.
const soundBaseAddr = 0x4DD6

// soundROM reads one byte from the raw $4DD6-$5027 blob (assets.SoundData)
// at its real Z80 address -- the pitch table, the script pointer table,
// and every note-command bytecode stream are all addressed uniformly this
// way in the original, so this port does the same rather than parsing them
// into separate Go structures.
func (c *Console) soundROM(addr uint16) byte {
	return assets.SoundData[addr-soundBaseAddr]
}

// soundScriptPointer is ported from $4BA3-$4BA9's indexing math (`ADD
// A,A` to double baseID into a byte offset, `LD DE,4DE0h` then `CALL
// $4040` to add it in): returns the 16-bit bytecode start address for
// sound/music `baseID`, read from the pointer table starting at $4DE0.
func (c *Console) soundScriptPointer(baseID byte) uint16 {
	addr := uint16(0x4DE0) + uint16(baseID)*2
	lo := c.soundROM(addr)
	hi := c.soundROM(addr + 1)
	return uint16(lo) | uint16(hi)<<8
}

// InitVDPAndSoundEngine is ported from $4685-$46A8: sets the PSG mixer
// register, triggers sound id $1B, clears all of VRAM, then programs the
// 8 VDP mode registers for Screen 2. Sound id $1B was previously labeled
// "startup jingle" throughout this port purely because of when/where it's
// triggered -- checking what its bytecode actually contains (base ID 27,
// and its two `repeatCount` partners 28/29, all point at the single
// terminator byte right at the end of the sound-data blob, $5027)
// disproves that: this id genuinely plays nothing on any of the 3
// channels it could land in. Left triggered here for fidelity (it's
// exactly what the ROM does), but no longer described as a jingle.
func (c *Console) InitVDPAndSoundEngine() {
	c.writePSGRegister7(0xB8)    // $4685-4689: LD A,$B8; CALL $4BF6
	c.TriggerSound(0x1B)         // $468A-468C: sound id $1B (plays nothing -- see doc comment), via $407F
	c.Sys.FillVRAM(0, 0, 0x4000) // $468F-4697: zero all 16KB of VRAM

	c.writeVDPModeRegisters() // $4698: falls through into the register loop
}

// writeVDPModeRegisters is ported from $4698-$46A8: the 8-entry VDP mode
// register table at $46A9 written to registers 0-7. It's the tail of
// InitVDPAndSoundEngine ($4685 falls straight into it), but it is also a
// CALL target in its own right -- stateAttractSequence's phase 0 ($411A)
// re-runs exactly this loop, without the PSG/VRAM-clear work above it,
// every time attract mode restarts.
func (c *Console) writeVDPModeRegisters() {
	for reg, val := range vdpInitRegisters { // $469F-$46A6
		c.Sys.WriteVDPRegister(byte(reg), val)
	}
}

// TriggerSound requests that sound/music id `id` start playing, ported from
// the $407F entry point (TriggerSound(A) == original "LD A,id; CALL $407F").
// $407F itself is just "JP $4B6E"; $4B6E wraps triggerSoundRaw with DI/EI
// and always passes checkPriority=true (D=0 at the call site; see the
// checkPriority doc below for why 0 means "yes, check").
func (c *Console) TriggerSound(id byte) {
	c.Sys.DisableInterrupts()
	c.triggerSoundRaw(id, true)
	c.Sys.EnableInterrupts()
}

// writePSGRegister7 corresponds to the small $4BF6 helper: it stashes the
// value at $E031 (last mixer/volume value written, reused by the per-frame
// tick as the "LD A,(E031h)" at $4BFF) before writing PSG register 7.
func (c *Console) writePSGRegister7(value byte) {
	c.RAM[0x031] = value
	c.Sys.WritePSGRegister(7, value)
}

// triggerSoundRaw is $4B76-$4BD1: the priority-based channel allocator.
// `id` is the requested sound/music id; its low 6 bits ("base id", `id &
// 0x3F`) select a priority tier and slot address, and index the
// sound-script pointer table. `checkPriority` corresponds to the original
// D register being non-zero after "DEC D" -- every known caller (via
// TriggerSound) passes D=0, i.e. checkPriority=true; the base-id==1 tier
// skips the check entirely regardless (see below).
//
// Tiers, from the disassembly's literal thresholds on the base id:
//   - == 1        -> slot $E012, repeatCount 1 (always wins, no priority check)
//   - 2,3,4        -> slot $E028, repeatCount 1
//   - 5..14        -> slot $E01D, repeatCount 2
//   - >= 15        -> slot $E012, repeatCount 3
//
// A new request only overrides what's already in its tier's slot if the new
// base id is numerically >= the currently-playing one (lower numbers appear
// to mean higher priority; RET C at $4BA2 aborts the request otherwise).
func (c *Console) triggerSoundRaw(id byte, checkPriority bool) {
	baseID := id & 0x3F
	var slot uint16
	var repeatCount int
	skipPriorityCheck := false
	switch {
	case baseID == 1:
		slot = 0xE012
		repeatCount = 1
		skipPriorityCheck = true // $4B80: JR Z,$4BA3 jumps straight past the check
	case baseID < 5:
		slot = 0xE028
		repeatCount = 1
	case baseID < 15:
		slot = 0xE01D
		repeatCount = 2
	default:
		slot = 0xE012
		repeatCount = 3
	}

	if checkPriority && !skipPriorityCheck {
		existingBase := c.readRAM(slot) & 0x3F
		if baseID < existingBase {
			return // lower-priority request, and a higher/equal one already owns this slot
		}
	}

	c.installSoundRequest(slot, id, baseID, repeatCount)
}

// installSoundRequest is $4BA3-$4BD1: writes a 5-field trigger record
// (active flags, id, script pointer) starting 2 bytes before `slot`, using
// the sound-script pointer table entry for `baseID`. It also unconditionally
// overwrites $E032-$E035 with two fixed reference tone periods on every
// single trigger, regardless of slot/repeatCount -- $0DEB and $0DEE,
// consumed by engineSoundPitchStep's $4C89-$4C8F. These are NOT 16-bit
// addresses despite how "$4BAF: LD HL,$EB0D; LD (E032h),HL" looks in
// isolation (an earlier pass at this code assumed exactly that): the only
// reader loads them back as `H=(IY+0), L=(IY+1)` -- i.e. the two stored
// bytes read in the OTHER order from how a normal 16-bit load would -- so
// the value actually used is $0DEB/$0DEE, a perfectly ordinary AY-3-8910
// tone-period magnitude, not an address.
//
// For repeatCount > 1 the original loops (DJNZ $4BBC), writing repeatCount
// consecutive records while walking `baseID`, `baseID+1`, ... through the
// pointer table, all sharing the SAME `id` field. An earlier pass at this
// loop misread its per-iteration advance as a 7-byte stride ("$4BCA: LD
// A,07h; ADD A,L; LD L,A" alone) and concluded the repeat records overlap
// into an adjacent channel's tail fields (chOffDuration/chOffLoopCount/
// etc) rather than installing a proper second/third voice -- which,
// correctly, produced no audible chord, since that misreading never wrote
// a nonzero chOffSoundID for those extra channels. Recounting the actual
// instructions between the record's `POP HL` and that add shows FOUR
// intervening `INC HL`s (one after each of the first four field writes),
// so the real net advance per iteration is 4+7=11 bytes -- exactly
// soundChannelRecordSize, i.e. each iteration lands squarely on the NEXT
// real channel's own record start (recordBase, recordBase+11,
// recordBase+22), a complete and valid install each time, not a partial
// overlap. This makes musical sense where the byte-overlap theory didn't:
// repeatCount==3 (base IDs >=15, recordBase $E010) installs the same tune
// across all 3 channels at once (a 3-voice chord); repeatCount==2 (base
// IDs 5-14, recordBase $E01B) installs across channels 1 and 2 (a 2-voice
// harmony); repeatCount==1 always installs on exactly one channel. Each
// channel gets its OWN script pointer (baseID+i), consistent with real ROM
// data showing base IDs N/N+1/N+2 hold genuinely different, non-empty
// bytecode (e.g. base ID 5 vs 6: `22B101C1...` vs `22B103C1...`) --
// i.e. different, presumably harmonizing, parts for each voice.
func (c *Console) installSoundRequest(slot uint16, id, baseID byte, repeatCount int) {
	c.writeRAM(0xE032, 0x0D) // $4BAF-$4BB2: LD HL,$EB0D; LD (E032h),HL
	c.writeRAM(0xE033, 0xEB)
	c.writeRAM(0xE034, 0x0D) // $4BB5-$4BB8: LD HL,$EE0D; LD (E034h),HL
	c.writeRAM(0xE035, 0xEE)

	recordBase := slot - 2
	for i := 0; i < repeatCount; i++ {
		base := recordBase + uint16(i)*soundChannelRecordSize
		c.writeRAM(base+chOffActive, 1)
		c.writeRAM(base+chOffNoteDuration, 1)
		c.writeRAM(base+chOffSoundID, id)
		ptr := c.soundScriptPointer(baseID + byte(i))
		c.writeRAM(base+chOffScriptLo, byte(ptr))
		c.writeRAM(base+chOffScriptHi, byte(ptr>>8))
	}
}

func (c *Console) readRAM(addr uint16) byte     { return c.RAM[addr-0xE000] }
func (c *Console) writeRAM(addr uint16, v byte) { c.RAM[addr-0xE000] = v }

// TickSoundChannels is the per-frame sound update, ported from
// $4BFF-$4C24: re-applies the last-written mixer value, then walks the 3
// channel records (11 bytes apart, starting at $E010) and, for each one,
// tests chOffSoundID -- NOT chOffNoteDuration, which an earlier pass at
// this code used by mistake (confirmed against the raw bytes: "$4C12: LD
// A,(IX+02h)" is unambiguously offset 2). A stored ID of exactly 1 routes
// to engineSoundPitchStep ($4C25) instead of the normal interpreter; per
// triggerSoundRaw's own doc comment, ID 1 is the one base ID with its own
// always-wins priority tier and is always installed into channel 0's slot
// ($E012, i.e. recordBase $E010), consistent with a continuously
// re-resolved sound (confirmed to be an engine-pitch-style effect once
// $4C25 was actually traced -- see engineSoundPitchStep) rather than a
// one-time "just triggered" special case as previously assumed. Any other
// nonzero ID advances normally; zero means the channel is silent (see
// chOffSoundID's own doc comment for how a note stream ending sets it to
// 0 to reach this state).
func (c *Console) TickSoundChannels() {
	c.writePSGRegister7(c.RAM[0x031]) // $4BFF-4C04: LD A,(E031h); CALL $4BF6

	for ch := 0; ch < 3; ch++ {
		base := uint16(soundChannelBase) + uint16(ch)*soundChannelRecordSize
		id := c.readRAM(base + chOffSoundID)
		if id == 1 {
			c.engineSoundPitchStep(base, channelSelector(ch)) // $4C12-$4C17: JR Z,$4C25
			continue
		}
		if id != 0 {
			c.advanceSoundChannel(base)
		}
	}
}

// channelSelector returns the value TickSoundChannels' loop keeps in C
// across all of advanceSoundChannel's callees ($4C05 "LD C,01h", then
// "$4C1D-4C1E: INC C; INC C" per channel): 1, 3, or 5 for channels 0-2.
// This doubles as the AY-3-8910 tone-period *coarse* register number for
// that channel (registers 1/3/5), with the paired *fine* register always
// one below it (0/2/4) -- see writeTonePeriod.
func channelSelector(ch int) byte { return byte(ch*2 + 1) }

// setMixerNoiseBit is ported from $4BE1-$4BFA: sets or clears this
// channel's noise-disable bit in the PSG mixer register ($E031, mirrored
// to PSG register 7 via writePSGRegister7). `channel` is channelSelector's
// value (1/3/5); disable=true sets the bit (silencing noise on that
// channel), disable=false clears it (enabling noise). Bit position is
// (channel-1)/2*... -- traced directly: channel 1/3/5 map to bits 3/4/5 of
// the mixer register, which are exactly the AY-3-8910's per-channel noise
// mute bits (bits 0-2 are the tone mute bits, not used here).
func (c *Console) setMixerNoiseBit(channel byte, disable bool) {
	bitIndex := channel // 1, 3, or 5
	if bitIndex != 1 {
		bitIndex--
	}
	bitIndex <<= 3 // RLCA x3 == rotate left 3 == (for these small values) shift left 3
	mixer := c.RAM[0x031]
	if disable {
		mixer |= bitIndex
	} else {
		mixer &^= bitIndex
	}
	c.writePSGRegister7(mixer)
}

// writeTonePeriod is ported from $4DC3-$4DCB: writes a 12-bit tone period
// (period=hi:lo, hi in the low nibble) to channel `sel`'s coarse (`sel`)
// and fine (`sel`-1) AY-3-8910 tone registers.
func (c *Console) writeTonePeriod(sel byte, hi, lo byte) {
	c.Sys.WritePSGRegister(sel, hi)
	c.Sys.WritePSGRegister(sel-1, lo)
}

// writeVolume is the shared tail at $4D57-$4D5C: writes `value` to
// channel `sel`'s AY-3-8910 volume register (8/9/10 for tone-period
// registers 1/3/5 respectively, via "RRCA; ADD A,$88").
func (c *Console) writeVolume(sel byte, value byte) {
	reg := byte((sel>>1)|(sel<<7)) + 0x88 // RRCA
	c.Sys.WritePSGRegister(reg, value)
}

// pitchTable is the confirmed 13-entry PSG tone-period table at $4DD6
// (assets.SoundData's first 13 bytes -- see soundROM).
func (c *Console) pitchTable(noteIndex byte) byte {
	return c.soundROM(0x4DD6 + uint16(noteIndex))
}

// advanceScriptPointer is ported from $4DCE-$4DD5: advances the script
// read pointer (HL) one byte past wherever the caller left it, and commits
// the result back into the channel record's chOffScriptLo/Hi.
func (c *Console) advanceScriptPointer(recordBase uint16, hl uint16) {
	hl++
	c.writeRAM(recordBase+chOffScriptLo, byte(hl))
	c.writeRAM(recordBase+chOffScriptHi, byte(hl>>8))
}

// advanceSoundChannel is the per-channel note-bytecode interpreter, ported
// from $4CBC down into $4D00-$4DD5, for the two paths reached once a note
// stream is genuinely playing (chOffSoundID not 0 or 1 -- see
// TickSoundChannels). `recordBase` is one of the 3 channel records
// ($E010/$E01B/$E026); `sel` is channelSelector's value for this channel.
//
// Top-level shape, confirmed instruction-by-instruction:
//  1. ($4CBC-$4CC0) If the stored ID's bit 6 is clear, mute this channel's
//     noise -- bit 6, like bit 7 (see chOffSoundID's doc comment), is a
//     format/mode selector baked into the *requested sound id itself*.
//  2. ($4CC3-$4CC7) If chOffSoundID's raw byte has its sign bit set --
//     which in practice means an id >= $80, i.e. bit 7 set, the
//     "pitch-table" format -- jump straight to the sustain/tremolo branch
//     ($4D35) instead of reading a new command. This lets a single
//     pitch-table note "ring on" over several calls before the next
//     command byte is read, distinct from bit 7 of the *current command
//     byte* tested later in step 4.
//  3. ($4CCA-$4CCD) Otherwise, decrement chOffActive (frames remaining);
//     idle if not yet zero.
//  4. ($4CCE-$4D5D / $4D5F-$4DAC) Read the next command byte(s) and
//     decode one of two formats selected by chOffSoundID bit 7 (see its
//     own doc comment): a compact "raw tone period" format (optional $2X
//     duration-set and $1X noise-period prefix bytes, then a period+
//     volume byte pair written directly to the tone/volume registers), or
//     a "pitch-table note" format (optional $DX/$FX/$EX prefix bytes
//     setting per-note modifiers, then a note nibble looked up in the
//     13-entry pitch table and shifted by the accumulated octave
//     transpose).
//  5. A byte of $FF at the current script position ($4CD4-$4CD7) ends the
//     stream: silences the channel (chOffSoundID=0) and mutes its noise.
//
// See docs/ANALYSIS.md's sound engine section for the full per-instruction
// trace this was built from, including which fields' exact purposes
// remain unconfirmed (chOffLoopLimit, chOffPitchIndex's real role).
func (c *Console) advanceSoundChannel(recordBase uint16) {
	sel := channelSelector(int(recordBase-soundChannelBase) / soundChannelRecordSize)
	id := c.readRAM(recordBase + chOffSoundID)

	if id&0x40 == 0 { // $4CBC-$4CC0: BIT 6,A; CALL Z,$4BE1 (D=1 -> disable)
		c.setMixerNoiseBit(sel, true)
	}

	if int8(id) < 0 { // $4CC3-$4CC7: OR A; JP M,$4D35
		c.advanceSoundChannelSustain(recordBase, sel)
		return
	}

	active := c.readRAM(recordBase + chOffActive)
	active--
	c.writeRAM(recordBase+chOffActive, active) // $4CCA: DEC (IX+00h)
	if active != 0 {
		return // $4CCD: RET NZ
	}

	c.advanceSoundChannelReadCommand(recordBase, sel, id)
}

// advanceSoundChannelReadCommand is $4CCE-$4D5D plus $4D5F-$4DAC: reads
// and decodes the next bytecode command once chOffActive reaches zero.
// Split out of advanceSoundChannel for readability; not a separate
// original routine.
func (c *Console) advanceSoundChannelReadCommand(recordBase uint16, sel, id byte) {
	hl := uint16(c.readRAM(recordBase+chOffScriptLo)) | uint16(c.readRAM(recordBase+chOffScriptHi))<<8

	if c.soundROM(hl) == 0xFF { // $4CD4-$4CD7
		c.writeRAM(recordBase+chOffLoopLimit, 0) // $4D25-$4D26
		c.setMixerNoiseBit(sel, true)            // $4D29-$4D2B: D=1 -> disable
		c.writeRAM(recordBase+chOffSoundID, 0)   // $4D2E-$4D2F
		c.writeVolume(sel, 0)                    // $4D32-$4D33: JR $4D57 with H=0
		return
	}

	if id&0x80 != 0 { // $4CD9-$4CDD: BIT 7,(IX+02h); JP NZ,$4D5F
		c.advanceSoundChannelNoteFormat(recordBase, sel, hl)
		return
	}

	// $4CE0-$4D23: "raw tone period" format.
	b := c.soundROM(hl)
	if b&0xF0 == 0x20 { // $4CE0-$4CEC: optional $2X duration-set prefix
		c.writeRAM(recordBase+chOffNoteDuration, c.soundROM(hl)&0x0F)
		hl++
	}
	b = c.soundROM(hl)
	if b&0xF0 == 0x10 { // $4CED-$4D02: optional $1X noise-period prefix
		c.Sys.WritePSGRegister(6, c.soundROM(hl)&0x1F) // PSG reg 6 = noise period
		c.setMixerNoiseBit(sel, false)                 // D=0 -> enable
		hl++
	}

	b = c.soundROM(hl) // $4D03-$4D0A: period-high-nibble/volume byte, then a raw period-low byte
	periodHi := b & 0x0F
	volumeHi := b & 0xF0
	hl++
	periodLo := c.soundROM(hl)
	c.advanceScriptPointer(recordBase, hl) // $4D0B: CALL $4DCE

	c.writeTonePeriod(sel, periodHi, periodLo) // $4D0E-$4D11

	c.advanceSoundChannelNewNote(recordBase, sel, volumeHi>>4) // $4D12-$4D23: nibble-swapped volumeHi falls straight through into the shared $4D17 tail
}

// advanceSoundChannelNoteFormat is $4D5F-$4DAC plus the shared pitch-table
// lookup at $4DAF-$4DD5: the "note via the 13-entry pitch table" command
// format, selected when the channel's stored sound ID has bit 7 set.
func (c *Console) advanceSoundChannelNoteFormat(recordBase uint16, sel byte, hl uint16) {
	b := c.soundROM(hl)
	if b&0xF0 == 0xD0 { // $4D5F-$4D6C: optional $DX prefix
		c.writeRAM(recordBase+chOffPitchIndex, b&0x0F)
		hl++
		b = c.soundROM(hl)
	}
	if b >= 0xF0 { // $4D6E-$4D77: optional $FX prefix
		c.writeRAM(recordBase+chOffAltCounter, b&0x0F)
		hl++
		b = c.soundROM(hl)
	}
	if b >= 0xE0 { // $4D79-$4D82: optional $EX prefix
		c.writeRAM(recordBase+chOffTranspose, b&0x0F)
		hl++
		b = c.soundROM(hl)
	}

	noteLow := b & 0x0F // $4D84-$4D86
	pitchIndex := c.readRAM(recordBase + chOffPitchIndex)
	accum := pitchIndex
	if pitchIndex != 0 { // $4D87-$4D91: DJNZ accumulate, exact byte-wraparound semantics (noteLow==0 loops 256 times, matching real hardware, not skipping)
		b := noteLow
		for {
			accum += pitchIndex
			b--
			if b == 0 {
				break
			}
		}
	}
	c.writeRAM(recordBase+chOffNoteDuration, accum)

	b = c.soundROM(hl)
	c.advanceScriptPointer(recordBase, hl) // $4D94-$4D97: CALL $4DCE

	noteHigh := (b & 0xF0) >> 4 // $4D98-$4D9E
	newDuration := int8(noteHigh) - 12
	c.writeRAM(recordBase+chOffDuration, byte(newDuration)) // $4D9F-$4DA1
	if newDuration != 0 {                                   // $4DA4-$4DA9: JR Z skips this substitution
		c.writeRAM(recordBase+chOffDuration, c.readRAM(recordBase+chOffAltCounter))
	}

	// $4DAC: CALL $4D17 -- H at this point is whatever chOffDuration was
	// just set to above (the Z80's A register carries straight through).
	c.advanceSoundChannelNewNote(recordBase, sel, c.readRAM(recordBase+chOffDuration))

	transpose := c.readRAM(recordBase + chOffTranspose) // $4DAF-$4DC1
	period := uint16(c.pitchTable(noteHigh))
	for i := byte(0); i < transpose; i++ {
		period <<= 1
	}
	c.writeTonePeriod(sel, byte(period>>8), byte(period)) // $4DC3-$4DCB
}

// advanceSoundChannelNewNote is the shared tail at $4D17-$4D23: reloads
// chOffActive/chOffLoopCount from chOffNoteDuration, then writes
// `hVolume` to the volume register. Reached two ways in the original: the
// raw-period format falls straight through into it after its own nibble
// swap ($4D12-$4D16, hVolume = that swapped value); the pitch-table format
// reaches it via a genuine CALL ($4DAC), passing whatever chOffDuration
// was just set to (the Z80's A register carries the value across the call
// boundary -- there's no explicit parameter-passing instruction for it).
func (c *Console) advanceSoundChannelNewNote(recordBase uint16, sel byte, hVolume byte) {
	duration := c.readRAM(recordBase + chOffNoteDuration)
	c.writeRAM(recordBase+chOffActive, duration)
	c.writeRAM(recordBase+chOffLoopCount, duration+2)
	c.writeVolume(sel, hVolume) // $4D57-$4D5C
}

// advanceSoundChannelSustain is $4D35-$4D56: runs while a pitch-table-
// format note (chOffSoundID bit 7 set) is "ringing on" between command
// reads -- a periodic volume adjustment (tremolo-like), gated by
// chOffLoopCount vs chOffActive, that eventually falls through to reading
// the next command once chOffActive itself reaches zero.
func (c *Console) advanceSoundChannelSustain(recordBase uint16, sel byte) {
	active := c.readRAM(recordBase + chOffActive)
	active--
	c.writeRAM(recordBase+chOffActive, active) // $4D35: DEC (IX+00h)
	if active == 0 {                           // $4D38: JP Z,$4CCE
		id := c.readRAM(recordBase + chOffSoundID)
		c.advanceSoundChannelReadCommand(recordBase, sel, id)
		return
	}

	loopCount := c.readRAM(recordBase + chOffLoopCount)
	loopCount--
	c.writeRAM(recordBase+chOffLoopCount, loopCount) // $4D3B: DEC (IX+08h)

	if loopCount != active { // $4D3E-$4D44
		loopCount--
		c.writeRAM(recordBase+chOffLoopCount, loopCount) // $4D4B: DEC (IX+08h)
	} else if loopCount < 3 { // $4D46-$4D48: CP 03h; JR C
		return // $4D4A: RET
	}

	duration := c.readRAM(recordBase + chOffDuration)
	duration--
	if int8(duration) < 0 { // $4D51-$4D52: DEC A; RET M
		return
	}
	c.writeRAM(recordBase+chOffDuration, duration) // $4D53
	c.writeVolume(sel, duration)                   // $4D56-$4D57
}

// engineSoundPitchStep is ported from $4C25-$4CB9: the per-frame pitch/
// volume resolution reached instead of advanceSoundChannel when a
// channel's stored sound ID is exactly 1 (see TickSoundChannels) --
// confirmed, once traced, to be genuinely about continuous pitch: it
// writes a real tone period to the channel's tone registers every single
// frame, unlike advanceSoundChannel's bytecode-stream-driven notes.
//
// Every 4th frame ($E003 & 3 == 0), recomputeEngineSoundOffset refreshes
// a live pitch offset at $E036 from $E04F (a live game value -- not
// independently confirmed which one, but its $30/$7E comparisons and
// per-range multiplier selection read like "current speed," scaled
// differently across a few bands the way a real engine's pitch changes
// across gears). The other 3 frames out of 4 reuse whatever $E036 already
// holds. Separately, which of two fixed reference tone periods ($E032 or
// $E034, both written by installSoundRequest -- see its own doc comment
// for why they're periods, not addresses, despite appearances) gets used
// this frame alternates by the SAME frame-mod-4 value: frames 0 and 2 use
// $E032, frames 1 and 3 use $E034. Since only $E036 (not the reference)
// updates every 4th frame, and the reference alternates every OTHER
// frame, the two together interleave into a small, continuous wobble --
// plausibly deliberate (a vibrato/tremor texture on the engine tone), not
// confirmed as intentional beyond the mechanism itself.
//
// The tone period actually written is reference-minus-offset (a plain
// 16-bit subtraction here; the original's SBC HL,DE relies on whatever
// carry state happens to be set from earlier in the frame, which isn't
// tracked by this port -- see docs/ANALYSIS.md).
//
// The volume half ($4C94-$4CB9) is a small, separately-gated selection
// keyed on $E083 (the same byte stateCrashTrigger checks) and $E028 (read
// as a raw byte -- the first field of the sound-priority slot triggerSoundRaw
// assigns to base IDs 2-4, so plausibly "is a specific higher-priority
// sound active/at a specific frame of its countdown"), muting or dropping
// the engine volume during specific moments of crash-adjacent state.
// Mechanically confirmed (exact comparisons and values); the semantic
// "why" is not.
func (c *Console) engineSoundPitchStep(recordBase uint16, sel byte) {
	frameMod4 := c.RAM[0x003] & 0x03 // $4C29-$4C2C

	useE032 := frameMod4 == 2 // $4C2E-$4C34: frame 2 uses $E032 without recomputing
	if frameMod4 == 0 {       // frame 0 recomputes, then also uses $E032
		c.recomputeEngineSoundOffset()
		useE032 = true
	}
	// frameMod4 == 1 or 3: useE032 stays false (use $E034), no recompute.

	var reference uint16
	if useE032 {
		reference = uint16(c.RAM[0x032])<<8 | uint16(c.RAM[0x033]) // $4C89-$4C8C: H=(IY+0), L=(IY+1) -- byte-swapped from storage order
	} else {
		reference = uint16(c.RAM[0x034])<<8 | uint16(c.RAM[0x035])
	}

	offset := uint16(c.RAM[0x036]) | uint16(c.RAM[0x037])<<8 // $4C85: LD DE,(E036h) -- little-endian
	period := reference - offset                             // $4C8F: SBC HL,DE
	c.writeTonePeriod(sel, byte(period>>8), byte(period))    // $4C91: CALL $4DC3

	volume := byte(0x0D) // $4CAA: LD E,0Dh (default)
	switch {
	case c.RAM[0x083] < 0x10: // $4C94-$4C99
		volume = 0x09
	case c.RAM[0x028] == 0x03: // $4C9B-$4CA0
		volume = 0x09
	case c.RAM[0x028] == 0x0A, c.RAM[0x028] == 0x0D: // $4CA2-$4CA8
		volume = 0x00
	}
	c.writeVolume(sel, volume) // $4CB4-$4CB6
}

// recomputeEngineSoundOffset is ported from $4C36-$4C7E: see
// engineSoundPitchStep's doc comment for what $E036 feeds into. Redundant
// re-check at the original's $4C57-$4C5C (re-testing $E038 for zero when
// it's already known nonzero, having not taken the $4C43 branch) is
// omitted here since it can never change the result.
func (c *Console) recomputeEngineSoundOffset() {
	a := uint16(c.RAM[0x04F])
	base := a * 4 // $4C39-$4C3F: HL = A*4

	var result uint16
	switch {
	case c.RAM[0x038] == 0: // $4C40-$4C45
		result = base*2 + 0x0300 // $4C67-$4C6B, $4C7D: HL=A*8; DE=$300; HL+=DE
	case a >= 0x7E: // $4C47-$4C4C
		result = (base + a) + 0x04B0 // $4C61-$4C65, $4C7D: HL=A*4+A; DE=$4B0; HL+=DE
	case a >= 0x30: // $4C4E-$4C50
		result = (a-0x30)*16 + 0x0500 // $4C6D-$4C7D: HL=(A-$30)*16; DE=$500; HL+=DE
	default: // a < 0x30, $E038 != 0
		result = base * 8 // $4C52-$4C55: HL=A*4*8=A*32 -- skips the $4C7D add entirely
	}
	c.RAM[0x036] = byte(result)
	c.RAM[0x037] = byte(result >> 8)
}
