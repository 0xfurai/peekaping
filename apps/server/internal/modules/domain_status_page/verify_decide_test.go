package domain_status_page_test

import (
	"testing"
	"time"

	"peekaping/internal/modules/domain_status_page"

	"github.com/stretchr/testify/assert"
)

var t0 = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func at(t time.Time) *time.Time { return &t }

func TestDecide_FoundVerifiesAPendingDomain(t *testing.T) {
	m := &domain_status_page.Model{PendingSince: at(t0.Add(-48 * time.Hour))}
	next, remove := domain_status_page.Decide(m, true, t0, true)
	assert.False(t, remove)
	assert.Equal(t, at(t0), next.VerifiedAt)
	assert.Nil(t, next.PendingSince)
	assert.Nil(t, next.FailingSince)
	assert.Equal(t, at(t0), next.LastCheckedAt)
}

func TestDecide_FoundClearsAVerifiedDomainsFailure(t *testing.T) {
	verified := t0.Add(-72 * time.Hour)
	m := &domain_status_page.Model{VerifiedAt: at(verified), FailingSince: at(t0.Add(-5 * time.Hour))}
	next, remove := domain_status_page.Decide(m, true, t0, true)
	assert.False(t, remove)
	assert.Equal(t, at(verified), next.VerifiedAt, "the first verification time stays")
	assert.Nil(t, next.FailingSince)
	assert.Equal(t, at(t0), next.LastCheckedAt)
}

func TestDecide_MissingRecordsStartTheGraceOfAVerifiedDomain(t *testing.T) {
	m := &domain_status_page.Model{VerifiedAt: at(t0.Add(-72 * time.Hour))}
	next, remove := domain_status_page.Decide(m, false, t0, true)
	assert.False(t, remove)
	assert.True(t, next.Verified(), "one miss doesn't unserve a domain")
	assert.Equal(t, at(t0), next.FailingSince)
	assert.Equal(t, at(t0), next.LastCheckedAt)
}

func TestDecide_AVerifiedDomainFailingFor23HoursStaysVerified(t *testing.T) {
	failing := t0.Add(-23 * time.Hour)
	m := &domain_status_page.Model{VerifiedAt: at(t0.Add(-72 * time.Hour)), FailingSince: at(failing)}
	next, remove := domain_status_page.Decide(m, false, t0, true)
	assert.False(t, remove)
	assert.True(t, next.Verified())
	assert.Equal(t, at(failing), next.FailingSince, "the failure keeps counting from its first miss")
}

func TestDecide_AVerifiedDomainFailingFor24HoursGoesBackToPending(t *testing.T) {
	m := &domain_status_page.Model{VerifiedAt: at(t0.Add(-72 * time.Hour)), FailingSince: at(t0.Add(-24 * time.Hour))}
	next, remove := domain_status_page.Decide(m, false, t0, true)
	assert.False(t, remove)
	assert.False(t, next.Verified())
	assert.Nil(t, next.FailingSince)
	assert.Equal(t, at(t0), next.PendingSince, "its week to verify again starts now")
}

func TestDecide_APendingDomainStartsItsWeekAtItsFirstCheck(t *testing.T) {
	next, remove := domain_status_page.Decide(&domain_status_page.Model{}, false, t0, true)
	assert.False(t, remove)
	assert.False(t, next.Verified())
	assert.Equal(t, at(t0), next.PendingSince)
	assert.Equal(t, at(t0), next.LastCheckedAt)
}

func TestDecide_APendingDomainIsRemovedAfterSevenDays(t *testing.T) {
	week := 7 * 24 * time.Hour
	lastCheck := at(t0.Add(-5 * time.Minute))
	m := &domain_status_page.Model{PendingSince: at(t0.Add(-week + time.Minute)), LastCheckedAt: lastCheck}
	next, remove := domain_status_page.Decide(m, false, t0, true)
	assert.False(t, remove, "7 days minus a minute")
	assert.Equal(t, at(t0.Add(-week+time.Minute)), next.PendingSince)

	m = &domain_status_page.Model{PendingSince: at(t0.Add(-week)), LastCheckedAt: lastCheck}
	_, remove = domain_status_page.Decide(m, false, t0, true)
	assert.True(t, remove, "7 days")
}

// Checks that were not running (verification off, or the server down) don't count against a
// Pending domain's week: its clock starts again at the first check after the pause (PP-165, AC7/AC8).
func TestDecide_APendingDomainsWeekRestartsAfterAGapInChecks(t *testing.T) {
	eightDays := 8 * 24 * time.Hour
	m := &domain_status_page.Model{PendingSince: at(t0.Add(-eightDays)), LastCheckedAt: at(t0.Add(-eightDays))}
	next, remove := domain_status_page.Decide(m, false, t0, true)
	assert.False(t, remove, "the week was mostly not checked")
	assert.Equal(t, at(t0), next.PendingSince)

	m = &domain_status_page.Model{PendingSince: at(t0.Add(-eightDays))}
	next, remove = domain_status_page.Decide(m, false, t0, true)
	assert.False(t, remove, "never checked since its clock was set")
	assert.Equal(t, at(t0), next.PendingSince)

	m = &domain_status_page.Model{PendingSince: at(t0.Add(-8 * 24 * time.Hour)), LastCheckedAt: at(t0.Add(-domain_status_page.RecheckEvery))}
	_, remove = domain_status_page.Decide(m, false, t0, true)
	assert.True(t, remove, "a last check an hour ago is no gap")
}

func TestDecide_WithoutEnforcementAMissIsOnlyRecorded(t *testing.T) {
	pending := &domain_status_page.Model{PendingSince: at(t0.Add(-30 * 24 * time.Hour))}
	next, remove := domain_status_page.Decide(pending, false, t0, false)
	assert.False(t, remove)
	assert.Equal(t, pending.PendingSince, next.PendingSince)
	assert.Equal(t, at(t0), next.LastCheckedAt)

	failing := &domain_status_page.Model{VerifiedAt: at(t0.Add(-72 * time.Hour)), FailingSince: at(t0.Add(-30 * 24 * time.Hour))}
	next, remove = domain_status_page.Decide(failing, false, t0, false)
	assert.False(t, remove)
	assert.True(t, next.Verified())

	next, _ = domain_status_page.Decide(&domain_status_page.Model{}, true, t0, false)
	assert.True(t, next.Verified(), "a domain found is verified whether or not verification is on")
}

func TestDecide_DoesNotChangeItsInput(t *testing.T) {
	m := &domain_status_page.Model{}
	domain_status_page.Decide(m, true, t0, true)
	assert.Nil(t, m.VerifiedAt)
	assert.Nil(t, m.LastCheckedAt)
}
