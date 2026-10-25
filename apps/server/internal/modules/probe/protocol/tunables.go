// Package protocol serves the probe protocol under /probe/v1: what a probe says to the server to
// join, fetch its monitors and report results. The messages themselves are in probeproto, which
// the probe binary shares. This package is the server's half: the bearer authentication of a
// probe's token, the gzip and rate limit middleware, and the handlers.
package protocol

import "time"

// The limits the server works within, which hello hands to the probe
const (
	// PollTimeout is how long an assignments request waits for a change before it answers 304
	PollTimeout = 25 * time.Second
	// FlushInterval is how often a probe sends the results it has buffered
	FlushInterval = 2 * time.Second
	// BatchMax is the most results a probe sends in one request
	BatchMax = 500
	// BufferMax is the most results a probe holds while the server can't be reached; the oldest
	// go first
	BufferMax = 10000
	// StoreConcurrency is how many monitors' results one results request stores at the same time.
	// Storing a result is a few queries, so a batch stored one result after another takes as long as
	// the database is far away, times the size of the batch.
	StoreConcurrency = 16
	// StoreConcurrencyTotal is how many monitors' results all the results requests of one server
	// store at the same time. Each result being stored holds a connection to the database, which
	// allows PostgreSQL's 100 by default, and the probe instances of an install can post together.
	StoreConcurrencyTotal = 64
)

// Timing is how long the server holds a poll. It is provided as PollTimeout, and a test shortens
// it by decorating the value, so that a poll that times out does not take 25 seconds.
type Timing struct {
	PollTimeout time.Duration
}

// DefaultTiming is the timing of a server that runs: hello tells the probes about it
func DefaultTiming() Timing { return Timing{PollTimeout: PollTimeout} }

const (
	// MinProtocolVersion is the oldest protocol version the server speaks. A hello below it is
	// answered 426.
	MinProtocolVersion = 1
	// MaxBody is the most bytes a request body may have once it is decompressed
	MaxBody = 1 << 20
)
