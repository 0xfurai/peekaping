package bruteforce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_toDomainModel_and_toSQLModel(t *testing.T) {
	now := time.Now()
	locked := now.Add(time.Hour)
	sm := &sqlModel{Key: "k", FailCount: 2, FirstFailAt: now, LockedUntil: &locked}
	m := toDomainModel(sm)
	assert.Equal(t, "k", m.Key)
	assert.Equal(t, 2, m.FailCount)
	assert.Equal(t, now, m.FirstFailAt)
	assert.Equal(t, &locked, m.LockedUntil)

	sm2 := toSQLModel(m)
	assert.Equal(t, sm, sm2)
}
