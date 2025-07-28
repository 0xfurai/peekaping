package bruteforce

import "time"

// Model represents the login state for bruteforce protection
type Model struct {
	Key         string
	FailCount   int
	FirstFailAt time.Time
	LockedUntil *time.Time
}

// UpdateModel represents fields that can be updated
type UpdateModel struct {
	FailCount   *int
	FirstFailAt *time.Time
	LockedUntil *time.Time
}
