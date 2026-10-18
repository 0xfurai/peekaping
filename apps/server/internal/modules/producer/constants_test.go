package producer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A leader that renews its lock only after the lock expires loses it between
// renewals, and another producer takes over.
func TestLeaderRenewsBeforeLeaseExpires(t *testing.T) {
	assert.Less(t, LeaderRenewalInterval, LeaderTTL)
}
