// Package rle implements the VRAM-blit compression scheme used throughout
// this ROM's graphics data (traced from $4611-$4634, with the SETWRT/OUT
// loop it drives at $4591 and $45B0). It is NOT a general-purpose format --
// it was written to stream decoded bytes straight out to the VDP data port
// one at a time, so decoding it into a flat byte buffer (as this package
// does, for extracting assets) is a straightforward re-reading of that same
// control flow with the VDP write replaced by an append.
//
// Stream format, one control byte at a time:
//   - 0x00        : end of stream
//   - 0x01-0x7F   : repeat run -- read one more byte, emit it N=control times
//   - 0x81-0xFF   : literal run -- copy the next N=(control&0x7F) bytes as-is
//   - 0x80        : destination retarget -- the next 2 bytes are a new VDP
//     write address (absolute VRAM address, little-endian); subsequent
//     output goes there instead of wherever writing left off. Reading of
//     the *control* stream (source) is completely unaffected -- it simply
//     continues with the byte right after those 2 address bytes. Traced
//     from $460B (see below): despite living at the same address as the
//     $460B *alternate entry point* (see below), this is a different use
//     of the same code, reached mid-stream via a plain jump rather than an
//     external CALL.
//
// The two run kinds are easy to mix up by their control-byte ranges alone;
// what actually distinguishes them in the original is which address the
// DJNZ loop jumps back to. The bit7-clear branch (starting at $462C) reads
// one byte, then DJNZ targets $462E -- *after* that read -- so it re-emits
// the same byte without advancing the source: a repeat run. The bit7-set
// branch (starting at $4621) DJNZ targets $4622, the read itself, so each
// iteration advances the source: a literal run. An earlier version of this
// file had these two swapped, which happened to decode some runs correctly
// by coincidence (e.g. a repeat-encoded run of zero bytes looks the same
// either way) while corrupting others -- exactly the "some glyphs fine,
// others not" symptom that caught it.
//
// $80 was *also* misread once: an earlier version of this file treated it
// as retargeting the *source* (jump the control stream to read from a new
// ROM address), on the theory that it let multiple compressed blocks share
// a common tail. Tracing $460B/$4611 register-by-register (which register
// SETWRT's address argument actually comes from at each point) showed the
// opposite: $80's 2 embedded bytes retarget the VDP *write* address
// (HL, i.e. the destination), and the *source* pointer (DE) is completely
// undisturbed, simply continuing right after those 2 bytes. This was only
// caught by fully decoding a real stream that uses it ($46FC, see
// docs/ANALYSIS.md) and getting nonsense until the direction was reversed
// -- no stream previously decoded exercises this path, so nothing else
// needed correcting once this was fixed.
//
// $460B is also reachable directly as an alternate *entry point*, not just
// as this in-stream control byte: a caller can `LD DE,<tableAddr>; CALL
// $460B` instead of the usual `LD HL,<dest>; LD DE,<streamAddr>; CALL
// $4611`. In that form, `<tableAddr>` holds a 2-byte VRAM destination
// address followed immediately by the real compressed stream -- $460B's
// preamble reads that word into HL and advances past it before falling
// into the same decode loop. Confirmed as literally the same code/effect
// as the in-stream case (down to the exact same register shuffle), just
// invoked from outside instead of from a $80 byte.
package rle

// Chunk is a run of decoded bytes whose destination was retargeted
// mid-stream via a $80 control byte (see the package doc comment) -- the
// destination is embedded in the stream itself, unlike the first run's
// (see Decode).
type Chunk struct {
	Dest uint16
	Data []byte
}

// Decode decodes the compressed stream starting at Z80 address `startAddr`
// within `rom` (the full cartridge image, addressed from `baseAddr` --
// $4000 for this ROM).
//
// It returns the first run's bytes as `out` -- this run's destination is
// NOT tracked here, matching the original: the code that CALLs $4611 sets
// up the VDP write address once via SETWRT *before* starting the decode
// loop, using whatever destination its own caller supplied (or, for the
// $460B alternate entry, the 2 bytes immediately preceding the stream --
// see the package doc comment). Decode has no way to know that value on
// its own; the caller already does, from tracing the call site.
//
// Any further runs the stream retargets to via $80 are returned as
// `extra`, each carrying its own destination address explicitly (since
// those addresses DO come from inside the stream). Most streams never use
// $80 and so return no extra chunks.
//
// endAddr is the address one past the terminating 0x00 in the control
// stream, or -1 if decoding ran off the end of the image without
// terminating. maxOut bounds the total output size (out plus all extra
// chunks combined) as a safety net against a malformed or misidentified
// start point looping forever; it does not affect correctly terminated
// streams shorter than the bound.
func Decode(rom []byte, baseAddr, startAddr, maxOut int) (out []byte, extra []Chunk, endAddr int) {
	pos := startAddr - baseAddr
	total := 0
	// emit appends to the current destination -- the initial run (`out`)
	// until the first $80, then whichever Chunk is currently open.
	emit := func(b ...byte) {
		if len(extra) == 0 {
			out = append(out, b...)
		} else {
			extra[len(extra)-1].Data = append(extra[len(extra)-1].Data, b...)
		}
		total += len(b)
	}

	for total < maxOut {
		if pos < 0 || pos >= len(rom) {
			return out, extra, -1
		}
		ctrl := rom[pos]
		pos++
		switch {
		case ctrl == 0x00:
			return out, extra, pos + baseAddr

		case ctrl == 0x80: // retarget the destination; source is unaffected
			if pos+1 >= len(rom) {
				return out, extra, -1
			}
			dest := uint16(rom[pos]) | uint16(rom[pos+1])<<8
			pos += 2
			extra = append(extra, Chunk{Dest: dest})

		case ctrl&0x80 == 0: // repeat run: one value byte repeated `ctrl` times
			n := int(ctrl)
			if pos >= len(rom) {
				return out, extra, -1
			}
			v := rom[pos]
			pos++
			for i := 0; i < n; i++ {
				emit(v)
			}

		default: // literal run of (ctrl&0x7F) distinct bytes
			n := int(ctrl & 0x7F)
			if pos+n > len(rom) {
				return out, extra, -1
			}
			emit(rom[pos : pos+n]...)
			pos += n
		}
	}
	return out, extra, -1
}
