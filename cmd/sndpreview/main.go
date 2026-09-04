// Command sndpreview is an interactive PSG (AY-3-8910-compatible) sound
// preview tool. It backs internal/psg.Chip with a real audio output via
// Ebitengine, so you can hear the effect of PSG register writes directly --
// useful for verifying the audio synthesizer itself, and for triggering the
// game's own sound requests to hear what they actually do.
//
// The Go port's note/pitch bytecode interpreter (advanceSoundChannel, see
// docs/ANALYSIS.md) is ported for both note-command formats, and
// engineSoundPitchStep ($4C25, the continuous-pitch path for sound ID 1,
// confirmed to be an engine-RPM-style effect) is also ported. Once a game
// sound is triggered, this tool now calls TickSoundChannels every real
// frame (matching VBlankHook's own cadence) instead of a fixed 5-frame
// burst, so multi-note sounds and sustained/tremolo effects actually
// evolve in real time the way they would during real gameplay -- the
// previous one-shot burst could only ever capture whatever the FIRST note
// happened to be, then let that single tone/volume drone on indefinitely,
// which is not what triggering a real game sound sounds like.
//
// Controls:
//
//	1-8              play a C-major scale note (C4..C5) on PSG channel A
//	N                toggle a noise burst on channel A
//	S                silence all 3 channels, direct AND game-triggered
//	Left / Right     change the pending sound ID by 1
//	Up / Down        jump to the previous/next KNOWN sound ID (see below)
//	Enter / Space    trigger the pending sound ID via game.Console.TriggerSound
//	Esc / Q          quit
//
// "Known" sound IDs are every literal id this port's own code is confirmed
// to call TriggerSound with (see knownSounds below) -- not an exhaustive
// list of every id the ROM could produce, since base IDs range 0-63 and
// this port doesn't yet drive every code path that might trigger one.
package main

import (
	"fmt"
	"image/color"
	"log"
	"sort"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/basicfont"

	"go-msx-racer/internal/game"
	"go-msx-racer/internal/msx"
	"go-msx-racer/internal/psg"
)

const (
	screenWidth  = 760
	screenHeight = 560
	sampleRate   = 44100
)

// scaleNotes are C4..C5, played on PSG channel A by the 1-8 number keys.
var scaleNotes = [8]struct {
	name string
	freq float64
}{
	{"C4", 261.63},
	{"D4", 293.66},
	{"E4", 329.63},
	{"F4", 349.23},
	{"G4", 392.00},
	{"A4", 440.00},
	{"B4", 493.88},
	{"C5", 523.25},
}

// knownSound is one confirmed TriggerSound call site somewhere in this
// port's own code, with a short label saying where/why.
type knownSound struct {
	id    byte
	label string
}

// knownSounds is every literal id found via `grep -rn "TriggerSound("` across
// internal/game -- kept in sync by hand, not generated; re-check this list
// after adding a new TriggerSound call site elsewhere in the port.
var knownSounds = []knownSound{
	{0x02, "updateLaneFlagAndFuel: low-fuel warning (fuel < $10)"},
	{0x03, "stateAttractSequence phase 3 exit beep ($778C)"},
	{0x05, "stateGetReadyWait phase 9: countdown beep at count 4 (2-voice harmony, channels 1+2)"},
	{0x07, "stateGetReadyWait phase 9 (count 2) / stateCrashSequence phase 4 ($E002 bit 6 set) (2-voice harmony, channels 1+2)"},
	{0x1B, "boot / menu-skip / crash-map-empty triggers -- CONFIRMED TO PLAY NOTHING: base IDs 27, 28, and 29 (its own id and both repeatCount=3 partners) all point at the single terminator byte at the very end of the sound-data blob ($5027)"},
	{0x92, "checkDistanceMilestone: distance-bonus jingle (3-voice chord)"},
	{0x95, "stateCrashTrigger: no-crash-pending beep (3-voice chord)"},
	{0x98, "stateGetReadyWait: level-start fanfare, right as the screen wipe begins (3-voice chord)"},
}

func init() {
	sort.Slice(knownSounds, func(i, j int) bool { return knownSounds[i].id < knownSounds[j].id })
}

// labelFor returns the known-sound label for id, or "" if id isn't one of
// knownSounds.
func labelFor(id byte) string {
	for _, k := range knownSounds {
		if k.id == id {
			return k.label
		}
	}
	return ""
}

// nextKnown returns the known sound id after (dir=+1) or before (dir=-1)
// `from`, wrapping around, or `from` unchanged if knownSounds is empty.
func nextKnown(from byte, dir int) byte {
	if len(knownSounds) == 0 {
		return from
	}
	// Find the closest known index in the requested direction; if `from`
	// isn't itself a known id, this still lands on a sensible neighbor.
	idx := sort.Search(len(knownSounds), func(i int) bool { return knownSounds[i].id >= from })
	if dir > 0 {
		if idx < len(knownSounds) && knownSounds[idx].id == from {
			idx++
		}
		if idx >= len(knownSounds) {
			idx = 0
		}
	} else {
		idx--
		if idx < 0 {
			idx = len(knownSounds) - 1
		}
	}
	return knownSounds[idx].id
}

// soundOnlySystem backs game.Console for this tool: it reuses *msx.VDP for
// the VDP/interrupt side of msx.System (harmless here -- this tool never
// renders VDP output) but routes PSG writes to our real audio chip instead
// of VDP's own inert PSG register array.
type soundOnlySystem struct {
	*msx.VDP
	chip *psg.Chip
}

func (s *soundOnlySystem) WritePSGRegister(register, value byte) {
	s.chip.WriteRegister(register, value)
}

type app struct {
	chip            *psg.Chip
	console         *game.Console
	face            *text.GoXFace
	lastNote        string
	noiseOn         bool
	soundID         byte
	playing         bool // true once a game sound has been triggered -- gates continuous ticking
	ticks           int
	log             []string
	tickSampleAccum float64 // fractional leftover from sampleRate/TPS, so chip.Tick's sample counts sum exactly over time
}

func (a *app) Update() error {
	for i, k := range []ebiten.Key{ebiten.Key1, ebiten.Key2, ebiten.Key3, ebiten.Key4, ebiten.Key5, ebiten.Key6, ebiten.Key7, ebiten.Key8} {
		if inpututil.IsKeyJustPressed(k) {
			a.playNote(i)
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyN) {
		a.noiseOn = !a.noiseOn
		a.applyMixerAndNoise()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyS) {
		a.silence()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyLeft) {
		a.soundID--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyRight) {
		a.soundID++
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyUp) {
		a.soundID = nextKnown(a.soundID, -1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDown) {
		a.soundID = nextKnown(a.soundID, 1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		a.triggerGameSound()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyQ) {
		return ebiten.Termination
	}

	// Advance the sound engine every real frame, same cadence as
	// VBlankHook, for as long as a game sound is active -- this is what
	// lets a multi-note sound or a sustained/tremolo effect actually be
	// heard evolving over time instead of freezing after one snapshot.
	if a.playing {
		a.console.TickSoundChannels()
		a.ticks++
		if !anyChannelActive(a.console) {
			a.playing = false
			a.log = append(a.log, fmt.Sprintf("  (finished after %d frames)", a.ticks))
			a.trimLog()
		}
	}

	// Close out this tick's register writes -- from the note/noise keys,
	// TriggerSound, and TickSoundChannels above -- into one batch the chip
	// will hold for this many samples once Read gets to it. This must run
	// once per Update call, after every WriteRegister call above: it's
	// what makes audio generation advance in lockstep with game logic
	// instead of racing ahead of it under the audio backend's own
	// buffering, which is what caused the "same sound plays differently
	// each time" bug -- see psg.Chip.Tick's doc comment for the full story.
	a.tickSampleAccum += float64(sampleRate) / float64(ebiten.TPS())
	samples := int(a.tickSampleAccum)
	a.tickSampleAccum -= float64(samples)
	a.chip.Tick(samples)

	return nil
}

// anyChannelActive reports whether any of the 3 real channel records
// still has a nonzero chOffSoundID (see sound.go's own doc comment for
// why that byte doubles as the interpreter's activity flag).
func anyChannelActive(c *game.Console) bool {
	for ch := 0; ch < 3; ch++ {
		base := 0x010 + ch*0x0B
		if c.RAM[base+2] != 0 {
			return true
		}
	}
	return false
}

// playNote sets channel A's tone period and volume directly -- this is the
// "poke the PSG and hear it" path, independent of the game port entirely.
func (a *app) playNote(i int) {
	note := scaleNotes[i]
	period := int(psg.ClockHz/(16*note.freq) + 0.5)
	a.chip.WriteRegister(0, byte(period))
	a.chip.WriteRegister(1, byte(period>>8)&0x0F)
	a.chip.WriteRegister(8, 15) // channel A volume, full, no envelope
	a.applyMixerAndNoise()
	a.lastNote = fmt.Sprintf("channel A: %s (%.2f Hz, period %d)", note.name, note.freq, period)
}

// applyMixerAndNoise writes register 7 to match the current noise-burst
// toggle: channel A always has its tone enabled; noise A is enabled only
// while toggled on. Channels B/C are left disabled (no tone, no noise).
// Also mirrors the value into $E031 so a subsequent game-sound trigger's
// own TickSoundChannels call (which re-applies $E031 every frame, see
// sound.go) doesn't immediately clobber this.
func (a *app) applyMixerAndNoise() {
	mixer := byte(0b111110) // tone B,C disabled; noise A,B,C disabled; tone A enabled (bit0=0)
	if a.noiseOn {
		mixer &^= 1 << 3 // clear noise-A-disable bit -> noise A enabled
		a.chip.WriteRegister(8, 15)
	}
	a.chip.WriteRegister(7, mixer)
	a.console.RAM[0x031] = mixer
}

func (a *app) silence() {
	a.chip.WriteRegister(8, 0)
	a.chip.WriteRegister(9, 0)
	a.chip.WriteRegister(10, 0)
	a.noiseOn = false
	a.lastNote = "silenced"

	// Also stop any game-triggered sound: zero each channel's stored id
	// (chOffSoundID, offset 2 -- see sound.go) so TickSoundChannels treats
	// all 3 as silent, matching what a real note-stream's own $FF
	// terminator does.
	for ch := 0; ch < 3; ch++ {
		base := 0x010 + ch*0x0B
		a.console.RAM[base+2] = 0
	}
	a.playing = false
	a.log = append(a.log, "silenced (direct + game channels)")
	a.trimLog()
}

// triggerGameSound calls the real, ported game.Console.TriggerSound --
// exercising the actual priority/channel-allocation logic traced in
// sound.go, not a simulation of it -- then switches this tool into
// continuous-ticking mode (see Update) so the triggered sound plays out
// in real time instead of a one-shot snapshot.
func (a *app) triggerGameSound() {
	a.console.TriggerSound(a.soundID)
	a.playing = true
	a.ticks = 0

	label := labelFor(a.soundID)
	msg := fmt.Sprintf("Triggered $%02X", a.soundID)
	if label != "" {
		msg += ": " + label
	}
	a.log = append(a.log, msg)
	a.trimLog()
}

func (a *app) trimLog() {
	if len(a.log) > 8 {
		a.log = a.log[len(a.log)-8:]
	}
}

var (
	colBG      = color.RGBA{0x18, 0x18, 0x20, 0xFF}
	colTitle   = color.White
	colSub     = color.RGBA{0xAA, 0xAA, 0xB4, 0xFF}
	colFooter  = color.RGBA{0x78, 0x78, 0x82, 0xFF}
	colWarning = color.RGBA{0xE0, 0xB0, 0x40, 0xFF}
	colKnown   = color.RGBA{0x70, 0xD0, 0x90, 0xFF}
)

func (a *app) Draw(screen *ebiten.Image) {
	screen.Fill(colBG)

	drawText(screen, "PSG sound preview", a.face, 16, 30, colTitle)

	reg := a.chip.Registers()
	regLine := "R0-R7:  "
	for i := 0; i < 8; i++ {
		regLine += fmt.Sprintf("%02X ", reg[i])
	}
	drawText(screen, regLine, a.face, 16, 60, colSub)
	regLine2 := "R8-R15: "
	for i := 8; i < 16; i++ {
		regLine2 += fmt.Sprintf("%02X ", reg[i])
	}
	drawText(screen, regLine2, a.face, 16, 78, colSub)

	drawText(screen, "Last direct action: "+a.lastNote, a.face, 16, 110, colTitle)

	idLine := fmt.Sprintf("Pending sound ID: $%02X", a.soundID)
	if a.playing {
		idLine += fmt.Sprintf("   (PLAYING, frame %d)", a.ticks)
	}
	drawText(screen, idLine, a.face, 16, 145, colTitle)
	if label := labelFor(a.soundID); label != "" {
		drawText(screen, "  known: "+label, a.face, 16, 163, colKnown)
	} else {
		drawText(screen, "  (not one of the known ids below -- Up/Down jumps between those)", a.face, 16, 163, colFooter)
	}

	y := 195
	drawText(screen, "Known sound IDs (Up/Down to jump between them):", a.face, 16, y, colSub)
	y += 20
	for _, k := range knownSounds {
		marker := "  "
		if k.id == a.soundID {
			marker = "->"
		}
		drawText(screen, fmt.Sprintf("%s $%02X  %s", marker, k.id, k.label), a.face, 16, y, colSub)
		y += 18
	}

	y += 10
	drawText(screen, "Trigger log:", a.face, 16, y, colSub)
	y += 20
	for _, line := range a.log {
		drawText(screen, line, a.face, 16, y, colWarning)
		y += 18
	}

	footer1 := "1-8: play scale note (chan A)   N: toggle noise   S: silence (direct + game)"
	footer2 := "Left/Right: id +-1   Up/Down: jump known id   Enter/Space: trigger   Esc/Q: quit"
	drawText(screen, footer1, a.face, 16, screenHeight-40, colFooter)
	drawText(screen, footer2, a.face, 16, screenHeight-20, colFooter)
}

func (a *app) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func drawText(dst *ebiten.Image, s string, face *text.GoXFace, x, y int, clr color.Color) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(clr)
	text.Draw(dst, s, face, op)
}

func main() {
	chip := psg.NewChip(sampleRate)

	audioContext := audio.NewContext(sampleRate)
	player, err := audio.NewPlayer(audioContext, chip)
	if err != nil {
		log.Fatal(err)
	}
	// See cmd/play's own identical call for why: ebitengine's default
	// 0.5s player buffer is a standing latency for a live synthesizer,
	// separate from (not fixed by) psg.Chip.Read's startup fix.
	player.SetBufferSize(50 * time.Millisecond)
	player.Play()

	sys := &soundOnlySystem{VDP: msx.NewVDP(), chip: chip}
	console := &game.Console{Sys: sys}
	// Mirror InitVDPAndSoundEngine's own mixer setup ($4685-4689: tone
	// enabled/noise disabled on all 3 channels) directly into $E031,
	// without calling the full boot routine (which would also clear all
	// of VRAM and program VDP mode registers this audio-only tool has no
	// use for) -- otherwise the first TickSoundChannels call would
	// re-apply $E031's Go zero-value and silently re-enable noise on
	// every channel, exactly the bug this session found and fixed in the
	// real boot path.
	console.RAM[0x031] = 0xB8

	a := &app{
		chip:     chip,
		console:  console,
		face:     text.NewGoXFace(basicfont.Face7x13),
		lastNote: "(none yet)",
		soundID:  knownSounds[0].id,
	}

	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("go-msx-racer -- PSG sound preview")
	if err := ebiten.RunGame(a); err != nil {
		log.Fatal(err)
	}
}
