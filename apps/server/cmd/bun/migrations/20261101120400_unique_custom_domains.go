package migrations

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/uptrace/bun"
)

// Makes custom domains unique across the install (PP-146, docs/adr/0005 section 6): a request
// finds its status page by the domain alone, and a workspace's pages will share the database
// with other workspaces'.
//
// A domain is stored lower-case and trimmed from now on, since the same host written two ways is
// one host. The domains stored before are normalized here. The service already refused a domain
// that another page holds, so normalizing finds a conflict only between a domain that was written
// in two cases. A page's own duplicates that this makes are the same domain twice, and the oldest
// stays.
//
// A domain that two pages hold can't be fixed here: the upgrade doesn't know which page should
// keep it. So it stops, before it changes anything, and names the domain and the pages.
//
// A re-run finds nothing to change.

func init() {
	Migrations.MustRegister(makeCustomDomainsUnique, allowSharedCustomDomains)
}

type pageDomain struct {
	ID           string `bun:"id"`
	StatusPageID string `bun:"status_page_id"`
	Domain       string `bun:"domain"`
	Slug         string `bun:"slug"`
}

func normalizedDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

func makeCustomDomainsUnique(ctx context.Context, db *bun.DB) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// The oldest row of a page's domain comes first
		var rows []pageDomain
		err := tx.NewSelect().
			TableExpr("domain_status_page AS d").
			Join("JOIN status_pages AS p ON p.id = d.status_page_id").
			ColumnExpr("d.id AS id, d.status_page_id AS status_page_id, d.domain AS domain, p.slug AS slug").
			OrderExpr("d.created_at, d.id").
			Scan(ctx, &rows)
		if err != nil {
			return err
		}

		// The first row of each page for each domain stays, and the rest are that page's duplicates
		type held struct{ domain, page string }
		kept := map[held]bool{}
		slugsByDomain := map[string]map[string]bool{}
		var duplicates []string
		var renamed []pageDomain
		for _, row := range rows {
			domain := normalizedDomain(row.Domain)
			if slugsByDomain[domain] == nil {
				slugsByDomain[domain] = map[string]bool{}
			}
			slugsByDomain[domain][row.Slug] = true
			key := held{domain, row.StatusPageID}
			if kept[key] {
				duplicates = append(duplicates, row.ID)
				continue
			}
			kept[key] = true
			if row.Domain != domain {
				row.Domain = domain
				renamed = append(renamed, row)
			}
		}
		if err := sharedCustomDomains(slugsByDomain); err != nil {
			return err
		}

		// The duplicates go first: a page's row can't take a domain its other row still holds
		if len(duplicates) > 0 {
			if _, err := tx.NewDelete().Table("domain_status_page").Where("id IN (?)", bun.In(duplicates)).Exec(ctx); err != nil {
				return err
			}
		}
		for _, row := range renamed {
			if _, err := tx.NewUpdate().Table("domain_status_page").Set("domain = ?", row.Domain).Where("id = ?", row.ID).Exec(ctx); err != nil {
				return err
			}
		}

		_, err = tx.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_domain_status_page_domain ON domain_status_page(domain)")
		return err
	})
}

// sharedCustomDomains is an error that names each domain more than one page holds, with the
// pages' slugs, or nil when no domain is shared
func sharedCustomDomains(slugsByDomain map[string]map[string]bool) error {
	var shared []string
	for domain, slugSet := range slugsByDomain {
		if len(slugSet) < 2 {
			continue
		}
		slugs := make([]string, 0, len(slugSet))
		for slug := range slugSet {
			slugs = append(slugs, fmt.Sprintf("%q", slug))
		}
		sort.Strings(slugs)
		shared = append(shared, fmt.Sprintf("%q is held by the status pages %s", domain, strings.Join(slugs, ", ")))
	}
	if len(shared) == 0 {
		return nil
	}
	sort.Strings(shared)
	return fmt.Errorf("custom domains must be unique, ignoring case and spaces, and %s. "+
		"In the previous version, remove each domain from all but one of its status pages, then upgrade again",
		strings.Join(shared, "; "))
}

// The unique index goes. The domains that were normalized or removed stay as they are.
func allowSharedCustomDomains(ctx context.Context, db *bun.DB) error {
	_, err := db.ExecContext(ctx, "DROP INDEX IF EXISTS idx_domain_status_page_domain")
	return err
}
