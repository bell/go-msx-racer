// Command screenshot captures stills of the port straight from a
// msx.VDP, with no window and no Ebitengine involved -- so it runs
// anywhere, including a headless shell where cmd/play cannot start.
//
// It drives a real game.Console exactly the way cmd/play does: the same
// keyboard matrix rows for held keys, the same VBlankHook per frame, and
// the same level-select cheat entry points. Rendering goes through
// internal/msxrender, the one compositor cmd/play also draws every frame
// with, so a screenshot cannot show something the running game wouldn't.
//
//	go run ./cmd/screenshot -out screenshots
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"go-msx-racer/internal/game"
	"go-msx-racer/internal/msx"
	"go-msx-racer/internal/msxrender"
)

var (
	outDir = flag.String("out", "screenshots", "directory to write the PNGs into")
	border = flag.Int("border", 16, "pixels of backdrop-colored border on each side, as cmd/play draws")
	scale  = flag.Int("scale", 2, "integer pixel scale factor (nearest-neighbour, so pixel art stays sharp)")
	drive  = flag.Int("drive", 260, "frames to hold the accelerator for before capturing a stage")
)

// session is one booted console plus the VDP it renders from.
type session struct {
	console *game.Console
	vdp     *msx.VDP
}

func newSession() *session {
	vdp := msx.NewVDP()
	vdp.SetPowerOnDefaults()
	s := &session{console: &game.Console{Sys: vdp}, vdp: vdp}
	s.console.ColdBoot()
	return s
}

// hold presses or releases the keys cmd/play maps, using the same
// active-low matrix rows (see cmd/play's updateKeyboardMatrix).
func (s *session) hold(accelerate bool) {
	if accelerate {
		s.vdp.KeyboardMatrix[8] &^= 1 << 0 // SPACE
	} else {
		s.vdp.KeyboardMatrix[8] |= 1 << 0
	}
}

func (s *session) tick(n int) {
	for i := 0; i < n; i++ {
		s.console.VBlankHook()
	}
}

// tickUntil advances until cond holds, and reports whether it ever did.
func (s *session) tickUntil(limit int, cond func() bool) bool {
	for i := 0; i < limit; i++ {
		if cond() {
			return true
		}
		s.console.VBlankHook()
	}
	return cond()
}

func (s *session) state() byte { return s.console.RAM[0x000] }

// atMenu boots and runs the attract sequence through to the "PLAY SELECT"
// menu (game state 1), which is where both the select-screen shot and
// every stage start begins.
func atMenu() (*session, error) {
	s := newSession()
	if !s.tickUntil(200000, func() bool { return s.state() == 1 }) {
		return nil, fmt.Errorf("never reached the PLAY SELECT menu")
	}
	return s, nil
}

// captureSelect grabs the level-select menu. It waits a few frames past
// the menu appearing so the blinking cursor is in its visible half --
// drawLevelSelectCursor blinks off $E004 bit 3, so sampling on a frame
// where that bit is clear shows the cursor rather than a gap.
func captureSelect() (image.Image, error) {
	s, err := atMenu()
	if err != nil {
		return nil, err
	}
	if !s.tickUntil(64, func() bool { return s.console.RAM[0x004]&0x08 == 0 }) {
		return nil, fmt.Errorf("cursor never reached its visible blink phase")
	}
	s.tick(1)
	return msxrender.Frame(s.vdp, *border), nil
}

// captureStage starts a race on `stage` the way cmd/play's cheat does,
// then holds the accelerator so the road is actually scrolling and traffic
// has had time to appear before the still is taken.
func captureStage(stage byte) (image.Image, error) {
	s, err := atMenu()
	if err != nil {
		return nil, err
	}

	// The E002 selector byte both paths need: game-active bit, LEVEL A.
	s.console.RAM[0x002] = 0x40
	if stage == 1 {
		// The real fresh-game commit: state 3, phase 0, countdown and all.
		s.console.RAM[0x000] = 3
		s.console.RAM[0x001] = 0
	} else {
		s.console.StartRaceAtStage(stage)
	}

	if !s.tickUntil(4000, func() bool { return s.state() >= 5 }) {
		return nil, fmt.Errorf("stage %d: never reached gameplay", stage)
	}
	if got := s.console.RAM[0x043]; got != stage {
		return nil, fmt.Errorf("stage %d: ended up racing stage %d", stage, got)
	}

	s.hold(true)
	for i := 0; i < *drive; i++ {
		s.console.VBlankHook()
		if s.state() != 5 {
			return nil, fmt.Errorf("stage %d: left gameplay after %d frames (state %d)", stage, i, s.state())
		}
	}
	return msxrender.Frame(s.vdp, *border), nil
}

// upscale does nearest-neighbour integer scaling: pixel art must not be
// interpolated, or every tile edge goes soft.
func upscale(src image.Image, factor int) image.Image {
	if factor <= 1 {
		return src
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*factor, b.Dy()*factor))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					dst.Set(x*factor+dx, y*factor+dy, c)
				}
			}
		}
	}
	return dst
}

func write(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func main() {
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	shots := []struct {
		name    string
		capture func() (image.Image, error)
	}{
		{"select", captureSelect},
	}
	for stage := byte(1); stage <= 6; stage++ {
		stage := stage
		shots = append(shots, struct {
			name    string
			capture func() (image.Image, error)
		}{
			fmt.Sprintf("stage%d", stage),
			func() (image.Image, error) { return captureStage(stage) },
		})
	}

	for _, shot := range shots {
		img, err := shot.capture()
		if err != nil {
			log.Fatalf("%s: %v", shot.name, err)
		}
		path := filepath.Join(*outDir, shot.name+".png")
		if err := write(upscale(img, *scale), path); err != nil {
			log.Fatalf("%s: %v", shot.name, err)
		}
		b := img.Bounds()
		fmt.Printf("wrote %s (%dx%d at %dx)\n", path, b.Dx(), b.Dy(), *scale)
	}
}
