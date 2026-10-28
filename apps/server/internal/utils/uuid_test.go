package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLowerUUID_LowersAUUIDAndLeavesAnythingElse(t *testing.T) {
	assert.Equal(t, "0a1b2c3d-0000-4000-8000-00000000abcd", LowerUUID("0A1B2C3D-0000-4000-8000-00000000ABCD"))
	assert.Equal(t, "0a1b2c3d-0000-4000-8000-00000000abcd", LowerUUID("0a1b2c3d-0000-4000-8000-00000000abcd"))
	assert.Equal(t, "Not-A-UUID", LowerUUID("Not-A-UUID"))
	assert.Equal(t, "", LowerUUID(""))
}

func TestLowerUUIDs_KeepsOrderAndNil(t *testing.T) {
	assert.Nil(t, LowerUUIDs(nil))
	assert.Equal(t, []string{}, LowerUUIDs([]string{}))
	assert.Equal(t, []string{"0a1b2c3d-0000-4000-8000-00000000abcd", "x"}, LowerUUIDs([]string{"0A1B2C3D-0000-4000-8000-00000000ABCD", "x"}))
}
