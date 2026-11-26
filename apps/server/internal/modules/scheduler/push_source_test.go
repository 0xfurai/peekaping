package scheduler

import (
	"context"
	"fmt"
	"testing"

	"peekaping/internal/modules/check/job"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// everyMonitorSource is a Source of monitors of every type, which pages them as the database does
type everyMonitorSource struct {
	monitors []*shared.Monitor
	watching int
	created  func(string)
	updated  func(string)
	deleted  func(string)
	listed   int
}

func (s *everyMonitorSource) ListActive(_ context.Context, page, limit int) ([]*shared.Monitor, error) {
	s.listed++
	from := min(page*limit, len(s.monitors))
	to := min(from+limit, len(s.monitors))
	return s.monitors[from:to], nil
}

func (s *everyMonitorSource) Find(_ context.Context, id string) (*shared.Monitor, error) {
	for _, m := range s.monitors {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, nil
}

func (s *everyMonitorSource) Load(context.Context, string) (*job.Job, error) {
	return &job.Job{}, nil
}

func (s *everyMonitorSource) Watch(created, updated, deleted func(string)) {
	s.watching++
	s.created, s.updated, s.deleted = created, updated, deleted
}

func monitorsOfEveryType(pushes, others int) []*shared.Monitor {
	var monitors []*shared.Monitor
	for i := range max(pushes, others) {
		if i < others {
			monitors = append(monitors, &shared.Monitor{ID: fmt.Sprintf("http-%03d", i), Type: "http", Active: true, Interval: 60})
		}
		if i < pushes {
			monitors = append(monitors, &shared.Monitor{ID: fmt.Sprintf("push-%03d", i), Type: "push", Active: true, Interval: 60})
		}
	}
	return monitors
}

// A scheduler on this source has one job: the check of a push monitor that finds no push in time
func TestPushSource_ListsOnlyThePushMonitorsInPages(t *testing.T) {
	inner := &everyMonitorSource{monitors: monitorsOfEveryType(250, 400)}
	source := NewPushSource(inner)

	var listed []string
	for page := 0; ; page++ {
		monitors, err := source.ListActive(context.Background(), page, pageSize)
		require.NoError(t, err)
		for _, m := range monitors {
			assert.Equal(t, "push", m.Type)
			listed = append(listed, m.ID)
		}
		if len(monitors) < pageSize {
			break
		}
	}

	assert.Len(t, listed, 250, "all the push monitors, across the pages the scheduler reads")
	assert.Equal(t, "push-000", listed[0])
	assert.Equal(t, "push-249", listed[249])
}

func TestPushSource_FindAnswersNilForAMonitorThatIsNotAPushMonitor(t *testing.T) {
	inner := &everyMonitorSource{monitors: monitorsOfEveryType(1, 1)}
	source := NewPushSource(inner)

	push, err := source.Find(context.Background(), "push-000")
	require.NoError(t, err)
	require.NotNil(t, push)

	other, err := source.Find(context.Background(), "http-000")
	require.NoError(t, err)
	assert.Nil(t, other, "an http monitor is a probe's to check")

	gone, err := source.Find(context.Background(), "gone")
	require.NoError(t, err)
	assert.Nil(t, gone)
}

// A replica holds the lock, loses it and gets it back, and a new scheduler watches the source each
// time. The inner source is watched once, and its signals go to the scheduler that watches now.
func TestPushSource_WatchesTheInnerSourceOnceAndSignalsTheSchedulerOfTheTerm(t *testing.T) {
	inner := &everyMonitorSource{}
	source := NewPushSource(inner)
	var first, second []string
	source.Watch(func(id string) { first = append(first, "created "+id) }, func(string) {}, func(id string) { first = append(first, "deleted "+id) })

	inner.created("m1")
	source.StopWatching()
	inner.created("between terms")
	source.Watch(func(id string) { second = append(second, "created "+id) }, func(id string) { second = append(second, "updated "+id) }, func(string) {})
	inner.created("m2")
	inner.updated("m2")
	inner.deleted("m2") // the second term's scheduler has a callback for it, which does nothing

	assert.Equal(t, 1, inner.watching, "the inner source is subscribed to once")
	assert.Equal(t, []string{"created m1"}, first)
	assert.Equal(t, []string{"created m2", "updated m2"}, second)
}
