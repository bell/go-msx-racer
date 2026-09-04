package main

import (
	"fmt"
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"

	"go-msx-racer/internal/msxgfx"
)

// This file synthesizes viewer-only "atlas" assets that have no single
// backing *.bin file: the actual colored sprite a traffic object or the
// player's own car resolves to at runtime, and what each of the 15
// level-layout "meta tile" IDs spawns. None of this is independent ROM
// data -- it's the same sprite pattern bytes already viewable elsewhere
// in assets/gfx, recombined here exactly the way internal/game's own
// ported code recombines them (see the lookup tables below, each citing
// its source in internal/game/gameplay.go), purely so the result can
// actually be *seen* rather than re-derived by reading code.
//
// Both a traffic object and the player's own car are two 16x16 hardware
// sprites stacked at the same position (confirmed: positionTrafficSprite
// and armPlayerCarSprites both write a "pattern" and "pattern+4" entry at
// identical row/column) -- a standard MSX1 trick for faking a 2-color
// sprite out of two single-color ones, with the lower sprite-attribute
// address (written first, here always the "+0" pattern) taking priority
// wherever both would draw the same pixel. composeOverlaySprite renders
// exactly that layering.

// trafficSpritePatternByType is internal/game/gameplay.go's table of the
// same name (the 9-byte ROM table at $72AF), duplicated here because
// cmd/gfxviewer is a separate package that doesn't import internal/game.
var trafficSpritePatternByType = [9]byte{0x7C, 0x00, 0x00, 0x84, 0x8C, 0x8C, 0x50, 0x50, 0x60}

// metaTileColors is internal/game/gameplay.go's table of the same name
// (the 18-byte ROM table at $536B): {topColor, bottomColor} per kind.
var metaTileColors = [9][2]byte{
	{0x01, 0x0C}, {0x01, 0x04}, {0x01, 0x04}, {0x01, 0x0D}, {0x0B, 0x01},
	{0x0B, 0x01}, {0x01, 0x08}, {0x01, 0x08}, {0x0F, 0x0D},
}

// carSpritePatternTable is internal/game/gameplay.go's table of the same
// name (the 11-byte ROM table at $6E3F), indexed by the per-state
// car-sprite-pattern selector byte $E053.
var carSpritePatternTable = [11]byte{0x00, 0x10, 0x18, 0x20, 0x08, 0x38, 0x30, 0x28, 0x40, 0x48, 0x68}

// playerCarColors are the two fixed sprite-attribute colors carInitTable10E
// ($5185, copied once at race init) gives the player's own 2-sprite pair --
// see armPlayerCarSprites' doc comment: entry $E112 (pattern+0) always gets
// color $01, entry $E116 (pattern+4) always gets color $06, never touched
// by armPlayerCarSprites itself.
var playerCarColors = [2]byte{0x01, 0x06}

// metaTileDefinitions is internal/game/gameplay.go's table of the same
// name (the 90-byte ROM table at $5311): 15 meta-tile IDs, each two 3-byte
// half-defs {kind, param1, param2}. Only byte 0 (kind, 0-8) matters here --
// it's what selects the rendered sprite via trafficSpritePatternByType/
// metaTileColors; param1/param2 (spacing/timing) have no visual form.
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

// spritePatternSourceNames are the three already-loaded assets that
// together form the hardware sprite pattern table's contents, in VRAM
// order: SpritesVehiclesPattern (patterns 0-9, $1800), its runtime
// horizontal mirror (patterns 10-15, computed -- see
// sprites_vehicles_mirrored_computed.bin), and SpritesCrashDebrisBonusPattern
// (patterns 16-40, $1A00). Concatenating them in this order reproduces the
// same byte layout the real sprite pattern table has at runtime, so a
// pattern number found in trafficSpritePatternByType/carSpritePatternTable
// can be looked up the same way regardless of which of the three files it
// actually falls in.
var spritePatternSourceNames = []string{
	"sprites_vehicles_5A73",
	"sprites_vehicles_mirrored_computed",
	"sprites_crash_debris_bonus_5B97",
}

// buildAtlases returns the synthesized, non-file-backed atlas assets
// described in this file's own doc comment. byName/loaded are the assets
// already read from disk by main(), used here to assemble the combined
// sprite pattern buffer.
func buildAtlases(byName map[string]int, loaded []asset) []asset {
	var patterns []byte
	for _, name := range spritePatternSourceNames {
		i, ok := byName[name]
		if !ok {
			// Missing prerequisite (e.g. pointed at a directory other than
			// assets/gfx) -- skip the atlases rather than panic.
			return nil
		}
		patterns = append(patterns, loaded[i].data...)
	}

	tile := func(patternNumber byte) [32]byte {
		var t [32]byte
		off := int(patternNumber) * 8
		if off >= 0 && off+32 <= len(patterns) {
			copy(t[:], patterns[off:off+32])
		}
		return t
	}

	return []asset{
		buildTrafficObjectAtlas(tile),
		buildPlayerCarAtlas(tile),
		buildMetaTileAtlas(tile),
	}
}

// composeOverlaySprite layers two 16x16 hardware sprites at the same
// position, topTile drawn over bottomTile wherever both have a pixel set
// (matching real sprite-attribute priority -- see this file's own doc
// comment), against bg where neither does.
func composeOverlaySprite(topTile, bottomTile [32]byte, topColor, bottomColor byte, bg color.RGBA) (pixels [16][16]color.RGBA) {
	top := msxgfx.Sprite16Visible(topTile)
	bottom := msxgfx.Sprite16Visible(bottomTile)
	topRGBA := msxgfx.Palette[topColor&0x0F]
	bottomRGBA := msxgfx.Palette[bottomColor&0x0F]
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			switch {
			case top[y][x]:
				pixels[y][x] = topRGBA
			case bottom[y][x]:
				pixels[y][x] = bottomRGBA
			default:
				pixels[y][x] = bg
			}
		}
	}
	return
}

// atlasGrid lays out n 16x16 (or wider, for multi-tile entries) cells in a
// row of `cols` columns with a 1px gap and a small caption baked under
// each cell, returning the composed image.
func atlasGrid(n, cols, cellW, cellH int, bg color.RGBA, draw func(i int, dst *image.RGBA, x0, y0 int), caption func(i int) string) *ebiten.Image {
	const gap = 4
	const capH = 12
	stride := cellW + gap
	rows := (n + cols - 1) / cols
	if rows < 1 {
		rows = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, cols*stride+gap, rows*(cellH+capH+gap)+gap))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, bg)
		}
	}
	for i := 0; i < n; i++ {
		x0 := gap + (i%cols)*stride
		y0 := gap + (i/cols)*(cellH+capH+gap)
		draw(i, img, x0, y0)
		if caption != nil {
			drawCaption(img, caption(i), x0, y0+cellH+2)
		}
	}
	return ebiten.NewImageFromImage(img)
}

// drawCaption renders a short ASCII string directly into an *image.RGBA
// using a fixed 3x5 dot-matrix digit/letter set -- deliberately tiny and
// dependency-free (no font face needed) since these captions are just
// index labels under each atlas cell, not prose.
func drawCaption(img *image.RGBA, s string, x, y int) {
	for i := 0; i < len(s); i++ {
		drawGlyph3x5(img, s[i], x+i*4, y, color.RGBA{0xD0, 0xD0, 0xD8, 0xFF})
	}
}

var glyph3x5 = map[byte][5]byte{
	'0': {0b111, 0b101, 0b101, 0b101, 0b111},
	'1': {0b010, 0b110, 0b010, 0b010, 0b111},
	'2': {0b111, 0b001, 0b111, 0b100, 0b111},
	'3': {0b111, 0b001, 0b111, 0b001, 0b111},
	'4': {0b101, 0b101, 0b111, 0b001, 0b001},
	'5': {0b111, 0b100, 0b111, 0b001, 0b111},
	'6': {0b111, 0b100, 0b111, 0b101, 0b111},
	'7': {0b111, 0b001, 0b010, 0b010, 0b010},
	'8': {0b111, 0b101, 0b111, 0b101, 0b111},
	'9': {0b111, 0b101, 0b111, 0b001, 0b111},
	'K': {0b101, 0b101, 0b110, 0b101, 0b101},
	'#': {0b101, 0b111, 0b101, 0b111, 0b101},
	'-': {0b000, 0b000, 0b111, 0b000, 0b000},
	'>': {0b100, 0b010, 0b001, 0b010, 0b100},
}

func drawGlyph3x5(img *image.RGBA, ch byte, x, y int, c color.RGBA) {
	rows, ok := glyph3x5[ch]
	if !ok {
		return
	}
	for row := 0; row < 5; row++ {
		for col := 0; col < 3; col++ {
			if rows[row]&(0b100>>uint(col)) != 0 {
				img.Set(x+col, y+row, c)
			}
		}
	}
}

// buildTrafficObjectAtlas renders each of the 9 traffic-object "kinds"
// (0-8, the value stored in a staged object record's own byte 0 -- see
// stageObjectRecord/spawnStagedTrafficObject in gameplay.go) as the actual
// colored 16x16 sprite pair it resolves to at runtime.
func buildTrafficObjectAtlas(tile func(byte) [32]byte) asset {
	img := atlasGrid(len(trafficSpritePatternByType), 9, 16, 16,
		color.RGBA{40, 40, 48, 255},
		func(i int, dst *image.RGBA, x0, y0 int) {
			p := trafficSpritePatternByType[i]
			cols := metaTileColors[i]
			pixels := composeOverlaySprite(tile(p), tile(p+4), cols[0], cols[1], color.RGBA{40, 40, 48, 255})
			for y := 0; y < 16; y++ {
				for x := 0; x < 16; x++ {
					dst.Set(x0+x, y0+y, pixels[y][x])
				}
			}
		},
		func(i int) string { return fmt.Sprintf("K%d", i) },
	)
	return asset{
		name:     "atlas_traffic_object_kinds",
		prebuilt: img,
		desc:     "synthesized -- 9 traffic-object kinds (0-8), each the real colored 16x16 sprite pair from trafficSpritePatternByType+metaTileColors (gameplay.go)",
	}
}

// buildPlayerCarAtlas renders all 11 carSpritePatternTable entries (the
// player's own steering/crash-spin sprite selector, $E053) as the colored
// sprite pair armPlayerCarSprites actually arms, using the two fixed
// colors carInitTable10E sets once at race init.
func buildPlayerCarAtlas(tile func(byte) [32]byte) asset {
	img := atlasGrid(len(carSpritePatternTable), 11, 16, 16,
		color.RGBA{40, 40, 48, 255},
		func(i int, dst *image.RGBA, x0, y0 int) {
			p := carSpritePatternTable[i]
			pixels := composeOverlaySprite(tile(p), tile(p+4), playerCarColors[0], playerCarColors[1], color.RGBA{40, 40, 48, 255})
			for y := 0; y < 16; y++ {
				for x := 0; x < 16; x++ {
					dst.Set(x0+x, y0+y, pixels[y][x])
				}
			}
		},
		func(i int) string { return fmt.Sprintf("#%d", i) },
	)
	return asset{
		name:     "atlas_player_car_steering",
		prebuilt: img,
		desc:     "synthesized -- 11 player car sprites ($E053 selector) from carSpritePatternTable, fixed colors $01/$06 (gameplay.go)",
	}
}

// buildMetaTileAtlas renders each of the 15 level-layout "meta tile" IDs
// (the values stageMetaTileIDs actually places in a stage's own 16-byte
// slice) as its two spawned-object halves side by side, each half drawn
// exactly as buildTrafficObjectAtlas draws its own kind.
func buildMetaTileAtlas(tile func(byte) [32]byte) asset {
	bg := color.RGBA{40, 40, 48, 255}
	drawHalf := func(dst *image.RGBA, x0, y0 int, kind byte) {
		p := trafficSpritePatternByType[kind]
		cols := metaTileColors[kind]
		pixels := composeOverlaySprite(tile(p), tile(p+4), cols[0], cols[1], bg)
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				dst.Set(x0+x, y0+y, pixels[y][x])
			}
		}
	}
	img := atlasGrid(len(metaTileDefinitions), 5, 34, 16, bg,
		func(i int, dst *image.RGBA, x0, y0 int) {
			def := metaTileDefinitions[i]
			drawHalf(dst, x0, y0, def[0])
			drawHalf(dst, x0+18, y0, def[3])
		},
		func(i int) string { return fmt.Sprintf("%d", i+1) },
	)
	return asset{
		name:     "atlas_meta_tile_definitions",
		prebuilt: img,
		desc:     "synthesized -- 15 meta-tile IDs (as used by stageMetaTileIDs), each its two spawned-object halves side by side (gameplay.go)",
	}
}
