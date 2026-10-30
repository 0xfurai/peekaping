package workspaceport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/workspace"
)

// writer makes the records of a plan inside one transaction. A record that links to others finds
// their IDs by the refs of the file, in the maps of the records made before it; a ref with no ID
// belongs to a record that was left out, and the link is dropped.
type writer struct {
	s    *ServiceImpl
	id   workspace.ID
	plan *importPlan

	tagIDs, proxyIDs, channelIDs, monitorIDs map[string]string
	created                                  Created
	monitors                                 []*monitor.Model
}

// write makes the plan's records in one transaction. It returns the monitors it made, to be
// published once the transaction has committed.
func (s *ServiceImpl) write(ctx context.Context, id workspace.ID, plan *importPlan) (*ImportResult, []*monitor.Model, error) {
	w := &writer{
		s: s, id: id, plan: plan,
		tagIDs: plan.reusedTags, proxyIDs: map[string]string{}, channelIDs: map[string]string{}, monitorIDs: map[string]string{},
	}
	err := s.deps.Transactor.WithTx(ctx, func(ctx context.Context) error {
		for _, step := range []func(context.Context) error{w.settings, w.tags, w.proxies, w.channels, w.monitorRecords, w.maintenances, w.statusPages} {
			if err := step(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	changes := plan.changes
	if changes == nil {
		changes = []Change{}
	}
	return &ImportResult{Created: w.created, Changes: changes}, w.monitors, nil
}

func (w *writer) settings(ctx context.Context) error {
	if days := w.plan.settings.retentionDays; days != 0 {
		model := &shared.SettingModel{Value: strconv.Itoa(days), Type: "int"}
		if _, err := w.s.deps.Settings.SetByKey(ctx, w.id, workspace.DataRetentionPeriodKey, model); err != nil {
			return fmt.Errorf("store the data retention period: %w", err)
		}
	}
	if notify := w.plan.settings.certDays; notify != nil {
		encoded, err := json.Marshal(notify)
		if err != nil {
			return err
		}
		if _, err := w.s.deps.Settings.SetByKey(ctx, w.id, workspace.CertExpiryNotifyDaysKey, &shared.SettingModel{Value: string(encoded), Type: "json"}); err != nil {
			return fmt.Errorf("store the certificate expiry days: %w", err)
		}
	}
	return nil
}

func (w *writer) tags(ctx context.Context) error {
	for _, t := range w.plan.tags {
		made, err := w.s.deps.TagService.Create(ctx, w.id, &tag.CreateUpdateDto{Name: t.Name, Color: t.Color, Description: t.Description})
		if err != nil {
			return fmt.Errorf("create tag %s: %w", t.Ref, err)
		}
		w.tagIDs[t.Ref] = made.ID
		w.created.Tags++
	}
	return nil
}

func (w *writer) proxies(ctx context.Context) error {
	for _, p := range w.plan.proxies {
		dto := &proxy.CreateUpdateDto{Protocol: p.Protocol, Host: p.Host, Port: p.Port, Auth: p.Auth, Username: p.Username, Password: p.Password}
		made, err := w.s.deps.ProxyService.Create(ctx, w.id, dto)
		if err != nil {
			return fmt.Errorf("create proxy %s: %w", p.Ref, err)
		}
		w.proxyIDs[p.Ref] = made.ID
		w.created.Proxies++
	}
	return nil
}

func (w *writer) channels(ctx context.Context) error {
	for _, c := range w.plan.channels {
		config, err := configText(c.Config)
		if err != nil {
			return err
		}
		dto := &notification_channel.CreateUpdateDto{Name: c.Name, Type: c.Type, Active: c.Active, IsDefault: c.IsDefault, Config: config}
		made, err := w.s.deps.ChannelService.Create(ctx, w.id, dto)
		if err != nil {
			return fmt.Errorf("create notification channel %s: %w", c.Ref, err)
		}
		w.channelIDs[c.Ref] = made.ID
		w.created.NotificationChannels++
	}
	return nil
}

// resolve turns the refs of a file into the IDs of the records made, leaving out those that were not
func resolve(refs []string, ids map[string]string) []string {
	resolved := make([]string, 0, len(refs))
	for _, ref := range refs {
		if id, ok := ids[ref]; ok {
			resolved = append(resolved, id)
		}
	}
	return resolved
}

func (w *writer) monitorRecords(ctx context.Context) error {
	for _, m := range w.plan.monitors {
		m.dto.NotificationIds = resolve(m.channelRefs, w.channelIDs)
		m.dto.TagIds = resolve(m.tagRefs, w.tagIDs)
		m.dto.ProxyId = w.proxyIDs[m.proxyRef]
		made, err := w.s.deps.MonitorService.CreateWithoutEvent(ctx, w.id, m.dto)
		if err != nil {
			return refuseMonitor(m.ref, err)
		}
		w.monitorIDs[m.ref] = made.ID
		w.monitors = append(w.monitors, made)
		w.created.Monitors++
	}
	return nil
}

// refuseMonitor turns a refusal of the monitor service that is about the monitor, and not about
// the install's state, into a problem with the file
func refuseMonitor(ref string, err error) error {
	var rule *monitor.InvalidLocationRuleError
	var probes *monitor.InvalidProbesError
	var token *monitor.PushTokenAlreadyUsedError
	if errors.As(err, &rule) || errors.As(err, &probes) || errors.As(err, &token) {
		return &InvalidFileError{Problems: []Problem{{Record: RecordMonitor, Ref: ref, Message: err.Error()}}}
	}
	return fmt.Errorf("create monitor %s: %w", ref, err)
}

func (w *writer) maintenances(ctx context.Context) error {
	for _, window := range w.plan.maintenances {
		monitorIDs := resolve(window.MonitorRefs, w.monitorIDs)
		if _, err := w.s.deps.MaintenanceService.Create(ctx, w.id, maintenanceModel(window), monitorIDs); err != nil {
			return fmt.Errorf("create maintenance window %s: %w", window.Ref, err)
		}
		w.created.Maintenances++
	}
	return nil
}

func (w *writer) statusPages(ctx context.Context) error {
	for _, p := range w.plan.pages {
		p.dto.MonitorIDs = resolve(p.monitorRefs, w.monitorIDs)
		if _, err := w.s.deps.StatusPageService.CreateWithLinks(ctx, w.id, p.dto); err != nil {
			return fmt.Errorf("create status page %s: %w", p.ref, err)
		}
		w.created.StatusPages++
	}
	return nil
}
