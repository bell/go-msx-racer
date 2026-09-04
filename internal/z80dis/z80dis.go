// Package z80dis implements a linear Z80 instruction disassembler,
// sufficient to disassemble MSX cartridge ROM code (no undocumented
// opcodes required beyond the common DD/FD CB forms).
package z80dis

import "fmt"

// Instr is one decoded instruction.
type Instr struct {
	Addr        int    // address of first byte
	Len         int    // length in bytes
	Text        string // mnemonic + operands
	Bytes       []byte
	IsCall      bool // CALL / CALL cc
	IsJump      bool // JP / JR (unconditional or conditional)
	IsRet       bool
	Target      int // branch target address, if IsCall/IsJump and target known
	HasTgt      bool
	Fallthrough bool // true if execution can continue to Addr+Len (i.e. not unconditional jump/ret/jp(hl)/rst-that-doesn't-return)
}

var r8 = [8]string{"B", "C", "D", "E", "H", "L", "(HL)", "A"}
var rp = [4]string{"BC", "DE", "HL", "SP"}
var rp2 = [4]string{"BC", "DE", "HL", "AF"}
var cc = [8]string{"NZ", "Z", "NC", "C", "PO", "PE", "P", "M"}
var alu = [8]string{"ADD A,", "ADC A,", "SUB ", "SBC A,", "AND ", "XOR ", "OR ", "CP "}
var rot = [8]string{"RLC", "RRC", "RL", "RR", "SLA", "SRA", "SLL", "SRL"}

func u8(b []byte, i int) byte {
	if i < len(b) {
		return b[i]
	}
	return 0
}

func i8(v byte) int {
	if v >= 0x80 {
		return int(v) - 256
	}
	return int(v)
}

// Decode decodes one instruction from b (the full ROM/memory image) starting
// at offset off (index into b), where the corresponding Z80 address is addr.
func Decode(b []byte, off int, addr int) Instr {
	start := off
	op := u8(b, off)

	mk := func(l int, text string) Instr {
		return Instr{Addr: addr, Len: l, Text: text, Bytes: append([]byte{}, b[start:min(start+l, len(b))]...), Fallthrough: true}
	}

	switch {
	case op == 0xDD || op == 0xFD:
		idx := "IX"
		if op == 0xFD {
			idx = "IY"
		}
		return decodeIndexed(b, off, addr, idx)
	case op == 0xCB:
		return decodeCB(b, off+1, addr, "", 0)
	case op == 0xED:
		return decodeED(b, off, addr)
	}

	x := op >> 6
	y := (op >> 3) & 7
	z := op & 7
	p := y >> 1
	q := y & 1

	switch x {
	case 0:
		switch z {
		case 0:
			switch y {
			case 0:
				return mk(1, "NOP")
			case 1:
				return mk(1, "EX AF,AF'")
			case 2:
				d := i8(u8(b, off+1))
				tgt := addr + 2 + d
				i := mk(2, fmt.Sprintf("DJNZ %s", label(tgt)))
				i.IsJump = true
				i.Target = tgt
				i.HasTgt = true
				return i
			case 3:
				d := i8(u8(b, off+1))
				tgt := addr + 2 + d
				i := mk(2, fmt.Sprintf("JR %s", label(tgt)))
				i.IsJump = true
				i.Target = tgt
				i.HasTgt = true
				i.Fallthrough = false
				return i
			default:
				d := i8(u8(b, off+1))
				tgt := addr + 2 + d
				i := mk(2, fmt.Sprintf("JR %s,%s", cc[y-4], label(tgt)))
				i.IsJump = true
				i.Target = tgt
				i.HasTgt = true
				return i
			}
		case 1:
			if q == 0 {
				lo, hi := u8(b, off+1), u8(b, off+2)
				n := int(hi)<<8 | int(lo)
				return mk(3, fmt.Sprintf("LD %s,%s", rp[p], hexw(n)))
			}
			return mk(1, fmt.Sprintf("ADD HL,%s", rp[p]))
		case 2:
			switch y {
			case 0:
				return mk(1, "LD (BC),A")
			case 1:
				return mk(1, "LD A,(BC)")
			case 2:
				return mk(1, "LD (DE),A")
			case 3:
				return mk(1, "LD A,(DE)")
			case 4:
				lo, hi := u8(b, off+1), u8(b, off+2)
				n := int(hi)<<8 | int(lo)
				return mk(3, fmt.Sprintf("LD (%s),HL", hexw(n)))
			case 5:
				lo, hi := u8(b, off+1), u8(b, off+2)
				n := int(hi)<<8 | int(lo)
				return mk(3, fmt.Sprintf("LD HL,(%s)", hexw(n)))
			case 6:
				lo, hi := u8(b, off+1), u8(b, off+2)
				n := int(hi)<<8 | int(lo)
				return mk(3, fmt.Sprintf("LD (%s),A", hexw(n)))
			case 7:
				lo, hi := u8(b, off+1), u8(b, off+2)
				n := int(hi)<<8 | int(lo)
				return mk(3, fmt.Sprintf("LD A,(%s)", hexw(n)))
			}
		case 3:
			if q == 0 {
				return mk(1, fmt.Sprintf("INC %s", rp[p]))
			}
			return mk(1, fmt.Sprintf("DEC %s", rp[p]))
		case 4:
			return mk(1, fmt.Sprintf("INC %s", r8[y]))
		case 5:
			return mk(1, fmt.Sprintf("DEC %s", r8[y]))
		case 6:
			n := u8(b, off+1)
			return mk(2, fmt.Sprintf("LD %s,%s", r8[y], hexb(n)))
		case 7:
			names := [8]string{"RLCA", "RRCA", "RLA", "RRA", "DAA", "CPL", "SCF", "CCF"}
			return mk(1, names[y])
		}
	case 1:
		if z == 6 && y == 6 {
			i := mk(1, "HALT")
			i.Fallthrough = false
			return i
		}
		return mk(1, fmt.Sprintf("LD %s,%s", r8[y], r8[z]))
	case 2:
		return mk(1, alu[y]+r8[z])
	case 3:
		switch z {
		case 0:
			i := mk(1, fmt.Sprintf("RET %s", cc[y]))
			i.IsRet = true
			return i
		case 1:
			if q == 0 {
				return mk(1, fmt.Sprintf("POP %s", rp2[p]))
			}
			switch p {
			case 0:
				i := mk(1, "RET")
				i.IsRet = true
				i.Fallthrough = false
				return i
			case 1:
				return mk(1, "EXX")
			case 2:
				i := mk(1, "JP (HL)")
				i.Fallthrough = false
				return i
			case 3:
				return mk(1, "LD SP,HL")
			}
		case 2:
			lo, hi := u8(b, off+1), u8(b, off+2)
			n := int(hi)<<8 | int(lo)
			i := mk(3, fmt.Sprintf("JP %s,%s", cc[y], label(n)))
			i.IsJump = true
			i.Target = n
			i.HasTgt = true
			return i
		case 3:
			switch y {
			case 0:
				lo, hi := u8(b, off+1), u8(b, off+2)
				n := int(hi)<<8 | int(lo)
				i := mk(3, fmt.Sprintf("JP %s", label(n)))
				i.IsJump = true
				i.Target = n
				i.HasTgt = true
				i.Fallthrough = false
				return i
			case 1:
				return decodeCB(b, off+1, addr, "", 0)
			case 2:
				n := u8(b, off+1)
				return mk(2, fmt.Sprintf("OUT (%s),A", hexb(n)))
			case 3:
				n := u8(b, off+1)
				return mk(2, fmt.Sprintf("IN A,(%s)", hexb(n)))
			case 4:
				return mk(1, "EX (SP),HL")
			case 5:
				return mk(1, "EX DE,HL")
			case 6:
				return mk(1, "DI")
			case 7:
				return mk(1, "EI")
			}
		case 4:
			lo, hi := u8(b, off+1), u8(b, off+2)
			n := int(hi)<<8 | int(lo)
			i := mk(3, fmt.Sprintf("CALL %s,%s", cc[y], label(n)))
			i.IsCall = true
			i.Target = n
			i.HasTgt = true
			return i
		case 5:
			if q == 0 {
				return mk(1, fmt.Sprintf("PUSH %s", rp2[p]))
			}
			lo, hi := u8(b, off+1), u8(b, off+2)
			n := int(hi)<<8 | int(lo)
			i := mk(3, fmt.Sprintf("CALL %s", label(n)))
			i.IsCall = true
			i.Target = n
			i.HasTgt = true
			return i
		case 6:
			n := u8(b, off+1)
			return mk(2, alu[y]+hexb(n))
		case 7:
			i := mk(1, fmt.Sprintf("RST %02Xh", y*8))
			i.IsCall = true
			i.Target = int(y) * 8
			i.HasTgt = true
			return i
		}
	}
	return mk(1, fmt.Sprintf("DB %02Xh ; unknown opcode", op))
}

func decodeCB(b []byte, off int, addr int, idx string, disp int) Instr {
	// off points at the opcode following the CB byte (or DDCB/FDCB disp byte).
	op := u8(b, off)
	y := (op >> 3) & 7
	z := op & 7
	x := op >> 6

	reg := r8[z]
	length := 2 // CB + opcode
	prefixLen := 0
	if idx != "" {
		// DD/FD CB disp op : total bytes from the DD/FD = 4; here off is opcode byte itself,
		// caller handles addr/len bookkeeping via decodeIndexed. We just build text.
		reg = fmt.Sprintf("(%s%s)", idx, dispStr(disp))
		length = 1
		prefixLen = 0
		_ = prefixLen
	}

	var text string
	switch x {
	case 0:
		text = fmt.Sprintf("%s %s", rot[y], reg)
	case 1:
		text = fmt.Sprintf("BIT %d,%s", y, reg)
	case 2:
		text = fmt.Sprintf("RES %d,%s", y, reg)
	case 3:
		text = fmt.Sprintf("SET %d,%s", y, reg)
	}
	bytes := []byte{b[off]}
	if idx == "" {
		bytes = []byte{u8(b, off-1), b[off]}
	}
	return Instr{Addr: addr, Len: length, Text: text, Bytes: bytes, Fallthrough: true}
}

func decodeED(b []byte, off int, addr int) Instr {
	start := off
	op1 := u8(b, off+1)
	mk := func(l int, text string) Instr {
		return Instr{Addr: addr, Len: l, Text: text, Bytes: append([]byte{}, b[start:min(start+l, len(b))]...), Fallthrough: true}
	}
	x := op1 >> 6
	y := (op1 >> 3) & 7
	z := op1 & 7
	p := y >> 1
	q := y & 1

	if x == 1 {
		switch z {
		case 0:
			if y == 6 {
				return mk(2, "IN (C)")
			}
			return mk(2, fmt.Sprintf("IN %s,(C)", r8[y]))
		case 1:
			if y == 6 {
				return mk(2, "OUT (C),0")
			}
			return mk(2, fmt.Sprintf("OUT (C),%s", r8[y]))
		case 2:
			if q == 0 {
				return mk(2, fmt.Sprintf("SBC HL,%s", rp[p]))
			}
			return mk(2, fmt.Sprintf("ADC HL,%s", rp[p]))
		case 3:
			lo, hi := u8(b, off+2), u8(b, off+3)
			n := int(hi)<<8 | int(lo)
			if q == 0 {
				return mk(4, fmt.Sprintf("LD (%s),%s", hexw(n), rp[p]))
			}
			return mk(4, fmt.Sprintf("LD %s,(%s)", rp[p], hexw(n)))
		case 4:
			return mk(2, "NEG")
		case 5:
			if y == 1 {
				i := mk(2, "RETI")
				i.IsRet = true
				i.Fallthrough = false
				return i
			}
			i := mk(2, "RETN")
			i.IsRet = true
			i.Fallthrough = false
			return i
		case 6:
			modes := [8]string{"0", "0/1", "1", "2", "0", "0/1", "1", "2"}
			return mk(2, "IM "+modes[y])
		case 7:
			names := [8]string{"LD I,A", "LD R,A", "LD A,I", "LD A,R", "RRD", "RLD", "NOP", "NOP"}
			return mk(2, names[y])
		}
	}
	if x == 2 {
		names := map[byte]string{
			0xA0: "LDI", 0xA1: "CPI", 0xA2: "INI", 0xA3: "OUTI",
			0xA8: "LDD", 0xA9: "CPD", 0xAA: "IND", 0xAB: "OUTD",
			0xB0: "LDIR", 0xB1: "CPIR", 0xB2: "INIR", 0xB3: "OTIR",
			0xB8: "LDDR", 0xB9: "CPDR", 0xBA: "INDR", 0xBB: "OTDR",
		}
		if n, ok := names[op1]; ok {
			return mk(2, n)
		}
	}
	return mk(2, fmt.Sprintf("DB EDh,%02Xh ; unknown ED opcode", op1))
}

func decodeIndexed(b []byte, off int, addr int, idx string) Instr {
	start := off
	op2 := u8(b, off+1)
	mk := func(l int, text string) Instr {
		return Instr{Addr: addr, Len: l, Text: text, Bytes: append([]byte{}, b[start:min(start+l, len(b))]...), Fallthrough: true}
	}

	if op2 == 0xCB {
		d := i8(u8(b, off+2))
		inner := decodeCB(b, off+3, addr, idx, d)
		return Instr{Addr: addr, Len: 4, Text: inner.Text, Bytes: append([]byte{}, b[start:min(start+4, len(b))]...), Fallthrough: true}
	}

	ir := idx // "IX" or "IY"

	switch op2 {
	case 0x21:
		lo, hi := u8(b, off+2), u8(b, off+3)
		n := int(hi)<<8 | int(lo)
		return mk(4, fmt.Sprintf("LD %s,%s", ir, hexw(n)))
	case 0x22:
		lo, hi := u8(b, off+2), u8(b, off+3)
		n := int(hi)<<8 | int(lo)
		return mk(4, fmt.Sprintf("LD (%s),%s", hexw(n), ir))
	case 0x2A:
		lo, hi := u8(b, off+2), u8(b, off+3)
		n := int(hi)<<8 | int(lo)
		return mk(4, fmt.Sprintf("LD %s,(%s)", ir, hexw(n)))
	case 0x23:
		return mk(2, fmt.Sprintf("INC %s", ir))
	case 0x2B:
		return mk(2, fmt.Sprintf("DEC %s", ir))
	case 0x34:
		d := i8(u8(b, off+2))
		return mk(3, fmt.Sprintf("INC (%s%s)", ir, dispStr(d)))
	case 0x35:
		d := i8(u8(b, off+2))
		return mk(3, fmt.Sprintf("DEC (%s%s)", ir, dispStr(d)))
	case 0x36:
		d := i8(u8(b, off+2))
		n := u8(b, off+3)
		return mk(4, fmt.Sprintf("LD (%s%s),%s", ir, dispStr(d), hexb(n)))
	case 0x39:
		return mk(2, fmt.Sprintf("ADD %s,SP", ir))
	case 0xE1:
		return mk(2, fmt.Sprintf("POP %s", ir))
	case 0xE5:
		return mk(2, fmt.Sprintf("PUSH %s", ir))
	case 0xE9:
		i := mk(2, fmt.Sprintf("JP (%s)", ir))
		i.Fallthrough = false
		return i
	case 0xF9:
		return mk(2, fmt.Sprintf("LD SP,%s", ir))
	}

	// ADD ix,rp  (09,19,29,39 handled above for SP; 09/19/29 generic)
	if op2 == 0x09 || op2 == 0x19 || op2 == 0x29 {
		names := map[byte]string{0x09: "BC", 0x19: "DE", 0x29: ir}
		return mk(2, fmt.Sprintf("ADD %s,%s", ir, names[op2]))
	}

	// LD r,(ix+d) / LD (ix+d),r / INC/DEC r via (ix+d) covered by general r8 table when z/y == 6
	x := op2 >> 6
	y := (op2 >> 3) & 7
	z := op2 & 7

	usesDisp := (x == 1 && (y == 6 || z == 6)) // LD involving (HL)->(idx+d)
	if x == 0 && z == 4 && y != 6 && (op2&0xC7) == 0x04 {
		// INC r  where r could be IXH/IXL (undocumented) - skip, rare
	}

	if x == 1 && y == 6 && z == 6 {
		i := mk(2, "HALT")
		i.Fallthrough = false
		return i
	}
	if usesDisp {
		d := i8(u8(b, off+2))
		if y == 6 {
			return mk(3, fmt.Sprintf("LD (%s%s),%s", ir, dispStr(d), r8[z]))
		}
		return mk(3, fmt.Sprintf("LD %s,(%s%s)", r8[y], ir, dispStr(d)))
	}
	if x == 2 && z == 6 {
		d := i8(u8(b, off+2))
		return mk(3, alu[y]+fmt.Sprintf("(%s%s)", ir, dispStr(d)))
	}
	if x == 0 && z == 6 && y == 6 {
		// already handled by 0x36 above
	}
	if op2 == 0x34 || op2 == 0x35 {
		// handled above
	}

	// Fallback: treat DD/FD as a no-op prefix and decode following byte normally (rare/undocumented cases).
	inner := Decode(b, off+1, addr+1)
	return Instr{Addr: addr, Len: 1 + inner.Len, Text: inner.Text, Bytes: append([]byte{b[off]}, inner.Bytes...), Fallthrough: inner.Fallthrough, IsCall: inner.IsCall, IsJump: inner.IsJump, IsRet: inner.IsRet, Target: inner.Target, HasTgt: inner.HasTgt}
}

func dispStr(d int) string {
	if d >= 0 {
		return fmt.Sprintf("+%02Xh", d)
	}
	return fmt.Sprintf("-%02Xh", -d)
}

func hexb(v byte) string    { return fmt.Sprintf("%02Xh", v) }
func hexw(v int) string     { return fmt.Sprintf("%04Xh", v) }
func label(addr int) string { return fmt.Sprintf("L%04X", addr) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
