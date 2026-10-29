// Package limits says what a workspace is held to (its plan, PP-160): how many monitors it may
// have, its shortest check interval, how many probes a monitor of it runs on, and so on. The
// services that create or change those records ask a Source, keyed by the workspace's ID, and
// enforce what it answers.
//
// A workspace's limits come from the first of these that has any:
//  1. the workspace's own limits, which an instance admin sets ("no limits" is a value of its own)
//  2. the install's defaults (the WORKSPACE_LIMIT_* environment variables)
//  3. none
//
// So an install that sets nothing has no limits. A paid plan can be one more source in front of
// the others later, without the services changing.
package limits

import (
	"context"
	"fmt"
	"net/http"

	"peekaping/internal/httpx"
	"peekaping/internal/modules/workspace/workspaceid"
)

// Name is a limit as the API names it
type Name string

const (
	// Monitors is how many active monitors a workspace may have. A paused monitor doesn't count.
	Monitors Name = "monitors"
	// MinCheckInterval is the shortest check interval and retry interval, in seconds, a monitor of
	// the workspace may have. The install's MIN_CHECK_INTERVAL applies as well: the stricter wins.
	MinCheckInterval Name = "min_check_interval"
	// StatusPages is how many status pages a workspace may have
	StatusPages Name = "status_pages"
	// CustomDomains is how many custom domains a workspace's status pages may have, together
	CustomDomains Name = "custom_domains"
	// Members is how many members a workspace may have
	Members Name = "members"
	// OwnedWorkspaces is how many workspaces a user who owns this one may own
	OwnedWorkspaces Name = "owned_workspaces"
	// Probes is how many probes one monitor of the workspace may run on
	Probes Name = "probes"
	// RetentionDays is the longest data retention period, in days, a workspace may keep
	RetentionDays Name = "retention_days"
)

// Limits are what a workspace is held to. A nil field is no limit, and so is the zero Limits.
type Limits struct {
	Monitors         *int `json:"monitors,omitempty"`
	MinCheckInterval *int `json:"min_check_interval,omitempty"`
	StatusPages      *int `json:"status_pages,omitempty"`
	CustomDomains    *int `json:"custom_domains,omitempty"`
	Members          *int `json:"members,omitempty"`
	OwnedWorkspaces  *int `json:"owned_workspaces,omitempty"`
	ProbesPerMonitor *int `json:"probes_per_monitor,omitempty"`
	RetentionDays    *int `json:"retention_days,omitempty"`
}

// fields are the limits by name, for the code that treats them all alike
func (l *Limits) fields() map[Name]**int {
	return map[Name]**int{
		Monitors:         &l.Monitors,
		MinCheckInterval: &l.MinCheckInterval,
		StatusPages:      &l.StatusPages,
		CustomDomains:    &l.CustomDomains,
		Members:          &l.Members,
		OwnedWorkspaces:  &l.OwnedWorkspaces,
		Probes:           &l.ProbesPerMonitor,
		RetentionDays:    &l.RetentionDays,
	}
}

// normalized is l with every value of 0 or less made no limit, which is what such a value means
// wherever it comes from
func (l Limits) normalized() Limits {
	for _, field := range l.fields() {
		if *field != nil && **field <= 0 {
			*field = nil
		}
	}
	return l
}

// IsZero reports whether l holds no limit at all
func (l Limits) IsZero() bool {
	for _, field := range l.fields() {
		if *field != nil {
			return false
		}
	}
	return true
}

// Of is a limit as a value: n, or no limit for 0 or less
func Of(n int) *int {
	if n <= 0 {
		return nil
	}
	return &n
}

// Origin is where a workspace's limits come from
type Origin string

const (
	// OriginWorkspace is the workspace's own limits, which an instance admin set
	OriginWorkspace Origin = "workspace"
	// OriginInstall is the install's defaults
	OriginInstall Origin = "install"
	// OriginNone is no limits at all: neither the workspace nor the install sets any
	OriginNone Origin = "none"
)

// Source says what each workspace is held to
type Source interface {
	// For returns the workspace's limits and where they come from
	For(ctx context.Context, workspaceID workspaceid.ID) (Limits, Origin, error)
	// Snapshot returns every workspace's limits at once, for a read that covers many workspaces:
	// one lookup and then none for each row
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot is every workspace's limits at one moment
type Snapshot struct {
	// Defaults are the limits of a workspace that has none of its own
	Defaults Limits
	// Own are the workspaces that have limits of their own, which replace the defaults whole
	Own map[workspaceid.ID]Limits
}

// For returns the workspace's limits in the snapshot and where they come from
func (s Snapshot) For(workspaceID workspaceid.ID) (Limits, Origin) {
	if own, ok := s.Own[workspaceID]; ok {
		return own, OriginWorkspace
	}
	if !s.Defaults.IsZero() {
		return s.Defaults, OriginInstall
	}
	return Limits{}, OriginNone
}

// Lowest is the smallest value any workspace has for the limit, and nil when no workspace has
// one: a read that covers every workspace can skip the limit then
func (s Snapshot) Lowest(name Name) *int {
	var lowest *int
	consider := func(l Limits) {
		if value := *l.fields()[name]; value != nil && (lowest == nil || *value < *lowest) {
			lowest = value
		}
	}
	consider(s.Defaults)
	for _, own := range s.Own {
		consider(own)
	}
	return lowest
}

// ReachedError refuses a write that would take the workspace over one of its limits. The
// services return it, and the controllers answer 403 with it as the error, through Fail.
type ReachedError struct {
	Code  string `json:"code" example:"LIMIT_REACHED"`
	Limit Name   `json:"limit" enum:"monitors,status_pages,custom_domains,members,owned_workspaces,probes" doc:"The limit the request would go over." example:"monitors"`
	Max   int    `json:"max" doc:"The limit's value for the workspace." example:"10"`
}

func (e *ReachedError) Error() string {
	switch e.Limit {
	case Monitors:
		return fmt.Sprintf("the workspace has reached its limit of %d active monitors", e.Max)
	case Probes:
		return fmt.Sprintf("a monitor of this workspace runs on at most %d probes", e.Max)
	default:
		return fmt.Sprintf("the workspace has reached its limit of %d %s", e.Max, e.Limit)
	}
}

// Reached is the refusal of a write over the limit, whose value is max
func Reached(limit Name, max int) *ReachedError {
	return &ReachedError{Code: "LIMIT_REACHED", Limit: limit, Max: max}
}

// ReachedResponse is the answer to a write over one of the workspace's limits: a 403 with the
// usual message, which names the limit and its value, and both in the error
type ReachedResponse struct {
	Message string       `json:"message" example:"the workspace has reached its limit of 10 active monitors"`
	Data    httpx.Null   `json:"data"`
	Error   *ReachedError `json:"error"`
}

// Fail is the 403 for a write over the limit
func Fail(err *ReachedError) error {
	return httpx.Fail(http.StatusForbidden, ReachedResponse{Message: err.Error(), Error: err})
}
