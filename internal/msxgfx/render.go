// Package msxgfx renders MSX1 (TMS9918A) Screen 2 "Graphic 2" tile data:
// an 8x8 tile is 8 pattern bytes (1bpp, MSB first) plus, for background
// tiles, 8 matching color bytes -- one per row, high nibble = foreground
// color index, low nibble = background color index. A pixel is drawn in
// the foreground color where its pattern bit is 1, background otherwise.
//
// This differs from MSX Screen 1 (Graphic 1), where color is one byte per
// 8 *characters* (shared), not per character-row -- Screen 2's per-row
// color table is what lets a single 8x8 tile show up to 8 different
// 2-color combinations, one per scanline.
//
// Road Fighter's VRAM layout (derived from the game's own VDP register
// values -- see docs/ANALYSIS.md) places the color table at $0000-$17FF,
// the sprite pattern generator table at $1800-$1FFF, the background
// pattern generator table at $2000-$37FF, and the name table at
// $3800-$3AFF. A destination address's range is therefore a reliable
// signal for whether a decoded RLE stream is pattern or color data in this
// ROM: $0xxx/$1xxx = color (or sprite pattern, in the narrow $1800-$1FFF
// band), $2xxx/$3xxx (below the name table) = background pattern.
//
// A 16x16 hardware sprite (MSX1's "big sprite" mode) is 32 bytes, but NOT
// 16 rows of 2 bytes as a naive guess would have it -- it's four
// independent 8-byte 8x8 patterns in a fixed order: top-left, bottom-left,
// top-right, bottom-right. See Sprite16Mono.
//
// A third, different 16x16 layout shows up for at least one asset in this
// ROM (a background-tile-composited title font, not a hardware sprite):
// four 8x8 background pattern-table tiles in plain raster order -- top-left,
// top-right, bottom-left, bottom-right. See BigChar16Mono. Three different
// tile-orderings across one 16KB ROM is a good reminder that "32 bytes ==
// 16x16" doesn't imply a single universal byte layout.
package msxgfx

import "image/color"

// Palette is the standard TMS9918A 15-color palette. Index 0 is
// "transparent" -- on real hardware this is NOT black, it means "show the
// VDP's backdrop color instead" (register 7's low nibble), independently
// per nibble: a tile with foreground color 0 shows the backdrop wherever
// its pattern bit is set, even if its background color is a real color,
// and vice versa. This entry exists only as a placeholder for direct
// indexing (e.g. rendering the backdrop itself, which really is index 0
// = literal black when register 7 selects it); TileRGBA does not use it
// for tile pixels -- see its `backdrop` parameter.
var Palette = [16]color.RGBA{
	0:  {0, 0, 0, 255},       // literal black (only correct for the backdrop layer itself)
	1:  {0, 0, 0, 255},       // black
	2:  {33, 200, 66, 255},   // medium green
	3:  {94, 220, 120, 255},  // light green
	4:  {84, 85, 237, 255},   // dark blue
	5:  {125, 118, 252, 255}, // light blue
	6:  {212, 82, 77, 255},   // dark red
	7:  {66, 235, 245, 255},  // cyan
	8:  {252, 85, 84, 255},   // medium red
	9:  {255, 121, 120, 255}, // light red
	10: {212, 193, 84, 255},  // dark yellow
	11: {230, 206, 128, 255}, // light yellow
	12: {33, 176, 59, 255},   // dark green
	13: {201, 91, 186, 255},  // magenta
	14: {204, 204, 204, 255}, // gray
	15: {255, 255, 255, 255}, // white
}

// TileRGBA renders one 8x8 tile to an 8x8 grid of colors, given 8 pattern
// bytes and 8 matching color bytes (row i's color byte is colorRows[i]).
//
// Color index 0, in either the foreground or background nibble, is the
// TMS9918A's "transparent" code: it does not mean black, it means "show
// the backdrop color" (the VDP's register 7, low nibble) instead --
// independently per nibble, so a tile can have a real foreground color
// over a transparent (backdrop-showing) background, or vice versa. This
// is genuinely significant for this ROM: most of the Konami logo's tiles
// use color 0 for their background nibble specifically so the logo shows
// against whatever backdrop color the game has selected (blue), not a
// hardcoded black box -- rendering index 0 as literal black, as an
// earlier version of this package did, is a real correctness bug, not a
// cosmetic one. `backdrop` supplies that color for this call; pass
// whatever the live VDP's register 7 currently selects
// (`msx.VDP.BackdropColor`) when rendering real running state, or a
// reasonable placeholder for tools with no live VDP (e.g. a preview
// canvas's own background).
func TileRGBA(pattern, colorRows [8]byte, backdrop color.RGBA) (pixels [8][8]color.RGBA) {
	resolve := func(idx byte) color.RGBA {
		if idx == 0 {
			return backdrop
		}
		return Palette[idx]
	}
	for row := 0; row < 8; row++ {
		p := pattern[row]
		fg := resolve(colorRows[row] >> 4)
		bg := resolve(colorRows[row] & 0x0F)
		for bit := 0; bit < 8; bit++ {
			if p&(0x80>>uint(bit)) != 0 {
				pixels[row][bit] = fg
			} else {
				pixels[row][bit] = bg
			}
		}
	}
	return
}

// TileMono renders one 8x8 tile using a single foreground/background color
// pair for the whole tile (no per-row color table) -- used for data whose
// color companion isn't known/extracted, or for genuine hardware sprites
// (MSX1 sprites are single-color, set via the sprite attribute table at
// runtime, not a ROM color table).
func TileMono(pattern [8]byte, fg, bg color.RGBA) (pixels [8][8]color.RGBA) {
	for row := 0; row < 8; row++ {
		p := pattern[row]
		for bit := 0; bit < 8; bit++ {
			if p&(0x80>>uint(bit)) != 0 {
				pixels[row][bit] = fg
			} else {
				pixels[row][bit] = bg
			}
		}
	}
	return
}

// spriteQuadrantOffsets gives the pixel origin of each of a 16x16 sprite's
// four 8-byte quadrants within the 32-byte pattern, in storage order.
var spriteQuadrantOffsets = [4]struct{ x, y int }{
	{0, 0}, // bytes 0-7:   top-left
	{0, 8}, // bytes 8-15:  bottom-left
	{8, 0}, // bytes 16-23: top-right
	{8, 8}, // bytes 24-31: bottom-right
}

// Sprite16Mono renders one 16x16 MSX1/TMS9918A hardware sprite pattern
// (32 bytes) in a single flat color pair. Critically, a 16x16 sprite is
// NOT stored as 16 rows of 2 bytes each (that's a natural but incorrect
// guess) -- the VDP addresses it as four independent 8x8 patterns, always
// in the order top-left, bottom-left, top-right, bottom-right. Rendering
// with the wrong layout still "looks like something" (8 real pixel rows
// exist, just interleaved wrong), which is what makes this easy to get
// wrong and not notice -- confirmed here by the naive interleaving
// producing checkerboard noise where the quadrant layout produces clean
// vehicle silhouettes, on the same bytes.
func Sprite16Mono(tile [32]byte, fg, bg color.RGBA) (pixels [16][16]color.RGBA) {
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			pixels[y][x] = bg
		}
	}
	for q, off := range spriteQuadrantOffsets {
		for row := 0; row < 8; row++ {
			b := tile[q*8+row]
			for bit := 0; bit < 8; bit++ {
				if b&(0x80>>uint(bit)) != 0 {
					pixels[off.y+row][off.x+bit] = fg
				}
			}
		}
	}
	return
}

// Sprite16Visible reports, for each pixel of a 16x16 hardware sprite
// pattern, whether that pixel is "on" (drawn in the sprite's one color) --
// unlike Sprite16Mono, which fills a complete 16x16 grid including a solid
// background color, a real hardware sprite has no background of its own:
// only "on" pixels are ever drawn, and everything else (whatever's
// already on screen underneath) shows through unchanged. Use this instead
// of Sprite16Mono when compositing a sprite over an already-rendered
// background rather than onto a flat preview canvas. Shares
// spriteQuadrantOffsets with Sprite16Mono, so the two never disagree on
// layout.
func Sprite16Visible(tile [32]byte) (visible [16][16]bool) {
	for q, off := range spriteQuadrantOffsets {
		for row := 0; row < 8; row++ {
			b := tile[q*8+row]
			for bit := 0; bit < 8; bit++ {
				if b&(0x80>>uint(bit)) != 0 {
					visible[off.y+row][off.x+bit] = true
				}
			}
		}
	}
	return
}

// bigCharQuadrantOffsets gives the pixel origin of each of a 16x16
// background-tile character's four 8-byte quadrants, in storage order --
// plain raster order (unlike the hardware sprite order above).
var bigCharQuadrantOffsets = [4]struct{ x, y int }{
	{0, 0}, // bytes 0-7:   top-left
	{8, 0}, // bytes 8-15:  top-right
	{0, 8}, // bytes 16-23: bottom-left
	{8, 8}, // bytes 24-31: bottom-right
}

// BigChar16Mono renders one 16x16 character built from four ordinary
// background pattern-table tiles (32 bytes total) stored in plain raster
// order -- top-left, top-right, bottom-left, bottom-right. This is not a
// hardware feature (MSX1 has no such addressing mode); it's just a game
// storing a big character as four consecutive tiles in the natural reading
// order, for compositing onto the name table as 2x2 background characters.
// Confirmed against assets/gfx/bigfont_pattern_road_4A25.bin, which spells
// out (at least) "ROAD" when rendered this way. Do not confuse with
// Sprite16Mono's different (hardware) quadrant order -- the same 32 bytes
// render as noise under the wrong one of the two.
func BigChar16Mono(tile [32]byte, fg, bg color.RGBA) (pixels [16][16]color.RGBA) {
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			pixels[y][x] = bg
		}
	}
	for q, off := range bigCharQuadrantOffsets {
		for row := 0; row < 8; row++ {
			b := tile[q*8+row]
			for bit := 0; bit < 8; bit++ {
				if b&(0x80>>uint(bit)) != 0 {
					pixels[off.y+row][off.x+bit] = fg
				}
			}
		}
	}
	return
}
