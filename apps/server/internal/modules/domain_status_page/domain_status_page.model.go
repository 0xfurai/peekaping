package domain_status_page

import (
	"time"
)

type Model struct {
	ID           string    `json:"id"`
	StatusPageID string    `json:"status_page_id"`
	Domain       string    `json:"domain"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// VerificationToken is the secret the domain's owner puts in a TXT record to prove the domain
	// is theirs (PP-165). It is empty on a row from before verification until the first time the
	// server shows or checks it (see Service.EnsureToken). It never leaves the server except to the
	// workspace that holds the domain, so it has no JSON name.
	VerificationToken string `json:"-"`
	// VerifiedAt is when the domain was found pointing at this install: nil while it is Pending
	VerifiedAt *time.Time `json:"verified_at"`
	// PendingSince is when a check first found the Pending domain unverified with verification on:
	// its days to verify count from here, so a domain that was added while verification was off
	// doesn't run out before it was ever asked for. Nil until then, and once the domain is Verified.
	PendingSince *time.Time `json:"pending_since"`
	// FailingSince is when a check first found a Verified domain's records missing, nil while they
	// are there
	FailingSince *time.Time `json:"failing_since"`
	// LastCheckedAt is the last time a check looked for the domain's records, nil before the first
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

// Verified reports whether the domain was found pointing at this install
func (m *Model) Verified() bool { return m.VerifiedAt != nil }

type UpdateModel struct {
	StatusPageID *string `json:"status_page_id,omitempty"`
	Domain       *string `json:"domain,omitempty"`
}
