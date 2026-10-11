# Porting an MSX cartridge ROM to native Go — project plan

Intended as the `CLAUDE.md` for a new ROM-port project. It is distilled from
a completed one (a 16KB MSX1 cartridge, fully ported and playable), so every
rule below is here because something went wrong without it, or because it is
the thing that actually caught the bug. Where a rule cost real rework, the
story is kept — a rule with its scar attached gets followed.

Replace the bracketed bits with this project's specifics as you learn them.

**The goal is a native Go port, not an emulator.** No Z80 is executed at
runtime. Each routine is read, understood, and rewritten as ordinary Go
against a small hardware-abstraction interface. That distinction drives
everything else: the legal position, the verification strategy, and what
"done" means for a routine.

---

## Phase 0 — Repo hygiene and legal, before any code

Do this on day one. The previous project retrofitted it at the end and it
cost a full day of migration plus a careful audit, because by then ~3,000
bytes of ROM data had accumulated inside committed Go source.

1. **`.gitignore` the ROM, the extracted assets and the generated
   disassembly, in the very first commit.** `*.rom`, `assets/**/*.bin`,
   `docs/listing.asm`. All three are the rights holder's data — decompressed
   or decoded, but not yours. Screenshots of your own running engine are
   fine.
2. **No ROM bytes in committed Go source.** This is the rule that is easiest
   to break by accident and most expensive to undo. A data table longer than
   a few bytes goes through the extract tool (Phase 1) and is reached via
   `//go:embed`, not pasted in as `[217]byte{0x..., ...}`. Short constants
   (a 2-byte tile pair, a palette index) are fine; use judgement, but when
   in doubt extract it.
3. **Name the repo for what it is, not what it plays.** A generic,
   descriptive name. Do not put the original title or publisher in the
   repository name, the module path, the window title, or any heading.
   Reference them only where technically necessary — the ROM filename a user
   must actually look for, and the tracing notes.
4. **Add `NOTICE` and a README "Legal" section up front:** unofficial,
   non-commercial, not affiliated or endorsed; what is and is not
   distributed; that the engine executes no original code; an invitation for
   rights holders to open an issue. Add a `LICENSE` too if the README is
   going to call the project open source.
5. **Decide the module path now.** Renaming later is mechanical but touches
   every import.

## Phase 1 — The tooling spine

Build these before porting anything. Each one paid for itself many times
over. Roughly in this order:

| Tool | Why it has to exist |
| --- | --- |
| `internal/z80dis` + `cmd/disasm` | Recursive-descent disassembler with **byte-level coverage tracking**, so a misaligned decode can't quietly corrupt already-confirmed code. Needs special handling for computed-jump trampolines; find them early. |
| `cmd/extract` | Rebuilds every asset from the ROM via a **manifest** (address, length, and *how*: raw range, compressed stream, or derived). This is the thing that lets you ship no ROM data. Give it a `-check` mode that re-derives everything and diffs without writing. |
| `internal/<compression>` + `cmd/rledecode` | Almost certainly the graphics are compressed. Decode to flat buffers for extraction. |
| `cmd/gfxpreview`, `cmd/gfxviewer` | Render byte ranges as tile grids to classify data. |
| `internal/msxrender` + `cmd/screenshot` | **The single most valuable tool.** See "One renderer" below. |
| `cmd/sndpreview` | Only once you reach audio. |

### One renderer, shared — and a headless way to use it

Ebitengine (or any windowed toolkit) cannot run in a sandboxed shell, which
means you cannot see what you just built. The trap is to write a throwaway
compositor inside a test to inspect VRAM. **Do not.** That copy silently
drifts from what the game actually renders, and then you are debugging a
rendering difference that does not exist in the real program.

Instead:

- Put the compositor in its own package (`internal/msxrender`): VRAM in,
  `image.RGBA` out.
- The game draws every frame through it.
- A headless `cmd/screenshot` drives a real console instance (attract
  sequence → menu → into a stage, holding the accelerator) and writes PNGs
  through *the same* package.

A still then cannot show something the running game would not. This also
gives you the regression test in Phase 5.

## Phase 2 — Map the ROM before porting

1. Generate the listing. Re-run `cmd/disasm` whenever you confirm new code
   addresses: they feed back into its jump-table heuristics and recover more
   of the ROM automatically.
2. Record the ROM's checksums (CRC32/MD5/SHA-1/SHA-256) and size in the
   analysis doc, and have `cmd/extract` warn on a mismatch. Every address in
   your notes is meaningless against a different dump.
3. **Derive the VRAM layout from the ROM's own VDP register writes.** Do not
   assume the MSX BASIC default. The previous ROM used a non-default layout
   (colour table at `$0000`, patterns at `$2000`, name table at `$3800`,
   sprites at `$1800`/`$3B00`), and assuming the textbook layout mislabelled
   every pattern/colour pair backwards — "pattern" files that were colour
   data and vice versa. Confirm the derivation by compositing one
   pattern+colour pair and checking you get readable text or a recognisable
   shape.
4. Find the top-level state machine and the per-frame tick early. Everything
   else hangs off them.

## Phase 3 — Assets

- **Check whether a block is compressed before extracting it.** Grep the
  listing for a `LD DE,<addr>` feeding a call into the decompressor. A raw
  render that "looks roughly like tiles" is not confirmation; that is exactly
  how a font shipped with subtly corrupted glyphs.
- **Render the entire decoded output and check it is a complete, coherent
  set.** Not a spot check. "It terminates cleanly" and "most tiles look
  fine" were both satisfied by a decoder with its two run kinds swapped.
- **Tight-packing is the strongest cheap signal**: a correctly-sized
  extraction ends exactly where the next known thing begins.
- **Verify provenance empirically; never trust a filename.** In the previous
  project two assets were *named* for a ROM address that really is a valid
  compressed stream — decoding from it succeeds and yields plausible,
  completely wrong data. They were actually load-time mirrors of slices of
  other assets. Confirm each manifest entry by extracting it and diffing
  against the file you already have.
- Assets the original computes at load time (mirrored tiles, derived
  colours) belong in the manifest as derived entries, computed from their
  source asset — not as separate captured blobs.

## Phase 4 — Port the engine

Work outward from the state machine, one routine at a time.

### Conventions (keep these strict)

- Every ported function's doc comment names the Z80 address range it came
  from (`// Ported from $4685-$46A8`), so anyone can jump back to the
  listing and check the translation.
- **Say what is confirmed vs inferred, inline.** Use a three-level key —
  High (traced instruction by instruction, and/or visually confirmed),
  Medium (control flow clear, meaning inferred), Low (plausible guess). A
  confident-sounding function name must not imply more certainty than the
  tracing supports.
- **Prefer an honest stub with `// TODO: not yet ported, see $XXXX` over a
  guessed implementation.** A wrong translation that looks finished is worse
  than a visible gap.
- Address unconfirmed RAM the way the original did (`c.RAM[0x0NN]` for
  `$E0NN`). Promote to a named accessor only once the byte's role is
  confirmed, and note what confirmed it.
- Keep the analysis doc append-only for corrections: add a correction note
  rather than editing history away. The record of *how* something was got
  wrong is worth as much as the right answer.

### The one failure mode that will cost you the most

**Assuming where a routine starts and ends, instead of reading it.**

This single mistake, in six different disguises, caused nearly every real
bug in the previous port:

- A shared helper was a *tripler* — it wrote three copies, 2KB apart, for
  the three Screen 2 thirds. Read as a single write because the call site
  looked like one.
- A terminator byte in a data stream was really a *gap marker*; the stream
  continued past it.
- A "row repeat" copier actually advanced its source pointer each row, so
  rows walked forward through ROM.
- `JP L70E5` landed in the *middle* of another routine, not at the start of
  the one whose `CALL` sat at that address. Control continued through three
  more calls that the port never ran.
- A scoring routine looked like it ended at its `RET`; the no-carry branch
  three instructions earlier fell through into the high-score update, which
  therefore never ran at all.
- A state handler was read starting three instructions too late, dropping
  the two `CALL`s that cleared the screen.

The rules that would have prevented all six:

1. **Read from the actual branch target, not from the nearest label.** When
   something jumps to `$411A`, start reading at `$411A`.
2. **Follow every fall-through.** A `RET` is not the end of a routine if a
   conditional branch above it skips past to more code. Trace each exit.
3. **Read the callee before modelling a call.** Never infer what a shared
   routine does from how one call site uses it.
4. **For loops, the jump target tells you the semantics.** In the previous
   ROM the two compression run-kinds are distinguished *only* by whether
   `DJNZ` targets the byte-read or the instruction after it — one re-emits
   without advancing (repeat), the other advances (literal). The control-byte
   ranges alone look symmetric and will mislead you.
5. When a bug makes no sense, re-read the raw bytes of the routine rather
   than re-reading your Go.

### Z80 and MSX semantics that bit us

- **`DJNZ` decrements a register, not RAM.** A dispatch that loads a state
  byte into `B` each frame and `DJNZ`es on it is an *inefficient switch over
  the current value*, not a persistent countdown. Modelling it as a RAM
  countdown broke the main loop for a long time.
- **8-bit wrap is load-bearing.** `ADD A,A` wraps at 8 bits; widening to
  `uint16` before doubling changes behaviour. Model the width the original
  used.
- **BCD via `DAA`** — scores are packed BCD. Write a small
  `bcdAddCarry(a, b, carryIn)` helper and use it everywhere.
- **16-bit loads read two bytes** (`LD BC,(E000h)` fetches `$E000` *and*
  `$E001`), and overlapping 16-bit stores can touch three addresses.
- **TMS9918A specifics:** colour index 0 is transparent and shows the
  backdrop (not black); sprite Y `$D0` terminates the sprite list for all
  following entries; displayed sprite row is Y+1; a 16x16 sprite's pattern
  number has its low 2 bits ignored and the table is addressed in 8-byte
  units, not 32.
- **The same 32 bytes have multiple 16x16 layouts in one ROM.** Hardware
  sprites are four 8x8 quadrants in TL/BL/TR/BR order; "big font" background
  characters are TL/TR/BL/BR; neither is 16 interleaved rows. All three
  produce *something* plausible from the same bytes. Justify the layout from
  the destination address and which hardware table it targets — never from
  "it rendered as something".
- Entropy sources (`LD A,R`) have no equivalent in a native port. Substitute
  a real RNG, preserve the *outcome distribution*, and document the
  deviation rather than faking the source.

## Phase 5 — Make it playable, then keep it that way

Getting it on screen early is what surfaces bugs; static analysis does not.

**Two verification steps, run before every commit:**

```bash
go run ./cmd/extract -check    # every asset still matches the ROM
go run ./cmd/screenshot -out /tmp/shots && \
  for f in screenshots/*.png; do cmp "$f" "/tmp/shots/$(basename "$f")"; done
```

- `-check` after touching the manifest, the assets package, or how any asset
  is produced. A wrong address fails loudly instead of shipping plausible,
  wrong data.
- The screenshot byte-diff after any engine change *not* meant to alter
  behaviour — a refactor, moving data into assets, a rename. All stills
  staying byte-identical exercises the whole pipeline at once: name table,
  tiles, colours, sprites, HUD. It is the cheapest high-coverage test you
  will have. When a change *is* meant to alter what is drawn, regenerate the
  stills, commit them, and say so in the message.

**Commit after each logical change**, once `go build ./...` and
`go vet ./...` are clean. Never commit ROM-derived files; if `git status`
offers them, `.gitignore` is broken.

**Write throwaway probes as real programs, delete them after.** Temporary
`_test.go` files in the engine package, or a scratch `cmd/` directory, both
work; just remove them before committing. Measuring a change ("off-road
object-frames fell from 175 to 20 over 12,000 frames") beats asserting it.

## Phase 6 — Only once it plays

Extensions (a second renderer, new modes, tooling) should sit *over* the
port, never inside it. The previous project added a 3D chase-camera mode as
a separate package that reads the VRAM the engine already wrote and
reinterprets it; the engine itself was untouched, so the 3D mode shows the
real simulation and cannot desync from it. Keep that boundary.

---

## Working with the user

- **The user playing the game is your best bug detector.** Most real defects
  in the previous port were found that way, not by analysis. Ask them to
  play after each milestone.
- **When the user says it's still wrong, believe them.** On one bug I twice
  concluded "this is genuine ROM behaviour" and was wrong both times; a
  real-hardware screenshot they supplied settled it. Both times the error was
  the same: combining two true statements into a false one without reading
  the routine they both depended on.
- **A real-hardware screenshot or video is worth a day of tracing.** Ask for
  one whenever behaviour is disputed.
- **Report measurements, not impressions.** "All seven stills byte-identical"
  and "longest off-road stretch 86 frames → 2" are checkable; "looks right"
  is not.
- Distinguish, out loud, between a bug in the port and faithful reproduction
  of an original quirk. Both are common. Say which you think it is and why.

## Documentation hygiene

- Keep one analysis doc with: an **Open issues** list at the top (what is
  actually open, nothing resolved left sitting in it), the confidence-rated
  asset inventory, the VRAM map, the compression format, and dated
  write-ups of every bug and correction.
- **Sweep for stale claims periodically.** Doc comments rot fast. Near the
  end of the previous project a sweep found four comments still saying
  "not yet ported" about work long since finished, an instruction telling
  future sessions to commit files that must never be committed, and a layout
  section describing a directory structure that had changed. Each had been
  true when written.
- When you move a resolved item out of the open list, move the *write-up*
  into the body rather than deleting it.

---

## Suggested first week

1. Repo, `.gitignore`, `NOTICE`, README skeleton, module path. *(Phase 0)*
2. Disassembler + listing + ROM checksums recorded. *(Phases 1-2)*
3. VRAM layout derived from the ROM's own register writes and confirmed by
   compositing one pattern+colour pair.
4. Compression format traced; decoder; first assets extracted and
   **fully rendered** for inspection.
5. `cmd/extract` with a manifest and `-check`; `internal/msxrender`;
   `cmd/screenshot`. Nothing ROM-derived committed, ever.
6. Boot path + state machine ported far enough to show the title screen, and
   a first screenshot committed as the regression baseline.
