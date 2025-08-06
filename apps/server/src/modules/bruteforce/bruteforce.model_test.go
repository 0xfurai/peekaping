package bruteforce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_toDomainModelFromMongo_and_toMongoModel(t *testing.T) {
	now := time.Now()
	locked := now.Add(time.Hour)
	mm := &mongoModel{Key: "k", FailCount: 1, FirstFailAt: now, LockedUntil: &locked}
	m := toDomainModelFromMongo(mm)
	assert.Equal(t, "k", m.Key)
	assert.Equal(t, 1, m.FailCount)
	assert.Equal(t, now, m.FirstFailAt)
	assert.Equal(t, &locked, m.LockedUntil)

	mm2 := toMongoModel(m)
	assert.Equal(t, mm, mm2)
}

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

func TestModel_UpdateModel(t *testing.T) {
	// Test Model struct
	now := time.Now()
	locked := now.Add(time.Hour)
	m := &Model{
		Key:         "test_key",
		FailCount:   5,
		FirstFailAt: now,
		LockedUntil: &locked,
	}
	assert.Equal(t, "test_key", m.Key)
	assert.Equal(t, 5, m.FailCount)
	assert.Equal(t, now, m.FirstFailAt)
	assert.Equal(t, &locked, m.LockedUntil)

	// Test UpdateModel struct
	failCount := 3
	updateTime := now.Add(time.Minute)
	updateLocked := now.Add(2 * time.Hour)
	um := &UpdateModel{
		FailCount:   &failCount,
		FirstFailAt: &updateTime,
		LockedUntil: &updateLocked,
	}
	assert.Equal(t, &failCount, um.FailCount)
	assert.Equal(t, &updateTime, um.FirstFailAt)
	assert.Equal(t, &updateLocked, um.LockedUntil)
}
