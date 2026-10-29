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
// keep it. So it stops, before it changes anything (the migration check_unique_push_tokens_and_domains checks
// this first, before any other change of the upgrade, and this one checks again), and names the
// domain and the pages.
//
// A domain that is empty once trimmed is no one's: it serves nothing. It is not a conflict, and
// the rows that hold one are deleted.
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

// loadPageDomains reads the domain rows with the slug of their page, the oldest row of a page's
// domain first
func loadPageDomains(ctx context.Context, q bun.IDB) ([]pageDomain, error) {
	var rows []pageDomain
	err := q.NewSelect().
		TableExpr("domain_status_page AS d").
		Join("JOIN status_pages AS p ON p.id = d.status_page_id").
		ColumnExpr("d.id AS id, d.status_page_id AS status_page_id, d.domain AS domain, p.slug AS slug").
		OrderExpr("d.created_at, d.id").
		Scan(ctx, &rows)
	return rows, err
}

// checkCustomDomainsAreUnique reads the domains and returns an error that names each one that
// more than one page holds, or nil. It changes nothing. The check migration runs it before any
// other change of this upgrade, and the migration that makes the domains unique runs it again.
func checkCustomDomainsAreUnique(ctx context.Context, q bun.IDB) error {
	rows, err := loadPageDomains(ctx, q)
	if err != nil {
		return err
	}
	return sharedCustomDomains(rows)
}

func makeCustomDomainsUnique(ctx context.Context, db *bun.DB) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		rows, err := loadPageDomains(ctx, tx)
		if err != nil {
			return err
		}
		if err := sharedCustomDomains(rows); err != nil {
			return err
		}

		// The first row of each page for each domain stays, and the rest are that page's duplicates.
		// A row with no domain once trimmed serves nothing, and goes too.
		type held struct{ domain, page string }
		kept := map[held]bool{}
		var remove []string
		var renamed []pageDomain
		for _, row := range rows {
			domain := normalizedDomain(row.Domain)
			key := held{domain, row.StatusPageID}
			if domain == "" || kept[key] {
				remove = append(remove, row.ID)
				continue
			}
			kept[key] = true
			if row.Domain != domain {
				row.Domain = domain
				renamed = append(renamed, row)
			}
		}

		// The rows that go are deleted first: a page's row can't take a domain its other row still holds
		if len(remove) > 0 {
			if _, err := tx.NewDelete().Table("domain_status_page").Where("id IN (?)", bun.In(remove)).Exec(ctx); err != nil {
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
// pages' slugs, or nil when no domain is shared. A domain that is empty once trimmed is held by
// no one: it serves nothing, and the migration deletes it.
func sharedCustomDomains(rows []pageDomain) error {
	slugsByDomain := map[string]map[string]bool{}
	for _, row := range rows {
		domain := normalizedDomain(row.Domain)
		if domain == "" {
			continue
		}
		if slugsByDomain[domain] == nil {
			slugsByDomain[domain] = map[string]bool{}
		}
		slugsByDomain[domain][row.Slug] = true
	}
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
