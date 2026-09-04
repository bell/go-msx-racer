// Command gfxpreview renders a byte range of the ROM as a grid of tiles, to
// help visually classify suspected graphics data blocks (MSX Screen 2
// pattern-generator tiles are 8 bytes/8x8px; 16x16 data is 32 bytes and
// comes in at least two different layouts in this ROM -- hardware sprites
// (four 8x8 quadrants in TL,BL,TR,BR order) and background-tile "big font"
// characters (TL,TR,BL,BR order) -- select with -layout; see
// internal/msxgfx.Sprite16Mono / BigChar16Mono).
//
// By default tiles render monochrome (any set pattern bit in one fixed
// color). Pass -color to additionally load a matching MSX Screen 2 color
// table (8 bytes per 8x8 tile, one byte per row: high nibble = foreground,
// low nibble = background) and render each tile in its real colors -- see
// internal/msxgfx and docs/ANALYSIS.md ("Compression format" / VRAM map)
// for how pattern and color data are told apart in this ROM.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"

	"go-msx-racer/internal/msxgfx"
)

func main() {
	romPath := flag.String("rom", "", "path to ROM file")
	startStr := flag.String("start", "", "start address (hex, e.g. 4A25)")
	length := flag.Int("len", 256, "number of bytes to render")
	tileBytes := flag.Int("tile", 8, "bytes per tile row-group: 8 = 8x8 1bpp tile, 32 = 16x16 1bpp sprite/bigchar")
	layout := flag.String("layout", "sprite", "for -tile 32: \"sprite\" (hardware sprite quadrant order: TL,BL,TR,BR) or \"bigchar\" (background-tile big-font order: TL,TR,BL,BR)")
	cols := flag.Int("cols", 16, "tiles per row in the output grid")
	scale := flag.Int("scale", 4, "pixel scale factor")
	out := flag.String("out", "preview.png", "output PNG path")
	colorPath := flag.String("color", "", "optional: path to a matching decoded MSX color-table file (8x8 tiles only) to render in real colors instead of monochrome")
	flag.Parse()

	if *tileBytes == 32 && *layout != "sprite" && *layout != "bigchar" {
		fmt.Fprintln(os.Stderr, `-layout must be "sprite" or "bigchar"`)
		os.Exit(1)
	}

	data, err := os.ReadFile(*romPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	const baseAddr = 0x4000
	startAddr, err := strconv.ParseInt(*startStr, 16, 32)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad -start:", err)
		os.Exit(1)
	}
	off := int(startAddr) - baseAddr
	end := off + *length
	if end > len(data) {
		end = len(data)
	}
	chunk := data[off:end]

	var colorData []byte
	if *colorPath != "" {
		if *tileBytes != 8 {
			fmt.Fprintln(os.Stderr, "-color only supported with -tile 8")
			os.Exit(1)
		}
		colorData, err = os.ReadFile(*colorPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	tileW, tileH := 8, 8
	if *tileBytes == 32 {
		tileW, tileH = 16, 16
	}

	numTiles := len(chunk) / *tileBytes
	if numTiles == 0 {
		fmt.Fprintln(os.Stderr, "no full tiles in range")
		os.Exit(1)
	}
	if colorData != nil && len(colorData)/8 < numTiles {
		numTiles = len(colorData) / 8
	}
	rows := (numTiles + *cols - 1) / *cols

	imgW := *cols * (tileW + 1) * *scale
	imgH := rows * (tileH + 1) * *scale
	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	bg := color.RGBA{40, 40, 48, 255}
	fg := color.RGBA{240, 220, 120, 255}
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			img.Set(x, y, bg)
		}
	}

	putPixel := func(tx, ty, px, py int, c color.Color) {
		ox := (tx*(tileW+1) + px) * *scale
		oy := (ty*(tileH+1) + py) * *scale
		for sy := 0; sy < *scale; sy++ {
			for sx := 0; sx < *scale; sx++ {
				img.Set(ox+sx, oy+sy, c)
			}
		}
	}

	for t := 0; t < numTiles; t++ {
		tx := t % *cols
		ty := t / *cols
		tileData := chunk[t**tileBytes : (t+1)**tileBytes]

		if colorData != nil {
			var pat, colr [8]byte
			copy(pat[:], tileData)
			copy(colr[:], colorData[t*8:t*8+8])
			// No live VDP here (this tool previews raw asset bytes, not a
			// running system) -- color index 0 means "show the backdrop"
			// on real hardware (see TileRGBA's doc comment), so fall back
			// to this preview canvas's own background rather than
			// rendering it as black.
			pixels := msxgfx.TileRGBA(pat, colr, bg)
			for row := 0; row < 8; row++ {
				for bit := 0; bit < 8; bit++ {
					putPixel(tx, ty, bit, row, pixels[row][bit])
				}
			}
			continue
		}

		if *tileBytes == 32 {
			var tile [32]byte
			copy(tile[:], tileData)
			var pixels [16][16]color.RGBA
			if *layout == "bigchar" {
				pixels = msxgfx.BigChar16Mono(tile, fg, bg)
			} else {
				pixels = msxgfx.Sprite16Mono(tile, fg, bg)
			}
			for row := 0; row < 16; row++ {
				for col := 0; col < 16; col++ {
					putPixel(tx, ty, col, row, pixels[row][col])
				}
			}
			continue
		}

		var pat [8]byte
		copy(pat[:], tileData)
		pixels := msxgfx.TileMono(pat, fg, bg)
		for row := 0; row < 8; row++ {
			for bit := 0; bit < 8; bit++ {
				putPixel(tx, ty, bit, row, pixels[row][bit])
			}
		}
	}

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	png.Encode(f, img)
	fmt.Printf("wrote %s (%d tiles, %dx%d each)\n", *out, numTiles, tileW, tileH)
}
