package mongoimport_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/domain_status_page"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// SQL stores a custom domain lower-case and trimmed, and keeps it unique across the install
// (PP-146). MongoDB did neither: the import normalizes the domains, and the first status page
// to hold a domain keeps it.
func TestImportNormalizesCustomDomainsAndKeepsASharedDomainOnTheFirstPage(t *testing.T) {
	in := mongoimporttest.Empty(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	page := func(slug string) primitive.ObjectID {
		return in.Insert(t, "status_pages", bson.D{
			{Key: "slug", Value: slug}, {Key: "title", Value: slug}, {Key: "published", Value: true},
			{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
		})
	}
	domain := func(page primitive.ObjectID, domain string) {
		in.Insert(t, "domain_status_page", bson.D{
			{Key: "status_page_id", Value: page}, {Key: "domain", Value: domain},
			{Key: "created_at", Value: now}, {Key: "updated_at", Value: now},
		})
	}
	shop, blog := page("shop"), page("blog")
	domain(shop, " Status.Shop.Example")
	domain(shop, "status.shop.example") // the same once normalized
	domain(blog, "STATUS.SHOP.EXAMPLE") // held by the page before
	domain(blog, "Blog.Example")

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())
		require.NoError(t, err)

		domains := domain_status_page.NewSQLRepository(db)
		onShop, err := domains.GetDomainsForStatusPage(ctx, uuidOf(t, shop.Hex()))
		require.NoError(t, err)
		require.Len(t, onShop, 1)
		assert.Equal(t, "status.shop.example", onShop[0].Domain)
		onBlog, err := domains.GetDomainsForStatusPage(ctx, uuidOf(t, blog.Hex()))
		require.NoError(t, err)
		require.Len(t, onBlog, 1)
		assert.Equal(t, "blog.example", onBlog[0].Domain)

		r := collectionReport(t, report, "domain_status_page")
		assert.Equal(t, 4, r.Read)
		assert.Equal(t, 2, r.Copied)
		assert.Equal(t, map[string]int{"the domain of this status page already copied": 1, "the domain is held by another status page": 1}, r.Skipped)
	})
}
