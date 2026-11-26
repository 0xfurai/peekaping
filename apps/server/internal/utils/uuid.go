package utils

import "github.com/google/uuid"

// LowerUUID is the lower case form of an ID that is a UUID, and the ID as it is otherwise. A
// database compares UUIDs in any letter case but SQLite compares text, so a record that names
// another by an upper case copy of its ID is stored with the lower case one, as PostgreSQL does.
func LowerUUID(id string) string {
	if parsed, err := uuid.Parse(id); err == nil {
		return parsed.String()
	}
	return id
}

// LowerUUIDs is LowerUUID for each ID, in order. A nil list stays nil.
func LowerUUIDs(ids []string) []string {
	if ids == nil {
		return nil
	}
	lowered := make([]string, len(ids))
	for i, id := range ids {
		lowered[i] = LowerUUID(id)
	}
	return lowered
}
