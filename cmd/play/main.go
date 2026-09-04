// Command play runs the Go port (internal/game) against a real, in-memory
// VDP (internal/msx.VDP) and renders whatever ends up in VRAM using
// Ebitengine -- i.e. this draws the actual MSX Screen 2 output the ported
// code produces, the same way real MSX hardware would, rather than a
// hand-picked preview of one asset (cmd/gfxviewer's job). PSG register
// writes are backed by a real internal/psg.Chip and played through
// Ebitengine's audio player -- the same soundSystem/Tick wiring
// cmd/sndpreview uses (see psg.Chip.Tick's doc comment for why an explicit
// tick clock, not wall-clock time, is what keeps audio generation from
// racing ahead of what the game has actually decided should be playing).
//
// Most of the game's state machine is still stubbed (see
// docs/ANALYSIS.md's next-steps list and internal/game/state.go) -- this
// exists to run and see (and now hear) whatever IS ported, starting with
// the boot sequence and the Konami logo in stateAttractSequence.
//
// Esc simulates a full power cycle (fresh RAM and VRAM, ColdBoot re-run
// from scratch) -- a tool-only convenience for development, not something
// the original cartridge/BIOS supports.
//
// F12 dumps the currently-composited frame (the exact same image.RGBA
// Draw scales up for display, pre-scale) to a PNG file -- see -dump-path
// and -dump-every -- so debugging a rendering question is "read the PNG
// file" instead of reimplementing this file's own compositor in a
// throwaway test each time, which is how this tool used to have to be
// verified.
//
// F refills the fuel gauge to full ($E083=$D7, the same cap
// fuelPickupTick's own real refill logic clamps to) -- a tool-only cheat
// for easier testing/playing, not something the original cartridge
// supports. The gauge itself catches up within a few frames via the
// game's own normal per-frame fuel-tick logic (updateLaneFlagAndFuel),
// not forced here.
//
// E, during gameplay (game state 5), finishes the current level. It only
// writes the 16-bit distance counter ($E078/$E079) to $0F12 --
// checkDistanceMilestone's own finish threshold -- so everything after
// that is the game's own unmodified sequence: the finish state ($E049=3)
// and its deceleration, the results/map screen, the stage increment in
// stateResultsPhase2's phase 4, and the inter-stage animation, which
// together take a bit under a thousand frames. Nothing is forced past
// the distance itself. Gated on state 5 because that counter also drives
// the HUD progress marker (scrollTrafficSprite) outside a race, where
// poking it would just corrupt the display. One difference from driving
// there honestly: this skips straight past $0F09, so injectCheckpointText's
// "CHECK POINT" never fires for that level.
//
// Digit1-Digit6, while at the "PLAY SELECT" menu (game state 1), start a
// race directly at the corresponding stage (1-6) instead of always stage
// 1 -- a tool-only cheat, replacing a fire-button press rather than
// requiring one first. Both paths below first write E002's selector byte
// (game-active bit plus the current LEVEL A/B choice), which is what
// stateExtra43C3's own fire-button handling commits.
//
// Digit1 then does exactly what that handling does: game state 3
// (stateGetReadyWait) at phase 0, i.e. the real fresh-game start with its
// "3, 2, 1" countdown.
//
// Digit2-Digit6 instead call game.StartRaceAtStage, which is
// stateGetReady's own race-start branch ($415A-$4162) with the stage
// chosen rather than randomized: zero the lap counter, set the stage, run
// initPlayerCarSpritesAndFuel, transitionTo(4). Game state 4
// (stateCrashSequence) then runs the ROM's ordinary race entry -- screen
// wipe, HUD, 26 scroll steps of that stage's own scenery, "START", then
// statePlaying -- with no countdown at all, which is also what a genuine
// arrival at stage N>1 does.
//
// Three earlier attempts drove the fresh-game "get ready" sequence and
// tried to patch up the result instead, and each failed on the starting
// signal. Recorded here because the shape recurs: the gantry is not drawn
// on top of the road, it *is* road-buffer content, put there by
// initTrackLayoutData. So pre-rolling the road strip by hand scrolled it
// away and left the countdown lamps flashing unattached; restoring it
// across that scroll looked right in a byte dump but wrong in motion; and
// suppressing drawCountdownDigit still left a lone green lamp scrolling
// down the road, because triggerRaceStart ($424A) pokes $E1E3/$E1E4 =
// $48,$49 -- the gantry's own bottom lamp row -- straight into that
// buffer at the end of the countdown. Diverting out of the sequence late
// then showed the HUD twice with stage 1's static layout in between. Not
// running the countdown at all is what actually fixes it.
//
// What the cheat still does that the ROM never does is choose the stage,
// and the stage has to be chosen inside StartRaceAtStage rather than by
// poking $E043 afterwards -- see that function's own doc comment.
package main

import (
	"flag"
	"image"
	"image/png"
	"log"
	"os"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"go-msx-racer/internal/game"
	"go-msx-racer/internal/msx"
	"go-msx-racer/internal/msxrender"
	"go-msx-racer/internal/psg"
)

const (
	screenW = msxrender.ScreenW
	screenH = msxrender.ScreenH
	scale   = 3

	// borderPx is an approximate MSX screen border thickness, in native
	// (unscaled) pixels -- not tied to any confirmed real-hardware or TV-
	// timing spec, just enough margin to make the VDP's backdrop color
	// (see msx.VDP.BackdropColor) visible around the 256x192 active
	// display, the way it always is on a real MSX/CRT.
	borderPx = 16

	canvasW = screenW + 2*borderPx
	canvasH = screenH + 2*borderPx

	sampleRate = 44100
)

var (
	dumpPath  = flag.String("dump-path", "frame.png", "file path F12 (and -dump-every) write the composited frame to")
	dumpEvery = flag.Duration("dump-every", 0, "if set, also dump the composited frame to -dump-path on this interval (e.g. -dump-every=1s), overwriting each time")
)

// soundSystem backs game.Console: it embeds *msx.VDP for the VDP/keyboard/
// interrupt side of msx.System (used as-is by Draw/updateKeyboardMatrix
// via app.vdp directly) but routes PSG register writes to a real audio
// chip instead of VDP's own inert PSG register array, the same split
// cmd/sndpreview's soundOnlySystem uses.
type soundSystem struct {
	*msx.VDP
	chip *psg.Chip
}

func (s *soundSystem) WritePSGRegister(register, value byte) {
	s.chip.WriteRegister(register, value)
}

type app struct {
	console *game.Console
	vdp     *msx.VDP
	chip    *psg.Chip
	booted  bool
	canvas  *image.RGBA

	tickSampleAccum float64 // fractional leftover from sampleRate/TPS, so chip.Tick's sample counts sum exactly over time

	dumpRequested bool      // set by an F12 press in Update, consumed at the end of the next Draw
	lastDump      time.Time // zero value means "-dump-every has never fired yet", so the first interval starts counting from app startup
}

// dumpFrame writes canvas (the pre-scale, already-composited RGBA image
// Draw builds every frame) to path as a PNG.
func dumpFrame(canvas *image.RGBA, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, canvas)
}

// newVDP builds a fresh VDP already carrying real-MSX power-on defaults
// (see SetPowerOnDefaults) -- called both at startup and on every reset,
// so the emulated hardware never starts from an unrealistic
// all-registers-zero state.
func newVDP() *msx.VDP {
	vdp := msx.NewVDP()
	vdp.SetPowerOnDefaults()
	return vdp
}

// reset discards all emulated state -- RAM and VRAM alike -- and re-runs
// ColdBoot, i.e. a simulated power cycle. This is a tool-level convenience
// for development, not a feature of the original ROM: real Road Fighter
// cartridges have no soft-reset input, this is purely cmd/play's own.
//
// The audio chip is NOT recreated here -- it backs a live audio.Player
// that's already playing, and Console.Sys just needs to start writing to
// it again through a fresh soundSystem wrapping the new VDP.
func (a *app) reset() {
	a.vdp = newVDP()
	a.console = &game.Console{Sys: &soundSystem{VDP: a.vdp, chip: a.chip}}
	a.booted = false
}

// stageStartKeys maps Digit1-Digit6 to the corresponding stage number
// (this ROM's own stage tables -- stageMetaTileIDs, stageLapTransitionRow,
// roadFeatureStageRecords, etc. -- are all confirmed to cover exactly 6
// stages, 1-6). Cheat only, see Update's own doc comment.
var stageStartKeys = map[ebiten.Key]byte{
	ebiten.KeyDigit1: 1,
	ebiten.KeyDigit2: 2,
	ebiten.KeyDigit3: 3,
	ebiten.KeyDigit4: 4,
	ebiten.KeyDigit5: 5,
	ebiten.KeyDigit6: 6,
}

// updateKeyboardMatrix writes real host keyboard state into the emulated
// MSX keyboard matrix's rows 7 and 8 -- the only two rows the ported game
// code (internal/game.readCombinedInput, $46C3) actually reads. Bit
// assignments were confirmed by probing which matrix bits $46C3 extracts
// (each one changes a distinct bit of its combined output) and matching
// that against the standard MSX1 keyboard matrix layout: row 8 holds the
// arrow keys (bit4=LEFT, bit5=UP, bit6=DOWN, bit7=RIGHT) and SPACE
// (bit0); row 7 bit6 is SELECT. This lines up exactly with the game's own
// joystick-bit layout (up/down/left/right/fire1/fire2), i.e. the keyboard
// is wired to emulate a joystick -- arrows steer, Space is fire 1. SELECT
// has no obvious modern-keyboard equivalent, so fire 2 is mapped to
// Right Shift instead.
//
// Matrix bits are active-low (0 = key held down), matching real MSX
// hardware -- msx.VDP.KeyboardMatrix defaults every row to all-1s
// ("nothing pressed"), so only rows 7 and 8 need touching here.
func updateKeyboardMatrix(vdp *msx.VDP) {
	set := func(row, bit int, pressed bool) {
		if pressed {
			vdp.KeyboardMatrix[row] &^= 1 << uint(bit)
		} else {
			vdp.KeyboardMatrix[row] |= 1 << uint(bit)
		}
	}
	set(8, 5, ebiten.IsKeyPressed(ebiten.KeyArrowUp))
	set(8, 6, ebiten.IsKeyPressed(ebiten.KeyArrowDown))
	set(8, 4, ebiten.IsKeyPressed(ebiten.KeyArrowLeft))
	set(8, 7, ebiten.IsKeyPressed(ebiten.KeyArrowRight))
	set(8, 0, ebiten.IsKeyPressed(ebiten.KeySpace))
	set(7, 6, ebiten.IsKeyPressed(ebiten.KeyShiftRight)) // SELECT -- no modern equivalent; mapped here as fire 2
}

func (a *app) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		a.reset()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF12) {
		a.dumpRequested = true
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyF) {
		a.console.RAM[0x083] = 0xD7 // cheat: refill fuel -- see this file's own doc comment
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyE) && a.console.RAM[0x000] == 5 {
		// cheat: finish the current level -- see this file's own doc
		// comment. $E078/$E079 is the 16-bit distance counter and $0F12
		// is checkDistanceMilestone's own finish threshold, so this just
		// hands the game a position it would have reached by driving.
		a.console.RAM[0x078] = 0x12
		a.console.RAM[0x079] = 0x0F
	}
	if a.console.RAM[0x000] == 1 { // game state 1: "PLAY SELECT" menu
		for key, stage := range stageStartKeys {
			if !inpututil.IsKeyJustPressed(key) {
				continue
			}
			// cheat: start the race directly at `stage` -- see this
			// file's own doc comment. E002's selector byte (game-active
			// bit plus the current LEVEL A/B choice) is what
			// stateExtra43C3's own fire-button commit ($43E6-$43F0)
			// writes, and both paths below need it.
			selector := byte(0x40)
			if a.console.RAM[0x03B] != 0 { // levelSelectFlag: nonzero means LEVEL B
				selector = 0x60
			}
			a.console.RAM[0x002] = selector

			if stage == 1 {
				// Stage 1 keeps the real fresh-game start, countdown and
				// all: game state 3 (stateGetReadyWait) at phase 0, the
				// rest of what that commit does. The forced-visible cursor
				// redraw it also does ($5032) is skipped -- purely
				// cosmetic, and unexported.
				a.console.RAM[0x000] = 3
				a.console.RAM[0x001] = 0
			} else {
				// Later stages go in the way the ROM itself starts a race
				// when it isn't running the fresh-game countdown.
				a.console.StartRaceAtStage(stage)
			}
			break
		}
	}
	if !a.booted {
		a.console.ColdBoot()
		a.booted = true
	}
	updateKeyboardMatrix(a.vdp)
	a.console.VBlankHook()

	// Close out this tick's PSG register writes (made above, inside
	// VBlankHook's own TickSoundChannels calls) into one batch the audio
	// chip will hold for this many samples -- see psg.Chip.Tick's doc
	// comment for why this, not wall-clock time, is what has to gate audio
	// generation: ebitengine's audio backend can read up to ~0.5s ahead of
	// actual playback in a burst, and only an explicit tick count survives
	// that without collapsing distinct frames' writes together.
	a.tickSampleAccum += float64(sampleRate) / float64(ebiten.TPS())
	samples := int(a.tickSampleAccum)
	a.tickSampleAccum -= float64(samples)
	a.chip.Tick(samples)

	return nil
}

// Draw composites the visible screen from VRAM exactly the way the VDP
// would: a border filled with the current backdrop color (see
// msx.VDP.BackdropColor), then, inset by borderPx, the 256x192 active
// display -- for each of its 32x24 name-table cells, look up its
// character code, then read that code's 8 pattern bytes and 8 color
// bytes and composite via internal/msxgfx -- then the 32-entry hardware
// sprite attribute table on top (see drawSprites).
//
// Graphic 2 mode nominally splits the pattern/color tables into 3
// independent 2KB "thirds", one per 8-row band, and earlier versions of
// this function read from patBase+(row/8)*0x800 accordingly. That's the
// textbook TMS9918A behavior when the pattern-table/color-table mask
// registers (R4/R3) have their relevant low bits all set to 1 -- but
// this ROM's confirmed, never-rewritten boot-time values (R4=$0E, R3=$7F;
// see docs/ANALYSIS.md's VRAM memory map) don't satisfy that, and a
// real-hardware screenshot comparison (see docs/ANALYSIS.md's
// "Real-hardware screenshot comparison" section) confirms the practical
// effect for this specific ROM: all 3 bands read from the SAME physical
// block (band 0), not 3 independent ones. Verified by rendering with
// this collapsed addressing and finding it makes the previously-missing
// bottom two-thirds of the roadside scenery reappear correctly, with no
// regression to anything already confirmed working (every asset that
// writes to more than one band -- see internal/game.placeInAllThirds --
// writes the *identical* bytes to all 3 anyway, so reading only band 0
// shows the same content either way). This is a targeted fix for this
// ROM's specific, unchanging register values, not a general TMS9918A
// address-masking model for arbitrary R3/R4 -- this project has not
// independently verified that general formula, and no code anywhere in
// the ROM's disassembly ever targets bands 1/2 with unique content, so
// there's nothing here that would need one.
func (a *app) Draw(screen *ebiten.Image) {
	// The compositor lives in internal/msxrender so cmd/screenshot renders
	// through the exact same code -- see that package's own doc comment.
	msxrender.Compose(a.canvas, a.vdp, borderPx)

	due := *dumpEvery > 0 && time.Since(a.lastDump) >= *dumpEvery
	if a.dumpRequested || due {
		if err := dumpFrame(a.canvas, *dumpPath); err != nil {
			log.Println("frame dump failed:", err)
		} else {
			log.Println("wrote frame to", *dumpPath)
		}
		a.dumpRequested = false
		a.lastDump = time.Now()
	}

	img := ebiten.NewImageFromImage(a.canvas)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(scale, scale)
	screen.DrawImage(img, op)
}

func (a *app) Layout(outsideWidth, outsideHeight int) (int, int) {
	return canvasW * scale, canvasH * scale
}

func main() {
	flag.Parse()

	chip := psg.NewChip(sampleRate)

	audioContext := audio.NewContext(sampleRate)
	player, err := audio.NewPlayer(audioContext, chip)
	if err != nil {
		log.Fatal(err)
	}
	// ebitengine's default player buffer is 0.5s (oto/v3's own
	// Mux.defaultBufferSize) -- fine for a fixed audio clip, but it's a
	// standing pipeline latency for a live synthesizer like psg.Chip:
	// every sound is audible roughly 0.5s after the frame that triggered
	// it, forever, on top of (not fixed by) psg.Chip.Read's own startup
	// fix for the one-time silence-prefix bug. Shrunk to 50ms -- a few
	// frames' worth of slack against normal frame-time jitter, per the
	// audio package's own doc comment recommending a small buffer "if
	// you want to play a real-time PCM".
	player.SetBufferSize(50 * time.Millisecond)
	player.Play()

	vdp := newVDP()
	console := &game.Console{Sys: &soundSystem{VDP: vdp, chip: chip}}
	a := &app{
		console: console,
		vdp:     vdp,
		chip:    chip,
		canvas:  image.NewRGBA(image.Rect(0, 0, canvasW, canvasH)),
	}

	ebiten.SetWindowSize(canvasW*scale, canvasH*scale)
	ebiten.SetWindowTitle("go-msx-racer")
	if err := ebiten.RunGame(a); err != nil {
		log.Fatal(err)
	}
}
