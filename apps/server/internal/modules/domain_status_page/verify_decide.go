package domain_status_page

import "time"

const (
	// FailingGrace is how long a Verified domain's records may be missing before it is Pending again
	FailingGrace = 24 * time.Hour
	// PendingLife is how long a domain may stay Pending, counted in days of checks that found it
	// unverified, before it is removed from its page. Days when no check ran (verification was off,
	// or the server was down) don't count: see RecheckEvery.
	PendingLife = 7 * 24 * time.Hour
	// RecheckEvery is how long a Verified domain goes unchecked. A Pending domain is checked every
	// CheckEvery, so a Pending domain whose last check is older than this had a pause in its checks.
	RecheckEvery = time.Hour
)

// Decide is the state a domain moves to after a check that looked for its records at now, or
// remove when the domain has run out of time and goes from its page. It changes nothing in m.
//
// Records found verify the domain, and clear its failure and its pending clock. Records missing
// start a Verified domain's grace, and send it back to Pending when FailingGrace has passed; they
// start a Pending domain's life, and remove it when PendingLife has passed. The life restarts at
// the first miss after a pause in the checks (m.LastCheckedAt nil, or older than RecheckEvery), so
// the days a domain is removed after are days of checks. enforce is whether verification is on:
// with it off a miss is only recorded, since the domain serves anyway and its days must not run
// while nobody asks for the records.
func Decide(m *Model, found bool, now time.Time, enforce bool) (next Model, remove bool) {
	next = *m
	previous := m.LastCheckedAt
	next.LastCheckedAt = &now
	switch {
	case found:
		next.markFound(now)
	case !enforce:
	case next.Verified():
		next.markVerifiedMissing(now)
	default:
		remove = next.markPendingMissing(now, previous)
	}
	return next, remove
}

func (m *Model) markFound(now time.Time) {
	if m.VerifiedAt == nil {
		m.VerifiedAt = &now
	}
	m.FailingSince, m.PendingSince = nil, nil
}

func (m *Model) markVerifiedMissing(now time.Time) {
	if m.FailingSince == nil {
		m.FailingSince = &now
	}
	if now.Sub(*m.FailingSince) >= FailingGrace {
		m.VerifiedAt, m.FailingSince, m.PendingSince = nil, nil, &now
	}
}

// markPendingMissing starts a Pending domain's life, or starts it again when the checks paused since
// previous, and reports whether it is over
func (m *Model) markPendingMissing(now time.Time, previous *time.Time) bool {
	if m.PendingSince == nil || previous == nil || now.Sub(*previous) > RecheckEvery {
		m.PendingSince = &now
	}
	return now.Sub(*m.PendingSince) >= PendingLife
}
