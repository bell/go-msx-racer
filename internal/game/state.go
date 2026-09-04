package game

import "math/rand/v2"

// RunGameStateMachine is the top-level game-mode dispatcher, ported from
// $4082-$4098 plus its 9-entry jump table at $4099-$40AA. This dispatch
// mechanism itself is fully confirmed: it's the "computed jump right after
// the CALL" idiom (see internal/z80dis and docs/ANALYSIS.md) -- $4096 calls
// the trampoline at $4045, which pops its own return address (the table),
// indexes it by gameStateByte()*2, and jumps to the resulting address. This
// port replaces that mechanism with an ordinary switch.
//
// All 9 state bodies are ported, as are the functions they call into
// (track/physics update, score display, and the rest) -- an earlier
// version of this comment described those as TODO stubs, which they have
// not been for some time. The names
// given to the 9 states are inferred from that control flow -- e.g. state 5
// repeatedly drives the same per-frame update ($5224) that a "playing"
// state would -- but are NOT confirmed against the original source or
// documentation, so treat them as working hypotheses, not facts.
func (c *Console) RunGameStateMachine() {
	c.RAM[0x003]++ // $4082-4085: INC (HL) on $E003, a free-running frame/mode counter

	// $4086-4090: tests $E002 bit 6 (via "ADD A,A; JP M", which tests the
	// doubled value's sign, i.e. the original bit 6). In the original this
	// is a stack trick, not a branch: when the bit is clear, the code
	// PUSHes the address $43C3 before falling into the dispatch at $4091,
	// so that whichever state handler gets picked will -- if it ends in a
	// plain RET -- return into $43C3 as a continuation, instead of back to
	// this function's own caller. That only holds if the picked handler
	// actually falls through to a RET rather than jumping out some other
	// way, which isn't confirmed for all 9 states, so this is ported as an
	// unconditional "call it afterward" approximation rather than a true
	// stack rewrite.
	runContinuation := int8(c.RAM[0x002]<<1) >= 0

	switch *c.gameStateByte() {
	case 0:
		c.stateAttractSequence() // $40AB
	case 1:
		c.stateAttractWait() // $412D
	case 2:
		c.stateGetReady() // $4137
	case 3:
		c.stateGetReadyWait() // $416F
	case 4:
		c.stateCrashSequence() // $4270
	case 5:
		c.statePlaying() // $4306
	case 6:
		c.stateCrashTrigger() // $4325
	case 7:
		c.stateResultsPhase1() // $4348
	case 8:
		c.stateResultsPhase2() // $4373
	}

	if runContinuation {
		c.stateExtra43C3()
	}
}

// setGameState is a helper for the small "LD A,n; LD (E004h),A; JP $4167"
// idiom seen repeatedly in the state bodies, where $4167 additionally bumps
// $E001 (attractPhaseByte) and returns.
func (c *Console) setGameState(subPhaseCounter byte) {
	c.RAM[0x004] = subPhaseCounter
	*c.attractPhaseByte()++
}

// stateAttractSequence is game state 0, ported from $40AB-$4166.
// attractPhaseByte() ($E001) selects one of 5 sub-phases via a DJNZ chain
// ($40AB, $40C0, $40CD, $40E6, falling through to $411A) -- worked out
// precisely by tracing the decrement/jump semantics rather than guessing:
//
//	== 0 or >= 5:  $411A -- re-programs the 8 Screen 2 VDP mode registers
//	               (writeVDPModeRegisters, $4698) and blanks the whole
//	               name table (clearNameTable, $4584), then triggers sound
//	               id $1B (plays nothing on any channel -- see
//	               InitVDPAndSoundEngine's own doc comment) + font setup +
//	               draw the Konami logo (ShowKonamiLogo), then advance to
//	               phase 1. PORTED. The two leading CALLs were missed on
//	               the first pass here (which started reading at $4120),
//	               which left every re-entry into attract mode -- notably
//	               the one right after game over -- scrolling the logo up
//	               over the previous screen's leftover HUD/road/"GAME
//	               OVER" tiles instead of a blank screen.
//	               (0 reaches here because DJNZ's B-1 underflows to $FF,
//	               which is >0, so it cascades through every jump the same
//	               way any value >=5 does -- confirmed by tracing the
//	               decrement at each of the 4 DJNZ sites, not assumed.)
//	== 1:          $40AD -- runs on odd frames only ($E003 bit0 -- $E003 is
//	               the free-running per-frame counter incremented at the
//	               top of RunGameStateMachine, so this is just "every other
//	               frame"), and fully PORTED: each such frame it's the
//	               Konami logo's scroll animation, one step
//	               (scrollKonamiLogoStep) -- confirmed as genuinely
//	               per-frame because $48DD (what phase 1 CALLs) has no RET
//	               before falling into the redraw+decrement code, so this
//	               isn't a one-shot draw the way an earlier version of this
//	               port assumed. Once the scroll's hold counter reaches
//	               zero, decompresses a stream at $46FC (via the $460B alt
//	               entry) -- this is showSoftwareCredit, the
//	               "KONAMI / -------- / SOFTWARE" caption drawn just below
//	               the settled logo -- and advances to phase 2.
//	== 2:          $40C2 -- counts down $E004 (set to 0 by phase 1, so this
//	               is really a 256-frame/~4.3s hold on the credit screen
//	               before continuing) via decrementAttractSubCounter; once
//	               it reaches zero, clears the name table and decompresses
//	               the "ROAD FIGHTER" big-font title's pattern+color ($4A25,
//	               see docs/ANALYSIS.md) painted in its first color (fixed
//	               palette index 1) -- this is showRoadTitle -- and advances
//	               to phase 3 (E004 reset to $20). PORTED, but its visible
//	               effect isn't yet: nothing references the newly-loaded
//	               tiles from the name table until phase 3 runs.
//	== 3:          $40CF -- decrements $E004 (from $20, so $1F down to 0)
//	               and tests the result against 7. While >=7 (25 frames):
//	               CALL $49C2 then tail-jumps (JP, not CALL) into $5028.
//	               All three pieces are now PORTED: $49C2-$49CA
//	               (drawAttractMenuText) draws "(c)KONAMI 1985" /
//	               "PLAY SELECT" / "LEVEL A" / "LEVEL B" via two calls to
//	               the $45F0 text-drawing interpreter -- missed on the
//	               first pass at this phase (only $49CB onward was ported
//	               initially) until running the game and comparing
//	               against the real attract screen surfaced the gap.
//	               $49CB-$4A24 (revealRoadFighterTitle) IS the
//	               "ROAD FIGHTER" name-table placement script (previously,
//	               incorrectly, thought to be missing/unlocatable when
//	               phase 2 was ported -- it was simply phase 3's job): a
//	               genuine per-frame reveal, each frame writing further
//	               into two ROM tables ($4B0C, 17 bytes, walked backward;
//	               $4B2F, 21 bytes, walked forward; both $FF-terminated)
//	               at positions that also shift by one column per frame.
//	               $5028-$5049 (drawLevelSelectCursor) is the blinking
//	               "finger" cursor next to LEVEL A/B -- also previously
//	               (incorrectly) assumed, without checking, to be an
//	               undifferentiated "large self-play demo driver" not
//	               worth porting; tracing its actual CALL/RET structure
//	               showed it's small and self-contained, RETurning before
//	               ever reaching the much larger RAM-clearing code
//	               physically next to it at $504C (a separate, still-
//	               untraced routine reached from elsewhere, not from
//	               here). All three confirmed correct by register-accurate
//	               simulation and/or rendering through the real
//	               Console/VDP pipeline against the real ROM bytes -- see
//	               docs/ANALYSIS.md's bigfont section for the full trace.
//	               What's still open: what sets $E03B (LEVEL A/B
//	               selection) from player input -- every confirmed read
//	               site is deep in gameplay/difficulty code, and no write
//	               site has turned up in the ~72% of the ROM disassembled
//	               so far, so the cursor currently always blinks at
//	               LEVEL A. Once E004 drops below 7 (last 7 frames): CALL
//	               $778C (confirmed trivial -- TriggerSound(3)) then
//	               advances to phase 4 via setGameState($20).
//	== 4:          $40E8 -- decrements $E004 (from $20, set by phase 3's
//	               transition, so 32 frames total) and tests for zero.
//	               While not zero: repaints the title's color via the same
//	               shared $40EF tail phase 2 uses (paintRoadTitleColor),
//	               but with the live countdown value as the palette
//	               selector instead of a fixed 1 -- a color-cycling
//	               effect. Once it hits zero ($410E-$4305): resets $E004
//	               to 0 and advances the TOP-LEVEL game state
//	               (gameStateByte $E000, 0 -> 1) instead of incrementing
//	               attractPhaseByte -- i.e. phase 4 doesn't hand off to a
//	               "phase 5" of stateAttractSequence at all, it exits the
//	               attract-sequence state entirely into game state 1
//	               (stateAttractWait), resetting attractPhaseByte to 0 for
//	               whenever state 0 is re-entered later. PORTED. Does not
//	               depend on $5028 (that's phase 3's own unported branch,
//	               not phase 4's) so is reachable/exercisable already.
//
// All of stateAttractSequence (phases 0-4, including phase 3's full
// $49C2/$5028 chain) is now ported. The one open loose end is what sets
// $E03B (LEVEL A/B selection) from player input -- see levelSelectFlag's
// doc comment in boot.go.
func (c *Console) stateAttractSequence() {
	phase := *c.attractPhaseByte()
	switch {
	case phase == 0 || phase >= 5:
		c.writeVDPModeRegisters() // $411A: CALL $4698 -- re-program the 8 Screen 2 mode registers
		c.clearNameTable()        // $411D: CALL $4584 -- blank the whole name table
		c.TriggerSound(0x1B)      // $4120-$4122: sound id $1B (plays nothing, see InitVDPAndSoundEngine)
		c.SetupFont()             // $4125: CALL $4773
		c.ShowKonamiLogo()        // $4128: CALL $48BB (performs its own first scroll step)
		c.advanceAttractPhase()

	case phase == 1:
		if c.RAM[0x003]&1 == 0 {
			return // $40AD-40B1: even frame, "RRA; RET NC" -- do nothing this frame
		}
		if !c.scrollKonamiLogoStep() { // $40B2: CALL $48DD
			return // $40B5: RET NZ -- more scroll steps left, wait for the next odd frame
		}
		c.showSoftwareCredit() // $40B6-$40BB: CALL $460B (decompress $46FC)
		c.setGameState(0)      // $40BC-$40BD: XOR A; JP $4167

	case phase == 2:
		if !c.decrementAttractSubCounter() { // $40C2: CALL $440C
			return // $4411-$4412: POP HL; RET -- counter hasn't reached zero yet
		}
		c.showRoadTitle()    // $40C5: CALL $49A6
		c.setGameState(0x20) // $40C8-$40CA: LD A,20h; JP $4167

	case phase == 3:
		counter := c.attractSubCounter()
		*counter--         // $40CF-$40D2: DEC (HL)
		if *counter >= 7 { // $40D3-$40D6: LD A,(HL); CP 07h; JR C,$40DE -- branch not taken here
			c.drawAttractMenuText()            // $49C2-$49CA: "(c)KONAMI 1985" / "PLAY SELECT" / "LEVEL A" / "LEVEL B"
			c.revealRoadFighterTitle(*counter) // $40D8: CALL $49C2 (continued at $49CB)
			c.drawLevelSelectCursor()          // $40DB: JP $5028 -- see its doc comment: $5028 is
			// just the blinking level-select cursor, not the "large
			// self-play demo driver" this was first assumed to be; the
			// much larger RAM-clearing code physically next to it at
			// $504C is a separate, still-unported routine reached from
			// elsewhere, not from here (confirmed by tracing $5028's own
			// CALL/RET structure, not assumed from proximity).
			return
		}
		c.TriggerSound(0x03) // $40DE: CALL $778C (LD A,03h; JP $4B6E)
		c.setGameState(0x20) // $40E1-$40E3: LD A,20h; JP $4167

	case phase == 4:
		counter := c.attractSubCounter()
		*counter--         // $40E8-$40EB: DEC (HL)
		if *counter == 0 { // $40EC: JR Z,$410E
			c.exitAttractSequence(0) // $410E,$42FA-$4305: XOR A; ... (see exitAttractSequence)
			return
		}
		c.paintRoadTitleColor(*counter) // $40EE-$40EF: LD A,(HL); falls into the shared $40EF tail
	}
}

// exitAttractSequence is the shared tail at $42FA-$4305 (reached three
// ways: falling from $42F8's "LD A,$18" for stateAttractWait's and
// statePlaying's exits, or directly via phase 4's own "$410E: XOR A; JP
// $42FA" with A already 0). It advances the TOP-LEVEL game state
// (gameStateByte++) -- always by exactly 1, regardless of which state
// calls it -- resets attractPhaseByte to 0, and sets $E004 to nextE004
// for whatever state comes next to use. Despite the name (which reflects
// its first two call sites, where +1 from states 0/1 happens to land on
// the next attract-sequence state), it isn't specifically an "exit to
// attract mode" -- statePlaying's use of it (state 5 -> 6) demonstrates
// that plainly.
func (c *Console) exitAttractSequence(nextE004 byte) {
	*c.attractSubCounter() = nextE004
	*c.gameStateByte()++
	*c.attractPhaseByte() = 0
}

// advanceAttractPhase is the "JR $416A" tail of the phase-0 branch: it
// jumps into the middle of the setGameState helper, running only its
// "$E001++" half (not the "$E004 = n" half setGameState also does).
func (c *Console) advanceAttractPhase() {
	*c.attractPhaseByte()++
}

// stateAttractWait is game state 1, ported from $412D-$4136: decrements
// $E004 (phase 4's exit leaves it at 0, so this first wraps to $FF -- a
// ~255-frame/4.25s wait) and, while it's still nonzero, tail-jumps into
// $5028 every frame -- the exact same finger-cursor blink phase 3 uses
// (drawLevelSelectCursor). This is why the cursor keeps blinking well
// after "ROAD FIGHTER" has finished revealing and its color-flash (phase
// 4) has ended: this state is what keeps the blink alive through the
// "PLAY SELECT" wait. Not previously wired up -- $5028 itself was ported
// while tracing phase 3, but this state stayed an empty stub, so nothing
// called it again after phase 3 ended.
//
// Once the countdown reaches zero, exits to game state 2 (stateGetReady)
// with $E004 reset to $18, via the same exitAttractSequence tail phase 4
// uses ($42F8's "LD A,$18" falling into the shared $42FA-$4305).
func (c *Console) stateAttractWait() {
	counter := c.attractSubCounter()
	*counter--         // $412D-$4130: DEC (HL)
	if *counter != 0 { // $4131: JP NZ,$5028
		c.drawLevelSelectCursor()
		return
	}
	c.exitAttractSequence(0x18) // $4134: JP $42F8 (LD A,$18; ...)
}

// transitionTo is the shared tail at $414F-$4157 (falling through into
// $4301's "attractPhaseByte=0; RET"): sets the TOP-LEVEL game state to an
// absolute value -- unlike exitAttractSequence, which increments it --
// then resets $E004 to $18 and attractPhaseByte to 0. Reached two ways in
// the original: directly via $414A-$414E's fall-through (game state 0),
// and via $4162's "JP $414F" with A pre-loaded (game state 4).
func (c *Console) transitionTo(gameState byte) {
	*c.gameStateByte() = gameState
	*c.attractSubCounter() = 0x18
	*c.attractPhaseByte() = 0
}

// StartRaceAtStage is NOT part of the port -- no ROM routine takes a stage
// argument. It reproduces stateGetReady's own race-start branch
// ($415A-$4162) exactly, with one substitution: the stage is chosen
// instead of randomized. That branch is how the attract demo starts a
// race, and it is the whole of what the ROM does before handing over to
// stateCrashSequence -- zero the lap counter, pick a stage, run
// initPlayerCarSpritesAndFuel, transitionTo(4).
//
// It exists so cmd/play's level-select cheat can start a later stage the
// way the ROM starts one, rather than driving the fresh-game "get ready"
// sequence and then diverting out of it (which showed the HUD twice, put
// stage 1's static layout on screen in between, and ran a countdown whose
// starting signal then had to be undone piece by piece). See cmd/play's
// own doc comment.
//
// The stage has to be set here rather than by the caller poking $E043 each
// frame: initPlayerCarSpritesAndFuel arms the stage's road-feature stream
// from $E043 ($507E: CALL $50BF), so a stage written after this returns is
// already too late -- the roadside will have been armed from
// randomizeStartingStage's random pick instead, and the stage renders with
// generic off-track filler where its own scenery belongs.
func (c *Console) StartRaceAtStage(stage byte) {
	c.clearHUDPanelColumns()

	// Not part of $415A's branch: reset the per-race RAM, including the
	// score at $E03F-$E041. The ROM's own users of this path never need
	// it -- the attract demo does not score, and a genuine stage advance
	// deliberately keeps the score you have earned -- but the cheat is
	// starting a *new game*, which on the real fresh-game path is exactly
	// what stateGetReadyWait's phase 2 does here ($418E: CALL $4413).
	// Without it a cheat-started race inherited the previous race's
	// score. This runs before the stage is chosen because it also sets
	// $E043 back to 1, which the next line then overrides -- the same
	// order phase 2 and phase 3 run in.
	c.clearRaceRAMBlock() // $4413, via stateGetReadyWait phase 2

	c.RAM[0x042] = 0                // $52B8-$52BC: randomizeStartingStage zeroes the lap counter
	c.RAM[0x043] = stage            // in place of its random 1-3 pick
	c.initPlayerCarSpritesAndFuel() // $415D: CALL $504C
	c.transitionTo(4)               // $4160-$4162: LD A,04h; JP $414F
}

// clearHUDPanelColumns blanks name-table columns 23-31, the HUD panel to
// the right of the 22-column road strip. NOT part of the port; it exists
// only to make StartRaceAtStage's handover safe from the attract screen.
//
// The arrival state 4 hands off to wipes the screen through
// countdownGetReadyMessage, but only 23 columns wide (columns 0-22)
// whenever gameStateByte() != 3 and $E002 bit 6 is set -- deliberately, so
// a stage change mid-game doesn't wipe the live HUD. The ROM's own users
// of this path always satisfy that: a genuine stage advance has a real HUD
// there, and the attract demo reaches state 4 with bit 6 *clear*, which
// selects the full 31-column wipe instead.
//
// This cheat is the one caller that has neither. It comes from the attract
// screen, so those columns hold leftovers from the "ROAD FIGHTER" title
// (character codes $C0-$E0), and it must set bit 6 before the handover --
// state 4's own phase 5 reads it to decide "real race" versus "loop back
// to the demo", and while it is clear stateExtra43C3 runs every frame and
// sends any keypress straight back to the menu. So the narrow wipe is
// unavoidable here, and three title tiles at column 24 survived it into
// the race. Blanking the panel up front is what the wide wipe would have
// done; phase 0 redraws the real HUD over it a moment later.
func (c *Console) clearHUDPanelColumns() {
	for row := uint16(0); row < 24; row++ {
		c.Sys.FillVRAM(0x3800+row*32+23, 0, 9)
	}
}

// stateGetReady is game state 2, ported from $4137-$4166. Like every
// state, it's entered with $E001 already loaded into the DJNZ counter by
// the dispatch trampoline (see attractPhaseByte's doc comment); this
// state reuses that same byte for its own purpose, unrelated to attract
// sub-phases.
//
// CORRECTED: an earlier pass at this port modeled the DJNZ at $4137 as a
// persistent countdown -- decrementing $E001 in RAM every time this
// state runs, the same mistake statePlaying's own doc comment now
// explains in full. $4137's DJNZ operates on B, a transient copy of
// $E001 the dispatch trampoline loads fresh each frame ($4091: LD
// BC,(E000h)); nothing in this function's own body ($4137-$4166) ever
// writes the decremented value back to $E001 -- confirmed by an
// exhaustive search of the entire disassembly for literal writes to
// that address, which turns up exactly two sites in the whole ROM
// (advanceAttractPhase's own increment at $416A, and
// exitAttractSequence's own reset to 0 at $4302), neither of which is
// reached from within this function. So this is not a countdown at
// all: it's a plain check of $E001's *current* value, re-evaluated
// fresh every frame, changing only via those two functions' own
// deliberate writes (both already called correctly below, via
// transitionTo).
//
// $E001 arrives as 0 via the normal path (stateAttractWait's exit resets
// it), which underflows the (transient) DJNZ decrement to $FF (nonzero)
// -- the same trick phase 0 of stateAttractSequence uses -- taking the
// branch at $415A: CALL $52B8 (randomizeStartingStage, now ported -- see
// its own doc comment), then CALL $504C (initPlayerCarSpritesAndFuel,
// already ported -- see gameplay.go), then transitions straight to game
// state 4 -- in the SAME frame, no waiting.
//
// The other DJNZ branch (falls through normally, requiring $E001==1 on
// entry) is small and fully traced: CALL $52D1 (autoSteerTowardCenter,
// now ported -- see its own doc comment) and $5224 (GameplayTick, now
// ported -- see gameplay.go), then: if the crash flag ($E045) is set
// AND the game-over flag ($E00D) is clear, do nothing more this frame
// (idle, presumably waiting for something else to resolve the crash);
// otherwise clear the game-over flag and transition to game state 0
// (attract mode). What sets $E001=1 to reach this branch is now
// confirmed too: stateCrashSequence's own phase 5 ($42CE-$42D1, already
// ported) writes $E000=2/$E001=1 directly -- so this branch is the
// "resume driving after the per-lap crash-sequence cycle" path, not a
// player-input path as an earlier pass here guessed.
func (c *Console) stateGetReady() {
	if *c.attractPhaseByte() != 1 { // $4137: DJNZ $415A -- branch taken whenever $E001 != 1 (see doc comment: transient, not decremented in RAM)
		c.randomizeStartingStage()      // $415A: CALL $52B8 -- see its own doc comment
		c.initPlayerCarSpritesAndFuel() // $415D: CALL $504C
		c.transitionTo(4)               // $4160-$4162: LD A,04h; JP $414F
		return
	}

	c.autoSteerTowardCenter() // $4139: CALL $52D1 -- see its own doc comment
	c.GameplayTick()          // $413C: CALL $5224 -- see gameplay.go

	if *c.crashFlag() != 0 && *c.gameOverFlag() == 0 { // $413F-$4149
		return // crashed but not game over yet -- idle this frame
	}
	*c.gameOverFlag() = 0 // $414A-$414B: XOR A; LD (E00Dh),A
	c.transitionTo(0)     // $414E onward -- back to attract mode
}

// randomizeStartingStage is ported from $52B8-$52D0: zeroes $E042 (the
// lap counter, confirmed by stateResultsPhase2's own use of it) and picks
// a starting stage in {1, 2, 3} for $E043 -- confirmed by the exact
// bit-masking: a masked 2-bit value 0-3 is incremented to 1-4, then 4 is
// remapped back down to 1, giving a non-uniform distribution (stage 1
// twice as likely as 2 or 3, from the two masked inputs 0 and 3 that
// both resolve to it) rather than a uniform pick among all 3.
//
// The original derives its entropy from the Z80 R register (`LD A,R`)
// combined with $E003 (the free-running frame counter) -- this port has
// no per-instruction Z80 core to source an equivalent R value from (see
// CLAUDE.md: this is a native port, not a cycle-accurate emulator), so
// there's no way to reproduce the exact sequence real hardware would
// produce. Substituted with Go's own math/rand/v2, preserving the
// confirmed *outcome distribution* above (which is the only part of this
// that's actually gameplay-relevant -- which stage a race starts on)
// rather than attempting to fake a specific fake entropy source. This is
// a deliberate, acknowledged deviation, not a bug: everything else this
// project ports is verified byte-for-byte against the real ROM, but a
// hardware timing register has no such target to match.
func (c *Console) randomizeStartingStage() {
	c.RAM[0x042] = 0 // $52B8-$52BC
	stage := byte(rand.IntN(4)) + 1
	if stage == 4 {
		stage = 1
	}
	c.RAM[0x043] = stage // $52CE-$52CF
}

// autoSteerTowardCenter is ported from $52D1-$530E, reached from
// stateGetReady's own $E001==1 branch -- i.e. every frame while resuming
// driving after a per-lap crash-sequence cycle (see stateGetReady's own
// doc comment). Synthesizes input bits into $E009 (the raw combined
// input state readCombinedInput normally populates from real player
// input) so the car auto-recenters and keeps accelerating during this
// hand-off, rather than sitting still or drifting off-track before the
// player regains control.
//
// First, unconditionally overrides $E008 (the *edge-triggered* "just
// pressed" byte) to $0C (bits 2 and 3, left and right steering --
// confirmed bit assignments, see updateNormalDriving's own doc comment)
// when the car-behavior state is exactly 4 (updateSteeringAndCarState's
// own "lane change in progress" state) and its own sub-counter ($E0B1)
// hasn't yet counted down past $15 -- a synthetic "just pressed both
// steering directions" edge, plausibly there to force
// updateCarPositionFromSteering's own edge-gated logic to re-evaluate
// immediately rather than wait for a real key transition. $E0AA's own
// role isn't independently confirmed beyond this one read site.
//
// Then computes a target column band from $E0AA (masked to a multiple of
// 4, doubled, offset by $1A) and compares the player's own column
// ($E04E) against it: within a ±7-ish window of center, only forces the
// accelerate bit ($E009 bit 4); outside it, ALSO forces the steering bit
// for whichever direction points back toward center (left if the player
// is right of the band, right if left of it) -- confirmed via the exact
// unsigned-comparison branches, not guessed from the shape alone. Unlike
// $E008 above, this ORs new bits into $E009's own existing value rather
// than replacing it outright.
func (c *Console) autoSteerTowardCenter() {
	if c.RAM[0x049] == 4 && c.RAM[0x0B1] < 0x15 { // $52D1-$52DD
		c.RAM[0x008] = 0x0C // $52DF-$52E1
	}

	center := (c.RAM[0x0AA]&0xFC)<<1 + 0x1A // $52E4-$52EC
	col := c.RAM[0x04E]
	offset := col - center + 1 // $52ED-$52F1
	if offset < 7 {            // $52F3-$52F5: unsigned CP 07h; JR C
		c.RAM[0x009] |= 0x10 // $530C-$5305: accelerate only, no steering correction
		return
	}
	steerBit := byte(0x04) // $52FF: LD B,04h -- left (the col>=center fall-through case)
	if col < center {      // $52FA/$52FD: CP C; JR C -- col<center keeps B=8 (right), steering back toward center
		steerBit = 0x08 // right
	}
	c.RAM[0x009] |= steerBit // $5301-$5304
	c.RAM[0x009] |= 0x10     // $5305-$5308: accelerate, always
}

// stateGetReadyWait is game state 3, ported from $416F-$426F. Reached
// from stateExtra43C3 once the player commits a level selection at the
// "PLAY SELECT" menu.
//
// Structurally this is a 9-way switch on attractPhaseByte() ($E001),
// encoded in the original as a cascading chain of DJNZ instructions all
// sharing one CPU register (never written back to $E001 mid-chain), which
// is why -- despite looking like it repeatedly decrements across frames
// the way stateGetReady's/statePlaying's single DJNZ does -- it's really
// just an inefficient switch over whatever $E001 already was on entry;
// only the individual branch bodies below (via setGameState/
// advanceAttractPhase) ever write $E001 for the *next* frame. Values 1-9
// each have their own confirmed behavior; 0 and everything above 9 fall
// through the whole chain to the same fallback ($4266), mirroring how
// stateAttractSequence's phase 0 falls through to its own ">=5" case.
// levelAGetReadyText and levelBGetReadyText are the ROM byte windows at
// $4747 and $4751: single drawTextRun-format runs spelling "LEVEL A" (at
// name-table address $3A0D) and "LEVEL B" (at $3A4D) respectively -- the
// same two positions the attract-mode menu's own "LEVEL A"/"LEVEL B"
// text occupies (see attractMenuText's own doc comment), redrawn here
// with a blink (see stateGetReadyWait's own phase 1) during the
// "get ready" countdown.
var levelAGetReadyText = [10]byte{0x0D, 0x3A, 0x2C, 0x25, 0x36, 0x25, 0x2C, 0x00, 0x21, 0xFF}
var levelBGetReadyText = [10]byte{0x4D, 0x3A, 0x2C, 0x25, 0x36, 0x25, 0x2C, 0x00, 0x22, 0xFF}

func (c *Console) stateGetReadyWait() {
	switch phase := *c.attractPhaseByte(); {
	case phase == 1:
		counter := c.attractSubCounter()
		*counter--         // $4174: DEC (HL) on $E004
		if *counter == 0 { // $4175: JR Z,$416A
			c.advanceAttractPhase()
			return
		}
		text := levelAGetReadyText[:] // $4177-$4181: BIT 5,(E002h); JR Z -- level A vs B selection, mirroring stateExtra43C3's own commit
		if c.RAM[0x002]&0x20 != 0 {
			text = levelBGetReadyText[:]
		}
		mask := byte(0xFF) // $4184: BIT 2,(HL) -- (HL) is $E004, the just-decremented counter
		if *counter&0x04 != 0 {
			mask = 0x00 // $4607: LD C,00h -- blank the text instead of drawing it, creating a blink every 4 frames
		}
		c.drawTextRun(text, 0, mask) // $4186/$4189: JP $45F0 / JP $4607

	case phase == 2:
		c.clearRaceRAMBlock() // $418E: CALL $4413
		c.setGameState(0x18)  // $4191-$4193

	case phase == 3:
		if !c.countdownGetReadyMessage() { // $4197: CALL $4429; $419A: RET P
			return
		}
		c.initHUDAndTrackGraphics()     // $419B: CALL $68AA -- see graphics.go
		c.initPlayerCarSpritesAndFuel() // $419E: CALL $504C (partial -- see its own doc comment)
		// CORRECTED: an earlier pass at this port called drawFuelGaugeBar
		// here as an explicitly-labeled stopgap, since updateLaneFlagAndFuel's
		// own drain-triggered redraw looked permanently dead (no write site
		// for $E057 anywhere in this port's own disassembled ROM coverage).
		// That's no longer true -- $E057 is written by advanceScrollSpeedCounter
		// ($7374, now ported, see gameplay.go), so the gauge draws itself for
		// real within about a second of real gameplay, matching the real ROM
		// exactly: it has no third call site for $7444 either, so the fuel
		// gauge genuinely isn't drawn until the first real drain event on
		// real hardware too. Stopgap call removed.
		c.drawScoreDisplay()    // $41A1: CALL $44D0 -- see graphics.go
		c.drawStageNumber()     // $41A4: CALL $42ED -- see graphics.go
		c.RAM[0x004] = 0x78     // $41A7-$41A9
		c.advanceAttractPhase() // $41AC: JP $416A

	case phase == 4:
		if !c.decrementAttractSubCounter() { // $41B1: CALL $440C
			return
		}
		c.setGameState(4) // $41B4-$41B6

	case phase == 5:
		c.setGameState(0x1A) // $41BB-$41BD

	case phase == 6:
		c.initTrackLayoutData() // $41C2: CALL $77C8 -- see gameplay.go
		c.drawRoadStrip()       // $41C5: CALL $573A -- see gameplay.go
		c.advanceAttractPhase() // $41C8: JP $416A

	case phase == 7:
		c.initTrafficSpriteSlots() // $41CD: CALL $5197 -- see gameplay.go
		c.setGameState(5)          // $41D0-$41D2

	case phase == 8:
		if !c.decrementAttractSubCounter() { // $41D7: CALL $440C
			return
		}
		c.RAM[0x003] = 0x10 // $41DA-$41DB: DEC HL; LD (HL),10h -- overwrites $E003, the free-running frame counter
		c.setGameState(9)   // $41DD-$41DF

	case phase == 9:
		c.blitSpriteAttributeTable(0x78) // $41E6: CALL $5270 -- see gameplay.go

		// $41E9-$41F4: picks the player car's own row ($E04C) -- 0xC8 on
		// even ~16-frame halves of $E003 bit 4, else 0x80 -- then arms its
		// sprite pair via armPlayerCarSprites ($41F7: CALL $6E1D, now
		// ported -- see gameplay.go). Originally guessed as an "engine
		// sound parameter" before $6E1D was traced and found to be the
		// sprite-arming routine, not a sound one; alternating $E04C
		// between two rows this way is a small idle bounce/rock effect on
		// the starting grid, not a sound parameter.
		row := byte(0xC8)
		if c.RAM[0x003]&0x10 != 0 {
			row = 0x80
		}
		c.RAM[0x04C] = row
		c.armPlayerCarSprites()

		if c.RAM[0x003]&0x1F != 0 {
			return // $41FE-$4200: AND $1Fh; RET NZ -- only every 32nd frame
		}

		counter := c.attractSubCounter() // $E004, now the "3,2,1" countdown (seeded at 9 by phase 8's setGameState(9), not 5 -- an earlier comment here had this wrong; see drawCountdownDigit's own doc comment for how that was caught)
		*counter--                       // $4202: DEC (HL)
		if *counter == 1 {               // $4204-$4206: CP 01h; JR Z,$424A
			c.triggerRaceStart()
			return
		}

		// $4208-$4214: on even counts (4 or 2) plays a countdown beep --
		// id 7 for count 2, id 5 for count 4; odd counts (3) are silent.
		if *counter&1 == 0 {
			if *counter == 2 {
				c.TriggerSound(7)
			} else {
				c.TriggerSound(5)
			}
		}

		c.drawCountdownDigit() // $4217-$423E -- see graphics.go

	default: // phase == 0 or phase >= 10
		c.TriggerSound(0x98) // $4266-$4268
		c.setGameState(0x50) // $426B-$426D
	}
}

// clearRaceRAMBlock is ported from $4413-$4428: zero-fills $E03F-$E14E
// (a self-referencing LDIR clear -- writes 0 to $E03F, then LDIR-copies
// that zero forward through the next 271 bytes) and then sets $E042=0,
// $E043=1. What this 272-byte RAM range holds isn't confirmed -- its size
// and position suggest per-race/per-lane state (positions, distances,
// whatever's tracked per opponent car), but that's a guess; ported
// mechanically for its confirmed *effect*, not its unconfirmed *purpose*.
func (c *Console) clearRaceRAMBlock() {
	for i := 0x03F; i <= 0x14E; i++ {
		c.RAM[i] = 0
	}
	c.RAM[0x042] = 0
	c.RAM[0x043] = 1
}

// countdownGetReadyMessage is ported from $4429-$447E: the "get ready"
// wait counter (E004) gate, plus -- now traced past what an earlier pass
// left as a large unported block -- the line-by-line screen wipe it drives
// one row per frame while the wait is still in progress. This is the
// effect stateGetReadyWait's own phase-3 doc comment didn't yet account
// for: while `!countdownGetReadyMessage()` keeps returning early each
// frame, the name table gets cleared one row at a time, top to bottom,
// finishing right as phase 3 hands off to initHUDAndTrackGraphics/
// drawScoreDisplay/drawStageNumber (the HUD and "STAGE 01" reveal) --
// confirmed against real frame counts: with E004 starting at 24 (set by
// phase 2's setGameState(0x18)), this gate returns false for exactly 24
// frames (rows 0-23) before finally returning true on the 25th.
//
// $442E-$443A computes HL = $3800 + 32*(23-counter) -- the name-table
// address of row (23-counter): row 0 on the first call (counter==23,
// post-decrement) counting up to row 23 on the 24th (counter==0). The
// original computes this via a non-carrying 8-bit add into L followed by
// five doublings of the full 16-bit HL; that only differs from the
// closed-form $3800+32*row used below if the 8-bit add overflows, which it
// can't here since counter never exceeds 23. Same technique
// pixelToNameTableAddr's own doc comment uses to justify its closed form.
//
// $443B-$444D picks the fill width for that row: 31 bytes normally, or 23
// only when gameStateByte() != 3 AND E002 bit 6 is set -- the
// stateResultsPhase2 case (itself unported, so not exercised by any
// confirmed caller of this function yet; stateGetReadyWait always takes
// the 31-byte branch). $4452-$445D always stamps $D0 at $3B04, and
// additionally at $3B00 when gameStateByte()==3. An earlier pass here
// called those "just past the 768-byte name table" and left their purpose
// unconfirmed -- wrong on both counts: $3B00 is this ROM's sprite
// attribute table base (R5=$76 -> $76*$80 = $3B00, see
// msx.VDP.SpriteAttrTableBase), and $D0 written to an entry's Y byte is
// the TMS9918A's sprite-list terminator, so these hide every sprite from
// that entry onward. $3B04 hides sprites 1-31 while leaving sprite 0 (the
// player car / countdown sprite) visible; the gameStateByte()==3 case
// additionally hides sprite 0 too. $4460-$447E stamps a
// sentinel byte ($C8) into a 28-entry RAM table at $E112 (4 bytes apart)
// every single frame this runs -- purpose not confirmed either.
func (c *Console) countdownGetReadyMessage() bool {
	counter := c.attractSubCounter()
	*counter--
	if int8(*counter) < 0 { // $442D: RET M
		return true
	}

	row := byte(0x17) - *counter // $442E-$443A
	rowAddr := uint16(0x3800) + uint16(row)*32

	count := uint16(0x1F) // $443B-$444D
	if *c.gameStateByte() != 3 && c.RAM[0x002]&0x40 != 0 {
		count = 0x17
	}
	c.Sys.FillVRAM(rowAddr, 0, count) // $444E-$4451: XOR A; CALL $0056

	c.Sys.FillVRAM(0x3B04, 0xD0, 1) // $4452-$4457: LD HL,3B04h; CALL $446A -- hide sprites 1-31
	if *c.gameStateByte() == 3 {    // $4458-$445D
		c.Sys.FillVRAM(0x3B00, 0xD0, 1) // CALL $4467 -> $446A -- hide sprite 0 as well
	}

	for i := 0; i < 28; i++ { // $4460-$447E
		c.RAM[0x112+i*4] = 0xC8
	}

	return false
}

// triggerRaceStart is ported from $424A-$4263: the countdown's final
// step, reached once the "3,2,1" counter (driven from $E004 by
// stateGetReadyWait's phase 9) hits 1. Writes two still-unidentified
// bytes ($E1E3/$E1E4 = $48/$49), restashes the same sound-channel
// parameter ($E04C=$80) phase 9's per-frame engine-sound update uses,
// then advances the TOP-LEVEL game state twice in a row -- once via a
// direct increment on $E000, then again via exitAttractSequence's shared
// tail -- landing on game state 5 (statePlaying) rather than state 4
// (stateCrashSequence), skipping it entirely.
//
// $E045 is also set to 1 here, which is surprising: every other confirmed
// write site for that byte is stateCrashSequence/stateCrashTrigger
// setting it on an actual crash (see crashFlag's doc comment in
// boot.go). Ported faithfully rather than "corrected," but worth
// revisiting once those two states are themselves traced -- either this
// call site's effect on $E045 isn't really "a crash", or that byte means
// something other than what crashFlag's name currently implies.
func (c *Console) triggerRaceStart() {
	c.RAM[0x1E3] = 0x48         // $424D
	c.RAM[0x1E4] = 0x49         // $4250
	c.RAM[0x04C] = 0x80         // $4252-$4254
	c.armPlayerCarSprites()     // $4257: CALL $6E1D -- re-syncs the player sprite pair to the row just reset above; same stale-TODO pattern already found and fixed at several other $6E1D call sites this session
	*c.gameStateByte()++        // $425D: INC (HL) on $E000 -- 3 -> 4
	*c.crashFlag() = 1          // $4261 -- see doc comment above
	c.exitAttractSequence(0x18) // $4263: JP $42F8 -- $E004=$18; $E000++ (4 -> 5); $E001=0
}

// stateCrashSequence is game state 4, ported from $4270-$4305: a 5-phase
// DJNZ-chain dispatch, the same idiom as stateGetReadyWait's.
// triggerRaceStart deliberately skips straight from state 3 to state 5,
// bypassing this state entirely on a normal race start -- but
// stateResultsPhase2's own phase 4 (fully ported, not a TODO) transitions
// here directly on every completed lap that isn't also a full wrap, so
// this state is genuinely reached in real play, reusing "crash sequence"
// as a per-lap transition rather than literally meaning "you crashed"
// (see stateResultsPhase2's own doc comment).
func (c *Console) stateCrashSequence() {
	switch phase := *c.attractPhaseByte(); {
	case phase == 1:
		if !c.decrementAttractSubCounter() { // $4272: CALL $440C
			return
		}
		*c.crashFlag() = 1   // $4275-$4278
		c.setGameState(0x1A) // $427A-$427C

	case phase == 2:
		counter := c.attractSubCounter()
		*counter--         // $4284: DEC (HL)
		if *counter == 0 { // $4285-$4287: LD A,30h; JP Z,$4167
			c.setGameState(0x30)
			return
		}
		c.runRoadFeatureDispatch() // $428A-$428E: CALL $551D -- see its own doc comment; the doubled return-address push is a no-op in terms of observable control flow

	case phase == 3:
		c.armPlayerCarSprites()          // $4294: CALL $6E1D -- already-ported, same stale-TODO pattern the "Real-hardware screenshot comparison" write-up already found and fixed at phase 9's own two call sites
		c.blitSpriteAttributeTable(0x34) // $4297: CALL $526B
		c.setGameState(0x28)             // $429A-$429C

	case phase == 4:
		if !c.decrementAttractSubCounter() { // $42A1: CALL $440C
			return
		}
		c.Sys.WriteVRAM(0x394A, startText[:]) // $42A4-$42AD: CALL $45A7 -- a manual SETWRT+OUT loop, functionally identical to LDIRVM; DE=$7895 (ROM), HL=$394A (name table), BC=5
		if c.RAM[0x002]&0x40 != 0 {           // $42B0-$42B5: BIT 6,(E002h)
			c.TriggerSound(7) // $42B7-$42B9
		}
		c.setGameState(0x48) // $42BC-$42BE

	case phase == 5:
		if !c.decrementAttractSubCounter() { // $42C3: CALL $440C
			return
		}
		if c.RAM[0x002]&0x40 != 0 { // $42C6-$42CB: BIT 6,(E002h)
			c.exitAttractSequence(0x18) // $42CB: JP $42F8 -- back to statePlaying (state 5)
			return
		}
		// $42CE-$42D1: LD HL,$0102; LD (E000h),HL -- a direct 16-bit
		// store writing $E000=2 (stateGetReady) and $E001=1, rather than
		// going through transitionTo/setGameState.
		*c.gameStateByte() = 2
		*c.attractPhaseByte() = 1

	default: // phase == 0 or phase >= 6
		if !c.countdownGetReadyMessage() { // $42D5: CALL $4429; $42D8: RET P
			return
		}
		c.initHUDAndTrackGraphics()      // $42D9: CALL $68AA -- see graphics.go
		c.drawScoreDisplay()             // $42DC: CALL $44D0 -- see graphics.go
		c.drawStageNumber()              // $42DF: CALL $42ED -- see graphics.go
		c.blitSpriteAttributeTable(0x34) // $42E2: CALL $526B
		c.RAM[0x004] = 0x78              // $42E5-$42E7
		c.advanceAttractPhase()          // $42EA: JP $416A
	}
}

// statePlaying is game state 5, ported from $4306-$4324. Like every
// state, entered with $E001 loaded into the DJNZ counter (see
// attractPhaseByte's doc comment); this state reuses it yet again for its
// own purpose, distinct from stateGetReady's.
//
// CORRECTED: an earlier pass at this port modeled $4306's DJNZ as a
// persistent 256-frame countdown -- decrementing $E001 in RAM every
// single frame this state runs, and only calling GameplayTick once
// every ~256 frames when the count finally reached zero. That's wrong.
// DJNZ decrements B, a transient copy of $E001 the dispatch trampoline
// loads fresh every frame ($4091: LD BC,(E000h)) -- and nothing in this
// function's own body ($4306-$4324) ever writes that decremented value
// back to $E001. An exhaustive search of the entire disassembly for
// literal writes to $E001 turns up exactly two sites in the whole ROM
// (advanceAttractPhase's own increment at $416A, and
// exitAttractSequence's own reset to 0 at $4302, both real state
// transitions, not per-frame bookkeeping) -- neither reachable from
// within this function. So there is no countdown here at all: this is
// a plain, stable check of $E001's *current* value, re-evaluated fresh
// every single frame, which only changes via those two functions'
// deliberate writes elsewhere (see exitAttractSequence's own call
// below, and stateCrashTrigger's direct $E000/$E001 store).
//
// Found while investigating a user report that both the HUD progress
// sprite and the score display were updating in big, rare jumps rather
// than small, frequent ones -- both are among GameplayTick's own
// callees (scrollTrafficSprite, redrawScoreAndHiscoreDigits), so a
// GameplayTick that only fires once every ~256 frames (the old, wrong
// model) instead of every frame while $E001==1 (the real behavior)
// explains exactly that symptom.
//
// With $E001==1 (DJNZ falls through): calls GameplayTick ($5224, the
// main per-frame gameplay dispatcher -- see gameplay.go), then checks
// the game-over flag ($E00D) and crash flag ($E045). Game over
// transitions to game state 8 (stateResultsPhase1) via transitionTo,
// but with a confirmed quirk: the original immediately overwrites
// $E004 back to 0 right after transitionTo's own tail would have left
// it at $18 -- this port replicates that exact net effect rather than
// reusing transitionTo's normal result. A crash with no game-over just
// idles (RET NZ). Neither: advances to game state 6 (stateCrashTrigger)
// via exitAttractSequence($18) -- that helper always *increments*
// gameStateByte (it's not an "exit to attract mode" despite the name,
// which comes from its other two call sites, phase 4 and
// stateAttractWait, where +1 from states 0/1 happens to land on the next
// attract-sequence state); from state 5 that's simply "the next state."
// Since nothing here changes $E001 away from 1, this path repeats every
// frame for as long as $E001 stays 1 -- e.g. right after
// stateCrashTrigger's own direct store sets it, GameplayTick runs on
// every frame until something else (a state transition) changes $E001
// again, not just once.
//
// With $E001!=1: tail-jumps to $527D/advanceCarBehavior (see
// gameplay.go) -- itself another gameplay dispatcher (CALL
// $5270/$6A70/$6CA1/$738C/$5501, then $528F's per-lane position-
// adjustment loop), now ported control-flow-only, same standard as
// GameplayTick. This is what runs on ordinary driving frames (whenever
// $E001 currently holds anything other than 1), which is why wiring it
// up is what makes the speed/fuel gauges (drawn by one of its own
// callees, $738C) update every frame.
func (c *Console) statePlaying() {
	if *c.attractPhaseByte() != 1 { // $4306: DJNZ $4322 -- branch taken whenever $E001 != 1 (see doc comment: transient, not decremented in RAM)
		c.advanceCarBehavior() // $4322: JP $527D -- see gameplay.go
		return
	}

	c.GameplayTick() // $4308: CALL $5224

	if *c.gameOverFlag() != 0 { // $430B-$430F
		c.transitionTo(8)          // $4319-$431B: LD A,08h; CALL $414F
		*c.attractSubCounter() = 0 // $431E: LD (E004h),A -- overwrites transitionTo's $18 with the leftover A=0 from its own tail
		return
	}
	if *c.crashFlag() != 0 { // $4312-$4316
		return // $4316: RET NZ -- crashed, idle
	}
	c.exitAttractSequence(0x18) // $4317: JR $42F8
}

// stateCrashTrigger is game state 6, ported from $4325-$4347: checks
// $E083 -- fuel (confirmed: drainFuelAndResetPickup, fuelPickupTick, and
// updateLaneFlagAndFuel all read/write it as the same fuel level
// drawFuelGaugeBar renders). If empty, the race is over: plays a sound
// and advances to game state 7 (stateResultsPhase1) via
// exitAttractSequence. If fuel remains: runs the post-crash reset
// (recoverFromNonFatalCrash, $51C5 -- previously an untraced TODO here,
// see its own doc comment for what this was blocking), sets the crash
// flag ($E045=1), and jumps directly into game state 5 (statePlaying,
// with $E001=1) rather than through stateCrashSequence at all.
func (c *Console) stateCrashTrigger() {
	if c.RAM[0x083] == 0 { // $4325-$4329: LD A,(E083h); OR A; JR Z,$4341
		c.TriggerSound(0x95)        // $4341-$4343
		c.exitAttractSequence(0x18) // $4346: JR $42F8
		return
	}

	c.recoverFromNonFatalCrash() // $432B: CALL $51C5 -- see gameplay.go
	*c.crashFlag() = 1           // $432E-$4330

	c.drawRoadStrip() // $4337: CALL $573A -- see gameplay.go

	// $433A-$433D: LD HL,$0105; LD (E000h),HL -- a direct 16-bit store
	// writing $E000=5 (statePlaying) and $E001=1.
	*c.gameStateByte() = 5
	*c.attractPhaseByte() = 1
}

// stateResultsPhase1 is game state 7, ported from $4348-$4372. Every
// frame, regardless of phase: refreshes the sprite-attribute-table VRAM
// copy (blitSpriteAttributeTable) then overrides its own entry 0's color
// byte to $06 via a direct $004D (WRTVRM, the standard MSX BIOS
// write-one-byte-to-VRAM call -- confirmed against a full BIOS reference,
// the same vector ReadVDPStatus's own doc comment already identifies
// $013E as; not the "unconfirmed routine" an earlier pass here assumed,
// though stateGetReadyWait's own phase 9 use of the same BIOS call for
// countdown digits remains separately untraced). Then a 1-phase DJNZ
// dispatch: while $E001==1, waits out
// decrementAttractSubCounter, then clears $E002 bit 6 (ending the
// "game in progress" state that gates stateExtra43C3) and returns to
// attract mode (transitionTo(0)). Otherwise (phase 0 or >=2), gates on
// the shared countdownGetReadyMessage ($4429) wait, then draws
// unconfirmed text and advances to game state 0xD8 -- clearly a stray/
// out-of-range value for the 0-8 top-level dispatch (ported faithfully;
// not corrected, since the original genuinely writes it), which would
// make RunGameStateMachine's switch fall through and do nothing further
// until something else resets $E000.
func (c *Console) stateResultsPhase1() {
	c.blitSpriteAttributeTable(0x34)      // $4349: CALL $526B
	c.Sys.WriteVRAM(0x3B03, []byte{0x06}) // $434C-$4351: LD A,06h; LD HL,3B03h; CALL $004D (WRTVRM) -- overrides sprite-attribute-table entry 0's own color byte ($3B00+3) to $06 (dark red, matching carInitTable10E's own entry-0 color)

	switch phase := *c.attractPhaseByte(); {
	case phase == 1:
		if !c.decrementAttractSubCounter() { // $4357: CALL $440C
			return
		}
		c.RAM[0x002] &^= 0x40 // $435A-$4360: AND $BFh
		c.transitionTo(0)     // $4361: JP $414E

	default: // phase == 0 or phase >= 2
		if !c.countdownGetReadyMessage() { // $4364: CALL $4429; $4367: RET P
			return
		}
		c.drawTextRun(gameOverText[:], 0, 0xFF) // $4368-$436B: CALL $45F0 with DE=$475E
		c.setGameState(0xD8)                    // $436E-$4370
	}
}

// gameOverText is the ROM byte window at $475E-$4769: a single
// drawTextRun-format run (dest address $3969, i.e. name-table row 11,
// column 9) spelling "GAME OVER" -- decoded via the same font mapping
// confirmed elsewhere (0x21-0x3A = A-Z, 0x00 = space).
var gameOverText = [12]byte{0x69, 0x39, 0x27, 0x21, 0x2D, 0x25, 0x00, 0x2F, 0x36, 0x25, 0x32, 0xFF}

// startText is the 5-byte ROM window at $7895, written directly to name
// table address $394A by stateCrashSequence's own phase 4 (via $45A7, a
// manual SETWRT+OUT loop -- functionally identical to the standard
// LDIRVM copy this port already uses elsewhere, just not routed through
// the BIOS vector since $0006/$0007 are read directly). Plain name-table
// character codes (not drawTextRun's own control-byte format -- no
// destination prefix or $FE/$FF terminator, just 5 raw bytes matching
// BC=5), decoding via the same font mapping to "START".
var startText = [5]byte{0x33, 0x34, 0x21, 0x32, 0x34}

// stageLapTransitionRow is the per-stage byte read by stateResultsPhase2's
// own phases 2 and 3 (via $4040, an 8-bit-indexed lookup analogous to
// $403B's own "table+2*index" idiom but with a 1-byte stride): the
// target row $E04C counts down to during the post-lap car-repositioning
// animation. Only the first 6 bytes of the much larger 671-byte ROM
// window at $74B6 are read here (indexed by stage-1, values 0-5) -- the
// rest of that window is unrelated bulk data for some other, not-yet-
// traced consumer, not part of this table.
var stageLapTransitionRow = [6]byte{0x8B, 0x71, 0x54, 0x3A, 0x1B, 0x00}

// stateResultsPhase2 is game state 8, ported from $4373-$43C2. Every
// frame: refreshes the sprite-attribute-table VRAM copy and one
// scrolling sprite entry's position (blitSpriteAttributeTable/
// scrollTrafficSprite, shared with GameplayTick). Then a 4-phase
// DJNZ dispatch:
//
//   - phase 1: gates on countdownGetReadyMessage, then drawStageMapScreen
//     ($744C, already ported -- see graphics.go) and back to game state 0
//     (attract mode).
//   - phases 2 and 3 ($747D, $7495): a car-repositioning animation,
//     ported as stageLapTransitionRow. Phase 2 re-arms the player's
//     sprites every frame and counts $E04C (the player's own row) down
//     until it reaches a per-stage target row; phase 3 then re-arms the
//     sprites and counts $E004 down as a hold timer, alternating $E04C
//     between that same per-stage target and a fixed $C8 every 16 frames
//     (a visible blink/shake while the car settles into position) until
//     the hold expires and advanceAttractPhase moves on.
//   - phase 4 (the interesting one): increments $E043 (a lap/stage
//     counter) and, once it wraps past 7, resets it to 1 and bumps $E042
//     (a separate, still-unconfirmed counter) -- then either way advances
//     to a new top-level state directly ($E000/$E001 set as a pair):
//     normally $E000=4 (stateCrashSequence!) with $E001=0, or on the
//     wrap, $E000=3 (stateGetReadyWait) with $E001=3 -- i.e. the wrap
//     case is what actually starts the next race's countdown, while the
//     non-wrap case's $E000=4 looks like it's reusing stateCrashSequence
//     as some kind of per-lap transition, not literally "you crashed".
//     Also sets $E00D (gameOverFlag) to whatever value was just computed
//     for this pair (1 on the wrap, or the raw incremented $E043 value
//     2-6 otherwise) -- surprising for a flag whose only other confirmed
//     writes are plain 0/1 (see stateGetReady's use of it), so treat that
//     byte's "always boolean" assumption as unconfirmed too. The original
//     reaches this state via `CALL $414F` (transitionTo) purely for its
//     $E004=$18 side effect: the very next instruction
//     (`LD (E000h),BC`) immediately overwrites transitionTo's own
//     $E000/$E001 writes, so only $E004=$18 survives -- ported directly
//     rather than reproducing that pointless intermediate write.
//   - phase 0 or >=5: waits out decrementAttractSubCounter, then
//     setGameState($18).
func (c *Console) stateResultsPhase2() {
	c.blitSpriteAttributeTable(0x34) // $4374: CALL $526B
	c.scrollTrafficSprite()          // $4377: CALL $597D

	switch phase := *c.attractPhaseByte(); {
	case phase == 1:
		if !c.countdownGetReadyMessage() { // $437D: CALL $4429; $4380: RET P
			return
		}
		c.drawStageMapScreen() // $4381: CALL $744C -- see graphics.go
		c.setGameState(0)      // $4384-$4385

	case phase == 2: // $438A: JP $747D
		c.armPlayerCarSprites()                                    // $747D: CALL $6E1D
		c.RAM[0x04C]--                                             // $748B-$748E
		if c.RAM[0x04C] == stageLapTransitionRow[c.RAM[0x043]-1] { // $748F-$7490: CP (HL); RET NZ
			c.setGameState(0) // $7491-$7492: XOR A; JP $4167
		}

	case phase == 3: // $438F: JP $7495
		c.armPlayerCarSprites() // $7495: CALL $6E1D
		c.RAM[0x004]--          // $7498-$749B
		if c.RAM[0x004] == 0 {  // $749C: JR Z,$74B3
			c.advanceAttractPhase() // $74B3: JP $416A
			return
		}
		if c.RAM[0x004]&0x10 != 0 { // $749E-$74A2: BIT 4,(HL); JR NZ,$74AF
			c.RAM[0x04C] = 0xC8
		} else {
			c.RAM[0x04C] = stageLapTransitionRow[c.RAM[0x043]-1] // $74A4-$74AF
		}

	case phase == 4:
		c.RAM[0x043]++ // $4397: INC (HL) on $E043
		lapCounter := c.RAM[0x043]

		nextState, nextPhase := byte(4), byte(0) // $439B: LD BC,$0004h (default)
		gameOverValue := lapCounter

		if lapCounter == 7 { // $4399-$439E: CP 07h; JR NZ
			nextState, nextPhase = 3, 3 // $43A0: LD BC,$0303h
			gameOverValue = 1           // $43A3: LD A,01h
			c.RAM[0x043] = 1            // $43A5: LD (HL),A -- reset the lap counter
			c.RAM[0x042]++              // $43A6-$43A7: DEC HL; INC (HL)
		}

		c.RAM[0x004] = 0x18               // $43A8: CALL $414F -- only its E004=$18 side effect survives, see doc comment
		*c.gameStateByte() = nextState    // $43AB: LD (E000h),BC (low byte)
		*c.attractPhaseByte() = nextPhase // $43AB: LD (E000h),BC (high byte)
		*c.gameOverFlag() = gameOverValue // $43AF
		c.initPlayerCarSpritesAndFuel()   // $43B2: CALL $504C (partial -- see its own doc comment)
		c.RAM[0x10E] = 0x6C               // $43B5-$43B7: overrides initPlayerCarSpritesAndFuel's own $E10E=$E0

	default: // phase == 0 or phase >= 5
		if !c.decrementAttractSubCounter() { // $43BB: CALL $440C
			return
		}
		c.setGameState(0x18) // $43BE-$43C0
	}
}

// stateExtra43C3 is ported from $43C3-$4406, fully. It's the branch
// RunGameStateMachine takes when $E002's bit 6 is CLEAR (tested via
// "ADD A,A; JP M" -- the jump, taken when bit
// 6 is *set*, skips pushing $43C3 as a continuation; the doc comment on
// RunGameStateMachine's own `runContinuation` already has this right,
// this function's own comment previously had it backwards) -- i.e. every
// frame *before* a game has been started this session. This routine is
// what starts one: bit 6 gets set as a side effect of doing so (below),
// which is exactly what stops this whole routine from running at all
// once real gameplay begins.
//
// This was a genuinely unexplored routine: `cmd/disasm`'s recursive
// descent never reaches it, since it's only ever invoked via a manually
// pushed return address (RunGameStateMachine's "PUSH $43C3"), not a
// literal CALL/JP anywhere -- exactly the kind of control-flow trick
// static analysis misses. Hand-disassembled directly from the ROM bytes
// to find it.
//
// Samples input completely independently of UpdateInputAndTimers (its
// own $E039/$E03A edge-triggered pair via updateEdgeTriggeredPair, not
// $E008/$E009): if nothing was freshly pressed this frame, returns
// immediately ($43CD: RET Z). This is exactly why implementing this
// routine doesn't disturb the previously-verified, purely-timing-driven
// attract sequence -- every prior headless test simulated no input at
// all, so this always returned here without reaching anything below.
//
// If something WAS freshly pressed, $E004 is unconditionally zeroed
// ($43CE-$43D3) -- an HL-construction trick in the original (builds
// HL=$E000 cheaply from H=$E0, already in place from an immediately
// preceding "LD HL,$E004", plus a fresh L=0 read back from the byte just
// zeroed) with the side effect of genuinely zeroing $E004. Confirmed
// intentional, not incidental: for game state 1 (stateAttractWait), this
// resets its own $E004-based idle/blink countdown, so interacting with
// the level-select menu extends the wait rather than letting attract
// mode time out from under the player mid-interaction.
//
// Then:
//   - In game state 1 (stateAttractWait, showing "PLAY SELECT"/
//     "LEVEL A"/"LEVEL B"): if either fire button was freshly pressed
//     ($43DA-$43DC: AND $30, bits 4-5), commits the current level
//     selection into $E002 as $40 or $60 depending on $E03B (bit 6 set
//     either way), transitions to game state 3 (stateGetReadyWait), and
//     draws the level-select cursor one final time forced fully visible
//     ($5032, drawLevelSelectCursorAt(true)) rather than following its
//     normal blink timer -- confirming the player's choice. Any other
//     freshly-pressed input (practically, up/down on this 2-item menu)
//     instead toggles $E03B -- **the previously-missing write site**.
//   - In any other state (practically, only game state 0's attract
//     cycle, since bit 6 stays clear only until a game starts): any
//     input at all skips straight to game state 1, retriggering sound id
//     $1B (plays nothing -- see InitVDPAndSoundEngine), then
//     ($43FA-$4401: CALL $49BF) calls
//     showFullTitleAndMenu to draw the complete "ROAD FIGHTER" title and
//     menu immediately -- see that function's own doc comment. Without
//     this, skipping the attract sequence would land on game state 1
//     (the blinking-cursor wait) with nothing yet drawn, since phases
//     2-3 of stateAttractSequence (which normally draw the title/menu
//     gradually) never ran -- exactly the bug an early version of this
//     port had, caused by misreading $43FF's call target as a nearby but
//     unrelated address.
func (c *Console) stateExtra43C3() {
	edge := c.updateEdgeTriggeredPair(0x03A, c.readCombinedInput()) // $43C3-$43CD
	if edge == 0 {
		return // $43CD: RET Z -- nothing freshly pressed this frame
	}

	c.RAM[0x004] = 0 // $43CE-$43D3: see doc comment -- E000-construction trick, E004 genuinely zeroed

	if *c.gameStateByte() == 1 { // $43D4-$43D8: DJNZ, via the just-built $E000 read
		if edge&0x30 != 0 { // $43DA-$43DC: AND $30 -- either fire button
			selector := byte(0x40)
			if *c.levelSelectFlag() != 0 { // $43DE-$43E4: LD A,(DE); OR A; JR Z; LD A,$60
				selector = 0x60
			}
			c.RAM[0x002] = selector         // $43E6-$43E8: LD (E002h),A
			*c.gameStateByte() = 3          // $43E9: LD (HL),03h -- state 3, stateGetReadyWait
			*c.attractPhaseByte() = 0       // $43EB-$43EE: INC HL; LD C,0; LD (HL),C
			c.drawLevelSelectCursorAt(true) // $43F0: JP $5032
			return
		}
		*c.levelSelectFlag() ^= 1 // $4407-$440A: LD A,(DE); XOR 1; LD (DE),A
		return
	}

	// $43F3 onward: any input outside game state 1 skips straight to the
	// level-select menu.
	*c.gameStateByte() = 1   // $43F3: LD (HL),01h
	c.TriggerSound(0x1B)     // $43F5-$43F7: LD A,$1B; CALL $4B6E (plays nothing, see InitVDPAndSoundEngine)
	c.showFullTitleAndMenu() // $43FA-$4401: LD A,7h; E004=7; CALL $49BF (see its own doc comment)
	c.RAM[0x004] = 0         // $4402-$4405: XOR A; LD (E004h),A
}

// showFullTitleAndMenu is ported from $49BF-$4A24 (reached via a
// non-obvious call target: $43FF's "CALL $49BF" -- easy to misread as a
// nearby, unrelated address at a glance, which is exactly the mistake a
// first attempt at this made). $49BF is "CALL $49A6", i.e. showRoadTitle's
// own entry point, and the fall-through after it returns lands on
// $49C2/drawAttractMenuText and then $49CB/revealRoadFighterTitle in
// sequence -- the same three pieces phases 2-3 of stateAttractSequence
// build up gradually over ~280 frames, run back-to-back here instead so
// skipping the attract sequence shows the complete title screen
// immediately rather than jumping to game state 1 with nothing drawn.
//
// revealRoadFighterTitle is called with 7 specifically because that's
// what $E004 was just set to at $43FA -- the same value phase 3's own
// reveal loop uses on its *final* frame (the last frame before it hands
// off to phase 4), which is exactly why passing it here produces the
// fully-revealed title in one call instead of a partial one.
func (c *Console) showFullTitleAndMenu() {
	c.showRoadTitle()           // $49BF: CALL $49A6 (tail-jumps through $40EF/paintRoadTitleColor internally)
	c.drawAttractMenuText()     // falls through to $49C2 once the above RETs
	c.revealRoadFighterTitle(7) // falls through to $49CB with $E004=7
}
