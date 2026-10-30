package status_page_test

import (
	"errors"
	"testing"

	"peekaping/internal/limits"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/status_page"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const upperMonitor = "0D6F1E0E-6B1C-4D49-8C37-0B5F4A1B3A0A"
const lowerMonitor = "0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a0a"

func strs(values ...string) *[]string { return &values }

// A page is updated once it, its monitors, its slug and its domains have been checked, and its
// monitors and domains are written in the transaction that wrote it.
func TestStatusPageService_Update_Flow(t *testing.T) {
	t.Run("a title alone", func(t *testing.T) {
		f := newFlow(nil)
		f.repo.page.Title = "Found"

		got, err := f.update(status_page.UpdateStatusPageDTO{
			Title: ptr("New"), Description: ptr("d"), Icon: ptr("i"), Theme: ptr("t"), Published: ptr(true),
			ShowLocations: ptr(true), FooterText: ptr("f"), AutoRefreshInterval: ptr(30),
		})

		require.NoError(t, err)
		assert.Same(t, f.repo.page, got, "the page is read back")
		assert.Equal(t, []string{"repo.find page-1", "repo.update page-1", "repo.find page-1"}, f.log.calls)
		assert.Equal(t, &status_page.UpdateModel{
			Title: ptr("New"), Description: ptr("d"), Icon: ptr("i"), Theme: ptr("t"), Published: ptr(true),
			ShowLocations: ptr(true), FooterText: ptr("f"), AutoRefreshInterval: ptr(30),
		}, f.repo.updated)
	})

	t.Run("monitors are put in order, and those not in the list taken off", func(t *testing.T) {
		f := newFlow(nil)
		f.monitors.linked = []string{"keep", "drop"}
		dto := status_page.UpdateStatusPageDTO{MonitorIDs: strs(upperMonitor, "keep", "new")}

		_, err := f.update(dto)

		require.NoError(t, err)
		assert.Equal(t, []string{
			"repo.find page-1",
			"monitors.require " + lowerMonitor + ",keep,new",
			"repo.update page-1",
			"monitors.get page-1",
			"monitors.remove page-1 drop",
			"monitors.add page-1 " + lowerMonitor + " 0 true",
			"monitors.add page-1 new 2 true",
			"repo.find page-1",
		}, f.log.calls)
	})

	t.Run("an empty list takes every monitor off", func(t *testing.T) {
		f := newFlow(nil)
		f.monitors.linked = []string{"a", "b"}

		_, err := f.update(status_page.UpdateStatusPageDTO{MonitorIDs: strs()})

		require.NoError(t, err)
		require.Len(t, f.log.calls, 7)
		assert.Equal(t, []string{"repo.find page-1", "monitors.require ", "repo.update page-1", "monitors.get page-1"}, f.log.calls[:4])
		assert.ElementsMatch(t, []string{"monitors.remove page-1 a", "monitors.remove page-1 b"}, f.log.calls[4:6])
		assert.Equal(t, "repo.find page-1", f.log.calls[6])
	})

	t.Run("a slug is checked against the other pages before anything is written", func(t *testing.T) {
		f := newFlow(nil)

		_, err := f.update(status_page.UpdateStatusPageDTO{Slug: ptr("shop"), MonitorIDs: strs("m1")})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"repo.find page-1", "monitors.require m1", "repo.slug shop", "repo.update page-1", "monitors.get page-1", "monitors.add page-1 m1 0 true", "repo.find page-1",
		}, f.log.calls)
	})

	t.Run("a page keeps its own slug", func(t *testing.T) {
		f := newFlow(nil)
		f.repo.slugHolder = &status_page.Model{ID: "page-1"}

		_, err := f.update(status_page.UpdateStatusPageDTO{Slug: ptr("shop")})

		require.NoError(t, err)
		assert.Equal(t, []string{"repo.find page-1", "repo.slug shop", "repo.update page-1", "repo.find page-1"}, f.log.calls)
	})

	t.Run("domains are normalized, checked, and written with those not in the list taken off", func(t *testing.T) {
		f := newFlow(nil)
		f.domains.current = []string{"old.example.com", "a.example.com"}
		f.domains.holders["a.example.com"] = "page-1"

		_, err := f.update(status_page.UpdateStatusPageDTO{Domains: strs(" A.Example.com ", "b.example.com", "a.example.com", " ")})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"repo.find page-1",
			"domains.find a.example.com",
			"domains.find b.example.com",
			"domains.get page-1",
			"domains.get page-1",
			"repo.update page-1",
			"domains.add page-1 a.example.com",
			"domains.add page-1 b.example.com",
			"domains.remove page-1 old.example.com",
			"repo.find page-1",
		}, f.log.calls)
	})

	t.Run("an empty list of domains takes every domain off", func(t *testing.T) {
		f := newFlow(nil)
		f.domains.current = []string{"a.example.com"}

		_, err := f.update(status_page.UpdateStatusPageDTO{Domains: strs()})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"repo.find page-1", "domains.get page-1", "domains.get page-1", "repo.update page-1", "domains.remove page-1 a.example.com", "repo.find page-1",
		}, f.log.calls)
	})

	t.Run("monitors, then domains, in one transaction with limits", func(t *testing.T) {
		f := newFlow(limitsOf(intPtr(5), intPtr(4)))
		f.domains.current = []string{"old.example.com"}
		f.repo.domainCount = 1
		f.monitors.linked = []string{"gone"}

		_, err := f.update(status_page.UpdateStatusPageDTO{Slug: ptr("shop"), MonitorIDs: strs("m1"), Domains: strs("a.example.com")})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"repo.find page-1",
			"monitors.require m1",
			"repo.slug shop",
			"domains.find a.example.com",
			"limits.for",
			"domains.get page-1",
			"repo.lock",
			"repo.count domains",
			"tx begin",
			"domains.get page-1",
			"repo.lock",
			"repo.count domains",
			"repo.update page-1",
			"monitors.get page-1",
			"monitors.remove page-1 gone",
			"monitors.add page-1 m1 0 true",
			"domains.add page-1 a.example.com",
			"domains.remove page-1 old.example.com",
			"tx commit",
			"repo.find page-1",
		}, f.log.calls)
	})

	t.Run("the limits are read for domains alone", func(t *testing.T) {
		f := newFlow(limitsOf(intPtr(5), intPtr(4)))

		_, err := f.update(status_page.UpdateStatusPageDTO{Title: ptr("New"), MonitorIDs: strs("m1")})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"repo.find page-1", "monitors.require m1", "tx begin", "repo.update page-1", "monitors.get page-1", "monitors.add page-1 m1 0 true", "tx commit", "repo.find page-1",
		}, f.log.calls)
	})
}

// The domains a page may have together are counted against the limit with the domains the update
// adds and those it takes off, and an update that adds none is never refused.
func TestStatusPageService_Update_DomainLimit(t *testing.T) {
	t.Run("adding up to the limit", func(t *testing.T) {
		f := newFlow(limitsOf(nil, intPtr(3)))
		f.domains.current = []string{"x.example.com", "y.example.com"}
		f.repo.domainCount = 2

		// z and w are added and y is taken off: 2 - 1 + 2 = 3
		_, err := f.update(status_page.UpdateStatusPageDTO{Domains: strs("x.example.com", "z.example.com", "w.example.com")})

		require.NoError(t, err)
	})

	t.Run("adding past the limit", func(t *testing.T) {
		f := newFlow(limitsOf(nil, intPtr(2)))
		f.domains.current = []string{"x.example.com", "y.example.com"}
		f.repo.domainCount = 2

		got, err := f.update(status_page.UpdateStatusPageDTO{Domains: strs("x.example.com", "z.example.com", "w.example.com")})

		var reached *limits.ReachedError
		require.ErrorAs(t, err, &reached)
		assert.Equal(t, limits.CustomDomains, reached.Limit)
		assert.Equal(t, 2, reached.Max)
		assert.Nil(t, got)
		assert.Equal(t, []string{
			"repo.find page-1", "domains.find x.example.com", "domains.find z.example.com", "domains.find w.example.com",
			"limits.for", "domains.get page-1", "repo.lock", "repo.count domains",
		}, f.log.calls)
	})

	t.Run("a workspace above a lowered limit can still take domains off", func(t *testing.T) {
		f := newFlow(limitsOf(nil, intPtr(0)))
		f.domains.current = []string{"x.example.com", "y.example.com"}
		f.repo.domainCount = 2

		_, err := f.update(status_page.UpdateStatusPageDTO{Domains: strs("x.example.com")})

		require.NoError(t, err)
		assert.NotContains(t, f.log.calls, "repo.lock")
	})

	t.Run("a limit that was reached by another write since the check", func(t *testing.T) {
		f := newFlow(limitsOf(nil, intPtr(2)))
		f.repo.domainCounts = []int{1, 2}

		_, err := f.update(status_page.UpdateStatusPageDTO{Domains: strs("a.example.com")})

		var reached *limits.ReachedError
		require.ErrorAs(t, err, &reached)
		assert.Equal(t, []string{
			"repo.find page-1", "domains.find a.example.com", "limits.for", "domains.get page-1", "repo.lock", "repo.count domains",
			"tx begin", "domains.get page-1", "repo.lock", "repo.count domains", "tx rollback",
		}, f.log.calls)
	})
}

// An update that is refused writes nothing, and the first thing wrong with it is the answer.
func TestStatusPageService_Update_Refusals(t *testing.T) {
	dto := func() status_page.UpdateStatusPageDTO {
		return status_page.UpdateStatusPageDTO{Slug: ptr("shop"), MonitorIDs: strs("m1", "m2"), Domains: strs("a.example.com", "b.example.com")}
	}
	prefix := []string{"repo.find page-1", "monitors.require m1,m2", "repo.slug shop"}
	checked := append(append([]string{}, prefix...), "domains.find a.example.com", "domains.find b.example.com")
	inTx := append(append([]string{}, checked...), "limits.for", "domains.get page-1", "repo.lock", "repo.count domains", "tx begin")
	written := append(append([]string{}, inTx...), "domains.get page-1", "repo.lock", "repo.count domains", "repo.update page-1")
	monitored := append(append([]string{}, written...), "monitors.get page-1")
	domainLimit := limitsOf(nil, intPtr(10))

	for _, tt := range []struct {
		name    string
		limits  *limits.Limits
		arrange func(f *flow)
		check   func(t *testing.T, err error)
		calls   []string
	}{
		{
			name:    "a page that can't be read",
			arrange: func(f *flow) { f.repo.findErr = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   []string{"repo.find page-1"},
		},
		{
			name:    "a page that is not there",
			arrange: func(f *flow) { f.repo.page = nil },
			check:   func(t *testing.T, err error) { assert.Equal(t, status_page.ErrNotFound, err) },
			calls:   []string{"repo.find page-1"},
		},
		{
			name:    "a monitor that is not in the workspace",
			arrange: func(f *flow) { f.monitors.requireErr = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   []string{"repo.find page-1", "monitors.require m1,m2"},
		},
		{
			name:    "a slug that can't be checked",
			arrange: func(f *flow) { f.repo.slugErr = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   prefix,
		},
		{
			name:    "a slug another page holds",
			arrange: func(f *flow) { f.repo.slugHolder = &status_page.Model{ID: "other"} },
			check: func(t *testing.T, err error) {
				var used *status_page.SlugAlreadyUsedError
				require.ErrorAs(t, err, &used)
				assert.Equal(t, "shop", used.Slug)
			},
			calls: prefix,
		},
		{
			name:    "a domain that can't be checked",
			arrange: func(f *flow) { f.domains.findErr["b.example.com"] = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   checked,
		},
		{
			name:    "a domain another page holds",
			arrange: func(f *flow) { f.domains.holders["a.example.com"] = "other" },
			check: func(t *testing.T, err error) {
				var used *status_page.DomainAlreadyUsedError
				require.ErrorAs(t, err, &used)
				assert.Equal(t, "a.example.com", used.Domain)
			},
			calls: append(append([]string{}, prefix...), "domains.find a.example.com"),
		},
		{
			name:    "limits that can't be read",
			limits:  domainLimit,
			arrange: func(f *flow) { f.limits.err = errBoom },
			check:   func(t *testing.T, err error) { assert.EqualError(t, err, "read the workspace's limits: boom") },
			calls:   append(append([]string{}, checked...), "limits.for"),
		},
		{
			name:    "the domains of the page that can't be read",
			arrange: func(f *flow) { f.domains.currentOK = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   append(append([]string{}, checked...), "domains.get page-1"),
		},
		{
			name:    "a workspace that can't be locked before the write",
			limits:  domainLimit,
			arrange: func(f *flow) { f.repo.lockErr = errBoom },
			check:   func(t *testing.T, err error) { assert.EqualError(t, err, "lock the workspace: boom") },
			calls:   append(append([]string{}, checked...), "limits.for", "domains.get page-1", "repo.lock"),
		},
		{
			name:    "domains that can't be counted before the write",
			limits:  domainLimit,
			arrange: func(f *flow) { f.repo.domainErr = errBoom },
			check:   func(t *testing.T, err error) { assert.EqualError(t, err, "count the custom domains: boom") },
			calls:   append(append([]string{}, checked...), "limits.for", "domains.get page-1", "repo.lock", "repo.count domains"),
		},
		{
			name:   "the domains of the page that can't be read in the transaction",
			limits: domainLimit,
			arrange: func(f *flow) {
				f.domains.currentOK = nil
				f.domains.current = nil
				f.domains.failOn = 2
			},
			check: func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls: append(append([]string{}, inTx...), "domains.get page-1", "tx rollback"),
		},
		{
			name:    "a page that can't be written",
			limits:  domainLimit,
			arrange: func(f *flow) { f.repo.updateErr = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   append(append([]string{}, written...), "tx rollback"),
		},
		{
			name:    "a slug another page took since the check",
			limits:  domainLimit,
			arrange: func(f *flow) { f.repo.updateErr = errors.New("UNIQUE constraint failed: status_pages.slug") },
			check: func(t *testing.T, err error) {
				var used *status_page.SlugAlreadyUsedError
				require.ErrorAs(t, err, &used)
				assert.Equal(t, "shop", used.Slug)
			},
			calls: append(append([]string{}, written...), "tx rollback"),
		},
		{
			name:    "the monitors of the page that can't be read",
			limits:  domainLimit,
			arrange: func(f *flow) { f.monitors.linkedErr = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   append(append([]string{}, monitored...), "tx rollback"),
		},
		{
			name:   "a monitor that can't be taken off",
			limits: domainLimit,
			arrange: func(f *flow) {
				f.monitors.linked = []string{"gone"}
				f.monitors.removeErr["gone"] = errBoom
			},
			check: func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls: append(append([]string{}, monitored...), "monitors.remove page-1 gone", "tx rollback"),
		},
		{
			name:    "a monitor that can't be put on",
			limits:  domainLimit,
			arrange: func(f *flow) { f.monitors.addErr["m2"] = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   append(append([]string{}, monitored...), "monitors.add page-1 m1 0 true", "monitors.add page-1 m2 1 true", "tx rollback"),
		},
		{
			name:    "a domain that can't be put on",
			limits:  domainLimit,
			arrange: func(f *flow) { f.domains.addErr["a.example.com"] = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls: append(append([]string{}, monitored...), "monitors.add page-1 m1 0 true", "monitors.add page-1 m2 1 true",
				"domains.add page-1 a.example.com", "tx rollback"),
		},
		{
			name:   "a domain another page took since the check",
			limits: domainLimit,
			arrange: func(f *flow) {
				f.domains.addErr["b.example.com"] = &domain_status_page.DomainTakenError{Domain: "b.example.com"}
			},
			check: func(t *testing.T, err error) {
				var used *status_page.DomainAlreadyUsedError
				require.ErrorAs(t, err, &used)
				assert.Equal(t, "b.example.com", used.Domain)
			},
			calls: append(append([]string{}, monitored...), "monitors.add page-1 m1 0 true", "monitors.add page-1 m2 1 true",
				"domains.add page-1 a.example.com", "domains.add page-1 b.example.com", "tx rollback"),
		},
		{
			name:   "a domain that can't be taken off",
			limits: domainLimit,
			arrange: func(f *flow) {
				f.domains.current = []string{"old.example.com"}
				f.domains.removeErr["old.example.com"] = errBoom
			},
			check: func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls: append(append([]string{}, monitored...), "monitors.add page-1 m1 0 true", "monitors.add page-1 m2 1 true",
				"domains.add page-1 a.example.com", "domains.add page-1 b.example.com", "domains.remove page-1 old.example.com", "tx rollback"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlow(tt.limits)
			tt.arrange(f)

			got, err := f.update(dto())

			require.Error(t, err)
			assert.Nil(t, got)
			tt.check(t, err)
			assert.Equal(t, tt.calls, f.log.calls)
		})
	}
}

// The page the update answers with is read back after the write.
func TestStatusPageService_Update_ReadsThePageBack(t *testing.T) {
	f := newFlow(nil)
	f.repo.findErrOn = 2

	got, err := f.update(status_page.UpdateStatusPageDTO{Title: ptr("New")})

	assert.Equal(t, errBoom, err)
	assert.Nil(t, got)
	assert.Equal(t, []string{"repo.find page-1", "repo.update page-1", "repo.find page-1"}, f.log.calls)
}
