package ingester

import (
	"context"
	"fmt"
	"time"

	"peekaping/internal/modules/queue"
	"peekaping/internal/modules/shared"
)

// queueService ingests results by enqueueing each one as an ingester task, which cluster mode's
// ingester takes from its queue and stores. The task's payload is the Result.
type queueService struct {
	queue queue.Service
	// uniqueKeyPrefix and uniqueTTL dedupe a result's task. Its key is the prefix, the
	// monitor ID, the probe that ran the check and the result's start time, so that two probes'
	// results of one monitor with the same start time are two tasks.
	uniqueKeyPrefix string
	uniqueTTL       time.Duration
}

// NewQueueService ingests the results of checks by enqueueing them for the ingester: cluster
// mode's worker ingests through it
func NewQueueService(q queue.Service) Service {
	// TTL to prevent duplicates during processing
	return &queueService{queue: q, uniqueKeyPrefix: "ingest:", uniqueTTL: time.Second}
}

// NewPushQueueService ingests pushes to push URLs by enqueueing them for the ingester: cluster
// mode's API ingests through it
func NewPushQueueService(q queue.Service) Service {
	// Short TTL for push monitors to allow frequent updates
	return &queueService{queue: q, uniqueKeyPrefix: "ingest:push:", uniqueTTL: 5 * time.Minute}
}

// Ingest enqueues the result for the ingester. It returns once the task is queued, before the
// result is stored.
func (s *queueService) Ingest(ctx context.Context, r *Result) error {
	opts := &queue.EnqueueOptions{
		Queue:     "ingester",
		MaxRetry:  3,
		Timeout:   2 * time.Minute,
		Retention: 1 * time.Hour,
	}

	// Use EnqueueUnique to prevent duplicate ingester tasks
	// The unique key includes monitor ID, probe and start time to ensure each result is processed only once
	probeID := r.ProbeID
	if probeID == "" {
		probeID = shared.LocalProbeID
	}
	uniqueKey := fmt.Sprintf("%s%s:%s:%d", s.uniqueKeyPrefix, r.MonitorID, probeID, r.StartTime.UnixNano())

	_, err := s.queue.EnqueueUnique(ctx, TaskTypeIngester, r, uniqueKey, s.uniqueTTL, opts)
	return err
}
