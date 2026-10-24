package ingester

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingQueue is a queue that keeps the unique key of each enqueue
type recordingQueue struct {
	queue.Service
	keys []string
}

func (q *recordingQueue) EnqueueUnique(_ context.Context, _ string, _ interface{}, uniqueKey string, _ time.Duration, _ *queue.EnqueueOptions) (*queue.TaskInfo, error) {
	q.keys = append(q.keys, uniqueKey)
	return &queue.TaskInfo{}, nil
}

func TestQueueService_TheDedupeKeyNamesTheProbe(t *testing.T) {
	startedAt := time.Date(2026, 10, 24, 12, 0, 0, 5, time.UTC)

	for name, newService := range map[string]func(queue.Service) Service{"checks": NewQueueService, "pushes": NewPushQueueService} {
		t.Run(name, func(t *testing.T) {
			q := &recordingQueue{}
			service := newService(q)

			for _, probeID := range []string{probeA, probeB, "", shared.LocalProbeID} {
				require.NoError(t, service.Ingest(context.Background(), &Result{MonitorID: "mon-1", ProbeID: probeID, StartTime: startedAt}))
			}

			prefix := map[string]string{"checks": "ingest:", "pushes": "ingest:push:"}[name]
			assert.Equal(t, []string{
				prefix + "mon-1:probe-a:1792843200000000005",
				prefix + "mon-1:probe-b:1792843200000000005",
				prefix + "mon-1:local:1792843200000000005",
				prefix + "mon-1:local:1792843200000000005",
			}, q.keys, "two probes' results with the same start time don't collide, and a result with no probe is the local probe's")
		})
	}
}
