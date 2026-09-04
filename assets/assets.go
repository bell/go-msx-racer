// Package assets embeds the pre-decoded graphics/sound assets extracted
// from the ROM (see docs/ANALYSIS.md) so the Go port in internal/game can
// load them directly, without needing the original ROM file at runtime and
// without reimplementing the RLE decompressor (internal/rle) as part of
// the port itself -- that work already happened once, producing these
// files; the port just needs the result.
package assets

import _ "embed"

// FontPattern is the decoded 8x8 character font (43 glyphs: digits, ©, a
// couple of icons, A-Z with D/J/Q/U/X/Z left blank). Ported from the RLE
// stream at $478A; see assets/gfx/font_8x8.bin and docs/ANALYSIS.md.
//
//go:embed gfx/font_8x8.bin
var FontPattern []byte

// KonamiLogoPattern is the decoded "Konami®" boot-splash pattern (27
// tiles' worth of bytes, only the first 26 used). Ported from the RLE
// stream at $490F; see assets/gfx/logo_pattern_konami_490F.bin and
// docs/ANALYSIS.md's dedicated section on this asset.
//
//go:embed gfx/logo_pattern_konami_490F.bin
var KonamiLogoPattern []byte

// BigFontRoadTitlePattern is the decoded "ROAD..." big-font title pattern
// (33 tiles' worth of 8x8 background tiles, composed 4-at-a-time into
// 16x16 characters via internal/msxgfx.BigChar16Mono -- only the first 4
// groups are confirmed to read as "ROAD"; see docs/ANALYSIS.md's dedicated
// section on this asset for what's still unresolved about the rest).
// Ported from the RLE stream at $4A25.
//
//go:embed gfx/bigfont_pattern_road_4A25.bin
var BigFontRoadTitlePattern []byte

// The HUD speed/fuel panel, gauge-bar tiles (3 sets, each with a
// horizontally-mirrored companion), UI border, and tree scenery below are
// all loaded by internal/game.initHUDAndTrackGraphics/
// drawTrackBorderAndTreeScenery, ported from $68AA-$6A05 (see their doc
// comments in internal/game/graphics.go). Every one of these is a
// confirmed RLE stream reached via a literal `LD DE,<addr>h; CALL $45E0`
// in the listing.
//
// hud_pattern_gauge_bars1_mirrored_601F.bin and
// hud_pattern_gauge_bars3_mirrored_6031.bin are NOT embedded here and are
// not independent ROM streams at all, despite their filenames: $601F and
// $6031 are only ever placed at color-table destinations in the listing
// (re-decoding both directly from the ROM confirms the "_color_" versions
// here are their only real content). Their "_pattern_" counterparts are
// precomputed reconstructions -- the mirrored pattern tiles for gauge
// bars 1 and 3 are generated at runtime by bit-reversing the already-
// placed normal pattern (see mirrorPatternRows/$4652 below) -- kept on
// disk only for cmd/gfxviewer's standalone preview convenience (see its
// colorPairs map), not consumed by the running port.

//go:embed gfx/hud_pattern_speed_fuel_panel_5DEC.bin
var HudSpeedFuelPanelPattern []byte

//go:embed gfx/hud_color_speed_fuel_panel_5F25.bin
var HudSpeedFuelPanelColor []byte

//go:embed gfx/hud_pattern_gauge_bars1_5FEC.bin
var HudGaugeBars1Pattern []byte

//go:embed gfx/hud_color_gauge_bars1_601D.bin
var HudGaugeBars1Color []byte

//go:embed gfx/hud_color_gauge_bars1_mirrored_601F.bin
var HudGaugeBars1MirroredColor []byte

//go:embed gfx/hud_pattern_gauge_bars2_5FF7.bin
var HudGaugeBars2Pattern []byte

//go:embed gfx/hud_color_gauge_bars2_6022.bin
var HudGaugeBars2Color []byte

//go:embed gfx/hud_pattern_gauge_bars3_6008.bin
var HudGaugeBars3Pattern []byte

//go:embed gfx/hud_color_gauge_bars3_602D.bin
var HudGaugeBars3Color []byte

//go:embed gfx/hud_color_gauge_bars3_mirrored_6031.bin
var HudGaugeBars3MirroredColor []byte

// SpritesVehiclesPattern and SpritesCrashDebrisBonusPattern are two
// consecutive RLE streams ($5A73, 320 bytes / 10 16x16 sprites; $5B97,
// 800 bytes / 25 sprites) reached via $68AA's two `CALL $460B` sites -- an
// indirected decompress where the caller passes the address of a 2-byte
// pointer ($5A71, $5B95) rather than the stream address directly.
// Decoding that pointer (traced instruction-by-instruction: EX DE,HL; LD
// E,(HL); INC HL; LD D,(HL); EX DE,HL; INC DE) shows it actually encodes
// the VRAM *destination* ($1800 and $1A00, both within the confirmed
// sprite pattern table base of $1800) with the real stream starting 2
// bytes after the pointer itself -- confirmed by re-decoding both
// addresses directly from the ROM and finding they decode cleanly
// back-to-back with no gap ($5A73's stream ends exactly where $5B95's
// pointer begins, and $5B97's stream ends exactly where the HUD panel's
// own $5DEC stream begins).
//
// Rendered and visually inspected (internal/msxgfx.Sprite16Mono) to name
// them: $5A73 is genuinely top-down car sprites at various rotation
// angles, but $5B97 -- initially assumed to be more vehicles, hence its
// original working filename -- turned out to be explosion/spark frames,
// debris fragments, a heart (extra-life icon?), and, unambiguously, the
// bitmap digits for bonus score popups: "300", "500", "800", "1000".
// Renamed once the render made that obvious rather than left as a guess.
//
//go:embed gfx/sprites_vehicles_5A73.bin
var SpritesVehiclesPattern []byte

//go:embed gfx/sprites_crash_debris_bonus_5B97.bin
var SpritesCrashDebrisBonusPattern []byte

//go:embed gfx/ui_pattern_diagonal_border_63AC.bin
var UIDiagonalBorderPattern []byte

//go:embed gfx/ui_color_diagonal_border_67C2.bin
var UIDiagonalBorderColor []byte

//go:embed gfx/scenery_pattern_tree_bush_6645.bin
var SceneryTreeBushPattern []byte

//go:embed gfx/scenery_color_tree_bush_6737.bin
var SceneryTreeBushColor []byte

//go:embed gfx/scenery_pattern_tree_road_6697.bin
var SceneryTreeRoadPattern []byte

//go:embed gfx/scenery_color_tree_road_675E.bin
var SceneryTreeRoadColor []byte

// StageTerrainPattern/Color hold the per-stage roadside terrain sets
// loaded by internal/game.loadStageTerrainScenery, ported from
// $6A30-$6A4E: a 4-byte-per-stage ROM table at $6A51 ({patternSrcAddr,
// colorSrcAddr}, both little-endian) indexed by (stage-1)*4, with six
// real entries (the 7th 4-byte slot decodes as unrelated data). Stages
// 1-3 and 6 each get their own distinct terrain look -- stage 2 is
// confirmed against a real-hardware screenshot to be the beach/sea/palm
// set, and stage 6 renders as a snow scene (fir trees, drifts,
// red-and-white markers); stage 1's and 3's readings are visual only.
// Stages 4 and 5 share one identical entry, which is exactly
// SceneryTreeBushPattern/Color above -- already loaded a different way
// for those stages by drawTrackBorderAndTreeScenery, so not re-embedded
// here. Confirmed real (not guessed) by decoding them tightly packed and
// finding every stream boundary lands exactly on the next table entry's
// own start address, with stage 3's color stream ending exactly at $6645
// -- SceneryTreeBushPattern's own address.
//
// Discovered while investigating a user-reported real-hardware mismatch:
// initHUDAndTrackGraphics's per-stage switch had this call left as a TODO
// for the stage 1/2/3 and default cases, so those stages never got their
// own terrain loaded and instead showed whatever HUD-panel/gauge-bar tile
// data happened to already occupy the same character-code range -- see
// docs/ANALYSIS.md for the full comparison against a real reference
// screenshot.

//go:embed gfx/scenery_pattern_stage1_terrain_603E.bin
var StageTerrainPattern1 []byte

//go:embed gfx/scenery_color_stage1_terrain_61C8.bin
var StageTerrainColor1 []byte

//go:embed gfx/scenery_pattern_stage2_terrain_6279.bin
var StageTerrainPattern2 []byte

//go:embed gfx/scenery_color_stage2_terrain_6431.bin
var StageTerrainColor2 []byte

//go:embed gfx/scenery_pattern_stage3_terrain_64CA.bin
var StageTerrainPattern3 []byte

//go:embed gfx/scenery_color_stage3_terrain_65D7.bin
var StageTerrainColor3 []byte

// StageTerrainPattern6/Color6 are the $6A51 table's own 6th entry
// ($67D5 pattern, $6885 color, 200 bytes each / 25 tiles). Missed by the
// original pass at that table, which stopped at 5 entries on the
// assumption stage 5 was the last -- so stage 6 loaded no terrain at all
// (stageTerrainSources' own bounds check returned early). Confirmed
// genuine by the same tightly-packed-boundary test as the entries above:
// $67D5's control stream ends exactly at $6885, and $6885's ends exactly
// at $68AA -- initHUDAndTrackGraphics itself, i.e. the end of this data
// region, with no gap anywhere in the chain.

//go:embed gfx/scenery_pattern_stage6_terrain_67D5.bin
var StageTerrainPattern6 []byte

//go:embed gfx/scenery_color_stage6_terrain_6885.bin
var StageTerrainColor6 []byte

// SoundData is the raw, undecoded $4DD6-$5027 blob (594 bytes): the
// 13-entry PSG tone-period table ($4DD6-$4DE2), the per-sound-ID script
// pointer table (2-byte pointers starting at $4DE0 -- baseID 0's "entry"
// at $4DE0-$4DE1 is unused/never read since baseID is never 0, and
// baseID 1's entry at $4DE2-$4DE3 legitimately overlaps the pitch table's
// last byte, confirmed by the resulting pointer, $4E1C, continuing the
// same monotonically-increasing address sequence as every other entry --
// a real ROM space-reuse, not a bug), and the note-command bytecode
// streams themselves, all packed contiguously. Confirmed not RLE-
// compressed (see docs/ANALYSIS.md's sound engine section). Kept as one
// raw blob rather than parsed into separate Go tables since the original
// Z80 code addresses all three uniformly (HL just points somewhere in
// this range) -- see internal/game/sound.go's soundROM/soundScriptPointer.
//
//go:embed sfx/note_table_and_sound_scripts_4DD6.bin
var SoundData []byte

// MapTerrainTilesetPattern/Color are the confirmed pattern/color pair at
// $74BC/$7638 (see the "Pattern/color pairs" table in docs/ANALYSIS.md):
// a stage-map terrain tileset -- sky with clouds, grass, sand, water,
// gray winding roads, trees, a red car icon, a magenta gate/door, and
// railroad-crossing-sign tiles. Placed onto the visible screen by
// drawStageMapScreen ($744C, in graphics.go).
//
//go:embed gfx/map_pattern_terrain_tileset_74BC.bin
var MapTerrainTilesetPattern []byte

//go:embed gfx/map_color_terrain_tileset_7638.bin
var MapTerrainTilesetColor []byte

// The tables below are read by the port directly rather than blitted into
// VRAM: road curve and feature streams, the track-record blocks the
// starting layout is punched from, the attract-mode title reveal script,
// and the inter-stage map rows. They used to live in internal/game as Go
// byte-array literals -- ROM data transcribed verbatim into committed
// source -- and are embedded from extracted files instead so that no
// cartridge data ships with this repository. Each is a plain ROM range;
// the original reads them with ordinary loads, with no decompressor
// involved, so unlike the graphics above these are raw bytes rather than
// decoded streams. See cmd/extract's manifest for the addresses.

// TitleRevealTable is the $4AFC-$4B6D script revealRoadFighterTitle walks
// (backward from one end, forward from the other) to place the title's
// name-table characters over 25 frames.
//
//go:embed data/title_reveal_table_4AFC.bin
var TitleRevealTable []byte

// RoadCurveArray is the $5998 block injectRoadCurve indexes by absolute
// ROM address to read its per-row curve bytes.
//
//go:embed data/road_curve_array_5998.bin
var RoadCurveArray []byte

// StageMapRows is the $76EC-$7754 block drawStageMapScreen paints the
// inter-stage course map from, 5 bytes per row for 21 rows.
//
//go:embed data/stage_map_rows_76EC.bin
var StageMapRows []byte

// TrackRecordData is the $785B-$78C3 record block applyTrackRecord copies
// the starting track layout from. Byte-identical to
// gfx/tiles_candidate_785B.bin, which is the same ROM range reached by a
// different routine.
//
//go:embed data/track_record_data_785B.bin
var TrackRecordData []byte

// RoadFeatureStreamStage145, RoadFeatureStreamStage23 and
// RoadFeatureStreamStage6 are injectRoadFeature's per-stage streams;
// stages 1/4/5 share one, stages 2/3 another, stage 6 its own.
//
//go:embed data/road_feature_stream_stage145_78E5.bin
var RoadFeatureStreamStage145 []byte

//go:embed data/road_feature_stream_stage23_7BAA.bin
var RoadFeatureStreamStage23 []byte

//go:embed data/road_feature_stream_stage6_7F6B.bin
var RoadFeatureStreamStage6 []byte

// RoadStripEntry3 is the third and by far largest of the four
// roadStrip7ByteEntries data blocks ($7CB1); the other three are 14 bytes
// each and stay inline.
//
//go:embed data/road_strip_entry3_7CB1.bin
var RoadStripEntry3 []byte

// RoadCurvePhaseStage1..6 are refreshRoadCurvePhase's per-stage
// curve-phase streams. Stages 2 and 3 share one: the ROM holds a single
// stream at $7AAA that both read.
//
//go:embed data/road_curve_phase_stage1_78C4.bin
var RoadCurvePhaseStage1 []byte

//go:embed data/road_curve_phase_stage23_7AAA.bin
var RoadCurvePhaseStage23 []byte

//go:embed data/road_curve_phase_stage4_7E83.bin
var RoadCurvePhaseStage4 []byte

//go:embed data/road_curve_phase_stage5_7E60.bin
var RoadCurvePhaseStage5 []byte

//go:embed data/road_curve_phase_stage6_7EA8.bin
var RoadCurvePhaseStage6 []byte
