package auth

import "peekaping/internal/modules/events"

// SessionsEndedEvent is the payload of SessionsEnded.
type SessionsEndedEvent struct {
	UserID string `json:"user_id"`
}

// SessionsEnded is published once every session of a user has ended and the change is stored: an
// instance admin disabled or deleted the user, or a password reset ended their sessions. The
// tokens already stop working at the API; the websocket closes the user's open sockets, which a
// token no longer reaches. It belongs to no monitor, so it is handled with no order relative to
// other events.
//
// The name is the topic's name on the wire, so keep it as it is.
var SessionsEnded = events.NewTopic("auth.sessions.ended", func(SessionsEndedEvent) string { return "" })
