// Package msx abstracts the MSX BIOS calls and hardware I/O that the
// original Z80 cartridge code relied on, so the ported game logic in
// internal/game can be expressed as ordinary Go without embedding a full
// Z80/VDP/PSG emulator. Each method corresponds 1:1 to a specific BIOS
// routine or hardware access identified in the disassembly (see
// docs/listing.asm and docs/ANALYSIS.md for the address-by-address mapping).
package msx

// System is the hardware surface the ported game logic runs against. A real
// backend (e.g. driving an actual VDP/PSG emulator, or a native
// reimplementation of the graphics/audio) implements this interface.
type System interface {
	// WriteVDPRegister corresponds to BIOS WRTVDP (address 0047h in the
	// original ROM's own local trampoline at $4038, which itself just jumps
	// to the real BIOS entry point at $0047). Calling convention observed in
	// the disassembly: B = value, C = register number.
	WriteVDPRegister(register, value byte)

	// WritePSGRegister corresponds to BIOS WRTPSG (address 0093h). Calling
	// convention observed: A = register number, E = value.
	WritePSGRegister(register, value byte)

	// ReadPSGRegister corresponds to BIOS RDPSG (address 0096h). Calling
	// convention: A = register number in, A = value out. The game's own
	// input routine ($46C3) uses this to read PSG register 14 (AY-3-8910
	// I/O Port A), which on MSX carries the joystick port's direction/
	// trigger lines -- not sound data -- after first using
	// WritePSGRegister on register 15 (I/O Port B) to select/configure
	// which joystick is being read.
	ReadPSGRegister(register byte) byte

	// SenseKeyboardMatrixRow corresponds to BIOS SNSMAT (address 0141h):
	// returns the specified keyboard matrix row's 8-bit state, one bit
	// per key, 0 meaning that key is held down (real MSX keyboard
	// matrix wiring is active-low). Unrelated to the PSG -- this is a
	// separate physical keyboard matrix scanned via the PPI, which the
	// original code's own port-level access this project doesn't
	// reimplement (see internal/msx.VDP.KeyboardMatrix).
	SenseKeyboardMatrixRow(row byte) byte

	// FillVRAM corresponds to BIOS FILVRM (address 0056h): fills `count`
	// bytes of VRAM starting at `addr` with `value`.
	FillVRAM(addr uint16, value byte, count uint16)

	// WriteVRAM corresponds to BIOS LDIRVM (address 005Ch): copies `data`
	// into VRAM starting at `addr`. Used for the *net effect* of the
	// original code's "CALL $45E0 to decompress an RLE stream into VRAM"
	// -- the port loads the already-decoded bytes (see the assets package)
	// directly rather than reimplementing the decompressor
	// (internal/rle) as part of the running game.
	WriteVRAM(addr uint16, data []byte)

	// ReadVRAM corresponds to BIOS RDVRM (address 004Ah): reads a single
	// byte from VRAM. Used by mirrorPatternRows/$4652 (via its $4679/
	// $004A callee) to read back an already-placed tile pattern before
	// bit-reversing it into a mirrored copy.
	ReadVRAM(addr uint16) byte

	// DisableInterrupts / EnableInterrupts correspond to the Z80 DI/EI
	// instructions, used throughout the original code to guard sections
	// that must not be interrupted by the per-frame VBlank hook.
	DisableInterrupts()
	EnableInterrupts()
}
