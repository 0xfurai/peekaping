package probeclient

import (
	"crypto/rand"
	"sync"
	"time"
)

// ulidAlphabet is Crockford's base32, which ULIDs are written in
const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ulidGenerator makes ULIDs: 26 characters that sort by the time they were made. The first 10
// encode the time in milliseconds, the other 16 are 80 random bits. Two made in the same
// millisecond keep their order, as the ULID specification asks: the second takes the first's
// random bits plus one.
type ulidGenerator struct {
	mu      sync.Mutex
	lastMs  uint64
	entropy [10]byte
}

// next makes a ULID for the instant now
func (g *ulidGenerator) next(now time.Time) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	ms := uint64(now.UnixMilli())
	if ms > g.lastMs {
		g.lastMs = ms
		g.randomize()
	} else if !g.increment() {
		// The 80 bits are used up, which takes 2^80 ids in one millisecond: go on to the next one
		g.lastMs++
		g.randomize()
	}

	var id [26]byte
	t := g.lastMs
	for i := 9; i >= 0; i-- {
		id[i] = ulidAlphabet[t&0x1f]
		t >>= 5
	}
	var acc uint64
	var bits uint
	at := 10
	for _, b := range g.entropy {
		acc = acc<<8 | uint64(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			id[at] = ulidAlphabet[(acc>>bits)&0x1f]
			at++
		}
		acc &= 1<<bits - 1
	}
	return string(id[:])
}

func (g *ulidGenerator) randomize() {
	if _, err := rand.Read(g.entropy[:]); err != nil {
		// crypto/rand does not fail on the platforms Go runs on; a ULID that is only the time and
		// a counter is still unique within this process, and the server dedupes on id per probe
		g.entropy = [10]byte{}
	}
}

// increment adds one to the random bits, and reports false when they overflow
func (g *ulidGenerator) increment() bool {
	for i := len(g.entropy) - 1; i >= 0; i-- {
		g.entropy[i]++
		if g.entropy[i] != 0 {
			return true
		}
	}
	return false
}
