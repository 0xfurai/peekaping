package probe

import "context"

// SweepOnce runs one pass of the sweep, which Run does every interval
func (s *Sweep) SweepOnce(ctx context.Context) error { return s.sweepOnce(ctx) }
