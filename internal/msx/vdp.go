package msx

// VDP is a concrete, in-memory System implementation modeling the
// TMS9918A's 8 VDP registers and 16KB of VRAM directly. It has no display
// of its own -- a renderer (e.g. cmd/play, backed by Ebitengine) reads
// VRAM and the derived table-base addresses below to draw the screen.
// PSG registers are stored but not sonified yet.
//
// Despite the name, this struct also holds the rest of the MSX's
// non-CPU hardware state this project has needed so far -- PSG
// registers, and now the keyboard matrix -- bundled here for the same
// reason PSG already was: one concrete struct standing in for "the
// hardware", rather than a separate type per chip for a project this
// size. KeyboardMatrix and PSG registers 14/15 (the joystick I/O ports)
// default to "nothing pressed"/"idle" and are never updated by anything
// in this package -- a driver (e.g. cmd/play) is expected to write real
// host input into them each frame; see SenseKeyboardMatrixRow and
// ReadPSGRegister.
type VDP struct {
	Reg  [8]byte
	VRAM [0x4000]byte
	PSG  [16]byte

	// KeyboardMatrix models the MSX keyboard matrix's 11 rows (8 keys
	// each), bit=0 meaning that key is held down (matching real
	// hardware's active-low wiring) -- see SenseKeyboardMatrixRow. All
	// bits default to 1 (nothing pressed); NewVDP sets this up since
	// Go's zero value for a byte array is all-0, the opposite of "idle."
	KeyboardMatrix [11]byte
}

// NewVDP returns a VDP with all hardware state at its real power-on/
// idle default -- notably KeyboardMatrix rows and PSG registers 14/15
// (the joystick I/O ports) default to $FF (nothing pressed/idle), not
// Go's zero value. This matters concretely, not just cosmetically: real
// joystick lines are pulled high when idle, and the game's own input
// routine ($46C3, see internal/game/input.go) complements (CPL) the
// value it reads from register 14 before using it -- leaving it at
// Go's zero-value default would have every joystick direction/button
// read as permanently "pressed."
func NewVDP() *VDP {
	v := &VDP{}
	for i := range v.KeyboardMatrix {
		v.KeyboardMatrix[i] = 0xFF
	}
	v.PSG[14] = 0xFF
	v.PSG[15] = 0xFF
	return v
}

func (v *VDP) WriteVDPRegister(register, value byte) {
	if int(register) < len(v.Reg) {
		v.Reg[register] = value
	}
}

func (v *VDP) WritePSGRegister(register, value byte) {
	if int(register) < len(v.PSG) {
		v.PSG[register] = value
	}
}

// ReadPSGRegister is BIOS RDPSG ($0096). For the sound-relevant
// registers (0-13) this is just reading back whatever was last written,
// same as real silicon. Registers 14/15 are the AY-3-8910's I/O ports;
// on MSX, port A (14) carries joystick direction/trigger state driven
// by external hardware, not by anything written via WritePSGRegister --
// so its value only reflects real input once a driver writes it in (see
// the VDP doc comment). Out-of-range registers read as $FF, matching
// this package's other "absent hardware reads idle" choices.
func (v *VDP) ReadPSGRegister(register byte) byte {
	if int(register) < len(v.PSG) {
		return v.PSG[register]
	}
	return 0xFF
}

// SenseKeyboardMatrixRow is BIOS SNSMAT ($0141): returns the given
// keyboard matrix row's current state (see KeyboardMatrix). Out-of-range
// rows read as $FF (nothing pressed), matching a real MSX's behavior for
// the matrix's unused rows.
func (v *VDP) SenseKeyboardMatrixRow(row byte) byte {
	if int(row) < len(v.KeyboardMatrix) {
		return v.KeyboardMatrix[row]
	}
	return 0xFF
}

func (v *VDP) FillVRAM(addr uint16, value byte, count uint16) {
	for i := uint16(0); i < count; i++ {
		v.VRAM[(addr+i)&0x3FFF] = value
	}
}

func (v *VDP) WriteVRAM(addr uint16, data []byte) {
	for i, b := range data {
		v.VRAM[(int(addr)+i)&0x3FFF] = b
	}
}

// ReadVRAM is BIOS RDVRM ($004A).
func (v *VDP) ReadVRAM(addr uint16) byte {
	return v.VRAM[addr&0x3FFF]
}

func (v *VDP) DisableInterrupts() {}
func (v *VDP) EnableInterrupts()  {}

// Table base addresses below are derived from the VDP registers the same
// way this specific ROM's own values were reverse-engineered in
// docs/ANALYSIS.md's "VRAM memory map" section -- the Graphic 2 addressing
// quirk this game's boot sequence relies on (color/pattern base selected
// by a single bit, not the general TMS9918A per-mode formula for every
// register). Confirmed against this ROM's actual boot-time register
// values: name table $3800, color $0000, pattern $2000, sprite pattern
// $1800, sprite attribute table $3B00 (immediately after the 768-byte
// name table, with zero gap) -- a complete, non-overlapping 16KB layout.

// NameTableBase is R2*0x400.
func (v *VDP) NameTableBase() int { return int(v.Reg[2]) * 0x400 }

// ColorTableBase is $2000 if R3 bit 7 is set, else $0000.
func (v *VDP) ColorTableBase() int {
	if v.Reg[3]&0x80 != 0 {
		return 0x2000
	}
	return 0
}

// PatternTableBase is $2000 if R4 bit 2 is set, else $0000.
func (v *VDP) PatternTableBase() int {
	if v.Reg[4]&0x04 != 0 {
		return 0x2000
	}
	return 0
}

// SpritePatternTableBase is R6*0x800.
func (v *VDP) SpritePatternTableBase() int { return int(v.Reg[6]) * 0x800 }

// SpriteAttrTableBase is R5*0x80: the 32-entry, 4-byte-per-sprite sprite
// attribute table (Y, X, pattern number, color/EC). Confirmed for this
// specific ROM's boot-time register value (R5=$76, giving base $3B00) by
// cross-referencing two independent things landing on exactly that
// address: internal/game.copyTrackDataToVRAM's own destination, and a
// direct byte write of $D0 (the real TMS9918A "hide this sprite" Y value)
// found in the ROM at that same address -- see docs/ANALYSIS.md's sprite
// write-up.
func (v *VDP) SpriteAttrTableBase() int { return int(v.Reg[5]) * 0x80 }

// BackdropColor is R7's low nibble: the TMS9918A "backdrop" color, shown
// in the border area surrounding the 256x192 active display (and,
// outside Screen 2, wherever color index 0 would otherwise appear).
func (v *VDP) BackdropColor() byte { return v.Reg[7] & 0x0F }

// SetPowerOnDefaults programs the VDP registers to the state a real MSX's
// system BIOS establishes during its own power-on initialization, before
// handing control to a cartridge -- NOT ported from any ROM this project
// has (the MSX system BIOS is a separate, non-Konami ROM, entirely out of
// scope here): it's a reasonable, documented approximation of the
// well-known default MSX boot appearance (background/backdrop color 4,
// blue), for callers that want their emulated hardware to start in a
// realistic state -- matching what real hardware briefly shows -- rather
// than all-registers-zero (black) before a cartridge's own init (e.g.
// this ROM's InitVDPAndSoundEngine, which sets its own register 7 a few
// frames later regardless) takes over.
func (v *VDP) SetPowerOnDefaults() {
	v.WriteVDPRegister(7, 0xF4) // text color white (bits 4-7), backdrop color blue (bits 0-3)
}
