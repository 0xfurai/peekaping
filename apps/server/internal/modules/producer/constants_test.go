package producer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A leader that renews its lock only after the lock expires loses it between
// renewals, and another producer takes over. A zero interval makes the
// renewal ticker panic when the leader starts.
func TestLeaderRenewsBeforeLeaseExpires(t *testing.T) {
	assert.Greater(t, LeaderRenewalInterval, time.Duration(0))
	assert.Less(t, LeaderRenewalInterval, LeaderTTL)
}
