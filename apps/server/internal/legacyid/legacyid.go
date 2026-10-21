// Package legacyid maps the IDs of a MongoDB install to the UUIDs it gets when
// "bun db import-mongo" copies it into SQLite or PostgreSQL.
//
// Every ID in a MongoDB install is an ObjectID (24 hex characters), and every
// ID in the SQL tables is a UUID. The import maps each ObjectID to a UUID with
// FromObjectID, so it is the same on every run. Some IDs live outside the
// database, in API key tokens and badge URLs. After the import those still
// carry the ObjectID, and the SQL services use FromObjectID to find the row.
package legacyid

import (
	"strings"

	"github.com/google/uuid"
)

// namespace is the UUIDv5 namespace of every mapped ObjectID. It must never
// change: imported rows and the lookups of old tokens and URLs both depend on it.
var namespace = uuid.NewSHA1(uuid.NameSpaceDNS, []byte("object-id.mongo.peekaping.com"))

// FromObjectID returns the UUID that an ObjectID maps to: the UUIDv5 of its
// lower-case hex form. ok is false if objectID isn't 24 hex characters, such as
// a UUID, and then nothing is mapped.
func FromObjectID(objectID string) (id string, ok bool) {
	if !isObjectID(objectID) {
		return "", false
	}
	return uuid.NewSHA1(namespace, []byte(strings.ToLower(objectID))).String(), true
}

func isObjectID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
