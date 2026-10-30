package status_page_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"peekaping/internal/limits"
	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/modules/workspace/workspaceid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The service over fakes that note every call they get, in order, so a test can say what the
// service reads and writes, and in which order. A call a test doesn't configure panics.

var errBoom = errors.New("boom")

const flowWorkspace = workspace.ID("0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a01")

type flowLog struct{ calls []string }

func (l *flowLog) add(format string, args ...any) {
	l.calls = append(l.calls, fmt.Sprintf(format, args...))
}

// flowRepository is a status page repository with one page, "page-1" in the workspace
type flowRepository struct {
	status_page.Repository
	log *flowLog
	// page is what FindByID answers for any id: nil is a page that is not there
	page    *status_page.Model
	findErr error
	// findErrOn, when set, is the read of a page that fails, counting from 1
	findErrOn int
	finds     int
	// slugHolder is the page that holds a slug, nil if none does
	slugHolder  *status_page.Model
	slugErr     error
	createErr   error
	updateErr   error
	lockErr     error
	pages       int
	pagesErr    error
	domainCount int
	// domainCounts, when set, are the counts the successive reads answer, then domainCount
	domainCounts []int
	domainErr    error
	updated      *status_page.UpdateModel
	created      *status_page.Model
}

func (r *flowRepository) FindByID(_ context.Context, _ workspace.ID, id string) (*status_page.Model, error) {
	r.log.add("repo.find %s", id)
	r.finds++
	if r.findErrOn > 0 && r.finds == r.findErrOn {
		return nil, errBoom
	}
	return r.page, r.findErr
}

func (r *flowRepository) FindBySlugAnyWorkspace(_ context.Context, slug string) (*status_page.Model, error) {
	r.log.add("repo.slug %s", slug)
	return r.slugHolder, r.slugErr
}

func (r *flowRepository) Create(_ context.Context, _ workspace.ID, page *status_page.Model) (*status_page.Model, error) {
	r.log.add("repo.create %s", page.Slug)
	r.created = page
	if r.createErr != nil {
		return nil, r.createErr
	}
	created := *page
	created.ID = "page-1"
	return &created, nil
}

func (r *flowRepository) Update(_ context.Context, _ workspace.ID, id string, page *status_page.UpdateModel) error {
	r.log.add("repo.update %s", id)
	r.updated = page
	return r.updateErr
}

func (r *flowRepository) LockWorkspace(context.Context, workspace.ID) error {
	r.log.add("repo.lock")
	return r.lockErr
}

func (r *flowRepository) CountPages(context.Context, workspace.ID) (int, error) {
	r.log.add("repo.count pages")
	return r.pages, r.pagesErr
}

func (r *flowRepository) CountDomains(context.Context, workspace.ID) (int, error) {
	r.log.add("repo.count domains")
	if len(r.domainCounts) > 0 {
		count := r.domainCounts[0]
		r.domainCounts = r.domainCounts[1:]
		return count, r.domainErr
	}
	return r.domainCount, r.domainErr
}

// flowMonitors is the monitor links of the pages
type flowMonitors struct {
	monitor_status_page.Service
	log        *flowLog
	requireErr error
	linked     []string
	linkedErr  error
	addErr     map[string]error
	removeErr  map[string]error
}

func (m *flowMonitors) RequireMonitors(_ context.Context, _ workspace.ID, ids []string) error {
	m.log.add("monitors.require %s", strings.Join(ids, ","))
	return m.requireErr
}

func (m *flowMonitors) GetMonitorsForStatusPage(_ context.Context, id string) ([]*monitor_status_page.Model, error) {
	m.log.add("monitors.get %s", id)
	var links []*monitor_status_page.Model
	for _, monitorID := range m.linked {
		links = append(links, &monitor_status_page.Model{StatusPageID: id, MonitorID: monitorID})
	}
	return links, m.linkedErr
}

func (m *flowMonitors) AddMonitorToStatusPage(_ context.Context, id, monitorID string, order int, active bool) (*monitor_status_page.Model, error) {
	m.log.add("monitors.add %s %s %d %t", id, monitorID, order, active)
	return nil, m.addErr[monitorID]
}

func (m *flowMonitors) RemoveMonitorFromStatusPage(_ context.Context, id, monitorID string) error {
	m.log.add("monitors.remove %s %s", id, monitorID)
	return m.removeErr[monitorID]
}

// flowDomains is the domains of the pages
type flowDomains struct {
	domain_status_page.Service
	log *flowLog
	// holders is the page that holds each domain
	holders   map[string]string
	findErr   map[string]error
	current   []string
	currentOK error
	// failOn, when set, is the read of the page's domains that fails, counting from 1
	failOn    int
	gets      int
	addErr    map[string]error
	removeErr map[string]error
}

func (d *flowDomains) FindByDomain(_ context.Context, domain string) (*domain_status_page.Model, error) {
	d.log.add("domains.find %s", domain)
	if err := d.findErr[domain]; err != nil {
		return nil, err
	}
	if holder, ok := d.holders[domain]; ok {
		return &domain_status_page.Model{StatusPageID: holder, Domain: domain}, nil
	}
	return nil, nil
}

func (d *flowDomains) GetDomainsForStatusPage(_ context.Context, id string) ([]*domain_status_page.Model, error) {
	d.log.add("domains.get %s", id)
	d.gets++
	if d.failOn > 0 && d.gets == d.failOn {
		return nil, errBoom
	}
	var held []*domain_status_page.Model
	for _, domain := range d.current {
		held = append(held, &domain_status_page.Model{StatusPageID: id, Domain: domain})
	}
	return held, d.currentOK
}

func (d *flowDomains) AddDomainToStatusPage(_ context.Context, id, domain string) (*domain_status_page.Model, error) {
	d.log.add("domains.add %s %s", id, domain)
	return nil, d.addErr[domain]
}

func (d *flowDomains) RemoveDomainFromStatusPage(_ context.Context, id, domain string) error {
	d.log.add("domains.remove %s %s", id, domain)
	return d.removeErr[domain]
}

type flowLimits struct {
	log    *flowLog
	limits limits.Limits
	err    error
}

func (l *flowLimits) For(context.Context, workspaceid.ID) (limits.Limits, limits.Origin, error) {
	l.log.add("limits.for")
	return l.limits, limits.OriginInstall, l.err
}

func (l *flowLimits) Snapshot(context.Context) (limits.Snapshot, error) { panic("not used") }

type flowTransactor struct{ log *flowLog }

func (tx *flowTransactor) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx.log.add("tx begin")
	err := fn(ctx)
	if err != nil {
		tx.log.add("tx rollback")
		return err
	}
	tx.log.add("tx commit")
	return nil
}

type flow struct {
	log      *flowLog
	repo     *flowRepository
	monitors *flowMonitors
	domains  *flowDomains
	limits   *flowLimits
	service  status_page.Service
}

// newFlow builds the service with the workspace's limits, or with no limits at all when limit is nil
func newFlow(withLimits *limits.Limits) *flow {
	log := &flowLog{}
	f := &flow{
		log:      log,
		repo:     &flowRepository{log: log, page: &status_page.Model{ID: "page-1", WorkspaceID: flowWorkspace}},
		monitors: &flowMonitors{log: log, addErr: map[string]error{}, removeErr: map[string]error{}},
		domains:  &flowDomains{log: log, holders: map[string]string{}, findErr: map[string]error{}, addErr: map[string]error{}, removeErr: map[string]error{}},
	}
	var options []status_page.ServiceOption
	if withLimits != nil {
		f.limits = &flowLimits{log: log, limits: *withLimits}
		options = append(options, status_page.WithLimits(f.limits, &flowTransactor{log: log}))
	}
	f.service = status_page.NewService(f.repo, nil, f.monitors, f.domains, zap.NewNop().Sugar(), options...)
	return f
}

func (f *flow) create(dto status_page.CreateStatusPageDTO) (*status_page.Model, error) {
	return f.service.Create(context.Background(), flowWorkspace, &dto)
}

func (f *flow) update(dto status_page.UpdateStatusPageDTO) (*status_page.Model, error) {
	return f.service.Update(context.Background(), flowWorkspace, "page-1", &dto)
}

func limitsOf(pages, domains *int) *limits.Limits {
	return &limits.Limits{StatusPages: pages, CustomDomains: domains}
}

func intPtr(n int) *int { return &n }

// A page is created after its monitors, slug and domains are checked, and its domains are written
// with it, in the transaction that counted it against the workspace's limits.
func TestStatusPageService_Create_Flow(t *testing.T) {
	t.Run("a bare page, with no limits to read", func(t *testing.T) {
		f := newFlow(nil)

		created, err := f.create(status_page.CreateStatusPageDTO{Slug: "shop", Title: "Shop"})

		require.NoError(t, err)
		assert.Equal(t, "page-1", created.ID)
		assert.Equal(t, []string{"monitors.require ", "repo.slug shop", "repo.create shop"}, f.log.calls)
	})

	t.Run("monitors and domains, with limits", func(t *testing.T) {
		f := newFlow(limitsOf(intPtr(5), intPtr(3)))

		created, err := f.create(status_page.CreateStatusPageDTO{
			Slug: "shop", Title: "Shop", Description: "d", Icon: "i", Theme: "t", Published: true, ShowLocations: true, FooterText: "f", AutoRefreshInterval: 30,
			MonitorIDs: []string{"0D6F1E0E-6B1C-4D49-8C37-0B5F4A1B3A0A", "m2"}, Domains: []string{" Status.Example.com ", "status.example.com", "  ", "b.example.com"},
		})

		require.NoError(t, err)
		assert.Equal(t, "page-1", created.ID)
		assert.Equal(t, []string{
			"monitors.require 0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a0a,m2",
			"repo.slug shop",
			"domains.find status.example.com",
			"domains.find b.example.com",
			"limits.for",
			"tx begin",
			"repo.lock",
			"repo.count pages",
			"repo.count domains",
			"repo.create shop",
			"domains.add page-1 status.example.com",
			"domains.add page-1 b.example.com",
			"tx commit",
			"monitors.add page-1 0d6f1e0e-6b1c-4d49-8c37-0b5f4a1b3a0a 0 true",
			"monitors.add page-1 m2 1 true",
		}, f.log.calls)
		assert.Equal(t, &status_page.Model{
			Slug: "shop", Title: "Shop", Description: "d", Icon: "i", Theme: "t", Published: true, ShowLocations: true, FooterText: "f", AutoRefreshInterval: 30,
		}, f.repo.created)
	})

	t.Run("limits that are not set count nothing", func(t *testing.T) {
		f := newFlow(limitsOf(nil, nil))

		_, err := f.create(status_page.CreateStatusPageDTO{Slug: "shop", Domains: []string{"a.example.com"}})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"monitors.require ", "repo.slug shop", "domains.find a.example.com", "limits.for",
			"tx begin", "repo.create shop", "domains.add page-1 a.example.com", "tx commit",
		}, f.log.calls)
	})

	t.Run("a page limit counts the page and not the domains", func(t *testing.T) {
		f := newFlow(limitsOf(intPtr(2), nil))
		f.repo.pages = 1

		_, err := f.create(status_page.CreateStatusPageDTO{Slug: "shop", Domains: []string{"a.example.com"}})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"monitors.require ", "repo.slug shop", "domains.find a.example.com", "limits.for",
			"tx begin", "repo.lock", "repo.count pages", "repo.create shop", "domains.add page-1 a.example.com", "tx commit",
		}, f.log.calls)
	})

	t.Run("a domain limit counts the domains and not the page", func(t *testing.T) {
		f := newFlow(limitsOf(nil, intPtr(2)))
		f.repo.domainCount = 1

		_, err := f.create(status_page.CreateStatusPageDTO{Slug: "shop", Domains: []string{"a.example.com"}})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"monitors.require ", "repo.slug shop", "domains.find a.example.com", "limits.for",
			"tx begin", "repo.lock", "repo.count domains", "repo.create shop", "domains.add page-1 a.example.com", "tx commit",
		}, f.log.calls)
	})

	t.Run("a domain limit counts nothing for a page with no domains", func(t *testing.T) {
		f := newFlow(limitsOf(nil, intPtr(0)))

		_, err := f.create(status_page.CreateStatusPageDTO{Slug: "shop"})

		require.NoError(t, err)
		assert.Equal(t, []string{"monitors.require ", "repo.slug shop", "limits.for", "tx begin", "repo.create shop", "tx commit"}, f.log.calls)
	})

	t.Run("a monitor that can't be put on the page is skipped", func(t *testing.T) {
		f := newFlow(nil)
		f.monitors.addErr["m1"] = errBoom

		created, err := f.create(status_page.CreateStatusPageDTO{Slug: "shop", MonitorIDs: []string{"m1", "m2"}})

		require.NoError(t, err)
		assert.Equal(t, "page-1", created.ID)
		assert.Equal(t, []string{
			"monitors.require m1,m2", "repo.slug shop", "repo.create shop", "monitors.add page-1 m1 0 true", "monitors.add page-1 m2 1 true",
		}, f.log.calls)
	})
}

// A page that is refused writes nothing, and the first thing wrong with it is the answer.
func TestStatusPageService_Create_Refusals(t *testing.T) {
	dto := status_page.CreateStatusPageDTO{Slug: "shop", Title: "Shop", MonitorIDs: []string{"m1"}, Domains: []string{"a.example.com", "b.example.com"}}

	for _, tt := range []struct {
		name    string
		arrange func(f *flow)
		limits  *limits.Limits
		check   func(t *testing.T, err error)
		calls   []string
	}{
		{
			name:    "a monitor that is not in the workspace",
			arrange: func(f *flow) { f.monitors.requireErr = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   []string{"monitors.require m1"},
		},
		{
			name:    "a slug that can't be checked",
			arrange: func(f *flow) { f.repo.slugErr = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   []string{"monitors.require m1", "repo.slug shop"},
		},
		{
			name:    "a slug another page holds",
			arrange: func(f *flow) { f.repo.slugHolder = &status_page.Model{ID: "other"} },
			check: func(t *testing.T, err error) {
				assert.Equal(t, `{"code":"SLUG_EXISTS", "slug":"shop"}`, err.Error())
				var used *status_page.SlugAlreadyUsedError
				assert.ErrorAs(t, err, &used)
			},
			calls: []string{"monitors.require m1", "repo.slug shop"},
		},
		{
			name:    "a domain that can't be checked",
			arrange: func(f *flow) { f.domains.findErr["a.example.com"] = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com"},
		},
		{
			name:    "the second domain another page holds",
			arrange: func(f *flow) { f.domains.holders["b.example.com"] = "other" },
			check: func(t *testing.T, err error) {
				assert.Equal(t, `{"code":"DOMAIN_EXISTS", "domain":"b.example.com"}`, err.Error())
				var used *status_page.DomainAlreadyUsedError
				assert.ErrorAs(t, err, &used)
			},
			calls: []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com"},
		},
		{
			name:    "limits that can't be read",
			limits:  limitsOf(nil, nil),
			arrange: func(f *flow) { f.limits.err = errBoom },
			check: func(t *testing.T, err error) {
				assert.EqualError(t, err, "read the workspace's limits: boom")
				assert.ErrorIs(t, err, errBoom)
			},
			calls: []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "limits.for"},
		},
		{
			name:    "a workspace that can't be locked",
			limits:  limitsOf(intPtr(1), nil),
			arrange: func(f *flow) { f.repo.lockErr = errBoom },
			check:   func(t *testing.T, err error) { assert.EqualError(t, err, "lock the workspace: boom") },
			calls:   []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "limits.for", "tx begin", "repo.lock", "tx rollback"},
		},
		{
			name:    "pages that can't be counted",
			limits:  limitsOf(intPtr(1), nil),
			arrange: func(f *flow) { f.repo.pagesErr = errBoom },
			check:   func(t *testing.T, err error) { assert.EqualError(t, err, "count the status pages: boom") },
			calls:   []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "limits.for", "tx begin", "repo.lock", "repo.count pages", "tx rollback"},
		},
		{
			name:    "pages past the limit",
			limits:  limitsOf(intPtr(2), intPtr(10)),
			arrange: func(f *flow) { f.repo.pages = 2 },
			check: func(t *testing.T, err error) {
				var reached *limits.ReachedError
				require.ErrorAs(t, err, &reached)
				assert.Equal(t, limits.StatusPages, reached.Limit)
				assert.Equal(t, 2, reached.Max)
			},
			calls: []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "limits.for", "tx begin", "repo.lock", "repo.count pages", "tx rollback"},
		},
		{
			name:    "domains that can't be counted",
			limits:  limitsOf(intPtr(5), intPtr(5)),
			arrange: func(f *flow) { f.repo.domainErr = errBoom },
			check:   func(t *testing.T, err error) { assert.EqualError(t, err, "count the custom domains: boom") },
			calls:   []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "limits.for", "tx begin", "repo.lock", "repo.count pages", "repo.count domains", "tx rollback"},
		},
		{
			name:    "domains past the limit",
			limits:  limitsOf(intPtr(5), intPtr(3)),
			arrange: func(f *flow) { f.repo.domainCount = 2 },
			check: func(t *testing.T, err error) {
				var reached *limits.ReachedError
				require.ErrorAs(t, err, &reached)
				assert.Equal(t, limits.CustomDomains, reached.Limit)
				assert.Equal(t, 3, reached.Max)
			},
			calls: []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "limits.for", "tx begin", "repo.lock", "repo.count pages", "repo.count domains", "tx rollback"},
		},
		{
			name:    "a page that can't be written",
			arrange: func(f *flow) { f.repo.createErr = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "repo.create shop"},
		},
		{
			name:    "a slug another page took since the check",
			arrange: func(f *flow) { f.repo.createErr = errors.New("UNIQUE constraint failed: status_pages.slug") },
			check: func(t *testing.T, err error) {
				var used *status_page.SlugAlreadyUsedError
				require.ErrorAs(t, err, &used)
				assert.Equal(t, "shop", used.Slug)
			},
			calls: []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "repo.create shop"},
		},
		{
			name:    "a domain that can't be written",
			arrange: func(f *flow) { f.domains.addErr["a.example.com"] = errBoom },
			check:   func(t *testing.T, err error) { assert.Equal(t, errBoom, err) },
			calls:   []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "repo.create shop", "domains.add page-1 a.example.com"},
		},
		{
			name: "a domain another page took since the check",
			arrange: func(f *flow) {
				f.domains.addErr["b.example.com"] = &domain_status_page.DomainTakenError{Domain: "b.example.com"}
			},
			check: func(t *testing.T, err error) {
				var used *status_page.DomainAlreadyUsedError
				require.ErrorAs(t, err, &used)
				assert.Equal(t, "b.example.com", used.Domain)
			},
			calls: []string{"monitors.require m1", "repo.slug shop", "domains.find a.example.com", "domains.find b.example.com", "repo.create shop", "domains.add page-1 a.example.com", "domains.add page-1 b.example.com"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFlow(tt.limits)
			tt.arrange(f)

			created, err := f.create(dto)

			require.Error(t, err)
			assert.Nil(t, created)
			tt.check(t, err)
			assert.Equal(t, tt.calls, f.log.calls)
		})
	}
}
