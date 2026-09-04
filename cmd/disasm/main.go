// Command disasm disassembles the MSX cartridge ROM this project targets.
//
// It performs a recursive-descent (code-flow-following) disassembly starting
// at the ROM's declared entry point, so that embedded data tables (graphics,
// sound, jump tables) which are never reached as code are left undisassembled
// and reported separately as raw data regions.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"go-msx-racer/internal/z80dis"
)

const baseAddr = 0x4000

func main() {
	romPath := flag.String("rom", "", "path to ROM file")
	out := flag.String("out", "", "output listing file (default stdout)")
	flag.Parse()

	if *romPath == "" {
		fmt.Fprintln(os.Stderr, "usage: disasm -rom <file> [-out listing.asm]")
		os.Exit(1)
	}

	data, err := os.ReadFile(*romPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	size := len(data)
	end := baseAddr + size

	entry := int(data[2]) | int(data[3])<<8

	visited := make(map[int]z80dis.Instr) // keyed by instruction start address
	claimed := make([]bool, size)         // byte-level coverage, any offset touched by a confirmed instruction

	isFree := func(a, length int) bool {
		off := a - baseAddr
		if off < 0 || off+length > size {
			return false
		}
		for i := 0; i < length; i++ {
			if claimed[off+i] {
				return false
			}
		}
		return true
	}
	claim := func(a, length int) {
		off := a - baseAddr
		for i := 0; i < length; i++ {
			claimed[off+i] = true
		}
	}

	var queue []int
	seedFrom := func(a int) {
		if a >= baseAddr && a < end {
			queue = append(queue, a)
		}
	}
	seedFrom(entry)
	seedFrom(0x4013) // H.KEYI hook target, installed at runtime by the init routine

	// Known dispatcher trampolines that consume the CALL return address (or a
	// caller-supplied HL) as a jump-table base and never return to the call
	// site: the bytes immediately following such a CALL are jump-table data,
	// not fallthrough code. Discovered by inspection of the listing.
	nonReturning := map[int]bool{
		0x4045: true, // POP HL / add A (doubled) into L, carry to H / read word at (HL) / JP there -- table follows the call site
		0x4047: true, // caller-supplied-HL variant of the same trampoline
	}

	runQueue := func() {
		for len(queue) > 0 {
			a := queue[0]
			queue = queue[1:]
			if a < baseAddr || a >= end {
				continue
			}
			if _, ok := visited[a]; ok {
				continue
			}
			off := a - baseAddr
			ins := z80dis.Decode(data, off, a)
			if ins.Len == 0 {
				ins.Len = 1
			}
			if ins.Addr+ins.Len > end {
				continue
			}
			if !isFree(a, ins.Len) {
				// Collides with already-confirmed code at a different
				// alignment: this flow path is inconsistent with what we
				// already know, so drop it rather than corrupt the listing.
				continue
			}
			if ins.IsCall && ins.HasTgt && nonReturning[ins.Target] {
				ins.Fallthrough = false
			}
			claim(a, ins.Len)
			visited[a] = ins
			if ins.HasTgt && (ins.IsCall || ins.IsJump) {
				queue = append(queue, ins.Target)
			}
			if ins.Fallthrough {
				queue = append(queue, ins.Addr+ins.Len)
			} else if ins.IsCall && ins.HasTgt && nonReturning[ins.Target] {
				// The bytes right after the CALL are a jump table consumed by
				// the trampoline (indexed by some register doubled), not
				// code. Consume consecutive little-endian words as long as
				// each looks like a plausible ROM code address; each is
				// itself a genuine jump target.
				p := ins.Addr + ins.Len
				for isFree(p, 2) {
					word := int(data[p-baseAddr]) | int(data[p-baseAddr+1])<<8
					if word < baseAddr || word >= end {
						break
					}
					claim(p, 2)
					queue = append(queue, word)
					p += 2
				}
			}
		}
	}
	runQueue()

	// Heuristic pass: many jump/call targets are only reachable via computed
	// jumps or hook-table installs that static flow analysis can't see.
	// Scan every byte offset that is NOT already claimed by confirmed code
	// for an embedded little-endian word that looks like a pointer into ROM
	// code space -- this recovers addresses sitting in raw jump/dispatch
	// tables (no CALL/JP opcode needs to precede them at all). Restricting
	// the scan to unclaimed bytes avoids re-scanning already-correct code at
	// the wrong byte alignment, which would otherwise be a rich source of
	// false positives since ~25% of all random 16-bit words happen to fall
	// in this ROM's 0x4000-0x7FFF address range. looksLikeCode additionally
	// requires the tentative run to reach a clean instruction-boundary
	// terminator before it's accepted.
	tried := map[int]bool{}
	for pass := 0; pass < 16; pass++ {
		var candidates []int
		for off := 0; off+1 < size; off++ {
			if claimed[off] {
				continue
			}
			c := int(data[off]) | int(data[off+1])<<8
			if c < baseAddr || c >= end || tried[c] {
				continue
			}
			if _, ok := visited[c]; ok {
				continue
			}
			candidates = append(candidates, c)
		}

		sort.Ints(candidates)
		newCode := false
		for _, c := range candidates {
			if tried[c] {
				continue
			}
			tried[c] = true
			if _, ok := visited[c]; ok {
				continue
			}
			if !claimed[c-baseAddr] && looksLikeCode(data, c, end, claimed) {
				queue = append(queue, c)
				newCode = true
			}
		}
		if newCode {
			runQueue()
		} else {
			break
		}
	}

	var addrs []int
	for a := range visited {
		addrs = append(addrs, a)
	}
	sort.Ints(addrs)

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}

	fmt.Fprintf(w, "; Road Fighter (Konami 1985) disassembly\n; entry point: %04Xh\n\n", entry)

	i := 0
	for i < len(addrs) {
		a := addrs[i]
		ins := visited[a]
		nextCovered := a + ins.Len
		fmt.Fprintf(w, "%04X: %-18s %s\n", a, bytesHex(ins.Bytes), ins.Text)
		i++
		if i < len(addrs) && addrs[i] > nextCovered {
			gapStart := nextCovered
			gapEnd := addrs[i]
			fmt.Fprintf(w, "\n; ---- DATA %04X-%04X (%d bytes) ----\n", gapStart, gapEnd-1, gapEnd-gapStart)
			dumpData(w, data, gapStart, gapEnd)
			fmt.Fprintln(w)
		}
	}
	if len(addrs) > 0 {
		last := addrs[len(addrs)-1]
		lastEnd := last + visited[last].Len
		if lastEnd < end {
			fmt.Fprintf(w, "\n; ---- DATA %04X-%04X (%d bytes) ----\n", lastEnd, end-1, end-lastEnd)
			dumpData(w, data, lastEnd, end)
		}
	}

	fmt.Fprintf(os.Stderr, "disassembled %d instructions, %d bytes covered of %d total\n", len(addrs), coveredBytes(visited), size)
}

// looksLikeCode tentatively decodes a stretch of instructions starting at
// addr and reports whether it looks like genuine Z80 code. Random data
// decoded as Z80 will almost never reach a *clean* terminator (RET/RETI/
// RETN/HALT, or an unconditional JP/JR whose target itself is a plausible
// ROM address) at a natural instruction boundary within a short window
// without ever overlapping already-claimed code, so requiring that is a much
// stronger signal than merely avoiding unknown opcodes.
func looksLikeCode(data []byte, addr, end int, claimed []bool) bool {
	const maxInsns = 40
	a := addr
	o := addr - baseAddr
	for count := 0; count < maxInsns; count++ {
		if o >= len(data) || a >= end {
			return false
		}
		ins := z80dis.Decode(data, o, a)
		if ins.Len == 0 {
			ins.Len = 1
		}
		if len(ins.Text) >= 2 && ins.Text[:2] == "DB" {
			return false
		}
		for i := 0; i < ins.Len; i++ {
			if o+i >= len(claimed) || claimed[o+i] {
				return false
			}
		}
		if !ins.Fallthrough {
			if ins.IsRet {
				return count >= 1
			}
			if ins.IsJump && ins.HasTgt {
				return ins.Target >= baseAddr && ins.Target < end && count >= 1
			}
			return count >= 1 // HALT or JP (HL)/(IX)/(IY): weaker but still accepted
		}
		o += ins.Len
		a += ins.Len
	}
	return false
}

func coveredBytes(v map[int]z80dis.Instr) int {
	n := 0
	for _, ins := range v {
		n += ins.Len
	}
	return n
}

func bytesHex(b []byte) string {
	s := ""
	for _, x := range b {
		s += fmt.Sprintf("%02X ", x)
	}
	return s
}

func dumpData(w *os.File, data []byte, start, end int) {
	off := start - baseAddr
	length := end - start
	for row := 0; row < length; row += 16 {
		lineAddr := start + row
		fmt.Fprintf(w, "%04X: ", lineAddr)
		rowLen := 16
		if row+rowLen > length {
			rowLen = length - row
		}
		ascii := make([]byte, rowLen)
		for c := 0; c < rowLen; c++ {
			bv := data[off+row+c]
			fmt.Fprintf(w, "%02X ", bv)
			if bv >= 0x20 && bv < 0x7F {
				ascii[c] = bv
			} else {
				ascii[c] = '.'
			}
		}
		for c := rowLen; c < 16; c++ {
			fmt.Fprint(w, "   ")
		}
		fmt.Fprintf(w, " %s\n", string(ascii))
	}
}
