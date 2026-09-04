// Command gfxviewer is an interactive Ebitengine viewer for the graphics
// assets extracted from the ROM (see assets/gfx/ and docs/ANALYSIS.md). It
// loads every *.bin file in a directory (assets/gfx by default), renders
// each as a grid of tiles using the same interpretation as cmd/gfxpreview,
// and lets you step through them one at a time with the filename shown on
// screen.
//
// A handful of assets are MSX Screen 2 pattern/color pairs (see
// colorPairs below and docs/ANALYSIS.md's "VRAM memory map" section); for
// those, press C to render with their real color-table colors instead of
// a flat monochrome tile, via internal/msxgfx.
//
// Appended after the directory's own files are three synthesized "atlas"
// entries with no backing *.bin at all (see atlas.go): every traffic
// object "kind" and the player's own car/crash-spin sprites, each as the
// actual colored 16x16 sprite pair the ROM's own lookup tables resolve
// them to, plus every level-layout "meta tile" ID as the object pair it
// spawns. These make internal/game/gameplay.go's trafficSpritePatternByType/
// metaTileColors/carSpritePatternTable/metaTileDefinitions tables -- data
// with no standalone visual form of their own -- actually visible.
//
// Controls:
//
//	Right / Space / Enter / N   next asset
//	Left / Backspace / P        previous asset
//	T                           toggle tile size (8x8 <-> 16x16)
//	L                           at 16x16, toggle layout (hardware sprite <-> big-font)
//	G                           toggle the grid line between tiles
//	C                           toggle color (only for known pattern/color pairs)
//	Esc / Q                     quit
package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/basicfont"

	"go-msx-racer/internal/msxgfx"
)

const (
	screenWidth  = 960
	screenHeight = 720
	defaultCols  = 16
)

// colorPairs maps a pattern asset's base filename (no directory, no
// .bin extension) to its confirmed MSX Screen 2 color-table companion,
// both decoded from RLE streams whose VRAM destinations share the same
// low-order offset -- see "VRAM memory map" in docs/ANALYSIS.md for how
// these were identified and confirmed by rendering.
var colorPairs = map[string]string{
	"hud_pattern_speed_fuel_panel_5DEC": "hud_color_speed_fuel_panel_5F25",
	"hud_pattern_gauge_bars1_5FEC":      "hud_color_gauge_bars1_601D",
	"hud_pattern_gauge_bars2_5FF7":      "hud_color_gauge_bars2_6022",
	"hud_pattern_gauge_bars3_6008":      "hud_color_gauge_bars3_602D",
	"ui_pattern_diagonal_border_63AC":   "ui_color_diagonal_border_67C2",
	"scenery_pattern_tree_bush_6645":    "scenery_color_tree_bush_6737",
	"scenery_pattern_tree_road_6697":    "scenery_color_tree_road_675E",
	"map_pattern_terrain_tileset_74BC":  "map_color_terrain_tileset_7638",

	// These two patterns aren't independent ROM data: they're derived by
	// the game itself at runtime via the VRAM-mirroring routine at $4652
	// (a per-byte bit-reversal, i.e. a horizontal flip) applied to a
	// subrange of an already-decoded pattern. Saved here as their own
	// files purely for viewer convenience -- see docs/ANALYSIS.md's
	// "Pattern/color pairs" section for the derivation and which source
	// bytes they came from.
	"hud_pattern_gauge_bars1_mirrored_601F": "hud_color_gauge_bars1_mirrored_601F",
	"hud_pattern_gauge_bars3_mirrored_6031": "hud_color_gauge_bars3_mirrored_6031",

	// Three more runtime-mirrored patterns (same $4652 routine, same
	// per-byte bit-reversal, this time over an entire source file rather
	// than a subrange -- see internal/game/graphics.go's
	// initHUDAndTrackGraphics/drawTrackBorderAndTreeScenery call sites:
	// mirrorPatternRows(patBase, 0x6C8, 0x708, 8), (0x400, 0x588, 24), and
	// (0x400, 0x468, 13) respectively). Unlike the gauge1/3 pair above,
	// the ROM reuses each of these three's own *normal* color asset
	// unchanged for the mirrored half (confirmed at those same call
	// sites: "$68FF-$6907: same color asset, reused" and the two
	// "placed again" comments in drawTrackBorderAndTreeScenery) -- so
	// there's no separate mirrored-color file to pair here, and no
	// _computed color file needed either.
	"hud_pattern_gauge_bars2_mirrored_computed":    "hud_color_gauge_bars2_6022",
	"ui_pattern_diagonal_border_mirrored_computed": "ui_color_diagonal_border_67C2",
	"scenery_pattern_tree_bush_mirrored_computed":  "scenery_color_tree_bush_6737",
}

// bigCharAssets are 16x16 tiles known to use the background-tile "big
// font" layout (TL,TR,BL,BR raster order) rather than the MSX1 hardware
// sprite layout (TL,BL,TR,BR) -- see internal/msxgfx.BigChar16Mono and
// docs/ANALYSIS.md. Defaults to sprite layout for anything not listed here.
var bigCharAssets = map[string]bool{
	"bigfont_pattern_road_4A25": true,
}

type asset struct {
	name string // base filename, no extension
	data []byte

	// prebuilt is set only for synthesized entries that don't correspond
	// to a single *.bin file at all -- the traffic-object/player-car/
	// meta-tile atlases built by buildAtlases (see atlas.go). When set,
	// rebuild() uses this image directly instead of running data through
	// renderTilesMono/renderTilesColor, and the tile-size/layout/color
	// toggles are reported as not applicable.
	prebuilt *ebiten.Image
	desc     string // subtitle text shown in place of the byte/tile-count line, when prebuilt is set
}

type game struct {
	assets   []asset
	byName   map[string]int
	index    int
	tileSize int  // 8 or 16
	bigChar  bool // at tileSize 16: false = hardware sprite layout, true = big-font layout
	showGrid bool
	colorize bool
	tileImg  *ebiten.Image
	face     *text.GoXFace
}

func main() {
	dir := "assets/gfx"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.bin"))
	if err != nil {
		log.Fatal(err)
	}
	if len(matches) == 0 {
		log.Fatalf("no .bin files found in %s", dir)
	}
	sort.Strings(matches)

	assets := make([]asset, 0, len(matches))
	byName := make(map[string]int, len(matches))
	for _, p := range matches {
		b, err := os.ReadFile(p)
		if err != nil {
			log.Fatal(err)
		}
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		byName[base] = len(assets)
		assets = append(assets, asset{name: base, data: b})
	}

	for _, a := range buildAtlases(byName, assets) {
		byName[a.name] = len(assets)
		assets = append(assets, a)
	}

	g := &game{
		assets:   assets,
		byName:   byName,
		tileSize: 8,
		showGrid: true,
		colorize: true,
		face:     text.NewGoXFace(basicfont.Face7x13),
	}
	g.selectAsset(0)

	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("go-msx-racer -- extracted graphics viewer")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}

// colorPartner returns the color-table companion's byte data for the
// current asset, and true, if the current asset is on either end of a
// known pattern/color pair (colorPairs, checked in both directions).
func (g *game) colorPartner() ([]byte, bool) {
	if g.assets[g.index].prebuilt != nil {
		return nil, false
	}
	name := g.assets[g.index].name
	if colorName, ok := colorPairs[name]; ok {
		if i, ok := g.byName[colorName]; ok {
			return g.assets[i].data, true
		}
	}
	for patName, colorName := range colorPairs {
		if colorName == name {
			if i, ok := g.byName[patName]; ok {
				return g.assets[i].data, true
			}
		}
	}
	return nil, false
}

func (g *game) rebuild() {
	if p := g.assets[g.index].prebuilt; p != nil {
		g.tileImg = p
		return
	}
	colorData, hasColor := g.colorPartner()
	if g.colorize && hasColor {
		g.tileImg = renderTilesColor(g.assets[g.index].data, colorData, g.tileSize, defaultCols, g.showGrid, g.bigChar)
	} else {
		g.tileImg = renderTilesMono(g.assets[g.index].data, g.tileSize, defaultCols, g.showGrid, g.bigChar)
	}
}

func (g *game) selectAsset(i int) {
	g.index = i
	g.bigChar = bigCharAssets[g.assets[g.index].name] // auto-detect default layout; L overrides
	g.rebuild()
}

func (g *game) next() { g.selectAsset((g.index + 1) % len(g.assets)) }
func (g *game) prev() { g.selectAsset((g.index - 1 + len(g.assets)) % len(g.assets)) }

func (g *game) Update() error {
	for _, k := range []ebiten.Key{ebiten.KeyRight, ebiten.KeySpace, ebiten.KeyEnter, ebiten.KeyN} {
		if inpututil.IsKeyJustPressed(k) {
			g.next()
			break
		}
	}
	for _, k := range []ebiten.Key{ebiten.KeyLeft, ebiten.KeyBackspace, ebiten.KeyP} {
		if inpututil.IsKeyJustPressed(k) {
			g.prev()
			break
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyT) {
		if g.tileSize == 8 {
			g.tileSize = 16
		} else {
			g.tileSize = 8
		}
		g.rebuild()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyG) {
		g.showGrid = !g.showGrid
		g.rebuild()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyL) {
		g.bigChar = !g.bigChar
		g.rebuild()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyC) {
		g.colorize = !g.colorize
		g.rebuild()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyQ) {
		return ebiten.Termination
	}
	return nil
}

var (
	colBG       = color.RGBA{0x18, 0x18, 0x20, 0xFF}
	colTitle    = color.White
	colSubtitle = color.RGBA{0xAA, 0xAA, 0xB4, 0xFF}
	colFooter   = color.RGBA{0x78, 0x78, 0x82, 0xFF}
)

func (g *game) Draw(screen *ebiten.Image) {
	screen.Fill(colBG)

	a := g.assets[g.index]
	title := fmt.Sprintf("[%d/%d] %s", g.index+1, len(g.assets), a.name)

	var subtitle, footer string
	if a.prebuilt != nil {
		subtitle = a.desc
		footer = "<-/-> or N/P: navigate    Esc/Q: quit"
	} else {
		tileCount := len(a.data) / tileByteSize(g.tileSize)
		gridState := "on"
		if !g.showGrid {
			gridState = "off"
		}
		_, hasColor := g.colorPartner()
		colorState := "n/a"
		if hasColor {
			if g.colorize {
				colorState = "on"
			} else {
				colorState = "off (available)"
			}
		}
		layoutState := ""
		if g.tileSize == 16 {
			if g.bigChar {
				layoutState = "  --  layout bigchar"
			} else {
				layoutState = "  --  layout sprite"
			}
		}
		subtitle = fmt.Sprintf("%d bytes  --  %d tiles at %dx%d  --  grid %s  --  color %s%s", len(a.data), tileCount, g.tileSize, g.tileSize, gridState, colorState, layoutState)
		footer = "<-/-> or N/P: navigate    T: tile size    L: layout (16x16)    G: grid    C: color    Esc/Q: quit"
	}

	drawText(screen, title, g.face, 16, 30, colTitle)
	drawText(screen, subtitle, g.face, 16, 50, colSubtitle)
	drawText(screen, footer, g.face, 16, screenHeight-14, colFooter)

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(3, 3)
	op.GeoM.Translate(16, 72)
	screen.DrawImage(g.tileImg, op)
}

func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func drawText(dst *ebiten.Image, s string, face *text.GoXFace, x, y int, clr color.Color) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(clr)
	text.Draw(dst, s, face, op)
}

func tileByteSize(tileSize int) int {
	if tileSize == 16 {
		return 32
	}
	return 8
}

// grid draws a background of either a grid-line color (showGrid) or the
// plain tile background color, sized for `cols`x`rows` tiles of side
// `tileSize` with a 1px gap when showGrid is set, and returns the
// resulting image along with the pixel stride per tile.
func grid(cols, rows, tileSize int, showGrid bool) (*image.RGBA, int) {
	gap := 0
	if showGrid {
		gap = 1
	}
	stride := tileSize + gap
	img := image.NewRGBA(image.Rect(0, 0, cols*stride, rows*stride))
	fill := bgColor
	if showGrid {
		fill = gridLineColor
	}
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, fill)
		}
	}
	return img, stride
}

var (
	bgColor       = color.RGBA{40, 40, 48, 255}
	fgColor       = color.RGBA{240, 220, 120, 255}
	gridLineColor = color.RGBA{90, 90, 100, 255}
)

// renderTilesMono renders `data` as a grid of `tileSize`x`tileSize`
// monochrome (1bpp) tiles. An 8x8 tile is 8 bytes (one per row); 16x16 is
// 32 bytes in one of two layouts depending on bigChar -- hardware sprite
// quadrants (top-left, bottom-left, top-right, bottom-right; see
// internal/msxgfx.Sprite16Mono) or background-tile "big font" quadrants
// (top-left, top-right, bottom-left, bottom-right; see BigChar16Mono).
// Trailing bytes that don't fill a whole tile are dropped.
func renderTilesMono(data []byte, tileSize, cols int, showGrid, bigChar bool) *ebiten.Image {
	tileBytes := tileByteSize(tileSize)

	numTiles := len(data) / tileBytes
	rows := 1
	if numTiles > 0 {
		rows = (numTiles + cols - 1) / cols
	}
	img, stride := grid(cols, rows, tileSize, showGrid)

	for t := 0; t < numTiles; t++ {
		originX := (t % cols) * stride
		originY := (t / cols) * stride
		for y := originY; y < originY+tileSize; y++ {
			for x := originX; x < originX+tileSize; x++ {
				img.Set(x, y, bgColor)
			}
		}

		tileData := data[t*tileBytes : (t+1)*tileBytes]
		if tileSize == 16 {
			var tile [32]byte
			copy(tile[:], tileData)
			var pixels [16][16]color.RGBA
			if bigChar {
				pixels = msxgfx.BigChar16Mono(tile, fgColor, bgColor)
			} else {
				pixels = msxgfx.Sprite16Mono(tile, fgColor, bgColor)
			}
			for row := 0; row < 16; row++ {
				for col := 0; col < 16; col++ {
					if pixels[row][col] != bgColor {
						img.Set(originX+col, originY+row, pixels[row][col])
					}
				}
			}
			continue
		}

		var pat [8]byte
		copy(pat[:], tileData)
		pixels := msxgfx.TileMono(pat, fgColor, bgColor)
		for row := 0; row < 8; row++ {
			for bit := 0; bit < 8; bit++ {
				if pixels[row][bit] != bgColor {
					img.Set(originX+bit, originY+row, pixels[row][bit])
				}
			}
		}
	}

	return ebiten.NewImageFromImage(img)
}

// renderTilesColor renders `pattern` as a grid of 8x8 MSX Screen 2 tiles
// composited with matching per-row colors from `colorData` (8 bytes per
// tile, see internal/msxgfx). Only 8x8 is meaningful here -- MSX per-row
// color tables don't apply to 16x16 sprite patterns, which are single
// color via the runtime sprite attribute table instead.
func renderTilesColor(pattern, colorData []byte, tileSize, cols int, showGrid, bigChar bool) *ebiten.Image {
	if tileSize != 8 {
		return renderTilesMono(pattern, tileSize, cols, showGrid, bigChar)
	}

	numTiles := len(pattern) / 8
	if n := len(colorData) / 8; n < numTiles {
		numTiles = n
	}
	rows := 1
	if numTiles > 0 {
		rows = (numTiles + cols - 1) / cols
	}
	img, stride := grid(cols, rows, tileSize, showGrid)

	for t := 0; t < numTiles; t++ {
		originX := (t % cols) * stride
		originY := (t / cols) * stride

		var pat, colr [8]byte
		copy(pat[:], pattern[t*8:t*8+8])
		copy(colr[:], colorData[t*8:t*8+8])
		// No live VDP here -- color index 0 means "show the backdrop" on
		// real hardware (see TileRGBA's doc comment), so fall back to
		// this preview's own background rather than rendering it black.
		pixels := msxgfx.TileRGBA(pat, colr, bgColor)
		for row := 0; row < 8; row++ {
			for bit := 0; bit < 8; bit++ {
				img.Set(originX+bit, originY+row, pixels[row][bit])
			}
		}
	}

	return ebiten.NewImageFromImage(img)
}
