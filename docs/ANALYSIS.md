# Reverse-engineering notes

Working notes for `go-msx-racer`, an unofficial Go re-implementation of a
1985 MSX racing game. Brand and product names below are used only to
identify the hardware and software being interoperated with; see NOTICE.

## Next steps

### Open issues (as of 2026-09-04)

Ordered roughly by how likely each is to produce something a player
notices. Details for most are in the doc comment of the routine named.

1. **`$E00D` (gameOverFlag) is cleared by a deviation, not by traced
   code.** `initPlayerCarSpritesAndFuel` zeroes it so a finished stage
   doesn't immediately re-finish. A raw byte scan of the whole ROM found
   only 3 writes, none reachable on the in-race path, so *why* real
   hardware doesn't hit this is still unexplained. The riskiest item
   here: it's the one place behaviour was changed without being able to
   justify it from the disassembly.
2. **9 of the 11 decorative traffic-sprite pairs are never reset.**
   `scrollTrafficSpritesAndWrap` only ever decrements Y; whatever sends a
   pair back to the top once it reaches the wrap band was never found,
   and is presumably in `$70A8`/`$70D9`'s remaining unported pieces.
   `positionTrafficSprite` refreshes the first 2 pairs when an object
   spawns; the other 9 should simply freeze at the bottom.
3. **`positionTrafficSprite`'s `triggerOffTrackRecovery` path returns
   `true` on an unverified assumption.** Tracing `checkPlayerObjectProximity`'s
   reach of the same tail showed it resolves to a single-level return,
   which raises the same question for this call site. Left as-is because
   it's working, tested code; flagged for its own focused pass.
4. **Road-curve phase streams for stages 2-6 were never checked against a
   real-ROM oracle.** They're extracted and self-consistent (clean
   pair-aligned `$FE`, no bounds failures over many wraparounds), but the
   `curvecmp`-style byte-for-byte harness that validated stage 1 wasn't
   available to re-run against them.
5. **Feature streams are bounded by extraction, not by the ROM.** Each is
   taken as far as it stays self-consistent; real hardware has no bound
   and keeps walking into adjacent data. Late in a long stage the port
   will stop injecting roadside features where the cartridge would keep
   going (into what would likely be visible garbage).
6. **Object types 3 and 6 appear unreachable.** Every traced type-setting
   write is 0, 1, 2, 5 or 7. Harmless today, but it means part of the
   object system has never been exercised.
7. **The level-select cheat is a `cmd/play` deviation, but a small one
   now.** `game.StartRaceAtStage` is `stateGetReady`'s own race-start
   branch ($415A-$4162) with the stage chosen rather than randomized, so
   the race entry itself -- wipe, HUD, 26 scroll steps of the stage's own
   scenery, "START" -- is the ROM's, not ours, and consumes exactly the
   streams the ROM would. Two things remain deviations: choosing the
   stage at all, and `clearHUDPanelColumns`, which blanks name-table
   columns 23-31 because the arrival's 23-column wipe deliberately spares
   the HUD and the cheat arrives from the attract screen rather than from
   a live race. Stage 1 is untouched and still gets the real countdown.
   Three earlier approaches to this failed; see "Resolved: cheat-starting
   a later stage" near the end of this file for what they were and why
   they were wrong, which is worth reading before touching this again.
8. **The `E` cheat skips `$0F09`,** so `injectCheckpointText`'s "CHECK
   POINT" never fires for a level finished that way.
9. **Six assets are visually-inspected guesses, not confirmed** —
   `tiles_candidate_5998/62D0/637C/6479/785B` and `data_candidate_5E2C`
   (1352 bytes total).
10. **The `$7900-$7EFF` record cluster is undecoded** (`$7940`, `$78CB`,
    `$7AAF`, `$7EB3`, `$7ECF`, plus the `tiles_candidate_785B`
    neighbourhood). The copy *mechanism* (`$783F`) is ported and
    confirmed; what these specific records *mean* never was.
11. **~530 bytes of small scattered fragments remain unaccounted**, plus
    `$4BD2-$4BE0`: 15 bytes that disassemble as valid Z80 calling into
    `triggerSoundRaw`, with no caller found anywhere in the ROM.

Nothing else is known-broken. Everything a player has reported so far this
cycle has been traced to a root cause and fixed; those write-ups are at the
end of this file under their own "Resolved:" headings, most recently the
magenta car leaving the road, the Konami logo scrolling over the game-over
screen, the starting signal appearing on cheat-started stages, and three
title tiles surviving into a cheat-started race.

**Status note (accurate as of 2026-09-02, superseding the item-by-item
history this section used to hold):** the entire gameplay engine is now
ported and playable end-to-end — `cmd/play` boots, runs the full attract
sequence and level-select menu, races, scrolls and curves the road,
spawns and draws/moves traffic objects, handles fuel/scoring/HUD,
crashes and recovers, shows results, and plays real PSG audio throughout.
All 9 top-level game states, all 14 of `GameplayTick`'s dispatch targets,
all 6 stages of `updateRoadAndScore`, the entire `$70A8` object-slot
system (including both of its own previously-open gaps, see items 4-5
below), all 8 `updateSteeringAndCarState` car-behavior states, and the
sound engine are ported. The detailed, dated write-ups of how each of
those got traced are further down this file — search for the routine's
own `$XXXX` address (e.g. `$70A8`, `$5501`, `$4CBC`) to find the relevant
section; none of that history is repeated here anymore.

One adjacent curiosity noted but not chased further: object type 3
(`handleObjectType3`'s own dispatch case) and type 6 (`positionTrafficSprite`'s
own direct dispatch-table entry) don't appear to be reachable anywhere in
the traced object system -- every real type-setting write found is 0, 1,
2, 5, or 7. This doesn't block anything currently ported (the shared
$70A8 continuation runs regardless of type, and $7234's own gate is field
0, not the dispatch type), so it's left as a note for whoever traces the
object system further, not an open item of its own.

**ROM coverage sweep (2026-09-03)**: with every routine above now ported,
did a fresh audit of what's still genuinely unclassified, rather than
just trusting the last known count. Re-ran `cmd/disasm` first -- byte-
identical output to the checked-in listing, confirming there's no newly-
reachable code its own jump-table heuristics can recover automatically
right now (everything ported this session was already fully disassembled,
just not yet translated). Current coverage: 11854/16384 bytes (72.3%).

Auditing the remaining ~4530 bytes (105 `DATA` blocks) against every
known asset file and every address cited in this doc or `internal/game`'s
own doc comments accounts for the overwhelming majority of it:
confirmed RLE graphics/sound streams, the sound engine's own pointer
table, and — unchanged from before — the same 6 pre-existing
`tiles_candidate_*`/`data_candidate_5E2C` files (1352 bytes total) that
were already flagged as visually-inspected-but-unconfirmed. One of those
audit passes also caught a false negative worth naming: a handful of
small blocks (e.g. `$5526-$5533`) that looked unaccounted for by literal
address are actually the ROM's own per-stage jump tables for systems this
project re-implemented as Go `switch` statements instead of literal byte
tables -- genuinely used, just not citable by their own ROM address.

What's left after that accounting, roughly 530 bytes across ~19 small
scattered fragments, falls into two real, still-open buckets:

- A cluster in the `$7900-$7EFF` range (`$7940`, `$78CB`, `$7AAF`,
  `$7EB3`, `$7ECF`, plus the already-known `tiles_candidate_785B`
  neighborhood) sharing a distinctive repeating byte pattern
  (`xx 21/22 xx 20`-style pairs). This is very likely the actual
  *content* of the level/object-layout records `initTrackLayoutData`'s
  own `$783F` block-copier (already ported) reads and copies into RAM --
  i.e. the copy *mechanism* is confirmed, but what these specific records
  mean (object placement? road-curve cues? something else already
  covered by `stageMetaTileIDs`?) has never been decoded. Worth its own
  focused pass: extract the raw records, cross-reference against
  known-good stage layouts, and see if a pattern falls out the way
  `stageMetaTileIDs` did.
- A dozen or so smaller fragments (`$530F`, `$6127`, `$629A`, `$67BB`,
  `$6753`, `$6089`, `$65F4`, `$45BC`, `$5B5D`, `$600F`, `$5ACB`) in the
  `$4500-$6800` graphics/scenery region, each 8-70 bytes, not cross-
  referenced by any ported code or doc comment. Plausibly more per-stage
  scenery/parameter tables in the same family as the ones already
  extracted nearby, but not checked byte-for-byte. Low priority --
  small, and everything that visibly *renders* from this region is
  already accounted for by confirmed assets.
- One specific oddity worth flagging on its own: `$4BD2-$4BE0` (15
  bytes) disassembles as plausible, valid Z80 code -- `LD A,(IX+2); PUSH
  BC; LD D,1; CALL $4B76; POP BC; EX AF,AF'; LD (IX+9),A; RET` -- calling
  directly into the already-confirmed `triggerSoundRaw` ($4B76-$4BD1).
  No confirmed caller reaches it (searched for a literal `$4BD2` jump-
  table entry anywhere in the decoded ROM; none found), and its IX-
  relative addressing style doesn't match this ROM's usual HL-based
  convention elsewhere -- could be genuine dead/leftover code, or reached
  via a mechanism this project hasn't modeled (an interrupt vector?).
  Not chased further; noted here in case it turns out to matter.

None of this blocks anything currently ported or playable -- it's ROM
content the game's own already-confirmed code never asks this port to
render or act on (as far as has been traced). Treat it as the actual
remaining "unknown territory" the next deep-dive session should aim at,
rather than re-auditing the ~72% that's already accounted for.

**Items resolved this pass**, each independently small and tractable:

1. ~~**`$5434`**~~ **Done** — `advanceApproachingObjectMarker` in
   `gameplay.go`. Turned out to be a genuine gameplay mechanic, not
   passive "scenery": a scrolling hazard marker (the same `$E105` record
   `writeApproachingObjectRecord` arms) that forces an off-track crash if
   the player gets too close to it in both row and column. Verified via
   direct-call ROM tracing across ~30 cases (row/column boundary sweeps,
   the retire threshold, the sound-trigger gate) plus a 30000-frame
   full-game soak.
2. ~~**`$6BB3: CALL $6CD1`**~~ **Done** — `updateSteeringAndCarState`'s
   case 4 now calls `checkOffTrackCollisionAndCrash`, same target and
   stack depth as `updateNormalDriving`'s own `$6A90` call (case 4 is
   entered via the same discarding trampoline as case 0), so its
   "double-abort" `return true` is modeled identically at both call
   sites. Verified with a forced off-track position (blank VRAM reads as
   unsafe everywhere, a cheap way to force the branch): aborts and sets
   `$E049=1` exactly as the already-verified case-0 path does.
3. ~~**`$6A06`**~~ **Done** — `applyStageGaugeBar3ColorOverride` in
   `graphics.go`: ORs a per-stage high-nibble byte onto gauge bar 3's own
   128-byte color-table region (`colBase+$748`, confirmed by address --
   the literal `$748` only makes sense against this ROM's own
   colBase=`$0`), across all 3 Graphic 2 thirds. Verified via direct-call
   ROM tracing with the real gauge-bar-3 color assets loaded exactly as
   production init does, for all 6 stage values -- exact byte match.
4. ~~`checkSiblingProximityAndResolve`'s own `$733D-$7351` tail~~ **Done**
   — confirmed via direct-call ROM tracing across all 3 of
   `resolveObjectProximity`'s own outcomes (now itself changed to return
   `(outcome byte, apply bool)` instead of nothing, since the real Z80
   flags/A value it leaves behind is exactly what this tail needs) that
   it also transitions *this* object to type 2, not just its sibling —
   see `gameplay.go`'s own doc comments on both functions.

   ~~**Still open**: `positionTrafficSprite`'s `$7234-$7248`~~ **Done** --
   a full single-step ROM trace of a real, reachable object (field0=6,
   type=1, driven through its own normal dispatch chain) resolved the
   remaining pattern-byte mystery: it's always exactly `pattern+4` --
   `$724B`'s own last computed value, left over from its own second
   sprite-pair write just before it returns, independent of which
   dispatch path reached it. See `positionTrafficSprite`'s own doc
   comment and the dated write-up below for the full trace.
5. Also found and fixed while chasing the above: a real, previously-
   missed 20-byte gap (`$6EA7-$6EBA`) in the already-shipped
   `checkPlayerObjectProximity` -- see the dated write-up below.

**Ongoing practice, not a one-time task**: re-run `cmd/disasm` after
tracing any of the above — newly confirmed CALL/JP targets feed back into
its jump-table/candidate heuristics and can recover more of the ROM
automatically for free. The 5 pre-existing `tiles_candidate_*`/
`data_candidate_5E2C` assets are still genuinely unclassified (checked
directly — none appear as an `LD DE,` operand feeding `$45E0`/`$4611`),
not just unresolved instances of the compressed-source-detection bug.


## ROM used

Everything in this repo is derived from one specific dump. If your bytes
don't match these checksums, addresses/offsets throughout this document
and `docs/listing.asm` may not line up with your copy — check before
filing something as a discrepancy.

| | |
|---|---|
| Filename | `Road Fighter - Konami (1985) [7349].rom` |
| Size | 16384 bytes (16KB, unbanked — mapped straight into `$4000-$7FFF`) |
| CRC32 | `1adc13f4` |
| MD5 | `291130f4d9b5b01337e41505daee9568` |
| SHA-1 | `9fb10a44ecc5941325f8c7ddf472d0c78f37bfc3` |
| SHA-256 | `9a3bbfabdc2b089790b9549dfe503a8d250925ed091517de078beddd627d2888` |

Verify with any of, e.g.:

```bash
shasum -a 256 "Road Fighter - Konami (1985) [7349].rom"   # macOS/Linux
sha256sum "Road Fighter - Konami (1985) [7349].rom"        # Linux (coreutils)
```

The ROM file itself is **not** checked into this repo (`.gitignore`
excludes `*.rom`) — it's copyrighted Konami material; place your own copy
at the repo root under the filename above (or pass `-rom <path>` to any of
the `cmd/` tools) to reproduce this project's steps.

## Overview

Source: `Road Fighter - Konami (1985) [7349].rom`, 16384 bytes, mapped at
Z80 address `$4000-$7FFF`. Standard MSX cartridge header at `$4000`:

| Offset | Bytes | Meaning |
|---|---|---|
| $4000-$4001 | `41 42` | "AB" signature |
| $4002-$4003 | `4F 40` | init address = `$404F` |
| $4004-$400F | all zero | statement/device/basic handler hooks, unused |

## Tooling

- [`internal/z80dis`](../internal/z80dis/z80dis.go) — a from-scratch Z80
  disassembler (full documented instruction set incl. CB/ED/DD/FD/DDCB/FDCB).
- [`cmd/disasm`](../cmd/disasm/main.go) — recursive-descent disassembler
  driver for this ROM. Regenerate the listing with:

  ```bash
  go run ./cmd/disasm -rom "Road Fighter - Konami (1985) [7349].rom" -out docs/listing.asm
  ```

  Beyond following CALL/JP/JR targets, it handles two idioms found in this
  ROM that a naive linear/recursive disassembler gets wrong:
  1. **Computed-jump-table calls.** `$4045` and `$4047` are tiny trampolines
     that pop their own return address and use it as a jump-table base
     (indexed by a register), so the CALL never falls through — the bytes
     right after such a CALL are table data, not code. The tool special-cases
     these two addresses and walks the table directly.
  2. **Byte-level coverage tracking**, so a mis-aligned tentative decode can
     never silently overlap/corrupt an already-confirmed instruction — a
     classic disassembler failure mode with variable-length instruction sets.
  
  Current coverage: ~7500 instructions, ~12KB/16KB (~73%) confidently
  classified as code; the rest is reported as raw `DATA` blocks in the
  listing.
- [`internal/msxgfx`](../internal/msxgfx/render.go) — renders MSX Screen 2
  tiles: `TileRGBA(pattern, colorRows)` composites 8 pattern bytes with 8
  matching color bytes (one MSX color-table row each) into real RGB using
  the standard TMS9918A 15-color palette; `TileMono` renders pattern-only
  data in a single flat color, for data with no known color companion (or
  genuine hardware sprites, which don't have one at all — see "VRAM memory
  map" below); `Sprite16Mono` renders a 32-byte 16x16 hardware sprite
  pattern using its *actual* storage layout — four 8x8 quadrants
  (top-left, bottom-left, top-right, bottom-right), **not** 16
  interleaved rows, which is an easy-to-reach-for but wrong guess (see the
  correction note under "Extracted assets"). Shared by `gfxpreview` and
  `gfxviewer`.
- [`cmd/gfxpreview`](../cmd/gfxpreview/main.go) — renders a byte range as a
  grid of 8x8 or 16x16 tiles to a PNG, used to visually confirm which
  `DATA` blocks are actually pattern-generator graphics:

  ```bash
  go run ./cmd/gfxpreview -rom "Road Fighter - Konami (1985) [7349].rom" \
    -start 478A -len 248 -tile 8 -cols 20 -scale 6 -out preview.png
  ```

  Add `-color <path>` (8x8 tiles only) to render in real color instead of
  monochrome, given a matching decoded MSX color-table file:

  ```bash
  go run ./cmd/gfxpreview -rom assets/gfx/hud_pattern_speed_fuel_panel_5DEC.bin \
    -start 4000 -len 512 -tile 8 -cols 8 -scale 10 \
    -color assets/gfx/hud_color_speed_fuel_panel_5F25.bin -out preview.png
  ```

  **Important:** most of this ROM's graphics data is RLE-compressed (see
  "Compression format" below) — running `gfxpreview` directly on raw ROM
  bytes only produces a meaningful image for *uncompressed* data. Check
  whether an address is a confirmed `LD DE,` source feeding `$4611`/`$45E0`
  first (`grep -n "LD DE,<addr>h" docs/listing.asm`); if so, decode it with
  `cmd/rledecode` first and run `gfxpreview` on the decoded output instead.
- [`cmd/rledecode`](../cmd/rledecode/main.go) — decodes one of the
  compressed streams described below, starting at a given address:

  ```bash
  go run ./cmd/rledecode -rom "Road Fighter - Konami (1985) [7349].rom" \
    -start 478A -max 512 -out decoded.bin
  ```

- [`cmd/gfxviewer`](../cmd/gfxviewer/main.go) — interactive Ebitengine
  viewer that steps through every extracted asset in `assets/gfx/` one at a
  time, name on screen, advancing on a key press (arrows/space/N/P, T to
  toggle tile size, G to toggle the tile grid, Esc/Q to quit). Same tile
  interpretation as `gfxpreview`, so it's for browsing already-decoded
  assets, not raw ROM addresses. Also appends three synthesized "atlas"
  entries with no backing file (see `cmd/gfxviewer/atlas.go` and the
  "Pattern/color pairs" section's follow-up below): every traffic-object
  kind and player car/crash sprite in its real color, and every meta-tile
  ID as the object pair it spawns:

  ```bash
  go run ./cmd/gfxviewer
  ```

- [`cmd/play`](../cmd/play/main.go) — runs the actual Go port
  (`internal/game`) against a real, in-memory VDP
  (`internal/msx.VDP`) and renders whatever ends up in VRAM every frame
  with Ebitengine: the port genuinely *runs*, not just compiles. Most of
  the state machine is still stubs (see "What's not yet ported"), so
  what's on screen is only whatever's been ported so far — currently the
  boot sequence and the Konami logo.

  Before the Go port's own `ColdBoot` runs, `cmd/play` calls
  `msx.VDP.SetPowerOnDefaults` — **not** a port of any ROM (the MSX
  system BIOS that would really do this is a separate, non-Konami ROM
  this project doesn't have), just a documented approximation of the
  well-known default MSX boot appearance (backdrop color 4, blue), so the
  emulated hardware starts realistically instead of all-registers-zero
  (black). `cmd/play` also renders a `borderPx`-wide margin around the
  256x192 active display, filled with the VDP's *current* backdrop color
  (`VDP.BackdropColor`, R7's low nibble) every frame — without it, the
  confirmed blue R7 value (set by both `SetPowerOnDefaults` and this
  ROM's own `InitVDPAndSoundEngine`) would have nothing to render into,
  since Screen 2's 256x192 area is always fully covered by name-table
  tiles and never shows the backdrop color itself.

  ```bash
  go run ./cmd/play
  ```

  Assets it needs (font, Konami logo) are loaded from the `assets`
  package (`//go:embed` over `assets/gfx/*.bin`), not decompressed from
  the ROM at runtime — see the "Go port" section below for why.
- [`internal/psg`](../internal/psg/psg.go) — a simplified, non-cycle-accurate
  real-time audio synthesizer for the AY-3-8910-compatible PSG: recomputes
  each channel's frequency from its registers once per output sample and
  phase-accumulates a square wave (plus a simple randomized noise source),
  rather than simulating the chip's actual ~1.79MHz clock. Verified against
  the AY-3-8910 datasheet's own formula (`f = clock/(16*period)`) by
  generating a 440Hz tone and counting zero-crossings — got exactly 880 in
  one second, i.e. exactly 440Hz. `WriteRegister(register, value byte)`
  matches `msx.System.WritePSGRegister`'s signature, so a `*psg.Chip` (or a
  small adapter around one) can back that interface directly.
- [`cmd/sndpreview`](../cmd/sndpreview/main.go) — interactive PSG preview:
  plays a scale/noise directly (bypassing the game entirely, to test the
  synthesizer itself) and can trigger real game sound IDs via
  `game.Console.TriggerSound`, logging whether any tone/volume register
  actually changed. It now will, for any sound ID — `advanceSoundChannel`
  and `engineSoundPitchStep` (see "What's not yet ported") between them
  cover every path a triggered sound can take.

  ```bash
  go run ./cmd/sndpreview
  ```

## Compression format (`internal/rle`)

Most graphics data in this ROM — confirmed so far: the text font, and at
least one color-table block — is compressed with a simple custom RLE
scheme, traced from the VRAM-fill loop at `$4611-$4634` (which drives the
byte-at-a-time VDP-port writer at `$4591`/`$45B0-$45BB`), and its 3x/8x
broadcast wrappers at `$45E0` (copy to 3 VRAM regions 0x800 apart — the
Screen 2 pattern-table thirds) and `$45CF` (same idea, for a plain constant
fill via `FILVRM`, unrelated to the RLE format itself). Implemented in
[`internal/rle`](../internal/rle/rle.go):

- `0x00` — end of stream
- `0x01`-`0x7F` — repeat run: read one more byte, emit it N=control times
- `0x81`-`0xFF` — literal run: copy the next N=(control&0x7F) bytes as-is
- `0x80` — destination retarget: the next 2 bytes are a new absolute VRAM
  write address; subsequent output goes there instead of wherever writing
  left off. The *control* stream (source) is completely unaffected — it
  simply continues with the byte right after those 2 address bytes. Traced
  from `$460B`/`$4611` register-by-register (which register `SETWRT`'s
  address argument comes from at each point). This was **initially
  misdocumented as the opposite** — retargeting the *source*/control
  stream to let multiple compressed blocks share a common tail — on
  nothing but a plausible-sounding theory; no stream exercised the path
  until `$46FC` was decoded (see "Extracted assets" → the Konami boot
  credit), which produced garbage until the direction was reversed. Fixed
  in both this document and `internal/rle/rle.go`; see the package's doc
  comment for the same trace in more detail. `internal/rle.Decode` returns
  any such retargeted run as a separate `Chunk{Dest, Data}` in its `extra`
  return value, since — unlike the stream's first run — its destination
  comes from inside the stream rather than from the call site.

`$460B` is also reachable directly as an **alternate entry point**, not just
as the in-stream `0x80` target: a caller can `LD DE,<tableAddr>; CALL
$460B` instead of the usual `LD HL,<dest>; LD DE,<streamAddr>; CALL $4611`.
In that form, `<tableAddr>` holds a 2-byte VRAM destination address
followed immediately by the real compressed stream — `$460B`'s preamble
reads that word into HL (the destination `$4611` normally expects the
caller to have loaded directly) and advances past it before falling into
the same decode loop. Seen in practice at `$692E` (`LD DE,$5A71; CALL
$460B`): `$5A71`-`$5A72` hold the destination ($1800), and the actual
stream starts at `$5A73` (decoded as `assets/gfx/sprites_vehicles_5A73.bin`). Saves
a few bytes over the two-literal-loads form at call sites that don't
otherwise need HL. `cmd/rledecode` only implements the plain `$4611` form
(point `-start` at the stream itself, i.e. `tableAddr+2` for this variant)
— it doesn't parse the `$460B` header automatically.

The two run kinds are easy to mix up from the control-byte ranges alone —
and an earlier version of this document (and `internal/rle`) had them
backwards. What actually distinguishes them in the original is which
address the `DJNZ` loop jumps back to: the bit7-clear branch (`$462C`) reads
one byte, then loops back to `$462E` — *after* that read — re-emitting the
same byte without ever advancing the source, i.e. a repeat run; the bit7-set
branch (`$4621`) loops back to `$4622`, the read itself, so each iteration
advances the source, i.e. a literal run. See `internal/rle/rle.go`'s package
comment for the full trace.

This whole format was discovered because the extracted font looked visually
broken for a handful of characters — some tiles had a cut-off top row,
others looked like noise, in a pattern that shifted in and out of
alignment. That's the signature of reading a variable-length stream as
fixed-width data. The *first* fix (literal/repeat swapped) reproduced the
same kind of partial breakage one level down: some glyphs decoded correctly
by coincidence (a repeat-encoded run of zero bytes looks identical either
way), while others didn't — which is what surfaced the swap. See the
"Extracted assets" correction note below for how it was actually confirmed
fixed (full A-Z decoded cleanly, not just "looks less obviously wrong").

## VRAM memory map

MSX Screen 2 ("Graphic 2") splits color from shape: each 8x8 tile has 8
pattern bytes (1bpp, one per row) *and* 8 matching color bytes, one per
row (high nibble = foreground palette index, low nibble = background) —
unlike Screen 1, where color is shared across groups of characters. That
per-row color table is what lets a single tile show up to 8 different
2-color combinations top-to-bottom.

Where each table actually lives in VRAM is configured by VDP registers,
and it is *not* the layout MSX BASIC's default `SCREEN 2` uses — this game
reprograms it. Using the confirmed boot-time register values
(`assets/vdp_init_registers.bin`, R0-R7 = `02 E2 0E 7F 07 76 03 E4`) and
the standard TMS9918A formulas:

| Table | Formula | Value here |
|---|---|---|
| Name table | `R2 * 0x400` | `$3800` |
| Color table | `R3` bit 7 ? `$2000` : `$0000` | **`$0000`** |
| Background pattern table | `R4` bit 2 ? `$2000` : `$0000` | **`$2000`** |
| Sprite pattern table | `R6 * 0x800` | `$1800` |
| Sprite attribute table | `R5 * 0x80` | `$3B00` |

That's the opposite of the textbook default (which puts pattern at `$0000`
and color at `$2000`) — worth stating plainly since it's easy to assume the
default and get pattern/color backwards, which is exactly what happened
here initially (see the correction note below). It also happens to pack
all 16KB of VRAM with zero gaps and zero overlap: color `$0000-$17FF`,
sprite patterns `$1800-$1FFF`, background patterns `$2000-$37FF`, name
table `$3800-$3AFF`, sprite attribute table `$3B00-$3B7F` — a good sign
the derivation is right, not just plausible. The sprite attribute table
entry sat undiscovered for a while: `internal/game.blitSpriteAttributeTable`
(formerly misnamed `copyTrackDataToVRAM`) had already been ported as "copy
some RAM buffer to `$3B00`, purpose unconfirmed" purely from tracing
`$526B`'s own bytes, well before anyone multiplied out `R5*0x80` and got
the identical address — see the dedicated sprite write-up under "What's
not yet ported" for the full story, including confirmation that the
player's car really is drawn as two 16x16 hardware sprites, not one.

Practical consequence for telling extracted RLE streams apart: a
decompression destination in `$0xxx`/`$1xxx` (outside the narrow
`$1800-$1FFF` sprite band) is **color** data; `$2xxx`/`$3xxx` (below the
name table) is **background pattern** data. This held with zero exceptions
across all 18 non-font, non-sprite streams decoded this session — once
using the *correct* base assignment. (An MSX1/TMS9918A hardware sprite,
like the ones at `$1800`, is a different animal again: it's single-color,
set per-sprite at runtime via the sprite attribute table, not per-row from
a ROM color table — so there's no "color file" to pair it with, and
`internal/msxgfx.TileMono` is the right tool for those, not `TileRGBA`.)

### Correction: color index 0 is "transparent" (shows the backdrop), not black

`internal/msxgfx.TileRGBA` originally rendered palette index 0 as literal
black, in either the foreground or background nibble. That's wrong on
real TMS9918A hardware: index 0 means "transparent" — show the VDP's
*backdrop* color (register 7's low nibble) instead — independently per
nibble, so a tile can have a real foreground color over a
backdrop-showing background, or vice versa. This isn't a cosmetic
nitpick for this ROM specifically: `ShowKonamiLogo`'s own color fill is a
flat `$F0` (foreground white, background **0**), and `SetupFont`'s is the
same `$F0` for every font-using screen (the "SOFTWARE" credit, the
"©KONAMI 1985"/"PLAY SELECT"/"LEVEL A"/"LEVEL B" menu text) — meaning
essentially all of this game's text and the Konami logo were being
rendered as white-on-a-black-box instead of white floating directly on
the selected backdrop color, which is how it actually looks on real
hardware (and is *why* the game bothers setting the backdrop to blue at
all — half the payoff was invisible with the bug in place).

Fixed by giving `TileRGBA` a `backdrop color.RGBA` parameter, substituted
for either nibble whenever it's 0, instead of indexing `Palette[0]`
(which stays defined as literal black — it's still the right entry for
rendering the backdrop layer itself, which really is black when register
7 selects color 0). `cmd/play` passes the *live* VDP's current backdrop
(`msx.VDP.BackdropColor`, i.e. register 7); `cmd/gfxpreview` and
`cmd/gfxviewer`, which preview raw asset bytes with no live VDP, fall
back to their own existing neutral canvas background instead of a
specific MSX color. Confirmed by rendering the Konami logo/credit screen
end to end: text now floats on the selected blue backdrop rather than
sitting in a black box, matching real Road Fighter.

### Follow-on correction: `showRoadTitle` also sets the backdrop to black, via a previously misidentified BIOS call

Fixing the transparency bug above exposed a second, real gap: once the
"ROAD FIGHTER" title screen replaces the Konami logo, the backdrop should
switch from blue to **black** — but nothing in this port changed it, so
it stayed blue straight through the title screen. The ROM does change
it, at `$49A6`-`$49AA`: `LD B,$E0; CALL $46B1`.

`$46B1` (`LD C,$07; JP $4038`, and `$4038` is itself just `JP $0047`) was
previously documented as an unclear call to "the real MSX BIOS WRTPSG
vector" and left unported, on the reasoning that its register setup
(only `C` loaded) didn't match WRTPSG's real `A`/`E` signature clearly
enough to guess at. That reasoning was solid but the destination was
wrong: `$0047` is **WRTVDP**, not WRTPSG (register in `C`, value in
`B`) — confirmed unambiguously by `InitVDPAndSoundEngine`'s own
register-init loop, which calls this exact same `$0047` the exact same
way (see "Go port" below, which already had the correct `$0047`=WRTVDP
mapping — the inconsistency was in this one call site's analysis, not a
project-wide misunderstanding). With `$49A6`'s `LD B,$E0` feeding it,
this writes VDP register 7 = `$E0`: backdrop color 0 (black).

Ported as a single `c.Sys.WriteVDPRegister(7, 0xE0)` at the top of
`showRoadTitle`. Confirmed by rendering both screens end to end: backdrop
is blue during the Konami logo/credit, and switches to black exactly
when the "ROAD FIGHTER" title takes over.

### Second follow-on correction: the 3-way Graphic 2 "thirds" split doesn't actually apply to this ROM's registers

A second real-hardware screenshot from the user, of the exact same
starting-grid screen already discussed above, pointed at a much more
fundamental rendering bug: the user correctly diagnosed that the top 8
rows of roadside scenery looked "almost right" while the bottom 16 were
missing almost entirely, and asked directly whether the pattern tables
for the second and third screen-thirds might not be populated. They were
right, but the actual mechanism is more interesting than "a missing
loader" (which is what a first pass at this, following the same
methodology as the `$6A30` fix above, went looking for and failed to
find): a further exhaustive grep of the entire disassembly for any
literal VRAM address in the pattern table's band-1/band-2 ranges
(`$28xx`-`$2Fxx`, `$30xx`-`$37xx`) or the color table's equivalents
(`$0Cxx`-`$17xx`) turned up **zero hits, anywhere in the 16KB ROM** —
not one instruction in this entire game ever addresses those ranges
directly. Every single graphics load either writes once to band 0
(`$6A30`, `$744C`, `drawTrackBorderAndTreeScenery`'s tree/border pieces)
or writes the *identical* bytes to bands 0, 1, and 2 via a 3-times loop
(`initHUDAndTrackGraphics`'s HUD panel/gauge-bar loads, `initFirstSixteenChars`
— see `internal/game.placeInAllThirds`). No code anywhere ever puts
*different* content in band 1 or band 2 than what's in band 0.

That's consistent with a ROM that doesn't actually get 3 independent
bands at all. The textbook TMS9918A Graphic 2 formula (pattern/color
address = table base + (row/8)*0x800 + character*8 + fine row) requires
the pattern-table and color-table mask registers (R4 and R3) to have
their relevant low bits set to 1 for that 3-way independence to hold.
This ROM's confirmed, never-rewritten boot-time values are R4=`$0E`
(low 2 bits = `10`, not `11`) and R3=`$7F` — both irregular by that
standard. `cmd/play`'s own compositor (`Draw`), and the equivalent logic
this session's diagnostic harness copied from it, had always computed
the read address using the textbook independent-3-way formula anyway,
which is *why* `$6A30`'s newly-loaded stage-1 terrain only ever showed up
in the top 8 rows: those were the only rows whose computed read address
actually landed on the one place `$6A30` wrote to.

Rather than derive the TMS9918A's real general address-masking formula
for arbitrary R3/R4 values from memory (a real risk of confidently
shipping a plausible-but-wrong formula, exactly what this project's own
conventions warn against), this was resolved empirically, the same way
the sprite-quadrant-order and pattern/color-table-swap mistakes were
caught in earlier sessions: render with a specific hypothesis and check
it against the real screenshot. Forcing the read side to always use band
0 regardless of row — i.e. exactly matching "nothing in the ROM ever
puts unique content in bands 1/2" — made the previously-missing bottom
two-thirds of the roadside scenery reappear, matching the reference
photo's continuous grass/scenery for the full screen height, with zero
change to anything already confirmed correct (the Konami logo/credit
screen and the "ROAD FIGHTER" title/menu screen were both re-rendered
after the change and are pixel-identical to before, as expected, since
every asset visible on those screens was already written identically to
all 3 bands).

Fixed by removing the per-row band offset from `cmd/play`'s `Draw`
entirely — both pattern and color reads now always use the base address
plus `character*8`, no row-dependent term. This is deliberately scoped
as a fix for *this ROM's specific, unchanging register values*, not a
general TMS9918A model: the general formula for arbitrary R3/R4 masking
remains unverified and unimplemented, honestly, since nothing in this
ROM would exercise it anyway (see the exhaustive band-1/2 address search
above). `internal/game.placeInAllThirds` and every routine that calls it
are untouched — they still faithfully replicate the ROM's own literal
(if redundant, on this hardware) 3-times-write behavior.

This also resolves a puzzle from the `$6A30` write-up above: why
`drawTrackBorderAndTreeScenery` (stages 4/5) uses `placeInAllThirds`
(looping) for its tree scenery while `$6A30` (stages 1-3) writes once —
both are equally "correct" on real hardware given the collapse, so the
looping version isn't doing anything the single-write version doesn't;
it's just a different, more defensive coding style from whoever wrote
that particular routine, not a sign that stages 1-3 were ever intended
to look different.

**Still not explained by this fix**: the second, narrower band of
roadside tile codes (around `$4C`/`$4D`) that still shows HUD-panel/
gauge-bar fragments instead of a plain building, and the small
"flag"/pennant-like decorations the user reports as visibly
mispositioned, plus the starting-grid countdown-signal icons rendering
with the wrong shape/color. These are tile-code-level content bugs (the
name table referencing the wrong character codes, or those codes'
pattern/color data itself coming from the wrong source), not VRAM-address
bugs — the collapse fix changes *which* band gets read, not *what*
ended up written to band 0 in the first place. Not yet traced.

## Extracted assets

Non-graphics assets and the font (unpaired, so shown mono in the viewer):

| File | Address range | Confidence | Contents |
|---|---|---|---|
| `assets/gfx/font_8x8.bin` | RLE stream at $478A, decodes to 344 bytes | **High** — visually confirmed: renders as a complete, clean 8x8 font (digits 0-9, ©, a couple of icons, `A-Z`) | 43 8x8 1bpp glyphs, contiguous A-Z pattern range with `D`, `J`, `Q`, `U`, `X`, `Z` left as genuinely blank (all-zero) pattern data — confirmed intentional, not a bug: the blanks fall at exactly the right alphabetical slots (not missing/compacted), and the render tool draws pattern bits directly with no color-table involvement, so a real shape couldn't be hiding there as "invisible." Read as: a full A-Z range was reserved but only the letters this font is actually used for were drawn (likely HUD/status text; the "ROAD FIGHTER" title logo, which needs a D, is almost certainly a separate fixed graphic elsewhere, not built from this font) — not traced further than that. |
| `assets/gfx/sprites_vehicles_5A73.bin` | RLE stream at $5A73 (via the `$460B` alt entry, see below), decodes to 320 bytes | **High** — dest `$1800` is confirmed hardware sprite pattern table (see VRAM map above), and rendered with the *correct* quadrant layout (see `internal/msxgfx.Sprite16Mono` below) it shows several clean, clearly car-shaped 16x16 silhouettes | 10 tiles, 16x16 1bpp. Genuine MSX1 hardware sprites — single-color at runtime via the sprite attribute table, no ROM color companion to pair (`TileMono`/`Sprite16Mono`, not `TileRGBA`) |
| `assets/gfx/sprites_crash_debris_bonus_5B97.bin` | RLE stream at $5B97 (same `$460B` pointer-indirection family, immediately following `$5A73`'s stream with no gap), decodes to 800 bytes | **High** — dest `$1A00`, same sprite pattern table; rendered, shows explosion/spark frames, debris fragments, a heart shape, and the unambiguous bitmap digits "300"/"500"/"800"/"1000" (bonus score popups) | 25 tiles, 16x16 1bpp. Initially assumed to be more vehicle sprites (a natural-looking continuation of `$5A73`'s destination) until actually rendering it showed otherwise — see the `$68AA` write-up under "What's not yet ported" for the correction |
| `assets/gfx/bigfont_pattern_road_4A25.bin` | RLE stream at $4A25, decodes to 264 bytes | **High** — see the dedicated section below | 33 8x8 tiles, composited as 16x16 "big characters" in plain raster order (`internal/msxgfx.BigChar16Mono`) — a **third** distinct tile layout in this ROM, different from both the RLE pattern/color convention and the hardware-sprite quadrant order. Placed by phase 3's `$49C2`/`$49EC` name-table script into two lines reading **"ROAD FIGHTER"**. |
| `assets/vdp_init_registers.bin` | $46A9-$46B0 | **High** (confirmed by the `WRTVDP` loop that reads it, and by reverse-deriving the VRAM map above from it, which checks out with zero gaps/overlaps) | VDP registers 0-7 |
| `assets/sfx/note_pitch_table.bin` | $4DD6-$4DE2 | **High** (confirmed by the note player indexing it per-semitone) | 13-entry PSG tone-period table (one octave) |
| `assets/sfx/note_table_and_sound_scripts_4DD6.bin` | $4DD6-$5027 | Medium | Pitch table + the per-sound-ID pointer table + (presumably) the note-command bytecode streams themselves, taken as **raw, undecoded** bytes. Confirmed *not* RLE-compressed (see correction note below) — the raw bytes are what the sound engine actually reads. Bounded above by `$5028`, a confirmed CALL/JP target (real code), so the blob can't run past that. |
| `assets/gfx/tiles_candidate_*.bin`, `data_candidate_5E2C.bin` | various | **Low/Medium**, and specifically **not confirmed RLE sources** (checked) | Visually inspected with `gfxpreview` on raw bytes; some rows show clean repeating tile-like shapes, others are noisy — could still be compressed via some other mechanism, sprites at a different bit depth/layout, or non-graphics tables. Not conclusively classified. |

### `assets/gfx/logo_pattern_konami_490F.bin` — the Konami logo

**High confidence, fully confirmed** — both by call site and by rendering.
Previously listed as `ui_pattern_unpaired_490F.bin` at Medium confidence
("likely title/attract-screen graphics", no located color); tracing its
one and only call site resolved it completely.

Call chain: `$4128` (`CALL $48BB`) is reached from inside
`stateAttractSequence`'s (game state 0) DJNZ sub-phase chain, in the same
branch that triggers sound id $1B (`LD A,$1B; CALL $4B6E` — confirmed to
play nothing, see `InitVDPAndSoundEngine`'s doc comment; the same id
`InitVDPAndSoundEngine` triggers at boot) and calls the
font-setup routine (`$4773`) — squarely a title/boot-screen context.
`$48BB` sets up a countdown/position pair (`$E00A`=14, `$E00E`=$3AAA, a
name-table address) and falls into `$48C9`:

- `$48C9`-`$48CF`: `LD DE,$490F; LD HL,$6200; CALL $45E0` — decompresses
  the pattern into background pattern table character slots `$40`-`$59`
  (masked dest `$2200`, offset `$200` = char 64 * 8).
- `$48D2`-`$48DA`: `LD HL,$0200; LD BC,$00D8; LD A,$F0; JP $45CF` — fills
  the **color** table at the matching offset (`$0200`) with a *constant*
  `$F0` (foreground white / background black) across exactly 216 bytes —
  the entire logo's worth. This is why no decompressed color companion
  was ever found: there isn't one. A flat two-color logo doesn't need
  per-row color, so the game just fills a constant instead of spending
  ROM space compressing one. This project's tooling only checked for the
  `$45E0` decompression pattern when looking for color pairs, which is
  what missed it.
- `$48DD`-`$4900`: writes the actual character codes into the name table
  via `$4901` (a tight `WRTVRM`-style loop, sequential codes starting at
  `$40`) across three consecutive name-table rows of 3, 11, and 12 tiles.

Rendering the 26 used tiles (of 27 in the file) in exactly that row-width
layout — not a uniform grid, which is what made this look like unresolved
noise before — produces an unmistakable **"Konami®" wordmark**: the row
widths directly reflect the logotype's varying letter heights (the tall
"K" needs all three tile-rows; the shorter lowercase letters and the ®
mark need fewer).

**Update — the scroll animation is confirmed and ported.** `$E00A`'s
countdown and `$E00E`'s per-row-decrement do drive a genuine multi-frame
scroll, not a one-shot draw: `$48DD` has no `RET` before falling straight
into the redraw-and-decrement code at `$48E7`-`$4900`, so whatever `CALL`s
`$48DD` runs the *entire* "move up one row, redraw, clear the row vacated
below, decrement the counter" sequence every time. That caller is
`stateAttractSequence`'s phase-1 branch (`$40AD`-`$40BD`), gated to run on
odd frames only (`$E003` bit 0 — `$E003` is just the free-running
per-frame counter, so this means "every other frame"), and its `RET NZ`
right after the `CALL` is testing the flag left by that same routine's
final `DEC (HL)` on the counter — i.e. "keep waiting" until the countdown
hits zero, then fall through and advance to phase 2. 14 total steps (1
performed by `ShowKonamiLogo` itself, falling through from setup into the
first scroll step, + 13 more from phase 1) move the logo from row 20
(near the bottom) up to row 7, roughly the screen's vertical center —
confirmed both by the address arithmetic and by rendering the animation
frame-by-frame in `cmd/play`. Ported as `ShowKonamiLogo` +
`scrollKonamiLogoStep` in `internal/game/graphics.go`; see
`stateAttractSequence`'s doc comment in `state.go` for the phase-1 side.

### `$46FC` — the "KONAMI / -------- / SOFTWARE" boot credit

**High confidence, fully confirmed** — by decoding, by the confirmed font
mapping, by column-alignment against the logo, and by rendering.

Once the logo's scroll finishes (phase 1's countdown hits zero),
`stateAttractSequence` runs `$40B6`-`$40BB`: `LD DE,$46FC; CALL $460B` —
the `$460B` *alternate entry* form (see "Compression format" above), where
the 2 bytes at `$46FC`-`$46FD` (`$4A,$39` → destination `$394A`) are the
initial VRAM write address and the real compressed stream starts at
`$46FE`. Decoding it (`cmd/rledecode -start 46FE`, cross-checked by hand)
gives two runs:

- A 12-byte repeat run of pattern character `$5A` at name-table address
  `$394A`. `$5A` is tile 26 of `logo_pattern_konami_490F.bin` — the one
  tile of the 27 in that file the wordmark itself never uses in the
  `$48DD` redraw loop — and its 8 pattern bytes (`00 00 00 FF 00 00 00
  00`) render as a single solid horizontal line. So this reuses leftover
  logo pattern data as a decorative rule rather than shipping its own tile.
- A `$80` destination retarget to `$396C`, followed by an 8-byte literal
  run: `$33,$2F,$26,$34,$37,$21,$32,$25`. Finding this run is actually
  what caught `$80`'s semantics being documented backwards (see
  "Compression format" above) — decoding it under the old "retarget the
  source" theory produced a bogus ROM address and the stream never
  terminated. Once corrected, `$396C` is a plain name-table address, and
  running those 8 bytes through the confirmed font character mapping (see
  "the text font" below — character code `$20`+N corresponds to font tile
  N, with digits at `$10` and letters at `$21`) decodes to exactly
  **S, O, F, T, W, A, R, E**.

Both writes line up with the settled logo: the rule spans name-table
columns 10-21 (12 wide), "SOFTWARE" spans columns 12-19 (8 wide), both
centered on column 15-16 — directly below row 7, where `scrollKonamiLogoStep`
leaves the logo once its countdown reaches zero. Rendering the full boot
sequence in `cmd/play` (or headlessly, compositing VRAM the same way)
confirms it: the classic Konami logo, rule, and "SOFTWARE" caption, stacked
exactly as on real hardware. Ported as `showSoftwareCredit` in
`internal/game/graphics.go`, called from `stateAttractSequence`'s phase-1
branch in `state.go` right before it advances to phase 2 — phase 1 is now
**fully ported**.

### `assets/gfx/bigfont_pattern_road_4A25.bin` — a 16x16 "big font": **"ROAD FIGHTER"**

**High confidence, fully confirmed and ported** — by decoding,
register-accurate simulation against the real ROM bytes, and rendering,
both standalone and through the real ported `Console`/`VDP` pipeline.

This file was originally called "vehicle sprites" on the strength of a
render using the *wrong* 16x16 layout (the naive row-interleaved guess);
fixing that to the correct hardware-sprite quadrant order (`Sprite16Mono`,
top-left/bottom-left/top-right/bottom-right) turned it to noise instead —
see the correction note below. It turns out to use a **third** layout,
different from both of those: four 8x8 background pattern-table tiles in
plain raster order (top-left, top-right, bottom-left, bottom-right),
implemented as `internal/msxgfx.BigChar16Mono`.

The name-table placement script that finally settles what the full 33
tiles spell was found in **phase 3** of `stateAttractSequence`
($40CF-$40E6, specifically its `$49C2`/`$49EC` callee) — not phase 2,
which only loads the pattern/color data (see that phase's section below).
`$49EC` is a shared routine, entered twice back-to-back (once by an
explicit `CALL`, once by falling straight through into its own entry
point from the second call site's setup code — a 3-byte code-size trick,
not a bug): each entry does 3 outer passes 32 name-table bytes apart, and
within each pass, an inner loop -- gated to run `$20 - E004` times, so it
grows by one iteration per frame as `$E004` counts down from `$1F` to
`$07` -- writes one character code per iteration from a small ROM table,
terminated by a `$FF` sentinel byte. The two entries differ in which
direction they walk: the first (`C`=`$11`=17, `LD DE,$4B0C`) walks
*backward* (net `-1` per iteration — traced from `$4A09`-`$4A13`: one net
`INC` cancelled by two `DEC`s), the second (`C`=`$15`=21, `LD DE,$4B2F`)
walks *forward* (net `+1` — the `DEC`s are skipped here). Both tables'
3-pass consumption ranges overlap in ROM, which looked like a
contradiction at first — it isn't one; the two scans genuinely share
overlapping bytes by design, a byte-golfing trick typical of this era.
More surprising: the backward scan doesn't stop at `$4B0C` either — later
frames walk straight past it into bytes this project's own disassembler
had flagged as undecoded/code-like garbage (`$4B00`-`$4B0B`), which turn
out to simply be more of the same table. The actual data spans a single
contiguous **114-byte** ROM window, `$4AFC`-`$4B6D`, covering everything
either scan touches across all 25 frames — confirmed by instrumenting the
simulation to record the address range it actually reads, then rendering
with exactly that window and nothing more.

Simulating this precisely (reading straight from the ROM, not
hand-transcribed, across all 25 frames of the `$E004` countdown, onto
VRAM already carrying phase 2's loaded pattern/color) and rendering the
result reads unambiguously as two lines: **"ROAD"** and **"FIGHTER"**,
each in the red/light-red palette entry phase 2 paints first. This
confirmed both the full text (settling the "reads ROAD..." open question)
and that `$49CB`/`$49EC` — not anything in phase 2 or phase 4 — is the
placement script (`$49C2` itself starts slightly earlier; see below).
**Ported** as `revealRoadFighterTitle` in `internal/game/graphics.go` (the
114-byte window embedded as `roadFighterRevealTable`), called from
`stateAttractSequence`'s phase-3 branch in `state.go` for as long as
`$E004`'s countdown is `>= 7`; running it through the real `Console`/`VDP`
pipeline reproduces the identical "ROAD FIGHTER" render. Phase 3's other
branch (`$E004 < 7`: `TriggerSound(3)` then advance to phase 4) is also
ported. Its `$5028` tail-jump is also ported — see the dedicated section
below.

#### `$49C2`'s missing lead-in: "©KONAMI 1985" / "PLAY SELECT" / "LEVEL A" / "LEVEL B"

**High confidence, fully confirmed and ported.** The initial phase-3 port
above only translated `$49CB` onward — `$49C2`'s own first two
instructions (`$49C2`-`$49CA`: `LD DE,$472A` then two `CALL $45F0`) were
missed entirely, which meant the real game's four lines of attract-screen
text never appeared. Caught by actually running the game and comparing
against what the real attract screen is expected to show.

`$45F0`-`$4606` is a **different** small interpreter from `internal/rle`'s
VRAM-blit format, despite superficially similar control bytes (worth
flagging explicitly, since confusing two similar-looking formats is
exactly the kind of mistake this project has made before — see `internal/
rle`'s `$80` correction). Register-traced precisely: `DE` points at a
2-byte little-endian **absolute VRAM address**, then character codes
follow, written one at a time and advancing the destination by one each
time, until a control byte: `$FE` reads a fresh 2-byte destination and
continues, `$FF` stops that call. A second `CALL $45F0` with no
intervening `LD DE` (exactly what `$49C2`/`$49C8` does) simply continues
from wherever the first call's `$FF` left `DE`, so the two calls together
walk one continuous, `$FF`-delimited chain of text runs.

The data (`$472A`-`$475A`, 49 bytes) decodes — via the same confirmed font
mapping as the "SOFTWARE" credit (`$10+N`=digit `N`, `$21+(L-'A')`=letter
`L`, `$00`=space) plus one new code, `$1A` (one past the last digit,
confirmed by rendering `font_8x8.bin` at full size to be a **©** glyph,
the first of the "couple of icons" this project's font notes already
flagged as present but unidentified) — into four lines, each at its own
retargeted destination:

| Text | Destination |
|---|---|
| `©KONAMI 1985` | `$394A` (the same address `showSoftwareCredit` used for its rule/"SOFTWARE" text — phase 3 overwrites it) |
| `PLAY SELECT` | `$39AB` |
| `LEVEL A` | `$3A0D` |
| `LEVEL B` | `$3A4D` |

Ported as `drawTextRun` (the generic interpreter) and `drawAttractMenuText`
(the two calls plus the embedded `attractMenuText` data), called from
`stateAttractSequence`'s phase-3 branch immediately before
`revealRoadFighterTitle`, every frame — confirmed correct by rendering
through the real `Console`/`VDP` pipeline mid-reveal: "ROAD FIGHTER"
(partially revealed) above "©KONAMI 1985" / "PLAY SELECT" / "LEVEL A" /
"LEVEL B", matching the real game's attract screen exactly.

One transcription bug caught along the way: a first attempt at simulating
`$45F0` advanced the read cursor by 3 bytes past each 2-byte destination
(reasoning by analogy from `$460B`'s own entry code, which really does
need a `+3`) instead of the `+2` this routine's own register trace calls
for. That silently ate the first character after every retarget, rendering
"PLAY SELECT" / "LEVEL A" / "LEVEL B" as "LAY SELECT" / "EVEL A" / "EVEL B"
until the render was checked and the off-by-one found and fixed.

#### `$5028` — the blinking "finger" level-select cursor (**not** a large demo driver)

**High confidence, fully confirmed and ported** — this corrects an
earlier, wrong assessment in this document. `$5028` is what phase 3's
`>= 7` branch tail-jumps to every frame, and it was initially assumed
(from proximity alone — it sits right before a routine that clears 846
bytes of RAM and branches through a 6-entry jump table into several
untraced routines, at `$504C`) to be a single, large "self-playing demo
driver" not worth porting. That assumption turned out to be wrong on two
counts: `$5028` and `$504C` are two **separate** routines, and `$5028`
itself is small.

Register-traced precisely: `$5028`-`$502D` tests bit 3 of `$E004` (the
same countdown phase 3 is already decrementing for the title reveal) to
pick `C = $FF` or `$00`; `$5032`-`$503D` sets up `HL`/`DE` as the
name-table addresses 3 columns before "LEVEL A" (`$3A0A`) and "LEVEL B"
(`$3A4A`), then swaps them if `$E03B` (the level-select flag — see below)
is nonzero, so `HL` always ends up as the *selected* line's cursor
position; `$503F`-`$5045` draws the cursor at `HL` with the bit-3-derived
`C`, then draws it again at the *other* (unselected) position with
`C` forced to `$00`. Both draws reuse `$45F0`'s own inner write loop
(`$45F8` — see the text-drawing section above) via a direct `JP`,
skipping its usual "read a destination from the table" preamble since
the destination is already in `HL`; the 3-byte draw data itself
(`$475B`-`$475D`: `$1B $1C $FF`) is font pattern tiles 11-12 — the two
right after the `©` glyph — terminated the same way `$45F0`'s other data
is. `C = $00` blanks a draw entirely, via the shared loop's `AND C` right
before every `WRTVRM` — the same masking trick used to erase rather than
needing a separate "clear" code path.

Critically: `$5040`'s `CALL $5046` is a genuine call/return pair, and the
second draw (`$5044`-`$5049`) falls through into `$5046`'s body without
a further `CALL` — so *that* draw's eventual `RET` (on hitting the data's
`$FF` terminator) unwinds the stack all the way back to whatever called
`$5028` in the first place. Execution never reaches `$504C` at all from
here; that RAM-clearing/jump-table code's actual call site turned out to
be `stateGetReady` (game state 2, `$4137`-`$4166`) instead — see that
state's own section below — reached whenever `$E001=0` entering that
state, not from `$5028` or anywhere in the attract sequence.

Ported as `drawLevelSelectCursor` in `internal/game/graphics.go`, called
from **two** sites, both confirmed: phase 3 right after
`revealRoadFighterTitle`, and `stateAttractWait` (game state 1,
`$412D`-`$4136`, see below) — its own `$E004` countdown left at 0 by
phase 4's exit, wrapping to `$FF` for a further ~255-frame/4.25s window
of blinking once the title reveal and color-flash are both done. Missing
this second call site initially (`stateAttractWait` was still an empty
stub) is exactly why the cursor appeared to stop blinking once "ROAD
FIGHTER" settled — not a bug in `drawLevelSelectCursor` itself, which was
correct from the start, just a second reachable call site that hadn't
been wired up. Confirmed headlessly, frame-by-frame, against the real
`Console`/`VDP` pipeline in both states: the cursor blinks in the exact
pattern `$E004`'s bit 3 produces as it counts down (roughly 8 frames on,
8 off), and — now that `stateExtra43C3` is ported (see the dedicated
section below) — genuinely moves between LEVEL A/B in response to real
input, confirmed headlessly end-to-end.

#### `$43C3` — the missing "press fire to start" and `$E03B` write site, found by hand

**High confidence, fully confirmed and ported.** This single, previously
totally unexplored routine turned out to be both long-standing open
questions at once: what sets `$E03B` (LEVEL A/B selection) from player
input, and what actually
starts the game once the player is ready.

It was unexplored for a structural reason, not an oversight:
`RunGameStateMachine`'s dispatch ($4086-$4090) conditionally does
`PUSH $43C3` before jumping into whichever state handler is current —
manufacturing a return address, not a `CALL`. Whichever handler's own
`RET` fires (if `$E002` bit 6 is clear) lands back in `$43C3` as a
"continuation" that runs *after* the state's own per-frame logic, every
frame, for as long as bit 6 stays clear — i.e. for as long as no game
has been started this session. `cmd/disasm`'s recursive descent has no
way to discover a target reached only via a manufactured return address
like this, so it never reached these bytes; the region shows up as raw,
undecoded data in `docs/listing.asm`. Found only by hand-disassembling
directly from the ROM file byte-by-byte, after every other lead (raw PPI/
PSG port I/O, every existing `$E008`/`$E009`/`$E03B` read site) came up
empty.

Traced in full ($43C3-$4406, plus the `$4407`-`$440B` tail):

1. Samples input completely independently of `UpdateInputAndTimers` —
   its own edge-triggered pair at `$E039`/`$E03A` (via
   `updateEdgeTriggeredPair`, the same shared tail `$46B6` uses, just with
   a different RAM pair), not `$E008`/`$E009`. If nothing was freshly
   pressed this frame, returns immediately (`$43CD`: `RET Z`) — which is
   exactly why porting this routine didn't disturb any previously-verified,
   purely-timing-driven test: none of them ever simulated input, so this
   always took the immediate-return path.
2. If something was freshly pressed, `$E004` gets unconditionally zeroed
   (`$43CE`-`$43D3`) — an `HL`-construction trick in the original (builds
   `HL=$E000` cheaply from `H=$E0`, already in place, plus a fresh `L=0`
   read back from the very byte just zeroed), with the side effect of
   genuinely zeroing `$E004`. Confirmed intentional, not incidental: for
   game state 1 (`stateAttractWait`), this resets its own `$E004`-based
   idle/blink countdown, so interacting with the level-select menu
   extends the wait rather than letting attract mode time out mid-
   interaction.
3. In game state 1 (`stateAttractWait`, showing "PLAY SELECT"/"LEVEL A"/
   "LEVEL B"): if either fire button was freshly pressed (`$43DA`-`$43DC`:
   `AND $30`, bits 4-5 — see the input-pipeline section below for how
   those bits map to Space/Right-Shift), commits the current selection
   into `$E002` as `$40` or `$60` depending on `$E03B` (bit 6 set either
   way — this is exactly what stops `$43C3` from running at all once a
   game has started), transitions to game state 3 (`stateGetReadyWait`),
   and draws the level-select cursor one final time forced fully visible
   (`$5032` — a second entry point into the same `$5028` routine, skipping
   its own blink-timer check — ported as `drawLevelSelectCursorAt(true)`)
   rather than following its normal blink timer, confirming the choice.
   Any *other* freshly-pressed input (practically, up/down on this 2-item
   menu — though the check is really just "not fire", so left/right also
   toggle it, not a bug, just how simple the original is for two choices)
   instead toggles `$E03B` directly — **the previously-missing write
   site**.
4. In any other state (practically, only game state 0's attract-mode
   cycle, since bit 6 stays clear only until a game starts): any input at
   all skips straight to game state 1, retriggering sound id $1B (plays
   nothing — see `InitVDPAndSoundEngine`'s doc comment) via
   `TriggerSound($1B)` — i.e. "press any key to skip the demo," standard
   arcade convention — then (`$43FA`-`$4401`: `CALL $49BF`) immediately
   draws the complete "ROAD FIGHTER" title and menu (`showFullTitleAndMenu`,
   below), so the skip lands on a fully-drawn screen rather than an empty
   one.

Ported as `stateExtra43C3` in `state.go`, fully — including
`showFullTitleAndMenu` ($49BF-$4A24), which one pass at this genuinely
got wrong and is worth recording as its own correction: `$43FF`'s
`CALL $49BF` was first misread as a call to a nearby but *unrelated*
address glanced at around the same time while tracing other routines, and
left as an unported TODO on that mistaken identification. `$49BF` is
actually `CALL $49A6` — `showRoadTitle`'s own entry point — followed by a
fall-through (once that call's internal tail-jump chain eventually
`RET`s) straight into `$49C2`/`drawAttractMenuText` and then
`$49CB`/`revealRoadFighterTitle`, called with `$E004=7` (the exact value
`$43FA` just set, and the same value phase 3's own reveal loop uses on
its *final* frame — which is exactly why it produces the fully-revealed
title in one shot rather than a partial one). Without this, skipping the
attract sequence left game state 1's blinking-cursor wait running over
whatever was on screen before (the Konami logo, still on a blue
backdrop) instead of the title screen — caught by actually running
`cmd/play` and comparing the result to real hardware, then fixed by
re-reading the call target correctly.

Verified headlessly, end-to-end, five ways: (a) the purely-timing-driven
boot sequence still reaches game state 4 with zero simulated input, no
regression; (b) pressing Up at the level-select menu toggles `$E03B`;
(c) pressing Space (fire) at the menu commits the selection into `$E002`
and transitions to game state 3; (d) pressing any key during the Konami
logo/title (game state 0) immediately skips to game state 1 *and*
correctly draws the full title/menu, backdrop switching to black in the
same frame — confirmed both via VDP register state and by rendering the
frame, matching real Road Fighter's skip behavior exactly. Also confirmed
visually: forcing the confirmation draw (`drawLevelSelectCursorAt(true)`)
renders the finger cursor fully visible at the moment fire is pressed,
not mid-blink.

**The game is now genuinely startable from the keyboard** — the
practical payoff of this whole investigation, from "why does the finger
keep blinking" through the input-pipeline BIOS identifications to this.

**Phase 4 is also ported.** It reuses `paintRoadTitleColor` (see the
`$4112` palette above) with the *live* `$E004` countdown value as the
selector instead of phase 2's fixed `1`, repainting the title's color
once per frame for 32 frames — a color-cycling flash on the finished
title. Once that countdown reaches zero, it doesn't advance
`stateAttractSequence` to a further sub-phase at all: it exits the
attract-sequence state entirely, advancing the *top-level* game state
(`$E000`, 0 → 1) and resetting `attractPhaseByte` to 0 for whenever state
0 is re-entered. Confirmed headlessly frame-by-frame: color cycles through
the palette as expected, and the state transition fires on exactly the
frame the countdown hits zero.

### Pattern/color pairs — confirmed by rendering in real color

All 10 pairs below were confirmed correct by rendering pattern+color
together with `internal/msxgfx.TileRGBA` (`cmd/gfxpreview -color`, or
`cmd/gfxviewer` with `C`) and checking the result is a coherent image, per
the VRAM map above (destination `$2xxx` = pattern, `$0xxx` = color; same
low-order offset between a pair's two destinations).

| Pattern file | Color file | Dest (pat/color) | What it actually is |
|---|---|---|---|
| `hud_pattern_speed_fuel_panel_5DEC.bin` | `hud_color_speed_fuel_panel_5F25.bin` | $2200 / $0200 | **The HUD dashboard panel** — renders with the literal text "SPEED" and "FUEL" plus decorative icons (a tree, a sun/wheel, a flag-like red shape). This is what broke the case: rendering it colorized was unambiguous proof of both the pairing and the corrected VRAM map. |
| `hud_pattern_gauge_bars1_5FEC.bin` | `hud_color_gauge_bars1_601D.bin` | $2680 / $0680 | Gray/white vertical bar segments — plausibly part of a gauge (speed/fuel meter) graphic |
| `hud_pattern_gauge_bars2_5FF7.bin` | `hud_color_gauge_bars2_6022.bin` | $26C8 / $06C8 (color reused at $0708) | Gray-to-white gradient bars with a black notch — also gauge-like |
| `hud_pattern_gauge_bars3_6008.bin` | `hud_color_gauge_bars3_602D.bin` | $2748 / $0748 | Green/white/black bars — another gauge or meter variant |
| `ui_pattern_diagonal_border_63AC.bin` | `ui_color_diagonal_border_67C2.bin` | $2588 (also transiently $2400) / $0588 | Red/blue diagonal shapes — reads as a decorative border or divider pattern, not obviously a specific game object |
| `scenery_pattern_tree_bush_6645.bin` | `scenery_color_tree_bush_6737.bin` | $2400 / $0400 (color reused at $0468) | Green speckled foliage texture on a red/pink background — a roadside tree or bush |
| `scenery_pattern_tree_road_6697.bin` | `scenery_color_tree_road_675E.bin` | $24D0 / $04D0 | More foliage plus gray road-surface-like tiles and a white "H"-cross shape on red — mixed scenery/UI, not fully identified |
| `map_pattern_terrain_tileset_74BC.bin` | `map_color_terrain_tileset_7638.bin` | $2400 (later reuse) / $0400 (later reuse) | **A stage-map terrain tileset** — sky with clouds, grass, sand, water, gray winding roads, trees, a red car icon, a magenta gate/door, and railroad-crossing-sign tiles (black cross-hatch). Reads as a course-overview/map screen. |
| `hud_pattern_gauge_bars1_mirrored_601F.bin` | `hud_color_gauge_bars1_mirrored_601F.bin` | $26A8 / $06A8 | Gray/white bars, same family as `gauge_bars1` — see below, this pattern isn't a fresh ROM stream |
| `hud_pattern_gauge_bars3_mirrored_6031.bin` | `hud_color_gauge_bars3_mirrored_6031.bin` | $2798 / $0798 | Green/white/black bars, same family as `gauge_bars3` — see below |

The last two pairs' *patterns* aren't independent RLE streams — they were
originally two "color data with no located pattern companion" entries
(`$601F`, `$6031`) until tracing the code around them turned up why: the
game derives their pattern at runtime via the separate VRAM-mirroring
routine at `$4652` (`SETRD`/`SETWRT` plus the bit-reversal loop at
`$466E`, i.e. a per-byte horizontal flip), applied to a subrange of an
*already-decoded* pattern, rather than decompressing fresh ROM bytes:

- `$601F`'s pattern is tiles 1-4 (bytes 8-39) of
  `hud_pattern_gauge_bars1_5FEC.bin`, bit-reversed per byte
  (`68CE`-`68D6`: mirror `$2688→$26A8`, `C=4`).
- `$6031`'s pattern is tiles 3-8 (bytes 24-71) of
  `hud_pattern_gauge_bars3_6008.bin`, bit-reversed per byte
  (`691A`-`6922`: mirror `$2760→$2798`, `C=6`).

Confirmed by computing that transform and rendering the result with each
color file — both produced clean bar graphics consistent with their
source family's style, not noise. The two derived pattern files above are
saved for viewer convenience (`cmd/gfxviewer`'s `colorPairs` map points at
them) but are reconstructions, not independently-extracted ROM bytes;
their true "source" is the pattern file named in the byte ranges above.

**Follow-up: four more derived files, plus three synthesized sprite
atlases, added so every known pattern/sprite/meta-tile is actually
viewable in `cmd/gfxviewer`.** The same `$4652` mirror routine
(`internal/game/graphics.go`'s `mirrorPatternRows`/`mirrorSprite16`, both
already ported) has three more call sites whose *output* had no
standalone file before: `hud_pattern_gauge_bars2_mirrored_computed.bin`
(whole-file mirror of `hud_pattern_gauge_bars2_5FF7.bin`, reusing its
*normal* color file unchanged — the ROM does the same, see
`initHUDAndTrackGraphics`'s `"$68FF-$6907: same color asset, reused"`),
`ui_pattern_diagonal_border_mirrored_computed.bin` (whole-file mirror of
`ui_pattern_diagonal_border_63AC.bin`, color likewise reused unchanged),
and `scenery_pattern_tree_bush_mirrored_computed.bin` (whole-file mirror
of `scenery_pattern_tree_bush_6645.bin`, color likewise reused). All
three verified by rendering pattern+color together — clean, coherent
horizontally-flipped tile sets, not noise (see git history for the
verification renders). A fourth, `sprites_vehicles_mirrored_computed.bin`,
is `mirrorSprite16`'s block-swapping mirror (not `mirrorPatternRows`'s
simple per-byte one) of `sprites_vehicles_5A73.bin`'s bytes 128-319
(`$6934`: `mirrorSprite16(0x1880, 0x1940, 6)`) — six 16x16 hardware
sprites, verified by rendering them as part of the traffic-object atlas
below and getting recognizable vehicle silhouettes, not garbage.

None of these four are independent ROM streams (same caveat as the
`_601F`/`_6031` pair above), aren't `//go:embed`ed in `assets/assets.go`,
and aren't consumed by the running port — they exist purely so
`cmd/gfxviewer` can display byte ranges the game only ever assembles at
runtime. See `cmd/gfxviewer`'s `colorPairs` map for how they're paired.

Separately, three lookup tables in `internal/game/gameplay.go` —
`trafficSpritePatternByType`, `metaTileColors`, `carSpritePatternTable`,
and `metaTileDefinitions` — map small integer IDs (an object "kind" 0-8,
a player steering/crash-spin state 0-10, a level-layout "meta tile" ID
1-15) to actual rendered sprites, but had no visual form anywhere in the
tooling; reading the tables told you *that* a mapping existed, not what
it looked like. `cmd/gfxviewer/atlas.go` now duplicates those four tables
(cmd/gfxviewer is a separate `package main`, doesn't import
`internal/game`) and appends three synthesized, non-file-backed entries
after the directory's own assets:

- `atlas_traffic_object_kinds` — all 9 kinds as their real colored 16x16
  sprite pair (two hardware sprites at the same position, the
  lower-priority one showing where the higher doesn't — confirmed via
  `positionTrafficSprite`/`spawnStagedTrafficObject` always writing a
  `pattern`/`pattern+4` pair at identical row/column with two different
  color bytes). Rendered result: recognizable green/blue/magenta/red cars
  and trucks, a black-and-yellow oil-slick-looking object (kinds 4-5), and
  a pink heart with a "B" (a bonus-score icon) for kind 8 — all clearly
  real game objects, not noise.
- `atlas_player_car_steering` — all 11 `carSpritePatternTable` entries
  the same way, fixed colors `$01`/`$06` per `carInitTable10E`. Rendered
  result: entry 0 is a straight-on car, entries 1-7 show a clear
  progressive left/right steering rotation, and entries 8-10 turn into a
  scattered/chaotic pattern consistent with the crash-spin animation.
- `atlas_meta_tile_definitions` — all 15 `stageMetaTileIDs` values as the
  two objects they actually stage (`stageObjectRecord`'s own two 3-byte
  half-defs, byte 0 of each being the "kind" fed to the traffic-object
  atlas above), side by side. Rendered result: coherent object pairs
  (e.g. two green cars, a car plus the bonus heart, a car plus an oil
  slick) — matches what a level layout entry should plausibly spawn.

All three were rendered headlessly (a standalone throwaway `package main`
outside the module's own commands, since importing `ebiten` at all panics
with no display attached, per this doc's own Tooling section on
`cmd/play`/`cmd/gfxviewer`) and visually confirmed before trusting the
lookup-table math, not just assumed correct because it compiled.

To re-inspect a candidate visually (raw, uncompressed assumption):

```bash
go run ./cmd/gfxpreview -rom "Road Fighter - Konami (1985) [7349].rom" \
  -start 5998 -len 128 -tile 8 -cols 8 -scale 10 -out preview.png
```

**Correction note (font, two rounds):** the font was originally extracted as
248 raw bytes read directly off the ROM at `$478A`, on the assumption that
MSX character patterns are always flat 8-bytes-per-glyph. Visual inspection
looked plausible at a glance but a few glyphs had a cut-off top row or
looked like noise — the ROM data was RLE-compressed (see above), not raw.

The first fix decoded the stream but had the repeat/literal branches
backwards (see the format section above), which produced a *second*,
smaller-scale version of the exact same symptom: some glyphs decoded
correctly (a repeat run of zero bytes decodes the same either way you
interpret it), others didn't, terminating early at 116 decoded bytes (~14
glyphs, digits and a few symbols only). The tell that something was still
wrong: digits like 4-8 looked clean while others didn't, and there was no
letters A-Z at all despite the game clearly needing them for on-screen
messages. Fixing the branch swap and re-decoding produced 344 bytes — a
complete, clean 43-glyph set including A-Z — confirmed by rendering it (not
just by "decodes without error" or "terminates on the 0x00 marker", both of
which the first, still-broken decode *also* satisfied).

The lesson generalizes twice over: for this ROM, neither "a `DATA` block
looks roughly right as raw fixed-width tiles" nor "it decodes without error
under my understanding of the compression format" is sufficient
confirmation on its own — the actual bar is rendering the *entire* decoded
result and checking it's a complete, clean set, not just spot-checking that
it looks less obviously wrong than before.

**Correction note (VDP table):** an earlier pass mis-labeled `$46A9` as
sound data purely from its address being near the sound driver; closer
reading showed it's fed byte-by-byte into `WRTVDP` with an incrementing
register number — i.e. it's the VDP init table, not audio. Kept here as a
reminder that proximity to a known routine isn't enough evidence on its
own; the asset table above only lists things confirmed by an actual
call-site trace, a visual check, or (now) a decode that terminates cleanly.

**Correction note (pattern vs. color, backwards):** the first pass at the
18 non-font streams assumed the *textbook default* Screen 2 layout
(pattern at `$0000`, color at `$2000`) and labeled destinations
accordingly — calling `$5F25` (dest `$0200`) "high-confidence pattern"
because it rendered as clean bars, and its real pattern companion `$5DEC`
(dest `$2200`) "medium-confidence color" because it looked blockier. That
was backwards on both counts. Color bytes fed through a *monochrome*
tile renderer (as `gfxpreview` does without `-color`) can easily look like
a clean, plausible "shape" by coincidence — a repeated color byte across
many rows renders as a solid or striped block, which is indistinguishable
from a real drawn shape without actually compositing pattern+color
together and checking the *result* makes sense. That's what the VRAM map
above was derived to fix: computing the actual pattern/color base
addresses from this game's own VDP register values (not assumed defaults),
then confirming it by rendering `$5DEC`+`$5F25` together and getting
literal readable text ("SPEED"/"FUEL") — about as unambiguous as
confirmation gets. Every one of the 18 streams was relabeled against the
corrected map; see the pairs table above for the current (correct) roles.

**Correction note (16x16 sprite layout, backwards):** the original
`gfxpreview`/`gfxviewer` treated a 32-byte 16x16 tile as 16 rows of 2
bytes (left half then right half, per row) — a natural-sounding guess, and
wrong. MSX1/TMS9918A hardware addresses a 16x16 sprite pattern as four
independent 8x8 patterns in a fixed order: top-left, bottom-left,
top-right, bottom-right (`internal/msxgfx.Sprite16Mono`). The two layouts
produce very different pixels from the same bytes, but *both* produce
"something" — 8 real rows of pixels exist either way, just sliced up
differently — so a wrong layout doesn't obviously look broken the way,
say, a misaligned RLE decode does; it just looks like a plausible if messy
sprite. This is exactly what happened: `sprites_vehicles_5A73.bin` looked like
vague noise under the old layout and clean car silhouettes once fixed
(confirming the fix), but `bigfont_pattern_road_4A25.bin` (then labeled
"vehicle sprites") had it backwards — it looked like clean car silhouettes
under the *old, wrong* layout, and turned to noise under the corrected
hardware-sprite layout. Its "vehicle sprites" label was walked back to
unresolved/low-confidence at that point — correctly, since that specific
guess (hardware sprite quadrants) was wrong, but the file wasn't actually
unreadable, just misinterpreted a *third* way: as background tiles in
plain raster order it reads as text (see the dedicated section above).
Same general lesson, twice over now: a render "looking like something" is
not confirmation by itself, especially for a format with more than one
plausible byte-ordering — check whether the
*specific* layout is justified (here: destination address, VRAM table
identity) before trusting what it produces.

**Correction note (sound data RLE question, resolved):** an earlier
next-step flagged whether `note_table_and_sound_scripts_4DD6.bin` might
also be RLE-compressed, unchecked. It isn't, and this didn't need a decode
attempt to settle — it follows directly from the calling convention:
every confirmed RLE stream in this ROM is consumed by routing it through
`$4611`/`$45E0`/`$460B` first, and `triggerSoundRaw`/`installSoundRequest`
(`sound.go`, ported from `$4B76-$4BD1`) read this table with plain `LD
A,(DE)` against the ROM address directly — no decompressor call anywhere
near that code path (checked across `$4C00-$4E2F`). Data a program reads
directly can't be compressed in a format the program never decompresses.

## Go port (`internal/game`, `internal/msx`)

`internal/msx.System` is a small interface standing in for the MSX BIOS
calls the original code used (`WRTVDP` $0047, `WRTPSG` $0093, `FILVRM`
$0056, `LDIRVM` $005C, DI/EI). `internal/msx.VDP` is a concrete, in-memory
implementation of it — real VDP registers and 16KB of VRAM, with no
display of its own. `cmd/play` wires a `VDP` up to a `game.Console` and
renders whatever ends up in VRAM every frame with Ebitengine (reading the
name table, then each cell's pattern+color bytes via the derived table
bases, composited through `internal/msxgfx` exactly like `cmd/gfxviewer`
does for a single static asset) — i.e. the port actually **runs**, not
just compiles. Most of the state machine is still stubs, so what's on
screen right now is whatever's been ported below; see "What's not yet
ported" for the rest.

`WritePSGRegister` calls are backed by a real audio device —
`internal/psg.Chip` (see "Tooling") is a genuine, verified real-time PSG
synthesizer, not a stub. Both `cmd/play` and `cmd/sndpreview` wire it in,
via the same `soundSystem`-wrapper-plus-`chip.Tick()`-per-frame pattern
(see `psg.Chip.Tick`'s own doc comment for why an explicit tick clock,
not wall-clock time, is what keeps audio generation from racing ahead of
what the game has actually decided should be playing — ebitengine's audio
backend can read up to ~0.5s of audio ahead of real playback in a burst).
The sound engine itself (`TriggerSound` through `TickSoundChannels`/
`advanceSoundChannel`) is fully ported — see item 6 above — so this is now
end-to-end audible when actually playing the game, not just in
`cmd/sndpreview`'s standalone testing harness.

Where the original decompresses an RLE graphics stream into VRAM
(`CALL $45E0` etc.), the port loads the matching pre-decoded bytes from the
`assets` package (`//go:embed` over `assets/gfx/*.bin`) via `WriteVRAM`
instead of reimplementing `internal/rle` as part of the running game —
that decompression already happened once, producing the asset files;
loading the result is the faithful *net effect*, not a shortcut around
anything not-yet-understood.

**Correction (sound channel RAM addressing):** `soundChannelBase` in
`sound.go` was defined as `0x010`, while its own doc comment correctly said
"$E010" — the `0xE000` RAM base was missing from the actual constant. This
went unnoticed until `TickSoundChannels` actually *ran*: `readRAM`
subtracts `0xE000` from its argument, so a small offset like `0x010`
underflowed to a huge `uint16` and panicked on the RAM slice bounds. A
reminder that a port can type-check and even look right on inspection
while still being wrong in a way only running it surfaces. Fixed;
`soundChannelBase = 0xE010` now matches its own doc comment.

Ported so far, with original addresses noted in each function's doc comment:

- **Boot / per-frame hook** (`boot.go`): `ColdBoot` ($404F-$4037) and
  `VBlankHook` ($4013-$4037), including the reentrancy guard and the
  install of the H.KEYI hook. `CALL $013E` is `RDVDP` (confirmed against
  a full MSX BIOS reference — an earlier note here that its target fell
  outside the standard jump-table range was simply wrong), ported as
  `ReadVDPStatus`: it reads the VDP status register, whose bit 7 is the
  frame-interrupt-pending flag, matching exactly how `VBlankHook` uses
  its result (`int8(status) < 0`, an overrun/frame-skip check). Always
  returns 0 in this port — correct and complete, not a placeholder, since
  the condition it would detect (a second frame interrupt arriving mid-
  frame) can't occur in this synchronous, host-frame-driven execution
  model; see `ReadVDPStatus`'s own doc comment for the full reasoning.
- **Input pipeline** (`input.go`): `$46C3`-`$46FB` and `$46B6`-`$46C2`
  (`UpdateInputAndTimers`) are now fully ported, unblocked by three BIOS
  identifications: `WRTPSG` ($0093, register in `A`, value in `E`),
  `RDPSG` ($0096, register in `A`, value out in `A`), and `SNSMAT`
  ($0141, row in `A`, value out in `A`) — all confirmed against a full
  MSX BIOS reference (`WRTVDP` $0047 and `RDVDP` $013E were checked the
  same way and matched what this project already had). `readCombinedInput`
  reads PSG register 14 (the AY-3-8910's I/O Port A, which on MSX carries
  joystick state, not sound data — after configuring things via
  `WritePSGRegister` on register 15) and two keyboard matrix rows (7 and
  8), merging all three into one byte via a chain of `CPL`/`RRCA`/`AND`/
  `OR` operations. Translated as an exact, mechanical instruction-by-
  instruction mirror rather than worked out algebraically by hand, then
  verified two ways: an independently-written from-scratch re-simulation
  of the same instruction sequence agreed with the ported Go across
  ~2200 input combinations, and a bit-probe (flip one matrix bit at a
  time, observe which output bit changes) confirmed exactly which final
  bit each physical key/joystick line lands on. `UpdateInputAndTimers`
  then derives `$E008` (edge-triggered "just pressed") and `$E009` (raw,
  level-triggered) from it, exactly as traced.

  The bit-probe result lines up perfectly with the standard MSX1 keyboard
  matrix layout: row 8 bit5/6/4/7 (UP/DOWN/LEFT/RIGHT) land on the
  combined byte's bits 0/1/2/3, row 8 bit0 (SPACE) lands on bit 4, and
  row 7 bit6 (SELECT) lands on bit 5 — exactly matching the joystick's
  own bit layout on the same 6 bits (up/down/left/right/fire1/fire2).
  I.e. the keyboard is wired to emulate a joystick: arrows steer, Space
  is fire 1. Confirms the translation is correct at the semantic level,
  not just the mechanical one.

  Added `msx.System.ReadPSGRegister` and `.SenseKeyboardMatrixRow`, plus
  a `KeyboardMatrix [11]byte` field on `msx.VDP`. Both `KeyboardMatrix`
  and PSG registers 14/15 default to all-`$FF` ("nothing pressed"/idle,
  matching real hardware's active-low wiring and pulled-high idle
  joystick lines) — this needed an explicit fix in `NewVDP`, since a
  first attempt left PSG 14/15 at Go's zero-value default despite the
  VDP doc comment already (incorrectly) claiming they were idle; since
  `readCombinedInput` complements (`CPL`) whatever it reads from register
  14, a `$00` default read as "every joystick button permanently held,"
  masking whether keyboard input was wired correctly at all until this
  was caught by an end-to-end verification and fixed.

  `cmd/play` now wires real keyboard state in every frame
  (`updateKeyboardMatrix`, called from `Update` before `VBlankHook`):
  arrow keys, Space (fire 1), and Right Shift (fire 2, standing in for
  SELECT, which has no obvious modern-keyboard equivalent). Verified
  headlessly end-to-end (`KeyboardMatrix` → `readCombinedInput` →
  `UpdateInputAndTimers` → `$E008`/`$E009`): each of the 6 mapped keys
  produces exactly its own isolated bit, idle reads as `$00`, and the
  normal boot sequence still reaches game state 4 with no regression.

- **Attract-mode input handler / "start game"** (`state.go`):
  `stateExtra43C3` ($43C3-$4406, plus the `$4407`-`$440B` tail) is now
  fully ported, including `showFullTitleAndMenu` ($49BF-$4A24, called
  when skipping the attract sequence). This is what actually reads the
  input the pipeline above computes and does something with it: toggling
  `$E03B` (LEVEL A/B selection) on non-fire input at the menu, committing
  the selection and starting the game (transition to game state 3) on
  fire, and skipping straight to the menu on any key during the
  logo/title — immediately drawing the complete title and menu rather
  than leaving the screen blank, care of `showFullTitleAndMenu`. Reused
  `updateEdgeTriggeredPair` (factored out of `UpdateInputAndTimers` once
  a second call site needed the identical shared tail with a different
  RAM pair, `$E039`/`$E03A` instead of `$E008`/`$E009`) and added
  `drawLevelSelectCursorAt` (a forced-visible variant of
  `drawLevelSelectCursor`, for the `$5032` confirmation-draw entry point).
  See the dedicated `$43C3` section under "Extracted assets" for the full
  trace, including why this routine was completely invisible to
  `cmd/disasm` (reached only via a manufactured return address, not a
  literal `CALL`/`JP`), how it was found anyway (hand-disassembling the
  ROM directly once every other lead came up empty), and a genuine
  transcription mistake caught by actually running it (misreading
  `$49BF` as a nearby unrelated address, briefly shipping the skip path
  without its title/menu draw). The game is now genuinely playable from
  the keyboard as a result.
- **Sound engine** (`sound.go`): `InitVDPAndSoundEngine` ($4685-$46A8) and
  the priority-based sound trigger `TriggerSound`/`triggerSoundRaw`
  ($407F, $4B6E, $4B76-$4BD1) are ported with high confidence — every
  branch and register was traced against the listing. `TickSoundChannels`
  ($4BFF-$4C24) captures the confirmed 3-channel, 11-byte-record loop
  structure. `advanceSoundChannel` (`$4CBC`, `$4D00-$4DD5`), the actual
  note-bytecode interpreter — a real custom tracker format (nibble-packed
  note/octave/duration, indexing the pitch table, writing PSG registers
  directly) — is now ported for both of its command formats, and so is
  `engineSoundPitchStep` (`$4C25`-`$4CB9`), the special continuous-pitch
  path for sound ID 1; see the dedicated write-ups under "What's not yet
  ported" for the full traces.
- **Top-level game state machine** (`state.go`): `RunGameStateMachine`
  ($4082-$4098) faithfully replaces the original's "CALL a trampoline that
  pops its own return address as a jump-table base" idiom with a plain
  `switch` over 9 states. The 9 state bodies ($40AB, $412D, $4137, $416F,
  $4270, $4306, $4325, $4348, $4373) are stubbed with their *confirmed
  control flow* (DJNZ sub-phases, which RAM flags they test, which
  functions they call) documented in comments, but the tentative names
  (`statePlaying`, `stateCrashSequence`, etc.) are **inferred hypotheses**,
  not confirmed against any external documentation — state 5 is the
  strongest guess ("playing") because it's the one that repeatedly drives
  the same per-frame routine ($5224) that the "get ready" state calls once.
  **State 0's first two sub-phases are now actually ported**, not just
  documented: `stateAttractSequence`'s 5-way DJNZ chain ($40AB, $40C0,
  $40CD, $40E6, falling through to $411A) was traced precisely (which
  values of `attractPhaseByte()` reach which branch, including *why* both
  0 and >=5 land on the same one — DJNZ's `B-1` underflowing 0 to `$FF`
  cascades through every jump the same way a literal >=5 does, confirmed
  by tracing the decrement at each site rather than assumed).
  The `==0 || >=5` branch (sound id $1B — plays nothing, see
  `InitVDPAndSoundEngine`'s doc comment — font setup, draw the Konami
  logo) and phase 1 (the logo's scroll-into-place animation, confirmed
  genuinely per-frame rather than a one-shot draw — see the correction
  under the Konami-logo section above — followed by the `$46FC`
  "KONAMI / -------- / SOFTWARE" credit decompress once the scroll
  settles) are both fully implemented (`graphics.go`: `SetupFont`,
  `ShowKonamiLogo`, `scrollKonamiLogoStep`, `initFirstSixteenChars`,
  `showSoftwareCredit`) and run for real against `internal/msx.VDP`.
  Phase 2 (the ~256-frame hold on the credit screen, then clearing the name
  table and decompressing+coloring the "ROAD FIGHTER" title —
  `showRoadTitle` and `paintRoadTitleColor` in `graphics.go`), phase 3 (the
  letter-by-letter title reveal plus "©KONAMI 1985"/"PLAY SELECT"/
  "LEVEL A"/"LEVEL B" plus the blinking level-select cursor —
  `revealRoadFighterTitle`, `drawAttractMenuText`, `drawLevelSelectCursor`),
  and phase 4 (the title's color-cycle flash, then exit to game state 1 —
  reusing `paintRoadTitleColor`) are now all fully implemented and run for
  real. The one functional gap: nothing yet sets `$E03B`, so the
  level-select cursor always sits at LEVEL A — see the dedicated section
  under "Extracted assets" (`$5028`) and "What's not yet ported" for what's
  been ruled out.

## What's not yet ported

**Status note (accurate as of 2026-09-02):** this section is a historical
snapshot from early in the reverse-engineering effort and is now largely
superseded — nearly everything it describes below as unported/TODO has
since been traced and shipped (the full state machine, 13 of
`GameplayTick`'s 14 callees, all 6 stages of `updateRoadAndScore`, the
`$70A8` object-slot system, the sound engine, and more). It's left in
place rather than rewritten, per this project's own convention of
appending correction notes instead of editing history away (see the
"Correction:"/"Follow-on correction:" sections elsewhere in this file for
the same pattern) — the register-by-register trace detail below is still
accurate even where its "TODO" framing no longer is. **For the current,
accurate list of what's actually still open, see "Next steps" at the top
of this file.** When citing anything below, treat every "not yet
ported"/"still unported"/"TODO" claim as a claim about the state *at the
time that paragraph was written*, and verify against the current code
first (`grep -n "func (c \*Console) <name>" internal/game/*.go`) — the
same discipline this project's own `CLAUDE.md` already asks of anyone
citing a ROM address from this doc.

The remaining ~4KB of confirmed code (physics/scrolling at `$51xx-$59xx`,
collision and scoring, the road-curve renderer, enemy/traffic AI) is
disassembled in `docs/listing.asm` but not yet translated into Go. The
functions each state handler calls out to (`$52D1`, `$5224`, `$51C5`,
`$526B`, `$597D`, `$4429`) are named as stubs in `state.go`/`sound.go`
where referenced, and are the natural next things to trace and port —
`docs/listing.asm` already has their disassembly; they just haven't been
read and translated yet. Also open, and *not* the same routine as
`$5028` despite sitting right next to it (see below): `$504C` onward,
confirmed to clear 846 bytes of RAM (`$E048`-`$E049`) and branch through
a 6-entry jump table (indexed via `$E043`, entries at `$50E7`/`$50E8`/
`$50EF`/`$510A`/`$5122`/`$50D5`) into at least 3 more entirely untraced
routines (`$51E5`, `$5132`, `$7020`) — clearly real level/gameplay
initialization, but its actual call site hasn't been found yet (nothing
this project has traced so far reaches it), so it can't be assessed for
scope, let alone ported, until that's located.

Closer to the surface: `stateAttractSequence` is now **fully ported**,
all 5 sub-phases (`$40AB`-`$4166`), including phase 3's complete
`$49C2`/`$5028` chain:

- `$40CF`-`$40E6`: decrements `$E004` (set to `$20` by phase 2) and tests
  the result against 7. While it's `>= 7` (25 frames, `$1F` down to `$07`),
  calls `$49C2` then tail-jumps (`JP`, not `CALL`) into `$5028`. Once the
  countdown drops below 7 (the last 7 frames), the real port calls
  `TriggerSound(3)` (`$778C`: `LD A,$03; JP $4B6E`) and advances to phase 4
  via `setGameState($20)`, exactly as traced.
- `$49C2` splits into two ported pieces: `$49C2`-`$49CA` (`drawAttractMenuText`
  + `drawTextRun`, a *different* small text interpreter than `internal/
  rle`'s VRAM-blit format) draws "©KONAMI 1985" / "PLAY SELECT" /
  "LEVEL A" / "LEVEL B"; `$49CB`-`$4A24` (`revealRoadFighterTitle`) is the
  "ROAD FIGHTER" name-table placement script (see the bigfont asset
  section above for the full trace, the 114-byte ROM window it actually
  reads, and the confirmed render). The text-drawing half was missed on
  the initial phase-3 port and only found by actually running the game
  and comparing against the real attract screen — see the dedicated
  section under "Extracted assets" for the full trace, including an
  off-by-one caught the same way.
- `$5028`-`$5049` (`drawLevelSelectCursor`) is the blinking "finger"
  cursor next to LEVEL A/B — see the dedicated section under "Extracted
  assets" for the full correction: this was originally assumed, from
  proximity to `$504C`'s RAM-clearing code alone, to be part of one large
  "self-playing demo driver" not worth porting. That assumption was wrong
  — `$5028` is small, self-contained, and confirmed (by tracing its exact
  `CALL`/`RET`/`JP` structure) to `RET` before ever reaching `$504C`.
  Reached from **two** call sites, both now wired up: phase 3's tail-jump
  (above), and `stateAttractWait` (game state 1, `$412D`-`$4136`,
  see below) — missing the second site was exactly why the cursor
  appeared to stop blinking once the title reveal finished, even though
  `drawLevelSelectCursor` itself was already fully correct.
- `$40E8`-`$4111`, phase 4, is fully ported: it repaints the title's color
  once per frame for 32 frames via the same `paintRoadTitleColor` tail
  phase 2 uses, but with the live `$E004` countdown value as the palette
  selector (a color-cycling flash), then — once the countdown hits zero —
  exits the *entire* attract-sequence state, advancing the top-level game
  state (`$E000`, 0 → 1) and resetting `attractPhaseByte` to 0, rather than
  handing off to a further sub-phase of its own. Confirmed headlessly
  frame-by-frame: the color cycles as expected and the exit transition
  fires on exactly the frame the countdown reaches zero.

**`stateAttractWait` (game state 1, `$412D`-`$4136`) is also now fully
ported.** It decrements `$E004` (left at 0 by phase 4's exit, so this
first wraps to `$FF` — a ~255-frame/4.25s wait) and, while it's still
nonzero, tail-jumps into `$5028` every frame, exactly like phase 3 does —
reusing `drawLevelSelectCursor` unchanged. This is what keeps the
level-select cursor blinking well after "ROAD FIGHTER" has finished
revealing and phase 4's color-flash has ended: it was never a gap in
`drawLevelSelectCursor` itself, just a second call site (this one) that
hadn't been wired up yet, since `stateAttractWait` was still an empty
stub. Once the countdown reaches zero, it exits to game state 2
(`stateGetReady`) with `$E004` reset to `$18` — the same
`exitAttractSequence` tail phase 4 uses, now factored out since both
call it identically (`$42F8`'s `LD A,$18` falling into the shared
`$42FA`-`$4305`). Confirmed headlessly: the cursor toggles visibility 32
times over the ~256-frame wait (an 8-frame period, matching `$E004` bit
3), then transitions to game state 2 with `$E004=$18` exactly as traced.

**`stateGetReady` (game state 2, `$4137`-`$4166`) and `statePlaying`
(game state 5, `$4306`-`$4324`) are ported control-flow, plus one real
callee (`GameplayTick`/`$5224`) that is itself mostly still TODOs.**
Every state's dispatch loads `$E001` into the DJNZ counter (`B`, via
`$4091`'s `LD BC,(E000h)` — confirmed to apply to every state, not just
`stateAttractSequence`), and both of these states reuse that same byte
for their own purposes, distinct from each other and from
`stateAttractSequence`'s.

`stateGetReady`, entering with `$E001=0` (what `stateAttractWait`'s exit
leaves it at), underflows the DJNZ decrement to `$FF` — the same trick
`stateAttractSequence`'s phase 0 uses — taking the branch that calls
`$52B8` (looks like a per-race lane/RNG seed, reading the Z80 `R`
register for entropy) then `$504C` (the large RAM-clearing/jump-table
routine whose call site was previously unlocated when tracing `$5028` —
see the correction note above), then transitions straight to game state
4 in the *same* frame, with no waiting. The other DJNZ branch (requiring
`$E001=1` on entry, not yet traced how anything sets that) calls `$52D1`
then `GameplayTick`, then either idles (crash flag set, game-over flag
clear) or resets back to game state 0.

`statePlaying`, with `$E001=1`, calls `GameplayTick` then checks the
game-over and crash flags: game over transitions to game state 8 with a
confirmed quirk (`$E004` ends up 0, not the `$18` `transitionTo` would
normally leave, because the original overwrites it immediately
afterward — reproduced exactly, not smoothed over); a crash with no
game-over idles; otherwise it advances to game state 6 via
`exitAttractSequence` — a good illustration that that helper's name is a
holdover from its first two call sites and it isn't specifically an
"exit to attract mode," it just always adds 1. The other DJNZ branch
(`$E001!=1` — the branch taken on ~255 out of every 256 frames, since
`$E001` decrements from 0 down to 0 before next satisfying `$E001==1`)
tail-jumps to `$527D`/`advanceCarBehavior`, itself another gameplay
dispatcher (`CALL $5270`/`$6A70`/`$6CA1`/`$738C`/`$5501`, then a per-lane
position-adjustment loop at `$528F`) — now ported control-flow-only, see
the dedicated section below for the full trace, including a genuine
second screen-drawing find (the "EMPTY" fuel-out message).

**`GameplayTick` (`$5224`-`$5250`) turned out to be the dispatcher for
essentially the entire gameplay engine, not a single physics routine as
hoped when it was first identified as a lead.** It's 14 sequential
`CALL`s — `$526B`, `$597D`, `$70A8`, `$5434`, `$7755`, `$6A70`, `$7374`,
`$738C`, `$5251`, `$6E4A`, `$5501`, `$7411`, `$6CA1`, `$6F25` — then a
tail-jump to `$44D9`, covering (going by a light read of each one's
first few instructions, not a full trace) road/traffic physics,
scrolling, scoring, and collision, reading and writing a web of
interconnected RAM state: `$E049` looks like a race-phase gate (zeroed
by `$504C` during gameplay init; several callees skip their real work
unless it's exactly 1), `$E078`/`$E079` form a 16-bit distance counter
several callees read or accumulate into, `$E0B8` and `$E083` look
related to the crash sequence, and `$E03B` (level select) feeds a
scroll-speed choice in one of them. Only the dispatcher itself and its
two shortest, fully self-contained callees are ported (in the new
`gameplay.go`):

- `blitSpriteAttributeTable` (`$526B`, plus its alternate entry `$5270`)
  copies bytes from RAM `$E10E` to VRAM `$3B00` — one byte past the
  visible name table, i.e. confirmed to have **no visible on-screen
  effect** (unused padding space, the same address `$4467` — part of
  `showRoadTitle`'s still-untraced leading `CALL` — wrote a single stray
  byte to). `$E10E` falls in the level/object-layout RAM region
  (`$E1xx`-`$E3xx`) the record-copier at `$783F` fills from ROM, which is
  itself unported, so this currently just copies zeros — real mechanism,
  but nothing populates its source yet.
- `checkDistanceMilestone` (`$5251`) triggers sound id `$92` once the
  distance counter passes `$0F12` (gated on `$E002` bit 6, the same bit
  `RunGameStateMachine`'s own stack-trick branch tests) and sets `$E049`
  to 3.

The other 12 callees are honest TODOs citing their real addresses and
whatever RAM each is confirmed to touch from a first look — none are
guessed at. Fully porting `GameplayTick` is comparable in scope to the
sound engine's note interpreter (item 6 below): worth treating as its
own dedicated, unhurried tracing project, not a quick follow-up. Verified
headlessly: the normal boot sequence still reaches game state 4 with no
regression, and directly driving `stateGetReady`/`statePlaying` with
synthetic RAM state (`$E001=1`, a distance past the milestone threshold,
a set game-over flag) produces exactly the transitions and side effects
traced above.

All of the above confirmed correct by rendering through the real
`Console`/`VDP` pipeline (not just the standalone simulations used to
find them), reproducing the real attract screen exactly.

**The `$E03B`/"press fire to start" gap is now closed** — see the
dedicated `$43C3` section under "Extracted assets" for the full trace of
`stateExtra43C3`, a previously totally unexplored routine (reachable only
via a manufactured return address, invisible to `cmd/disasm`'s recursive
descent) that turned out to be both the `$E03B` write site and the
"start the game" trigger at once.

Practical effect: `cmd/play` is now genuinely playable from the keyboard.
It reveals "ROAD FIGHTER" letter-by-letter with "©KONAMI 1985" /
"PLAY SELECT" / "LEVEL A" / "LEVEL B" and a blinking cursor; pressing any
key during the logo/title skips straight to the level-select menu; arrow
keys at the menu move the cursor between LEVEL A/B; Space commits the
choice and transitions to game state 3 (`stateGetReadyWait`, `$416F`-
`$426F`, now also ported — see the dedicated write-up below), which runs
a roughly 8-9 second countdown sequence and then hands off to game state
5 (`statePlaying`) — confirmed headlessly end-to-end (see below). Leaving
the keyboard alone still works exactly as before: `stateAttractWait`'s
own ~4.25s timeout advances things regardless.

**`stateGetReadyWait` (game state 3, `$416F`-`$426F`) is now ported
control-flow-only, same standard as `stateGetReady`/`statePlaying`.** Like
every state, `$E001` (`attractPhaseByte`) is loaded fresh at dispatch and
used as this state's own local counter — but here the original encodes a
9-way dispatch as a *cascading chain* of `DJNZ` instructions that all
share one CPU register never written back to `$E001` mid-chain. That looks
at first glance like it might decrement across frames the way
`stateGetReady`'s single `DJNZ` does, but it doesn't: it's really just an
inefficiently-encoded switch over whatever `$E001` already was on entry,
confirmed by tracing every `DJNZ` site's target and fall-through
individually rather than assumed from the shape. Phases 1-9 (`$E001`
values, each written by the previous phase via `setGameState`/
`advanceAttractPhase`) run in strict sequence:

1. `$4171`-`$4189`: decrements `$E004` (left at `$18` by
   `stateExtra43C3`'s commit) to zero, then advances to phase 2; while
   nonzero, TODO — redraws level-specific "get ready" text/graphics every
   4th frame via `$45F0` or `$4607` depending on `$E002` bit 5 (the A/B
   selection), not traced.
2. `$418E`-`$4193`: `clearRaceRAMBlock` (`$4413`-`$4428`, fully ported —
   a self-referencing `LDIR` zero-fill of `$E03F`-`$E14E`, 272 bytes,
   plus `$E042=0`/`$E043=1`; the range's *purpose* isn't confirmed, only
   its effect), then `setGameState($18)`.
3. `$4197`-`$41AC`: `countdownGetReadyMessage` (`$4429`-`$447E`, now fully
   ported). What an earlier pass left as a large unported "message-box"
   block turned out, once traced, to be a line-by-line screen wipe: each
   frame the entry gate (decrement `$E004`, `RET M` once negative) doesn't
   yet return true, the routine also clears one more name-table row —
   `$3800 + 32*(23-counter)`, so row 0 first, row 23 last — with `E004`
   starting at 24 (`setGameState($18)` in phase 2), this produces exactly
   24 frames of one-new-row-per-frame top-to-bottom clearing before the
   gate finally opens on the 25th, confirmed by direct frame-by-frame
   VRAM inspection. This is what plays out visually while sound id `$98`
   (triggered by phase 0's fallback branch, `$4266`-`$4268`, the instant
   phase 3 begins right after a level is confirmed at "PLAY SELECT" — not
   a crash-sequence sound despite an earlier `cmd/sndpreview` label
   claiming that; `$4266` falls inside `stateGetReadyWait`'s own
   `$416F`-`$426F` range, well before `stateCrashSequence` starts at
   `$4270`) is audible: the level-start "clear the screen while a fanfare
   plays" moment, confirmed end to end by driving a real level-select
   transition headlessly and watching both the row-clear and the sound
   channel activate together. Once the gate opens:
   `initHUDAndTrackGraphics` (`$68AA`), `drawScoreDisplay` (`$44D0`), and
   `drawStageNumber` (`$42ED`), all now ported — see their own write-ups
   below — plus TODO `$504C`, then `$E004=$78` and advance to phase 4.
4. `$41B1`-`$41B6`: `decrementAttractSubCounter` (`$440C`, already
   ported); once it hits zero (120 frames), `setGameState(4)`.
5. `$41BB`-`$41BD`: unconditionally `setGameState($1A)`.
6. `$41C2`-`$41C8`: `initTrackLayoutData` (`$77C8`) then `drawRoadStrip`
   (`$573A`), both now ported — see the dedicated write-up under "What's
   not yet ported" — then advance to phase 7.
7. `$41CD`-`$41D2`: `initTrafficSpriteSlots` (`$5197`, now ported — see
   the dedicated write-up under "What's not yet ported") then
   `setGameState(5)`.
8. `$41D7`-`$41DF`: `decrementAttractSubCounter` again (5 frames this
   time, since phase 7 set `$E004=5`); once zero, overwrites `$E003`
   (the free-running frame counter!) to `$10` and `setGameState(9)`.
9. `$41E6`-`$4265`: the "3, 2, 1, GO" countdown proper. Every frame: TODO
   `$5270` (shared with `statePlaying`'s own unported `$527D` chain), then
   picks an engine-sound parameter from `$E003` bit 4 into `$E04C` (TODO
   `$6E1D`, presumably a sound-channel update using it). Every 32nd frame
   only (`$E003 & $1F`): decrements `$E004` (set to 9 by phase 8, so this
   is the actual "3…2…1" counter — it takes several 32-frame ticks to
   walk down through values that only sometimes decrement a displayed
   digit; not fully reconciled with an on-screen "3/2/1" count, since the
   digit-drawing part is unported), triggering `TriggerSound(7)` at count
   2 and `TriggerSound(5)` at count 4 (odd counts silent), and TODO
   picking one of three digit-graphic pages (`$44`/`$46`/`$48`) blitted
   via unconfirmed helpers `$403B`/`$004D` (also used by
   `stateResultsPhase1`/`2`) — not traced. Once the counter reaches 1:
   `triggerRaceStart` (`$424A`-`$4263`, fully ported) writes two
   still-unidentified bytes (`$E1E3`/`$E1E4` = `$48`/`$49`), restashes
   `$E04C=$80`, then advances the top-level game state **twice** —
   `$E000` 3 → 4 → 5 — landing on `statePlaying` directly, skipping game
   state 4 (`stateCrashSequence`, itself still unported) entirely. It also
   sets `$E045` to 1, which is the same byte `crashFlag()` reads
   elsewhere (per `stateCrashSequence`/`stateCrashTrigger`, both still
   unported stubs) — surprising for a "race is starting" trigger to set
   what looks like a crash flag, ported faithfully rather than
   reinterpreted; worth revisiting once those two states are traced,
   since either this call's effect on `$E045` isn't really "a crash," or
   that byte means something other than what the name currently implies.
   TODO `$4257`: `$6E1D`, not traced.

`0` or `>=10` (unreachable in normal play, since phases only ever advance
by exactly 1 through 1-9) falls through the whole chain to
`$4266`-`$426D` — `TriggerSound($98)` then `setGameState($50)` —
mirroring how `stateAttractSequence`'s phase 0 falls through to its own
`>=5` case.

Verified headlessly end-to-end: driving a synthetic `Console`/`VDP`
through boot, committing a level selection at the menu, then running
frames continuously reproduces the exact phase sequence traced above
(1→2→…→9) and lands on `$E000=5` (`statePlaying`) with `$E1E3=$48`,
`$E1E4=$49`, and `$E045=1` exactly as the trace predicts — no manual
RAM poking involved, just real input events and `VBlankHook` calls.

**The remaining four states — `stateCrashSequence` (4), `stateCrashTrigger`
(6), `stateResultsPhase1` (7), `stateResultsPhase2` (8) — are now also
ported control-flow-only, closing out the full 9-state skeleton (every
`RunGameStateMachine` case now calls a real, non-empty function).** All
four use the same DJNZ-chain-over-`$E001` idiom as `stateGetReadyWait`;
none are reachable from a normal race start in this port yet, since
`triggerRaceStart` (state 3's countdown finale) jumps straight from state
3 to state 5, skipping `stateCrashSequence` entirely — these were only
exercised by directly setting `$E000`/`$E001` in a headless test, not by
playing through to them.

- `stateCrashSequence` (`$4270`-`$4305`, 5 phases): phase 1 sets the crash
  flag and holds; phase 2 holds again then advances; phase 3 refreshes
  the track-data VRAM copy and advances; phase 4 holds, then (TODO
  `$45A7` with a literal ROM source address `$7895` — outside
  `blitSpriteAttributeTable`'s RAM-only model, since it copies from ROM, not
  RAM) optionally beeps and advances; phase 5 holds, then either returns
  to `statePlaying` (state 5, if `$E002` bit 6 — "game in progress" — is
  set) or drops back to `stateGetReady` (state 2) with `$E001=1` via a
  direct 16-bit store, not `transitionTo`. The fallback (phase 0 or ≥6)
  reuses `stateGetReadyWait`'s own `countdownGetReadyMessage` gate and,
  once it opens, `initHUDAndTrackGraphics` (`$68AA`), `drawScoreDisplay`
  (`$44D0`), and `drawStageNumber` (`$42ED`, all three now ported) and the
  track-data refresh again.
- `stateCrashTrigger` (`$4325`-`$4347`, a single flag check on `$E083`):
  clear means no crash pending — beep and advance to
  `stateResultsPhase1`; set means set the crash flag and jump directly
  into `statePlaying` (state 5, `$E001=1`) via a direct 16-bit store,
  bypassing `stateCrashSequence` altogether. `$E083` itself isn't
  independently confirmed, but `GameplayTick`'s own doc comment for its
  unported `$7755` callee already flagged `$E0B8` as sharing `$E083` with
  this state, which lines up.
- `stateResultsPhase1` (`$4348`-`$4372`, 1 phase + fallback): refreshes
  the track-data VRAM copy and an unconfirmed tile/RAM copy (`$004D`)
  every frame regardless of phase; phase 1 holds, then clears `$E002`
  bit 6 (ending the "game in progress" state `stateExtra43C3`'s dispatch
  gate checks) and returns to attract mode via `transitionTo(0)`. The
  fallback gates on `countdownGetReadyMessage` again, then (TODO,
  unconfirmed text draw) sets the top-level state to `$D8` — a value
  clearly out of the 0-8 range `RunGameStateMachine`'s switch actually
  handles. Ported faithfully rather than "corrected" since the original
  genuinely writes it; the practical effect is the state machine idles
  (no case matches) until something else resets `$E000`.
- `stateResultsPhase2` (`$4373`-`$43C2`, 4 phases + fallback) is the most
  interesting of the four. Phase 1 gates on the countdown, then calls
  `drawStageMapScreen` (`$744C`, now ported — see its own write-up below)
  before returning to attract mode; phases 2-3 are still unconfirmed
  tail-jumps, currently no-ops. **Phase 4** increments `$E043`
  (a lap/stage counter) and, once it wraps past 7, resets it to 1 and
  bumps a separate counter, `$E042` — then either way writes a new
  `$E000`/`$E001` pair directly: normally `$E000=4` (`stateCrashSequence`
  again — not literally "you crashed," more likely this state being
  reused as a per-lap transition point) with `$E001=0`, or on the wrap,
  `$E000=3` (`stateGetReadyWait`) with `$E001=3` — the wrap case is what
  actually starts the *next* race's countdown. It also writes
  `gameOverFlag` (`$E00D`) to whatever value was just computed for that
  pair: `1` on the wrap, but the **raw incremented `$E043` value (2-6)**
  otherwise — a second instance (after `triggerRaceStart`'s `$E045` write)
  of a RAM byte's name implying stricter "boolean flag" semantics than a
  confirmed write site actually respects; ported faithfully, flagged
  in-code, not smoothed over. Confirmed headlessly: driving this phase
  directly with `$E043` at various values reproduces exactly this
  wrap/no-wrap split and every RAM side effect above.

**`$68AA`-`$6A05` (`initHUDAndTrackGraphics`/`drawTrackBorderAndTreeScenery`
in `graphics.go`), one of `stateGetReadyWait` phase 3's TODOs, is now
ported** — the first real new on-screen graphics since the attract/title
sequence: the SPEED/FUEL HUD panel, all three gauge-bar tile sets (each
with a mirrored companion), two pages of 16x16 sprites, and — for stages 4
and 5 — the diagonal UI border plus tree/bush and tree/road scenery.
Rendering each piece directly (bypassing the name table, which this
routine doesn't touch — see below) confirmed all of it: the HUD panel
reads "SPEED FUEL" with icon decorations exactly as already documented in
the pattern/color pairs table above; the gauge-bar mirrors are genuine
left-right flips of their normal counterparts; the border+scenery
combination renders as a coherent mountain/tree/river/railroad-crossing
scene; and the two sprite pages, rendered with `internal/msxgfx.Sprite16Mono`,
are top-down car sprites at several rotation angles (`$5A73`) and — a
real correction, not assumed from the destination address alone —
explosion/spark frames, debris fragments, and the bitmap digits for bonus
score popups ("300"/"500"/"800"/"1000"), not more vehicles as the second
page's working name first assumed. Renamed to
`sprites_crash_debris_bonus_5B97.bin` once the render made that obvious.

Two mechanics feed nearly every placement here and are worth calling out
since they weren't obvious from the listing alone:

- **Every `CALL $45E0` triples its destination.** The listing shows one
  literal `CALL $45E0` per placement, but `SetupFont`/`ShowKonamiLogo`/
  `showRoadTitle` already established (and this routine's placements
  confirm again) that the real effect is 3 identical copies at
  `dest`, `dest+$800`, `dest+$1000` — the same "identical data in all 3
  Graphic 2 thirds" requirement those three already needed. Ported as
  `placeInAllThirds`, reused for every `$45E0` site in this routine.
- **A gauge bar's mirrored *pattern* isn't a separate ROM stream — it's
  generated at runtime.** `$4652` (with its `$4679`/`$466E`/`$004A`
  callees) reads back an already-placed pattern tile, bit-reverses each
  byte (confirmed by hand-simulating the `RR`/`RLA` sequence against a
  sample byte), and writes the mirrored result elsewhere — ported as
  `mirrorPatternRows`. Cross-checked against the *existing*
  `hud_pattern_gauge_bars{1,3}_mirrored_*.bin` reconstruction files
  (already known, per the pattern/color pairs table above, to be
  precomputed rather than independent ROM streams): running the same
  transform in Go reproduced them byte-for-byte. The mirrored *color*
  data, by contrast, genuinely is a separate ROM asset in every case
  except gauge bar 2, whose "mirrored" color turned out to just be its
  own normal color asset placed a second time, unchanged.
- `$45CF` (`fillVRAMTriplicated`) is the color-table equivalent — a
  `CALL $0056`/FILVRM wrapped in the same ×3 loop — and turned out to be
  simple enough to fully confirm and port too, including its use in the
  fully-traced `fixupGaugeBarBackgroundNibble` (`$6961`-`$6988`, a
  256-byte scan that patches one specific "transparent-looking" color
  nibble) even though the routine that's supposed to run *before* it in
  that branch (`$6A30`) is still a TODO.

Not ported: `$6A30` (a per-stage terrain pattern+color table for stages
0/1/2/3/≥6 — table entries point at genuinely new, not-yet-extracted ROM
graphics streams at `$603E`/`$61C8`/`$64CA`/`$65D7`, worth its own
dedicated pass rather than a guess), `$6A06` (called unconditionally,
not traced), and `$4636` (a second mirror-generator used for the sprite
pages, structurally like `$4652` but with different row/count constants
that don't cleanly map onto 16x16 sprite quadrants yet).

One open question this surfaced: this routine never touches the name
table, only pattern/color table data — meaning none of it was visible on
`cmd/play`'s rendered screen by itself, since nothing places these
character indices into the name table. **Resolved** — that's exactly what
the very next TODO at the same call site, `$44D0`, turned out to be.

**`$44D0`-`$44E9` (`drawScoreDisplay` in `graphics.go`), the other
TODO at `stateGetReadyWait` phase 3's same gate as `$68AA`, is now
ported** — and it's the routine that actually places characters into the
name table, so this is the first `$68AA`-family graphics that's genuinely
visible end to end. Renders as "HISCORE" at $3817 with the high score
(`$E03C`-`$E03E`, a 3-byte packed-BCD counter, confirmed to be capped at
999999 by an existing overflow clamp at `$449C`-`$44A0`) directly below
it at $3838, and a "SCORE" label at $3877 — confirmed by rendering
end-to-end and reading real text off the result: "HISCORE" / "563412" /
"SCORE" for a synthetic `$E03C-E03E` of `$12,$34,$56`, positioned exactly
where a top-right HUD score display belongs.

The digit rendering itself (`$4535`-`$4560`, `drawPackedBCDDigits`) is a
small, self-contained packed-BCD-with-leading-zero-blanking routine,
fully traced and confirmed by two rendered test cases: a nonzero score
(no blanking needed) and an all-but-last-digit-zero score (`$000005`),
which correctly rendered as five zeros preceded by an invisible blanked
tile rather than "000005" — including the format's one non-obvious rule,
found by tracing the raw bytes rather than assumed: the "always show even
if zero" exception applies only to the *last byte's high nibble*
explicitly (via a `DEC B`/`JR NZ` check), with the low nibble inheriting that via a
persistent "seen a nonzero digit yet" flag that, once set, never resets —
ported as a boolean accumulator with the same observable effect rather
than literally replaying the register-level trick.

Also ported: `drawHUDBorderDecoration` (`$44F4`-`$452A`), a handful of
solid-color fills and a repeated-tile vertical strip forming decorative
border/icon graphics next to the HUD panel — mechanically confirmed
(exact addresses, tile codes, and fill counts, including a non-obvious
carry through the high byte of HL across its 8-row loop at
`$450E`-`$4519` that would have landed the second `FILVRM` one row off if
missed) but the individual tiles' visual identity isn't independently
confirmed beyond "these reuse character ranges `$68AA` and `ShowKonamiLogo`
already loaded." `$740B` — its fall-through tail, at the time flagged as
"a separate and apparently substantial routine, not chased further" —
turned out, once actually traced, to be one line: `CALL $73AD`, the speed
gauge's own entry point, i.e. an initial gauge draw right after the
border decoration, followed by a tail-jump to `$44D9` (still TODO, the
same address `GameplayTick` itself ends with). Now ported as part of
`drawHUDBorderDecoration`.

**Confirmed interaction, not a bug**: this border strip's own last row
(`$39F9`, name-table row 15) and the speed gauge's topmost row land on
the exact same address whenever `$E04F` (speed) is high enough to fill
all 8 gauge rows (`>= $E0`) — both routines' addresses and extents were
independently traced and verified byte-for-byte, so this overlap is
genuinely present in the ROM's own code, not introduced by this port.
Tracing `$740B` all the way through found nothing that redraws or
protects the border strip afterward, so whether the original game
tolerates this visually (real hardware, a CRT, a rare/brief condition)
or whether some other still-unported routine is meant to refresh the
strip each frame isn't resolved — flagged here rather than "fixed" by
guessing at a resize or reposition that isn't backed by a confirmed
trace.

**`$42ED`-`$44E9` (`drawStageNumber` in `graphics.go`), the third and
last TODO at `stateGetReadyWait` phase 3's `$68AA` gate, is now ported**
— confirmed by rendering: "STAGE 01" appears centered in the middle of
the screen, right below the "©KONAMI 1985" line. Draws "STAGE" (plus 3
trailing blanks) via `drawTextRun` at `$3969`, then backs the name-table
destination up 2 characters and overwrites the last 2 blanks with the
current stage number (`$E043`, one packed-BCD byte, reusing
`drawPackedBCDDigits` from the `$44D0` write-up above) — reached via a
genuinely easy-to-miss shared entry point, `$44ED`, that only makes sense
once you notice `$42F3`-`$42F5` (`DEC L; DEC L; JP $44ED`) is deliberately
walking the name-table pointer *backward* into the space `drawTextRun`
itself just wrote, not forward past it.

**Why the real hardware shows a blank/track screen behind all of this,
and this port currently shows the old title/menu screen instead**
(reported after actually running it and comparing to real hardware): it
is not a missing "clear the screen" call. `$504C` — the one remaining
TODO at this same gate — was fully read end to end while chasing this,
and it turns out to be *entirely* work-RAM manipulation and ROM data-
pointer setup (level/track data pointers into `$7xxx`-range ROM regions,
none of them VRAM addresses) — there is no name-table clear anywhere in
it, nor in either of its two RAM-clearing callees (`$51E5`, and the
inline clear at `$504C`'s own start). The real explanation is more basic:
on real hardware, the middle of the screen at this point is the actual
scrolling road/track view, which naturally overwrites whatever was there
before — it was never "cleared," it's *replaced* by new content this
port doesn't draw yet. That drawing job belongs to `GameplayTick`'s
still-mostly-unported callees (see "What's not yet ported" above) and
plausibly some of `$504C`'s own callees (`$51FF`, `$7020` and the ROM
pointer tables they set up) once those are traced — not a quick fix on
its own, but now a correctly-diagnosed one rather than a guess.

**Follow-up, found while doing a quick assessment pass over
`GameplayTick`'s 12 unported callees looking specifically for
screen-drawing code**: one of them is exactly the SPEED gauge-bar overlay
flagged as unlocated above. `$738C` (`GameplayTick`'s own `$5239`
dispatch — see item 4 in "Next steps") falls straight through, after its
own distance-accumulation logic, into `$73AD`-`$73FA`
(`drawSpeedGaugeBar`): a 3-tile-wide vertical bar gauge at `$3AD8`
rendering `$E04F` (the "speed" value also read by `engineSoundPitchStep`
and the acceleration logic at `$6CA1`) with 32 discrete fill levels (top
3 bits of `$E04F` select how many rows are fully lit, the next 2 bits
select one partially-lit row, the rest are unlit). Confirmed two ways: a
byte-for-byte match between the traced control flow and the actual
name-table bytes written across 7 different speed values (every single
row matched by hand-derived expectation), and a direct tile render of
the gauge's own character range showing real, non-blank
horizontal-bar-segment graphics, not empty tiles. Ported as
`scrollSpeedAndDrawGauge` (the `$738C` entry) plus `drawSpeedGaugeBar`
itself, in `gameplay.go`.

**Correction, found once the car could actually accelerate to high speed
and the gauge visibly overwrote HUD graphics above it**: this pass's own
"8-row" description was wrong, and so was the port. Every one of
`drawGaugeBar`'s three drawing paths (full rows via repeated `SUB $20`
until it borrows, the unlit fill, and the `value==0` all-unlit case)
shares the same boundary check at `$73ED`-`$73F0` (`CP H; RET Z` once the
address's high byte reaches `$39`, i.e. name-table row 15) and stops the
instant it would touch that row. Hand-tracing every value band shows the
gauge only ever reaches 8 rows (touching row 15) for values `$E0` and
above — unreachable given the confirmed `$D7` cap shared by both speed
and fuel — so in every value this port can actually produce, the gauge is
genuinely **7 rows tall** (rows 16-22), not 8; row 15 belongs to other
HUD graphics above it. The original 7-row verification above was real and
correct as far as it went (it just didn't happen to test a value near the
boundary), which is exactly how this stayed unnoticed until real gameplay
exercised the high end of the range. Fixed by making the unlit-fill and
subnotch-then-continue paths check the same `$39` boundary and stop,
instead of unconditionally filling a fixed 8 rows; verified for all 256
possible input values that name-table row 15 is left untouched for every
value below `$E0`, and correctly touched (matching the original's own
behavior) at and above it.

A quick sweep of every other `GameplayTick` callee's opening instructions
for the same VRAM-writing call pattern (`$45E0`/`$0056`/`$004D`/`$45F0`/
`$4575`/`$45A7`) turned up nothing else in that first pass. No FUEL-bar
equivalent was found that time; it turned out to need a second look.

**Follow-up pass, found the FUEL gauge**: `$7444`-`$7449` -- called from
two *other* `GameplayTick` callees, `$7411` (`$5245`) and `$7755`
(`$5230`) -- loads `$E083` into A, sets the column to `$DB` (exactly 3
columns right of the speed gauge's `$D8`, i.e. immediately alongside it),
then jumps *into the middle of the speed gauge's own code* at `$73B2`
(the shared tail starting right after `$73AD`-`$73B0`'s "load the value
and column" preamble) rather than duplicating it. Both gauges are
genuinely the same routine underneath, parameterized only by value and
column — refactored here into a shared `drawGaugeBar(value, col)`, with
`drawSpeedGaugeBar`/`drawFuelGaugeBar` as the two thin, address-specific
callers.

`$E083` is confirmed to be a real, actively-managed fuel level, not a
crash-adjacent flag with a coincidental threshold check (its other
confirmed reads, in `stateCrashTrigger` and `engineSoundPitchStep`, only
hint at that): the player-car state-machine callee reached from `$6A70`
drains it by `$14` every 8 frames at one of its own sub-states
(`$6AF4`), and two `GameplayTick` callees manage it directly —
- `$7411`/`updateLaneFlagAndFuel` ($5245): edge-detects a difference
  between bit 5 of `$E057` and `$E084` (semantics of that bit
  unconfirmed), and when it fires, drains fuel by 1 (or sets `$E049=2`
  if it was already/just went to 0), redraws the fuel gauge, and beeps a
  low-fuel warning (sound id 2) once fuel drops under `$10` (gated on
  `$E028`, a channel-record byte, being clear).
- `$7755`/`fuelPickupTick` ($5230), partially: dispatches on `$E0B8`
  (0 = idle). Its `==1` branch (refueling) is fully ported: refills fuel
  by `$20` capped at `$D7`, redraws the gauge, advances `$E0B8` to 2.
  Beyond that point ($7773-$778E: an `$E0B9` counter, a 5-byte ROM table
  at `$7791`, the shared counter-compare helper `$447F` also used by
  `$5501`, and a sound trigger) and the entire `$E0B8==2` branch
  ($7796-$77C7, object/level-layout RAM manipulation) are left as TODOs
  — real gameplay logic beyond the drawing itself, not guessed at.

Verified headlessly: every branch of both drain and refill logic
(same-bit no-op, drain-to-exactly-zero, already-zero, low-fuel warning
arming a real `TriggerSound(2)` request, refill with and without
hitting the cap, idle `$E0B8=0`) produces exactly the expected RAM and
gauge-byte state, and driving both gauges from one `GameplayTick` call
with independent speed/fuel values renders two correct, independent
bars side by side.

**Known gap, found after this pass landed: the fuel gauge never actually
draws in real play (confirmed by instrumentation over 3000 frames of
normal gameplay — the byte at its VRAM address never changes from its
initial 0).** The drawing code itself is correct (see the verification
above); the problem is upstream. Both of its triggers are conditional:
`updateLaneFlagAndFuel` only fires when bit 5 of `$E057` differs from
bit 5 of `$E084`, and `fuelPickupTick` only fires when `$E0B8==1`
(refueling). Neither `$E057` nor `$E0B8` is ever written by anything
ported so far — confirmed by watching both sit at 0 for the same
3000-frame run — so neither trigger condition is ever met. Both are
presumably set by parts of the "normal driving" car-behavior state
(`updateNormalDriving`/`$6A90`, `$E049==0` in `updateSteeringAndCarState`'s
dispatch, now ported — see the dedicated `$6A70` write-up below) or by
track/object collision code reached from it. Porting `updateNormalDriving`
did *not* close this gap — it doesn't touch `$E057` itself, only its own
unported `$6CD1` callee is left as a candidate. Unlike the speed gauge,
which redraws unconditionally every frame regardless of value, the fuel
gauge is only ever *event*-driven in the original — meaning tracing
`$6CD1` (or whatever else sets `$E057`) remains a real prerequisite for
ever seeing it on screen, not a follow-up polish item.

**Follow-up once the car could actually drive around and the fuel gauge
was still never visible**: re-checked both trigger bytes properly (an
earlier pass here only searched for literal `(addr)` operand text, which
misses indirect writes through a register loaded with that address
earlier). `$E057` genuinely has zero write sites anywhere in the entire
16KB ROM by *any* addressing form (`grep`'d for the bare address text,
not just the operand pattern) — it's read exactly once, in
`updateLaneFlagAndFuel` itself. `$E084` is referenced from exactly that
one place too (a single `LD HL,E084h` — no other routine even knows the
address). Since both start at 0 (`clearRaceRAMBlock`'s clear) and nothing
else ever changes either, `updateLaneFlagAndFuel`'s drain trigger cannot
fire as literally traced — not just in this port, seemingly in the ROM
itself. Confirmed `fuelPickupTick`'s own trigger is equally unarmed
(nothing writes `$E0B8`), and confirmed via an exhaustive search that no
third "just draw it once at race start" call site for `$7444`
(`drawFuelGaugeBar`'s real entry) exists anywhere in the ROM either — only
the two dead ones. The likely explanation is that one of `$E057`/`$E0B8`
is actually written via `IX`/`IY`-relative addressing elsewhere in the
ROM (used for other RAM structures already, e.g. the sound channel
records at `$E010` via `IX+n`) — a literal-address search can't surface
that, and running it down would mean checking every `IX`/`IY` base value
used anywhere in the ROM against these two offsets, a real but
open-ended sub-investigation, not done yet.

Pending that, at the user's own request, `stateGetReadyWait` phase 3 now
calls `drawFuelGaugeBar` once, explicitly labeled as a stopgap and not
ROM-faithful (the real ROM has no such call), purely so the gauge shows
the starting fuel level instead of staying invisible for the entire
race. Verified: the gauge correctly stops at row 16 for the starting fuel
value ($D0, 6 full rows + 1 partial), leaving row 15 to
`drawHUDBorderDecoration`'s own tiling there, matching the row-15 fix
above exactly. TODO: remove this call once the real `$E057`/`$E0B8`
write site is found and the gauge updates live during gameplay.

**Third pass: `$527D`-`$528F`/`advanceCarBehavior`, `statePlaying`'s
*other* DJNZ branch, is now ported control-flow-only** — and this one
matters more than either gauge did on its own. `statePlaying` reuses
`$E001` as its own countdown, entering state 5 with it at 0; each frame
it decrements first, so it's `255` before it's ever `1` again, meaning
the "call `GameplayTick`" branch fires only 1 frame in every ~256 — the
other ~255 take this tail-jump instead. Confirmed by instrumentation, not
just the arithmetic: `$E077` (only ever touched inside `GameplayTick`'s
own callees) sat frozen for 255 frames at a time before this pass, and
changes every single frame after wiring this dispatcher up. Practical
effect: the speed and fuel gauges — both reached again from here, via
`$5270`/`blitSpriteAttributeTable($78)` and `$738C`/`scrollSpeedAndDrawGauge`,
already ported for `GameplayTick`'s own sake — now update every frame in
real play instead of in ~4-second jumps.

The other confirmed callee here, `$6A70`/`updateSteeringAndCarState`
(also `GameplayTick`'s own separate `$5233` dispatch), turned out to be
the entry point for an 8-state per-frame car-behavior machine, dispatched
via a jump table at `$6A80` and selected by `$E049` — confirming `$E049`
is a genuine state selector, not just the simple "race-phase gate" it
looked like from `checkDistanceMilestone`/the fuel-drain routines' `DEC
A; RET Z` checks alone. Initially only state `$E049==2` (fuel-empty,
reached from this session's own `updateLaneFlagAndFuel`/`fuelPickupTick`)
was ported: while speed is still above 0 it decelerates by 1 each call
(`$6C73`'s own continuation left as TODO); once speed has *already*
reached 0, it draws "EMPTY" — a genuine second screen-drawing find,
positioned at the car's own screen location. That position comes from
`$4561` (`pixelToNameTableAddr`), a coordinate-conversion routine whose
bit-level mechanics (four solo `RRA`s then three interleaved `RRA`/`RR H`
rounds, splitting bits across two registers) were confirmed by literally
simulating the instruction sequence in Go against a closed-form formula
across a full sweep of inputs and both incoming-carry states, rather than
derived by inspection of the shuffled bits alone — the carry state turns
out not to matter, since the bits it could affect are all discarded by
the final mask. Verified headlessly across every branch (still
decelerating, fuel unexpectedly nonzero → the `$6C2C` TODO, message
drawn and correctly clamped away from the HUD column range).

**A later pass completed all 8 states.** The other 7
(`$6A90`/`$6ABB`/`$6B5E`/`$6B90`/`$6C05`/`$6C31`/`$6C3A`, states
0/1/3/4/5/6/7) are now ported at their own top level, plus `$6C2C` (state
2's "fuel unexpectedly nonzero" branch — just `$E049=0`, far simpler than
it looked from the outside) and the small shared helpers several of them
call into: `$6AB4`/`$6AB5` (`setEngineSoundSelector` — stores a selector
byte into `$E053` then re-arms `$6E1D`), `$6E1D`
(`armEngineSoundParams` — installs two 3-byte records into a 4-byte-strided
table at `$E112`/`$E116`, the same stride `countdownGetReadyMessage`'s own
sentinel-fill loop uses, from the car's row/column position at
`$E04C`/`$E04E` and a small ROM lookup table at `$6E3F` indexed by
`$E053`), `$6E0B` (`applySignedPositionOffset` — adds a doubled, optionally
negated magnitude to a 16-bit fixed-point position at `$E04D`
(fraction)/`$E04E` (integer pixel column, matching how `drawFuelEmptyMessage`
already reads `$E04E` directly as a plain column)), `$6DEC`
(`updateCarPositionFromSteering` — derives that magnitude from speed and
edge-triggered input), and `$6BE2` (`nudgeCarPositionTowardSpeed` — a
speed-driven position nudge used directly by states 4 and 7, skipping the
doubling step). None of these five helpers' *ultimate* purpose (what
`$E112`/`$E116` drive, why `$E054` swaps between two selector/sign
conventions) is confirmed — ported for mechanical effect, the same
standard `installSoundRequest`'s own `$E032`/`$E034` constants were held to
before their role was understood.

State 3 turned out to be genuinely significant: triggered by
`checkDistanceMilestone` once distance crosses its threshold, it zeroes
speed, decelerates a position byte (`$E04C`) toward a fixed target in
steps of 4, and — once within 8 of that target — sets `gameOverFlag`
(`$E00D`) if two still-unconfirmed flags (`$E0E6`/`$E0F6`) are both clear.
Verified headlessly: `$E04C` decrements exactly on schedule and
`gameOverFlag` flips the frame it crosses the threshold, with a fresh
console's zeroed `$E0E6`/`$E0F6` satisfying the gate immediately — this
reads like "stage complete" (or possibly a genuine game-over trigger; the
existing `gameOverFlag` name predates this pass and isn't re-litigated
here) rather than a crash-adjacent state, given it's reached purely from
distance, not collision.

A structural wrinkle, confirmed by literal stack-effect tracing rather
than assumed from the "8 states" framing alone: states 1, 3, and 6 each
begin with a bare `POP HL`, discarding the return address their own
caller (`GameplayTick` or `advanceCarBehavior`, via `$6A70`) pushed. Any
`RET` reached afterward then returns not to that immediate caller but to
*its* caller instead — i.e. these three states also abort the rest of
whatever `GameplayTick`/`advanceCarBehavior` was about to do that frame,
the same "discarded return address" trick `decrementAttractSubCounter`
already established a translation convention for (an ordinary early
`return`, not a literal stack replay). Modeled here as a `bool` return
from `updateSteeringAndCarState`, checked at both call sites. One
sub-branch of state 1 (fuel unexpectedly still nonzero right as its own
~11-frame counter bottoms out) genuinely unwinds one level further than
that — approximated as the same single-level abort, flagged in the doc
comment, since correctly modeling it would mean restructuring
`GameplayTick`/`RunGameStateMachine`'s own call chains for a case that
shouldn't occur in practice.

One more real bug turned up and got fixed along the way:
`drawScoreDisplay`'s own digit-drawing tail (`$44D9`) only ever drew the
first of *two* sequential packed-BCD digit groups in the original —
hiscore (`$E03C`-`$E03E`) and current score (`$E03F`-`$E041`, confirmed
distinct by `clearRaceRAMBlock` zeroing `$E03F` onward at the start of
every race while leaving hiscore alone) — easy to miss since both live
inside one Z80 function via two sequential calls into the shared `$4535`
tail, not two separately-named routines. Also resolved two long-standing
`JP $44D9` TODOs elsewhere (`GameplayTick`'s own end, and
`drawHUDBorderDecoration`'s `$740E`) plus a genuine `CALL $44D9` from
state 3 above, all now sharing one `redrawScoreAndHiscoreDigits` helper.

Verified end-to-end: driving a real level-select transition headlessly
into `statePlaying` and forcing each of the 8 `$E049` values in turn shows
every state's counters/flags evolving exactly as traced (state 1's
`$E04A` counting down over ~160 frames to the fuel-empty message, state
3's `$E04C` decrementing by 4 each frame into a `gameOverFlag` flip,
state 6 clearing `crashFlag` immediately, state 7 cycling `$E0B1`/`$E053`
on schedule) with no panics across all 8 states over hundreds of frames
each, and no regression in the existing boot→race smoke test.

Still open, all left as clearly-cited TODOs rather than guessed: `$5501`
(a large per-stage scroll/lane dispatcher — its own jump table has
7 stage-specific entries, each touching ROM data pointers `$504C` sets up,
clearly on the same scale as `GameplayTick`'s own remaining callees) in
`advanceCarBehavior`'s own chain; and the `$528F` per-lane
object-adjustment tail (no VRAM writes found in a sweep of it). `$6CD1`
itself is now ported — see the dedicated write-up below. `$6C73`
and `$6CA1` themselves are now ported — see the dedicated write-up below.

**`$6CA1`/`accelerateCarSpeed` (acceleration/collision) is now ported**,
and tracing it led to a much bigger find: the game's hardware sprites.
`$6CA1` itself turned out to be acceleration and road-scroll only, not
collision (nothing resembling a hit-test was found in it) — while the
accelerate button (`$E009` bit 4, level-triggered) is held, increments
speed (`$E04F`, capped at `$D7`) and, in one of two alternating "active"
speed bands (`speedInAccelerationBand`, `$18`-`$4F` and `$7E`-`$FF` — why
these specific bands isn't confirmed), advances a 16-bit car row position
by `$48`. Releasing the button (or three car-behavior states managing
their own speed already, `carBehaviorManagesOwnSpeed` — 1, 2, 7) instead
decelerates the same way in reverse (`decelerateCarSpeed`/
`scrollRoadBackward`, ported alongside it, resolving state 2's own
`$6C73` TODO too). Verified headlessly: holding "accelerate" for 250
calls shows speed and row climbing in lockstep, correctly freezing the
row exactly through the inactive `$50`-`$7D` speed band and resuming
after — then releasing it mirrors the whole climb symmetrically back down
to a full stop.

That "16-bit car row position" is `$E04B` (fraction)/`$E04C` (integer) —
confirmed to exist, and to be a fixed-point pair mirroring the
already-known `$E04D`(fraction)/`$E04E`(integer) *column* pair, by
`$6CA1`'s own "`LD HL,(E04Bh)`" 16-bit load reading `$E04C` as the high
byte. But `$E04C` chasing this pair down is what surfaced something much
bigger: both `accelerateCarSpeed` and `scrollRoadBackward` end by calling
`$6E1D`, a routine this port had *already* ported (while tracing `$6A70`
last session) under the guess "engine sound parameters," writing into
`$E112`/`$E116`. Multiplying out `msx.VDP`'s sprite-attribute-table
register formula (`R5*0x80`) for this ROM's real boot value (`R5=$76`)
lands on `$3B00` — the *exact* address `internal/game.copyTrackDataToVRAM`
(a function already ported, entirely from its own bytes, as "copies a RAM
buffer to `$3B00`, purpose unconfirmed") was already blitting a RAM buffer
to, every frame, from `$E10E` onward. `$E112`/`$E116` are two of that
buffer's 4-byte sprite-attribute records (Y, X, pattern, color) — meaning
the "engine sound" guess was simply wrong, corrected now that the real
picture is confirmed: **the player's car is drawn as two 16x16 hardware
sprites**, and `$6E1D` (renamed `armPlayerCarSprites`) is what positions
and re-patterns them every time the car's row/column moves or its
steering-angle sprite selector (`$E053`, indexing an 11-entry ROM table of
pattern offsets, all multiples of 8 — `carSpritePatternTable`) changes.
`copyTrackDataToVRAM` itself was renamed `blitSpriteAttributeTable` to
match. A third confirmation, independent of the register math: a direct
`$D0` byte write (the real TMS9918A "hide this sprite" Y value) was
already sitting in the ROM at that exact address, inside
`countdownGetReadyMessage`'s own line-by-line wipe (hiding the player's
car for entries 0 and 1 while the screen clears) — three independent
things landing on the identical address is not a coincidence.

Filling in the rest of the picture: `$597D`/`scrollTrafficSprite`
(`GameplayTick`'s own `$5227` dispatch, previously TODO, also reached from
`stateResultsPhase2`'s `$4377`) derives sprite-attribute entry 0's own row
from the distance counter `$E078` and blinks its color every 16 frames —
entry 0 is a plausible traffic/opponent car (its row scrolls with
progress, unlike the player's own row/column pair), though that's not
independently confirmed. `$51FF`/`initPlayerCarSpritesAndFuel` (the one
self-contained, confirmed piece of the much larger `$504C` gameplay-init
routine, wired into `stateGetReadyWait` phase 3 and `stateResultsPhase2`
phase 4 — the other two `$504C` call sites are untouched) copies two ROM
tables into `$E049`-`$E053` and `$E10E`-`$E119`: the player car's initial
`$80`/`$52` row/column, and the two sprite entries' own colors — `$01`
(black) and `$06` (dark red), confirming the two-sprite car has two
distinct visible colors, not one repeated. Fuel (`$E083`) is also set to
`$D0` here, near its `$D7` cap.

On the rendering side, `cmd/play` had never drawn hardware sprites at
all — only background tiles. Added `msx.VDP.SpriteAttrTableBase` and a
`drawSprites` pass in `cmd/play`'s own `Draw`, compositing the real
TMS9918A way: 32 possible entries, stopping at the first `Y=$D0`
terminator (matching real hardware, not just hiding that one entry),
displayed row is `Y+1` (a well-known quirk), color 0 means fully
transparent, bit 7 of the color byte is the "early clock" flag (shift
32px left), and sprite 0 has the highest priority (drawn last, on top,
when entries overlap). `internal/msxgfx` gained `Sprite16Visible`
alongside the existing `Sprite16Mono` — the latter always fills a flat
background, which is right for a standalone preview but wrong for
compositing over an already-drawn scene; `Sprite16Visible` returns which
pixels are "on" and leaves the rest untouched, verified to produce
identical on-pixel positions to `Sprite16Mono` (same 32-byte hardware
sprite quadrant layout, zero mismatches checked against a real extracted
vehicle sprite). The `8-sprites-per-scanline` hardware flicker limit is
not modeled, the same kind of simplification this project's PSG
synthesizer already applies to itself.

End-to-end verified headlessly: driving a real level-select transition
into gameplay and stepping several frames shows the real VRAM sprite
attribute table holding exactly the expected data — entry 0 hidden
off-screen (`Y=$E0`), entries 1/2 at the player car's actual position
with patterns correctly re-selected from `$E053`'s table lookup every
frame while their colors (set once at init) persist unchanged.

**Correction: `drawSprites`' own pattern-table addressing was wrong**,
found once the car was actually visible on screen and looked broken both
driving and crashing. It computed the pattern generator address as
`patIndex*32`, treating the sprite-attribute-table's pattern byte as a
plain "tile index." Real TMS9918A 16x16-sprite hardware ignores the low 2
bits of that byte (forcing it to a multiple of 4, since one 16x16 sprite
occupies 4 consecutive 8x8 patterns) and addresses the pattern generator
table in 8-byte units, not 32-byte ones — the correct formula is
`(patIndex & $FC) * 8`. For pattern number 0 the two formulas coincide
(both give offset 0), which is exactly why this went unnoticed until a
*second*, non-zero pattern number was actually reached in practice —
every other value read 4x too far into VRAM, e.g. the player car's own
second sprite (`carSpritePatternTable[n]+4`) landing on unrelated sprite
data, and the crash-adjacent selectors (`$40`/`$48`, `setCarSpritePattern`
from state 1) landing as far as the background pattern table instead of
sprite data at all. Fixed, and the fix's correctness is confirmed by more
than just re-deriving the formula: recomputing every `carSpritePatternTable`
entry through it shows indices 0-4 exhaustively cover all 10 vehicle
sprite tiles with zero gaps and zero overlaps (`{0,1}`, `{4,5}`, `{6,7}`,
`{8,9}`, `{2,3}` — the union is exactly `0`-`9`), and indices 8-9 land
cleanly inside the crash/debris/bonus sprite sheet — far too clean a
pattern to be coincidental. Indices 5-7 (plausibly "turning left/right"
states, though not confirmed) land in a genuine gap: VRAM tiles 10-15,
between the 10-tile vehicle sheet and the 25-tile crash/debris/bonus one,
which nothing currently writes — the likely reason is `initHUDAndTrackGraphics`'s
own already-documented `$4636` TODO ("a sprite-pattern mirror generator
... for 16x16 sprites, not traced"), presumably meant to fill that gap
with horizontally-mirrored vehicle sprites for turning. Until `$4636` is
traced, the car will render correctly going straight (and using the
crash/debris sprites) but show blank or garbage sprites while turning.

**`$6CD1`/`checkOffTrackCollisionAndCrash` (the actual collision
detection) is now ported**, called every frame from `updateNormalDriving`
(state 0) before anything else it does, and directly (as `$6DC1`, its
inner dispatcher, skipping `$6CD1`'s own wrapper) from state 7's ending.
The mechanism: look up a per-row boundary column from a 24-byte RAM table
at `$E098` (`checkOffTrackCollision`, indexed by a clamped version of the
car's own row — `clampRowBand`, the same "special-case rows near the
bottom of the screen" idiom `checkTrackBoundaryTile`'s own row clamp
uses, just a different band width), compare it to the car's actual column
(`$E04E`), and read one background tile 4 rows ahead and slightly to one
side (`checkTrackBoundaryTile`, `$6D8B` — the original reads it via
`SETRD`+`IN` on the VDP data port rather than calling `RDVRM`, but the
observable effect is identical, so this uses `msx.System.ReadVRAM`
directly). A tile in `$CF`-`$D8` is "safe" (on-track); anything else
triggers further classification (`boundaryCheckNear`/`boundaryCheckFar`,
`$6D56`/`$6D2A` — including two narrow substituted-value special cases,
tile exactly `$F3` or `$EC`) that can report "still safe" or a specific
non-zero value stashed into `$E055` (role not confirmed). If unsafe, the
*raw tile value itself* then gets classified a second, independent way
(`checkCrashBoundaryTile`, `$6D09`): `$74`-`$77` is a genuine crash
(`$E049=4`, i.e. `stateCrashSequence` — the first real path into it this
port has found), `$78`-`$7B` is a "bounce" (speed reset to `$20`, HUD
redrawn), and everything else (including tiles *below* `$74`, via the
original's byte-wraparound `SUB $74` — confirmed the hard way: an
initial translation that checked `tile>=$7C`/`tile<$78` directly passed
every hand-traced case but incorrectly crashed on tile `$60`, caught only
by testing a value far outside the narrow `$74`-`$7B` band on purpose)
falls through to a "car left the track, start recovering" step: `$E049=1`
with a fresh countdown, and then either a slow recovery (`$E049=5`,
speed/timers reset to fixed values) if speed is below `$A0`, or — if
still fast — a full stop (speed and the fuel-pickup dispatcher both
zeroed) plus a deep, two-level "abort the caller" signal (the original's
`$6D04`, `POP HL; POP HL; JP $6F1E`, discards *both* this function's own
return address and `updateNormalDriving`'s own — modeled as the same
propagated bool `updateSteeringAndCarState` already uses, just threaded
one level deeper).

Verified headlessly across all 5 outcomes (crash, bounce, safe,
slow-recovery, deep-abort recovery) with the exact expected RAM state
each time, plus regenerating that off-by-one bug on purpose to confirm
the fix; and confirmed no panics over 30000 frames of real gameplay with
the accelerate button held. One honest caveat from that run, since
partially superseded (see below): at the time, real track/road graphics
weren't rendered yet, so the track-area name table stayed blank (tile
`$00`), which this collision check correctly treated as "off-track"
essentially always. `initTrackLayoutData`/`drawRoadStrip` (see their own
write-up further down) now paint a real road cross-section into that
same name-table region once, at race start — so this collision check now
reads genuine `$D0`/`$D1` "safe" tiles in the actual road columns rather
than uniform `$00`. It's still not a moving/curving road frame to frame
(see the caveat on `updateRoadAndScore`'s own reachability below), so the
car's column relative to that *static* drawn strip is what determines
safe-vs-off-track for the whole race, not a scrolling track — an
improvement over "always off-track," not yet the real driving
experience.

A second, unrelated screen-drawing find from the same sweep (not a
`GameplayTick` callee, but adjacent code checked while chasing `$7374`/
`$738C`): `$744C`-`$7474` (`drawStageMapScreen`), called from
`stateResultsPhase2` phase 1 right before returning to attract mode.
Places the already-identified stage-map terrain tileset
(`assets.MapTerrainTilesetPattern`/`Color`, confirmed pattern/color pair,
see the table above) into VRAM, draws a 5-tile-wide by 21-row vertical
strip of the just-placed map tiles at `$382B` using a literal ROM-to-VRAM
row-repeat, and resets 8 bytes of gameplay state (including `$E04F`, the
same speed value the gauge bar reads, back to 0 for the next race).
Rendered and visually confirmed as a coherent repeating pattern (road
segments, car icons, greenery) consistent with the tileset's already-
established identity.

**`$5501`'s stage-0/1 entry (`updateRoadAndScore`) plus its two callees
`$5716`/`scrollAndDrawRoadStrip` and `$447F`/`addToScore` are now
ported** — a first, honest slice of what is confirmed to be a much
larger dispatcher (see "What's not yet ported" for the other 5 stage
branches and the 4 pre-`$5716` callees left as TODOs: `$58AF`, `$56F7`,
`$5944`, `$54AF`, plus `$592A`).

`addToScore` (`$447F`-`$44A4`) adds a 3-byte packed-BCD amount to the
current score (`$E03F`-`$E041`), gated on `$E002` bit 6, using a new
`bcdAddCarry` helper that replicates `ADD`/`ADC` + `DAA` digit-by-digit.
On overflow it caps the hiscore bytes at `$99` each — genuinely all
*three* of `$E03C`-`$E03E`, not just two, since the original does two
overlapping 16-bit stores (`LD (E03Ch),BC` then `LD (E03Dh),BC`, both
`BC=$9999`). An initial translation only set two of the three bytes;
caught by a scratch test exercising the overflow path directly, not by
hand-tracing (the overlap is easy to read as a typo/redundancy and
skip). This looks like a real original-game bug — capping the *hiscore*
bytes on a *score* overflow — ported faithfully regardless, per this
project's own rule of preserving observed behavior over "fixing" it.

`scrollAndDrawRoadStrip` (`$5716`-`$5767`) decrements a 16-bit read
pointer (`$E046`) by 22, wrapping `$E170`→`$E380` — confirmed deliberate:
the two addresses are exactly 528 = 24×22 bytes apart, i.e. a circular
buffer sized for exactly what this routine reads. It then projects
24 rows × 22 columns (528 consecutive bytes, not the same 22 bytes
repeated per row) directly into the visible name table starting at
`$3801`, with a second, independent wraparound partway through that
same 528-byte span (`$E396`→`$E186`). Verified headlessly: pointer
arithmetic, both wraparounds, and the exact VRAM bytes written, plus a
30000-frame real-gameplay run (level-select → `statePlaying`,
accelerate held) with no panics.

`updateRoadAndScore` (`$5501`'s own top-level check through its stage
0/1 branch) gates all of this on `$E049==1`, toggles score-increment
on alternating calls via a bit in `$E07E` compared against `$E078` bit 0,
then dispatches on `$E043` (0 or 1 → the ported road-strip path; 2-6 →
TODO). **Important, non-obvious finding from testing this**: across the
two confirmed call sites (`$5242` in `GameplayTick`, `$5289` in
`advanceCarBehavior`), both call `$6A70`/`updateSteeringAndCarState`
*before* `$5501`, and every exit path out of car-behavior-state 1's own
dispatch (verified directly: forced `$E049=1` and called
`updateSteeringAndCarState` in a loop — every single frame reported
`abort=true` until the state's own countdown moved it to state 6) is a
"discard the caller's return address" exit. Tracing exactly how many
stack frames `$6CD1`'s own abort path (`$6D04`: `POP HL; POP HL; JP
$6F1E`) discards confirms it unwinds past *both* `$6A90` (state 0's
`CALL $6CD1`) and `GameplayTick`/`advanceCarBehavior`'s own `CALL $6A70`
site, aborting the whole tick — and the non-abort ("slow recovery")
path through the same code overwrites `$E049` from 1 to 5 before its own
`RET`, so by the time `$5501` gets a chance to check `$E049`, it has
already moved past 1 either way, on the very frame it was set.

**Follow-up (this was chased down further, and is now a confirmed
conclusion, not an open question): every other `$E049` write site in the
ROM has been traced, and none of them change this.** The 7 addresses
that were unaccounted for (`$6AE4`, `$6B54`, `$6BDC`, `$6C2D`, `$6D16`,
`$6ED6`, `$6F1B`) resolve as follows: `$6AE4`/`$6B54`/`$6BDC`/`$6C2D`
write `6`/`6`/`0`/`2`-or-`0` respectively, from car-behavior states 1-5's
own already-ported tail transitions — none of them ever write `1`.
`$6D16` is `checkCrashBoundaryTile`'s own already-ported "genuine crash"
branch (`$E049=4`), not a new site. `$6ED6` (part of an as-yet-untraced
function reached from `$6E4A`) writes `4`, not `1`, so it's also
irrelevant to this gate specifically. That leaves exactly **two** sites
that ever write `$E049=1`: `$6CE5` (`checkOffTrackCollisionAndCrash`,
already covered above) and `$6F1B` — which turned out to be state 7's
*own* off-track check (`$6C5D`-`$6C64`, already ported as
`checkOffTrackCollision`'s direct call from `case 7`): on detecting
unsafe ground it sets `$E049=1`/`$E04A=$0C` and returns the identical
double-discard `true` that aborts the whole tick, exactly mirroring
`$6CE5`'s own high-speed branch. So *both* of the only two `$E049=1`
writers in the entire ROM immediately abort the tick that sets it, and
re-reading `updateSteeringAndCarState`'s own `case 1` (`$6ABB`-`$6AD1`)
line by line confirms every single one of its branches — the "every 8th
frame" early exit, all three `$E04A` countdown milestones, and the
default fallthrough — returns `true` too. There is no path through the
confirmed ROM control flow, at all, that leaves `$E049==1` observable at
either of `$5501`'s two call sites. This is now a structural fact about
the original game, not a gap in this port's tracing: either `$5501`'s
stage 0/1 branch is genuinely dead code in the real ROM (plausible —
Konami-era games shipped dead branches from earlier design iterations
often enough), or the real per-frame road-scroll mechanism the visible
game uses lives somewhere else entirely, e.g. inside the still-unexplored
`$70A8` object dispatcher.

**MAJOR CORRECTION (a later session, prompted by a real "road never
scrolls" bug report from the user): all of the above, from the "Every
other $E049 write site" heading down, answers the wrong question,
because `updateRoadAndScore`'s own entry gate was ported backwards.**
The real instructions are:
```
5501: LD A,(E049h)
5504: DEC A
5505: RET Z
```
`DEC A; RET Z` returns early (skips the whole function) exactly when
`E049` **was** 1 — not when it *isn't*. The Go port had `if
c.RAM[0x049] != 1 { return false }`, the literal opposite. So the
question this entire section spent so much effort answering —
"can `$E049` ever be observed equal to 1 here?" — was never the
question that mattered. What actually gates this function is "is
`$E049` observed to be exactly 1", and the answer to *that* is "no,
essentially always" (state 0, normal driving, dominates), which means
this branch runs on **almost every frame**, not never. Every trace above
is still individually correct (each write site really does write what's
claimed) — the exhaustive search just answered a mirror-image question
without anyone checking the polarity of the one line it all hinged on.
Fixed by flipping the condition to `== 1`; confirmed by driving real
gameplay (forced speed, no synthetic input beyond that) and watching
`$E046` decrement by 22 every qualifying frame, wrapping `$E170`↔`$E380`
exactly as `scrollAndDrawRoadStrip` was always documented to do — the
road-strip buffer genuinely advances now. See the "Road actually
scrolls now" section further below for the full write-up; every
"confirmed dead"/"frozen"/"static" conclusion elsewhere in this document
that cites this section should be read as superseded, not current.

**Follow-up, now resolved (and it isn't `$70A8`)**: `$70A8` turned out,
once actually scoped and largely ported (see its own dedicated section
below), to be a 2-slot traffic-object collision/positioning system —
completely unrelated to background scrolling; its own destination
writes land in the sprite-attribute staging table, never the name
table or `$E046`. The real question — what else could advance
`$E046`, the road-strip's own read pointer, during gameplay — was
settled by an exhaustive search: **every write site to `$E046` in the
entire 16KB ROM** is either the one-time init (`$50B9`, already ported)
or `$5716`'s own decrement — and `$5716` has exactly one caller,
`$5540`, sitting inside this same confirmed-dead `$5501` stage-0/1
branch. There is no other candidate anywhere in the ROM. Concretely
confirmed this isn't just a missing Go implementation: `$573A`
(`drawRoadStrip`, the paint-without-decrementing sibling `$5716` shares
its inner loop with) has exactly two callers, `$41C2`/`stateGetReadyWait`
phase 6 and `$4337`/`stateCrashTrigger`'s crash-into-new-race path —
both already ported, both one-shot paints, neither touching `$E046`
itself.

**Conclusion: the road-strip name-table content is set exactly once
per race (or on a crash-to-restart transition) and never refreshed
again during actual gameplay, in the real ROM as shipped, not merely
in this port.** This is very plausibly a genuine leftover from an
earlier design iteration where the road scrolled via per-frame
name-table redraws, later abandoned — likely in favor of the
sprite-based motion this project *has* confirmed works (traffic scroll
via `$528F`, see `scrollTrafficSpritesAndWrap`) — without anyone
removing the now-dead `$5501`/`$5716` code before shipping. Whatever
`initTrackLayoutData` bakes into the road-strip buffer at race start
(including, concretely, a `$785B`-table record that decodes via this
project's own confirmed font mapping to the literal text "START",
written twice into two adjacent visible rows by that record's own
repeat count — see the dedicated write-up below) is what stays on
screen, frozen, for the whole race. Not chased further as a *porting*
problem, since there is nothing left to port here — the mechanism
genuinely doesn't exist in the traced ROM. A fundamentally different,
pattern-generator-level (not name-table) scrolling trick remains a
theoretical possibility but has no supporting lead and would be a much
more open-ended search than anything above.

**`$77C8`-`$785A`/`initTrackLayoutData` and `$573A`-`$5767`/
`drawRoadStrip` are now ported** — the missing piece `scrollAndDrawRoadStrip`'s
own doc comment (and the collision-detection caveat above) flagged: without
this, the road-strip circular buffer at `$E170`-`$E380` stayed zeroed. Wired
into `stateGetReadyWait` phase 6 (`$41C2`/`$41C5`, previously TODO) and into
`stateCrashTrigger` (`$4337`, the "crash leads straight into a new race"
path).

`initTrackLayoutData` first does a backward `LDDR` fill covering
`$E186`-`$E3AB` (550 bytes) with `$A0` — almost certainly a "grass/
off-track" filler tile, since none of the record data below is anywhere
near the confirmed on-track range `$CF`-`$D8` (`checkTrackBoundaryTile`).
It then makes 9 calls into a shared record-copier (`$783F`): each reads a
2-byte header (repeat count, chunk length) plus that many data bytes from
a single 105-byte ROM table (`$785B`-`$78C3`, embedded as
`trackRecordData`), and stamps that chunk into RAM every 22 bytes (one
road-strip row) for `repeat count` rows, doing the whole pass `width`
times *without* resetting the destination pointer in between (so `width`
groups of `repeat count` rows land consecutively, each replaying the
identical source data) — plus two short direct-poke/`LDIR` fills of a
single repeated byte between some of the calls, not table-driven. Several
of the 9 calls don't get an explicit destination from the caller at all —
they carry over the low byte of the *previous* call's own final
destination pointer (itself only known by tracing how far that call's
repeat-count × 22 advanced it), the same register-reuse idiom `$783F`
uses internally for its own multi-pass loop. Given how easy this kind of
cascading register reuse is to get subtly wrong by hand, every one of the
9 `(src, dst, width)` triples and both direct-fill ranges was derived by
a standalone scratch simulation of the actual register flow against the
real ROM bytes (not hand arithmetic, and not the disassembly listing
read at a glance) before being hardcoded into `initTrackLayoutData` —
see the doc comment there for the full list.

The result, verified directly: reading back the drawn strip shows
`A0 A0 A0 A0 A0 42 42 A0 58 D0 D0 D1 D0 D0 59 A0 A0 A0 A0 4B A0 A0` — a
genuinely coherent road cross-section (grass padding, a `42 42` marker,
grass, a `58`/`59`-bracketed run of `D0`/`D1` road-surface tiles squarely
inside the confirmed safe range, more grass, a `4B` marker) rather than
noise, and confirmed reachable end-to-end by forcing the real state
machine into `stateGetReadyWait` phase 6 and observing both the RAM
table and the painted VRAM row directly, plus a full natural
get-ready-to-`statePlaying` run (2000+5000 frames) with no panics and
the road strip left correctly painted going into gameplay.

`drawRoadStrip` (`$573A`) is nearly identical to `scrollAndDrawRoadStrip`'s
own tail — same 24-row x 22-column projection onto the name table from
`$3801`, same `$E396`→`$E186` per-row wraparound (now shared between the
two as `paintRoadStrip`) — except it doesn't decrement/wrap the `$E046`
read pointer first: it's a one-shot "paint whatever's there right now"
call, not a per-frame scroll step. The original reaches the VDP via raw
`SETWRT`+`OUT` port I/O rather than a `WRTVRM`-style call, which is an
observably identical effect, so both are modeled through the same
`msx.System.WriteVRAM`.

**Important caveat, unchanged by this**: this paints the road exactly
once, at race start (and again on the `stateCrashTrigger` path). It does
not make the road visibly scroll during normal driving — that still
depends on `updateRoadAndScore`/`scrollAndDrawRoadStrip` actually running
during gameplay, and the write-up above already documents why, after
tracing every `$E049` write site in the ROM, that gate is now confirmed
structurally unreachable via the two confirmed call sites. So the
net effect of this session's work is: a real, static road cross-section
now appears at race start instead of a blank screen, and the off-track
collision check now sees genuine safe/unsafe tiles instead of uniform
`$00` — but the player's car does not yet see the road scroll past as
they drive.

**Follow-up after visual inspection**: rendering a real frame revealed
that the road-strip's own "roadside" tile codes (`$42`, `$4B`, `$55`,
`$58`, `$59`, confirmed straight from the traced ROM data, not
misread) all fall inside the exact tile-code range (`$40`-`$7F`) the
already-ported HUD SPEED/FUEL panel graphic
(`assets.HudSpeedFuelPanelPattern`, loaded by `$68AA`) occupies — so
those cells currently show fragments of the HUD panel's own border/icon
art (a green block, a diagonal icon piece, a striped label-underline
bar) rather than anything resembling trees or barriers. Before
concluding this was a missing "roadside decoration" loader, every
`CALL $45E0`/`$4611` (this ROM's two RLE-decode-into-VRAM entry points)
site in the entire disassembly was enumerated and its destination
tile-code range checked: **nothing else in the ROM ever writes to
pattern-generator tile codes `$40`-`$7F`** — `$68AA`'s own call is the
only writer, for the HUD panel, full stop. So this isn't a case of a
missing loader whose call site hasn't been found yet; the road-strip
data and the HUD panel deliberately share that tile-code range, the
same way this ROM's own sprite pattern table packs two logically
separate graphics (vehicles, crash/debris) end to end. Rendering the
whole 60-tile HUD panel sheet confirms the specific tiles the road-strip
borrows are real, deliberately-drawn pixel art (not garbage) — just art
drawn for the HUD panel, doubling as decoration elsewhere. This is most
likely faithful to the original: a normal 8-bit space-saving reuse, not
a bug in this port.

**Update, cast into doubt**: a later session compared this exact frame
against a real-hardware screenshot (see "Real-hardware screenshot
comparison" further below) and found stage 1's roadside *should* show a
plain building, not HUD-panel fragments — the opposite of what this
"faithful" conclusion predicted. That session found and fixed a
different, adjacent bug (`$6A30`, a whole missing per-stage terrain
loader) but confirmed `$6A30` writes to a disjoint character-code range
from the `$40`-`$7F` band discussed here, so it does not explain this
specific reuse away. Whether this `$40`-`$7F` reuse is genuinely faithful
or itself another not-yet-found bug is now an open question, not settled
— see that section for the current state.

**Superseded**: the paragraph below argued this view never moves because
`updateRoadAndScore`'s `$E049==1` gate was unreachable. That gate was
ported with its polarity inverted (see the "MAJOR CORRECTION" note
earlier in this section) — it actually runs on almost every frame, and
the road-strip genuinely scrolls now. Left for the historical record,
not because it's still true:

~~What *is* still a real, already-documented gap is that this view never
moves: since `updateRoadAndScore`'s own `$E049==1` gate is confirmed
(see the write-up above) to never be reachable via the ROM's traced
control flow, the road-strip painted once at race start is what stays
on screen, static, for the whole race — whatever momentary "reused HUD
fragment" oddity is visible in a single frame would, in a real
continuously-scrolling race, only ever be on screen briefly, blended
into a moving scene rather than sitting still under scrutiny. So the
"wrong-looking" roadside graphics reported after visually inspecting a
rendered frame are best attributed to this limitation, not a new,
separate defect.~~ Now that it does scroll, this reasoning actually
still points at something useful: the `$4C`/`$4D` red-block reuse and
the doubled "START" may well turn out to be exactly this — a single
static frame catching an artifact that's normally on screen only
briefly while moving past. Worth a fresh look once the scroll rate is
tuned (see the plan in the "Road actually scrolls now" section).

**Follow-up, now conclusive (the "no writer" part), but "dead branch"
was wrong**: the road-scroll write-up above originally left open whether
some other mechanism (speculatively, `$70A8`) might be the real
per-frame update this port was missing. `$70A8` has since been scoped
and largely ported (see its own dedicated section below) and turned out
to be a completely unrelated traffic-object collision/positioning
system, and a separate, exhaustive search found `$E046` (the road-strip's
own read pointer) has no writer anywhere in the ROM outside `$5501`'s
stage 0/1 branch. That part holds up: there really is exactly one writer.
What doesn't hold up is calling that branch dead — see the "MAJOR
CORRECTION" note earlier in this section. It's the live, normal-driving
path, and `$E046` does advance during real gameplay. Not settled after
all; see "Road actually scrolls now" further below.

**A second, concrete example of what "frozen" actually shows**: one of
the `$785B`-table records `initTrackLayoutData` copies into the
road-strip buffer (`romAddr=$7893`, applied at destination `$E331`,
`recB=2`) is the literal bytes `$33,$34,$21,$32,$34` — which, run
through the exact same confirmed font character mapping this project
already used to decode "SOFTWARE" from the boot credit screen
(`showSoftwareCredit`, `S=$33` matches directly), spell **"START"**.
Because this record's own repeat count is 2, 22 bytes apart (one
road-strip row), it lands in *two adjacent* visible rows at the same
column — confirmed by computing both destinations' offset from the
buffer's actual starting read position (`$E186`, not the buffer's own
low bound `$E170`) and checking the rendered name table directly: rows
19 and 20 both read `33 34 21 32 34` at the same column, with genuine
safe-road tiles (`D0 D0 D1 D0 D0`) immediately above and below. This is
almost certainly a "START" banner meant to be painted across the track
near the beginning of a race and scroll briefly past like everything
else — frozen by the same dead-scroll-mechanism finding above, it
instead sits permanently duplicated in the view. Not a translation
bug: the record-copy mechanism itself was already verified via a
standalone register-flow simulation when `initTrackLayoutData` was
first ported, and re-confirmed here by rendering the actual result.
One loose end, not chased further: in the rendered frame the "START"
tiles show as a solid color block rather than legible letters, which
looks like a separate, smaller gap in the font's own color-table setup
for this specific code path, distinct from the positioning issue above.

**`$5197`-`$51C0`/`initTrafficSpriteSlots` is now ported** — called once
from `stateGetReadyWait` phase 7 (`$41CD`, previously a bare TODO), right
after `drawRoadStrip` paints the initial road. Fills sprite-attribute-
table staging entries 3-24 (22 of the 30 entries
`blitSpriteAttributeTable`'s own `$78`-count call copies to VRAM — entries
0-2 are the traffic/opponent-car slot and the player's own 2-sprite pair,
already set up by `initPlayerCarSpritesAndFuel`) with two back-to-back
batches of records: entries 3-12 at a fixed `X=$51`, entries 13-24 at
`X=$65`, both cycling the same 5 (or 6) `Y` positions `$08`/`$20`/`$38`/
`$50`/`$68`(`/$80`) 24px apart and alternating between two fixed
pattern/color pairs (`$00`/`$01` and `$04`/`$04`) read from a 4-byte ROM
table (`$51C1`-`$51C4`).

That table is only 4 bytes, which surfaced a genuine tracing mistake
before this was implemented: a first pass at the loop assumed its
`LD HL,$51C1` (at `$51A8`) ran once, before the outer `DJNZ`'s own loop
started, making the table something the loop advances through pass by
pass — which would need up to 44 bytes and runs straight into the next
function's own code bytes (`$51C5`, itself a real, separately-called
routine — `stateCrashTrigger`'s own `$432B` target). Re-checking exactly
which address range the outer `DJNZ` (`$51BE: DJNZ $51A7`) actually loops
over shows `$51A8` sits *inside* that range, so the source pointer
actually resets to `$51C1` at the start of *every* pass, replaying the
same 4 bytes each time — the identical "restart the source, only the
destination advances" idiom `$783F`'s own outer loop already established
elsewhere in this ROM. Every `(Y, X, pattern, color)` value this session
attributes to the routine was cross-checked with a standalone scratch
simulation of the corrected loop before being hardcoded, not derived by
hand a second time.

Verified directly (both the isolated per-entry RAM contents matching a
simulated ground truth, and reachability through the real state
dispatch — forcing `stateGetReadyWait` into phase 7 and confirming both
the written entries and the phase advancing to 8), plus no regression in
the full get-ready-to-`statePlaying` sequence. What these 22 sprites
actually depict — additional traffic cars, roadside decoration, something
else — isn't independently confirmed, only their mechanical placement and
byte layout.

**`advanceSoundChannel` (`$4CBC`, `$4D00-$4DD5`), the note/pitch bytecode
interpreter, is now ported** for both "note already playing" command
formats. `internal/psg` and `cmd/sndpreview` (see "Tooling") mean the
audio *output* side was already ready and verified, so this is the piece
that turns triggered sounds from "silently armed" into real tone/volume
register writes — confirmed headlessly across 8 real sound IDs (2000
simulated frames each, no panics, every one advancing its script pointer
and eventually reaching its `$FF` terminator) and by the mixer-bit writes
landing on exactly the bit (3/4/5) matching whichever channel each sound
was allocated to.

Two corrections to an earlier, less-informed pass at this file surfaced
while actually tracing the interpreter body, both now reflected in
`sound.go`'s comments:

- `TickSoundChannels`' activity gate was reading channel-record offset
  $01 (`chOffNoteDuration`, née `chOffActive2`). The real Z80 instruction
  is unambiguous — `$4C12: LD A,(IX+02h)` — offset $02. This matters:
  offset $01 turned out to not be a flag at all, but a note-length
  *template* reloaded into offset $00 at the start of every note.
- The value being tested (offset $02, `chOffSoundID`) is the sound ID
  itself, not a separate "just triggered" sentinel. It doubles as the
  interpreter's activity flag (0 = silent, confirmed by `$4D2F` zeroing it
  when a stream ends) and, via its top two bits, as a format selector:
  bit 6 gates an initial noise-mute, and bit 7 picks between the two
  command formats decoded below. A stored ID of exactly 1 is special-cased
  to skip the interpreter entirely — very plausibly a continuously
  re-resolved sound like engine RPM noise, not "freshly triggered," since
  every other confirmed trigger site is one-shot.

The interpreter's two command formats, both fully traced:

- **Raw tone period** (id bit 7 clear): an optional `$2X` prefix sets the
  note-length template; an optional `$1X` prefix sets the noise period
  (PSG register 6) and enables noise; then a period-high-nibble/volume
  byte plus a raw period-low byte are written directly to the channel's
  tone-period registers, and the volume register gets the period byte's
  high nibble.
- **Pitch-table note** (id bit 7 set): optional `$DX`/`$FX`/`$EX` prefixes
  set a per-repeat transpose amount, an alternate note-off countdown, and
  an octave-transpose count, respectively; the final byte's low nibble
  feeds a DJNZ-style accumulation loop (ported with exact byte-wraparound
  semantics, not a range loop, since the original's zero-check guards a
  different variable than the loop counter — a real edge case, not
  cleaned up away) and its high nibble indexes the 13-entry pitch table,
  shifted left once per unit of accumulated octave transpose. Between
  command reads, a separate "sustain" branch (`$4D35`-`$4D56`) runs a
  periodic volume adjustment (tremolo-like), gated by a loop-count
  comparison, until the note's frame counter reaches zero and the next
  command is read.

Not yet resolved: `chOffLoopLimit`'s real purpose (only ever zeroed, never
read by anything ported so far) and `chOffPitchIndex`'s exact role in the
accumulation loop (confirmed *mechanically* — it's the per-repeat
increment — but the name, kept from before this trace, undersells that
it's not the final table index).

**`engineSoundPitchStep` (`$4C25`-`$4CB9`), the per-frame path used only
when a channel's stored ID is 1 (see `TickSoundChannels` above), is now
ported.** Confirmed, once actually traced, to be genuinely about
continuous pitch — not just plausible: it writes a real 16-bit tone
period to the channel's tone registers every frame, unconditionally.

One correction to `installSoundRequest`'s own doc comment, made once this
routine's only reader was traced: the two fixed 16-bit values it writes
to `$E032`/`$E034` on every trigger are **not addresses**, despite how
`$4BAF: LD HL,$EB0D; LD (E032h),HL` reads in isolation. `$4C89`-`$4C8C`
loads them back one byte at a time in the *other* order (`H=(IY+0),
L=(IY+1)`, i.e. swapped from how they were stored), so the value actually
used is `$0DEB`/`$0DEE` — an entirely ordinary AY-3-8910 tone-period
magnitude, not a pointer anywhere. Easy to miss without literally tracing
the reader; flagged here since it's exactly the kind of "looks like one
thing, is actually another" trap this project has hit before (`$80`'s
RLE semantics, the font's original raw-byte assumption).

The routine has two independent halves:

- **Pitch** (`$4C25`-`$4C91`): every 4th frame (`$E003 & 3 == 0`),
  recomputes a live offset from `$E04F` into `$E036`
  (`recomputeEngineSoundOffset`) — the other 3 frames out of 4 reuse
  whatever's already there. `$E04F`'s `$30`/`$7E` comparisons and
  per-range multiplier (`×32`, `×5+$4B0`, `×16+$500`, `×8+$300`
  depending on the range and a `$E038` flag) read like a "current
  speed" value scaled differently across a few bands, the way a real
  engine's pitch response changes across gears — not independently
  confirmed, but strongly supported by a headless sweep: tone period
  trends *down* as `$E04F` rises from `$00` to `$FF` (2795 → 755 with
  `$E038=0`; 3563 → 1088 with `$E038=1`), i.e. higher "speed" produces a
  higher pitch (lower AY-3-8910 period), exactly the direction a
  speed-linked engine RPM effect should go. Separately, which of the two
  fixed reference periods (`$E032` or `$E034`) gets subtracted from
  *also* alternates on the frame-mod-4 value (frames 0/2 vs 1/3) — since
  the offset itself only changes every 4th frame while the reference
  alternates every other frame, the two interleave into a continuous
  small wobble, plausibly a deliberate vibrato/tremor texture, not
  confirmed as intentional beyond the mechanism.
- **Volume** (`$4C94`-`$4CB9`): a small, separately-gated selection keyed
  on `$E083` (the same byte `stateCrashTrigger` checks) and `$E028` (read
  as a raw byte — the first field of the sound-priority slot
  `triggerSoundRaw` assigns to base IDs 2-4), muting or dropping the
  engine volume during specific crash-adjacent moments. Mechanically
  confirmed (exact comparisons and output values, verified headlessly
  against all reachable branches); the semantic "why" is not.

## `$70A8` scoping notes (not ported yet — this is a size/structure survey only)

`$70A8` has been mentioned throughout this doc as an out-of-scope "massive
8-way object dispatcher," based on an initial skim. Having now actually
walked its call tree address by address (not ported, no Go code written —
this is purely a scoping pass to size the work before committing to it),
that reputation turns out to be **overstated**: it's real and genuinely
un-ported, but it's comparable in size to `$6A70`/`$6CD1`/`$6CA1`
combined (all three fully ported this session), not an order of
magnitude bigger. Recorded here so a future porting pass doesn't have to
re-derive this structure from scratch.

**Overall shape**: `$70A8` runs a loop over exactly **2 object slots**
(`$E0E4` is the loop counter, 0 then 1 — not 32, not a per-sprite loop;
confirmed via the loop's own exit test at `$7298`-`$72A3`, `INC (HL);
CP 2; JP NZ,$70AC`). Each slot is 16 bytes, based at `$E0E5` (slot 0) or
`$E0F5` (slot 1) — confirmed via `$70AC`-`$70B9`'s own direct arithmetic
(`base = $E0E5 + idx*16`) — and consistent with every field offset seen
below being `<16`. `$E0BF` caches "pointer to the current slot" for the
iteration, and a small shared pair of helpers, `$72F6`-`$72FD` (read
slot field C) and `$72E7`-`$72F5` (add a 16-bit delta in place to slot
fields C-1/C), are used throughout instead of repeating the offset
arithmetic inline.

Each slot's byte 1 is a "type" (0-7): type 0 skips the slot entirely
(`$70BD`-`$70BF`) before ever reaching the dispatch table; types 0-7 are
otherwise dispatched through a `$4045`-trampolined 8-entry jump table at
`$70C9`-`$70D8`, 0-indexed by the type value itself — confirmed directly
from `$403B`'s own `ADD A,L` addressing (table + 2×type, not table +
2×(type-1)), correcting an initial guess made from the table's shape
alone that was off by one. All 8 branches return through a shared
"advance to next slot" tail pushed onto the stack ahead of time
(`$728A`) rather than a normal `RET` back into `$70A8` — the same
discarded-return-address idiom already established for the car-behavior
state dispatch (`$6A70`). The 8 table entries collapse to **5 distinct
handler bodies** (two pairs of types share a handler):

- type 0 → `$7289`: a bare `RET` — genuinely a no-op type (and
  unreachable in practice, since type 0 is already fast-pathed away
  before the table is ever consulted; this entry only exists as a
  defensive fallback).
- types 1 and 4 → `$70D9`-`$7122`: the largest handler. Calls
  `$72B8` (a distance/`$E049`-gated sound trigger, `$4B6E`), `$71C6`
  (type 3's own handler, reused here as a sub-call), `$53DD` (see
  below), `$716D`, `$72FE`, `$7123`, and `$7154`, then reads slot field
  6 and cross-references it into the same `$E098` per-row boundary table
  `checkOffTrackCollision` already reads.
- type 2 → `$71A2`-`$71C5` (**now ported**, as `handleObjectType2`):
  calls `$6DC1` (**already ported**, as `checkOffTrackCollision` — see
  that function's own doc comment for how this call site is what forced
  it to take a row-address parameter instead of a hardcoded `$E04C`)
  against the object's *own* position, not the player's.
- type 3 → `$71C6`-`$7208` (**now ported**, as `handleObjectType3`): a
  proximity/offset check between this slot and the *other* slot (an
  inverted lookup — idx 0 reads the other slot's own base, `$E0F5`, idx
  1 reads `$E0E5` — confirmed directly from `$71D1`-`$71DA`'s own
  branch, distinct from `$70AC`'s own idx→base arithmetic which goes the
  other way), transitioning to type 7 with a direction flag and a
  24-frame countdown when both a column and a row test pass.
- types 5 and 7 → `$7209`-`$7228`: a countdown-then-dispatch tail that
  falls into `$7229`-`$7280` (`$724B`, `$727D`/`$7282`), which **writes
  directly into a sprite-attribute-table staging entry** — confirmed by
  a literal `$E132` destination at `$723B`, which is exactly entry 9 of
  the same `$E10E`-based staging array `blitSpriteAttributeTable`
  already copies to VRAM every frame, and that `initTrafficSpriteSlots`
  (`$5197`, ported this session) initializes at race start. This is
  strong, concrete evidence that `$70A8` is what would actually *move*
  those traffic sprites frame to frame — confirming the suspicion raised
  while investigating why they render as static clutter.
- type 6 → `$728A` itself is *also* the shared advance-to-next-slot tail,
  which is why it additionally calls `$7229`/`$724B` on its way through.
  `$724B`'s own core (**now ported**, as `positionTrafficSprite`) reads
  the slot's row/column and, for field-0 values 2 or 4 (the latter only
  every 4th frame), bumps the row by 1, then writes Y/X/pattern straight
  into the object's own 2-sprite pair in the sprite-attribute staging
  buffer — the confirmed link between an object slot's own data and an
  actually-visible sprite, and the concrete proof (rendered and checked
  visually, not just by RAM inspection) that this project's sprite
  compositor genuinely reflects whatever this function writes. type 6's
  own remaining piece (`$7234`-`$7248`, writing a *second*, offset
  sprite when field 0 is 6 or 7) involves a Z80 shadow-register (`AF'`)
  shuffle intricate enough that a first attempt at tracing it by hand
  wasn't trustworthy enough to commit — left as an honest TODO rather
  than guessed at.
  **Important caveat discovered while checking whether any of this
  would actually be visible in real gameplay**: running 20000 frames
  from a real boot shows both object slots stay at type 0 forever —
  nothing anywhere in the ROM this project has traced ever writes a
  nonzero type into either slot, i.e. there is still no confirmed
  *spawn* mechanism. Separately, `positionTrafficSprite` itself doesn't
  persist a field-0=2/4 object's own incremented row back into the
  slot, so even a spawned object wouldn't visibly scroll continuously
  without something else (most likely still inside `$70D9`'s own
  unported tail, or the partially-explored `$528F` — see its own
  `LD HL,$E116`/wrap-near-the-bottom loop, touching the same staging
  buffer — glimpsed but not fully traced) updating that position every
  frame. Verified the rendering mechanism itself is correct regardless,
  via a test harness that manually drives one slot's row across several
  frames and renders the result with this project's own VRAM
  compositor — the sprite visibly moves down the screen exactly as
  expected.

  **Follow-up, resolved**: the continuous-scroll half of this open
  question turned out to live in `$528F` — `advanceCarBehavior`'s own
  `$528C` tail-jump target, previously an untraced TODO dismissed by an
  earlier, shallower pass at this file as "no VRAM writes found in it."
  It's now ported, as `scrollTrafficSpritesAndWrap`: it walks the same
  11 `initTrafficSpriteSlots`-populated 2-sprite pairs `$E11A`-`$E171`
  and decrements each pair's shared Y by 6 — and since
  `advanceCarBehavior` only skips this tail when `updateRoadAndScore`
  returns `true`, which this project's own earlier investigation already
  found never happens in practice, **this runs on essentially every
  frame of real gameplay**. Confirmed end to end with no synthetic test
  harness at all: booting the console for real, entering `statePlaying`,
  and running plain `VBlankHook` calls with no injected state shows the
  traffic sprites' own Y values changing frame to frame, and rendering
  actual frames a few dozen calls apart shows one whole lane of traffic
  visibly scrolling away. Once a pair's Y lands in `[$C0,$E0)` (near the
  bottom), its own decrement stops — consistent with the real hardware's
  `$D0` sprite-list-terminator value eventually being reached, which is
  very plausibly why entire lanes of traffic disappear together rather
  than each car fading out independently (sprite 0's own priority order
  means anything past the first `$D0` in the list stops being drawn at
  all, not just that one entry). What (if anything) resets a pair's Y
  back to the top once it wraps is still not resolved — but the
  decorative traffic scrolling was the more visible, more central half
  of "why doesn't anything move," and it's now fixed.

  **Follow-up, also resolved**: the spawn question for `$70A8`'s own 2
  object slots — genuinely unanswered at the time this caveat was
  written — turned out to have a real answer too: see the dedicated
  "`$6F25`/`$7020`" section further down. `$6F25`, GameplayTick's own
  last unported callee, is exactly that spawn trigger, now ported.

Two loose ends worth flagging for whoever ports this:

- **`$53DD`-`$5433`** (87 bytes, called from the type 1/4 handler, **now
  ported** as `writeApproachingObjectRecord`) writes an 8-byte record at
  `$E105` — and `$5434` (already a named TODO in `state.go`/
  `gameplay.go`'s doc comments, `advanceCarBehavior`'s `$522D` callee,
  described there only as "reads `$E105` (track/scenery pointer?)") is
  its consumer. These two already-separately-flagged TODOs are actually
  one write/read pair, not two unrelated ones — but `$5434` itself
  turned out to be **larger than this scoping pass originally implied**:
  tracing far enough to identify its own gates showed it continues well
  past its own preamble, touching `$E106`/`$E108`/`$E10A`/`$E10C` and
  more beyond that (an `LDI` into `$E13E` was the last thing traced
  before stopping). Left as its own TODO rather than guessed at or
  rushed — it did not turn out to be the quick "read side of a small
  pair" this note originally expected.
- **`$6E4A`-`$6F17`** (already a named `GameplayTick` TODO — `$523F: CALL
  $6E4A`, "gated on `$E049==1`, touches `$E0E4`. Not traced" — **now
  ported**, as `checkPlayerObjectProximity`) turns out to be a close
  sibling of `$70A8`, not an unrelated routine: its own preamble
  (`$6E4F`-`$6E62`) is the *identical* `$E0E4`/`$E0BF` slot-address
  computation `$70A8` uses, it calls the same `$736B` helper `$70A8`'s
  own type-5/7 handler falls into, and it calls `$7352` (`$6ECC`, not a
  coincidence — this is the same `$7352` reached from `$70A8`'s own
  `$72FE` handler, now also ported as `resolveObjectProximity`). It
  loops over **3** iterations, not 2 (`$6F0A`-`$6F14`: `INC (HL); CP 3`)
  — an initial guess that the third was "the player's own position"
  turned out wrong once actually traced: idx 2 lands on `$E105` via the
  exact same `$E0E5+idx*16` arithmetic (`$E0E5+2*16=$E105` precisely),
  i.e. the *same* approaching-object record `writeApproachingObjectRecord`
  writes and `$5434` (still unported) reads, processed here through the
  same generic per-slot logic as a real object. Its own exit paths
  (`$6E99`, `$6EA4`: `JP Z/C,$6F18`) jump into the same `$6F18`-`$6F22`
  tail already reachable from `case 7` of `updateSteeringAndCarState`
  (now factored out as the shared `triggerOffTrackRecovery`) —
  **but working out this call site's own exact stack depth (no CALL
  frames left unconsumed between `$6E4A`'s own entry and this jump)
  shows it resolves to a perfectly normal, single-level return here,
  not the double-discard `case 7`'s own path was modeled as** (that one
  leaves a `CALL $6DC1` frame unconsumed at the equivalent point,
  making its stack shape look structurally similar enough to raise a
  real question about whether `case 7`'s existing `return true` is
  actually correct — flagged in that function's own doc comment as an
  open question for focused re-verification, not changed here since
  it's pre-existing, tested code and this was noticed in passing while
  working on something else). So `checkPlayerObjectProximity` is
  ported as a plain function with no abort-signaling return value at
  all, unlike most of this state machine's other pieces.

**Measured size**: `$70A8`'s own self-contained span runs `$70A8`-`$7373`
(exactly where `$7374`, a different, already-separately-tracked
`GameplayTick` TODO, begins) — **≈715 bytes** across roughly 15-18 small
subroutines, plus the separate 87-byte `$53DD`/`$5434` pair, plus
`$6E4A`'s own ≈206 net-new bytes (its exit paths reuse already-ported
code, see above) — **≈1000 bytes total** for the whole traffic-object
system (`$70A8` + `$6E4A` + `$53DD`/`$5434`). For comparison, `$6A70`
(all 8 car-behavior states, fully ported this session) is a comparable
order of magnitude. This does **not** mean porting it is quick — 5
handler bodies plus ~10 shared/called helpers plus `$6E4A`'s own 3-slot
loop is still a real, multi-part effort, and several of the fields read
(slot offsets 0, 2-5, 7, `$0A`-`$0C`) were seen but not traced to a
confirmed meaning here — but it's a bounded, single-to-few-session task,
not the open-ended "its own multi-session project" this doc previously
described it as before anyone had actually walked its call tree.
Suggested porting order (and progress so far): the loop/dispatch
skeleton and the type-0 no-op first (cheap, establishes the harness) —
**done**; then type 2, the shortest real handler since it only calls
already-ported `checkOffTrackCollision` — **done**; then type 3 — **done**;
then `$53DD` (the write side of the `$53DD`/`$5434` pair) — **done**,
though its own consumer `$5434` turned out bigger than expected and
remains untraced; then `$6E4A` — **done**, also surfacing a real,
flagged-but-unresolved question about `case 7`'s own existing "return
true" (see above); then type 6's own core (`$724B`/`positionTrafficSprite`)
— **done**, confirming the slot-to-sprite rendering link and (see above)
that a real spawn mechanism is still missing; leaving `$5434` itself,
the largest handler (types 1/4, plus its own
`$716D`/`$72FE`/`$7123`/`$7154`/`$72B8` sub-calls), type 6's own small
remaining second-sprite write, and types 5/7 (which need the shared
`$70D9` tail) for last.

## The real object-spawn mechanism: `$6F25`/`$7020` (now ported)

This session spent a while concluding the road-strip's own scroll
mechanism is dead code (see above) and that `$70A8`'s 2 object slots
have no confirmed spawn trigger. Prompted by a description of the
classic "meta-tile row, read-ahead pointer, level-layout stream"
technique many vintage vertical scrollers use, re-examining
`GameplayTick`'s own last unported callee — `$6F25` ("compares `$E04F`
to `$B8`, touches `$E0C1`. Not traced.") — turned up exactly that
system, for real. It is **not** the road-scroll mechanism (that really
is dead code, per the finding above) — it's the spawn mechanism for
`$70A8`'s traffic objects, which this session had already established
was missing.

**The pieces, each independently confirmed from raw ROM bytes:**

- **`$537D`**: a per-stage byte array, 16 bytes/stage — **exactly 6
  stages** (`$537D`-`$53DC`, confirmed precisely: byte-for-byte, `$53DD`
  is where real, already-ported code (`writeApproachingObjectRecord`,
  `$53DD: LD HL,(E078h)` = bytes `2A 78 E0`) begins, with no gap or
  slack). Each stage's own 16 bytes hold small integers (1-15)
  terminated by `$FF` padding — e.g. stage 1:
  `01 02 04 05 02 03 04 08 02 06 05 04 02 01 03 FF`. These are
  **meta-tile IDs**, one per "event" along the stage.
- **`$E0BD`**: a 16-bit RAM pointer into the current stage's own slice
  of `$537D`. `$7020` increments it by 1, reads the byte there, and
  reloads it back to the start of the current stage's own 16-byte slot
  whenever that byte is exactly `$FF` — i.e. `$E0BD` is the "virtual
  Y-coordinate"/read-ahead cursor into the level stream, confirmed by
  its own literal wrap-on-terminator logic, not inferred.
- **`$5311`**: the meta-tile *definition* table, 6 bytes/entry,
  indexed by `(meta-tile ID - 1)`, or `-2` for ID `$0A` specifically on
  Level A (`$7041`-`$7048`) — confirmed via `$704E`-`$7054`'s own
  `idx*6 + $5311` addressing. **Exactly 15 entries** (`$5311`-`$536A`,
  90 bytes) — confirmed the same precise way as the stage array: entry
  15 is the last one that parses cleanly as 6 bytes before `$536B`
  turns out to be a *different*, already-identified table (next).
- **`$536B`**: a color-lookup table, 2 bytes/entry, **exactly 9
  entries** (`$536B`-`$537C`, 18 bytes — again confirmed by the byte
  immediately after being the start of the already-known `$537D` stage
  array, not a guessed boundary). Indexed not by meta-tile ID but by
  the meta-tile *definition's own byte 0* (`$6FFD`-`$7001`,
  `LD HL,$536B; ADD A,A; CALL $403B` where `A` came from the staged
  record's byte 0) — confirmed self-consistent, since every definition
  table byte-0 value is in `0..8`, exactly fitting a 9-entry table.
- **`$E0C1`-`$E0CC`+**: a small "staging area" `$7020` expands each
  6-byte meta-tile definition into — two 16-byte slots (`$E0C3`,
  `$E0D3`), `$E0C1` itself doubling as a "how many staged objects are
  left to place" counter (reset to 2 each time `$7020` stages a fresh
  meta-tile) and `$E0C2` as which of the two staging slots is current.
  The expansion isn't a straight copy: byte 1 of the definition gets
  doubled on Level A (`$706C`-`$7070`); byte 0 of the definition is
  separately stashed across the whole function via a shadow-register
  save/restore (`$7061`/`$707B`, `EX AF,AF'` bracketing everything in
  between) purely so it can be read back afterward — resolving what
  looked at first like an opaque, unrelated `B` register value into a
  handful of stage-number- and meta-tile-byte-0-conditional overrides
  of one further staged byte (`$707D`-`$7093`: stage `3` forces one
  byte to `$80`/else `$6C`; meta-tile byte 0 of `2` or `5` then further
  forces it to `$E4`). This part is fully traced and confident. The
  construction of the *second* staging slot (`$E0D3`, the loop-back
  through `$705F` for `$E0C2==1`) relies on an `EXX` register-bank swap
  purely to get a scratch `HL` for checking `$E0C2` without disturbing
  the real `HL`/`DE` — an initial hand-trace of this got the destination
  address wrong by one byte (landing on `$E0D4`), which turned out to
  be a plain arithmetic slip, not a real ambiguity: a standalone
  register-flow simulation (the same technique that resolved `$77C8`
  and `$5197` earlier this session) showed the second pass lands
  cleanly on `$E0D3`, mirroring the first slot exactly except
  continuing through the definition's own bytes 3-5 instead of 0-2.
  Both slots' construction is ported, as `stageObjectRecord`, called
  twice per `advanceLevelStream` invocation.
- **`$6F25`** (`GameplayTick`'s own `$524B` callee): the per-frame
  consumer. Gated on speed `>=$B8`, and does nothing while `$E0C1==0`
  (nothing staged). Otherwise, for the current staging slot: if it
  looks unclaimed (`entry[3]==0`), searches `$70A8`'s own 2 object
  slots (`$E0E5`/`$E0F5`) for one with a zero type field; once found,
  computes a candidate spawn position and walks it 8 pixels at a time
  via repeated `checkOffTrackCollision` calls (`$6DC1`, already
  ported) until it lands on-track, then **copies the entire 16-byte
  staged record directly into the object slot** (`$6FEC`-`$6FF4`, a
  literal `LDIR`) — this is the actual spawn. It also derives 2 sprite
  color bytes from a third small table (`$536B`, 2 bytes/entry, same
  meta-tile-ID indexing) and writes them into the object's own 2-sprite
  pair in the staging buffer (`trafficSpriteEntryAddr`, already
  ported). Once the current staging slot is otherwise occupied
  (`entry[1]!=0`) it instead runs a small per-frame countdown on it
  (`entry[3]--`, gated to every-other-frame below a speed threshold).
  When `$E0C1` reaches 0 (both staged slots placed), control falls
  straight through into `$7020` again to fetch the next meta-tile ID
  from the stream and restage — i.e. `$6F25` and `$7020` are one
  continuous per-frame loop, not two independently-callable functions
  with a clean boundary.
- `$7020` has a second call site too: `$51C5` (`stateCrashTrigger`'s
  `$432B` callee, tail-jumping via `$51D5`), which re-primes the very
  first meta-tile of a fresh race the same way `$504C`'s own one-time
  init does. **Not ported** — `$51C5` is its own larger, already-flagged
  TODO with several unrelated sub-pieces (`$51F3`, `$51D8`, an `$E0B9`
  reset), out of scope for this pass; `GameplayTick`'s own `$524B` call
  and `$504C`'s own one-time init (now wired into
  `initPlayerCarSpritesAndFuel`, `$509C`-`$50AE`) are both ported and
  are the two paths that matter for normal play.

**The `B`-register mystery and the second staging slot, both now
resolved**: what initially looked like an opaque `B` register value
blocking most of `$7020`'s own tail turned out to be the meta-tile
definition's own byte 0, stashed across the whole function via a
shadow-register save/restore purely so it could be read back later.
And the second staging slot's (`$E0D3`) construction, which an initial
hand-trace placed one byte off (landing on `$E0D4`), turned out to be a
plain arithmetic slip rather than a real ambiguity: a standalone
register-flow simulation (the same technique that resolved `$77C8` and
`$5197` earlier this session) showed it lands cleanly on `$E0D3`,
mirroring the first slot exactly. Both are now ported as
`stageObjectRecord`, called twice per `advanceLevelStream` invocation.

**One deliberate, documented deviation from literal translation**: the
on-track position search (`$6FB7`-`$6FC6`, nudging the candidate column
left by 8 until safe) is genuinely unbounded in the original — on real
hardware, with real road data always present by the time an object can
spawn, some column always is safe. This project's own road-strip is
confirmed static (frozen after a one-time paint, see the road-scroll
finding above), so a given frame's own row is not guaranteed to have
any safe column at all, and a true infinite loop would hang the whole
program rather than just misplace one sprite. Ported with an explicit
cap at 32 attempts (every distinct byte-wrapped column value the
decrement can ever produce, so further attempts would only repeat
values already tried) — a deliberate safety bound, not a ROM-confirmed
behavior.

**Verified**: a scratch test driving `advanceLevelStream` directly
confirmed the stream read/wrap and both staged slots' exact byte
contents against the ROM tables; another confirmed a full spawn cycle
(waiting out the staged countdown, finding an empty object slot,
landing on a real on-track position, and the automatic restage once
both slots are placed) using the real painted road, real `$E098`
table, and real staged data from the real `initPlayerCarSpritesAndFuel`
call chain — not synthetic inputs. Confirming this through unmodified
`VBlankHook`-driven gameplay ran into the same already-documented input
limitation noted elsewhere in this file (`UpdateInputAndTimers`
overwrites `$E009` from the always-idle host input every call, so a
poked "accelerate" bit never survives one frame) — worked around the
same established way, calling `GameplayTick` directly and repeatedly
with speed forced immediately before each call, which reproduced a
real spawn via the actual `GameplayTick` entry point. A 30000-frame
regression run through real `VBlankHook` calls shows no panics and no
hang from the position-search cap.

## Real-hardware screenshot comparison: two stale-TODO wiring bugs and a whole missing loader ($6A30)

A user-supplied real-hardware screenshot of the starting-grid/countdown
screen (`stateGetReadyWait` phase 9 — see its own doc comment in
`state.go`) made it possible, for the first time, to compare this port's
output against genuine MSX output at a specific, reproducible game state
rather than general impressions from playing. Reaching that exact frame
headlessly (no real display available) used a small throwaway harness
driving `VBlankHook` with synthetic `msx.VDP.KeyboardMatrix` presses
(Space to skip the title, Space again to commit LEVEL A), the same
mechanism `cmd/play`'s own `updateKeyboardMatrix` uses — not a new
input-injection path, just automated key presses through the existing one
— then dumping VRAM through the exact same compositor `cmd/play/main.go`'s
`Draw`/`drawSprites` use (copied byte-for-byte into the harness, not
reimplemented/approximated, for the same reason `cmd/play`'s own doc
comment gives for preferring its frame dump over a throwaway
reimplementation). The harness itself was never committed.

The very first comparison showed no traffic cars, no player car, and
wrong roadside graphics — three separate real bugs, two of them one-line
wiring mistakes:

**1. `$5270`/`blitSpriteAttributeTable` was never called from phase 9.**
`stateGetReadyWait`'s phase-9 case had `// TODO: $41E6: CALL $5270 -- not
yet traced` sitting right next to it — stale: `blitSpriteAttributeTable`
was ported and in active use elsewhere (`GameplayTick`, `advanceCarBehavior`)
long before this TODO was written, but the phase-9 call site itself was
never updated to actually call it. Confirmed against `docs/listing.asm`:
`41E6: CD 70 52` is `CALL $5270`, letter for letter. Without this call,
`initTrafficSpriteSlots`' 22 staged traffic-car sprite records sat in the
`$E11A`-staging buffer forever and never reached the real hardware sprite
attribute table, so nothing rendered. Fixed: phase 9 now calls
`c.blitSpriteAttributeTable(0x78)`.

**2. `$6E1D`/`armPlayerCarSprites` was never called from phase 9 either.**
Same shape of bug, same stale-TODO cause: `armPlayerCarSprites` was
ported and traced while investigating `$6A70`/`$6CA1` well before this
TODO was written (see its own doc comment in `gameplay.go`), but phase
9's `// TODO: $41F7: CALL $6E1D -- not yet traced` was never updated
either. Its absence meant the player's own two sprite-attribute-table
entries kept whatever `initPlayerCarSpritesAndFuel`'s cold-default table
(`carInitTable10E`) set them to — both entries using pattern index `$00`
(a sparse, near-blank shape), so the player car rendered as a handful of
almost-invisible black pixels. Fixed: phase 9 now sets `$E04C` (the
player row, alternating `$C8`/`$80` as a small idle bounce — the
comment above this code previously mis-guessed it as an "engine sound
parameter", written before `$6E1D` was known to be the sprite-arming
routine rather than a sound one; corrected in passing) and calls
`c.armPlayerCarSprites()`, which — because `$E053` defaults to 0 — arms
the two entries with pattern indices `$00`/`$04`, the same pairing
traffic cars use, just recolored (`$01` black, `$06` dark red instead of
traffic's `$01`/`$04`), producing a proper reddish-brown car matching the
reference photo.

**3. `$6A30`, the real per-stage roadside terrain loader, was a bare TODO
with no call site at all.** `initHUDAndTrackGraphics`'s own per-stage
switch had three separate `// TODO: CALL $6A30 -- per-stage terrain
table, not traced` comments (stage 2, stages 1/3, and the stage-0-or-≥6
default) and simply skipped the call in every one of those branches.
Only stages 4/5 got real scenery, via the already-ported
`drawTrackBorderAndTreeScenery`. For every other stage (1-3, including
stage 1, the very first level and what the reference screenshot shows),
no per-stage terrain graphics were ever loaded, so the roadside
character-code range these tiles occupy still held whatever
`initHUDAndTrackGraphics`'s earlier, unrelated HUD-panel/gauge-bar loads
had left there — exactly the "reused HUD panel fragments" pattern
`updateRoadAndScore`'s own write-up (above) had already flagged for a
*different*, narrower tile-code range (`$40`-`$7F`) and concluded was
"most likely faithful to the original." That conclusion was reached
without a real reference image to check against; **with one now in hand,
it does not hold up** — the reference screenshot shows an ordinary
building/tree roadside, not HUD-panel fragments.

Traced `$6A30-$6A4E` instruction by instruction: it reads `$E043` (stage),
computes `(stage-1)*4`, indexes a 5-entry, 4-byte-per-stage ROM table at
`$6A51` (`{patternSrcAddr, colorSrcAddr}`, both little-endian), and
RLE-decodes (`CALL $45E0`, twice) the selected pair to two *literal fixed*
VRAM addresses — `$2400` (pattern) and `$0400` (color) — not looped
across all three Graphic 2 thirds via `placeInAllThirds` the way this
same function's other loads are, so (unlike those) it only ever touches
third 0. Confirmed for real, not guessed, by decoding all three
stage-specific pairs (stages 1-3; stages 4-5's table entries are
confirmed identical to each other and to `SceneryTreeBushPattern/Color`,
already extracted and loaded a different way) straight from the ROM with
`cmd/rledecode` and finding every stream boundary lands exactly on the
next table entry's own start address — stage 1's pattern stream ends
exactly at stage 1's own color address, stage 1's color ends exactly at
stage 2's pattern address, and so on through stage 3's color stream,
which ends exactly at `$6645` — `SceneryTreeBushPattern`'s own address,
tying this table directly to an asset already on disk. Rendered each
with `cmd/gfxpreview -color` to sanity-check: stage 1 is grass, water,
red-brick buildings, and (unexpectedly, but clearly legible) a "Konami"
logo tile — presumably a roadside billboard; stage 2 is desert/road-curve
pieces; stage 3 is snow/water/mountain. Extracted as
`assets/gfx/scenery_{pattern,color}_stage{1,2,3}_terrain_*.bin` and
embedded as `assets.StageTerrainPattern{1,2,3}`/`StageTerrainColor{1,2,3}`;
ported as `loadStageTerrainScenery` in `graphics.go`, wired into all three
`initHUDAndTrackGraphics` branches that were bare TODOs. Stage values
outside 1-5 (the `stage == 0 or >= 6` case's `DEC A` wraparound on real
hardware would read ROM bytes adjacent to the table) are a documented
no-op in this port, since there's no ROM loaded at runtime to read
adjacent garbage from — an honest, flagged deviation, not a guess.

**Net effect**: the starting-grid screen now shows 10 traffic cars in
their two-lane grid, the player's own car in reddish-brown at the front
of the grid, and (for stage 1) real grass/building roadside scenery on
the left — a large, confirmed improvement, not a cosmetic tweak.

**Still open, not resolved this session** — two more discrepancies remain
against the reference photo, and the comparison above means the earlier
reasoning for both should be treated as unconfirmed rather than settled:

- A second, narrower band of roadside tile codes (around `$4C`/`$4D` in
  `trackRecordData`, visible as a solid red block where the reference
  shows a plain building) still shows fragments of HUD-panel/gauge-bar
  art. This is the *same* `$40`-`$7F`-range reuse `updateRoadAndScore`'s
  write-up already documented and called "faithful" — a conclusion this
  session's screenshot comparison casts real doubt on (see point 3
  above), but the fix, if there is one, isn't `$6A30` (it writes to
  `$2400`/`$0400`, char code `$80`+, confirmed disjoint from `$40`-`$7F`)
  and hasn't been found. **Ruled out**: a follow-up in this same session
  (see "Second follow-on correction" under "VRAM memory map") found and
  fixed a real Graphic 2 addressing bug (this ROM's pattern/color
  "thirds" collapse to a single band rather than 3 independent ones) —
  but `$40`-`$7F` is written identically to all 3 bands via
  `placeInAllThirds` regardless, so that fix changes nothing here. This
  specific reuse is still unexplained and needs its own dedicated trace.
- The "START" banner still renders twice (rows 19 and 20, identical
  bytes) — the road-scroll write-up's own conclusion, that this is a
  literal, register-flow-verified consequence of the ROM's own
  repeat-count-2 record landing in two adjacent *visible* rows of a
  frozen, one-shot-painted buffer, is mechanically solid on its own
  terms, but the same screenshot shows it only once on real hardware.
  Reconciling these two facts needs a fresh look at how the paint
  buffer's rows actually map onto the visible screen (e.g. whether the
  real starting position/window differs from what this port assumes),
  not more confidence in the already-verified copy mechanism itself,
  which isn't in question. Left as an open item rather than chased
  further this session, consistent with the earlier decision to stop
  digging into the road-scroll rabbit hole once it was confirmed dead
  code.

## Follow-up investigation: the three remaining reference-photo discrepancies, in order

Asked to look at the red block, the doubled "START", and the misplaced
"flag"/countdown-signal icons in that order. One of the three is now
fixed for real (a genuinely missing call), one is now understood far
better even though still open, and one turned out to already be fully
resolved by a second, related missing-call fix.

### 1. The red block (`$4C`/`$4D`/`$4B` roadside tiles): still open, but found a real sibling bug while looking

Went looking for any ROM code that touches pattern/color table bytes for
character codes `$4B`-`$4D` beyond `initHUDAndTrackGraphics`'s own
HUD-panel load (the "$40-$7F reuse" already documented above). Grepped
the entire disassembly for any literal address in that range across
every VRAM-writing primitive this ROM uses (`$45E0`/`$4611` RLE-decode,
`$45CF` constant-fill) -- zero hits beyond the one already-known write.
Directly re-verified `trackRecordData`'s bytes against the raw ROM file
(not just `docs/listing.asm`) to rule out a transcription error -- byte-
for-byte identical. So, as literally traced, code `$4C` really is
character 12 of the HUD-panel sheet (a solid bright-red tile, confirmed
by rendering that sheet with `cmd/gfxpreview -color`), and nothing else
ever touches it. This remains a genuine, unresolved contradiction with
the reference photo (which shows a plain building there) -- not
resolved this session, and not a place to guess further without new
evidence (e.g. a real-hardware VRAM dump at this exact frame).

While chasing this, though: a from-scratch, fully mechanical
register-flow simulation of `initTrackLayoutData`'s entire
`$77C8`-`$783C` body (not a hand-patch of one suspicious spot -- a
complete re-simulation, written specifically to cross-check every
existing call site, not just find new ones) turned up a **genuinely
missing 10th call**: `$77EE`-`$77F5` (`LD HL,78A5h; LD E,36h; LD A,0Fh;
CALL $783F`), sitting between the 3rd and 5th of the 9 calls this port
already had. The simulation reproduced all 9 existing calls' exact
`(romAddr, dst, width)` triples exactly, which is what makes the 10th
one credible rather than a new guess. It writes the record at `$78A5`
(`$4C,$4C,$4D,$4D,$4D` -- the same tile family as the already-known red
block) to column 1 (the road's *left* edge) for 15 rows -- i.e. the
left-side counterpart of the same roadside barrier graphic, previously
entirely absent from this port rather than merely misrendered. Also
discovered in the same pass: the short "direct fill" immediately after
that call (`$77F8`-`$7803`, a `4E`-byte propagate-forward trick with a
trailing `56`) writes to a destination *derived from* the previous
call's own leftover `DE` -- which changes once the missing call is
inserted into the sequence. Fixed both: added
`applyTrackRecord(0xE236, 0x78A5, 15)`, and moved the direct fill's
target from the old (now-stale) hardcoded `$E221`-`$E226` to the
correct, call-derived `$E380`-`$E385`.

Net visual effect: the roadside now shows the same red-tile artifact
symmetrically on *both* sides of the road instead of only the right --
not yet a fix for how it looks, but a confirmed, mechanically-verified
completion of what the ROM's own data actually specifies, and a much
more consistent basis for whoever eventually solves the `$4C`/`$4D`
mystery (a single unified left+right bug, not two unrelated ones).

### 2. The doubled "START": re-verified, still open, one hypothesis ruled out

**RESOLVED, later in this same session** (see the "`applyTrackRecord`'s
real bug" write-up further below): the "mechanically airtight ROM-data
trace" this section relies on was itself wrong — `applyTrackRecord`'s
own repeat loop doesn't reuse the same chunk `recB` times, it advances
through `recB` *different* chunks, and the "START" record's real second
row is 5 `$4F` tiles, not "START" again. Left below for the historical
record of how thoroughly the wrong conclusion was checked before a
user's own side-by-side hardware comparison (on an unrelated visual
artifact) exposed the actual bug.

Re-ran the exhaustive check from scratch rather than trusting the
earlier session's conclusion at face value: re-derived `trackRecordData`
byte-for-byte from the raw ROM file (see above) and re-confirmed, via
the same fresh `$77C8`-`$783C` simulation used for item 1, that the
"START" record's destination (`$E331`, `applyTrackRecord`'s existing
8th call) and its repeat count (2) are exactly what this port already
has -- no error in the mechanism. Considered one new hypothesis this
session's other findings raised: could the still-unported "3,2,1"
countdown digit (see item 3) visually cover the second "START" row,
making it look singular on real hardware despite being doubled in VRAM?
Traced that digit's actual screen position precisely (see item 3) and
found it lands at name-table rows 1-4, nowhere near rows 19-20 where
"START" duplicates -- ruling that hypothesis out. Still an open,
unexplained contradiction between a mechanically airtight ROM-data trace
and the reference photo; needs a different angle, not more scrutiny of
the copy mechanism itself, which isn't in question.

### 3. The "flags"/"countdown signal": found and ported a real missing routine, resolves at least part of this

Traced `$4217`-`$423E` for the first time -- previously a bare TODO in
`stateGetReadyWait` phase 9, guessed at as "presumably the actual
'3'/'2' countdown digit sprites" but never actually implemented because
its two callees (`$403B`, a tiny `HL+=A` pointer-add helper already used
elsewhere, and `$004D`, a *single-byte* `WriteVRAM` primitive already
inlined directly at every other call site that uses it, e.g.
`drawPackedBCDDigits`) were marked unconfirmed. Neither actually needed
new tracing once looked at directly.

The routine blits a 2-tile-wide graphic to name-table column 6, at a row
selected by the current "3,2,1" countdown value (`$E004`) via a 9-byte
ROM table at `$4241`. Getting the reachable counter range right required
correcting a stale assumption from an earlier session: phase 7 does
*not* seed the counter at 5 (there's no code that does that) -- phase
8's own `setGameState(9)` seeds it at **9**, so the values this code
actually runs with are 8 down to 2, not just 4 down to 2 as the old
comment claimed. Reworking the trace with the correct range shows the
routine landing on name-table rows 1, 2, 3 (two counter values each,
alternating between two character-code pairs `$44`/`$45` and `$46`/`$47`
as the count ticks down) and finally row 4 (`$48`/`$49`) for the last
count before the race starts -- i.e. **not digit numerals at all**, but
a row-by-row filling "starting lights" indicator, lighting up one row
at a time as the countdown progresses. This matches the user's own
description ("the countdown signal") far better than "digit sprites"
ever did.

Ported as `drawCountdownDigit` (kept the established name despite the
new understanding, to avoid describing more certainty about exact
per-row meaning than is independently confirmed) and wired into phase
9. Rendering confirms it: at counter=8, it draws over the exact cell
that previously showed a static, unexplained `$42`/`$44` HUD-panel tile
pair -- the very spot the user flagged as a misplaced decoration. That
tile wasn't misplaced scenery at all; it was the *undrawn placeholder*
underneath a countdown light this port hadn't implemented yet.

**Not resolved by this, but resolved later in this same session** (see
the "`applyTrackRecord`'s real bug" write-up further below): the taller
stack of white chevron/arrow-like tiles a few rows below the signal
lights (character code `$4B`, repeated down several rows via a separate,
already-ported `applyTrackRecord` call) turned out to be exactly 3
flags, not 5 identical ones -- the "already-ported `applyTrackRecord`
call" was itself buggy, not just feeding this decoration a value that
happened to look like a repeat.

## Road actually scrolls now: the `updateRoadAndScore` gate was inverted

Prompted by a user report ("the road never moves") and a request for a
plan, not a re-guess: re-read `updateRoadAndScore`'s entry gate straight
from `docs/listing.asm` instead of trusting the earlier, extensively
cross-checked "confirmed dead" conclusion (see the "MAJOR CORRECTION"
note under "What's not yet ported" for where that conclusion came from
and why it was wrong). The real instructions:

```
5501: LD A,(E049h)
5504: DEC A
5505: RET Z
```

`DEC A; RET Z` returns early — skipping the entire function, including
the `scrollAndDrawRoadStrip` call at the end of its stage 0/1 branch —
exactly when `E049` **was** 1. The Go port had `if c.RAM[0x049] != 1 {
return false }`: the literal opposite condition. `E049==1` is the
off-track-recovery car-behavior state; `E049==0` is normal driving. So
the function that an entire prior session's worth of exhaustive tracing
concluded was "unreachable" was actually unreachable *only during
recovery* — it runs on essentially every frame of normal driving, which
is the overwhelmingly common case. Every individual trace in that
earlier session was still correct on its own terms (each `$E049` writer
really does write what was claimed); the only thing wrong was the
polarity of the one comparison the whole conclusion pivoted on, never
independently re-verified because the conclusion it produced ("dead
code") seemed to close the investigation rather than invite a second
look.

Fixed with a one-line condition flip (`== 1` instead of `!= 1`) plus a
corrected doc comment. Confirmed empirically, not just by inspection:
drove a fresh console through real level-select/countdown input, into
actual gameplay (state 5), forced speed the established way (this
port's own "no held input" limitation — see the object-spawn write-up
above for the same workaround), and watched `$E046` every frame:

```
frame 609: E046 changed E186 -> E380 (E078=0001)
frame 610: E046 changed E380 -> E36A (E078=0002)
frame 611: E046 changed E36A -> E354 (E078=0003)
...
frame 628: E046 changed E20A -> E1F4 (E078=0013)
```

Decrementing by 22 (one road-strip row) every qualifying frame, wrapping
`$E170`→`$E380` exactly as `scrollAndDrawRoadStrip`'s own doc comment
already described — this is the *first* real confirmation that
mechanism ever actually runs. Rendered frames before and after ~150
ticks of forced-speed driving show the score climbing (00→57), the fuel
gauge draining, and the traffic-sprite layout visibly shifted — all
consistent with the game genuinely progressing, not a fluke read of one
RAM cell. `$E046` stopped advancing after `E078` reached `0x13` because
`$E049` became 6 (the already-ported, unrelated "out of fuel" state —
expected, since the test forced maximum speed for ~20 real frames with
no fuel pickups and no braking; not a new bug).

**What this does and doesn't mean yet**:
- The road-strip buffer's read pointer genuinely advances during normal
  driving now. That part is fixed.
- `updateRoadAndScore`'s stage 0/1 branch has 4 sibling calls before its
  `scrollAndDrawRoadStrip` tail — `$58AF`, `$56F7`, `$5944`, `$54AF` —
  still ported as bare TODOs (they were skipped under the same "this
  branch is dead" assumption that's now known wrong). `$58AF` in
  particular looks like the scroll's real rate limiter (it decrements a
  counter, `$E07F`, before anything else runs) — until it's traced, the
  scroll above fires on every qualifying frame rather than at whatever
  cadence the ROM actually intends, so the current scroll speed is very
  likely too fast, not yet correct. `$5944` is already decoded (writes
  "CHECK POINT", and "GOAL" on stage 6, into the buffer at a specific
  distance) but not yet ported. `$56F7` and `$54AF` are untraced.
- Stages 2-6 of `$5501` (`$5545`/`$5563`/`$55E9`/`$5642`/`$56AC`) remain
  completely untraced. Stage advancement itself was also found this same
  session: `$4394` (called from a still-unidentified state-transition
  site, likely `stateResultsPhase2`) increments `$E043` and wraps 7→1,
  incrementing a lap counter (`$E042`) on wrap — but only between race
  legs (results screen → next leg), not continuously while driving. Not
  yet ported.
- The `$4C`/`$4D` red-block reuse and the doubled "START" were both
  analyzed under the (now known wrong) assumption that the road never
  moves. Both deserve a fresh look once the scroll rate is tuned by
  `$58AF` — a duplicate or a reused tile that's only ever on screen
  briefly while scrolling past may turn out not to be a bug at all.

## Step 2 of the road-movement plan: `$58AF`/`injectRoadFeature`, and a second real bug it exposed

Tracing `$58AF` (guessed, before tracing it, to be the scroll's own rate
limiter) turned out to be something more interesting and confirmed a
real-hardware hint the user gave earlier in this session almost exactly:
"the game continuously reads a few bytes ahead... pulls the
corresponding row of meta-tiles, and injects them onto the top row of
the screen buffer just before it shifts down." That is, mechanically,
what `$58AF-$590D` does.

**The mechanism, fully traced**: a per-stage seed table at ROM `$5161`
(9 entries, 6 bytes each, indexed by `stage-1`) gives three 2-byte
fields per stage. Stages 1, 4, and 5 share identical values for fields
2 and 3 (only field 1 differs, a separate small pointer copied into
`$E07A`/`$E07B` for the still-unported `$56F7`) — confirmed by reading
the raw table bytes directly, not assumed. Field 2 seeds `$E073`, a
cursor that advances one byte at a time (roughly every 16 real frames)
through a ROM byte stream ($78E5 onward for stages 1/4/5, terminated by
`$FF`); field 3 seeds `$E075`, a pointer to an 8-entry table (at `$7960`
for stages 1/4/5) of `{hold, length, data}` records — `data` is exactly
`hold*length` bytes, confirmed by checking that every single entry's
`hold*length` byte count lands exactly on the next entry's own 2-byte
header with zero gap. Each stream byte's low nibble picks one of the 8
entries; the high nibble picks a destination offset into a 22-byte
staging row at `$E058`. Over `hold` consecutive calls, `length` bytes at
a time get copied from the entry's data into that same destination
offset, advancing through the entry's data linearly. Every single byte
in every entry is a `$80`+ character code — exactly the range this
session's earlier stage-terrain work (`loadStageTerrainScenery`) already
loads real pattern/color data for, including, unmistakably, the "Konami"
billboard tile group spotted while first rendering that tileset.

**A whole missing call, found while establishing where this mechanism
gets initialized**: `$58AF` reads `$E073`/`$E075`, but nothing in this
port ever wrote them — they'd have sat at their cold-boot zero value
forever. Tracing backwards from `$58AF`'s own reads led to `$5132`
(seeds `$E071`/`$E073`/`$E075`/`$E07A`/`$E07B`/`$E06F`, called via the
trivial one-instruction wrapper `$50BF`), and from there to `$50BF`'s
own caller: `$507E`, inside `initPlayerCarSpritesAndFuel`'s larger body
(`$504C`) — a call this port's existing, previously-committed version of
that function simply didn't have, sitting between two already-ported
pieces (the `$E098` fill and the `carInitTable49`/`10E` copy) that
happened to bracket it without anyone noticing the gap. Also found in
the same pass: a leading 846-byte zero-fill and a separate `$E0C1-$E104`
zero-fill at the very start of `$504C`, both confirmed to have zero
observable effect (every byte they touch is either already zero from
`clearRaceRAMBlock` or gets unconditionally overwritten later in the
same function) — correctly still unported, not a new gap. And `$E0B2`,
set to one of two fixed ROM addresses based on lap count and stage,
consumed by the still-untraced `$54AF` — added for completeness of the
init sequence even though nothing dereferences it yet.

**A second real bug, found by asking "where does this staged row
actually go?"**: `injectRoadFeature` writes into `$E058`, and
`scrollAndDrawRoadStrip`'s own `$5732-$5738` step was already documented
(this port's own doc comment, written honestly as "purpose of that
staging copy... not confirmed") as copying 22 bytes between `$E058` and
the circular road-strip buffer — but in the *wrong direction*. The real
instructions are `LD HL,E058h; LD BC,0016h; LDIR` with `DE` already
holding the buffer position — Z80 `LDIR` copies `(HL)->(DE)`, i.e. FROM
`$E058` INTO the buffer, not the other way around. With the direction
backwards, every injection `injectRoadFeature` (or `$592A`/`$5944`/
`$54AF`, still untraced) ever made into `$E058` was silently overwritten
by stale old buffer content moments later, before `paintRoadStrip`
(which reads straight from the buffer, never from `$E058`) ever ran —
so even with the scroll gate fixed and the injector ported, nothing new
would ever have actually appeared. Fixed by swapping the copy direction.

**Verified together, not separately**: rendered frames before and ~150
forced-speed ticks after driving confirm new content genuinely appears
that wasn't there at race start — most strikingly the "Konami" billboard
tile group from the stage-1 terrain set, plus a row of orange-dotted
tiles, neither present in the starting-grid frame. A 30000-frame soak
(fuel kept artificially topped up so the scroll/injection path runs the
whole time, not just the ~20 frames one tank allows) completed with no
panic.

**Still open**: `$56F7`, `$5944` (already decoded — writes "CHECK
POINT"/"GOAL" into the buffer at a specific distance), `$54AF`, and
`$592A` (the per-row *base* fill before features get punched in — its
absence means `$E058`'s untouched bytes carry over stale content from
the previous row rather than a clean per-stage default, a likely source
of any remaining visual glitchiness) all remain untraced. Stages 2, 3,
and 6 have their own separate ROM tables for this same mechanism,
scattered at different addresses, not yet extracted — `armRoadFeatureStream`
safely leaves them a no-op rather than reading the wrong stage's data.

## Step 3 of the road-movement plan: `$56F7` (partial), `$592A`, `$5944`

Continuing down `updateRoadAndScore`'s stage 0/1 callee list. Three
pieces landed cleanly; one large piece (`$56F7`'s own nibble-dispatched
tail) is deferred, honestly, rather than guessed at.

**`$592A`/`fillRoadRowDefault`**: small and self-contained — fills the
22-byte `$E058` staging row with a flat per-stage byte from `$E089`
before anything else touches it. This is the "clean baseline"
`injectRoadFeature`'s own targeted writes were missing; without it, the
untouched parts of each newly-revealed row carried over stale leftover
content from the previous row instead of the intended flat terrain
color.

**`$5944`/`injectCheckpointText`**: already decoded in the previous
session's write-up, now actually wired in. Writes "CHECK POINT" (and,
stage 6 only, "GOAL") straight into the road-strip circular buffer, 28
bytes ahead of the current read pointer, exactly once, when the distance
counter hits `$0F09`.

**`$56F7` (partial) — a genuinely significant find**: `$56F7` opens with
a check ("if `$E07A`==`$FE`, re-run the armRoadFeatureStream init" —
inert for stages 1/4/5, the only ones with real seeded `$E07A` data, so
skipped here rather than ported for a case that can't currently occur)
and then unconditionally calls `$590E`, now ported as
`advanceBoundaryHistoryTable`. That routine shifts the 23-byte
track-boundary table at `$E098` — the exact table `checkOffTrackCollision`
already reads every frame (confirmed by grep, not assumed: `lookup :=
c.RAM[0x098+int(tableIndex)]`) — up by one byte, and writes a fresh
value into the newly-vacant `$E098` from `($E07D<<2)|($E07A&3)`.

Before this session, that table was set once (a flat `$20`) at race
start and never touched again — meaning off-track detection has been
checking a static boundary for this port's entire history, regardless
of how the road curves. That's now fixed. What's *not* independently
confirmed yet is whether `$E07A`/`$E07D` actually encode "the current
curve shape" in a way that makes the resulting boundary values
meaningful end to end — both registers' own broader roles are still
partially traced (see `$56F7`'s own remaining TODO below). So: a real,
verified mechanical fix to a real gap, without a confirmed claim that
curve-aware collision now works correctly in every case.

**Deferred, honestly**: `$56F7`'s own 3-way nibble dispatch (a table at
`$570C` sending `$E07A&$0F` to one of `$5768`/`$5810`/`$57A7`, all three
of which write single bytes into `$E058` via a chased pointer starting
from `$E06F`, itself seeded from a not-yet-decoded ROM structure at
`$5998`) is substantial — comparable in scope to the `$70A8`/`$6F25`
object-spawn system, which needed its own dedicated register-flow
simulation. Traced enough to see the shape (three phase handlers sharing
a common `LDI`-into-`$E058` idiom, with `$E07D`/`$E081` counters
governing phase transitions between them), not enough to port correctly
without guessing at the `$5998` pointer chain's actual contents. Left as
the natural continuation point for a future pass at this same routine,
rather than shipped as a guess. `$54AF` also remains untraced.

Verified: rebuilt, re-ran the same forced-speed gameplay harness (still
scrolls and injects correctly, no regression) and a 30000-frame soak
(no panics).

## `$56F7`'s remaining tail: `injectRoadCurve` -- the road actually curves now

Continuing directly from the previous session's deferred piece: `$56F7`'s
own nibble-dispatched tail ($5768/$5810/$57A7), reaching a pointer chain
starting at ROM `$5998`. Given how much cascading `ADD`/`SUB`/`NEG`
arithmetic feeds into two further table lookups here, this was traced
with a standalone Python register-flow simulation from the start, the
same methodology the `$70A8`/`$6F25` object-spawn system and the
`$77C8` track-layout copier both needed -- not hand-traced first and
simulated only if something looked wrong.

**The data, decoded for real**: `$5998` is a 199-byte shared array of
road-boundary tile codes ($D0/$D1 mixed with $D9-$EC-range
transition values) -- confirmed shared, not 5 separate tables, by
checking that all 5 of `$593A`'s own pointer-table entries
(`$5998`/`$59C1`/`$5A19`/`$5A1B`/`$59C3`) land inside this same 199-byte
span. `$56F7`'s own 3 handlers (`$5768`, `$5810`, `$57A7`, dispatched on
`$E07A`'s low nibble) all share one shape: copy one byte from a chased
cursor into the `$E058` staging row, then a further variable-length run
computed from `$E07A`'s high nibble or `$E081`. `$E07A`/`$E07B`
themselves aren't static -- they're refreshed periodically (via
`$585F`/`refreshRoadCurvePhase`) from yet another ROM stream at `$78C4`
(stages 1/4/5's own "field 1" pointer, previously only used for its
first pair -- see the earlier `armRoadFeatureStream` write-up), read 2
bytes at a time whenever the current phase's own hold counter (`$E07B`)
expires.

**A real bug the simulation itself caught, not just the Go port**: the
first pass at `refreshRoadCurvePhase`'s own `$E081` derivation modeled
`$5893-589D`'s `EX AF,AF'` register shuffle as `(high nibble)*2` --
which produced plausible-*looking* output (in-bounds, non-crashing), so
it wasn't obviously wrong until the register flow was re-derived a
second time by hand and found to actually compute
`(low_nibble-3)+(high_nibble)`. Worth stating plainly: a simulation
running cleanly confirms the mechanism doesn't crash, not that every
arithmetic step is correct -- both still need checking, the same lesson
this project has learned more than once now.

**Missing initialization, found while wiring this in**: `armRoadFeatureStream`
(this session's own earlier addition) never set `$E071`, the read
cursor `refreshRoadCurvePhase` depends on -- confirmed from `$5132`'s
own `$5157: LD (E071h),HL` that it should end up at `$78C6` (2 bytes
past the pair already consumed during init), not 0. Without it,
`injectRoadCurve` would have silently never refreshed past its seeded
first phase. Fixed alongside the main port rather than as an afterthought.

**Verified visually, and it's a bigger effect than expected**: driving
through the harness's forced-speed test now produces a visibly curving
road -- a genuine S-bend, not a straight strip -- stable across repeated
frame dumps (not degrading into visual garbage over time), built from
real `$D0`/`$D1`-range tile codes. A 30000-frame soak with fuel kept
topped up (so the injection path runs continuously rather than the ~20
frames one tank allows) completed with no panic. Whether the *curve's
shape or rate* is faithful to real hardware isn't independently
confirmable without a reference recording -- what's confirmed is that
the mechanism now runs, uses real ROM data, and doesn't corrupt state.

**Still open**: the exact byte where the `$78C4` phase stream "ends"
isn't confirmed -- this port stops safely after its own extracted 16
pairs rather than reading further, unverified ROM bytes (a documented,
deliberate deviation, not a guess). `$54AF` remains untraced, as do the
5 non-stage-0/1 branches of `$5501`.

## `$54AF`/`injectMilestoneSign`: the last of `updateRoadAndScore`'s stage 0/1 callees

Completes the stage 0/1 branch's own callee list (`$58AF`, `$56F7`,
`$5944`, `$54AF`, `$5716` -- all now ported). Small and self-contained
compared to `$56F7`'s own tail: watches `$E079` (distance's high byte)
for a match against a small ROM table, and on a match injects a 4-byte
icon pair into the `$E058` staging row across 2 consecutive calls.

**One real table turned out to be a permanent no-op, confirmed rather
than assumed**: `$E0B2` (set once at race start by
`initPlayerCarSpritesAndFuel`'s own conditional, ported earlier this
session) points at one of two fixed addresses: `$50DC` (a real 5-entry
`{distanceHighByte, entryIndex}` table) or `$50BE` (a single literal
`$FF` byte). Since `$E079` would need to reach `$FF` (roughly 65280
distance units) to ever match that single byte, `$50BE` is a sentinel
meaning "never trigger" — confirmed by checking every other read site of
`$E0B2`'s target and finding none, not assumed from the value alone.
`initPlayerCarSpritesAndFuel`'s own conditional selects `$50BE` whenever
lap count is 0 and stage isn't 6 — i.e. every case this session's
gameplay harness actually exercises — so this feature has no observable
effect in any test run so far, by design, not because it's unimplemented.

**Verified the actually-active path separately**, since the default test
setup can't reach it: patched `$E0B2` to `$50DC` directly mid-run
(simulating what `initPlayerCarSpritesAndFuel` would have set had lap
count been nonzero) and forced the distance high byte to match the
table's first entry. Confirmed exactly the traced behavior: the first
call matches, advances past the entry, and injects the entry's first 2
bytes (`$76,$77`); the very next call injects the remaining 2 bytes
(`$74,$75`); every call after that leaves the injected bytes alone
(overwritten only by the other, already-running injection systems doing
their own unrelated work at nearby offsets, not by this one re-firing).

With this, every callee `updateRoadAndScore`'s stage 0/1 branch reaches
is now ported. What remains of the broader "road movement" arc: the
other 5 stage-specific branches of `$5501` (`$5545`/`$5563`/`$55E9`/
`$5642`/`$56AC`, for stages 2-6) are still bare TODOs, as is `$4394`
(the stage-advance-between-legs mechanism found earlier this session).

## `applyTrackRecord`'s real bug, found from a user's own side-by-side screenshot comparison

Resolves, in one fix: the doubled "START" (previously investigated
twice this session and concluded, on both occasions, to be a faithful
ROM-data duplication with no port bug to find), the 5-identical-flags
artifact a user spotted by directly comparing this port's output
against real hardware, and (as a side effect, not yet re-screenshotted)
almost certainly the still-unexplained uniform road dash pattern and
the `$4C`/`$4D`-area "red block".

**The bug**: `applyTrackRecord` (`$783F`, `initTrackLayoutData`'s
record-copier) has an inner loop that repeats `recB` times, writing 22
bytes apart each time (one road-strip row). This port modeled that as
"write the same `recC`-byte chunk `recB` times" — but the real Z80 code
(`$784B`'s own `LDIR`) advances its *source* pointer by `recC` on every
one of those `recB` repeats as a normal side effect of the copy, so each
repeat writes a **different** `recC`-byte chunk: `recB*recC` bytes
total, not `recC` bytes reused `recB` times. The earlier, extensive
register-flow simulation that found `initTrackLayoutData`'s own missing
10th call (see the "$4C/$4D" write-up above) used this exact same wrong
assumption in its own `call_783F` model — it happened not to matter for
finding the missing call, since that check only depended on the
*destination* pointer's advancement, which this bug doesn't affect, so
the simulation's own correctness on that question gave no signal about
this one.

**Confirmed byte-for-byte against the raw ROM, not just reasoned about**:
re-decoded all 10 of `initTrackLayoutData`'s confirmed `romAddr` values
under the corrected model. Every single one produces varied,
sensible-looking rows where the old model produced uniform repeats:

- **`$7893` (the "START" record)**: row 2 is 5 copies of tile `$4F` (an
  underline), not "START" again. The literal duplication a previous
  session traced very carefully and concluded was a real, unfixable ROM
  quirk was itself an artifact of this bug — the "$783F record-copy
  mechanism is verified correct" claim that conclusion rested on was
  wrong in exactly the way that made the duplicate look intentional.
- **`$78B2` (the flag/marker record)**: the 5 rows are
  `{$A0,$4B},{$A0,$A0},{$4B,$A0},{$A0,$A0},{$A0,$4B}` — flags on rows
  1/3/5 only, with row 3's shifted one column left (`$4B` lands in the
  chunk's *first* byte instead of its second). Matches a user's own
  side-by-side comparison against real hardware exactly: "3 flags, not
  5, on lines 1/3/5, with the middle one a tile earlier."
- **`$7875` (the main road-surface record)**: 2 rows with the
  dashed-centerline tile (`$D1`), 2 without — a genuine intermittent
  dash pattern, not a uniformly dashed line.
- **`$785B`**: only the first and last of 6 rows use the
  `$A0,$42,$42,$A0` pattern; the middle 4 use `$40,$46,$47,$41` instead
  — a real, previously never-rendered pattern in this port.
- **`$78AC`**: 2 distinct rows (`{$50,$51}` then `{$52,$53}`), not one
  pair repeated.

Fixed by advancing the record-copier's own source index by `recC` each
of the `recB` repeats, resetting only at the start of each outer
`width` pass (which the ROM's own `PUSH HL`/`POP HL` around the whole
inner loop confirms *does* restart from the record's beginning each
time — that part of the original translation was already right).

Verified visually: re-rendered the starting-grid frame and confirmed
"START" now shows once, and the flag decoration shows exactly 3 flags
with the middle one shifted left — both matching the user's own
real-hardware reference exactly. 30000-frame soak completed with no
panic.

## Freshly-scrolled rows had blank margins: `$E089` was never actually written

Found from a user's own screenshot of `cmd/play` after driving roughly
half a screen's worth of new rows into view: the road itself looked
correct throughout, but the grass margins on both sides went blank
(literal black gaps) on every row that had freshly scrolled in, while
the original static rows higher up the screen (painted once at race
start by `initTrackLayoutData`, never through this path) still looked
right.

**The bug**: `fillRoadRowDefault` (`$592A`) fills the 22-byte staging
row with one flat byte from `$E089` before any per-stage feature gets
punched into it. `$E089` is read in exactly one place in this port
(`fillRoadRowDefault` itself) and was **never written anywhere** —
its real write site, `$508E-$5099` (part of `initPlayerCarSpritesAndFuel`'s
larger body, a per-stage lookup into a 6-byte ROM table at `$5191`),
had simply never been ported. So `$E089` sat at its cold-boot value of
0 — an invisible tile — for this port's entire history, and every row
composed through the scrolling path got a blank fill instead of grass
wherever nothing else happened to write over it. The road itself still
looked right only because `injectRoadCurve` independently populates
those specific columns; the margins had nothing else writing to them.

This port's own doc comment for `fillRoadRowDefault` had previously
claimed `$E089` was set by "`$5132`'s own per-stage table" — never
actually checked, and wrong; `$5132` doesn't touch `$E089` at all.

Fixed by porting the real `$508E-$5099` lookup (`stageRoadRowFillTable`,
`{$A0,$C5,$AB,$06,$09,$80}` for stages 1-6) into
`initPlayerCarSpritesAndFuel`. Verified: re-rendered a frame with half
new content scrolled in and confirmed the name-table margins now read
`$A0` (grass) instead of `$00` on every row, matching the still-correct
static rows above them; the rendered frame shows continuous grass top
to bottom with no gaps. 30000-frame soak completed with no panic.

## Three of the decorative traffic sprites froze in place: `scrollTrafficSpritesAndWrap` skipped the wrong byte count

Found from a user report: "3 of the blue cars are stuck on the screen. When
starting, all the cars race ahead, except 3 of them. They never move,
sprites just are stuck."

**The bug**: `scrollTrafficSpritesAndWrap` (`$528F`, see the follow-up note
above) walks 11 pairs of decorative traffic sprites, each pair sharing one
Y value at `$E11A`+n\*8. For a pair whose Y has just entered the
"wrap band" (`Y-$C0 < $20`), the original has two cases: if it's the very
last pair (`DJNZ` about to hit zero), jump to `advanceAttractPhase`
(`$52AB`); otherwise (`$52B1-$52B3`: `LD A,04h; CALL $403B`), advance the
address by 4 to line up with the pair's *second* sprite entry — the same
alignment the normal (non-wrapped) branch reaches via its own two `+4`
advances — before falling through to the shared `DJNZ`/loop-continue tail.

This port's earlier translation of that "otherwise" case updated
`remaining` and looped back without ever advancing `addr`. The very next
loop iteration then read/wrote 4 bytes off from where it should have —
i.e., the second sprite's slot in the pair that just froze — and every
pair after it in that same pass inherited the same 4-byte misalignment,
compounding frame over frame. In practice this meant: once a pair's Y
entered the wrap band, that pair correctly stopped (matching the original,
pending the still-open question below), but every later pair in the same
`scrollTrafficSpritesAndWrap` call got its address corrupted and could
freeze mid-scroll or never move at all — exactly the "3 stuck cars"
symptom, not a rare edge case.

Fixed by adding the missing `addr += 4` in that branch.

**Verification**: full-gameplay simulation (forcing speed/fuel each frame,
no steering input, the established workaround for this port's "no held
input" test limitation) turned out to be confounded here specifically —
with zero steering correction the test car goes off-track almost
immediately, and `updateSteeringAndCarState`'s own abort then stops
`advanceCarBehavior` from ever reaching this function again, so the bug's
effect couldn't be observed that way. Verified instead with a temporary
exported test-only wrapper (`ScrollTrafficSpritesAndWrapTEMP`, deleted
after use — not part of the port) that reached real gameplay state through
normal boot/input, then called `scrollTrafficSpritesAndWrap` directly and
repeatedly, bypassing `advanceCarBehavior` entirely: before the fix, pairs
at two of the eleven slot indices visibly froze mid-scroll (one stuck at a
value that was never even a multiple of 6 away from its start, the other
never decrementing past its very first step) while the rest of the pairs
correctly progressed; after the fix, all 11 pairs decrement in lockstep and
each independently reaches the wrap band on schedule. 30000-frame soak
completed with no panic.

**Still open, not fixed here**: what resets a pair's Y back to the top of
the screen once it genuinely reaches the wrap band. With this fix, a pair
now correctly *stops* there instead of corrupting its neighbors, but
nothing yet makes it reappear at the top — so decorative sprites still
accumulate at the bottom of the screen over a long play session rather
than cycling. Not resolved by this fix and not guessed at; left for a
future session (see the follow-up note under `scrollTrafficSpritesAndWrap`'s
own entry above for where this was first flagged).

## `injectRoadCurve`'s refresh path re-dispatched the wrong handler: `$585F` is a loop-back, not a return

Found while investigating a user report of broken road-shoulder/tile
artifacts specifically during curves (screenshot showed a broken
checkerboard-looking patch on the road shoulder and stray tile artifacts
mid-road).

**The bug**: an earlier pass at this port modeled `$5768`/`$5810`/`$5852`/
`$57A7` (the curve handlers `injectRoadCurve` dispatches to) as
independent functions that, on their own internal countdown reaching
zero, called `refreshRoadCurvePhase` (`$585F-$58A8`) and then
**continued with that same handler's own remaining body** -- reading
`$E06F`/`$E07D` as refreshed and running its own copy-length/skip-distance
logic against them. That's not what the ROM does: `$585F`'s own body has
**no early-return path** on real hardware. Both of its exits (`$5890
JP NC,L56F7` and `$58A8 JP L56F7`) jump straight back to `$56F7`'s own
top -- a genuine loop-back with the call stack completely untouched,
re-running the boundary-history update and re-dispatching on the
*newly refreshed* nibble from scratch, which can easily route to a
**completely different handler** than the one that just expired. This is
confirmed, not inferred: `$4045` (the trampoline `$56F7`'s own dispatch
uses) pops and discards its own return address rather than preserving it
(see this doc's disassembler notes under "Tooling"), so `JP L56F7` really
is "restart the dispatch", not "return to whoever called `$56F7`" -- and
`$5716` (the actual scroll+paint routine) turned out to be a separate,
independently-CALLed routine (`$5540: CALL L5716`, run once per
`updateRoadAndScore` pass after `$56F7` fully returns), not something
`$56F7` itself falls into, which was the wrong mental model that first
made the loop-back look like it might need special handling on that end
too. Using one handler's own leftover arithmetic on state that was just
set up for a *different* handler is exactly the kind of thing that
silently corrupts curve tiles without crashing.

Fixed by making `injectRoadCurve` itself a loop matching `$56F7`
directly: `advanceBoundaryHistoryTable` and the nibble dispatch both run
on every pass, including loop-backs; each handler
(`roadCurveHandler5768`/`5810`/`5852`/`57A7`) now returns a bool -- true
for "ended via the `JP`-back-to-`$56F7` path, loop and re-dispatch",
false for "reached a natural RET, stop" -- and `refreshRoadCurvePhase`'s
own return value (true = success = always a loop-back on real hardware,
false = this port's own synthetic "phase stream exhausted" safety guard)
is threaded straight through as that signal instead of being treated as
"okay to continue with the old handler's body." `updateRoadAndScore`
itself was simplified to match: it no longer calls
`advanceBoundaryHistoryTable` separately before `injectRoadCurve` (that
call moved inside `injectRoadCurve`'s own loop, matching `$5701`'s real
position), and it still calls `injectCheckpointText`/`injectMilestoneSign`/
`scrollAndDrawRoadStrip` exactly once each afterward, matching
`$553A`/`$553D`/`$5540` being separate, independent `CALL`s that run
after `$56F7`'s own `CALL` at `$5537` returns.

**Verified** two ways: (1) a from-scratch register-flow simulation of the
corrected model (the earlier `injectRoadCurve` port was already flagged
as needing exactly this kind of check, having previously caught a
real `EX AF,AF'` arithmetic bug the same way) run for 2000 calls from
stage 1's own real seed data, producing stable, in-bounds `$D0`/`$D1`/
curve-transition-range writes throughout the span covered by this port's
own extracted 16-pair phase-stream prefix; (2) `go build`/`go vet`/
`gofmt` clean, plus the standard 30000-frame soak with no panic.

**Not fixed by this change, and not a regression it introduces**: the
visual "checkerboard/broken shoulder" look the user's screenshot showed
is a **separate**, already-documented issue -- see the next section,
which extends the "Still not explained by [the 3-way-thirds] fix" note
above with a direct identification of its scope.

## Extending the known "roadside tiles show HUD-panel fragments" bug: it's the entire `$D0-$F8` curve/boundary tile range

The "3-way Graphic 2 thirds" write-up above already flagged, as **not
explained by that fix** and **not yet traced**, that some roadside tile
codes render HUD-panel/gauge-bar fragments instead of their intended
graphic. Investigating the user's curve-corruption screenshot (checkerboard
patch on the road shoulder, stray artifacts mid-road) pins down exactly
why, and shows it's not a narrow, secondary glitch: it's the **entire**
`$D0-$F8` tile-code range `roadCurveArray`/`trackRecordData` use for every
non-straight road tile (every curve-transition and boundary code this
port's own confirmed table has).

`initHUDAndTrackGraphics` (`$68AA` onward) writes the HUD speed/fuel
gauge-bar pattern assets directly into pattern-table character codes
`$D0` through `$F8` (`placeInAllThirds(patBase, 0x680, ...)` through the
final `mirrorPatternRows(..., 0x798, ...)` call -- `0x680/8 = 0xD0`,
`0x7C8/8 ≈ 0xF9`). That's not a coincidence of overlapping ranges: it's
the **entire span** `roadCurveArray`'s own confirmed tile codes
(`$D0`,`$D1`,`$D3`,`$D7`-`$DF`,`$E0`-`$E8`,`$F0`-`$F8`) occupy. An
exhaustive grep of the full disassembly (the same method the 3-way-thirds
write-up above used) for any other write into this same VRAM range
(`$2680`-`$2800`, given `patBase=$2000`) found exactly one other hit:
`$49B4` (`showRoadTitle`'s own big-font loader, `LD HL,2600h`, DE pointed
at the already-confirmed `bigfont_pattern_road_4A25` asset) -- but that's
the **title-screen** font, loaded during the attract sequence and
overwritten by `initHUDAndTrackGraphics` well before any race starts, so
it can't be the real source either.

In other words: as far as this port's own coverage of the ROM's
disassembly reaches, **nothing ever writes distinct pattern/color data
for the road's own curve/boundary tile codes** -- whatever a race's HUD
init last left in that character-code range (the gauge-bar segment
graphics) is what actually renders wherever the road-strip name table
references those codes, which is exactly the "checkerboard" look the
user's screenshot shows (gauge-bar stripe/segment patterns, not a
road-edge graphic). The base road-surface tiles (`$D0`/`$D1`) still look
correct by coincidence -- a flat filled block looks like plausible gray
asphalt *and* a plausible gauge-bar "full" segment, so the collision
isn't visible there; it only becomes visible on the less common
transition codes with genuinely distinctive intended graphics.

**Not fixed here**: finding the real source (if the ROM has one at all in
the ~27% still-undecoded remainder, reached only through an
as-yet-unresolved indirect/computed jump) needs its own dedicated
tracing pass, not a guess. Given this project's own conventions against
shipping a plausible-looking but unconfirmed graphics load, this is left
as an open, clearly-scoped problem: **the pattern/color source for
character codes `$D0`-`$F8` as used by the road strip (distinct from
their HUD-gauge-bar use) is unidentified.**

## Resolved: this is confirmed genuine ROM behavior, not a porting gap — traced by running the actual cartridge on a real Z80 core

The open question above (is there a hidden road-tile graphics loader this
project's disassembler coverage missed?) is now closed, with a much
stronger form of evidence than static disassembly reading: a from-scratch,
throwaway diagnostic tool (`z80trace`, built against the third-party
`github.com/remogatto/z80` opcode core, never added to this repo or its
`go.mod` — pure investigation scaffolding, same spirit as `cmd/dbgshot` in
earlier write-ups) that runs the **actual, unmodified 16KB cartridge ROM
bytes** — not this Go port — with a minimal MSX hardware shim: a VDP
port emulator (tracking the real `$98`/`$99` two-byte address-latch
protocol and logging every write, tagged with the current PC, that lands
in `$2680`-`$2800`/`$0680`-`$0800`) and BIOS-vector traps for
`WRTVDP`/`RDVRM`/`WRTVRM`/`SETRD`/`SETWRT`/`FILVRM`/`LDIRVM`/`LDIRMV`/
`WRTPSG`/`RDPSG`/`RDVDP`/`SNSMAT` (the real MSX BIOS ROM isn't part of
this project and isn't needed — each of these vectors was already
independently confirmed by this project's own tracing, so trapping them
and executing the equivalent effect directly in Go, then simulating the
`RET`, stands in for the real BIOS routine without needing to write one).

Getting a clean trace took two real emulation bugs of its own, each worth
recording since they'd otherwise look exactly like "confirms the ROM has
no other writer" for the wrong reason:

- **`$0006`/`$0007` (VDP data/control port numbers) were never
  initialized.** `drawRoadStrip`'s own raw-`OUT` loop (`$5744-$5751`)
  reads the port number from these two fixed low-memory bytes rather than
  hardcoding `$98`/`$99` — a real MSX BIOS sets them once at power-on,
  before any cartridge runs, so this port's own harness (which starts
  execution at the cartridge's own entry point, skipping BIOS boot) has to
  seed them manually. Left at their zeroed default, `drawRoadStrip`'s
  entire draw loop silently wrote to I/O port `$00` instead of the VDP —
  the initial trace showed a **completely blank name table** even after
  directly calling `initTrackLayoutData`/`drawRoadStrip`, which would have
  been easy to misread as "confirms nothing else writes there" for
  entirely the wrong reason (nothing was being drawn *at all*, not "only
  the known writer draws there").
- **The idle-joystick PSG read returned the wrong polarity.** `RDPSG`
  was first stubbed to return `0`; `readCombinedInput`'s own logic
  complements that (`CPL`) and expects idle lines to read as all 1s on
  real hardware, so a raw `0` was reported as "every joystick direction
  and button held" for one frame. That spurious edge tripped
  `stateExtra43C3`'s "any fresh input outside the menu" path, which
  forces game state back to 1 and redraws the full title screen —
  overwriting the correctly-drawn road with title-screen name-table
  content moments after it was painted. The symptom (dumped frames
  showing "ROAD FIGHTER"/"PLAY SELECT" instead of the race) would also
  have been easy to misdiagnose as a state-machine bug in the trace
  harness rather than a one-line polarity mistake. Fixed by returning
  `0xFF` (idle) instead of `0`.

With both fixed, the harness correctly boots the ROM, runs
`initHUDAndTrackGraphics`/`initPlayerCarSpritesAndFuel`/
`initTrackLayoutData`/`drawRoadStrip`/`initTrafficSpriteSlots` (the exact
sequence `stateGetReadyWait`'s phases 3/6/7 call, invoked directly to
sidestep needing to precisely replicate the attract-sequence's own input
timing, which isn't what this investigation is about), and renders a
frame that's pixel-for-pixel the correct starting grid (matching this
port's own already-confirmed rendering exactly — pink starting-grid
buildings, blue player car, flags, dashed lane markings). This alone is
strong independent confirmation that both this port's rendering model
*and* the trace harness's VDP emulation are correct, cross-checked against
each other via two completely different code paths converging on the same
image.

Every VRAM write in that entire sequence landing in the `$D0`-`$F8`
pattern/color range traces back to exactly one call chain:
`initHUDAndTrackGraphics` → `$45E0` (the confirmed "repeat 3x across
thirds" trampoline) → `$4611` (the confirmed RLE decoder, `internal/rle`'s
own subject) → either `$004D`/WRTVRM for literal-run bytes or a raw
`OUT (C),A` loop at `$4627`/`$4631` for repeat-run bytes — i.e., **the
HUD gauge-bar assets, decoding through the exact mechanism this port
already models**, and nothing else. No new call site turned up anywhere
in this traced execution.

Directly confirming what those specific bytes actually look like removes
any doubt this is the same content the checkerboard/broken-shoulder
screenshot shows, not just an address-range coincidence: reading pattern
+ color straight out of the real ROM's own decoded VRAM for the exact
codes `roadCurveArray` uses (e.g. `$E9`: pattern `$7E` repeated, color
`$2F` — foreground 2 = medium green, background F = white; `$F2`: pattern
`$FF` solid, color `$2F`, same green/white) is **exactly** the notch-shaped
green/white gauge-bar-segment look the user's screenshot shows, byte for
byte, on the genuine cartridge ROM's own graphics data.

**Conclusion**: this is not a gap in this port's disassembly coverage,
and not something a deeper tracing pass would find and fix. The original
1985 16KB cartridge itself never allocates distinct pattern/color data
for the road's curve/boundary tile codes — it reuses the HUD gauge-bar
character-code range for them, and nothing in the ROM ever gives them a
road-appropriate graphic. On real hardware, a curve that uses one of the
more distinctive codes (anything beyond the plain `$D0`/`$D1` solid
block, which happens to look like plausible gray asphalt by coincidence)
would show this exact same gauge-bar fragment. This port's own
`injectRoadCurve` control-flow fix (see above) is still a real,
independently-confirmed correctness fix — it controls *which* curve
segment and how often, not what the segment's tiles look like — but it
was never going to change this VRAM-sharing artifact, because the
artifact is baked into the original cartridge's own graphics data, not
into this port's control flow. Making curves render with dedicated,
smooth-looking edge graphics would mean *designing new tile art that
never existed in the original ROM* — a deliberate deviation from a
faithful port, not a bug fix, and squarely a product decision for
whoever's driving this project, not something to guess at unasked.

## Two real `injectRoadCurve` bugs found by direct byte-for-byte comparison against the real ROM: `roadCurveHandler5810Tail` and a truncated `roadCurveArray`

Prompted by a much sharper user observation than "curves look corrupted"
in general: **widening/narrowing curves render fine; curves that turn at
a constant width don't.** That's a real, specific enough clue to justify
going back to `injectRoadCurve` with the strongest verification tool this
project has used yet -- rather than more reasoning about the disassembly
by hand, build a small standalone comparison harness (`curvecmp`,
throwaway investigation tooling, not part of this repo) that seeds the
**real ROM**, running on the same real Z80 core (`github.com/remogatto/z80`)
the earlier VRAM-tracing investigation used, with the exact register state
`armRoadFeatureStream`/`$504C` establish for stage 1, then calls the real
`$56F7` hundreds of times, logging the full 22-byte `$E058` staging row
after every call. The exact same seed and call count were run through
this port's own (unexported, so driven via a temporary `_test.go` file,
never committed) `injectRoadCurve`, and the two logs diffed line by line.

This is a strictly stronger check than anything used earlier in this
investigation: it doesn't just confirm a mechanism doesn't crash (the
Python register-flow simulations used when `injectRoadCurve` was first
ported) or that some VRAM address is never written by anything else (the
earlier gauge-bar-bleed-through tracing) -- it confirms this port's own
output is **byte-for-byte identical** to the real cartridge's, call by
call, for every nibble the dispatcher can select.

**Bug 1 -- `roadCurveHandler5810Tail` was missing a second literal byte.**
The first divergence appeared at the very first nibble-1 dispatch (call
24 in the comparison): this port wrote `$D1` at `$E058` offset 8 where
the real ROM wrote `$E7`, and `$E06F` ended up one byte short afterward.
Root cause: `$584B`'s `CALL L5784` doesn't call `roadCurveSharedTail`'s
own entry (`$5786`, what this port had modeled it as) -- it calls `$5784`,
which is the bare `LDI` instruction in the *middle* of
`roadCurveHandler5768`'s own body. Reached this way, that `LDI` performs
a **second** literal byte copy (using HL/DE already advanced by
`5810Tail`'s own first `LDI` at `$5849`) before falling straight through
into `$5786`'s shared arithmetic -- so `$5810`/`$5852`'s path writes two
literal bytes where `$5768`'s own path (which really does reach `$5786`
after exactly one `LDI`) writes only one. Fixed by having
`roadCurveHandler5810Tail` copy two bytes and hand off to
`roadCurveSharedTail` with the cursor advanced by 2, not 1. This affects
every nibble-1/nibble-2 dispatch -- i.e., every `roadCurveTableStarts`
entry except the first (nibble 0) and last two (nibble 3/4, which don't
use this shared tail at all).

**Bug 2 -- `roadCurveArray` was truncated at 199 bytes; the real table
is at least 217.** Fixing bug 1 got most calls matching, but a second,
different divergence appeared later (first at call 62): this port's
output stopped changing at certain `$E058` offsets from one call to the
next, while the real ROM's kept changing. Root cause: `roadCurveHandler57A7`
(nibble 3/4) computes a read start point up to `9 - $E081` bytes *before*
its current `$E06F` cursor and copies up to `$E081` bytes forward from
there -- and as `$E081` grows across repeated nibble-3/4 dispatches, that
range walks past this port's own 199-byte extraction. `roadCurveByte`'s
own bounds guard (deliberately added because this port has no ROM loaded
at runtime to read further data from, see its own doc comment) did
exactly what it was designed to do: it returned `ok=false` and the write
was silently skipped, rather than crashing or reading garbage -- but
silently-skipped writes are indistinguishable from "this byte
legitimately doesn't change yet" without a real reference to compare
against, which is exactly what this comparison provided. Re-reading the
ROM at `$5998` confirmed the tile-code pattern (every byte still in the
confirmed `$D0`-`$F8` boundary/transition range) continues cleanly
through byte 216, then changes character entirely at byte 217 (small
unrelated integers, not tile-shaped) -- a real, non-arbitrary table
boundary, not "extended until the test happened to pass." Fixed by
extending `roadCurveArray` from 199 to 217 bytes with the correctly
re-read data.

**Verification**: with both fixes applied, this port's `injectRoadCurve`
output matches the real ROM's **exactly, byte for byte, for all 950
calls** from stage 1's real seed -- the full range this port's own
extracted 16-pair phase-stream prefix supports before running past its
own already-documented, separate synthetic-guard limit (see
`roadCurvePhaseStreamStage145`'s own doc comment). That span covers every
handler (`$5768`/`$5810`/`$5852`/`$57A7`, i.e. every nibble 0-4) and
several phase transitions each, including the exact nibble-1 and
nibble-3 dispatches the two bugs above affected. `go build`/`go vet`/
`gofmt` clean; the standard 30000-frame soak completed with no panic.

This also resolves the user's own diagnostic observation cleanly: nibble
0 (`$5768`, reading a small repeating 40-byte window starting at
`roadCurveArray`'s own offset 0) was the *only* handler that already
matched the real ROM before either fix, for all 24 calls it covered in
this trace -- consistent with it being the single-source, densest,
most-frequently-dispatched phase in the real stream (used roughly every
other transition) rather than the two rarer paths this fix corrects.

## Third `injectRoadCurve` bug: the phase stream's own `$FE` terminator was neither extracted far enough to be read, nor acted on when read

Prompted by a follow-up user report, after the two fixes above: **after
driving for a while, one whole row of the road turns to blank grass** --
driveable at low speed, a crash at high speed (consistent with the game's
own off-track detection correctly treating an unrendered row as
off-track). "After a while" is the key detail -- not immediately, not
tied to a specific curve shape, but showing up once some amount of
distance/time has passed.

That phrasing pointed straight at `injectRoadCurve`'s own phase stream
(`roadCurvePhaseStreamStage145`, the 16-pair/32-byte extraction the two
fixes above were already verified against) running out. Re-examining
this port's own doc comment for that array turned up an old, wrong
conclusion: it already *knew* the byte right after the 16 extracted
pairs is `$FE`, and reasoned that since `$FE`'s low nibble (14) falls
outside `roadCurveTableStarts`' own 5 valid entries, this was a sign the
extraction was already complete and running further was an error case to
guard against. That reasoning had it backwards. `$56F7` itself opens with
`LD A,(E07Ah); CP FEh; JR NZ,L5701; CALL L50BF` -- a real, load-bearing
check for exactly this value, re-arming the entire phase stream (via
`$50BF`/`armRoadFeatureStream`, the *same* routine that seeds it at race
start) whenever it's seen. `$FE` isn't an error condition on real
hardware; it's the stream's own deliberate wraparound signal.

Confirmed empirically before touching any code: extended the same
real-ROM comparison harness (`curvecmp`, from the two fixes above) to run
the real `$56F7` for 3000 calls instead of 950, well past where the
extracted 16 pairs run out. At call 960, the real ROM's own
`$E06F`/`$E07A`/`$E07B`/`$E071` visibly reset to their exact initial
per-stage seed values (`$E071` jumping straight from `$78E4` back to
`$78C6`), and the curve data repeated from the beginning -- a clean,
intentional loop, not a crash or garbage state.

Two things needed fixing, not one:

1. **This port's own extraction stopped one byte too early to even see
   the terminator.** `roadCurvePhaseStreamStage145` was `[32]byte` (16
   pairs) -- long enough to hold all the *real* curve data, but
   `refreshRoadCurvePhase`'s own bounds guard (`idx+1 >= len(...)`)
   tripped trying to read the 17th pair (the one starting *at* the `$FE`
   byte itself), so `$E07A` never actually became `$FE` in this port at
   all -- the phase stream just silently stopped advancing once real
   data ran out, forever. Fixed by extending the array to 34 bytes (17
   pairs), adding the real `$FE` and the ROM byte after it (which
   belongs to the next, unrelated table, but is never used as anything
   meaningful -- see point 2).
2. **Even with `$E07A` correctly becoming `$FE`, nothing acted on it.**
   The `$56F7`-top re-arm check was previously documented as "inert,
   nothing writes literal `$FE`" -- true only because of bug 1 above.
   Fixed by adding the check to the top of `injectRoadCurve`'s own loop
   (in the same position as the real ASM, before
   `advanceBoundaryHistoryTable`): `if $E07A == $FE { armRoadFeatureStream() }`.
   `refreshRoadCurvePhase` itself was also still bailing out (returning
   `false`, "stop injecting") whenever the freshly-read nibble was 5 or
   greater -- reached for real when nibble 14 (`$FE`'s own low nibble)
   is read. On real hardware there's no such guard: the code
   unconditionally computes a new `$E06F` from `$593A + nibble*2`
   regardless of nibble's value, reading well past
   `roadCurveTableStarts`' own 5 confirmed entries into unrelated ROM
   data for nibble 14 -- but that value is providably never used, since
   the very next loop pass's own `$E07A==$FE` check re-arms everything
   before any handler dispatches on it. This port leaves `$E06F`
   unchanged for that one transitional call instead of computing a value
   from ROM data it doesn't have extracted, and returns `true` (loop
   back) instead of stopping.

**Verified**: with both parts of the fix, this port's `injectRoadCurve`
output matches the real ROM's **exactly, byte for byte, for 10000
consecutive calls** from stage 1's real seed -- over ten full
wraparound cycles of the phase stream, with zero drift. `go build`/
`go vet`/`gofmt` clean; the standard 30000-frame soak completed with no
panic.

### Follow-up: the fix above only ever covered stage 1 -- every other stage had the identical bug from frame one

User report, a later session: playing stage 2, "a whole row of tiles
missing in the road," described as similar to the stage-1 bug fixed
above.

It was the same bug, one level up: `roadCurvePhaseStreamStage145` (the
34-byte array the fix above extracted and verified) is real ROM data
from address `$78C4` -- but that's *only stage 1's own* curve-phase
stream. The array's own name and doc comment claimed it covered "stages
1/4/5" too, on the strength of `armRoadFeatureStream`'s doc comment
establishing those three stages' *seed* values ($E07A/$E07B/$E071) come
from a shared-looking table -- but that doc comment's own follow-up
correction already showed stages 4 and 5 have distinct `field1`
addresses (`$7E83`, `$7E60`) and distinct first-word high bytes ($29,
$31 vs stage 1's $19), i.e. genuinely different ROM data, not shared.
`refreshRoadCurvePhase` was never updated to match: it kept indexing
`roadCurvePhaseStreamStage145` at the fixed base `$78C4` regardless of
which stage was actually running. For every stage but 1, `$E071` (set
correctly, per-stage, by the already-fixed `armRoadFeatureStream`) landed
wildly outside that one array's 34 bytes on the very first call --
`idx+1 >= len(...)` tripped immediately, and `injectRoadCurve` never
injected a single byte of real curve data for the entire stage, from
frame 1 (not "after a while" like stage 1's original bug, but the same
visible symptom: a road row rendering as flat, uncurved grass instead of
following the track's real curvature).

Extracted and independently verified all 6 stages' real curve-phase
streams directly from the ROM (each stage's own `field1` address plus 2,
per `roadFeatureStageRecords`, through to that stage's own real `$FE`
terminator plus one trailing byte -- the same shape and validation
`roadCurvePhaseStreamStage145` already established: every one of the 6
extractions landed a clean, pair-aligned `$FE` with no truncation).
Stages 2 and 3 share byte-for-byte identical streams, matching
`roadFeatureStageRecords`' own already-confirmed identical record for
those two stages. Replaced the single hardcoded array/base with
`roadCurvePhaseStreams`, a 6-entry table `refreshRoadCurvePhase` now
indexes by the current stage, using each stage's own `field1` (already
available in `roadFeatureStageRecords`) as that stage's base instead of
a second hardcoded constant.

Verified headlessly for all 6 stages: after `armRoadFeatureStream` seeds
a stage's real state, repeatedly calling `refreshRoadCurvePhase` (with
`injectRoadCurve`'s own `$E07A==$FE` re-arm applied whenever hit, exactly
as `injectRoadCurve` itself does) cycles cleanly through 2000 iterations
for every stage with no out-of-bounds failure, each one re-arming (i.e.
completing a full lap of its own stream and wrapping) 95-133 times --
confirming all 6 stages now behave the way only stage 1 previously did.
No real-ROM comparison harness was available in this follow-up session
to re-confirm byte-for-byte output the way the original stage-1 fix did;
this is currently verified by extraction correctness (clean terminator
alignment) and by the port's own internal consistency (no bounds
failures across many full cycles), not an independent oracle -- worth
re-running the `curvecmp`-style direct-ROM-comparison harness against
stages 2-6 specifically if it's still available in a future session.

### Related follow-up: "the starting screen always looks like level 1" -- half right, and the wrong half hid a real bug (see the correction at the end of this section)

User report, same session as the fix above: starting directly at any
level (via `cmd/play`'s level-select cheat) appeared to show "level 1's
tile layout" as the starting screen.

**Read the correction at the end of this section before trusting the
conclusion below.** The road-surface half of this analysis holds up; the
"therefore the whole report is explained, nothing to fix" conclusion did
not, and a real, user-visible bug sat behind it for one more round.

Traced to `initTrackLayoutData` ($77C8-$785A, called once from
`stateGetReadyWait` phase 6, right before the road is first painted to
screen): this function bulk-fills the 528-byte circular road buffer
($E186-$E3AB) with a grass filler byte, then punches a fixed sequence of
track-surface stripes into it via `applyTrackRecord`, using ROM addresses
that are **literal immediate operands baked directly into the Z80
instruction stream** ($7875, $78B2, $78A5, $78BE, $785B, $789F, $7893,
$78AC) -- confirmed by reading the function's entire raw disassembly
instruction-by-instruction: there is no read of `$E043` (current stage)
anywhere in this function or in its one caller. Unlike the curve-phase-stream
bug above (where the *addresses themselves* were correctly stage-selected
by an outer function, `armRoadFeatureStream`, and only the *extracted
data* at those per-stage addresses was missing), this function's own
addresses are structurally incapable of varying by stage -- the CPU
executes the identical instructions with the identical operands
regardless of any RAM state, on real hardware exactly as much as in this
port. Every stage's race therefore genuinely starts from the identical
static road layout on real hardware too; there is nothing to extract or
select per stage here.

This static seed is only ever the *initial* view, though: `scrollAndDrawRoadStrip`
(called at the tail of every stage's own `runRoadFeatureDispatch` branch,
confirmed for all six -- stage 0/1, 2, 3, 4, 5, and 6 -- not just the
first) overwrites one fresh row of that same buffer every time the
distance counter's low bit flips (roughly tied to current speed), and
each stage's own `injectRoadCurve`/`injectRoadFeature` calls (now
correctly per-stage after the fix above) are what supply that fresh
row's real content. Verified headlessly: cheat-starting stage 2 and
driving for a couple thousand frames shows the vast majority of the
528-byte buffer diverging from its initial static values (some bytes
coincidentally stay the same -- both the static filler and normal
live-generated roadside content are frequently the same "grass" tile,
so an unchanged byte isn't proof it was never touched), confirming the
buffer is actively and continuously being replaced with live, per-stage
content, not stuck. So what a player perceives as "the starting screen"
is a brief (roughly a couple of seconds, matching how long it takes the
24-row buffer to fully cycle once at typical early-race speed), genuine,
by-design transient shared by every stage -- not a porting gap.

#### CORRECTION: the roadside half was a real bug -- `$45E0` is itself the "all three thirds" routine

The user pushed back on the conclusion above with a precise
clarification -- *the levels do start with a similar straight road
section, that part is fine, but the graphics on each side of the road
should differ per level* -- and then settled it with a real-hardware
screenshot of stage 2's start: its own beach scenery (cyan sea, a white
surf boundary, khaki sand, green palm trees, a small boat) running the
**full height of the screen**, not just a band at the top.

That immediately falsified the intermediate conclusion this
investigation had reached along the way: that per-stage terrain art
legitimately only ever reaches Graphic 2's first pattern-table third
(name-table rows 0-7). `loadStageTerrainScenery`'s own doc comment had
asserted exactly that, reasoning the original passes *literal*
destination addresses (`$6A40: LD HL,2400h`, `$6A4B: LD HL,0400h`)
"rather than looping over all 3 thirds the way `initHUDAndTrackGraphics`'s
other loads do." Both halves of that sentence are individually true and
the conclusion drawn from them is still wrong, because the distinction
it draws does not exist: **every one of those loads, this one included,
reaches VRAM through the same `CALL $45E0`, and `$45E0` is the tripling
routine.**

```
45E0: LD B,03h        ; three thirds
45E2: PUSH BC / PUSH DE
45E4: CALL L4611      ; decode the stream once, into VRAM at HL
45E7: LD DE,0800h
45EA: ADD HL,DE       ; next third
45EB: POP DE / POP BC
45ED: DJNZ L45E2
45EF: RET
```

`$4611` is the single-shot decode; `$45E0` wraps it in a 3-iteration
loop adding `$800` each time. So `LD HL,2400h; CALL $45E0` writes
`$2400`, `$2C00` **and** `$3400` on real hardware -- and this port was
modelling it as one `WriteVRAM` at `$2400`. `placeInAllThirds`' own doc
comment in the same file already stated the rule outright ("the same
tripling every confirmed `CALL $45E0` placement in this file performs"),
and an audit of every `$45E0`-annotated site found this to be the only
one that broke it: the font, Konami logo, big-font title, HUD panel,
gauge bars and UI border loads all triple correctly.

The visible result was that each stage's terrain art only ever reached
rows 0-7, leaving the roadside across the rest of the screen showing
whatever unrelated art already occupied that character-code range --
blank for stage 1 (nothing else writes there), fragments of the UI
diagonal border for stage 2 (which *is* placed in all thirds, at an
overlapping code range). Hence "the sides look the same whichever level
you start."

Fixed by routing both writes through `placeInAllThirds`. Verified
headlessly for all six stages: each stage's own pattern and color assets
now match byte-for-byte at all three pattern thirds (`$2400`/`$2C00`/
`$3400`) and all three color thirds (`$0400`/`$0C00`/`$1400`), and no two
stages load identical terrain except 4 and 5, which genuinely share one
table entry. Rendering stage 2's decoded pattern+color pair confirms it
is exactly the beach scene in the reference screenshot.

**A second bug found in the same table while fixing this**: `stageTerrainSources`
was a 5-entry array, on the assumption the `$6A51` table ended at stage
5. It doesn't -- the raw ROM shows a real 6th entry (`$67D5` pattern /
`$6885` color), and only the *7th* 4-byte slot decodes as unrelated data
(`$2000`/`$00B0`). Being one entry short, the array's own bounds check
made stage 6 return having loaded no terrain at all. Both streams are
now extracted (200 bytes each) and confirmed genuine by the same
tightly-packed-boundary test used for the rest of the table: `$67D5`
ends exactly at `$6885`, which ends exactly at `$68AA` --
`initHUDAndTrackGraphics` itself, the end of the data region, with no
gap anywhere in the chain. Rendered, stage 6's tileset is a coherent
snow scene (fir trees, snow drifts, red-and-white markers).

Lesson worth keeping: two true statements ("the original passes literal
addresses here" and "other call sites are tripled in this port") were
combined into a false one ("so this destination is genuinely
third-0-only") without ever checking what the shared callee actually
does. The check that would have caught it -- reading `$45E0` itself --
took about a minute once the question was asked. When a doc comment
explains why one call site differs from its neighbours, the cheapest
possible verification is to go read the thing they all call.

#### Second round: still no palm trees -- `$FF` is a gap marker, not a stream terminator, and stages 2/3/6 had no feature data at all

The tripling fix above got stage 2's sea and surf on screen but still no
palm trees, which the user reported straight back. Two more real bugs
were behind that, both in `injectRoadFeature` -- the routine that reads
a feature stream and splices roadside content into the row about to
scroll into view.

**1. Stages 2, 3 and 6 had no feature data extracted at all.** This was
an honest, documented gap rather than a wrong guess:
`armRoadFeatureStream` deliberately left `$E073` at 0 for those stages
and `injectRoadFeature` no-opped on that sentinel, because only the
stage 1/4/5 stream (`$78E5`) and entry table (`$7960`) had ever been
pulled out of the ROM. The effect was severe though, and easy to miss
precisely because it was a clean no-op: those three stages got *no*
roadside features for an entire race -- no trees, no signs, no scenery
-- leaving only whatever `fillRoadRowDefault`'s flat per-stage byte
painted. `roadFeatureStageRecords` had been carrying the right pointers
for them the whole time (`$7BAA`/`$7ACD` for stages 2 and 3, which share
one record; `$7F6B`/`$7ED1` for stage 6). Both sets are now extracted
and confirmed by the same tight-packing test used throughout this doc:
stage 2/3's six records chain from `$7ADB` and finish at exactly
`$7BAA`, its own stream's first byte, and stage 6's four run from
`$7ED9` (exactly its table + 8, no gap on either side) to exactly
`$7F6B`. Two independent cross-checks came free: stage 2/3's feature
data is threaded with `$C5`, which is exactly
`stageRoadRowFillTable`'s own stage-2 entry, and stage 6's entry 2 ends
`2B 2F 2E 21 2D 29` -- "KONAMI" under this ROM's confirmed font mapping,
i.e. a roadside billboard.

**2. `$FF` was misread as the stream's terminator.** All three streams
were extracted only as far as their first `$FF` (12 and 13 bytes), and
`injectRoadFeature` treated running off the end of that short array as
"stop injecting". The disassembly says otherwise:

```
58C1: LD HL,(E073h) / INC HL / LD (E073h),HL   ; cursor advances -- unconditionally
58C8: LD A,(HL) / INC A / JR Z,L58E7           ; $FF -> skip the entry reload
58E7: LD A,08h / LD (E086h),A                  ;        and just reset hold to 8
58EC: LD HL,(E073h) / LD A,(HL) / INC A / RET Z ; $FF -> inject nothing this step
```

`$FF` means "no feature at this step" -- a gap in the roadside -- and
the cursor walks past it either way. The streams are really 123, 123 and
131 bytes, threaded with 11, 35 and 15 of those gap markers
respectively; stage 1/4/5's runs from `$78E5` to exactly `$795F`, the
byte before its own entry pointer table at `$7960` (the same
tight-packing boundary again). Reading `$FF` as a terminator meant
features stopped appearing roughly ten seconds into every race, on every
stage -- traced live by logging the `$E073` cursor, which froze at
`$7BB7` (stage 2's first `$FF`) and never moved again.

Each stream is extracted as far as it stays self-consistent -- every
byte either `$FF` or selecting an in-range entry -- and the port stops
injecting past that. Real hardware has no bound and would keep walking
into adjacent, unrelated data, whose low nibbles would over-index the
entry pointer table; the extracted bound is a deliberate, documented
deviation rather than a claim about what the ROM does out there.

Verified by rendering the composited screen (name table plus per-third
pattern/color) at several points in a real driven session: stage 2 now
shows sea, surf, beach parasols and palm trees, matching the reference
screenshot; stage 1 shows its village scenery with hedges and a signed
building; stage 6, which previously had neither terrain nor features,
renders as a snow stage with fir trees. `cmd/play`'s own frame dump
can't run in this environment (importing Ebitengine panics with no
window server), so that compositor is a separate one -- background tiles
only, no sprites -- which is worth remembering when comparing against
what `cmd/play` actually draws.

## `statePlaying`/`stateGetReady` modeled a transient Z80 register decrement as a persistent RAM countdown

Prompted by a user report that both the HUD progress-indicator sprite and
the score display update in big, rare jumps rather than small, frequent
ones, and a hunch that the two are linked (they are -- same root cause).

Both functions dispatch on `$E001` via what looked like a per-frame
`DJNZ` countdown, and this port modeled that literally: decrement
`$E001` in RAM every time the function runs, only taking the "count
reached zero" branch (which calls `GameplayTick`, the function that
draws the score digits and moves the progress sprite) once every ~256
frames.

That's not what the real Z80 code does. `DJNZ` decrements the **B
register**, not memory. `$4091` (`RunGameStateMachine`'s own dispatch
trampoline) loads `B` from `$E001` fresh at the top of *every* call
(`LD BC,(E000h)`), and an exhaustive grep of the entire disassembly for
literal writes to `$E001` turns up exactly two sites in the whole 16KB
ROM: `advanceAttractPhase`'s own increment (`$416A`) and
`exitAttractSequence`'s own reset to 0 (`$4302`) -- both real,
deliberate state transitions. Neither `statePlaying` ($4306-$4324) nor
`stateGetReady` ($4137-$4166) ever writes the decremented value back
anywhere in their own bodies. So there is no countdown at all: `$E001`
is a **stable value**, checked fresh every frame, that only changes via
those two functions' own explicit writes (plus a couple of direct
16-bit stores, e.g. `stateCrashTrigger`'s own `$E000`/`$E001` set) --
meaning `GameplayTick` runs on *every* frame for as long as `$E001`
happens to hold 1, not once per 256-frame cycle.

Every other state handler that reuses `$E001` for its own dispatch
(`stateAttractSequence`, `stateGetReadyWait`, `stateCrashSequence`,
`stateResultsPhase1`/`2`) was **already** correctly modeled this way --
a plain `switch` on the current value, no decrement. Only these two
functions had the "decrement and store" mistake, and both are now fixed
to match: read `$E001` directly, branch on it, never modify it. Also
restored `stateGetReady`'s own `$415D: CALL $504C` (already-ported as
`initPlayerCarSpritesAndFuel`), left as a TODO by an earlier pass before
that routine existed.

**Verified**: `GameplayTick`, called directly and repeatedly with a real
manually-advancing distance counter, updates the progress-indicator
sprite in small, gradual steps every single call (7 distinct Y values
over 20 calls) -- confirming the mechanism itself is correct once
`GameplayTick` is reached at the right cadence. Verifying the *full*,
naturally-driven game loop hits the same pre-existing "no steering input
→ the test car goes off-track/crashes almost immediately" test-harness
limitation documented earlier in this file (checkOffTrackCollisionAndCrash
aborts `advanceCarBehavior` before it reaches the code that would
otherwise drive `$E001` through its natural cycle via
`scrollTrafficSpritesAndWrap`'s own increment) -- not a defect in this
fix, but a known gap in what this port's synthetic-input testing can
exercise without real player-style steering. `go build`/`go vet`/`gofmt`
clean; the standard 30000-frame soak completed with no panic.

## Resolved by the same fix: crashing inside the "CHECK POINT" text at the end of level 1

A separate user report, investigated after the `statePlaying`/`stateGetReady`
dispatch fix above: driving into the "CHECK POINT" text near the end of
level 1 caused an unavoidable crash instead of the level ending cleanly.

Tracing `injectCheckpointText`'s own destination arithmetic (`$5944-$596C`)
confirmed it's already a faithful translation: it writes directly into
the road-strip circular buffer at `$E046+28`, bypassing the normal
`$E058` staging-row pipeline entirely. Because the buffer is exactly
528 bytes (24 rows) -- the same size as the full visible screen -- this
means the row the text lands on genuinely has no real road surface
around it (confirmed by dumping it: `A0 A0 B7 B8 BA A0 [CHECK POINT] A0
A0 A0 A0 A0` -- grass and a decoration, no `$D0`-range road tiles
anywhere in the row). That's not a placement bug -- the same row scrolls
naturally through the whole screen exactly like any other and is not
overwritten before becoming visible (an earlier line of investigation
here was a dead end: the buffer being reused for later content happens
*after* a full lap, i.e. after the row has already scrolled fully down
and off screen, not before it's seen).

The real mechanism: `checkDistanceMilestone` sets `$E049=3` once distance
reaches `$0F12` -- just 9 units past the checkpoint text's own `$0F09`
trigger -- and `updateSteeringAndCarState`'s case 3 (reached only when
`$E049==3`) is the "arriving at the finish" handler: it stops the car and
hands off to the results state, *without* ever calling
`checkOffTrackCollisionAndCrash` the way normal driving (case 0) does.
On paper, by the time the car's own scroll position actually reaches the
grass-only checkpoint row (measured at ~24 buffer-position steps after
the text is written), distance should already be well past `$0F12`, safely
in case 3's non-collision-checking territory.

That safety margin depended entirely on `checkDistanceMilestone` actually
running promptly -- and it's a `GameplayTick` callee, gated by exactly the
`statePlaying`/`stateGetReady` dispatch bug fixed above. Verified directly:
reverting to the pre-fix dispatch code and re-running an unforced drive to
the checkpoint reproduces the bug precisely -- `checkDistanceMilestone`
fires late (distance already at `$0F17`, past the checkpoint), a normal
off-track detection (`$E049=5`) wins the race instead, and the car reaches
the grass-only row still in collision-checking mode. With the dispatch fix
in place, `$E049` reaches 3 only 24 frames after the checkpoint trigger --
long before the car's scroll position reaches that row -- and the game
transitions cleanly to the results state every time. No separate code
change was needed here; this was the same root cause as the progress-
indicator/score-display bug above, just with a much sharper symptom.

## Fuel gauge never visually decreasing: found the real `$E057` write site, ported `$7374`

A user asked directly: "did we hardcode something for debugging
earlier?" -- yes: `drawFuelGaugeBar` was called once at race start as an
explicitly-labeled stopgap, because `updateLaneFlagAndFuel`'s own
drain-triggered redraw (comparing bit 5 of `$E057` against bit 5 of
`$E084`) looked permanently dead -- an exhaustive grep of this port's
disassembled ~73% of the ROM for a direct write to `$E057` found none.

Traced the real write site by watching every write to `$E057` on the
actual ROM running on a real Z80 core (the same technique used for the
road-curve and VRAM-sharing investigations elsewhere in this file):
`$7374` (GameplayTick's own `$5236` dispatch, previously an untraced
TODO) writes it via a 16-bit `LD (E056h),HL` at `$7388` that touches
`$E057` as `HL`'s high byte -- invisible to a literal 8-bit-address grep,
which is exactly the "IX/IY-relative addressing" red herring the earlier
stopgap's own doc comment guessed at (the real explanation turned out to
be a 16-bit instruction, not indexed addressing).

`$7374` accumulates a level-dependent step (`$01FF` for level B, `$0180`
for level A) into a 16-bit counter at `$E056`/`$E057` every qualifying
frame (gated on `$E049!=1`, matching `checkDistanceMilestone`'s own
documented gate). Ported as `advanceScrollSpeedCounter`, wired into
`GameplayTick` at its real `$5236` call site (replacing the TODO), and
the stopgap call removed from `stateGetReadyWait`'s phase 3.

**Verified**: fuel now drains and the gauge redraws every ~20 frames in
this port, and the exact frame numbers of every drain event (`i=63, 83,
104, 127, 147, ...`) match the real ROM's own trace precisely, frame for
frame, for the first 900+ frames checked. `go build`/`go vet`/`gofmt`
clean; the standard 30000-frame soak completed with no panic.

**Follow-up correction**: removing the stopgap immediately surfaced a
second, real gap, reported directly by a user: the gauge was invisible
for the first ~20 frames of every race, until the first natural drain
event. This write-up originally concluded that matched the real ROM too
("no third call site for the fuel gauge's own draw entry") -- that
conclusion was wrong, and easy to reach precisely because a literal grep
for `CALL L7444` alone won't find a `JP` into the same address. Tracing
the real ROM's own VRAM at the exact moment gameplay begins (before any
drain trigger could have fired) showed a fully-lit fuel bar already
present, traced to `drawHUDBorderDecoration`'s own tail
(`$740E: JP $7444`) -- a genuine third call site, mistranslated in this
port as a call to `redrawScoreAndHiscoreDigits()` instead (see that
function's own doc comment in `graphics.go` for the full story and how
that specific mistake happened). Fixed; this port's own initial VRAM
content at race start now matches the real ROM's exactly, byte for
byte.

## Road objects (traffic cars, oil, gas spill) never drawn: $70D9 ported, empirical verification blocked by a test-harness limitation

Follow-up to the root-cause writeup below (kept for context): every
object placed into a `$70A8` slot started life with its type field
hardcoded to `1`, and `runObjectSlots`' own dispatch had `case 1, 4:
// TODO: $70D9 -- not yet ported`, so no spawned object was ever
transitioned past "staged." `$70D9-$7151` (`activateStagedObjectType`)
and its callees are now ported: `checkApproachingObjectSound` ($72B8,
proximity-warning honk), `handleObjectType3` (already existed),
`writeApproachingObjectRecord` ($53DD, already existed),
`resolveApproachingObjectDirection` ($716D, the type-1→5 transition),
`checkSiblingProximityAndResolve` ($72FE, with one honest gap -- see
below), `pushAwayFromTrackBoundary` ($7123), `pushByRelativeSpeed`
($7154), and `pushAwayFromCurveBoundary` ($70EE-$7151, the inline tail).
`runObjectSlots`' `case 1, 4` now calls `activateStagedObjectType`
instead of doing nothing.

**Deliberate gap**: `checkSiblingProximityAndResolve`'s own tail
($733D-$7351) is left unported. It's reached only when two objects are
already close together (a genuinely rare condition), and tracing it
safely would require modeling a Z80 zero-flag value threaded across a
`RET` back into the caller -- the same class of implicit-register-state
risk this project treats as a hard stop rather than a guess (see the
`checkOffTrackCollision` `resultB` precedent below, which *was* safe to
model because the flag's meaning was independently confirmed).

**Empirical verification status**: build, vet, gofmt, and the standard
30000-frame soak test all pass. Whether objects visually spawn and move
during real play was not confirmed, and the reason turned out to be
interesting in its own right, not a shortcoming of the new code. A
throwaway real-ROM trace (a third-party Z80 core running the actual ROM
bytes, same methodology as the `$740E`/fuel-gauge fix above) showed that
under every scripted input this session tried against the *real,
original ROM* -- no steering input at all, holding only the accelerate
key, and crude alternating left/right steering -- the real game's own
two `$70A8` object slots *also* never receive a spawned object within
20000 frames. The common thread: `spawnStagedTrafficObject`'s
32-attempt "find a safe spawn column" search, and separately
`checkOffTrackCollisionAndCrash`'s own per-frame check, both depend on
the actual rendered road tile at a computed VRAM address (see
`checkTrackBoundaryTile`) -- not a simple numeric range -- so any test
input that doesn't happen to land the player/object on an actually-
drivable tile at that exact frame trips the same "unsafe" classification
this project has already documented elsewhere (a forced, unrealistic
RAM value reliably reads as off-track). A real player doesn't crash
this way because real steering continuously adjusts column to stay on
whatever the road strip actually looks like at that row.

Given that, this port's own `activateStagedObjectType` was instead
verified in isolation: a temporary test (`internal/game/
object_temp_test.go`, not committed) forced a real object slot straight
into type 1 with an object row/column pair independently confirmed
on-track, then ran 10000 frames. Result: no panic, the slot's type
never corrupts, and `pushAwayFromTrackBoundary`'s own
`checkOffTrackCollision` call correctly reports that position as safe
and leaves it alone (matching the design -- objects should only get
pushed when actually near a boundary). This confirms the new code runs
safely and its boundary check agrees with the rest of the port's own
(already real-ROM-verified) collision logic, but doesn't independently
confirm `resolveApproachingObjectDirection`'s type-1→5 transition or the
push functions' own position math against real-ROM ground truth, since
neither this port nor a scripted real-ROM trace could get a spawned
object into a state where those paths run. That would need either
sustained interactive play (a human or simulated player actually
steering to follow the road) or a much larger investment in a synthetic
"drivable road" test fixture -- out of scope for this pass.

**Follow-up: a real spawn confirmed.** Rather than static forced values,
a further temporary test drove the player reactively -- reading
`checkOffTrackCollision`'s own `resultB` each frame (the same signal
`pushAwayFromTrackBoundary` uses) and pressing left/right through the
real keyboard-matrix input path to steer back on track, holding
accelerate throughout, with no other RAM forced. Under this more
realistic input, `objectSlotBase(0)+1` genuinely transitioned `00 -> 01`
at frame 363 -- a real spawn through the actual, unmodified
`spawnStagedTrafficObject`/`activateStagedObject` pipeline landing on a
slot now handled by this session's new `case 1, 4` dispatch, not a
forced test value. This is the positive end-to-end confirmation the
isolated test above couldn't provide: objects do reach a real slot
during play that at least superficially resembles steering. The
simplistic bang-bang steering crashed the car shortly after (~140
frames later), so this didn't run long enough to also observe a type
1→5 transition -- see the next section for what happened after that
crash, a separate, newly-found issue.

## Crash recovery got stuck oscillating between game states 5 and 6 (found and fixed)

Found while extending the auto-steering test above to run longer: once
a crash actually happened (`$E049` becomes 6, updateSteeringAndCarState's
own state 6), the port never recovered -- `$E000`/`$E001` oscillated
5→6→5→6... forever, one transition every couple of frames, with `$E04F`
(speed) pinned at 0. Traced the mechanism, all of it already-ported,
already-correct-per-the-listing code:

1. State 6 (`updateSteeringAndCarState`'s own dispatch case, $6C31-$6C39)
   unconditionally clears `crashFlag` ($E045=0) every time it runs, real
   ROM behavior confirmed against the listing (`CALL $440C` then `XOR A;
   LD (E045h),A; RET` -- the call's own result genuinely is discarded in
   the original too, not a porting gap).
2. `statePlaying`'s own post-`GameplayTick` check (state.go:714-722) then
   sees `gameOverFlag==0` and now `crashFlag==0` too, so it falls to the
   "neither" branch and calls `exitAttractSequence(0x18)`, advancing
   `$E000` from 5 to 6 (stateCrashTrigger) and resetting `$E001=0`.
3. `stateCrashTrigger` (game state 6) checks `$E083` (fuel); since it was
   still nonzero, it just set `crashFlag=1` again and jumped straight
   back to `$E000=5, $E001=1` -- landing right back where step 1
   started, since the next `$E001==1` frame calls state 6 again and
   clears `crashFlag` right back to 0. Nothing in this cycle ever reset
   `$E049` away from 6, so it repeated forever.

**Root cause**: `stateCrashTrigger`'s own body had a `// TODO: $432B:
CALL $51C5 -- not yet traced` sitting right before the `crashFlag=1`
line -- a whole subroutine skipped, not just a missing branch. Tracing
$51C5 found it's the real "resume driving after a non-fatal crash"
reset: it re-copies `carInitTable49` into `$E049-$E053` (**whose first
byte is 0** -- this is what was supposed to reset `$E049` back to 0 and
never ran), repositions the car's column from `$E0AD`, hides all 11
decorative traffic sprites, clears both real object slots plus both
staging slots, and restarts the level stream. Ported as
`recoverFromNonFatalCrash` (gameplay.go) and wired into
`stateCrashTrigger` in place of the TODO; the `$51FF` sub-piece it
reuses was already ported as part of `initPlayerCarSpritesAndFuel` (race
start), factored out into a shared `resetCarPositionTables` since it's a
real, separate ROM subroutine called from both places, not a synthetic
Go-side refactor.

**Verified**: the same reactive-auto-steering test that found the
soft-lock now runs a full race lifecycle cleanly -- multiple
crash/recover cycles with fuel visibly draining each time, then a clean
transition to the results screen once fuel actually reaches 0, matching
real game structure. The object-spawn test now sees *two* concurrent
real objects (both `$70A8` slots nonzero at once between frames 942 and
1346) that get correctly cleared by the crash reset's own object-area
wipe -- stronger end-to-end confirmation of the road-objects fix above
than the single spawn originally found. Build/vet/gofmt clean; the
standard 30000-frame soak test (varied scripted steering, not reactive)
still passes with no regressions.

One byte's role remains an inferred default rather than an independently
confirmed one: `$E0AD` (the post-crash respawn column source) has no
write site anywhere in this ROM's currently-decoded ~73%, so in practice
it currently always resolves to a fixed column of $20 -- see
`recoverFromNonFatalCrash`'s own doc comment.

**Follow-up correction (resolved, not just re-investigated):** the claim
above turned out to be stale, not wrong-in-principle -- re-checked by
scanning the *raw ROM bytes* directly for the literal operand pair `AD
E0` (not just the disassembled listing, in case something had been
misclassified as data), and confirmed again there's only the one read
site at `$51F3`. But `$E0AD` doesn't need a literal `LD (E0ADh),...`
instruction to be written: it's the 22nd byte of the 23-byte
`$E098-$E0AE` track-boundary table `advanceBoundaryHistoryTable` (ported
later than this section, in the "Step 2/3 of the road-movement plan"
write-ups below, and evidently never connected back to this earlier
note) shifts by one position every time it runs -- every frame, via
`injectRoadCurve`. Its own computed addressing (`c.RAM[addr+1] =
c.RAM[addr]`) is exactly the kind of write a literal-byte-pattern search
can't find. Verified end-to-end: drove real sessions of varying length
and steering, then forced a non-fatal crash after each -- the recovered
column genuinely differed every time (`$60`/`$50`/`$40`/`$48` across four
drive histories), not stuck at `$20`. `recoverFromNonFatalCrash`'s own
doc comment is updated with the full trace. Lesson for next time: a
doc-comment claim written before a *later* section's own discovery can
go stale silently if nothing links the two back together -- worth a
search for the address before trusting "no write site found" again.

### Original root-cause trace (superseded above, kept for the callee-by-callee groundwork)

- `$72B8` ($72B8-$72E6): gated on the game being active and the current
  slot's own field 0 (not field 1 -- a different byte) equaling 1;
  checks proximity to the player and speed, and if both close, triggers
  a sound effect (id 9).
- `$716D` ($716D-$71A1): gated on field 0 == 3; checks the player's row
  against the object's own row (offset 6) within a fixed band, and (if
  close) the player's column against offset 8 before writing offsets
  8/9 and transitioning the object to type 5 via the same `$719C-$71A1`
  shared tail `setObjectType` already models.
- `$7123` ($7123-$7136): gated on distance being before the checkpoint
  (`$0F09`, the same threshold `injectCheckpointText` uses) and a
  `checkOffTrackCollision` result, checking for an *unsafe* position
  (the opposite sense of the collision check's normal use elsewhere) --
  now ported as `pushAwayFromTrackBoundary`.

## `updateRoadAndScore` stage 2 ported; a real bug found and fixed in already-shipped stage 4/5 code

Started tracing `updateRoadAndScore`'s remaining stages 2-6
(`$5545`/`$5563`/`$55E9`/`$5642`/`$56AC`). Reading all 5 first: each one
tail-jumps back into a specific point of the already-ported stage 0/1
handler's own body once its own stage-specific prefix finishes (stage 2
and 4 both `JP` into `injectRoadCurve`; stage 3 and 5 both `JP` into
`injectCheckpointText`; stage 6 `JP`s all the way back to
`injectRoadFeature`) -- so the actual missing work per stage is just its
own prefix, not a whole separate tail.

**A real, confirmed pre-existing bug, found before porting anything
new.** `armRoadFeatureStream` (which sets up state `updateRoadAndScore`'s
stage handlers read) was itself an incomplete port of `$50BF`: an
earlier pass modeled it as "a trivial one-instruction wrapper" around
`$5132`, missing that `$50BF`'s own body is `$5132` *then* a 6-entry
per-stage jump table at `$50C9` -- a whole second half, only found while
tracing why stage 2/3/6's handlers read RAM state ($E08A/$E090/$E092/
etc.) that nothing in this port ever wrote.

Fixing that surfaced a second, independent bug already in the *shipped*
part of `$5132`'s own translation: it hardcoded `$E07A`/`$E07B`/`$E071`
to stage 1's own values for stages 1, 4, *and* 5 alike, on a doc
comment's claim that the three stages' own field-1 pointer targets have
"identical first 2 bytes." Reading the real ROM at those three addresses
directly (`$78C4`/`$7E83`/`$7E60`) during this investigation disproved
it -- only the first byte matches; the second (which becomes `$E07B`)
is `$19`/`$29`/`$31` respectively. Since `$E07B` is a live countdown
`refreshRoadCurvePhase` decrements every call, this meant stages 4 and 5
were silently using stage 1's own phase-countdown seed (25 instead of
41/49) and stage 1's own `$E071` read cursor -- wrong road-curve timing
on any stage past the first, never caught because this project's own
testing hadn't reached stage 4 or 5 before now. Fixed by generalizing to
a real per-stage table (`roadFeatureStageRecords`), with all 18 values
(3 fields x 6 stages) independently confirmed by direct-calling `$50BF`
against the real ROM once per stage and reading back every register it
touches, not derived from the ROM table's raw bytes by hand.

**Stage 2 is now fully ported** (`stage2RoadFeatureStep`): beyond the
same `injectRoadFeature` call stage 0/1 makes, it reads a flat,
entry-less 4-byte-at-a-time ROM stream (`stage2And4FeatureStream`, 96
bytes at `$7DC5`, terminated by a real `$FF` at `$7E25` -- confirmed by
scanning the actual ROM bytes for it) into the road-strip staging row,
then rejoins the shared tail. Verified against the real ROM: a
direct-call trace of `$50BF` for all 6 stages matches this port's own
`armRoadFeatureStream` output exactly, field for field (including the
now-fixed stage 4/5 values and the stage-3-specific `$E06F` road-curve
override), and the standard 30000-frame soak test still passes.

**Stage 3 is now ported too** (`stage3RoadFeatureStep`). It shares
`renderRoadPatternBits` (ported from `$56CC`/`$56CE`) with the
still-unported stage 6: reads a 16-bit "pattern word" via a cursor at
`$E090` and, for each of the word's top N bits (12 then 10, covering the
full 22-byte row), shifts it left and uses the carry-out to pick between
two fixed tile IDs (`$9B`/`$04` for stage 3) per bit. It then overwrites
the row's own middle 15 bytes from one of two 48-byte ROM streams
(`stage3StreamA`/`B`, extracted from `$7C23`/`$7C54`, both confirmed by
scanning for their own real `$FF` terminators) via `$E06F` -- yes, the
same field `injectRoadCurve` normally uses for its default `$5998`
table, safely overridden for stage 3's entire duration since
`injectRoadCurve` is never reached from stage 3's own dispatch branch.

Getting the control flow right took a real correction: an initial
translation read the terminator peek at `$E06F` and the reload countdown
at `$E08A` as two independent checks that both ran every call. A 30-call
direct-call trace against the real ROM disproved that immediately --
`$E08A`/`$E07C` stayed constant for exactly 6 calls (one full 48-byte
stream, 8 bytes/call) before changing on the 7th, not every call. The
real structure: the terminator peek *gates* whether the reload logic
runs at all -- when the peeked byte isn't `$FF`, execution jumps
straight past the rewind and the countdown to the 8-byte consume tail;
only a terminator hit falls through into both. Fixed and reverified: the
corrected version's own 30-call trace now matches the real ROM's
byte-for-byte, including the flip-flop's asymmetric hold (stream A gets
a 1-call hold before immediately reloading again; stream B gets 16) and
a free-running counter that blinks one tile (`$E063`) on 2 of every 4
calls -- a detail that, combined with the alternating bar pattern above
it, makes a railroad-crossing gate a stronger (if still not confirmed
against a rendered screen) guess for what this whole mechanism draws.

**Stage 6 is now ported too** (`stage6RoadFeatureStep`), and turned out
much simpler than stage 3's -- it doesn't touch `$E06F` at all. It's
just `renderRoadPatternBits` (reused unchanged) plus its own
pattern-word stream (`stage6PatternWordStream`, extracted from `$7FE4`,
6 words, terminated by a real `$FF` at `$7FF0`) and the `$E090`
wraparound, then rejoins the shared tail from its very start (`JP
$5534`) rather than partway through like stages 2 and 3. `patternWordAt`
now dispatches between stage 3's and stage 6's own pattern-word streams
by address range, since the two stages share one renderer but each arms
`$E090` with a different stream. Verified against the real ROM the same
way as stage 3 -- a 10-call direct-call trace, this time stopped right
at the `JP $5534` boundary (using the trace tool's own PC-watching
mechanism) so the row snapshot reflects only this prefix's own effect,
not the rest of the shared tail it jumps into -- and it matches this
port's own output byte-for-byte across all 10 calls, including the
6-call wraparound period and each word's own bit pattern.

**Stage 4 and 5's own shared prefix (`$5600`) is now ported too**
(`stage45RoadFeatureStep`). It's a nested-countdown entry selector: `$E08D`
counts down every call; once it reaches 0, a second, independent
countdown at `$E08C` decides whether to also advance a per-stage
selector-byte stream (`$E08A`/`$E08B` -- stage 4 from `$7E7C`, stage 5
from `$7D8A`) before picking one of 4 fixed-format ROM entries
(`roadStrip7ByteEntries`, from the `$569C` table: each a "count" byte
plus count*7 data bytes, packed back-to-back with exactly zero gap --
confirmed precisely, the same standard applied to
`roadFeatureTableStage145`). Either way, 7 bytes get copied from
whichever entry is current into the road-strip staging row.

Unlike every other stream extracted this session, the selector-byte
streams have **no confirmed terminator** -- `$5600`'s own disassembly
never checks bounds on `$E08A`'s advance, just a plain `INC HL`. Only a
64-byte prefix of each was extracted (enough for any realistic test);
stage 4's own stream opens with seven `$FE` bytes in a row, each
selecting the largest entry (204 bytes, a 63-cycle hold), so in practice
stage 4 essentially never advances past its very first selection within
a normal playthrough. Verified against the real ROM via direct-call
traces (30 calls for stage 4, 60 for stage 5 -- chosen because stage 5's
own stream has much shorter holds and actually exercises the advance
path) matching this port's own output byte-for-byte -- including a
same-call `$E08A` advance that neither reading the disassembly by hand
nor the first real-ROM run anticipated, caught only by comparing traces
rather than trusting the hand-derived prediction (both `$E08C` and
`$E08D` turned out to start at 1 via `armRoadFeatureStream`'s own
already-ported `stage45SharedInitTable` copy, triggering an immediate
reload on the very first call).

**Stages 4 and 5 are now fully ported**, completing all six of `$E043`'s
dispatch values and, with them, all of `$5501`/`updateRoadAndScore`.

Stage 4 (`stage4RoadFeatureStep`) reuses the same 7 bytes `$5600` just
returned (the original does this for free -- `$5600`'s own `LDIR`
conveniently leaves `HL` pointing just past them, and `HL` survives a
Z80 `CALL`/`RET` with no explicit return-value passing needed; this port
makes that reuse explicit): adds `$0D` to each and writes them into
`$E068`-`$E06E` in reverse order, needing no new ROM data at all.

Stage 5 (`stage5RoadFeatureStep`) is the one that needed the rest of
what was scoped as follow-up work: a second, independent entry-selector
mechanism, parallel to but different from `$5600`'s own. `$E094` tracks
a cursor into one of 4 ROM entries (`road56A4Entries`, from the `$56A4`
table) -- confirmed to be 3 *distinct* streams (index 0 and index 3
share one address): two new 48-byte streams extracted from `$7D94` and
`$7E26` (each terminated by a real `$FF` exactly 48 bytes in, the same
proof standard as every other stream this session), plus the middle
entry at `$7DC5`, which -- as suspected -- really is the same stream
already extracted as `stage2And4FeatureStream`, reused directly rather
than duplicated. Reload cadence is driven separately by `$E092`/`$E090`
(a per-stage selector-byte stream from `$7E57`, same "no confirmed
terminator, 32-byte prefix" caveat as `$E08A`'s own). Finally, a
register-shadow (`EXX`) peek of `$E090`'s current byte (modeled here
with an ordinary local variable, same observable effect) decides whether
4 more bytes get discarded or copied, reversed, into `$E06B`-`$E06E`.

**A second real bug, found the same way as the first.** Verifying stage
5 against the real ROM turned up a mismatch at its 17th call -- byte
values that didn't match `stage2And4FeatureStream`'s own extracted
content at all. Direct-comparing the full 96 bytes against a fresh ROM
dump found the cause: that array had been transcribed with only 92 of
the real 96 bytes, the last 32 wrong (a copy/paste slip that duplicated
an earlier row instead of the real tail). Stage 2's own consumption (4
bytes every *other* qualifying frame, rate-limited by
`updateRoadAndScore`'s own outer gate) had never run enough calls in
this port's own earlier verification to reach the wrong tail; stage 5's
heavier, ungated consumption (4 bytes every call) did. Fixed by
re-extracting the full, correct 96 bytes directly from the ROM and
reverifying both consumers -- stage 2 across a full 24-call cycle plus
one wraparound, stage 5 across 26 calls including its own reload event
-- both now matching the real ROM byte-for-byte. A concrete reminder
that "verified for N calls" only covers the bytes those N calls actually
reach, not the whole array; a lighter consumer's own passing test
doesn't clear a shared array for a heavier one.

With this, `updateRoadAndScore`'s own road-feature dispatch is complete
for all 6 stages, each independently verified against the real ROM via
direct-call traces that isolate a stage's own prefix from the rest of
the shared tail (stopping right at its own tail-jump boundary) and
compare this port's output to the real ROM byte-for-byte -- the same
rigor applied throughout, which is what caught both bugs above before
they shipped un-noticed.

## `stateGetReady`'s own `$52B8`/`$52D1`: a starting-stage RNG and an auto-recenter/accelerate mechanic

`$52B8` (`randomizeStartingStage`) zeroes `$E042` (the lap counter) and
picks a starting stage in `{1, 2, 3}` for `$E043` -- confirmed by the
exact bit-masking (a 2-bit masked value 0-3, incremented to 1-4, then 4
remapped back to 1), which gives a non-uniform outcome: stage 1 is twice
as likely as 2 or 3. The original sources its entropy from the Z80 `R`
register combined with `$E003` -- this port has no per-instruction Z80
core to read an equivalent `R` from (this is a native port, not a
cycle-accurate emulator, per its own project conventions), so the exact
sequence real hardware would produce can't be reproduced. Substituted
with Go's own `math/rand/v2`, preserving the confirmed *outcome
distribution* (the only gameplay-relevant part) rather than faking a
specific fake entropy source. **This is a deliberate, acknowledged
deviation** -- the one piece of this whole project that can't be
verified byte-for-byte against the real ROM, because the thing it's
substituting for has no equivalent in this port's execution model at
all.

`$52D1` (`autoSteerTowardCenter`), reached whenever `stateGetReady`
resumes driving after a per-lap crash-sequence cycle, synthesizes input
bits into `$E009` (normally populated from real player input) so the car
auto-recenters and keeps accelerating during that hand-off: within a
narrow band of a computed center column it just forces the accelerate
bit, outside it it also forces whichever steering bit points back toward
center. Also confirmed, as a side effect: `stateGetReady`'s own
`$E001==1` branch (previously "what sets this hasn't been traced,
plausibly a player-input path") is reached via `stateCrashSequence`'s
own phase 5, not player input at all. Verified against the real ROM via
9 direct-call cases spanning the center-band boundary, the steering-
direction boundary, and the `$E008` override condition, all matching
this port's output exactly.

## `drawTextRun` had an undiscovered second entry point that blanks instead of drawing

Tracing `stateGetReadyWait`'s own phase 1 (`$4177`-`$4189`, previously a
TODO -- "redraws level-specific 'get ready' text/graphics ... not yet
traced") found that its two possible targets, `$45F0` and `$4607`, are
**the same interpreter** -- `$4607` is just `LD C,$00; JR $45F2`,
jumping into the middle of `$45F0`'s own body with a different value in
`C`. That register turns out to be a mask ANDed into every character
byte right before the VRAM write (`$4600: AND C`): `$45F0` itself loads
`C=$FF` (a no-op mask, draw the real character), while `$4607`'s `C=$00`
forces every byte to blank, erasing whatever a prior real-mask call
drew at the same destinations.

This means `drawTextRun` (already ported and used at 6 other call
sites, all implicitly assuming the `$45F0`/`$FF` behavior) was an
*incomplete* model of the real routine -- it had no way to represent the
blank variant at all. Fixed by adding a `mask` parameter, updated at
every existing call site to pass `$FF` (preserving their current
behavior exactly), plus the new use: `stateGetReadyWait`'s phase 1 now
alternates between drawing "LEVEL A" or "LEVEL B" (whichever was
selected, reusing the exact same name-table positions the attract-mode
menu's own text occupies) and blanking it every 4 frames, producing a
blinking "get ready" message during the countdown. Verified against the
real ROM across all 4 combinations of level selection and the blink
phase, matching exactly.

## `fuelPickupTick`'s full sparkle/score animation, and a dead parameter found in already-shipped `addToScore`

Finished `fuelPickupTick` ($7755-$77C7, previously only its $E0B8==1
refuel branch was ported). $E0B8==1's own tail awards a score bonus via
a 5-byte table ($7791: 0, 3, 5, 8, 16) indexed by a pickup counter
($E0B9, clamped to 4), then arms a $60-frame countdown ($E0BA) for a
sparkle/twinkle sprite animation. $E0B8==2 runs that animation: while
the countdown is nonzero, positions a decorative sprite-attribute-table
entry ($E13A-$E13D) just above the player's own car ($E112/$E113,
confirmed via `armPlayerCarSprites`) and cycles through 4 pattern frames
as the pickup counter grows; once the countdown reaches 0, hides that
sprite and resets $E0B8 back to idle.

Tracing the score-bonus call site raised a real question about
`addToScore`'s own already-shipped, previously-verified signature: it
takes a `hi` parameter (the packed-BCD hundreds digit), but this new
call site had no confirmed value to pass for it. Rather than guess,
re-reading `addToScore`'s own real disassembly settled it: `$447F`'s
very first instruction is `LD C,$00`, unconditionally zeroing the
register `ADC A,C` later reads for that third byte -- before the
function even checks its own $E002 gate. **The real ROM has no way to
add anything but a carry to $E041 from any call site.** The existing
`hi` parameter could never have any observable effect and was pure risk
(a future caller passing a nonzero value the real ROM never would) --
removed, updating the one existing call site along with the new one.
This is a good example of why re-verifying an assumption instead of
copying it forward matters: the first `addToScore` call site already
happened to pass `hi=0`, so the parameter's uselessness was never
actually exercised until a second call site raised the question.

Verified both branches against the real ROM via 6 direct-call cases
(three refuel-bonus tiers, three animation-tick states including the
reset-to-idle transition), matching exactly. Standard 30000-frame soak
test still passes.

## Road objects still weren't visible after the `$70D9` port: `$70A8`'s real dispatch tail runs for every type, not just type 6

Follow-up to the section above: the user confirmed `activateStagedObjectType`
alone wasn't enough -- traffic cars, fuel, and oil still never appeared on
screen. Re-derived the whole `$70A8` dispatch from scratch rather than
trust the earlier scoping notes, and found the actual root cause: **only
type 6 (`positionTrafficSprite`, entered at `$728A`) ever wrote to the
sprite-attribute table, and nothing in the type-transition graph (staged
objects start at type 1; `resolveApproachingObjectDirection` moves them to
5; `handleObjectType3` moves them to 7; `checkSiblingProximityAndResolve`
moves the *sibling* to 2) ever reached type 6.** Every real object was
being correctly staged, spawned, and pushed around in RAM -- just never
drawn.

The mechanism itself was actually already on record: the `$70A8` scoping
notes below (written before any of this was ported) correctly call out
that "All 8 branches return through a shared 'advance to next slot' tail
pushed onto the stack ahead of time (`$728A`) rather than a normal `RET`
back into `$70A8`", and separately that type 6's own entry "is *also* the
shared advance-to-next-slot tail." The actual gap was that `runObjectSlots`
as shipped didn't reflect that documented structure -- it only ever called
`positionTrafficSprite` from `case 6`, leaving every other type's own fall-
through into `$728A` unmodeled. Re-confirmed the mechanism directly by
re-reading `$4045`'s own trampoline body (`POP HL` -- pops *its own*
return address, i.e. the table right after the `CALL $4045` instruction --
then indexes and `JP`s to the target) together with `$70A8`'s own dispatch
preamble:

```
70C2: LD HL,728Ah   ; push $728A as a return address BEFORE the trampoline
70C5: PUSH HL        ; runs -- NOT consumed by $4045 itself (that pops its
70C6: CALL L4045     ;   own return address, the jump table at $70C9, instead)
```

`$728A` is pushed onto the stack as a return address *before* the
type-dispatch jump happens. Every dispatch case's own call chain
(`$70D9`'s type-1/4 chain, `$71A2` type 2, `$71C6` type 3, and the newly-
ported `$7209` type 5/7 -- see below) eventually executes a plain `RET`,
which pops that pushed address and lands at `$728A` -- **not back at
`$70A8`'s own loop.** Type 6's table entry simply points straight at
`$728A` instead of a `RET`-terminated handler, since it has no
type-specific setup to do first. In other words, `$728A-$72AD` isn't "type
6's handler" at all -- it's the shared per-slot continuation *every*
type's dispatch falls into, confirmed empirically: a direct-call trace of
`$70A8` against the real ROM, with a slot forced to a freshly-staged type-1
object (field 0 = 3, so it can transition to type 5), showed sprite-
attribute writes at `trafficSpriteEntryAddr(0)` from the very first call,
type still 1 -- with no type-6 transition anywhere in the run.

Two concrete gaps followed from this, both fixed in `internal/game/gameplay.go`:

- **`positionTrafficSprite` ($724B-$7289) only wrote one 4-byte sprite
  entry.** The real routine writes it TWICE -- once with the looked-up
  pattern, once 4 bytes later with `pattern+4` (`$727D`'s own `CALL
  L7282` falls straight through into the same code again after
  `ADD A,04h`) -- the second half of the object's 2-sprite pair. Confirmed
  via the same direct-call trace (field0=3 produces pattern bytes `$84`
  then `$88`, matching `trafficSpritePatternByType[3]=0x84` and `+4`).
- **`runObjectSlots` only called `positionTrafficSprite` for `case 6`.**
  Restructured so it (plus a new `finishObjectSlot`, porting `$728D-$72A3`)
  runs after *every* non-empty slot's dispatch, matching the real
  trampoline's own fall-through. `finishObjectSlot` re-reads the object's
  persistent row field and, once it reaches the retirement band
  `[$C3,$CF)`, forces the row to `$DF` (an off-screen sentinel, not the
  `$D0` list terminator), clears the type to 0, and redraws once more --
  matching `$72A4-$72AD` exactly.

Also ported the previously-stubbed `$7209` (types 5/7's actual handler,
`handleObjectType57`): it wasn't part of the original bug (objects DID
draw without it, confirmed by the trace above), but leaving `case 5, 7` a
total no-op meant an object froze in place forever the first time it
transitioned to type 5 or 7, since `activateStagedObjectType` (the only
thing that moves a type-1/4 object) stops running once the type changes.
`$7209` counts down slot field `$0F`, demoting back to type 1 once it
expires (matching the observed real-ROM toggle between types 1 and 5 on
alternating frames), otherwise nudges the slot's 16-bit position (fields
7-8) by a fixed, type-dependent magnitude ($B8 for type 5, $A0 for type 7,
sign taken from the direction flag at field `$0E`), then falls through
into the already-ported `checkSiblingProximityAndResolve`.

**Verification**: a direct-call ROM trace (slot 0 forced to a freshly-
staged object, field0=3, driven through 60 calls to `$70A8`) was
replicated frame-for-frame in a Go test calling `runObjectSlots` the same
60 times from the same initial RAM state -- every single frame's
field0/type/row/col/sprite-bytes matched the real ROM exactly, including
the type 1↔5 toggle pattern and the row/col trajectory. A second, full-game
soak test (`ColdBoot` + simulated keyboard input through the real menu and
into gameplay, ~5000 frames of driving) confirmed actual traffic sprites
end up drawn on screen (non-off-screen Y values at `trafficSpriteEntryAddr`)
during normal play, not just in the isolated slot test. A headless
re-render of `cmd/play`'s own compositor (Ebitengine can't initialize in
this sandboxed shell) produced a screenshot showing the player's car
alongside two other cars on the road, confirming the fix visually end to
end. Both temporary test files were deleted before committing, per this
project's usual convention.

Deliberately still not ported: `$7234-$7248`, an extra sprite-pair write
to a fixed address ($E132) gated on the object's own type being 6 or 7 via
a Z80 shadow-register (AF') value whose origin isn't confirmed -- its
absence only affects that one extra, still-unidentified sprite pair, not
the main draw/retire mechanism this fix covers. `checkSiblingProximityAndResolve`'s
own `$733D-$7351` tail (setting *this* object, not just the sibling, to
type 2) also remains an honest gap, as before.

## Player car sometimes flickered invisible mid-crash: `$4636`'s missing sprite-mirror generator

User report: "crashing into the back of other cars" sometimes made the
player's car go invisible, other times it correctly showed spinning out
of control -- with an explicit hunch that this was "missing sprite
patterns, showing an invisible pattern for those crashes." The hunch was
exactly right.

The visible post-crash "spin" animation is `updateSteeringAndCarState`'s
case 7 (reached after case 4's initial bump-and-countdown): every 4th
frame it calls `setCarSpritePattern(c.RAM[0x0B1] & 0x07)`, cycling the
player car's sprite-pattern selector through all 8 values `0-7` via
`carSpritePatternTable`. That table's 8 relevant entries are `{0x00,
0x10, 0x18, 0x20, 0x08, 0x38, 0x30, 0x28}` -- pattern indices 0, 16, 24,
32, 8, 56, 48, 40 respectively, each needing 8 consecutive pattern slots
(the selected pattern for one 16x16 sprite plus `+4` for the paired
second sprite, i.e. `patIndex` through `patIndex+7`).

`assets.SpritesVehiclesPattern` (loaded directly to VRAM `$1800` in
`initHUDAndTrackGraphics`) is only **320 bytes = 40 patterns** (indices
0-39). That covers selectors 0, 1, 2, 3, 4 (needing up to pattern 39) --
but selectors 5, 6, 7 need patterns 56-63, 48-55, and 40-47
respectively, all *beyond* the loaded asset. The real ROM fills that gap
at runtime via `$6934-$693E: CALL $4636`, a sprite-pattern mirror
generator that was still an untraced TODO in this port -- so that VRAM
region was simply whatever the emulated VDP's zero-initialized memory
held: a fully blank 16x16 sprite. Exactly 3 of the 8 cycled selectors
(37.5%) landed there, producing the intermittent "sometimes invisible"
symptom -- selectors 0-4 (5/8 of the cycle) always rendered a real spin
frame, matching "other times it shows spinning."

Traced and ported `$4636` (`mirrorSprite16` in `graphics.go`). Rather
than reason by hand through its raw `PUSH DE`/`BIT 4,E` stack trick (the
same class of intricate register/stack shuffling this project already
treats as a hard stop -- see `positionTrafficSprite`'s own still-unported
`$7234` block above for the same caution applied), verified it via direct-
call ROM tracing: filled the real call site's own source region ($1880,
192 bytes) with distinct per-byte markers, called `$4636` with its own
real register values (`HL=$1880, DE=$1950, C=6`, read directly off the
call site at `$6934-$693C`), and checked every output byte. Result,
exact and unambiguous: `dst[i] = mirrorByte(src[i^16])` for every byte in
each 32-byte block -- i.e. the left half (bytes 0-15, the TL+BL 8x8
quadrants) and right half (bytes 16-31, TR+BR) swap position *and* every
byte's bits reverse (`mirrorByte`, already used by `mirrorPatternRows`),
producing a correct whole-sprite horizontal flip. With `C=6`, this
exactly regenerates selectors 5, 6, 7 (patterns 40-63) as horizontal
mirrors of selectors 1, 2, 3 (patterns 16-39) -- confirmed pattern-index
arithmetic, not just plausible byte counts.

**Verification**: after the fix, all three previously-blank pattern
groups (selectors 5/6/7) are confirmed nonzero in VRAM following a normal
`ColdBoot` + attract-sequence + level-select run (the real init path, not
a forced RAM write). A headless render of all 8 spin-cycle sprite frames
(the same VDP/sprite-compositor primitives `cmd/play` itself uses,
`internal/msxgfx.Sprite16Visible`) shows a solid, recognizable car shape
for every one of the 8 selectors -- none blank, none corrupted. Standard
`go build`/`go vet`/full test-suite pass. Both temporary test files were
deleted before committing.

### Follow-up correction: the call site used the wrong destination address (`$1950` instead of `$1940`), garbling the mirrored sprites

User follow-up: no longer invisible, and crashing off the *left* side
correctly showed the car spinning -- but crashing off the *right* side
showed "a bit messed up" graphics, not a clean car shape.

The direct-call trace above was correct about the *transformation*
(`dst[i] = mirrorByte(src[i^16])`), but the call site wiring it into
`initHUDAndTrackGraphics` used the wrong literal for `dst`: `$1950`, the
real ROM's own raw `DE` register value at its `$6934` call site, copied
over directly from the disassembly. That value is *not* the destination
block's base address in the real routine -- the original Z80 code's own
`PUSH DE` / `SUB $20` / `BIT 4,E` sequence exists specifically to turn
`DE=$1950` (block base + 16) into the true base `$1940` before writing,
an addressing trick this port's own clean, direct byte-manipulation
version has no need to replicate, but which the call site accidentally
did replicate by using the same raw literal instead of the resolved
base address. The bug: every one of the 6 mirrored blocks got written 16
bytes (2 patterns) higher than intended, each overlapping and corrupting
part of the *next* block's data -- non-blank (so no longer "invisible"),
but structurally wrong, matching "messed up, not a clean car shape."
Selectors 0-4 (the directly-loaded, un-mirrored patterns) were never
touched by this, which is why crashing left -- using those -- looked
correct throughout.

Since the mirrored selectors (5, 6, 7) are specifically the *right-hand*
steering/lean poses (horizontal mirrors of the left-leaning 1, 2, 3), and
`updateSteeringAndCarState`'s case 7 alternates which half of its 8-value
cycle it visits based on the crash direction flag (`$E054`), a crash to
the right spends more of its cycle on the corrupted selectors while a
crash to the left spends more of it on the untouched ones -- consistent
with the reported one-sided symptom, though the exact selector-to-crash-
direction mapping wasn't independently re-derived here (not needed to
explain or fix the bug, which was a plain destination-address error, not
a direction-logic one).

Fixed by changing the call site to `c.mirrorSprite16(0x1880, 0x1940, 6)`
-- the true base, not the raw call-site `DE`. Re-verified rigorously this
time: loaded the *real* `assets.SpritesVehiclesPattern` bytes (not
synthetic markers) at `$1800` exactly as production init does, ran both
the real ROM's `$4636` and this port's `mirrorSprite16` on the identical
source data, and diffed all 192 destination bytes (`$1940`-`$19FF`)
directly -- **exact match, byte for byte**, not just "looks like a car."
This is a stronger verification than the original pass, which checked
the transformation formula against synthetic markers but never checked
the call site's own literal argument against a full real-asset run.

## `$5434` ported: a scrolling hazard marker that can force an off-track crash, not passive scenery

`GameplayTick`'s last remaining unported callee (`$522D`) had been
guessed, from a light read, as something like "reads `$E105`, a
track/scenery pointer" -- a reasonable-sounding but wrong first
impression. Full trace: it's the per-frame update for the single
"approaching object" record `writeApproachingObjectRecord` (`$53DD`,
already ported) arms at `$E105` -- and it's a genuine collision hazard,
not decoration.

The record holds two independent 16-bit fixed-point positions: row
(`$E106` low / `$E107` high) and column (`$E108` low / `$E109` high),
each advanced every frame by its own fixed per-frame delta
(`$E10A`-`$E10D`, set once by `writeApproachingObjectRecord`) via the
same `addToSlot16` low/high accumulator helper the `$70A8` object system
uses. The updated integer row/column gets drawn as sprite-attribute-table
entry 12 (staging address `$E13E` -- confirmed as the last of the 13
entries `blitSpriteAttributeTable`'s own `count=$34` call copies to VRAM
every frame, i.e. `$E10E` through `$E141`). Once the row reaches `$C0`
(off the bottom of the play area), the marker retires (deactivates,
hides its sprite at the `$E0` off-screen sentinel).

The part that makes this a real mechanic rather than a moving decal: on
every frame the marker is still active and hasn't retired, the player's
own position is checked against it in row and (only if the row check
also hits) column. Direct-call ROM tracing -- not reasoned from the raw
chained-subtraction bytes alone, since the two checks use opposite
carry-flag polarities (`JR NC` for the row band, `JR C` for the column
band) and it would have been easy to get one backwards -- pinned down
both bands precisely: row within 24 of the marker (`playerRow - $E107 -
8 >= $E8`), then column within 16 (`playerCol - $E109 - 8 >= $F0`). If
both hit, the marker retires *and* `triggerOffTrackRecovery` fires --
the same off-track/crash tail case 7 of `updateSteeringAndCarState` and
`checkPlayerObjectProximity` already share. Verified with a direct-call
sweep across the exact boundary values on both axes (e.g. column deltas
`$EF`/`$F0`/`$FF`/`$00` bracketing the threshold precisely) plus the
retire threshold and the every-8th-frame proximity-warning sound gate
(`$E002` bit 6 set, `$E003&7==0`) -- around 30 cases total, all matching
the Go port exactly. A 30000-frame full-game soak (`ColdBoot` through
gameplay) confirmed no regressions.

## `$6A06` ported: the last of `initHUDAndTrackGraphics`' TODOs, a per-stage gauge-bar-3 color override

Short one: `$6A06` (`initHUDAndTrackGraphics`'s own `$6945` call, called
unconditionally right after the crash/debris sprite load) reads the
current stage (`$E043`), indexes a 7-byte table at `$6A69`
(`{0x00, 0x20, 0xB0, 0x00, 0x60, 0x90, 0xF0}`, entry 0 unused padding
since stages are 1-6), and ORs that byte's high nibble onto the low
nibble of 128 consecutive color-table bytes at `colBase+$748` -- across
all 3 Graphic 2 thirds, via the same `fillVRAMTriplicated` helper
`fixupGaugeBarBackgroundNibble` already uses for gauge bar 2's own color
fixup.

The address only makes sense once cross-checked against this ROM's own
confirmed VRAM map (`colBase=$0`, not the MSX BASIC default): `$748` is
exactly where `HudGaugeBars3Color` was placed a few lines earlier in the
same function, and the 128-byte span runs 40 bytes into the following
`HudGaugeBars3MirroredColor` region too (placed at `colBase+$798`) --
ported as one continuous sweep matching the real ROM's own address
range, not split along this port's own asset-extraction boundaries.

Verified via direct-call ROM tracing: loaded the real
`HudGaugeBars3Color`/`HudGaugeBars3MirroredColor` asset bytes at their
real production offsets, ran the real ROM's `$6A06` for each of the 6
stage values, and diffed all 384 output bytes (128 bytes × 3 thirds) per
stage against the Go port on identical input -- exact match every time.
`GameplayTick` and `initHUDAndTrackGraphics` are now fully ported; the
only remaining gaps are the two narrowly-scoped `$70A8` object-system
ones documented above.

## `checkSiblingProximityAndResolve`'s own tail ported; `positionTrafficSprite`'s remaining gap narrowed but not closed

The two previously-flagged `$70A8` gaps, tackled together:

**`$733D-$7351` (done).** This tail only runs when
`resolveObjectProximity`'s own real Z80 return state (A register plus
flags) indicates something specific -- exactly the kind of implicit
register-threading this port had, until now, only exposed via a
void-returning translation. Changed `resolveObjectProximity`'s own
signature to `(outcome byte, apply bool)`, matching what the real `$7352`
routine actually leaves behind: `apply=false` only when the sibling is
inactive (type 0, a real `RET Z`); `apply=true` with `outcome` equal to
either the sibling's own `field0-6` (when field 0 is 6 or 7, an abort
path that still reports a meaningful value) or `direction+1` (the normal
success path, after the sibling's own transition to type 2). Verified via
direct-call ROM tracing across all 3 outcomes -- inactive sibling (fully
skipped), field-0-is-6-or-7 (this object still flips to type 2, but with
a distinctly different direction value, `0xFE` for field0=6 vs `0x01` for
field0=7 -- confirming the two abort paths inside `resolveObjectProximity`
really do carry different flag states out through the real `RET`, not
just a hunch), and the normal success path (both objects transition, with
complementary directions) -- all exactly matching a straightforward
`(outcome-1)^1` for this object's own direction and a fixed 16-frame
countdown (vs. the sibling's own 12). `checkPlayerObjectProximity`'s own
existing call site needed no change -- Go allows discarding a multi-value
return as a bare statement.

**`$7234-$7248` (gate resolved, pattern index still open).** Re-read
`$724B`'s own `EX AF,AF'` pair ($724E and $7251, bracketing its "LD
A,(HL)" field-0 read) together instead of treating the trailing `EX
AF,AF'` at `$7229`'s own entry as an isolated, externally-mysterious
state read -- the *net effect* of that bracketing pair is a pure
save/restore, leaving the object's own field 0 sitting in shadow AF by
the time `$724B` returns. So `$7229`'s own gate (`CP 6`/`CP 7` right
after the swap) is exactly `field0==6||7`, not some caller-supplied
signal -- resolved, not guessed, by tracing the instruction pair as a
unit rather than each half separately (the same kind of "read the whole
mechanism, not just the one instruction that looked confusing" mistake
this project has corrected itself out of before).

The remaining piece -- the actual pattern byte the block writes
(`$7239: ADD A,4`, using whatever the *real* A register held at this
shared continuation's own entry) -- resisted the same treatment. Traced
every way this code can be reached: a direct dispatch-table jump for
type 6 (landing here with A already doubled by the `$4045` trampoline's
own leading `ADD A,A`), or a RET-fallthrough for every other type
(landing here with whatever that type's own call chain last left in A,
which differs per path). Attempted a direct-call trace forcing an object
into type 6 with field 0 = 6/7 directly, but the crafted RAM state was
missing enough real preconditions that the CPU ran off into uninitialized
memory as opcodes (a real risk of forcing state that skips real init,
not a tool bug) -- and driving an object through the *actual*,
currently-traced type-transition graph to reach type 6 or 7 with field 0
also 6 or 7 naturally wasn't found reachable (type 3, the only confirmed
path to type 7, doesn't appear to be reachable from any currently-known
transition into it either -- a second, adjacent open question, not
chased further here since it's outside this specific gap's own scope).
Left as an honest, better-documented gap: the write's *shape* (row+16,
the object's own column, fixed colors `$01`/`$08`, two 4-byte entries) is
confirmed via the same instruction-level reading that resolved the gate;
only the specific pattern byte is not, and guessing at it now would carry
the same risk this project already treats as a hard stop.

## While chasing `$7234`'s pattern byte: found a real, previously-missed 20-byte gap in already-shipped `checkPlayerObjectProximity`

Went looking for how an object's type ever becomes 3 or 6 (to find a real
test case for the `$7234` gap above) by re-scanning the whole listing for
literal type-field writes. One hit, `$6EB5: LD (HL),03h`, landed inside
`checkPlayerObjectProximity` ($6E4A) -- code already shipped and tested.
Re-reading that function's own doc comment against the raw bytes showed
it jumps straight from `$6E9C-$6EA4` to `$6EBB-$6EC1`, silently skipping
the 20 bytes in between (`$6EA7-$6EBA`) -- not a flagged gap, just missed
during the original porting pass.

Decoded: once the off-track check just above it has passed, this block
unconditionally reverts the object's own type to 1, sets field 2 to 5,
and sets field 12 (offset `$0C`) to 3 or `$FD` depending on the same
direction flag the function already computes a few lines earlier (`$6EB3`'s
own "LD A,C" reads back the exact value the already-ported `$6E84`
"LD C,$00" / "INC C" pair set -- not a separate, unaccounted-for
register). The type=1 write turns out to be harmlessly overwritten again
moments later by the already-ported `resolveObjectProximity` call right
after it (which sets type=2) -- confirmed via direct-call ROM tracing
that this two-step type=1-then-2 sequence is the real ROM's own genuine
behavior, not evidence of a bug in either write. Field 12's own role
isn't independently confirmed; for the `idx==2` pseudo-slot it aliases
into the sprite-attribute staging area (`$E105+$0C=$E111`), consistent
with quirks already documented elsewhere about that address range's
dual use.

This does *not* explain how an object's type ever becomes 3 (it sets
type back to 1, not 3) -- that adjacent curiosity is noted at the top of
this file's "Next steps" and not chased further. But it's a real,
independent gap in previously-verified code, ported and verified on its
own terms: a direct-call trace with the real off-track/proximity gates
satisfied, for both direction values, matched the Go port exactly
(`base+1=$02 base+2=$05 base+$0C=$FD` for direction 0, `$03` for
direction 1 -- the final value after `resolveObjectProximity` also runs,
not the intermediate one). 30000-frame full-game soak confirmed no
regressions.

## `positionTrafficSprite`'s last gap closed: the `$7234` pattern byte is just `pattern+4`

Went back to `$7234` with a working real-ROM tracing setup and a key
realization from the type-reachability search above: every type-setting
write anywhere in the object system is 0, 1, 2, 5, or 7 -- type 6 and
type 3 are never actually reached. That seemed at first like it would
make `$7234` (gated on field 0, not type, per the earlier finding) hard
to test realistically. It's actually the opposite: since field 0 is
independent of dispatch type and fixed for an object's whole life (set
once at spawn from real level data, which does include the value 6), and
the shared `$728A` continuation runs every frame regardless of type
(confirmed in an earlier fix), a perfectly ordinary field0=6 object going
through its perfectly ordinary type-1 dispatch chain reaches `$7234`
constantly -- no need for the unreachable type-6/7 states at all.

Drove exactly that (field0=6, type=1, real off-track/position gates
satisfied) through 20 real frames with a full single-step trace watching
the actual Z80 `A` register at `$7229`'s own entry. Result: **always
exactly `$54`**, frame after frame, regardless of the object's own
drifting row/column -- i.e. not a per-frame or caller-varying value at
all. Single-stepping through the preceding instructions pinpointed
exactly where it comes from: `$724B`'s own body (the "always draw" core
already ported) ends its second sprite-pair write with `pattern+4` still
sitting in `A`, and simply `RET`s without touching it again -- so
whatever `$724B` last wrote for the *main* sprite's second half is
exactly what `$7229`'s own tail reuses as the base for the *secondary*
pair's own two patterns (`+4` and `+8` further). For field0=6,
`pattern=trafficSpritePatternByType[6]=$50`, giving `$54`, `$58`, `$5C`
-- all confirmed against the trace to the byte. Re-ran with field0=7
(same `pattern=$50`, same table) and got identical results, confirming
it's not field0-specific.

One more edge worth checking: `$724B`'s own `row==$D0` early return
skips the second-write entirely, leaving `A=row=$D0` instead of
`pattern+4`. Traced that path too -- it does leave `A=$D0` as expected,
but since this block's own `row+$10` then lands at `$E0`, off the bottom
of the visible screen, the resulting sprite pair is never actually seen
regardless of its pattern. Ported both paths faithfully anyway (no
special-casing away the edge case), verified byte-for-byte against the
Go port for both the normal and `field0` gate-off cases, plus a
30000-frame full-game soak. `positionTrafficSprite` and the `$70A8`
object-slot system are now completely ported, closing out both of its
previously-open gaps.

## Resolved: leftover HUD/score behind the Konami logo after game over is confirmed genuine ROM behavior, not a porting gap

User report: after a game over, the Konami logo scrolls onto the screen
as expected, but the previous game's HUD (score, hi-score, speed/fuel
gauges) is still visible on the right side of the screen underneath it --
expected a full clear first.

Traced the whole path byte-for-byte: `statePlaying`'s game-over check ->
`stateCrashTrigger`'s own fuel-empty branch (confirmed: only a
`TriggerSound` + `exitAttractSequence`, no screen or sprite clear) ->
`stateResultsPhase1` (game state 7) -> its own default-phase gate,
`countdownGetReadyMessage` ($4429), which drives the already-ported
"line-by-line screen wipe" -> "GAME OVER" text -> a ~216-frame hold ->
`transitionTo(0)`, landing on `stateAttractSequence`'s phase 0
(`ShowKonamiLogo`). None of these steps do anything beyond what's already
ported; the doc comments and the underlying disassembly agree at every
step, so this isn't a missed call.

The actual mechanism: `countdownGetReadyMessage`'s own row-wipe width is
conditional -- 31 columns normally, but only **23** columns
(`$443B-$444D`: `if gameStateByte()!=3 && E002 bit6 set { count = $17 }`)
whenever the "game in progress" flag (`$E002` bit 6) is still set. That
flag is *only* cleared by `stateResultsPhase1`'s own phase 1 (right
before the final `transitionTo(0)`) -- i.e. *after* the phase-0 wipe
already ran with the narrow width. Columns 23-31 are exactly where the
HUD/score panel lives on screen, so the wipe deliberately -- or at least
verifiably -- leaves that whole panel untouched. Confirmed empirically
too: drove a real game session to a real, unforced game over (releasing
all input first, to rule out a test-harness artifact where held keys
looked like a fresh "skip the intro" press to `stateExtra43C3`) and
captured a frame mid-logo-scroll -- the Konami wordmark is fully in
place while "HI SCORE", "SCORE 1514", and both gauge bars are still
visible at the right edge, exactly matching the report.

This is reproduced byte-for-byte from the disassembly, not a gap in this
port -- as best as can be determined without a real cartridge to compare
against, the original 1985 ROM has this same visual quirk. The leftover
HUD does eventually disappear once the Konami logo's own scroll animation
passes through the affected rows, or once a new race starts and
`initHUDAndTrackGraphics` redraws the whole panel fresh. Left unchanged:
this project's own standard is faithful reproduction of confirmed
original behavior, not smoothing away a real quirk into something the
original hardware wouldn't actually show -- the same call already made
for other "surprising but confirmed real" behaviors elsewhere in this
doc. If a deliberately-cleaner departure from the original is ever
wanted here, it should be a separate, explicitly documented deviation,
not a silent "fix."

## Confirmed: the attract loop does show real gameplay, via a simple auto-pilot, not a scripted demo

User question: does the attract-mode loop ever show actual gameplay? The
`$5028` correction elsewhere in this doc already established there's no
large scripted "demo driver" hiding near the attract sequence -- but that
left open whether *anything* auto-plays. Traced it and confirmed: yes,
genuinely.

`stateAttractWait` (state 1, the blinking "PLAY SELECT" cursor)'s own
timeout, if nobody presses fire in time (~255 frames), doesn't loop back
to the logo directly -- `exitAttractSequence` advances it to state 2
(`stateGetReady`), *not* through `stateGetReadyWait`'s "3, 2, 1, GO"
countdown a real player's level selection goes through. `stateGetReady`'s
own phase-0 branch (`randomizeStartingStage` + `initPlayerCarSpritesAndFuel`
+ a real `stateCrashSequence`-reused init cycle) starts an actual race
exactly like a normal one, landing back in `stateGetReady` with
`$E001==1` -- the one branch that calls `autoSteerTowardCenter` every
frame before `GameplayTick`. That function unconditionally sets the
"accelerate" bit and applies simple proportional steering back toward
lane center (`$52D1-$5308`, already ported) -- not a recorded input
script, just a basic always-on auto-pilot layered onto the *exact same*
`GameplayTick`/`updateSteeringAndCarState`/`checkOffTrackCollision` code
a real player's inputs drive.

Verified end-to-end with a real, undirected run (`ColdBoot`, then never
touching a key again): the auto-piloted car genuinely accelerates 0 ->
`$D7` over several hundred frames, steers to stay roughly centered,
racks up real distance (`$E078/$E079` climbing continuously, confirming
actual track progress, not a frozen scene), drains fuel normally
(including picking up a real refuel along the way), and after roughly
1000 frames -- since simple center-steering can't handle a real curve or
obstacle -- runs off-track, triggering a genuine crash/recovery
(`$E049` transitioning to 1). `stateGetReady`'s own final check (crashed
but not "game over" -> idle; otherwise clear the flag and `transitionTo(0)`)
is what sends it back to the Konami logo, completing the loop. A
mid-drive frame dump shows an entirely ordinary-looking gameplay screen
(road, car, live HUD/score/gauges) -- this is real, playable driving
happening on its own, not a frozen or glitched state. Randomized starting
stage means each loop shows a different stage too.

## Stage instantly re-finished right after starting: `initPlayerCarSpritesAndFuel` skipped a leading zero-fill that isn't actually redundant

User report: started stage 3 via `cmd/play`'s level-select cheat, finished
it normally (watched the stage-map transition screen), then stage 4
started fine for one frame before instantly finishing too -- and every
stage after that did the same, an infinite instant-finish loop.

`checkDistanceMilestone` (`$5251`) triggers the finish sequence once the
16-bit distance counter `$E078/$E079` reaches `$0F12`. That counter is
part of the much larger 847-byte zero-fill `$504C`'s own leading
instructions perform (`$504C-$5057`, `LD HL,E048h; LD DE,E049h; LD
BC,034Eh; LD(HL),0; LDIR`) every single time `initPlayerCarSpritesAndFuel`
runs -- but this port's version of that function skipped that fill (and a
second one, `CALL $51E5`, zeroing `$E0C1-$E105`) entirely, reasoning every
byte either range touches was already zero (from `clearRaceRAMBlock`,
run earlier in the same state transition) or got unconditionally
overwritten later in the same function. That reasoning was only checked
against `initPlayerCarSpritesAndFuel`'s fresh-race-start call site
(`stateGetReadyWait` phase 3, always preceded by `clearRaceRAMBlock`) --
it doesn't hold for its *other* confirmed call site,
`stateResultsPhase2`'s own phase 4, reached on every stage-to-stage
transition within the same race, which calls this function directly with
no `clearRaceRAMBlock` in between. `$E078/$E079` has no other reset
anywhere in this port, so it carried over unchanged from the exact
value it held the instant the *previous* stage crossed the finish line --
already past `$0F12` -- making the very next stage's first
`checkDistanceMilestone` call re-trigger the finish sequence immediately,
every time.

Reproduced headlessly before fixing (`internal/game/gameplay.go`'s
`initPlayerCarSpritesAndFuel` called twice with `$043` bumped in between
and no `clearRaceRAMBlock`, exactly matching the real transition): the
distance counter did survive unchanged and did immediately re-trigger
`checkDistanceMilestone`. Fixed by porting both zero-fills for real
instead of trying to prove them redundant -- the second one is now its
own shared function, `clearObjectSlotsAndStaging`, since it's confirmed
called from two independent sites (`initPlayerCarSpritesAndFuel` and
`recoverFromNonFatalCrash`) the same way `resetCarPositionTables` already
was. This removes the whole class of "which bytes actually survive to the
*next* call" bug, not just this one instance -- confirmed correct by
re-running the same repro after the fix: `$E078/$E079` reads back `0` and
`checkDistanceMilestone` no longer fires on the new stage's first frame.

### Follow-up: a second, related bug the above fix exposed (`$E00D` never cleared) -- fixed, but not fully explained by the disassembly alone

Second user report, same investigation, one commit later: with the fix
above in place, a *normal* (non-cheat) game now finished stage 1 and
showed the map screen correctly, but stage 2 started with engine sound
and the car never moving, finishing again almost immediately.

Traced with the same headless approach, driving the real state machine
end to end (`ColdBoot`, "PLAY SELECT", hold fire) rather than poking RAM:
`statePlaying`'s own gate (`$430B`: `if gameOverFlag != 0 { transitionTo(8)
}`) was firing on the very first real `GameplayTick` call of the new
stage. `stateResultsPhase2`'s phase 4 sets `gameOverFlag` (`$E00D`) to the
just-incremented lap counter (2-6, or 1 on the wrap) as part of the
already-confirmed, already-ported per-lap transition (`$43AF`) -- and
nothing in the traced "resume driving" path ever clears it again for an
ordinary, in-progress race: `stateCrashSequence`'s own phase 5 takes the
"`$E002` bit 6 set" branch (confirmed the one taken here, since bit 6 --
"game in progress" -- is set for the whole race and only clears at a true
game over, `stateResultsPhase1`'s own `$435A`), which goes *directly* back
to `statePlaying` and never runs `stateGetReady`'s own gameOverFlag-clearing
branch (`$414B`, only reached via the *other*, bit-6-clear branch, i.e.
never during a real 1-player game already in progress).

Why this fires *immediately* rather than eventually: `scrollTrafficSpritesAndWrap`
increments `$E001` (making the next frame's `GameplayTick` call happen)
once the *last* of the 11 decorative traffic-sprite pairs has a Y
position in `[$C0,$E0)` -- and `countdownGetReadyMessage` (`$4460-$447E`,
run every frame of `stateCrashSequence`'s own phase-0 wait, itself
confirmed reached on every ordinary stage transition, not just the
6-stage wrap) stamps all of them to the sentinel "hidden" value `$C8`
every single frame -- which is already inside that band. So all 11 are
already "stuck" the instant real driving resumes, `$E001` flips within a
frame or two, and the very next `GameplayTick` reads the still-stale
`gameOverFlag` and bounces straight back to results -- which itself
repeats (`$E043` incrementing again each time) roughly every 900 frames
in a full headless run, cascading stage 2 -> 3 -> 4 -> 5 -> 6 entirely
without ever actually driving, confirmed by letting the reproduction run
that long.

Searched exhaustively for a missed clear before concluding one was
missing: grepped the disassembled listing for every `E00Dh` reference (5
total: 2 reads, 3 writes, all already accounted for above) and, distrusting
listing coverage the same way the `$E0AD` investigation earlier in this
doc already taught this project to, directly scanned the raw 16KB ROM
file for the literal operand bytes `0D E0` -- same 5 hits, at the same 5
addresses, nothing new. No indexed/computed write (the kind that
resolved the `$E0AD` case) turned up either, after checking every
function that touches nearby low RAM addresses. **This means the fix
below is a confirmed-by-effect deviation, not a traced one** -- unlike
the fix above, this project could not find where (or whether) real
hardware clears this flag between an ordinary stage transition and the
next stage's real driving; it's possible this really does happen on real
hardware too and is simply not how a human player perceives it, or there
is a mechanism this static-disassembly-only approach (no cycle-accurate
Z80 core, see CLAUDE.md) can't see. Fixed pragmatically: `gameOverFlag` is
now also cleared inside `initPlayerCarSpritesAndFuel` (the one place this
port already centralizes per-stage reset), documented in its own doc
comment as an explicit, flagged departure from the literal ROM rather
than presented as confirmed. Verified fixed by re-running the same
end-to-end reproduction: stage 2 now drives normally (speed and distance
climb realistically over hundreds of frames) instead of bouncing back to
results within two frames.

## The inter-stage map cut scene drew one row 21 times: `$4575`'s row loop advances its source

User report: the mini-map cut scene between levels showed its first row
of map graphics repeated down every row.

That was exactly what this port did. `drawStageMapScreen` painted a
5-byte constant (`stageMapStrip`, `80 81 82 83 80`) into all 21 rows of
the picture at `$382B`, documented as "a literal ROM-to-VRAM row-repeat
... copying fixed bytes each row". The reading came from `$7467`'s call
site alone -- `LD HL,382Bh; LD DE,76ECh; LD BC,1505h` (B=21 rows, C=5
bytes) -- without following the two routines underneath it:

```
4575: PUSH BC / LD B,00h / CALL L45A7   ; copy C bytes to VRAM at HL
457B: POP BC / LD A,20h / CALL L403B    ; HL += 32 -- next name-table row
4581: DJNZ L4575
...
45B0: LD A,(DE) / OUT (C),A / INC DE    ; the copier advances its source
```

`$4575` preserves only `BC` across its row loop, and `$45A7` advances
`DE` as it copies (`$45B5: INC DE`). So each row consumes the *next* 5
bytes: the picture is 21*5 = 105 bytes at `$76EC`, and the old constant
was merely its first row. The block ends at exactly `$7755` --
`fuelPickupTick`'s own address -- the same tight-packing boundary that
confirms this ROM's other data tables. Rendered, it is a proper
course-overview map: fields, a beach, a water crossing, forest, and a
marked finish, running bottom to top.

This is the third bug in this session with the identical shape -- a
shared callee's behaviour assumed from a call site rather than read.
`$45E0` (the "all three thirds" tripler, mistaken for a single write)
and `$58AF`'s `$FF` (a gap marker, mistaken for a terminator) were the
other two. The cheap check each time was the same: read the routine
being called before writing a doc comment that explains why this call
site is special.

## Resolved: the magenta car drove off the road (`$7226`'s `JP L70E5` lands mid-routine)

12. ~~**The magenta car sometimes bumps out and drives outside the
 road**~~ **Root cause found and fixed** -- `$7226`'s `JP L70E5` is a
 jump into the *middle* of `activateStagedObjectType`, not a call to
 the one routine that starts there, so object types 5 and 7 must run
 sibling resolution **and then** `pushAwayFromTrackBoundary`,
 `pushByRelativeSpeed` and the curve tail. The port ran only the
 first. Type 5 is exactly what a maneuvering kind-3 car enters (via
 `resolveApproachingObjectDirection`), and it moves sideways `$B8` per
 frame for up to `$20` frames -- with all three pushes skipped for
 that whole window, nothing pulled it back on-road. Measured over
 12000 frames on stage 5: off-road object-frames fell from 175 (3.4%,
 169 of them kind 3, longest unbroken stretch 86 frames) to 20 (0.3%,
 6 kind 3, longest stretch 2). The remaining 2-frame excursions are
 the boundary push correcting within a frame or two, as designed.
 Note for anyone revisiting: two kind-3 cars both take their direction
 from the player's own column, so they genuinely move together -- that
 part is authentic, and only the missing pushes made the outer one
 leave the road.

 Superseded detail from the first, partial pass (kept because the
 reasoning still holds): the magenta
 car is kind 3, and `resolveApproachingObjectDirection` ($716D) gates
 on exactly `field 0 == 3` -- it is the only kind that actively
 maneuvers relative to the player, which is why it's the one that gets
 noticed. Eight routines in its chain were compared instruction by
 instruction against the ROM (`$7123`, `$70EE`, `$7154`, `$716D`,
 `$71A2`, `$7209`, `$72FE`, `$6DC1` plus `clampRowBand`); all are
 faithful except `pushAwayFromCurveBoundary`, which widened before
 doubling where `$711B: ADD A,A` wraps in 8 bits -- now fixed. Two
 things are worth knowing before chasing this further:
 - The aggressive swerve itself is **genuine ROM behaviour**: object
   type 2 moves a full column per frame (`$71B6: LD DE,0100h`) for up
   to 16 frames, so crossing much of the road quickly is authentic.
 - The fix's measured effect was modest (worst single-frame lateral
   move over a 6000-frame run went from 3 columns to 2), so it likely
   is *not* the whole story. Still unexplained: going fully off the
   road (the off-track check `$6DC1` is faithful, so the cause is
   elsewhere -- the `$E098` boundary table's contents are the next
   lead), driving over other cars, and lingering in front of the
   player. Note only 2 object slots exist, so overlapping traffic may
   involve the decorative sprite pairs of item 2 rather than this
   system at all.

## Resolved: the Konami logo scrolled in over the game-over screen (attract phase 0 starts at `$411A`)

13. ~~**After game over, the Konami logo scrolls in over the previous
 screen**~~ **Fixed.** `stateAttractSequence`'s phase 0 begins at
 `$411A`, not `$4120` -- the first pass here started reading three
 instructions too late and dropped both leading CALLs:

 ```
 411A: CD 98 46   CALL L4698   ; re-program the 8 Screen 2 mode registers
 411D: CD 84 45   CALL L4584   ; hide all sprites + blank the name table
 4120: 3E 1B      LD A,1Bh     ; <- where the port used to start
 ```

 `$4584` is the ROM's general-purpose screen wipe: `CALL $4467` stamps
 `$D0` into sprite-attribute entry 0's Y byte (the TMS9918A sprite-list
 terminator, blanking all 32 sprites in one byte), then `FILVRM` blanks
 all 768 name-table cells. Without it, re-entering attract mode left the
 finished race's HUD, road and "GAME OVER" tiles on screen, with the
 logo scrolling up over only the ~26 cells it writes itself. `$4698` is
 just `InitVDPAndSoundEngine`'s own register-table tail, reachable as a
 CALL target in its own right -- now factored out as
 `writeVDPModeRegisters`.

 Correction this turned up: `$3B00` had been annotated in two places
 (`clearNameTable`, `countdownGetReadyMessage`) as "one byte past the
 768-byte name table, unused padding". It is not -- R5=`$76` puts the
 **sprite attribute table** exactly there, so every `$D0` written to
 `$3B00`/`$3B04` is a deliberate sprite-hiding write, not a stray one.
 `$3B04` hides sprites 1-31 and keeps sprite 0 (the player car);
 `$3B00` hides that one too.

 Same shape as items 12 and the four before it, for the fifth time this
 session: control flow was assumed from where a routine *appeared* to
 start rather than read from the listing.

## Resolved: cheat-starting a later stage -- three wrong approaches before the right one

Stages past the first
start through `game.StartRaceAtStage`, which is `stateGetReady`'s
own race-start branch ($415A-$4162) with the stage chosen rather than
randomized: zero the lap counter, set `$E043`, run
`initPlayerCarSpritesAndFuel`, `transitionTo(4)`. Game state 4
(`stateCrashSequence`) then runs the ROM's ordinary race entry -- wipe,
HUD, 26 scroll steps of that stage's own scenery, "START",
`statePlaying` -- with no countdown. Stage 1 keeps the real fresh-game
start.

The stage must be set inside that function, not by poking `$E043`
afterwards: `initPlayerCarSpritesAndFuel` arms the stage's road-feature
stream from `$E043` ($507E: `CALL $50BF`), so a later write leaves the
roadside armed from the random pick and the stage renders with generic
off-track filler where its own scenery belongs. That failure is visible
as a road strip dominated by `$A0` instead of the stage's own terrain
tiles.

Three earlier attempts drove the fresh-game "get ready" sequence and
patched up the result, and all three failed on the starting signal.
Recorded because the shape recurs: **the gantry is not drawn on top of
the road, it is road-buffer content**, put there by
`initTrackLayoutData`. So pre-rolling the strip by hand scrolled it
away and left the countdown lamps flashing unattached; restoring it
across that scroll looked right in a byte dump but wrong in motion; and
suppressing `drawCountdownDigit` still left a lone green lamp scrolling
down the road, because `triggerRaceStart` ($424A) pokes
`$E1E3`/`$E1E4` = `$48`,`$49` straight into that buffer -- exactly the
gantry's bottom lamp row. Diverting out of the sequence late then drew
the HUD twice with stage 1's static layout in between, and took 513
frames. Not running the countdown at all is what fixed it.

`StartRaceAtStage` also has to blank name-table columns 23-31 (the HUD
panel) before handing over. The arrival wipes the screen through
`countdownGetReadyMessage`, but only 23 columns wide whenever
`gameStateByte() != 3` and `$E002` bit 6 is set -- deliberately, so a
mid-game stage change doesn't wipe the live HUD. Both of the ROM's own
users of this path satisfy that: a genuine stage advance really does
have a HUD there, and the attract demo arrives with bit 6 *clear*,
which selects the full 31-column wipe. The cheat has neither -- it
comes from the attract screen, so those columns hold "ROAD FIGHTER"
title leftovers ($C0-$E0), and it must set bit 6 before the handover,
because state 4's phase 5 reads it to choose "real race" over "loop
back to the demo" and while it is clear `stateExtra43C3` sends any
keypress straight back to the menu. Three title tiles at column 24
survived the narrow wipe into the race until this was added.

Measured on stages 2-6: 284 frames from key press to racing, one HUD
draw, zero starting-signal tile-frames anywhere in the road strip, lap
counter 0 and fuel seeded, and all 216 HUD-panel cells identical to a
normal stage-1 start. Against the old pre-roll path the tile
vocabulary is identical except for two groups -- `$46`-`$49` (the lamps,
only old) and `$21`/`$32`/`$33`/`$34` (only new), which is "START",
`stateCrashSequence` phase 4's own `$394A` write. Stage 1 is unchanged.

Gantry geometry, since nothing else in this doc records it: name-table
rows 0-5, columns 5-8, i.e. road-buffer addresses `$E186 + row*22 +
(col-1)`. Row 0 is the `$42,$42` cap, rows 1-4 are
`$40`/`$46`/`$47`/`$41` (left post, two lamp tiles, right post), row 5
is the `$43,$43` foot; everything immediately outside -- all of row 6,
column 4 -- is the `$A0` off-track baseline. `$E1E3` (row 4, column 6)
independently confirms the mapping. This also corrects
`drawCountdownDigit`'s own doc comment, which said the lamp cells start
out as `$42` placeholders: they start out as `$46,$47`, and `$42` is
only the cap above them.

Worth knowing: a genuine arrival at stage N>1 shows no countdown either
-- it goes state 8 -> 4 -> 5 and never re-enters `stateGetReadyWait`.
The starting signal belongs to a fresh game start, which
`randomizeStartingStage` can legitimately place on stage 1, 2 or 3.

## Confidence key used throughout

- **High** — traced instruction-by-instruction against the listing, and/or
  visually/structurally confirmed (e.g. font rendered as a recognizable
  glyph grid).
- **Medium** — control flow is clear, but the semantic meaning of the
  data/state involved is inferred rather than confirmed.
- **Low** — a plausible guess (e.g. "looks like a tile" in a PNG preview)
  without independent confirmation.
