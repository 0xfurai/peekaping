package workspaceport

import (
	"context"
	"errors"
	"strconv"

	"peekaping/internal/limits"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/netguard"
)

// What an import does to a file that is fine in itself so that the workspace and the install take
// it: it reads the target and decides, and writes nothing. Each rule is one function, and each
// change it makes is an entry of the report, never an error.

// fitter fits the document to a workspace. The steps share what they have decided: the records
// left out, and the plan's limits.
type fitter struct {
	s      *ServiceImpl
	id     workspace.ID
	doc    *Document
	plan   *importPlan
	found  *problems
	limits limits.Limits
	// skippedProxies maps the ref of a proxy that is left out to its address
	skippedProxies map[string]string
}

func (s *ServiceImpl) fit(ctx context.Context, id workspace.ID, doc *Document, found *problems) (*importPlan, error) {
	f := &fitter{
		s: s, id: id, doc: doc, found: found,
		plan:           &importPlan{reusedTags: map[string]string{}, maintenances: doc.Maintenances},
		skippedProxies: map[string]string{},
	}
	var err error
	if f.limits, err = s.limitsOf(ctx, id); err != nil {
		return nil, err
	}
	steps := []func(context.Context) error{f.fitSettings, f.fitTags, f.fitProxies, f.fitChannels, f.fitMonitors, f.fitStatusPages}
	for _, step := range steps {
		if err := step(ctx); err != nil {
			return nil, err
		}
	}
	return f.plan, nil
}

// limitsOf are the workspace's limits, none when the install has no source of them
func (s *ServiceImpl) limitsOf(ctx context.Context, id workspace.ID) (limits.Limits, error) {
	if s.deps.Limits == nil {
		return limits.Limits{}, nil
	}
	l, _, err := s.deps.Limits.For(ctx, id)
	return l, err
}

// fitSettings keeps the data retention period within the plan's cap. The certificate expiry days
// and the workspace's name are not the plan's business: the days come as given, and the name stays.
func (f *fitter) fitSettings(context.Context) error {
	f.plan.settings = settingsPlan{retentionDays: f.doc.Settings.DataRetentionDays, certDays: f.doc.Settings.CertExpiryNotifyDays}
	days, ceiling := f.plan.settings.retentionDays, f.limits.RetentionDays
	if ceiling == nil || days <= *ceiling {
		return nil
	}
	f.plan.settings.retentionDays = *ceiling
	f.plan.add(Change{Kind: ChangeRetentionCapped, Record: RecordSettings, Name: "dataRetentionDays", From: strconv.Itoa(days), To: strconv.Itoa(*ceiling)})
	return nil
}

// fitTags reuses the tag of the workspace that has the same name. The comparison is the tag
// service's own, since tag names are unique within a workspace.
func (f *fitter) fitTags(ctx context.Context) error {
	for _, t := range f.doc.Tags {
		existing, err := f.s.deps.Tags.FindByName(ctx, f.id, t.Name)
		if err != nil {
			return err
		}
		if existing == nil {
			f.plan.tags = append(f.plan.tags, t)
			continue
		}
		f.plan.reusedTags[t.Ref] = existing.ID
		f.plan.add(Change{Kind: ChangeTagReused, Record: RecordTag, Ref: t.Ref, Name: t.Name, To: existing.ID})
	}
	return nil
}

// fitProxies leaves out a proxy at an address the private target block refuses
func (f *fitter) fitProxies(context.Context) error {
	for _, p := range f.doc.Proxies {
		address := p.Protocol + "://" + p.Host + ":" + strconv.Itoa(p.Port)
		var blocked *netguard.TargetError
		if err := f.s.deps.Guard.CheckLiteral(p.Host); errors.As(err, &blocked) {
			f.skippedProxies[p.Ref] = address
			f.plan.add(Change{Kind: ChangeProxySkippedTargetNotAllowed, Record: RecordProxy, Ref: p.Ref, Name: address, Reason: blocked.Code})
			continue
		}
		f.plan.proxies = append(f.plan.proxies, p)
	}
	return nil
}

// fitChannels leaves out a channel the API would refuse in this workspace: one that sends to an
// address the private target block refuses, and an install email channel that the install or the
// workspace can't take. Saved secrets come as the file gives them.
func (f *fitter) fitChannels(ctx context.Context) error {
	for _, c := range f.doc.NotificationChannels {
		config, err := configText(c.Config)
		if err != nil || f.s.deps.Providers.ValidateConfig(c.Type, config) != nil {
			continue // the check reported it
		}
		reason, err := f.s.deps.Providers.Refusal(ctx, f.id, c.Type, config)
		if err != nil {
			return err
		}
		if reason != "" {
			f.plan.add(Change{Kind: ChangeChannelSkipped, Record: RecordNotificationChannel, Ref: c.Ref, Name: c.Name, Reason: reason})
			continue
		}
		f.plan.channels = append(f.plan.channels, c)
	}
	return nil
}
