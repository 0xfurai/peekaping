package domain_status_page_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"peekaping/internal/modules/domain_status_page"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// fakeDNS answers the lookups of a check from a table, and counts them. A name it doesn't know is a
// lookup error, as an NXDOMAIN is.
type fakeDNS struct {
	mu     sync.Mutex
	txt    map[string][]string
	cname  map[string]string
	lookup int
}

func newFakeDNS() *fakeDNS {
	return &fakeDNS{txt: map[string][]string{}, cname: map[string]string{}}
}

func (d *fakeDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lookup++
	if values, ok := d.txt[name]; ok {
		return values, nil
	}
	return nil, errors.New("lookup " + name + ": no such host")
}

func (d *fakeDNS) LookupCNAME(_ context.Context, host string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lookup++
	if target, ok := d.cname[host]; ok {
		return target, nil
	}
	return "", errors.New("lookup " + host + ": no such host")
}

// clock is a time the tests move by hand
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type verifying struct {
	svc   domain_status_page.Service
	dns   *fakeDNS
	clock *clock
}

// verifying is a service with verification on, a fake resolver and a clock at t0. The CNAME target
// is target.example.
func (f fixture) verifying(target string) verifying {
	dns, c := newFakeDNS(), &clock{now: t0}
	return verifying{dns: dns, clock: c, svc: f.with(
		domain_status_page.WithVerification(true, target),
		domain_status_page.WithResolver(dns),
		domain_status_page.WithClock(c.Now),
	)}
}

func (v verifying) status(t *testing.T, page, domain string) *domain_status_page.Status {
	t.Helper()
	statuses, err := v.svc.Statuses(context.Background(), page)
	require.NoError(t, err)
	for _, s := range statuses {
		if s.Domain == domain {
			return s
		}
	}
	t.Fatalf("%s is not listed on the page", domain)
	return nil
}

func TestVerification_ANewDomainIsPendingWithItsRecords(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		added := add(t, f.svc, page, "Status.Example.com")

		assert.Len(t, added.VerificationToken, 64, "a new row gets a token on insert")
		assert.False(t, added.Verified())

		s := v.status(t, page, "status.example.com")
		assert.Equal(t, "pending", s.State)
		assert.Equal(t, "_peekaping-verify.status.example.com", s.Records.TXT.Name)
		assert.Equal(t, "peekaping-verify="+added.VerificationToken, s.Records.TXT.Value)
		require.NotNil(t, s.Records.CNAME)
		assert.Equal(t, "status.example.com", s.Records.CNAME.Name)
		assert.Equal(t, "peek.example", s.Records.CNAME.Target)
		assert.Nil(t, s.RemoveAt, "its week starts at its first check")
	})
}

func TestVerification_ATokenIsUniquePerDomain(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		page := f.page()
		a, b := add(t, f.svc, page, "a.example.com"), add(t, f.svc, page, "b.example.com")
		assert.NotEqual(t, a.VerificationToken, b.VerificationToken)
	})
}

func TestVerification_ARowWithoutATokenGetsOneWhenFirstShown(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		added := add(t, f.svc, page, "old.example.com")
		_, err := f.db.ExecContext(context.Background(), "UPDATE domain_status_page SET verification_token = '' WHERE id = ?", added.ID)
		require.NoError(t, err)

		first := v.status(t, page, "old.example.com")
		second := v.status(t, page, "old.example.com")
		assert.Regexp(t, "^peekaping-verify=[0-9a-f]{64}$", first.Records.TXT.Value)
		assert.Equal(t, first.Records.TXT.Value, second.Records.TXT.Value, "the token is stored, not made up again")
	})
}

func TestVerification_ATXTRecordWithTheTokenVerifiesTheDomain(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		added := add(t, f.svc, page, "status.example.com")
		ctx := context.Background()

		s, err := v.svc.CheckNow(ctx, page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "pending", s.State, "no record yet")
		require.NotNil(t, s.LastCheckedAt)

		v.dns.txt["_peekaping-verify.status.example.com"] = []string{"v=spf1 -all", "peekaping-verify=" + added.VerificationToken}
		s, err = v.svc.CheckNow(ctx, page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "verified", s.State)
		assert.Equal(t, t0, s.VerifiedAt.UTC())
	})
}

func TestVerification_ATXTRecordWithAnotherDomainsTokenDoesNotVerify(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		add(t, f.svc, page, "status.example.com")
		other := add(t, f.svc, page, "other.example.com")
		v.dns.txt["_peekaping-verify.status.example.com"] = []string{"peekaping-verify=" + other.VerificationToken}

		s, err := v.svc.CheckNow(context.Background(), page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "pending", s.State)
	})
}

func TestVerification_ACNAMEToTheTargetVerifiesTheDomain(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("Peek.Example")
		page := f.page()
		add(t, f.svc, page, "status.example.com")
		v.dns.cname["status.example.com"] = "PEEK.example."

		s, err := v.svc.CheckNow(context.Background(), page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "verified", s.State, "case and the trailing dot don't matter")
	})
}

func TestVerification_ACNAMEToAnotherHostDoesNotVerify(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		add(t, f.svc, page, "status.example.com")
		v.dns.cname["status.example.com"] = "cdn.elsewhere.example."
		add(t, f.svc, page, "bare.example.com")
		v.dns.cname["bare.example.com"] = "bare.example.com."

		for _, domain := range []string{"status.example.com", "bare.example.com"} {
			s, err := v.svc.CheckNow(context.Background(), page, domain)
			require.NoError(t, err)
			assert.Equal(t, "pending", s.State, domain)
		}
	})
}

func TestVerification_WithoutATargetOnlyTheTXTRecordVerifies(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("")
		page := f.page()
		add(t, f.svc, page, "status.example.com")
		v.dns.cname["status.example.com"] = "localhost."

		s, err := v.svc.CheckNow(context.Background(), page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "pending", s.State)
		assert.Nil(t, s.Records.CNAME)
	})
}

func TestVerification_TheTargetIsThePublicURLsHostWhenNoneIsSet(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		page := f.page()
		add(t, f.svc, page, "status.example.com")
		dns := newFakeDNS()
		dns.cname["status.example.com"] = "peek.example."
		svc := f.with(
			domain_status_page.WithVerification(true, ""),
			domain_status_page.WithResolver(dns),
			domain_status_page.WithPublicURL(publicURL("https://peek.example:8443/app")),
		)
		assert.Equal(t, "peek.example", svc.CNAMETarget(context.Background()))
		s, err := svc.CheckNow(context.Background(), page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "verified", s.State)

		for _, local := range []string{"http://localhost:8383", "http://192.168.1.4"} {
			svc = f.with(domain_status_page.WithVerification(true, ""), domain_status_page.WithPublicURL(publicURL(local)))
			assert.Empty(t, svc.CNAMETarget(context.Background()), local)
		}
	})
}

type publicURL string

func (p publicURL) PublicURL(context.Context) string { return string(p) }

func TestVerification_AVerifiedDomainFailsForADayBeforeItIsPendingAgain(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		added := add(t, f.svc, page, "status.example.com")
		ctx := context.Background()
		v.dns.txt["_peekaping-verify.status.example.com"] = []string{"peekaping-verify=" + added.VerificationToken}
		_, err := v.svc.CheckNow(ctx, page, "status.example.com")
		require.NoError(t, err)

		delete(v.dns.txt, "_peekaping-verify.status.example.com")
		v.clock.now = t0.Add(time.Hour)
		s, err := v.svc.CheckNow(ctx, page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "verified", s.State)
		assert.Equal(t, t0.Add(time.Hour), s.FailingSince.UTC())

		v.clock.now = t0.Add(time.Hour + 23*time.Hour)
		s, err = v.svc.CheckNow(ctx, page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "verified", s.State, "23 hours")

		v.clock.now = t0.Add(time.Hour + 24*time.Hour)
		s, err = v.svc.CheckNow(ctx, page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "pending", s.State, "24 hours")
		assert.Nil(t, s.FailingSince)
		require.NotNil(t, s.RemoveAt)
		assert.Equal(t, v.clock.now.Add(7*24*time.Hour), s.RemoveAt.UTC())
	})
}

func TestVerification_CheckDueChecksPendingDomainsAndVerifiedOnesOnceAnHour(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		ctx := context.Background()
		pending := add(t, f.svc, page, "pending.example.com")
		verified := add(t, f.svc, page, "verified.example.com")
		v.dns.txt["_peekaping-verify.verified.example.com"] = []string{"peekaping-verify=" + verified.VerificationToken}

		require.NoError(t, v.svc.CheckDue(ctx))
		assert.Equal(t, "verified", v.status(t, page, "verified.example.com").State)
		assert.Equal(t, "pending", v.status(t, page, "pending.example.com").State)
		firstRound := v.dns.lookup

		// Ten minutes on: the pending domain is looked up again, the verified one is not
		v.clock.now = t0.Add(10 * time.Minute)
		v.dns.txt["_peekaping-verify.pending.example.com"] = []string{"peekaping-verify=" + pending.VerificationToken}
		require.NoError(t, v.svc.CheckDue(ctx))
		assert.Equal(t, "verified", v.status(t, page, "pending.example.com").State)
		assert.Equal(t, 1, v.dns.lookup-firstRound, "the pending domain alone is looked up, and its TXT record is enough")

		// An hour on: both are due again
		v.clock.now = t0.Add(time.Hour + time.Minute)
		before := v.dns.lookup
		require.NoError(t, v.svc.CheckDue(ctx))
		assert.Greater(t, v.dns.lookup, before)
		assert.Equal(t, t0.Add(time.Hour+time.Minute), v.status(t, page, "verified.example.com").LastCheckedAt.UTC())
	})
}

func TestVerification_APendingDomainIsRemovedSevenDaysAfterItsFirstCheck(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page, other := f.page(), f.page()
		ctx := context.Background()
		add(t, f.svc, page, "status.example.com")
		kept := add(t, f.svc, other, "kept.example.com")
		v.dns.txt["_peekaping-verify.kept.example.com"] = []string{"peekaping-verify=" + kept.VerificationToken}

		require.NoError(t, v.svc.CheckDue(ctx))
		s := v.status(t, page, "status.example.com")
		require.NotNil(t, s.RemoveAt)
		assert.Equal(t, t0.Add(7*24*time.Hour), s.RemoveAt.UTC())

		// The background check runs every few minutes; hourly passes are the longest gap that counts
		for hour := 1; hour < 7*24; hour++ {
			v.clock.now = t0.Add(time.Duration(hour) * time.Hour)
			require.NoError(t, v.svc.CheckDue(ctx))
		}
		v.clock.now = t0.Add(7*24*time.Hour - time.Minute)
		require.NoError(t, v.svc.CheckDue(ctx))
		assert.Equal(t, []string{"status.example.com"}, domainsOf(t, f.svc, page), "7 days minus a minute")

		v.clock.now = t0.Add(7 * 24 * time.Hour)
		require.NoError(t, v.svc.CheckDue(ctx))
		assert.Empty(t, domainsOf(t, f.svc, page), "the name is free")
		assert.Equal(t, []string{"kept.example.com"}, domainsOf(t, f.svc, other), "a verified domain stays")
		_, err := f.svc.AddDomainToStatusPage(ctx, other, "status.example.com")
		assert.NoError(t, err, "another page can hold the freed name")
	})
}

// Checks that did not run, because verification was off or the server was down, are not days the
// domain was unverified (PP-165, AC7/AC8).
func TestVerification_APendingDomainKeepsItsWeekWhenChecksWereNotRunning(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		ctx := context.Background()
		add(t, f.svc, page, "status.example.com")
		require.NoError(t, v.svc.CheckDue(ctx))

		// Nothing checks for 30 days
		back := t0.Add(30 * 24 * time.Hour)
		v.clock.now = back
		require.NoError(t, v.svc.CheckDue(ctx))
		assert.Equal(t, []string{"status.example.com"}, domainsOf(t, f.svc, page), "not removed at the first check after the pause")
		s := v.status(t, page, "status.example.com")
		require.NotNil(t, s.RemoveAt)
		assert.Equal(t, back.Add(7*24*time.Hour), s.RemoveAt.UTC(), "its week starts again")
	})
}

// vanishing deletes the rows of a pass's due list once it has been read, as a page edit does while
// the pass runs
type vanishing struct {
	domain_status_page.Repository
	db *bun.DB
}

func (r vanishing) FindDueAnyWorkspace(ctx context.Context, before time.Time) ([]*domain_status_page.Model, error) {
	due, err := r.Repository.FindDueAnyWorkspace(ctx, before)
	if err == nil {
		_, err = r.db.ExecContext(ctx, "DELETE FROM domain_status_page")
	}
	return due, err
}

func TestVerification_ADomainThatVanishesDuringAPassIsNotAnError(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		page := f.page()
		added := add(t, f.svc, page, "status.example.com")
		_, err := f.db.ExecContext(context.Background(), "UPDATE domain_status_page SET verification_token = '' WHERE id = ?", added.ID)
		require.NoError(t, err)

		core, logs := observer.New(zapcore.DebugLevel)
		svc := domain_status_page.NewService(
			vanishing{Repository: domain_status_page.NewSQLRepository(f.db), db: f.db}, zap.New(core).Sugar(),
			domain_status_page.WithVerification(true, "peek.example"),
			domain_status_page.WithResolver(newFakeDNS()),
		)
		require.NoError(t, svc.CheckDue(context.Background()))
		for _, entry := range logs.All() {
			assert.Less(t, entry.Level, zapcore.WarnLevel, "logged: %s", entry.Message)
		}
	})
}

func TestVerification_ACheckNowNeverRemovesADomain(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		ctx := context.Background()
		add(t, f.svc, page, "status.example.com")
		require.NoError(t, v.svc.CheckDue(ctx))

		v.clock.now = t0.Add(8 * 24 * time.Hour)
		s, err := v.svc.CheckNow(ctx, page, "status.example.com")
		require.NoError(t, err)
		assert.Equal(t, "pending", s.State)
		assert.Equal(t, []string{"status.example.com"}, domainsOf(t, f.svc, page))
	})
}

func TestVerification_CheckNowOfADomainThePageDoesNotListIsNotFound(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page, other := f.page(), f.page()
		add(t, f.svc, other, "theirs.example.com")

		_, err := v.svc.CheckNow(context.Background(), page, "theirs.example.com")
		assert.ErrorIs(t, err, domain_status_page.ErrDomainNotFound)
		_, err = v.svc.CheckNow(context.Background(), page, "unknown.example.com")
		assert.ErrorIs(t, err, domain_status_page.ErrDomainNotFound)
	})
}

func TestVerification_OffChecksNothingAndServesEverything(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		dns := newFakeDNS()
		svc := f.with(domain_status_page.WithResolver(dns))
		page := f.page()
		ctx := context.Background()
		add(t, svc, page, "status.example.com")

		require.NoError(t, svc.CheckDue(ctx))
		assert.Zero(t, dns.lookup)
		assert.False(t, svc.VerificationRequired())
		served, err := svc.FindServedByDomain(ctx, "status.example.com")
		require.NoError(t, err)
		assert.NotNil(t, served, "a pending domain serves while verification is off")

		stop, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan struct{})
		go func() { svc.Run(stop); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run must return at once while verification is off")
		}
	})
}

func TestVerification_OnlyAVerifiedDomainIsServed(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		ctx := context.Background()
		added := add(t, f.svc, page, "status.example.com")

		served, err := v.svc.FindServedByDomain(ctx, "status.example.com")
		require.NoError(t, err)
		assert.Nil(t, served)
		listed, err := v.svc.FindByDomain(ctx, "status.example.com")
		require.NoError(t, err)
		assert.NotNil(t, listed, "the name stays taken while it is pending")

		v.dns.txt["_peekaping-verify.status.example.com"] = []string{"peekaping-verify=" + added.VerificationToken}
		_, err = v.svc.CheckNow(ctx, page, "status.example.com")
		require.NoError(t, err)
		served, err = v.svc.FindServedByDomain(ctx, " STATUS.example.com")
		require.NoError(t, err)
		assert.NotNil(t, served)
	})
}

// Turning verification on for an install with domains puts them in Pending, and they get their
// week from the first check, not from when they were added (PP-165, AC7).
func TestVerification_DomainsFromBeforeStartPendingWithAWeekFromTheFirstCheck(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		page := f.page()
		ctx := context.Background()
		old := add(t, f.svc, page, "old.example.com")
		// A row as it was before this change: no token, never checked, added a year ago
		_, err := f.db.ExecContext(ctx, "UPDATE domain_status_page SET verification_token = '', created_at = ? WHERE id = ?", t0.AddDate(-1, 0, 0), old.ID)
		require.NoError(t, err)

		v := f.verifying("peek.example")
		served, err := v.svc.FindServedByDomain(ctx, "old.example.com")
		require.NoError(t, err)
		assert.Nil(t, served, "pending at once")

		require.NoError(t, v.svc.CheckDue(ctx))
		s := v.status(t, page, "old.example.com")
		assert.Equal(t, "pending", s.State)
		assert.Equal(t, t0.Add(7*24*time.Hour), s.RemoveAt.UTC(), "the week starts at the first check")
		assert.Len(t, domainsOf(t, f.svc, page), 1)

		v.dns.cname["old.example.com"] = "peek.example."
		v.clock.now = t0.Add(5 * time.Minute)
		require.NoError(t, v.svc.CheckDue(ctx))
		assert.Equal(t, "verified", v.status(t, page, "old.example.com").State, "a domain already pointing here verifies on the next run")
	})
}

func TestVerification_RunChecksAtOnceAndStopsWhenItsContextIsDone(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		v := f.verifying("peek.example")
		page := f.page()
		added := add(t, f.svc, page, "status.example.com")
		v.dns.txt["_peekaping-verify.status.example.com"] = []string{"peekaping-verify=" + added.VerificationToken}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { v.svc.Run(ctx); close(done) }()
		assert.Eventually(t, func() bool { return v.status(t, page, "status.example.com").State == "verified" }, 5*time.Second, 10*time.Millisecond, "the first pass is not five minutes away")
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not stop")
		}
	})
}
