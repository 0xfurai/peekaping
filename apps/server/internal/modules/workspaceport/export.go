package workspaceport

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/status_page"
	"peekaping/internal/modules/tag"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/version"
)

// pageSize is how many records of a kind one read of the export asks for
const pageSize = 500

// defaultCertExpiryNotifyDays are the days a workspace that stores none notifies at (the
// certificate service's default)
var defaultCertExpiryNotifyDays = []int{7, 14, 21}

func (s *ServiceImpl) Export(ctx context.Context) (*Document, error) {
	id, err := owner(ctx)
	if err != nil {
		return nil, err
	}
	doc := &Document{
		Format: FormatName, Version: Version, ExportedAt: s.now().UTC().Truncate(time.Second),
		AppVersion: version.Version, ContainsSecrets: true,
	}
	steps := []func(context.Context, workspace.ID, *Document) error{
		s.addWorkspace, s.addSettings, s.addTags, s.addProxies, s.addChannels, s.addMonitors, s.addMaintenances, s.addStatusPages,
	}
	for _, step := range steps {
		if err := step(ctx, id, doc); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

// readAll reads every record of a kind a page at a time
func readAll[T any](read func(page, limit int) ([]*T, error)) ([]*T, error) {
	var all []*T
	for page := 0; ; page++ {
		batch, err := read(page, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < pageSize {
			return all, nil
		}
	}
}

// byNameThenRef orders records by name, then by ref
func byNameThenRef[T any](records []T, name, ref func(T) string) {
	slices.SortFunc(records, func(a, b T) int {
		return cmp.Or(strings.Compare(name(a), name(b)), strings.Compare(ref(a), ref(b)))
	})
}

// refSet is the refs of records
func refSet[T any](records []T, ref func(T) string) map[string]bool {
	set := make(map[string]bool, len(records))
	for _, record := range records {
		set[ref(record)] = true
	}
	return set
}

// sortedRefs returns the ids that name a record of the document, sorted, with none twice
func sortedRefs(ids []string, known map[string]bool) []string {
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		if known[id] && !slices.Contains(refs, id) {
			refs = append(refs, id)
		}
	}
	slices.Sort(refs)
	return refs
}

func (s *ServiceImpl) addWorkspace(ctx context.Context, id workspace.ID, doc *Document) error {
	found, err := s.deps.Workspaces.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if found == nil {
		return workspace.ErrNotFound
	}
	doc.Workspace = Workspace{Name: found.Name}
	return nil
}

func (s *ServiceImpl) addSettings(ctx context.Context, id workspace.ID, doc *Document) error {
	doc.Settings = Settings{DataRetentionDays: setting.DefaultDataRetentionDays, CertExpiryNotifyDays: slices.Clone(defaultCertExpiryNotifyDays)}
	retention, err := s.deps.Settings.GetByKey(ctx, id, workspace.DataRetentionPeriodKey)
	if err != nil {
		return err
	}
	if retention != nil {
		if days, err := setting.ParseDataRetentionPeriod(retention.Value); err == nil {
			doc.Settings.DataRetentionDays = days
		}
	}
	notify, err := s.deps.Settings.GetByKey(ctx, id, workspace.CertExpiryNotifyDaysKey)
	if err != nil {
		return err
	}
	var days []int
	if notify != nil && json.Unmarshal([]byte(notify.Value), &days) == nil && days != nil {
		doc.Settings.CertExpiryNotifyDays = days
	}
	return nil
}

func (s *ServiceImpl) addTags(ctx context.Context, id workspace.ID, doc *Document) error {
	tags, err := readAll(func(page, limit int) ([]*tag.Model, error) {
		return s.deps.Tags.FindAll(ctx, id, page, limit, "")
	})
	if err != nil {
		return err
	}
	doc.Tags = make([]Tag, len(tags))
	for i, t := range tags {
		doc.Tags[i] = Tag{Ref: t.ID, Name: t.Name, Color: t.Color, Description: t.Description}
	}
	byNameThenRef(doc.Tags, func(t Tag) string { return t.Name }, func(t Tag) string { return t.Ref })
	return nil
}

func (s *ServiceImpl) addProxies(ctx context.Context, id workspace.ID, doc *Document) error {
	proxies, err := readAll(func(page, limit int) ([]*proxy.Model, error) {
		return s.deps.Proxies.FindAll(ctx, id, page, limit, "")
	})
	if err != nil {
		return err
	}
	doc.Proxies = make([]Proxy, len(proxies))
	for i, p := range proxies {
		doc.Proxies[i] = Proxy{
			Ref: p.ID, Protocol: p.Protocol, Host: p.Host, Port: p.Port, Auth: p.Auth,
			Username: p.Username, Password: p.Password, SensitiveFields: []string{},
		}
		if p.Password != "" {
			doc.Proxies[i].SensitiveFields = []string{"password"}
		}
	}
	// A proxy has no name: its address is what tells two apart
	byNameThenRef(doc.Proxies, func(p Proxy) string { return p.Protocol + "://" + p.Host + ":" + strconv.Itoa(p.Port) }, func(p Proxy) string { return p.Ref })
	return nil
}

func (s *ServiceImpl) addChannels(ctx context.Context, id workspace.ID, doc *Document) error {
	channels, err := readAll(func(page, limit int) ([]*notification_channel.Model, error) {
		return s.deps.Channels.FindAll(ctx, id, page, limit, "")
	})
	if err != nil {
		return err
	}
	doc.NotificationChannels = make([]NotificationChannel, len(channels))
	for i, c := range channels {
		config := ""
		if c.Config != nil {
			config = *c.Config
		}
		doc.NotificationChannels[i] = NotificationChannel{
			Ref: c.ID, Name: c.Name, Type: c.Type, Active: c.Active, IsDefault: c.IsDefault,
			Config: objectConfig(config), SensitiveFields: notification_channel.SensitiveFields(s.deps.Providers, c.Type, config),
		}
	}
	byNameThenRef(doc.NotificationChannels, func(c NotificationChannel) string { return c.Name }, func(c NotificationChannel) string { return c.Ref })
	return nil
}

// objectConfig returns a stored config as the JSON object the document holds. A config that is
// not an object (or is empty) is {}.
func objectConfig(config string) ConfigJSON {
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(config), &object) != nil || object == nil {
		return ConfigJSON("{}")
	}
	return ConfigJSON(config)
}

func (s *ServiceImpl) addMonitors(ctx context.Context, id workspace.ID, doc *Document) error {
	monitors, err := readAll(func(page, limit int) ([]*monitor.Model, error) {
		return s.deps.Monitors.FindAll(ctx, id, page, limit, "", nil, nil, nil)
	})
	if err != nil {
		return err
	}
	ids := make([]string, len(monitors))
	for i, m := range monitors {
		ids[i] = m.ID
	}
	channelLinks, tagLinks, err := s.monitorLinks(ctx, ids)
	if err != nil {
		return err
	}
	proxies := refSet(doc.Proxies, func(p Proxy) string { return p.Ref })
	channels := refSet(doc.NotificationChannels, func(c NotificationChannel) string { return c.Ref })
	tags := refSet(doc.Tags, func(t Tag) string { return t.Ref })
	doc.Monitors = make([]Monitor, len(monitors))
	for i, m := range monitors {
		doc.Monitors[i] = exportedMonitor(m)
		if proxies[m.ProxyId] {
			doc.Monitors[i].ProxyRef = m.ProxyId
		}
		doc.Monitors[i].NotificationChannelRefs = sortedRefs(channelLinks[m.ID], channels)
		doc.Monitors[i].TagRefs = sortedRefs(tagLinks[m.ID], tags)
	}
	byNameThenRef(doc.Monitors, func(m Monitor) string { return m.Name }, func(m Monitor) string { return m.Ref })
	return nil
}

func exportedMonitor(m *monitor.Model) Monitor {
	return Monitor{
		Ref: m.ID, Name: m.Name, Type: m.Type, Active: m.Active, Interval: m.Interval, Timeout: m.Timeout,
		MaxRetries: m.MaxRetries, RetryInterval: m.RetryInterval, ResendInterval: m.ResendInterval,
		LocationRule: string(m.LocationRule), LocationThreshold: m.LocationThreshold,
		Config: objectConfig(m.Config), PushToken: m.PushToken,
	}
}

// monitorLinks returns, for each monitor, the IDs of the notification channels and the tags it is
// linked to. It reads the links a chunk of monitors at a time.
func (s *ServiceImpl) monitorLinks(ctx context.Context, monitorIDs []string) (channels, tags map[string][]string, err error) {
	channels, tags = map[string][]string{}, map[string][]string{}
	for chunk := range slices.Chunk(monitorIDs, pageSize) {
		notified, err := s.deps.MonitorNotifications.FindByMonitorIDs(ctx, chunk)
		if err != nil {
			return nil, nil, err
		}
		for monitorID, links := range notified {
			for _, link := range links {
				channels[monitorID] = append(channels[monitorID], link.NotificationID)
			}
		}
		tagged, err := s.deps.MonitorTags.FindByMonitorIDs(ctx, chunk)
		if err != nil {
			return nil, nil, err
		}
		for monitorID, links := range tagged {
			for _, link := range links {
				tags[monitorID] = append(tags[monitorID], link.TagID)
			}
		}
	}
	return channels, tags, nil
}

func (s *ServiceImpl) addMaintenances(ctx context.Context, id workspace.ID, doc *Document) error {
	windows, err := readAll(func(page, limit int) ([]*maintenance.Model, error) {
		return s.deps.Maintenances.FindAll(ctx, id, page, limit, "", "")
	})
	if err != nil {
		return err
	}
	monitors := refSet(doc.Monitors, func(m Monitor) string { return m.Ref })
	doc.Maintenances = make([]Maintenance, len(windows))
	for i, w := range windows {
		links, err := s.deps.MonitorMaintenances.FindByMaintenanceID(ctx, w.ID)
		if err != nil {
			return err
		}
		linked := make([]string, len(links))
		for j, link := range links {
			linked[j] = link.MonitorID
		}
		doc.Maintenances[i] = exportedMaintenance(w)
		doc.Maintenances[i].MonitorRefs = sortedRefs(linked, monitors)
	}
	byNameThenRef(doc.Maintenances, func(m Maintenance) string { return m.Title }, func(m Maintenance) string { return m.Ref })
	return nil
}

func exportedMaintenance(w *maintenance.Model) Maintenance {
	return Maintenance{
		Ref: w.ID, Title: w.Title, Description: w.Description, Active: w.Active, Strategy: w.Strategy,
		StartDateTime: w.StartDateTime, EndDateTime: w.EndDateTime, StartTime: w.StartTime, EndTime: w.EndTime,
		Weekdays: nonNil(w.Weekdays), DaysOfMonth: nonNil(w.DaysOfMonth), IntervalDay: w.IntervalDay,
		Cron: w.Cron, Timezone: w.Timezone, Duration: w.Duration,
	}
}

func nonNil(values []int) []int {
	if values == nil {
		return []int{}
	}
	return values
}

func (s *ServiceImpl) addStatusPages(ctx context.Context, id workspace.ID, doc *Document) error {
	pages, err := readAll(func(page, limit int) ([]*status_page.Model, error) {
		return s.deps.StatusPages.FindAll(ctx, id, page, limit, "")
	})
	if err != nil {
		return err
	}
	monitors := refSet(doc.Monitors, func(m Monitor) string { return m.Ref })
	doc.StatusPages = make([]StatusPage, len(pages))
	for i, p := range pages {
		exported := exportedStatusPage(p)
		if exported.Domains, err = s.pageDomains(ctx, p.ID); err != nil {
			return err
		}
		if exported.MonitorRefs, err = s.pageMonitors(ctx, p.ID, monitors); err != nil {
			return err
		}
		doc.StatusPages[i] = exported
	}
	byNameThenRef(doc.StatusPages, func(p StatusPage) string { return p.Title }, func(p StatusPage) string { return p.Ref })
	return nil
}

func exportedStatusPage(p *status_page.Model) StatusPage {
	return StatusPage{
		Ref: p.ID, Slug: p.Slug, Title: p.Title, Description: p.Description, Icon: p.Icon, Theme: p.Theme,
		Published: p.Published, ShowLocations: p.ShowLocations, FooterText: p.FooterText, AutoRefreshInterval: p.AutoRefreshInterval,
	}
}

// pageDomains returns the custom domains of a status page, sorted
func (s *ServiceImpl) pageDomains(ctx context.Context, pageID string) ([]string, error) {
	links, err := s.deps.Domains.GetDomainsForStatusPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	domains := make([]string, len(links))
	for i, link := range links {
		domains[i] = link.Domain
	}
	slices.Sort(domains)
	return domains, nil
}

// pageMonitors returns the refs of the monitors of a status page that the document holds, sorted:
// the links carry no stored order, so this is by monitor ID
func (s *ServiceImpl) pageMonitors(ctx context.Context, pageID string, known map[string]bool) ([]string, error) {
	links, err := s.deps.MonitorStatusPages.GetMonitorsForStatusPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(links, func(a, b *monitor_status_page.Model) int {
		return cmp.Or(cmp.Compare(a.Order, b.Order), strings.Compare(a.MonitorID, b.MonitorID))
	})
	refs := make([]string, 0, len(links))
	for _, link := range links {
		if known[link.MonitorID] {
			refs = append(refs, link.MonitorID)
		}
	}
	return refs, nil
}
