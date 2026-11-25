package monitor

import (
	"context"
	"testing"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

func slowFloatPtr(v float64) *float64                                { return &v }
func slowModePtr(v shared.SlowResponseMode) *shared.SlowResponseMode { return &v }

func TestSlowResponseValidation_Create(t *testing.T) {
	t.Run("defaults to off with 3 consecutive checks", func(t *testing.T) {
		cfg, err := resolveCreateSlowResponse(&CreateUpdateDto{})
		require.NoError(t, err)
		assert.Equal(t, shared.SlowResponseModeOff, cfg.mode)
		assert.Equal(t, shared.DefaultSlowResponseConsecutive, cfg.consecutive)
		assert.Nil(t, cfg.thresholdMs)
		assert.Nil(t, cfg.multiplier)
		assert.Nil(t, cfg.minMs)
	})

	t.Run("fixed mode requires positive threshold and clears baseline fields", func(t *testing.T) {
		_, err := resolveCreateSlowResponse(&CreateUpdateDto{
			SlowResponseMode: shared.SlowResponseModeFixed,
		})
		require.Error(t, err)
		var invalid *InvalidSlowResponseError
		assert.ErrorAs(t, err, &invalid)

		_, err = resolveCreateSlowResponse(&CreateUpdateDto{
			SlowResponseMode:        shared.SlowResponseModeFixed,
			SlowResponseThresholdMs: intPtr(0),
		})
		require.Error(t, err)

		cfg, err := resolveCreateSlowResponse(&CreateUpdateDto{
			SlowResponseMode:        shared.SlowResponseModeFixed,
			SlowResponseThresholdMs: intPtr(500),
			SlowResponseMultiplier:  slowFloatPtr(2.5),
			SlowResponseMinMs:       intPtr(100),
			SlowResponseConsecutive: intPtr(2),
		})
		require.NoError(t, err)
		assert.Equal(t, shared.SlowResponseModeFixed, cfg.mode)
		require.NotNil(t, cfg.thresholdMs)
		assert.Equal(t, 500, *cfg.thresholdMs)
		assert.Nil(t, cfg.multiplier)
		assert.Nil(t, cfg.minMs)
		assert.Equal(t, 2, cfg.consecutive)
	})

	t.Run("baseline mode requires multiplier > 1 and non-negative min_ms", func(t *testing.T) {
		_, err := resolveCreateSlowResponse(&CreateUpdateDto{
			SlowResponseMode:       shared.SlowResponseModeBaseline,
			SlowResponseMultiplier: slowFloatPtr(1.0),
		})
		require.Error(t, err)

		_, err = resolveCreateSlowResponse(&CreateUpdateDto{
			SlowResponseMode:       shared.SlowResponseModeBaseline,
			SlowResponseMultiplier: slowFloatPtr(2.0),
			SlowResponseMinMs:      intPtr(-1),
		})
		require.Error(t, err)

		cfg, err := resolveCreateSlowResponse(&CreateUpdateDto{
			SlowResponseMode:        shared.SlowResponseModeBaseline,
			SlowResponseThresholdMs: intPtr(500),
			SlowResponseMultiplier:  slowFloatPtr(2.5),
			SlowResponseMinMs:       intPtr(100),
			SlowResponseConsecutive: intPtr(4),
		})
		require.NoError(t, err)
		assert.Equal(t, shared.SlowResponseModeBaseline, cfg.mode)
		assert.Nil(t, cfg.thresholdMs)
		require.NotNil(t, cfg.multiplier)
		assert.InDelta(t, 2.5, *cfg.multiplier, 1e-9)
		require.NotNil(t, cfg.minMs)
		assert.Equal(t, 100, *cfg.minMs)
		assert.Equal(t, 4, cfg.consecutive)
	})

	t.Run("rejects unknown mode and consecutive < 1", func(t *testing.T) {
		_, err := resolveCreateSlowResponse(&CreateUpdateDto{
			SlowResponseMode: "invalid",
		})
		require.Error(t, err)

		_, err = resolveCreateSlowResponse(&CreateUpdateDto{
			SlowResponseMode:        shared.SlowResponseModeOff,
			SlowResponseConsecutive: intPtr(0),
		})
		require.Error(t, err)
	})
}

func TestSlowResponseValidation_Updates(t *testing.T) {
	current := &Model{
		SlowResponseMode:        shared.SlowResponseModeFixed,
		SlowResponseThresholdMs: intPtr(400),
		SlowResponseConsecutive: 2,
	}

	t.Run("full update omitting all slow response fields keeps current config", func(t *testing.T) {
		cfg, err := resolveFullSlowResponse(current, &CreateUpdateDto{})
		require.NoError(t, err)
		assert.Equal(t, shared.SlowResponseModeFixed, cfg.mode)
		require.NotNil(t, cfg.thresholdMs)
		assert.Equal(t, 400, *cfg.thresholdMs)
		assert.Equal(t, 2, cfg.consecutive)
	})

	t.Run("partial update merges fields and clears stale ones on mode switch", func(t *testing.T) {
		_, set, err := resolvePartialSlowResponse(current, &PartialUpdateDto{})
		require.NoError(t, err)
		assert.False(t, set)

		cfg, set, err := resolvePartialSlowResponse(current, &PartialUpdateDto{
			SlowResponseMode:       slowModePtr(shared.SlowResponseModeBaseline),
			SlowResponseMultiplier: slowFloatPtr(3.0),
			SlowResponseMinMs:      intPtr(150),
		})
		require.NoError(t, err)
		assert.True(t, set)
		assert.Equal(t, shared.SlowResponseModeBaseline, cfg.mode)
		assert.Nil(t, cfg.thresholdMs)
		require.NotNil(t, cfg.multiplier)
		assert.InDelta(t, 3.0, *cfg.multiplier, 1e-9)
		require.NotNil(t, cfg.minMs)
		assert.Equal(t, 150, *cfg.minMs)
		assert.Equal(t, 2, cfg.consecutive)
	})
}

func TestSQLRepository_StoresSlowResponseSettings(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()

		m := createTestMonitor("Latency API", true, shared.MonitorStatusPending)
		m.SlowResponseMode = shared.SlowResponseModeFixed
		m.SlowResponseThresholdMs = intPtr(350)
		m.SlowResponseConsecutive = 2

		created, err := repo.Create(ctx, testWorkspace, m)
		require.NoError(t, err)
		assert.Equal(t, shared.SlowResponseModeFixed, created.SlowResponseMode)
		require.NotNil(t, created.SlowResponseThresholdMs)
		assert.Equal(t, 350, *created.SlowResponseThresholdMs)
		assert.Equal(t, 2, created.SlowResponseConsecutive)

		// Partial update switches to baseline mode
		baseline := shared.SlowResponseModeBaseline
		mult := 2.5
		minMs := 120
		consec := 4
		require.NoError(t, repo.UpdatePartial(ctx, testWorkspace, created.ID, &UpdateModel{
			SlowResponseMode:        &baseline,
			SlowResponseThresholdMs: nil,
			SlowResponseMultiplier:  &mult,
			SlowResponseMinMs:       &minMs,
			SlowResponseConsecutive: &consec,
		}))

		found, err := repo.FindByID(ctx, testWorkspace, created.ID)
		require.NoError(t, err)
		assert.Equal(t, shared.SlowResponseModeBaseline, found.SlowResponseMode)
		assert.Nil(t, found.SlowResponseThresholdMs)
		require.NotNil(t, found.SlowResponseMultiplier)
		assert.InDelta(t, 2.5, *found.SlowResponseMultiplier, 1e-9)
		require.NotNil(t, found.SlowResponseMinMs)
		assert.Equal(t, 120, *found.SlowResponseMinMs)
		assert.Equal(t, 4, found.SlowResponseConsecutive)

		// Turning off clears numeric thresholds
		off := shared.SlowResponseModeOff
		require.NoError(t, repo.UpdatePartial(ctx, testWorkspace, created.ID, &UpdateModel{
			SlowResponseMode: &off,
		}))
		found, err = repo.FindByID(ctx, testWorkspace, created.ID)
		require.NoError(t, err)
		assert.Equal(t, shared.SlowResponseModeOff, found.SlowResponseMode)
		assert.Nil(t, found.SlowResponseThresholdMs)
		assert.Nil(t, found.SlowResponseMultiplier)
		assert.Nil(t, found.SlowResponseMinMs)
	})
}
