package legacyid_test

import (
	"testing"

	"peekaping/internal/legacyid"

	"github.com/stretchr/testify/assert"
)

// The expected UUIDs were computed outside Go, with
// python3: uuid.uuid5(uuid.uuid5(uuid.NAMESPACE_DNS, "object-id.mongo.peekaping.com"), "<hex>")
func TestFromObjectID(t *testing.T) {
	for objectID, want := range map[string]string{
		"507f1f77bcf86cd799439011": "2ec0f307-6408-57cd-b5a8-08c55d4ab445",
		"000000000000000000000001": "a1ae88b6-6b3b-5055-9c3e-8389eef0fd0c",
		"ffffffffffffffffffffffff": "4299398f-3aed-5134-a05d-9a74f0465fc6",
		// An ObjectID is the same ID in either case, so it maps to the same UUID.
		"507F1F77BCF86CD799439011": "2ec0f307-6408-57cd-b5a8-08c55d4ab445",
	} {
		got, ok := legacyid.FromObjectID(objectID)
		assert.True(t, ok, objectID)
		assert.Equal(t, want, got, objectID)
	}
}

func TestFromObjectIDLeavesOtherIDsAlone(t *testing.T) {
	for _, id := range []string{
		"",
		"2ec0f307-6408-57cd-b5a8-08c55d4ab445", // a UUID
		"507f1f77bcf86cd79943901",              // 23 characters
		"507f1f77bcf86cd7994390112",            // 25 characters
		"507f1f77bcf86cd79943901g",             // not hex
		"push-token",
	} {
		got, ok := legacyid.FromObjectID(id)
		assert.False(t, ok, id)
		assert.Empty(t, got, id)
	}
}
