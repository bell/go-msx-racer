// Package psg is a simplified real-time audio synthesizer for the AY-3-8910
// -compatible PSG (Programmable Sound Generator) used by MSX. It is not a
// cycle-accurate emulator -- it recomputes each channel's frequency from the
// current register values once per output sample and phase-accumulates a
// square wave (and a simple randomized noise source) at the output sample
// rate, rather than simulating the chip's own ~1.79MHz internal clock. That
// is accurate enough to correctly hear tones, noise, and the mixer's
// enable/disable bits -- which is this package's job -- without the
// complexity of a full chip simulation.
//
// Register layout (16 registers, matching real AY-3-8910/MSX PSG hardware):
//
//	R0/R1   Channel A tone period (12 bits: R0 fine, R1 bits0-3 coarse)
//	R2/R3   Channel B tone period
//	R4/R5   Channel C tone period
//	R6      Noise period (5 bits)
//	R7      Mixer: bit0-2 = tone A/B/C disable (1=disabled, 0=enabled),
//	        bit3-5 = noise A/B/C disable, bit6-7 = I/O port direction (ignored)
//	R8/R9/R10  Channel A/B/C volume (bits0-3 = 0-15, bit4 = use envelope)
//	R11/R12 Envelope period (16 bits)
//	R13     Envelope shape
//	R14/R15 I/O ports (ignored)
//
// Tone frequency formula (from the AY-3-8910 datasheet): f = clock/(16*period).
package psg

import (
	"math/rand"
	"sync"
	"time"
)

// ClockHz is the standard AY-3-8910/MSX PSG clock frequency.
const ClockHz = 1789772.5

// tickBatch is one game logic tick's worth of register state, queued by
// Tick and consumed sample-by-sample by Read. See Tick's and Read's doc
// comments for why ticks, not wall-clock time, gate how far Read is allowed
// to advance.
type tickBatch struct {
	reg     [16]byte
	samples int
}

// Chip is a PSG register file plus a real-time audio sample generator. Safe
// for concurrent use: WriteRegister/Tick are expected to be called from
// game logic, Read from an audio-mixing goroutine (as required by
// github.com/hajimehoshi/ebiten/v2/audio.Player, which this is designed to
// back).
type Chip struct {
	mu   sync.Mutex
	reg  [16]byte // latest requested register values, for Registers()/UI and as the source Tick snapshots from
	tick []tickBatch

	sampleRate int
	everTicked bool       // true once Tick has been called at least once -- see Read's startup wait
	curBatch   tickBatch  // the batch Read is currently draining -- touched only from Read's goroutine
	tonePhase  [3]float64 // 0..1, one square-wave cycle
	noisePhase float64
	noiseVal   float64 // current noise output, -1 or +1
	rng        *rand.Rand
}

// NewChip creates a Chip generating audio at sampleRate Hz (e.g. 44100).
func NewChip(sampleRate int) *Chip {
	return &Chip{
		sampleRate: sampleRate,
		rng:        rand.New(rand.NewSource(1)),
	}
}

// WriteRegister sets one PSG register. Matches the (register, value byte)
// signature of internal/msx.System.WritePSGRegister, so a *Chip (or a small
// adapter around one) can back that interface directly. Only takes effect
// in the generated audio once the caller's next Tick call closes out this
// batch of writes -- see Tick's doc comment.
func (c *Chip) WriteRegister(register, value byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if int(register) >= len(c.reg) {
		return
	}
	c.reg[register] = value
}

// Tick closes out every WriteRegister call made since the last Tick call
// into one queued batch, snapshotting the current register state and
// recording that it should be held for `samples` output samples once Read
// gets to it (typically sampleRate/ticksPerSecond -- e.g. 44100/60=735 for
// a 60Hz Ebitengine Update loop). The caller should call this once per
// logic tick, after making that tick's WriteRegister calls.
//
// This -- not wall-clock time -- is what governs how far Read is allowed to
// advance. An earlier version timestamped each write with wall-clock time
// and had Read apply whichever queued writes had "already happened" by the
// time it generated each sample. That is correct only if Read is asked for
// audio at roughly the same pace game logic produces it, which turned out
// to be false: ebitengine's audio backend (oto/v3, via its Mux)
// reads up to ~0.5 SECOND of audio ahead of actual playback in a burst
// (confirmed in oto's own source, internal/mux.Mux.defaultBufferSize).
// Once Read's own running sample count had raced that far ahead of
// wall-clock time, every subsequent wall-clock-timestamped write looked
// "already in the past" relative to it, so a whole burst of distinct game
// notes collapsed into whichever one happened to be last -- exactly
// reproducing the original "same sound id sounds different each time"
// symptom despite the timestamp-based fix, since the register writes
// themselves were already confirmed byte-for-byte deterministic (see
// docs/ANALYSIS.md). Gating on an explicit tick count instead means Read
// can never advance past what game logic has actually authorized: if asked
// for more samples than have been ticked yet, it holds the last ticked
// register state (see Read) rather than fabricating a position from a
// clock that the audio backend's own buffering had already invalidated.
func (c *Chip) Tick(samples int) {
	if samples <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.everTicked = true
	c.tick = append(c.tick, tickBatch{reg: c.reg, samples: samples})
}

// Registers returns a snapshot of the latest requested register values, for
// a UI to display -- may be slightly ahead of what Read is currently
// rendering (the most recently ticked batch), which is fine for display.
func (c *Chip) Registers() [16]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reg
}

func tonePeriod(fine, coarse byte) int {
	p := int(fine) | int(coarse&0x0F)<<8
	if p == 0 {
		p = 1 // datasheet: a period of 0 is treated as 1
	}
	return p
}

// Read implements io.Reader, generating 16-bit little-endian stereo PCM
// (the format github.com/hajimehoshi/ebiten/v2/audio requires). Never
// returns io.EOF -- it's a live synthesizer, not a fixed-length clip, so an
// audio.Player given this Read method plays indefinitely.
//
// Each output sample is generated from whichever ticked register batch
// (see Tick) is current, advancing to the next queued batch once the
// current one's sample count is exhausted. If the queue runs dry -- Read
// has been asked for more audio than game logic has ticked yet, which
// happens routinely given oto/v3's ~0.5s read-ahead buffering (see Tick's
// doc comment) -- Read holds curBatch's register state for the rest of the
// request rather than fabricating a position along some other clock. Two
// earlier approaches (a single register snapshot per Read call, then a
// wall-clock-timestamped write queue) both let audio generation race ahead
// of what the game had actually decided should be playing; ticking is
// immune to that by construction, since Read can never consume a batch
// that hasn't been queued yet.
//
// One gap that same design left, found from a real playback lag report
// (audio noticeably behind the picture, by roughly as long as oto/v3's own
// read-ahead): the very first Read call(s) after a player starts, before
// game logic has ever called Tick even once. oto's Mux immediately tries
// to fill its ~0.5s read-ahead buffer, calling Read far faster than a
// freshly-started game can tick real audio state -- and the code above,
// finding an empty queue and a zero-value curBatch, would happily generate
// up to half a second of real silence and hand it straight to oto. That
// silence gets baked into the stream at its very start, at the exact
// sample positions real playback will reach first -- so the game's actual
// first ~0.5s of ticked audio doesn't get dropped, but it does get pushed
// later in the stream than it was generated, and playback lags behind the
// picture by that same amount for the rest of the session (nothing ever
// un-does an already-written silent prefix). Ticking eventually catches
// up in the sense that every batch is still played in order -- it just
// starts from an offset instead of from zero.
//
// Fixed by treating "no batch queued AND Tick has never been called even
// once" as a real startup condition to wait out, not steady-state
// underrun to paper over: block (briefly polling, since a full
// sync.Cond is overkill for a wait that's normally under one frame long)
// until the game's first Tick call lands, instead of manufacturing
// silence for it. Once everTicked is true, an empty queue goes back to
// the original hold-last-state behavior, unchanged -- that case is
// harmless (steady-state underrun of a sample or two is inaudible) and
// isn't what caused the reported lag.
func (c *Chip) Read(p []byte) (int, error) {
	const bytesPerFrame = 4 // 2 channels * 16-bit
	n := len(p) / bytesPerFrame

	for i := 0; i < n; i++ {
		if c.curBatch.samples == 0 {
			for {
				c.mu.Lock()
				if len(c.tick) > 0 {
					c.curBatch = c.tick[0]
					c.tick = c.tick[1:]
					c.mu.Unlock()
					break
				}
				waiting := !c.everTicked
				c.mu.Unlock()
				if !waiting {
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
		if c.curBatch.samples > 0 {
			c.curBatch.samples--
		}

		reg := c.curBatch.reg
		mixer := reg[7]
		periods := [3]int{
			tonePeriod(reg[0], reg[1]),
			tonePeriod(reg[2], reg[3]),
			tonePeriod(reg[4], reg[5]),
		}
		volumes := [3]byte{reg[8], reg[9], reg[10]}
		noisePeriod := int(reg[6] & 0x1F)
		if noisePeriod == 0 {
			noisePeriod = 1
		}
		noiseFreq := ClockHz / (16 * float64(noisePeriod))

		// Advance the noise generator, re-randomizing its polarity each
		// time its period elapses (a simple stand-in for the AY's LFSR --
		// not bit-exact, but correctly noise-like and correctly timed).
		c.noisePhase += noiseFreq / float64(c.sampleRate)
		for c.noisePhase >= 1 {
			c.noisePhase -= 1
			if c.rng.Float64() < 0.5 {
				c.noiseVal = 1
			} else {
				c.noiseVal = -1
			}
		}

		var mix float64
		for ch := 0; ch < 3; ch++ {
			toneEnabled := mixer&(1<<uint(ch)) == 0
			noiseEnabled := mixer&(1<<uint(ch+3)) == 0

			freq := ClockHz / (16 * float64(periods[ch]))
			c.tonePhase[ch] += freq / float64(c.sampleRate)
			for c.tonePhase[ch] >= 1 {
				c.tonePhase[ch] -= 1
			}
			toneVal := 1.0
			if c.tonePhase[ch] >= 0.5 {
				toneVal = -1.0
			}

			var channelOut float64
			switch {
			case toneEnabled && noiseEnabled:
				channelOut = toneVal * c.noiseVal
			case toneEnabled:
				channelOut = toneVal
			case noiseEnabled:
				channelOut = c.noiseVal
			}

			var volFrac float64
			if volumes[ch]&0x10 != 0 {
				// Envelope mode: approximate with a fixed, reasonably
				// audible level rather than implementing the 3-bit
				// envelope-shape generator (R11-R13) -- out of scope for
				// a register-poke preview tool.
				volFrac = 0.6
			} else {
				volFrac = float64(volumes[ch]&0x0F) / 15.0
			}
			mix += channelOut * volFrac
		}

		sample := mix / 3.0 // average the 3 channels down to headroom for +-1
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}
		s16 := int16(sample * 0.5 * 32767) // extra 0.5 headroom, still audible

		off := i * bytesPerFrame
		p[off] = byte(s16)
		p[off+1] = byte(s16 >> 8)
		p[off+2] = byte(s16)
		p[off+3] = byte(s16 >> 8)
	}

	return n * bytesPerFrame, nil
}
