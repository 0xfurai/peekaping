package events

// Topic names one kind of event and the type of payload it carries. A topic lives next to
// its payload type: here for the types in package shared, in certificate and stale for theirs.
type Topic[T any] struct {
	name      string
	monitorID func(T) string
}

// NewTopic declares a topic. The name is the event's name on the wire (the Redis bus
// publishes to the channel peekaping:events:<name>), so renaming a topic breaks mixed
// versions during a rolling upgrade.
//
// monitorID returns the ID of the monitor a payload is about. A subscriber handles events
// with the same monitor ID in publish order, across all its topics. A nil monitorID, or an
// empty ID, marks an event that belongs to no monitor: it is handled with no order
// relative to other events.
func NewTopic[T any](name string, monitorID func(T) string) Topic[T] {
	return Topic[T]{name: name, monitorID: monitorID}
}

// Name is the topic's name on the wire.
func (t Topic[T]) Name() string { return t.name }

// key is the monitor ID that orders payload, empty for none.
func (t Topic[T]) key(payload T) string {
	if t.monitorID == nil {
		return ""
	}
	return t.monitorID(payload)
}
