# go-msx-racer

An open-source Go engine implementation for a 1985 MSX racing game.

> **Unofficial and unaffiliated.** This is an independent, non-commercial
> hobby project. It is not affiliated with, authorised by, endorsed by, or
> connected to Konami in any way. All trademarks are the property of their
> respective owners, referenced here only to describe which hardware and
> software this engine interoperates with. **No game data is distributed
> in this repository** — the engine reads a cartridge image you must
> supply yourself, from your own copy. See [Legal](#legal).

The engine is a native Go re-implementation, **not** an emulator: the
original Z80 machine code was reverse-engineered routine by routine and
rewritten as ordinary Go, running against a small hardware-abstraction
layer (`internal/msx.System`) that models the MSX1's TMS9918A video chip
and AY-3-8910 sound chip. No Z80 instructions are executed at runtime.

It runs the game end to end — attract sequence, level-select menu, all six
stages, traffic, fuel, scoring, crashes, the inter-stage map screen and
real PSG audio.

![Level select](screenshots/select.png)

## The game

A top-down racing game. You drive a car up a continuously scrolling road,
holding the accelerator as much as you dare while traffic, curves and
roadside obstacles get in the way. Touching another car or leaving the
road spins you out. Fuel drains constantly and is topped up by driving
over the hearts scattered along the course; running dry ends the race.
Reaching the end of a stage advances you to the next one.

![Level 5 - river](screenshots/stage5.png)

## Getting started

The ROM is copyrighted, so neither it nor anything extracted from it —
the game's graphics and sound data, and the disassembly listing — is
distributed in this repository. Two steps get you from a fresh clone to a
running game.

**1. Supply the ROM.** Put your own copy of
`Road Fighter - Konami (1985) [7349].rom` at the repo root. The dump
everything here was traced against is recorded with its checksums in
[`docs/ANALYSIS.md`](docs/ANALYSIS.md) ("ROM used"); `cmd/extract` warns
if yours differs.

**2. Extract the assets.** Once, before anything else:

```bash
go run ./cmd/extract
```

That decompresses the game's graphics and sound data out of the ROM, and
copies out the data tables the engine reads directly, into `assets/` —
which the port embeds at build time. It writes 59 files and takes a
moment. `go run ./cmd/extract -check` re-verifies them against the
ROM without writing anything.

Skipping this step gives a build error like
`pattern gfx/bigfont_pattern_road_4A25.bin: no matching files found` —
that just means the assets aren't there yet.

**3. Play.**

```bash
go run ./cmd/play
```

Needs Go 1.27+ and a display. The window is the MSX's 256x192 screen at
3x scale. Once the assets are extracted, `cmd/play`, `cmd/screenshot`,
`cmd/gfxviewer` and `cmd/sndpreview` no longer need the ROM at all.

### Working on the port

With Nix (flakes enabled), enter the development shell for Go 1.27 and
the native graphics/audio build and runtime dependencies:

```bash
nix develop
go run ./cmd/extract   # once, after supplying your own ROM as above
go build ./...
go run ./cmd/play
```

Run these commands from the repository root. The shell supports x86_64 and
ARM64 Linux and Apple Silicon macOS; playing still needs a graphical
desktop and working host graphics and audio drivers (including
X11/XWayland on Linux). Assets stay local:
the flake provides a development shell rather than a packaged game,
because the build embeds the ROM-derived files excluded from Git.

On Linux, the shell supplies ALSA plugins for desktop audio and uses
`/run/opengl-driver/lib` when available. On other Linux distributions
without that driver integration, OpenGL may need a
[nixGL wrapper](https://github.com/nix-community/nixGL) to access compatible
GPU drivers. A `GLX: No GLXFBConfigs returned` startup error can indicate
that the OpenGL driver libraries are not being found.

To trace routines yourself you also want the disassembly, which is
likewise generated rather than distributed:

```bash
go run ./cmd/disasm -rom "Road Fighter - Konami (1985) [7349].rom" -out docs/listing.asm
```

That writes the ~8000-line annotated listing every ported function's doc
comment refers to by address. Start from
[`docs/ANALYSIS.md`](docs/ANALYSIS.md).

### Controls

The MSX original is driven by a joystick or the equivalent keyboard keys;
the port maps the host keyboard onto that same matrix.

| Key | Action |
| --- | --- |
| Left / Right arrow | Steer |
| Space | Accelerate (release to slow down) |
| Up / Down arrow | At the menu: switch between LEVEL A and LEVEL B |
| Space (at the menu) | Start the race |
| Right Shift | The MSX SELECT key, wired as fire 2 — mapped for completeness; no confirmed in-race use |
| Esc | Reset (simulated power cycle) |

To start a normal game, let the attract sequence run to the "PLAY SELECT"
menu, choose LEVEL A or B with the arrows, and press Space.

## Cheats and options

These are conveniences added by `cmd/play` for development and testing.
None of them exist in the original cartridge.

| Key | Effect |
| --- | --- |
| `1`–`6` | At the "PLAY SELECT" menu, start directly on that stage instead of stage 1 |
| `F` | Refill the fuel gauge |
| `E` | While racing, jump to the end of the current stage |
| `F12` | Write the current frame to a PNG |

Stage `1` is the ordinary fresh-game start, countdown and all. Stages `2`–`6`
start through the same path the ROM itself uses when it isn't running that
countdown, so they arrive the way a stage advance does — screen wipe, HUD,
the stage's own scenery scrolling in, "START" — with no starting-signal
countdown. See `docs/ANALYSIS.md` (open issues, item 7) for exactly how
that differs from a stage reached by playing into it.

Command-line flags:

| Flag | Meaning |
| --- | --- |
| `-dump-path` | Where `F12` and `-dump-every` write the frame (default `frame.png`) |
| `-dump-every` | Also dump a frame on this interval, e.g. `-dump-every=1s`, overwriting each time |

`-dump-every` is the intended way to inspect what the port actually draws:

```bash
go run ./cmd/play -dump-every=1s
```

## Tools

| Tool | What it does |
| --- | --- |
| `cmd/play` | The game itself (above). |
| `cmd/extract` | Rebuilds every file under `assets/` from the ROM — the one-time setup step above. `-check` verifies the existing files instead of writing. |
| `cmd/gfxviewer` | Interactive browser for every extracted graphic in `assets/gfx/`, plus synthesized atlases of the sprites, tiles and meta-tiles. Arrows/Space/N/P to move between assets; `T` toggles 8x8 / 16x16; `L` switches 16x16 layout; `C` toggles real color; `G` toggles a grid; `Esc`/`Q` quits. Needs no ROM. |
| `cmd/disasm` | Recursive-descent Z80 disassembler for this ROM, with the jump-table trampolines and coverage tracking it needs. Regenerates `docs/listing.asm`. |
| `cmd/rledecode` | Decodes one of the ROM's compressed graphics streams starting at a given address. |
| `cmd/gfxpreview` | Renders a byte range as a tile-grid PNG, for classifying graphics data. |
| `cmd/screenshot` | Headless still capture — boots the game, drives it to the menu or into any stage, and writes PNGs. No window needed, so it works over a plain shell: `go run ./cmd/screenshot -out screenshots`. Flags: `-scale`, `-border`, `-drive` (frames to hold the accelerator before capturing). |
| `cmd/sndpreview` | Interactive PSG tester: `1`–`8` play notes, `N` toggles noise, arrows pick a game sound id, Enter/Space triggers it, `S` silences, `Esc`/`Q` quits. |

`cmd/extract`, `cmd/disasm`, `cmd/rledecode` and `cmd/gfxpreview` read the
cartridge image; the rest work from the extracted assets alone. All of them
default to, or take, the ROM at the repo root.

## How the Z80 ROM became Go

Briefly: `cmd/disasm` turns the 16KB cartridge into an annotated
disassembly (`docs/listing.asm`), routines are traced from there by hand,
and each is rewritten as a Go function whose doc comment names the address
range it came from — so any translation can be checked against the
original. Graphics and sound data are decompressed once by `cmd/rledecode`
into `assets/`, which the port embeds, so nothing needs the ROM at
runtime. Anything not yet traced is left as an explicit stub rather than a
plausible guess, and every finding carries a confidence rating.

The details — what is confirmed versus inferred, the extracted-asset
inventory, the VRAM layout, the compression format, the open issues, and a
running record of the mistakes this project made and corrected — are in
**[`docs/ANALYSIS.md`](docs/ANALYSIS.md)**. Conventions for adding to the
port are in [`CLAUDE.md`](CLAUDE.md).

## Screenshots

These are all six stages.

| | | |
| --- | --- | --- |
| ![Stage 1](screenshots/stage1.png) | ![Stage 2](screenshots/stage2.png) | ![Stage 3](screenshots/stage3.png) |
| Stage 1 — fields | Stage 2 — beach | Stage 3 — lakeside |
| ![Stage 4](screenshots/stage4.png) | ![Stage 5](screenshots/stage5.png) | ![Stage 6](screenshots/stage6.png) |
| Stage 4 — woodland | Stage 5 — river | Stage 6 — snow |

All of these, and the level-select shot above, are captured by
`cmd/screenshot` from the running port, rendered through the same
compositor `cmd/play` draws with.

## Legal

**This project is unofficial.** It is not affiliated with, authorised by,
endorsed by, or connected to Konami or any other rights holder. It is
non-commercial and exists for interoperability, preservation and study.
Trademarks referenced anywhere in this repository — including the names of
the hardware and of the original software this engine interoperates with —
belong to their respective owners and are used only descriptively, to say
what this code does.

**What is distributed here:** original Go source code, written from
scratch, plus documentation and screenshots of the running engine.

**What is not distributed here, and never will be:** the cartridge ROM;
the graphics and sound data extracted from it (`assets/**/*.bin`); and the
generated disassembly (`docs/listing.asm`). All three are excluded by
`.gitignore`. You supply your own legally-obtained cartridge image, and
`cmd/extract` and `cmd/disasm` rebuild the rest locally on your machine.

**Why this is a re-implementation, not a copy.** The engine executes no
Z80 code and contains no copy of the original program. Each routine was
studied to determine its observable behaviour and then written anew in Go
against an abstract hardware interface, so that the game runs natively on
modern operating systems. Reverse engineering for interoperability of this
kind is protected in many jurisdictions; that protection is the basis on
which this project is published.

None of the above is legal advice, and it is not a warranty. If you are a
rights holder with a concern about this repository, please open an issue.
See [NOTICE](NOTICE).
