// Package msxrender composites a msx.VDP's VRAM into an RGBA image the
// way a real TMS9918A would drive a display: Screen 2 background tiles
// first, then the 32-entry hardware sprite table on top.
//
// This is the one renderer in the project. cmd/play draws every frame
// through it and cmd/screenshot captures stills through it, so a still
// cannot drift from what the game actually shows -- which is exactly the
// failure mode CLAUDE.md warns about, and which this package exists to
// make impossible rather than merely discouraged.
package msxrender

import (
	"image"
	"image/color"
	"image/draw"

	"go-msx-racer/internal/msx"
	"go-msx-racer/internal/msxgfx"
)

// ScreenW and ScreenH are the MSX Screen 2 display size in pixels.
const (
	ScreenW = 256
	ScreenH = 192
)

// Frame composites the VDP's current VRAM into a new RGBA image, with
// `border` pixels of backdrop color on every side (0 for a bare screen).
func Frame(vdp *msx.VDP, border int) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, ScreenW+2*border, ScreenH+2*border))
	Compose(canvas, vdp, border)
	return canvas
}

// Compose draws into an existing canvas, so a per-frame caller doesn't
// allocate one every time.
func Compose(canvas *image.RGBA, vdp *msx.VDP, border int) {
	backdrop := msxgfx.Palette[vdp.BackdropColor()]
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{backdrop}, image.Point{}, draw.Src)
	drawBackground(canvas, vdp, border, backdrop)
	drawSprites(canvas, vdp, border)
}

func drawBackground(canvas *image.RGBA, vdp *msx.VDP, border int, backdrop color.RGBA) {
	nameBase := vdp.NameTableBase()
	patBase := vdp.PatternTableBase()
	colBase := vdp.ColorTableBase()

	for row := 0; row < 24; row++ {
		for col := 0; col < 32; col++ {
			code := int(vdp.VRAM[(nameBase+row*32+col)&0x3FFF])

			var pat, colr [8]byte
			pAddr := (patBase + code*8) & 0x3FFF
			cAddr := (colBase + code*8) & 0x3FFF
			copy(pat[:], vdp.VRAM[pAddr:pAddr+8])
			copy(colr[:], vdp.VRAM[cAddr:cAddr+8])

			pixels := msxgfx.TileRGBA(pat, colr, backdrop)
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					canvas.Set(border+col*8+x, border+row*8+y, pixels[y][x])
				}
			}
		}
	}
}

// drawSprites composites the 32-entry hardware sprite attribute table
// (see msx.VDP.SpriteAttrTableBase) on top of the already-drawn
// background, in real TMS9918A 16x16 "big sprite" mode: each entry is 4
// bytes (Y, X, sprite-pattern-table index, color -- low nibble is the
// palette index, color 0 meaning fully transparent same as a background
// tile's color 0 does NOT apply here, since a hardware sprite has no
// background nibble at all to begin with; bit 7 is the "early clock" flag
// that shifts the sprite 32px left, e.g. for smooth entry/exit at the
// screen edge). Y=$D0 is the real hardware's list terminator -- once seen,
// no further entries are drawn regardless of their own values, not just
// that one hidden -- and the displayed row is Y+1, a well-known
// TMS9918A quirk. Sprite 0 has the highest priority (drawn on top when
// entries overlap), so this composites from entry 31 down to entry 0.
// The real hardware's "8 sprites per scanline" flicker limit is not
// modeled -- out of scope for asset visibility, the same standard this
// project's PSG synthesizer already applies to its own simplifications.
func drawSprites(canvas *image.RGBA, vdp *msx.VDP, border int) {
	base := vdp.SpriteAttrTableBase()
	patBase := vdp.SpritePatternTableBase()

	count := 32
	for i := 0; i < 32; i++ {
		y := vdp.VRAM[(base+i*4)&0x3FFF]
		if y == 0xD0 {
			count = i
			break
		}
	}

	for i := count - 1; i >= 0; i-- {
		entry := vdp.VRAM[(base+i*4)&0x3FFF : (base+i*4)&0x3FFF+4]
		y := int(entry[0]) + 1 // TMS9918A quirk: displayed row is Y+1
		x := int(entry[1])
		patIndex := int(entry[2])
		colorByte := entry[3]
		if colorByte&0x80 != 0 { // early clock: shift 32px left
			x -= 32
		}
		colorIdx := colorByte & 0x0F
		if colorIdx == 0 { // fully transparent sprite -- nothing to draw
			continue
		}
		fg := msxgfx.Palette[colorIdx]

		var tile [32]byte
		// Real TMS9918A 16x16-sprite addressing: the low 2 bits of the
		// pattern number are ignored (forced to a multiple of 4, since a
		// 16x16 sprite occupies 4 consecutive 8x8 patterns), and the
		// pattern generator table is addressed in 8-byte (one 8x8
		// pattern) units, not 32-byte "tile" units -- NOT patIndex*32,
		// which was this port's own earlier, incorrect assumption
		// (confirmed wrong: it left every non-zero pattern number, e.g.
		// this game's own carSpritePatternTable entries, reading 4x too
		// far into VRAM -- for one of the player car's own two sprites,
		// landing in unrelated sprite data or even the background pattern
		// table instead of the intended 32-byte block).
		tAddr := (patBase + (patIndex&0xFC)*8) & 0x3FFF
		copy(tile[:], vdp.VRAM[tAddr:tAddr+32])
		visible := msxgfx.Sprite16Visible(tile)

		for row := 0; row < 16; row++ {
			py := y + row
			if py < 0 || py >= ScreenH {
				continue
			}
			for col := 0; col < 16; col++ {
				px := x + col
				if px < 0 || px >= ScreenW {
					continue
				}
				if visible[row][col] {
					canvas.Set(border+px, border+py, fg)
				}
			}
		}
	}
}
