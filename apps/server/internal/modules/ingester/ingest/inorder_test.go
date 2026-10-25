package ingest

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// monitorID is the monitor the tests that need only one ingest results for
const monitorID = "mon-1"

// serviceFunc is a Service made of a function
type serviceFunc func(ctx context.Context, r *Result) error

func (f serviceFunc) Ingest(ctx context.Context, r *Result) error { return f(ctx, r) }

// submissionGap is long enough for a goroutine started before another to be waiting for its turn
const submissionGap = 30 * time.Millisecond

func TestInOrderService_IngestsOneMonitorsResultsOneAtATimeInSubmissionOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	running, mostRunning := 0, 0
	release := make(chan struct{})
	svc := NewInOrderService(serviceFunc(func(_ context.Context, r *Result) error {
		mu.Lock()
		running++
		mostRunning = max(mostRunning, running)
		order = append(order, r.Message)
		mu.Unlock()

		if r.Message == "0" {
			<-release
		} else {
			time.Sleep(2 * time.Millisecond)
		}

		mu.Lock()
		running--
		mu.Unlock()
		return nil
	}))

	var wg sync.WaitGroup
	for i := range 5 {
		wg.Go(func() {
			assert.NoError(t, svc.Ingest(context.Background(), &Result{MonitorID: monitorID, Message: strconv.Itoa(i)}))
		})
		time.Sleep(submissionGap)
	}
	close(release)
	wg.Wait()

	assert.Equal(t, []string{"0", "1", "2", "3", "4"}, order)
	assert.Equal(t, 1, mostRunning)
}

func TestInOrderService_IngestsDifferentMonitorsAtTheSameTime(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	svc := NewInOrderService(serviceFunc(func(_ context.Context, r *Result) error {
		started <- r.MonitorID
		<-release
		return nil
	}))

	var wg sync.WaitGroup
	for _, id := range []string{"mon-a", "mon-b"} {
		wg.Go(func() {
			assert.NoError(t, svc.Ingest(context.Background(), &Result{MonitorID: id}))
		})
	}

	got := []string{<-started, <-started}
	close(release)
	wg.Wait()
	assert.ElementsMatch(t, []string{"mon-a", "mon-b"}, got)
}

func TestInOrderService_ForgetsAMonitorOnceItsIngestsAreDone(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 3)
	svc := NewInOrderService(serviceFunc(func(_ context.Context, _ *Result) error {
		entered <- struct{}{}
		<-release
		return nil
	})).(*inOrderService)

	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			assert.NoError(t, svc.Ingest(context.Background(), &Result{MonitorID: monitorID}))
		})
		time.Sleep(submissionGap)
	}
	svc.mu.Lock()
	assert.Len(t, svc.monitors, 1, "one entry for the monitor with a running and two waiting ingests")
	svc.mu.Unlock()

	// Let the three through, one after the other
	for range 3 {
		<-entered
		release <- struct{}{}
	}
	wg.Wait()

	svc.mu.Lock()
	defer svc.mu.Unlock()
	assert.Empty(t, svc.monitors)
}

func TestInOrderService_ForgetsEveryMonitorItIngested(t *testing.T) {
	svc := NewInOrderService(serviceFunc(func(context.Context, *Result) error { return nil })).(*inOrderService)

	for i := range 100 {
		require.NoError(t, svc.Ingest(context.Background(), &Result{MonitorID: strconv.Itoa(i)}))
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()
	assert.Empty(t, svc.monitors)
}

func TestInOrderService_ANextIngestStillRunsAfterAFailedOne(t *testing.T) {
	failure := errors.New("db down")
	calls := 0
	svc := NewInOrderService(serviceFunc(func(context.Context, *Result) error {
		calls++
		if calls == 1 {
			return failure
		}
		return nil
	}))

	assert.ErrorIs(t, svc.Ingest(context.Background(), &Result{MonitorID: monitorID}), failure)
	assert.NoError(t, svc.Ingest(context.Background(), &Result{MonitorID: monitorID}))
	assert.Equal(t, 2, calls)
}

func TestInOrderService_AnIngestWaitingForItsTurnGivesUpWhenItsContextIsDone(t *testing.T) {
	var mu sync.Mutex
	var ingested []string
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	svc := NewInOrderService(serviceFunc(func(_ context.Context, r *Result) error {
		mu.Lock()
		ingested = append(ingested, r.Message)
		mu.Unlock()
		if r.Message == "running" {
			entered <- struct{}{}
			<-release
		}
		return nil
	})).(*inOrderService)

	var wg sync.WaitGroup
	wg.Go(func() {
		assert.NoError(t, svc.Ingest(context.Background(), &Result{MonitorID: monitorID, Message: "running"}))
	})
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	gaveUp := make(chan error, 1)
	go func() { gaveUp <- svc.Ingest(ctx, &Result{MonitorID: monitorID, Message: "cancelled"}) }()
	time.Sleep(submissionGap)
	wg.Go(func() {
		assert.NoError(t, svc.Ingest(context.Background(), &Result{MonitorID: monitorID, Message: "last"}))
	})
	time.Sleep(submissionGap)

	cancel()
	assert.ErrorIs(t, <-gaveUp, context.Canceled)

	close(release)
	wg.Wait()
	assert.Equal(t, []string{"running", "last"}, ingested, "the cancelled result is never ingested and doesn't block the one behind it")
	svc.mu.Lock()
	defer svc.mu.Unlock()
	assert.Empty(t, svc.monitors)
}

func TestNewInOrderService_DoesNotWrapAnInOrderServiceAgain(t *testing.T) {
	svc := NewInOrderService(serviceFunc(func(context.Context, *Result) error { return nil }))

	assert.Same(t, svc, NewInOrderService(svc))
}
