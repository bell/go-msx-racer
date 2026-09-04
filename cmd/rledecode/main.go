// Command rledecode decodes one of this ROM's compressed VRAM-blit streams
// (see internal/rle) starting at a given address, and writes the decoded
// bytes to a file.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"go-msx-racer/internal/rle"
)

func main() {
	romPath := flag.String("rom", "", "path to ROM file")
	startStr := flag.String("start", "", "start address (hex, e.g. 478A)")
	maxOut := flag.Int("max", 4096, "max decoded output bytes (safety bound)")
	out := flag.String("out", "decoded.bin", "output file path")
	flag.Parse()

	data, err := os.ReadFile(*romPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	startAddr, err := strconv.ParseInt(*startStr, 16, 32)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad -start:", err)
		os.Exit(1)
	}

	const baseAddr = 0x4000
	decoded, extra, endAddr := rle.Decode(data, baseAddr, int(startAddr), *maxOut)
	if endAddr < 0 {
		fmt.Fprintf(os.Stderr, "warning: stream did not terminate within -max=%d bytes of output\n", *maxOut)
	} else {
		fmt.Fprintf(os.Stderr, "decoded %d bytes, control stream ends at $%04X\n", len(decoded), endAddr)
	}
	if len(extra) > 0 {
		fmt.Fprintf(os.Stderr, "note: stream retargets its VRAM destination %d time(s) via $80 -- each chunk below needs writing to its OWN address, not concatenated with the first:\n", len(extra))
		for i, c := range extra {
			fmt.Fprintf(os.Stderr, "  chunk %d: dest $%04X, %d bytes: % X\n", i+1, c.Dest, len(c.Data), c.Data)
		}
	}

	if err := os.WriteFile(*out, decoded, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, first destination only -- see stderr if the stream retargets its destination)\n", *out, len(decoded))
}
