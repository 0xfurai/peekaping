package websocket

import "time"

// OnConnection makes the server call fn, with the ID of the socket, first in its connection
// handler, and then wait for delay when it isn't zero, the way a busy server is late with the
// handler. Call it before the first client connects.
func OnConnection(s *Server, delay time.Duration, fn func(id string)) {
	s.onConnection = func(id string) {
		fn(id)
		time.Sleep(delay)
	}
}

// SweepEvery makes the server sweep its open sockets every interval. Call it before the first
// client connects: the sweep starts with the first socket.
func SweepEvery(s *Server, every time.Duration) {
	s.sweepEvery = every
}
