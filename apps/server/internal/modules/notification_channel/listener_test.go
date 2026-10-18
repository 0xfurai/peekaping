package notification_channel

import (
	"context"
	"errors"
	"testing"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/tag"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type fakeMonitorTagService struct {
	monitor_tag.Service
	rels []*monitor_tag.Model
	err  error
}

func (f *fakeMonitorTagService) FindByMonitorID(ctx context.Context, monitorID string) ([]*monitor_tag.Model, error) {
	return f.rels, f.err
}

type fakeTagService struct {
	tag.Service
	tags map[string]*tag.Model
	err  error
}

func (f *fakeTagService) FindByID(ctx context.Context, id string) (*tag.Model, error) {
	return f.tags[id], f.err
}

func newTagTestListener(monitorTags *fakeMonitorTagService, tags *fakeTagService) *NotificationEventListener {
	return &NotificationEventListener{
		monitorTagService: monitorTags,
		tagService:        tags,
		logger:            zap.NewNop().Sugar(),
	}
}

func TestLoadMonitorTags(t *testing.T) {
	tags := &fakeTagService{tags: map[string]*tag.Model{
		"t1": {ID: "t1", Name: "severity:critical"},
		"t2": {ID: "t2", Name: "country:ua"},
		"t3": {ID: "t3", Name: "team:core"},
	}}

	t.Run("sorts tag names", func(t *testing.T) {
		monitorTags := &fakeMonitorTagService{rels: []*monitor_tag.Model{{TagID: "t3"}, {TagID: "t1"}, {TagID: "t2"}}}
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(monitorTags, tags).loadMonitorTags(context.Background(), m)

		assert.Equal(t, []string{"country:ua", "severity:critical", "team:core"}, m.Tags)
	})

	t.Run("untagged monitor gets an empty list", func(t *testing.T) {
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(&fakeMonitorTagService{}, tags).loadMonitorTags(context.Background(), m)

		assert.Equal(t, []string{}, m.Tags)
	})

	t.Run("skips a relation whose tag is gone", func(t *testing.T) {
		monitorTags := &fakeMonitorTagService{rels: []*monitor_tag.Model{{TagID: "t1"}, {TagID: "deleted"}}}
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(monitorTags, tags).loadMonitorTags(context.Background(), m)

		assert.Equal(t, []string{"severity:critical"}, m.Tags)
	})

	t.Run("leaves tags empty when relations fail to load", func(t *testing.T) {
		monitorTags := &fakeMonitorTagService{err: errors.New("db down")}
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(monitorTags, tags).loadMonitorTags(context.Background(), m)

		assert.Nil(t, m.Tags)
	})

	t.Run("leaves tags empty when a tag fails to load", func(t *testing.T) {
		monitorTags := &fakeMonitorTagService{rels: []*monitor_tag.Model{{TagID: "t1"}, {TagID: "t2"}}}
		m := &monitor.Model{ID: "mon-1"}

		newTagTestListener(monitorTags, &fakeTagService{err: errors.New("db down")}).loadMonitorTags(context.Background(), m)

		assert.Nil(t, m.Tags)
	})
}
