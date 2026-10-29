package limits_test

import (
	"testing"

	"peekaping/internal/limits"
	"peekaping/internal/modules/setting"

	"github.com/stretchr/testify/assert"
)

// Usage reads a workspace's data retention period as the cleanup job does, so a workspace that
// has none stored keeps the same default in both
func TestUsage_TheDefaultRetentionIsTheSettings(t *testing.T) {
	assert.Equal(t, setting.DefaultDataRetentionDays, limits.DefaultRetentionDays)
}
