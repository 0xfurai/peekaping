package websocket

import "time"

// SlowConnectionHandler makes the server's connection handler wait for d before it does anything,
// the way a busy server is late with it. Call it before the first client connects.
func SlowConnectionHandler(s *Server, d time.Duration) {
	s.onConnection = func() { time.Sleep(d) }
}
