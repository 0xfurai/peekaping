package workspaceport

import (
	"context"
	"errors"
	"strconv"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/netguard"
)

// monitorEnv is what the monitors of a file are fitted against: the shortest interval the workspace
// may set, the room left for active monitors, and the push tokens the file's monitors have taken
// so far
type monitorEnv struct {
	minInterval int
	// room is how many more active monitors the plan allows, and nil when it sets no limit
	room   *int
	tokens map[string]bool
}

func (f *fitter) monitorEnv(ctx context.Context) (*monitorEnv, error) {
	minInterval, err := f.s.deps.MonitorService.MinCheckInterval(ctx, f.id)
	if err != nil {
		return nil, err
	}
	env := &monitorEnv{minInterval: minInterval, tokens: map[string]bool{}}
	if f.limits.Monitors == nil {
		return env, nil
	}
	active, err := f.s.deps.Monitors.CountActive(ctx, f.id)
	if err != nil {
		return nil, err
	}
	room := max(*f.limits.Monitors-active, 0)
	env.room = &room
	return env, nil
}

// fitMonitors leaves out the monitors the install can't run, and changes the rest to fit the plan.
// The monitors that stay run on the install's default probes, as a monitor created without probes
// does.
func (f *fitter) fitMonitors(ctx context.Context) error {
	env, err := f.monitorEnv(ctx)
	if err != nil {
		return err
	}
	for _, m := range f.doc.Monitors {
		if err := f.fitMonitor(ctx, env, m); err != nil {
			return err
		}
	}
	return nil
}

func (f *fitter) fitMonitor(ctx context.Context, env *monitorEnv, m Monitor) error {
	dto, err := monitorDto(m)
	if err != nil {
		return nil // the check reported it
	}
	if f.skipMonitor(m, dto) {
		return nil
	}
	proxyRef := f.monitorProxy(m)
	f.raiseIntervals(env, m, dto)
	if err := f.replaceTakenPushToken(ctx, env, m, dto); err != nil {
		return err
	}
	f.pauseOverLimit(env, m, dto)
	if err := monitor.ValidateCreateDto(dto); err != nil {
		f.found.add(RecordMonitor, m.Ref, "%s", describe(err))
	}
	f.plan.monitors = append(f.plan.monitors, plannedMonitor{ref: m.Ref, dto: dto, proxyRef: proxyRef, channelRefs: m.NotificationChannelRefs, tagRefs: m.TagRefs})
	return nil
}

// skipMonitor leaves out a monitor of a type the install turns off, and one that targets an address
// the private target block refuses
func (f *fitter) skipMonitor(m Monitor, dto *monitor.CreateUpdateDto) bool {
	if f.s.deps.MonitorService.TypeDisabled(m.Type) {
		f.plan.add(Change{Kind: ChangeMonitorSkippedTypeDisabled, Record: RecordMonitor, Ref: m.Ref, Name: m.Name, Reason: m.Type})
		return true
	}
	var blocked *netguard.TargetError
	if err := f.s.deps.MonitorService.ValidateMonitorConfig(dto.Type, dto.Config); errors.As(err, &blocked) {
		f.plan.add(Change{Kind: ChangeMonitorSkippedTargetNotAllowed, Record: RecordMonitor, Ref: m.Ref, Name: m.Name, Reason: blocked.Host})
		return true
	}
	return false
}

// monitorProxy is the ref of the proxy the monitor keeps: a monitor whose proxy is left out runs
// without one
func (f *fitter) monitorProxy(m Monitor) string {
	address, skipped := f.skippedProxies[m.ProxyRef]
	if !skipped {
		return m.ProxyRef
	}
	f.plan.add(Change{Kind: ChangeMonitorProxyDropped, Record: RecordMonitor, Ref: m.Ref, Name: m.Name, From: address})
	return ""
}

// raiseIntervals raises the interval and the retry interval to the shortest the workspace may set
func (f *fitter) raiseIntervals(env *monitorEnv, m Monitor, dto *monitor.CreateUpdateDto) {
	f.raise(env, m, "interval", &dto.Interval)
	f.raise(env, m, "retryInterval", &dto.RetryInterval)
}

func (f *fitter) raise(env *monitorEnv, m Monitor, field string, seconds *int) {
	if *seconds >= env.minInterval {
		return
	}
	f.plan.add(Change{Kind: ChangeMonitorIntervalRaised, Record: RecordMonitor, Ref: m.Ref, Name: m.Name, From: strconv.Itoa(*seconds), To: strconv.Itoa(env.minInterval), Reason: field})
	*seconds = env.minInterval
}

// replaceTakenPushToken gives the monitor a new push token when another monitor of the install, or
// an earlier one of the file, has its token: push tokens are unique across the install
func (f *fitter) replaceTakenPushToken(ctx context.Context, env *monitorEnv, m Monitor, dto *monitor.CreateUpdateDto) error {
	if dto.PushToken == "" {
		return nil
	}
	taken := env.tokens[dto.PushToken]
	if !taken {
		var err error
		if taken, err = f.s.deps.MonitorService.PushTokenTaken(ctx, dto.PushToken); err != nil {
			return err
		}
	}
	if taken {
		dto.PushToken = monitor.NewPushToken()
		dto.ApplyPushToken()
		f.plan.add(Change{Kind: ChangePushTokenReplaced, Record: RecordMonitor, Ref: m.Ref, Name: m.Name})
	}
	env.tokens[dto.PushToken] = true
	return nil
}

// pauseOverLimit imports an active monitor paused once the workspace has as many active ones as its
// plan allows, in the order of the file
func (f *fitter) pauseOverLimit(env *monitorEnv, m Monitor, dto *monitor.CreateUpdateDto) {
	if !dto.Active || env.room == nil {
		return
	}
	if *env.room > 0 {
		*env.room--
		return
	}
	dto.Active = false
	f.plan.add(Change{Kind: ChangeMonitorPausedOverLimit, Record: RecordMonitor, Ref: m.Ref, Name: m.Name})
}
