// Package error_reporting is the admin's switch for error reports (ticket #233, ADR 0003): the
// API that reads and changes the choice, the setting that stores it, and how the choice reaches
// the processes that report. The reports themselves are made by package reporting.
package error_reporting

import (
	"context"

	"peekaping/internal/reporting"
	"peekaping/internal/reporting/redissync"

	"github.com/redis/go-redis/v9"
)

// EnabledKey is the setting that holds "true" while an admin has opted in to error reports and
// "false" after they opted out. It is not user-editable, so the settings API can't reach it: the
// error reporting API is the only way to read or change it.
const EnabledKey = "ERROR_REPORTING_ENABLED"

// State is what the error reporting API reports: the admin's choice, and whether the
// environment overrides it.
type State struct {
	// Enabled is the admin's stored choice. It stays what they chose while ForcedOff is true.
	Enabled bool
	// ForcedOff is true when ERROR_REPORTING or DO_NOT_TRACK switches error reports off whatever
	// Enabled says
	ForcedOff bool
}

// Switch tells the processes that report whether to. Single mode has one process, and cluster
// mode has four kinds of them.
type Switch interface {
	// Set turns reporting on or off in this process and in the others. on is the effective choice:
	// false while the environment forces reporting off.
	Set(ctx context.Context, on bool) error
}

// LocalSwitch is single mode's switch: the one process is the only one to tell.
type LocalSwitch struct{}

func (LocalSwitch) Set(_ context.Context, on bool) error {
	reporting.SetEnabled(on)
	return nil
}

// RedisSwitch is cluster mode's switch, in the api: it turns reporting on or off in the api and
// copies the choice to a Redis key, which every cluster process reads once a minute
// (redissync.Refresher).
type RedisSwitch struct {
	client *redis.Client
}

func NewRedisSwitch(client *redis.Client) *RedisSwitch { return &RedisSwitch{client: client} }

func (s *RedisSwitch) Set(ctx context.Context, on bool) error {
	reporting.SetEnabled(on)
	return redissync.Publish(ctx, s.client, on)
}
