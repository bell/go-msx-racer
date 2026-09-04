# go-msx-racer: an MSX racing-game engine in Go

Unofficial, non-commercial, and not affiliated with or endorsed by any
rights holder. See NOTICE and the README's Legal section; the short
version is that nothing derived from the cartridge is distributed here.

This repo is an in-progress decompilation of the MSX cartridge ROM
`Road Fighter - Konami (1985) [7349].rom` into readable Go. It is not a Z80
emulator — the goal is a native Go port that preserves the original's
behavior, backed by a small hardware-abstraction interface
(`internal/msx.System`) rather than executing the original Z80 code.
`internal/msx.VDP` is a concrete implementation of that interface (real
VDP registers + 16KB VRAM, no display of its own), and `cmd/play` actually
runs the port against it and renders the result with Ebitengine — it
genuinely plays, start to finish: all 9 top-level game states, all 6
stages, traffic, fuel, scoring, crashes, the inter-stage map screen and
real PSG audio. There are no stub routines left in `internal/game`; what
remains open is listed under "Open issues" at the top of
`docs/ANALYSIS.md`, and is analysis work rather than missing code.

**Nothing derived from the ROM is checked in.** `.gitignore` excludes the
ROM itself (`*.rom`), the extracted assets (`assets/**/*.bin`) and the
generated disassembly (`docs/listing.asm`) — all copyrighted Konami data,
just decompressed or decoded. A fresh clone therefore has none of it, and
two commands rebuild all of it:

```bash
go run ./cmd/extract                       # assets/ -- needed before anything runs
go run ./cmd/disasm -rom "Road Fighter - Konami (1985) [7349].rom" -out docs/listing.asm
```

Place your own copy of the cartridge at the repo root first. Its checksums
are in `docs/ANALYSIS.md`'s "ROM used" section — `cmd/extract` warns on a
mismatch, and an address from that doc or `docs/listing.asm` may not mean
the same thing in a different dump. Once `cmd/extract` has run, `cmd/play`,
`cmd/screenshot`, `cmd/gfxviewer` and `cmd/sndpreview` need only the
`assets/` files (embedded via the `assets` package); `cmd/extract`,
`cmd/disasm`, `cmd/rledecode` and `cmd/gfxpreview` read the ROM directly.

Note `go build ./...` fails on a fresh clone until `cmd/extract` has run,
because the `assets` package `//go:embed`s files that don't exist yet.
`cmd/extract` deliberately does not import that package, so it always
builds.

**Read [`docs/ANALYSIS.md`](docs/ANALYSIS.md) first, every session.** It has
the prioritized next-steps list, the full confidence-rated inventory of
what's confirmed vs. inferred vs. still guessed, and the extracted-asset
table. This file is just orientation to get there faster.

## Workflow

**Commit after each change.** Once a logical change is complete and builds
cleanly (`go build ./...`, `go vet ./...`), commit it before starting the
next one — don't batch unrelated changes into a single commit, and don't
leave a turn's work uncommitted. This applies to code and to doc updates
(`docs/ANALYSIS.md`/`CLAUDE.md` edits get their own commit, or ride along
with the change they document — not left pending). It does **not** apply to
`assets/**/*.bin` or `docs/listing.asm`: those are ROM-derived and
gitignored, and must never be committed — if `git status` ever offers them,
something has gone wrong with `.gitignore`, not with the ignore rule.
Write a normal descriptive commit message (what changed and why); no need
to ask before committing — this instruction is the standing authorization.

**Two verification steps worth running before you commit**, both cheap and
both of which have caught real problems in this project:

```bash
go run ./cmd/extract -check      # every asset still matches the ROM
go run ./cmd/screenshot -out /tmp/shots && \
  for f in screenshots/*.png; do cmp "$f" "/tmp/shots/$(basename "$f")"; done
```

- **`cmd/extract -check`** after touching `cmd/extract`'s manifest, the
  `assets` package, or anything about how an asset is produced. It
  re-derives all of them from the ROM and diffs against the working tree
  without writing, so a wrong address or length fails loudly instead of
  silently shipping plausible, wrong data.
- **The screenshot diff** after changing anything in `internal/game` that
  is not meant to alter behaviour — a refactor, moving data into `assets`,
  a rename. All seven PNGs staying byte-identical is a strong,
  whole-pipeline regression check: name table, tiles, colors, sprites and
  the HUD all have to come out the same. When a change *is* meant to alter
  what is drawn (a real bug fix), regenerate them into `screenshots/` and
  commit the new ones instead, saying so in the message.

## Layout

- `internal/z80dis/` — a from-scratch Z80 disassembler (all documented
  opcodes incl. CB/ED/DD/FD/DDCB/FDCB). Stable; shouldn't need changes
  unless a new instruction-decoding bug turns up.
- `cmd/disasm/` — recursive-descent disassembler driver for this specific
  ROM, with special-case handling for the two computed-jump-table
  trampolines this game uses (`$4045`, `$4047`) and byte-level coverage
  tracking to prevent misaligned decodes from corrupting confirmed code.
  Regenerate the listing after tracing new CALL/JP targets:

  ```bash
  go run ./cmd/disasm -rom "Road Fighter - Konami (1985) [7349].rom" -out docs/listing.asm
  ```

- `cmd/extract/` — rebuilds every file under `assets/` from the ROM. Its
  `manifest` is the authoritative record of where each asset comes from:
  an RLE stream address, a raw ROM byte range, or a load-time mirror
  computed from another asset. **Add an entry here whenever you extract a
  new asset**, or it won't exist on anyone else's clone. **This includes
  data tables, not just graphics** — no ROM bytes belong in committed Go
  source, so a table longer than a few bytes goes in the manifest and is
  reached through the `assets` package (see `assets/data/`), the way the
  road curve/feature streams and the track records are. Every entry was
  confirmed by extracting and diffing against the file the project already
  had — note two mirrors are *named* for a ROM address that is a real RLE
  stream decoding to something else, so the filename is not evidence.
  `-check` re-verifies the working tree against the ROM without writing.
- `cmd/gfxpreview/` — renders a byte range as a tile-grid PNG, for visually
  classifying graphics data. **Only meaningful on already-decoded bytes** —
  most graphics in this ROM are RLE-compressed (see `internal/rle/`), so
  pointing it straight at raw ROM addresses without decoding first produces
  the same "looks plausible but is subtly wrong" result that the font asset
  originally shipped with:

  ```bash
  go run ./cmd/gfxpreview -rom /path/to/decoded.bin \
    -start 478A -len 344 -tile 8 -cols 20 -scale 6 -out /tmp/preview.png
  ```

  (gfxpreview reads a byte range starting at `-start` minus its assumed
  `$4000` base, so to preview a decoded buffer directly, either pass it as
  `-rom` with enough leading zero-padding that the address math lines up, or
  just treat its output as address-less and set `-start` to `4000`.)
- `cmd/rledecode/` — decodes one of the ROM's compressed graphics streams
  (`internal/rle`) starting at a Z80 address, writing the decoded bytes out.
  Always check `docs/listing.asm` for a confirmed `LD DE,<addr>h` feeding
  `CALL $45E0`/`$4611` before trusting an address is actually one of these
  streams — see the "Compression format" section of `docs/ANALYSIS.md`.

  ```bash
  go run ./cmd/rledecode -rom "Road Fighter - Konami (1985) [7349].rom" \
    -start 478A -max 512 -out /tmp/decoded.bin
  ```

- `internal/rle/` — the compressed-stream format itself (literal runs,
  repeat runs, a shared-tail indirection byte). Traced from `$4611-$4634`.
- `cmd/gfxviewer/` — interactive Ebitengine viewer that steps through every
  `*.bin` in `assets/gfx/` one at a time (arrow keys / space / N/P to
  navigate, T to toggle 8x8/16x16 tile interpretation, Esc/Q to quit),
  showing each file's name on screen. Same tile-rendering interpretation as
  `cmd/gfxpreview`, so the same RLE caveat above applies — it renders
  whatever bytes are in the file as-is, so point it at already-decoded
  assets, not raw ROM addresses:

  ```bash
  go run ./cmd/gfxviewer            # defaults to assets/gfx
  go run ./cmd/gfxviewer some/other/dir
  ```

  Requires a real display (opens a native window) — can't be verified via
  screenshot from a headless/sandboxed shell; the tile-rendering logic
  itself is shared with the already visually-verified `gfxpreview`, and the
  text layout was separately sanity-checked headlessly by rasterizing the
  same strings with `golang.org/x/image/font` directly.
- `docs/listing.asm` — the full generated disassembly. Grep this by address
  (e.g. `grep -n "^5224:" docs/listing.asm`) before tracing a routine by
  hand; it's already been decoded, just not read yet.
- `docs/ANALYSIS.md` — reverse-engineering notes: confirmed vs. inferred
  findings, extracted-asset inventory with confidence ratings, and the
  next-steps list.
- `assets/` — everything `cmd/extract` produces from the ROM. **Generated,
  gitignored, and absent on a fresh clone** (only `assets.go` is tracked);
  `cmd/extract`'s manifest is the authoritative list. Three subdirectories:
  `gfx/` — font (`font_8x8.bin`), the boot logo
  (`logo_pattern_konami_490F.bin`), a partially-read 16x16 title font that
  spells at least "ROAD..." (`bigfont_pattern_road_4A25.bin`), sprites
  (`sprites_*.bin`), HUD/scenery/map graphics (`hud_*.bin`, `scenery_*.bin`,
  `map_*.bin`, `ui_*.bin` — 10 confirmed pattern+color pairs, named by
  content), plus the still-unidentified `tiles_candidate_*`/
  `data_candidate_*` guesses; `sfx/` — the note table and sound scripts;
  `data/` — the road curve/feature streams, track records, title-reveal
  script and inter-stage map rows that the engine reads directly (these
  used to be Go byte-array literals in `internal/game`; see the note under
  `cmd/extract/`). The RLE-sourced files are already-decoded bytes, not
  raw ROM data; the `sfx/` and `data/` ones are raw ROM ranges. See
  `docs/ANALYSIS.md` for the full table with confidence ratings.
- `internal/msxgfx/` — renders MSX Screen 2 tiles in real color (pattern +
  per-row color table → RGB via the TMS9918A palette), or mono for
  pattern-only data. Used by `gfxpreview -color` and `gfxviewer`'s `C`
  toggle. See `docs/ANALYSIS.md`'s "VRAM memory map" section for how
  pattern/color table addresses are derived for this specific ROM (it does
  **not** use the MSX BASIC default layout — getting that backwards is
  what caused a mislabeling round this project already had to correct).
  Also has `Sprite16Mono` for 16x16 hardware sprites: a 32-byte sprite
  pattern is four 8x8 quadrants (top-left, bottom-left, top-right,
  bottom-right), **not** 16 interleaved rows — that wrong-but-plausible
  guess was the tools' original behavior and produced a confidently wrong
  "vehicle sprite" identification for one asset before being caught and
  corrected (see the correction note in `docs/ANALYSIS.md`). And
  `BigChar16Mono` for a *third*, different 32-byte 16x16 layout: some
  background-tile "big font" characters in this ROM are four 8x8 tiles in
  plain raster order (top-left, top-right, bottom-left, bottom-right) —
  neither of the other two. Three layouts for the same byte count in one
  16KB ROM is exactly why "it rendered as something plausible" is not
  confirmation when more than one byte-ordering is possible — check
  whether the specific layout is actually justified first (destination
  address, which hardware table it targets, etc), the way `docs/
  ANALYSIS.md` traces each of these three back to real evidence.
- `internal/msx/` — `system.go` has the hardware-abstraction interface the
  port codes against (`WriteVDPRegister`, `WritePSGRegister`, `FillVRAM`,
  `WriteVRAM`, DI/EI); `vdp.go` is `VDP`, the concrete in-memory
  implementation (real registers + 16KB VRAM) that `cmd/play` renders from.
- `internal/game/` — the actual port. One file per subsystem: `boot.go`
  (cold boot, the per-frame `VBlankHook`, RAM accessors), `state.go` (the
  9 top-level game states), `gameplay.go` (the per-frame engine — road,
  traffic, physics, scoring — and by far the largest file), `graphics.go`
  (VRAM setup: font, Konami logo, HUD, per-stage scenery), `sound.go`
  (the sound engine and its channel scheduler), `input.go` (joystick +
  keyboard-matrix sampling).
- `assets/assets.go` — the only tracked file under `assets/`. It
  `//go:embed`s what `cmd/extract` wrote into `gfx/`, `sfx/` and `data/`,
  so `internal/game` needs neither the ROM nor `internal/rle` at runtime:
  decompression happened once, during extraction. Because the embed
  patterns reference files that don't exist until `cmd/extract` has run,
  this package — and so `go build ./...` — fails on a fresh clone until it
  has. Adding an asset means an entry in the manifest *and* an embed here.
- `cmd/play/` — runs the port against a real `msx.VDP` and renders VRAM
  with Ebitengine; see `docs/ANALYSIS.md`'s Tooling section. `go run
  ./cmd/play`. **For debugging any graphics/VRAM question (is a tile
  rendering right, is a sprite positioned correctly, what does the
  screen actually look like at some point), render through
  `internal/msxrender` — never reimplement a compositor in a throwaway
  test.** That reimplementation is what this project used to fall back
  on, and being separate code it silently drifted from what `cmd/play`
  actually rendered. There are two ways in, and neither needs a second
  renderer:

  - `cmd/screenshot` (below) — **works headlessly**, so this is the one
    to reach for from a sandboxed shell. It drives the game to the menu
    or into any stage and writes PNGs you can just read.
  - `cmd/play` itself, when you have a display: F12 dumps the current
    frame to `frame.png` (`-dump-path` to change it), or `-dump-every`
    auto-dumps on an interval, overwriting the same file.

  ```bash
  go run ./cmd/screenshot -out /tmp/shots   # headless
  go run ./cmd/play -dump-every=1s          # needs a display
  ```
- `internal/msxrender/` — the one compositor: Screen 2 background tiles
  plus the 32-entry hardware sprite table, VRAM in and an `image.RGBA`
  out. `cmd/play` draws every frame through it and `cmd/screenshot`
  captures stills through it, so a still cannot show something the
  running game wouldn't. Add renderers here, not next to a caller.
- `cmd/screenshot/` — headless still capture: boots a real
  `game.Console`, drives it through the attract sequence to the menu or
  into any stage (using the same entry points `cmd/play`'s cheat keys
  use), holds the accelerator so the road is moving, and writes PNGs.
  No window, so it runs anywhere. `go run ./cmd/screenshot -out
  screenshots` regenerates the README's screenshots exactly.
- `internal/psg/` — a real, verified PSG (AY-3-8910-compatible) audio
  synthesizer (`Chip`, an `io.Reader` an `ebiten/v2/audio.Player` can play
  directly). Wired into `cmd/play`, which plays the game's real audio.
- `cmd/sndpreview/` — interactive PSG tester: play notes/noise directly to
  verify the synthesizer, or trigger real `game.Console.TriggerSound` IDs
  and hear them — the sound engine (`advanceSoundChannel`) is ported, so
  these now play rather than just arming a RAM record. `go run
  ./cmd/sndpreview`.

## Conventions for porting new routines

- Every ported function's doc comment names the original Z80 address
  range(s) it came from (e.g. `// Ported from $4685-$46A8`), so anyone can
  jump back to `docs/listing.asm` and check the translation.
- Say what's confirmed vs. inferred, inline. If a name, a table boundary, or
  a branch's purpose is a guess rather than something traced end-to-end,
  say so in the comment — don't let a confident-sounding function name imply
  more certainty than the tracing actually supports. `docs/ANALYSIS.md` uses
  a three-level High/Medium/Low confidence key; reuse it.
- Prefer a stub with a clear `// TODO: not yet ported, see $XXXX` over a
  guessed implementation. A wrong translation that looks finished is worse
  than an honest gap.
- When a routine's RAM usage isn't confirmed, address it the way the
  original did (`c.RAM[0x0NN]`, matching `$E0NN`) rather than inventing a
  Go field name that implies more understanding than exists yet. Once a
  byte's role is confirmed, promote it to a named accessor (see
  `gameStateByte()`, `soundBusyFlag()` in `boot.go`) and note the address
  that confirmed it.
- After porting something that resolves a previously-stubbed CALL/JP
  target, re-run `cmd/disasm` — newly confirmed code addresses feed back
  into its jump-table/candidate heuristics and can recover more of the
  ROM automatically.
- Before extracting or classifying a graphics/data block from raw ROM
  bytes, check whether it's a confirmed RLE source first (`grep -n "LD
  DE,<addr>h" docs/listing.asm`, then check that CALLs into `$45E0`/`$4611`)
  and decode it with `cmd/rledecode` if so. A raw-byte render that "looks
  roughly like a font/tile" is not enough confirmation on its own — that's
  exactly how the font asset shipped with a few subtly corrupted glyphs
  earlier in this project. When decoding via `internal/rle`, the same
  standard applies to the *decode itself*: "it terminates cleanly on 0x00"
  and "most tiles look fine" are both satisfied by a decoder with two run
  kinds swapped (this happened once already — see `internal/rle/rle.go`'s
  package comment). Render the *entire* decoded output and check it's a
  complete, coherent set before trusting it; see the correction notes in
  `docs/ANALYSIS.md`.
