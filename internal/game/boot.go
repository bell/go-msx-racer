// Package game is the in-progress Go port of the Road Fighter (Konami,
// 1985) MSX cartridge. Each function documents the ROM address(es) it was
// ported from; see docs/listing.asm for the full disassembly and
// docs/ANALYSIS.md for the reverse-engineering notes this port is based on.
package game

import "go-msx-racer/internal/msx"

// workRAMSize is the size of the game's scratch RAM area, $E000-$E7FF,
// cleared once at boot (original: $4060-$406B).
const workRAMSize = 0x0800

// WorkRAM models the cartridge's zero-page-style variable area at $E000, so
// ported routines can address it the same way the original code did (e.g.
// "LD A,(E005h)") instead of hand-picking Go field names for bytes whose
// purpose isn't fully confirmed yet. Bytes with a confirmed meaning also get
// a named accessor below.
type WorkRAM [workRAMSize]byte

// Console bundles the emulated hardware surface and the game's RAM image;
// it is the receiver for every ported routine.
type Console struct {
	Sys msx.System
	RAM WorkRAM

	// roadFeatureCursor models $E087/$E088 -- injectRoadFeature's own
	// read cursor into whichever roadFeatureTableStage145 entry is
	// currently active. The original stores this as a real 16-bit ROM
	// address and re-reads through it; this port has no ROM loaded at
	// runtime (pre-decoded Go data only, see assets' own doc comment),
	// so it's modeled as a Go slice over the same embedded data instead
	// -- observably identical (nothing else in the confirmed ROM control
	// flow reads $E087/$E088 directly), just not literal RAM bytes. See
	// injectRoadFeature's own doc comment.
	roadFeatureCursor []byte

	// stage45DataCursor models $E08E/$E08F the same way roadFeatureCursor
	// models $E087/$E088 above: a Go slice over whichever
	// roadStrip7ByteEntries entry is currently active, standing in for a
	// literal ROM read cursor this port has no ROM image to dereference
	// at runtime. See stage45RoadFeatureStep's own doc comment.
	stage45DataCursor []byte

	// stage5E094Cursor models $E094/$E095 the same way, for stage 5's own
	// $5642 handler: a Go slice over whichever road56A4Entries entry is
	// currently active. Unlike stage45DataCursor, this one must be
	// explicitly armed (see armRoadFeatureStream) rather than relying on
	// its target's own reload countdown starting at a value that forces
	// an immediate first-call reload -- $5642's own reload check tests
	// this cursor's own current content for a terminator, not a separate
	// counter, so an incorrectly-empty starting slice would misfire a
	// reload on the very first call that the real ROM's own already-armed
	// $E094 value would not. See stage5RoadFeatureStep's own doc comment.
	stage5E094Cursor []byte
}

// gameStateByte is $E000: the top-level game-mode selector consumed by
// RunGameStateMachine (ported from $4091-$4098 / the dispatch table at
// $4099-$40AA). Values 0-8 select one of nine top-level states; see state.go.
func (c *Console) gameStateByte() *byte { return &c.RAM[0x000] }

// attractPhaseByte is $E001. The dispatch trampoline ($4091: "LD
// BC,(E000h)") loads it into B for *every* top-level state's entry, not
// just state 0's -- confirmed by tracing $4091 itself, not assumed from
// state 0 alone -- so several states reuse this same byte as their own
// local DJNZ dispatch counter, unrelated to "attract phase" semantics.
// State 0 (stateAttractSequence, $40AB-$4166) is where the name comes
// from: there it genuinely does select one of 5 attract sub-phases.
// State 2 (stateGetReady) is a confirmed example of the byte being
// reused for something else entirely -- see its own doc comment.
//
// IMPORTANT for anyone porting a new "DJNZ" dispatch on this byte:
// DJNZ decrements the transient B register the trampoline just loaded,
// NOT this RAM byte -- a state handler only genuinely counts down
// across multiple frames if its own body explicitly writes the
// decremented value back (rare: exhaustively grepping the whole
// disassembly for literal writes to $E001 finds exactly two sites,
// advanceAttractPhase's increment and exitAttractSequence's reset to 0,
// both real state transitions). Two ported functions (statePlaying,
// stateGetReady) previously modeled their own DJNZ as if it persisted
// the decrement anyway -- found and corrected after a user-reported
// symptom (GameplayTick's own callees, like the HUD progress sprite and
// score digits, updating in big rare jumps instead of every frame)
// traced back to exactly this mistake. Model a new consumer the same
// way stateAttractSequence/stateGetReadyWait/stateCrashSequence/
// stateResultsPhase1/2 already do: read the current value and switch on
// it directly, don't decrement-and-store, unless you've confirmed by
// disassembly that this specific function's own body writes it back.
func (c *Console) attractPhaseByte() *byte { return &c.RAM[0x001] }

// attractSubCounter is $E004: a scratch countdown reused by several
// attractPhaseByte sub-phases for different purposes -- setGameState
// writes its starting value when a phase transition sets a new phase in
// motion; decrementAttractSubCounter counts it down.
func (c *Console) attractSubCounter() *byte { return &c.RAM[0x004] }

// decrementAttractSubCounter is ported from $440C-$4412: decrements $E004
// and reports whether it just reached zero. In the original, when it
// hasn't, the routine pops its own caller's return address and returns
// early -- i.e. "abort the rest of this frame's work entirely" -- which
// this port models as an ordinary early `return` at the call site instead
// of replicating the stack trick.
func (c *Console) decrementAttractSubCounter() (reachedZero bool) {
	counter := c.attractSubCounter()
	*counter--
	return *counter == 0
}

// crashFlag is $E045: nonzero once a crash/collision has been registered.
// Read by stateGetReady and statePlaying (both ported); set by
// stateCrashTrigger (ported) and cleared by updateSteeringAndCarState's own
// state 6 (also ported) -- stateCrashSequence (game state 4) remains an
// unported stub, but per triggerRaceStart's own doc comment it's bypassed
// entirely on a normal race start, so that gap doesn't block this flag's
// normal read/write cycle.
func (c *Console) crashFlag() *byte { return &c.RAM[0x045] }

// gameOverFlag is $E00D: nonzero once the game has ended. Read and
// cleared by stateGetReady; also read by statePlaying (ported). Set by
// updateSteeringAndCarState's own state 3 ($6B8A-$6B8C, also ported),
// gated on both object slots being empty -- not otherwise independently
// confirmed as the *only* real write site.
func (c *Console) gameOverFlag() *byte { return &c.RAM[0x00D] }

// levelSelectFlag is $E03B: 0 selects "LEVEL A", nonzero selects
// "LEVEL B" -- read throughout gameplay to choose between the two
// levels' difficulty/curve constants (see e.g. $6F72, $6FCC, $737C in
// docs/listing.asm), and by drawLevelSelectCursor (graphics.go) to decide
// which line the blinking cursor sits next to.
//
// NOT YET CONFIRMED how the original sets this from player input: every
// read site of $E03B in docs/listing.asm is deep in gameplay/difficulty
// code, not an input handler, and no write to it appears anywhere in the
// ~72% of the ROM this project's disassembler has reached so far -- the
// write is presumably in the remaining undecoded region. Defaults to 0
// (LEVEL A) and nothing in this port currently changes it; see
// drawLevelSelectCursor's doc comment.
func (c *Console) levelSelectFlag() *byte { return &c.RAM[0x03B] }

// soundBusyFlag is $E005: a simple reentrancy guard around the heavy
// per-frame update in VBlankHook -- set non-zero while the frame's main
// logic (46B6/4082) is running, so a second interrupt landing mid-update
// skips straight past it instead of re-entering.
func (c *Console) soundBusyFlag() *byte { return &c.RAM[0x005] }

// ColdBoot is the cartridge's one-time initialization routine, invoked by
// the MSX BIOS via the standard ROM header at $4002-$4003 (bytes 'A','B'
// then this address). Ported from $404F-$4037.
//
// Original flow:
//
//	404F  DI
//	4050  IM 1
//	4052  install the per-frame hook: H.KEYI ($FD9A) := "JP $4013"
//	405D  LD SP,$E800                  ; own stack, below work RAM
//	4060  clear $E000-$E7FF to zero    ; LDIR-based memset
//	406D  mark $E005 busy, CALL $4685  ; InitVDPAndSoundEngine
//	4075  clear $E005
//	4079  CALL $013E                   ; RDVDP -- see ReadVDPStatus
//	407C  EI
//	407D  JR $407D                     ; idle forever -- all real work now
//	                                    happens in the H.KEYI hook (VBlankHook)
func (c *Console) ColdBoot() {
	c.Sys.DisableInterrupts()

	// H.KEYI hook install ($4052-$405C): every VDP interrupt from here on,
	// the BIOS calls into VBlankHook via that hook, which is how this
	// cold-boot routine hands off control to the rest of the game --
	// modeled directly in this port rather than via a real RAM hook.

	for i := range c.RAM {
		c.RAM[i] = 0
	}

	*c.soundBusyFlag() = 1
	c.InitVDPAndSoundEngine()
	*c.soundBusyFlag() = 0

	c.ReadVDPStatus()

	c.Sys.EnableInterrupts()
	// Original: JR $407D (spin forever). In this port control simply
	// returns; the caller is expected to drive VBlankHook once per frame.
}

// VBlankHook is the per-frame update entry point, installed as the MSX
// H.KEYI hook by ColdBoot and invoked by the BIOS on every VDP interrupt.
// Ported from $4013-$4037.
func (c *Console) VBlankHook() {
	c.ReadVDPStatus() // $4013: CALL $013E -- see ReadVDPStatus
	c.Sys.DisableInterrupts()

	c.TickSoundChannels() // $4017: CALL $4BFF

	busy := c.soundBusyFlag()
	if *busy == 0 {
		*busy++
		c.Sys.EnableInterrupts()

		c.UpdateInputAndTimers() // $4023: CALL $46B6 -- see input.go
		c.RunGameStateMachine()  // $4026: CALL $4082

		*busy = 0
	}

	status := c.ReadVDPStatus() // $4034: CALL $013E -- see ReadVDPStatus
	if int8(status) < 0 {
		c.TickSoundChannels()
	}
	c.Sys.EnableInterrupts()
}

// ReadVDPStatus is ported from $4079/$4013/$4034: "CALL $013E", the
// standard MSX BIOS RDVDP routine (confirmed against a full BIOS
// reference, correcting an earlier assessment in docs/ANALYSIS.md that
// its target fell outside the standard jump-table range -- it doesn't).
// RDVDP reads the VDP's status register (S#0) into A; reading S#0 also
// clears its bit 7 on real hardware, the "F" frame-interrupt-pending
// flag set at the start of every vertical blank.
//
// This identification fits VBlankHook's own use of the result exactly:
// `int8(status) < 0` tests bit 7, i.e. "did a new frame interrupt arrive
// while this frame's game-logic section (the busy-flag-guarded block)
// was running" -- an overrun/frame-skip detector. When it has, the
// original ticks sound channels an extra time, keeping audio timing
// steady even when a frame's visuals fall behind.
//
// This port always returns 0 (bit 7 clear) -- the correct, complete
// translation here, not a placeholder: VBlankHook is invoked exactly
// once per host-driven frame in this synchronous execution model, not
// via cycle-accurate Z80 interrupt timing, so nothing within a single
// VBlankHook call could ever set a NEW frame-interrupt flag partway
// through -- the condition the second read is checking for structurally
// cannot occur here. The first read in each function (ColdBoot, and the
// top of VBlankHook -- both with their results discarded) is likewise a
// no-op in this model: on real hardware its only effect is acknowledging
// the pending interrupt line to the CPU, which this port's polled (not
// interrupt-driven) VBlankHook already does implicitly just by running.
func (c *Console) ReadVDPStatus() byte {
	return 0
}

// UpdateInputAndTimers is ported from $46B6-$46C2; see input.go.
