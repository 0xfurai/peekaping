package workspaceport

import (
	"context"
	"strconv"

	"peekaping/internal/modules/domain_status_page"
)

// pageEnv is what the status pages of a file are fitted against: the room the plan leaves for pages
// and custom domains, and the slugs and domains the file's earlier pages have taken
type pageEnv struct {
	// pageRoom and domainRoom are how many more the plan allows, nil when it sets no limit
	pageRoom, domainRoom *int
	slugs, domains       map[string]bool
}

func (f *fitter) pageEnv(ctx context.Context) (*pageEnv, error) {
	env := &pageEnv{slugs: map[string]bool{}, domains: map[string]bool{}}
	if f.limits.StatusPages != nil {
		have, err := f.s.deps.StatusPages.CountPages(ctx, f.id)
		if err != nil {
			return nil, err
		}
		room := max(*f.limits.StatusPages-have, 0)
		env.pageRoom = &room
	}
	if f.limits.CustomDomains != nil {
		have, err := f.s.deps.StatusPages.CountDomains(ctx, f.id)
		if err != nil {
			return nil, err
		}
		room := max(*f.limits.CustomDomains-have, 0)
		env.domainRoom = &room
	}
	return env, nil
}

// fitStatusPages leaves out the pages the plan has no room for, gives a page whose slug is taken
// the first free one with a number after it, and drops the custom domains that are taken or that
// the plan has no room for
func (f *fitter) fitStatusPages(ctx context.Context) error {
	env, err := f.pageEnv(ctx)
	if err != nil {
		return err
	}
	for _, p := range f.doc.StatusPages {
		if err := f.fitStatusPage(ctx, env, p); err != nil {
			return err
		}
	}
	return nil
}

func (f *fitter) fitStatusPage(ctx context.Context, env *pageEnv, p StatusPage) error {
	if !takeRoom(env.pageRoom) {
		f.plan.add(Change{Kind: ChangeStatusPageSkippedOverLimit, Record: RecordStatusPage, Ref: p.Ref, Name: p.Title, Reason: p.Slug})
		return nil
	}
	dto := statusPageDto(p)
	var err error
	if dto.Slug, err = f.freeSlug(ctx, env, p); err != nil {
		return err
	}
	if dto.Domains, err = f.keptDomains(ctx, env, p); err != nil {
		return err
	}
	f.plan.pages = append(f.plan.pages, plannedPage{ref: p.Ref, dto: dto, monitorRefs: p.MonitorRefs})
	return nil
}

// takeRoom uses one place of a limit and reports whether there was one. No limit has room for any.
func takeRoom(room *int) bool {
	if room == nil {
		return true
	}
	if *room == 0 {
		return false
	}
	*room--
	return true
}

// freeSlug is the page's slug, or the first of slug-2, slug-3, ... that no page of the install and
// no earlier page of the file has
func (f *fitter) freeSlug(ctx context.Context, env *pageEnv, p StatusPage) (string, error) {
	slug := p.Slug
	for n := 2; ; n++ {
		taken := env.slugs[slug]
		if !taken {
			var err error
			if taken, err = f.s.deps.StatusPageService.SlugTaken(ctx, slug); err != nil {
				return "", err
			}
		}
		if !taken {
			break
		}
		slug = numbered(p.Slug, n)
	}
	env.slugs[slug] = true
	if slug != p.Slug {
		f.plan.add(Change{Kind: ChangeStatusPageSlugChanged, Record: RecordStatusPage, Ref: p.Ref, Name: p.Title, From: p.Slug, To: slug})
	}
	return slug, nil
}

// maxSlugLength is what the slug column holds (VARCHAR(255)); the status page DTO sets a minimum
// and no maximum
const maxSlugLength = 255

// numbered is slug-n, with the slug cut short so that the whole fits in the column
func numbered(slug string, n int) string {
	suffix := "-" + strconv.Itoa(n)
	runes := []rune(slug)
	if room := maxSlugLength - len([]rune(suffix)); len(runes) > room {
		runes = runes[:room]
	}
	return string(runes) + suffix
}

// keptDomains are the page's custom domains, normalized as the service stores them, without those
// a page of the install or an earlier page of the file has and those past the plan's room
func (f *fitter) keptDomains(ctx context.Context, env *pageEnv, p StatusPage) ([]string, error) {
	kept, seen := []string{}, map[string]bool{}
	for _, raw := range p.Domains {
		// An empty domain is none, and a domain the page lists twice is one
		domain := domain_status_page.Normalize(raw)
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		taken, err := f.domainTaken(ctx, env, domain)
		if err != nil {
			return nil, err
		}
		switch {
		case taken:
			f.plan.add(Change{Kind: ChangeCustomDomainDropped, Record: RecordCustomDomain, Ref: p.Ref, Name: domain})
		case !takeRoom(env.domainRoom):
			f.plan.add(Change{Kind: ChangeCustomDomainDroppedOverLimit, Record: RecordCustomDomain, Ref: p.Ref, Name: domain})
		default:
			env.domains[domain] = true
			kept = append(kept, domain)
		}
	}
	return kept, nil
}

func (f *fitter) domainTaken(ctx context.Context, env *pageEnv, domain string) (bool, error) {
	if env.domains[domain] {
		return true, nil
	}
	existing, err := f.s.deps.Domains.FindByDomain(ctx, domain)
	return existing != nil, err
}
