// Command extract rebuilds everything under assets/ from a copy of the
// cartridge ROM. Run it once, after putting your own ROM at the repo
// root, and cmd/play (and gfxviewer, sndpreview, gfxpreview) will work:
//
//	go run ./cmd/extract
//
// The extracted assets are not distributed with this repo -- they are
// Konami's data, just decompressed -- so assets/*.bin is gitignored and
// this tool is how you get it. Everything it writes is byte-for-byte what
// the project was developed against; -check verifies that without writing.
//
// This tool deliberately does NOT import the assets package. That package
// //go:embeds the very files this tool produces, so importing it would
// make the tool impossible to build on a fresh clone -- exactly when it is
// needed most.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"go-msx-racer/internal/rle"
)

// romSHA256 is the dump every address in this project was traced against
// (see docs/ANALYSIS.md's "ROM used"). A different dump may still extract
// cleanly and be subtly wrong, so a mismatch is reported loudly.
const romSHA256 = "9a3bbfabdc2b089790b9549dfe503a8d250925ed091517de078beddd627d2888"

// romBase is where the cartridge is mapped in the Z80 address space, so a
// ROM address $XXXX is at file offset $XXXX-$4000.
const romBase = 0x4000

var (
	romPath = flag.String("rom", "Road Fighter - Konami (1985) [7349].rom", "path to the cartridge ROM")
	outDir  = flag.String("out", "assets", "directory to write the extracted assets into")
	check   = flag.Bool("check", false, "verify the existing files match what would be extracted, without writing anything")
)

// kind is how one asset is recovered from the ROM.
type kind int

const (
	// decoded is one of the ROM's RLE streams (internal/rle), decoded from
	// addr. These are what the original feeds through $45E0/$4611/$460B.
	decoded kind = iota
	// raw is a plain ROM byte range -- data the original reads directly,
	// with no decompressor anywhere on its code path.
	raw
	// mirrorRows horizontally flips each byte of a slice of an
	// already-extracted asset: the port's mirrorPatternRows ($4652), which
	// the original runs at load time to make the right-hand half of
	// symmetric 8x8 tile art.
	mirrorRows
	// mirrorSprite16 is the 16x16 hardware-sprite flip ($4636): a byte
	// reversal plus the i^16 quadrant-pair swap, since a 32-byte sprite is
	// four 8x8 quadrants in TL/BL/TR/BR order, not 16 plain rows.
	mirrorSprite16
)

type asset struct {
	path string // relative to -out
	kind kind
	addr int    // decoded/raw: ROM address
	n    int    // raw: byte count
	from string // mirror*: source asset path
	lo   int    // mirror*: source slice bounds
	hi   int
}

// manifest is the complete contents of assets/. Order matters only in that
// a mirror must come after the asset it reads.
//
// Every entry was confirmed by extracting it and comparing byte-for-byte
// against the file the project already had, not by reading it off the
// filename -- two of the mirrors are named for a ROM address that is a
// real RLE stream decoding to something else entirely, which would have
// produced plausible, wrong output.
var manifest = []asset{
	// --- font, tables ---
	{path: "gfx/font_8x8.bin", kind: decoded, addr: 0x478A},
	{path: "vdp_init_registers.bin", kind: raw, addr: 0x46A9, n: 8},
	{path: "sfx/note_table_and_sound_scripts_4DD6.bin", kind: raw, addr: 0x4DD6, n: 594},
	{path: "sfx/note_pitch_table.bin", kind: raw, addr: 0x4DD6, n: 13},

	// --- boot / title ---
	{path: "gfx/logo_pattern_konami_490F.bin", kind: decoded, addr: 0x490F},
	{path: "gfx/bigfont_pattern_road_4A25.bin", kind: decoded, addr: 0x4A25},

	// --- sprites ---
	{path: "gfx/sprites_vehicles_5A73.bin", kind: decoded, addr: 0x5A73},
	{path: "gfx/sprites_crash_debris_bonus_5B97.bin", kind: decoded, addr: 0x5B97},

	// --- HUD panel and gauge bars ---
	{path: "gfx/hud_pattern_speed_fuel_panel_5DEC.bin", kind: decoded, addr: 0x5DEC},
	{path: "gfx/hud_color_speed_fuel_panel_5F25.bin", kind: decoded, addr: 0x5F25},
	{path: "gfx/hud_pattern_gauge_bars1_5FEC.bin", kind: decoded, addr: 0x5FEC},
	{path: "gfx/hud_pattern_gauge_bars2_5FF7.bin", kind: decoded, addr: 0x5FF7},
	{path: "gfx/hud_pattern_gauge_bars3_6008.bin", kind: decoded, addr: 0x6008},
	{path: "gfx/hud_color_gauge_bars1_601D.bin", kind: decoded, addr: 0x601D},
	{path: "gfx/hud_color_gauge_bars1_mirrored_601F.bin", kind: decoded, addr: 0x601F},
	{path: "gfx/hud_color_gauge_bars2_6022.bin", kind: decoded, addr: 0x6022},
	{path: "gfx/hud_color_gauge_bars3_602D.bin", kind: decoded, addr: 0x602D},
	{path: "gfx/hud_color_gauge_bars3_mirrored_6031.bin", kind: decoded, addr: 0x6031},

	// --- per-stage scenery ---
	{path: "gfx/scenery_pattern_stage1_terrain_603E.bin", kind: decoded, addr: 0x603E},
	{path: "gfx/scenery_color_stage1_terrain_61C8.bin", kind: decoded, addr: 0x61C8},
	{path: "gfx/scenery_pattern_stage2_terrain_6279.bin", kind: decoded, addr: 0x6279},
	{path: "gfx/scenery_color_stage2_terrain_6431.bin", kind: decoded, addr: 0x6431},
	{path: "gfx/scenery_pattern_stage3_terrain_64CA.bin", kind: decoded, addr: 0x64CA},
	{path: "gfx/scenery_color_stage3_terrain_65D7.bin", kind: decoded, addr: 0x65D7},
	{path: "gfx/scenery_pattern_tree_bush_6645.bin", kind: decoded, addr: 0x6645},
	{path: "gfx/scenery_color_tree_bush_6737.bin", kind: decoded, addr: 0x6737},
	{path: "gfx/scenery_pattern_tree_road_6697.bin", kind: decoded, addr: 0x6697},
	{path: "gfx/scenery_color_tree_road_675E.bin", kind: decoded, addr: 0x675E},
	{path: "gfx/scenery_pattern_stage6_terrain_67D5.bin", kind: decoded, addr: 0x67D5},
	{path: "gfx/scenery_color_stage6_terrain_6885.bin", kind: decoded, addr: 0x6885},

	// --- UI border, inter-stage map ---
	{path: "gfx/ui_pattern_diagonal_border_63AC.bin", kind: decoded, addr: 0x63AC},
	{path: "gfx/ui_color_diagonal_border_67C2.bin", kind: decoded, addr: 0x67C2},
	{path: "gfx/map_pattern_terrain_tileset_74BC.bin", kind: decoded, addr: 0x74BC},
	{path: "gfx/map_color_terrain_tileset_7638.bin", kind: decoded, addr: 0x7638},

	// --- unidentified byte ranges, kept as visually-inspected guesses
	// (docs/ANALYSIS.md open issue 9); raw slices, not decoded streams ---
	{path: "gfx/tiles_candidate_5998.bin", kind: raw, addr: 0x5998, n: 199},
	{path: "gfx/data_candidate_5E2C.bin", kind: raw, addr: 0x5E2C, n: 410},
	{path: "gfx/tiles_candidate_62D0.bin", kind: raw, addr: 0x62D0, n: 144},
	{path: "gfx/tiles_candidate_637C.bin", kind: raw, addr: 0x637C, n: 148},
	{path: "gfx/tiles_candidate_6479.bin", kind: raw, addr: 0x6479, n: 346},
	{path: "gfx/tiles_candidate_785B.bin", kind: raw, addr: 0x785B, n: 105},

	// --- data tables the port reads directly rather than blitting to
	// VRAM: road curve/feature streams, the track-record blocks, the
	// title reveal script, the inter-stage map rows. These used to sit in
	// internal/game as Go byte-array literals, i.e. ROM data transcribed
	// verbatim into committed source; they are extracted here instead so
	// the repository holds none of it. All are plain ROM ranges -- the
	// original reads them with ordinary loads, no decompressor involved.
	//
	// Two overlap graphics assets above, which is not a mistake: $785B is
	// byte-for-byte tiles_candidate_785B.bin (the same 105 bytes, reached
	// by two different routines), and $5998 starts where
	// tiles_candidate_5998.bin does but runs 18 bytes further. The
	// candidates are kept under their own names because cmd/gfxviewer
	// lists them as unidentified graphics guesses (ANALYSIS open issue 9).
	{path: "data/title_reveal_table_4AFC.bin", kind: raw, addr: 0x4AFC, n: 114},
	{path: "data/road_curve_array_5998.bin", kind: raw, addr: 0x5998, n: 217},
	{path: "data/stage_map_rows_76EC.bin", kind: raw, addr: 0x76EC, n: 105},
	{path: "data/track_record_data_785B.bin", kind: raw, addr: 0x785B, n: 105},
	{path: "data/road_feature_stream_stage145_78E5.bin", kind: raw, addr: 0x78E5, n: 123},
	{path: "data/road_feature_stream_stage23_7BAA.bin", kind: raw, addr: 0x7BAA, n: 123},
	{path: "data/road_strip_entry3_7CB1.bin", kind: raw, addr: 0x7CB1, n: 203},
	{path: "data/road_feature_stream_stage6_7F6B.bin", kind: raw, addr: 0x7F6B, n: 131},

	// refreshRoadCurvePhase's six per-stage curve-phase streams. Stages 2
	// and 3 genuinely share one stream (same address, same bytes), which
	// is why there are five files for six stages -- matching
	// roadFeatureStageRecords' own identical record for those two.
	{path: "data/road_curve_phase_stage1_78C4.bin", kind: raw, addr: 0x78C4, n: 34},
	{path: "data/road_curve_phase_stage23_7AAA.bin", kind: raw, addr: 0x7AAA, n: 36},
	{path: "data/road_curve_phase_stage4_7E83.bin", kind: raw, addr: 0x7E83, n: 38},
	{path: "data/road_curve_phase_stage5_7E60.bin", kind: raw, addr: 0x7E60, n: 30},
	{path: "data/road_curve_phase_stage6_7EA8.bin", kind: raw, addr: 0x7EA8, n: 42},

	// --- mirrors the original generates at load time rather than storing.
	// The three gauge-bar entries take a *slice*, matching their real call
	// sites in initHUDAndTrackGraphics ($68CE/$68F4/$691A); the others
	// mirror the whole asset. Two are named for a ROM address ($601F,
	// $6031) that is itself a real RLE stream holding the mirrored
	// *color*, which is why the pattern file at the same address cannot be
	// decoded from it. ---
	{path: "gfx/hud_pattern_gauge_bars1_mirrored_601F.bin", kind: mirrorRows,
		from: "gfx/hud_pattern_gauge_bars1_5FEC.bin", lo: 8, hi: 40}, // $68CE: 4 tiles
	{path: "gfx/hud_pattern_gauge_bars2_mirrored_computed.bin", kind: mirrorRows,
		from: "gfx/hud_pattern_gauge_bars2_5FF7.bin", lo: 0, hi: 64}, // $68F4: 8 tiles
	{path: "gfx/hud_pattern_gauge_bars3_mirrored_6031.bin", kind: mirrorRows,
		from: "gfx/hud_pattern_gauge_bars3_6008.bin", lo: 24, hi: 72}, // $691A: 6 tiles
	{path: "gfx/ui_pattern_diagonal_border_mirrored_computed.bin", kind: mirrorRows,
		from: "gfx/ui_pattern_diagonal_border_63AC.bin", lo: 0, hi: 192}, // $699E: 24 tiles
	{path: "gfx/scenery_pattern_tree_bush_mirrored_computed.bin", kind: mirrorRows,
		from: "gfx/scenery_pattern_tree_bush_6645.bin", lo: 0, hi: 104}, // $69BB: 13 tiles
	{path: "gfx/sprites_vehicles_mirrored_computed.bin", kind: mirrorSprite16,
		from: "gfx/sprites_vehicles_5A73.bin", lo: 128, hi: 320}, // $6934: 6 sprites
}

// mirrorByte reverses a byte's bits, the horizontal flip of one 8-pixel
// tile row.
func mirrorByte(b byte) byte {
	var out byte
	for i := 0; i < 8; i++ {
		out = out<<1 | (b>>uint(i))&1
	}
	return out
}

func build(rom []byte, produced map[string][]byte, a asset) ([]byte, error) {
	switch a.kind {
	case decoded:
		out, _, _ := rle.Decode(rom, romBase, a.addr, 8192)
		if len(out) == 0 {
			return nil, fmt.Errorf("stream at $%04X decoded to nothing", a.addr)
		}
		return out, nil

	case raw:
		off := a.addr - romBase
		if off < 0 || off+a.n > len(rom) {
			return nil, fmt.Errorf("$%04X+%d is outside the ROM", a.addr, a.n)
		}
		return append([]byte(nil), rom[off:off+a.n]...), nil

	case mirrorRows, mirrorSprite16:
		src, ok := produced[a.from]
		if !ok {
			return nil, fmt.Errorf("source %s has not been built yet", a.from)
		}
		if a.hi > len(src) {
			return nil, fmt.Errorf("source %s is %d bytes, need [%d:%d]", a.from, len(src), a.lo, a.hi)
		}
		src = src[a.lo:a.hi]
		out := make([]byte, len(src))
		if a.kind == mirrorRows {
			for i, b := range src {
				out[i] = mirrorByte(b)
			}
			return out, nil
		}
		for n := 0; n*32 < len(src); n++ {
			for i := 0; i < 32; i++ {
				out[n*32+(i^16)] = mirrorByte(src[n*32+i])
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown kind")
}

func main() {
	flag.Parse()

	rom, err := os.ReadFile(*romPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read the ROM: %v\n\n"+
			"Put your own copy of the cartridge at the repo root (it is not\n"+
			"distributed here), or pass -rom with its path.\n", err)
		os.Exit(1)
	}
	sum := sha256.Sum256(rom)
	if got := hex.EncodeToString(sum[:]); got != romSHA256 {
		fmt.Fprintf(os.Stderr, "warning: this ROM is not the dump the project was traced against\n"+
			"  want sha256 %s\n  got  sha256 %s\n"+
			"Extraction will continue, but addresses may not line up -- see\n"+
			"docs/ANALYSIS.md's \"ROM used\" section.\n\n", romSHA256, got)
	}

	produced := make(map[string][]byte, len(manifest))
	var written, mismatched, total int

	for _, a := range manifest {
		data, err := build(rom, produced, a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", a.path, err)
			os.Exit(1)
		}
		produced[a.path] = data
		total += len(data)

		full := filepath.Join(*outDir, a.path)
		if *check {
			have, err := os.ReadFile(full)
			switch {
			case err != nil:
				fmt.Printf("MISSING  %s\n", a.path)
				mismatched++
			case !bytes.Equal(have, data):
				fmt.Printf("DIFFERS  %s (on disk %d bytes, extracted %d)\n", a.path, len(have), len(data))
				mismatched++
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", a.path, err)
			os.Exit(1)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", a.path, err)
			os.Exit(1)
		}
		written++
	}

	if *check {
		if mismatched != 0 {
			fmt.Printf("\n%d of %d assets differ from the ROM\n", mismatched, len(manifest))
			os.Exit(1)
		}
		fmt.Printf("all %d assets match the ROM (%d bytes)\n", len(manifest), total)
		return
	}
	fmt.Printf("extracted %d assets (%d bytes) into %s/\n", written, total, *outDir)
	fmt.Println("you can now run: go run ./cmd/play")
}
