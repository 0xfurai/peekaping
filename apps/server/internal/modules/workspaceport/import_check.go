package workspaceport

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/netguard"
	"peekaping/internal/validation"

	"github.com/go-playground/validator/v10"
)

// What an import holds a file to before it looks at the workspace or the install: the file is of a
// format and version this server reads, its refs are whole and its links point at records it holds,
// and each record is valid in itself. It collects every problem and stops at none.

// docRefs are the refs the document holds, by kind
type docRefs struct {
	tags, proxies, channels, monitors map[string]bool
}

func (s *ServiceImpl) checkFile(doc *Document, found *problems) {
	checkHeader(doc, found)
	refs := checkRefs(doc, found)
	checkLinks(doc, refs, found)
	checkTags(doc, found)
	checkProxies(doc, found)
	s.checkChannels(doc, found)
	s.checkMonitors(doc, found)
	s.checkMaintenances(doc, found)
	checkStatusPages(doc, found)
	checkSettings(doc, found)
}

func checkHeader(doc *Document, found *problems) {
	if doc.Format != FormatName {
		found.add(RecordFile, "", "format is %q, not %q", doc.Format, FormatName)
	}
	if doc.Version < 1 || doc.Version > Version {
		found.add(RecordFile, "", "version %d is not one this server reads (1 to %d)", doc.Version, Version)
	}
}

func checkRefs(doc *Document, found *problems) docRefs {
	return docRefs{
		tags:     checkKindRefs(found, RecordTag, doc.Tags, func(t Tag) string { return t.Ref }),
		proxies:  checkKindRefs(found, RecordProxy, doc.Proxies, func(p Proxy) string { return p.Ref }),
		channels: checkKindRefs(found, RecordNotificationChannel, doc.NotificationChannels, func(c NotificationChannel) string { return c.Ref }),
		monitors: checkKindRefs(found, RecordMonitor, doc.Monitors, func(m Monitor) string { return m.Ref }),
	}
}

// checkKindRefs reports the records of a kind with no ref or a ref another has, and returns the
// refs. The maintenance windows and status pages are no one's link targets, but their refs must be
// whole too.
func checkKindRefs[T any](found *problems, record string, records []T, ref func(T) string) map[string]bool {
	known := make(map[string]bool, len(records))
	for i, r := range records {
		switch value := ref(r); {
		case value == "":
			found.add(record, "", "%s %d has no ref", record, i+1)
		case known[value]:
			found.add(record, value, "ref is used by more than one %s", record)
		default:
			known[value] = true
		}
	}
	return known
}

func checkLinks(doc *Document, refs docRefs, found *problems) {
	for _, m := range doc.Monitors {
		if m.ProxyRef != "" {
			requireRefs(found, RecordMonitor, m.Ref, "proxy", refs.proxies, []string{m.ProxyRef})
		}
		requireRefs(found, RecordMonitor, m.Ref, "notification channel", refs.channels, m.NotificationChannelRefs)
		requireRefs(found, RecordMonitor, m.Ref, "tag", refs.tags, m.TagRefs)
	}
	for _, w := range doc.Maintenances {
		requireRefs(found, RecordMaintenance, w.Ref, "monitor", refs.monitors, w.MonitorRefs)
	}
	for _, p := range doc.StatusPages {
		requireRefs(found, RecordStatusPage, p.Ref, "monitor", refs.monitors, p.MonitorRefs)
	}
	checkKindRefs(found, RecordMaintenance, doc.Maintenances, func(w Maintenance) string { return w.Ref })
	checkKindRefs(found, RecordStatusPage, doc.StatusPages, func(p StatusPage) string { return p.Ref })
}

// requireRefs reports each ref that names no record of the kind in the document
func requireRefs(found *problems, record, ref, kind string, known map[string]bool, links []string) {
	for _, link := range links {
		if !known[link] {
			found.add(record, ref, "links to %s %q, which is not in the file", kind, link)
		}
	}
}

func checkTags(doc *Document, found *problems) {
	names := map[string]bool{}
	for _, t := range doc.Tags {
		if err := validation.Validate.Struct(&tag.CreateUpdateDto{Name: t.Name, Color: t.Color, Description: t.Description}); err != nil {
			found.add(RecordTag, t.Ref, "%s", describe(err))
		}
		if names[t.Name] {
			found.add(RecordTag, t.Ref, "name %q is used by more than one tag", t.Name)
		}
		names[t.Name] = true
	}
}

func checkProxies(doc *Document, found *problems) {
	for _, p := range doc.Proxies {
		dto := &proxy.CreateUpdateDto{Protocol: p.Protocol, Host: p.Host, Port: p.Port, Auth: p.Auth, Username: p.Username, Password: p.Password}
		if err := validation.Validate.Struct(dto); err != nil {
			found.add(RecordProxy, p.Ref, "%s", describe(err))
		}
	}
}

func (s *ServiceImpl) checkChannels(doc *Document, found *problems) {
	for _, c := range doc.NotificationChannels {
		config, err := configText(c.Config)
		if err == nil {
			err = s.deps.Providers.ValidateConfig(c.Type, config)
		}
		if err != nil {
			found.add(RecordNotificationChannel, c.Ref, "%s", err.Error())
		}
	}
}

// checkMonitors holds each monitor to its type's rules. A monitor of a type the install turns off
// is not held to them, since it won't be imported.
func (s *ServiceImpl) checkMonitors(doc *Document, found *problems) {
	for _, m := range doc.Monitors {
		if s.deps.MonitorService.TypeDisabled(m.Type) {
			continue
		}
		if err := s.monitorProblem(m); err != nil {
			found.add(RecordMonitor, m.Ref, "%s", err.Error())
		}
	}
}

// monitorProblem is what is wrong with the monitor in itself: its config for its type, and its
// location rule. A config that names a blocked target is the target's business, not the file's.
func (s *ServiceImpl) monitorProblem(m Monitor) error {
	dto, err := monitorDto(m)
	if err != nil {
		return err
	}
	var blocked *netguard.TargetError
	if err := s.deps.MonitorService.ValidateMonitorConfig(dto.Type, dto.Config); err != nil && !errors.As(err, &blocked) {
		return err
	}
	return monitor.CheckLocationRuleShape(dto.LocationRule, dto.LocationThreshold)
}

func (s *ServiceImpl) checkMaintenances(doc *Document, found *problems) {
	for _, w := range doc.Maintenances {
		if err := s.deps.MaintenanceService.Check(maintenanceModel(w)); err != nil {
			found.add(RecordMaintenance, w.Ref, "%s", describe(err))
		}
	}
}

func checkStatusPages(doc *Document, found *problems) {
	for _, p := range doc.StatusPages {
		if err := validation.Validate.Struct(statusPageDto(p)); err != nil {
			found.add(RecordStatusPage, p.Ref, "%s", describe(err))
		}
	}
}

func checkSettings(doc *Document, found *problems) {
	if days := doc.Settings.DataRetentionDays; days != 0 {
		if _, err := setting.ParseDataRetentionPeriod(strconv.Itoa(days)); err != nil {
			found.add(RecordSettings, "", "dataRetentionDays: %s", err.Error())
		}
	}
	if slices.ContainsFunc(doc.Settings.CertExpiryNotifyDays, func(days int) bool { return days < 0 }) {
		found.add(RecordSettings, "", "certExpiryNotifyDays can't hold a negative number of days")
	}
}

// configText is a config as the database stores it, once it is known to be a JSON object
func configText(c ConfigJSON) (string, error) {
	var object map[string]json.RawMessage
	if len(c) > 0 && json.Unmarshal(c, &object) != nil {
		return "", errors.New("config must be a JSON object")
	}
	if object == nil {
		return "{}", nil
	}
	return c.String(), nil
}

// describe is a validation error in a few words, without the validator's struct path
func describe(err error) string {
	var invalid validator.ValidationErrors
	if !errors.As(err, &invalid) {
		return err.Error()
	}
	reasons := make([]string, len(invalid))
	for i, fe := range invalid {
		reasons[i] = fe.Field() + " fails " + fe.Tag()
		if fe.Param() != "" {
			reasons[i] += "=" + fe.Param()
		}
	}
	return strings.Join(reasons, ", ")
}

// monitorDto is the monitor as the create route's body, without its links or probes
func monitorDto(m Monitor) (*monitor.CreateUpdateDto, error) {
	config, err := configText(m.Config)
	if err != nil {
		return nil, err
	}
	dto := &monitor.CreateUpdateDto{
		Type: m.Type, Name: m.Name, Interval: m.Interval, MaxRetries: m.MaxRetries, RetryInterval: m.RetryInterval,
		Timeout: m.Timeout, ResendInterval: m.ResendInterval, Active: m.Active, NotificationIds: []string{}, TagIds: []string{},
		Config: config, PushToken: m.PushToken, LocationRule: shared.LocationRule(m.LocationRule), LocationThreshold: m.LocationThreshold,
	}
	dto.ApplyPushToken()
	return dto, nil
}

func maintenanceModel(w Maintenance) *maintenance.Model {
	return &maintenance.Model{
		Title: w.Title, Description: w.Description, Active: w.Active, Strategy: w.Strategy,
		StartDateTime: w.StartDateTime, EndDateTime: w.EndDateTime, StartTime: w.StartTime, EndTime: w.EndTime,
		Weekdays: w.Weekdays, DaysOfMonth: w.DaysOfMonth, IntervalDay: w.IntervalDay,
		Cron: w.Cron, Timezone: w.Timezone, Duration: w.Duration,
	}
}

func statusPageDto(p StatusPage) *status_page.CreateStatusPageDTO {
	return &status_page.CreateStatusPageDTO{
		Slug: p.Slug, Title: p.Title, Description: p.Description, Icon: p.Icon, Theme: p.Theme, Published: p.Published,
		ShowLocations: p.ShowLocations, FooterText: p.FooterText, AutoRefreshInterval: p.AutoRefreshInterval,
	}
}
