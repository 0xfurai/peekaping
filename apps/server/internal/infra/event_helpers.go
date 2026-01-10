package infra

import (
	"encoding/json"
	"vigi/internal/modules/events"
)

// UnmarshalEventPayload attempts to unmarshal event payload from json.RawMessage
// This is needed because Redis event bus serializes payloads to JSON
func UnmarshalEventPayload[T any](event events.Event) (*T, bool) {
	// First, try direct type assertion (for local/non-Redis events)
	if payload, ok := event.Payload.(*T); ok {
		return payload, true
	}

	// If that fails, try to unmarshal from json.RawMessage (for Redis events)
	if rawMsg, ok := event.Payload.(json.RawMessage); ok {
		var result T
		if err := json.Unmarshal(rawMsg, &result); err == nil {
			return &result, true
		}
	}

	return nil, false
}

// UnmarshalEventPayloadValue is like UnmarshalEventPayload but returns a value instead of pointer
func UnmarshalEventPayloadValue[T any](event events.Event) (T, bool) {
	var zero T

	// First, try direct type assertion (for local/non-Redis events)
	if payload, ok := event.Payload.(T); ok {
		return payload, true
	}

	// Also try pointer version
	if payload, ok := event.Payload.(*T); ok {
		return *payload, true
	}

	// If that fails, try to unmarshal from json.RawMessage (for Redis events)
	if rawMsg, ok := event.Payload.(json.RawMessage); ok {
		var result T
		if err := json.Unmarshal(rawMsg, &result); err == nil {
			return result, true
		}
	}

	return zero, false
}
