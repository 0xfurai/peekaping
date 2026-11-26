package apitest

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/auth"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// importMongoInstall copies the install into a new SQLite file and returns its path.
func importMongoInstall(t *testing.T, in *mongoimporttest.Install) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "imported.db")
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path}, zap.NewNop().Sugar())
	require.NoError(t, err)
	_, err = mongoimport.Run(context.Background(), db, in.Options())
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return path
}

// What an install that moved from MongoDB to SQLite keeps working: the logins, the
// API keys handed out, the badge URLs on other sites, and the data behind them.
func TestImportedMongoInstall(t *testing.T) {
	parallel(t)
	in := mongoimporttest.Seed(t)
	path := importMongoInstall(t, in)
	a := newApp(t, func(cfg *config.Config) { cfg.DBName = path })

	t.Run("a user logs in with the password and two-factor code", func(t *testing.T) {
		login := map[string]any{"email": mongoimporttest.AdminEmail, "password": mongoimporttest.AdminPassword}
		assert.Equal(t, http.StatusUnauthorized, a.login(t, with(login, "password", "Wrong-password-1")).Code)
		assert.Equal(t, http.StatusUnauthorized, a.login(t, login).Code, "the code is required")
		assert.Equal(t, http.StatusUnauthorized, a.login(t, with(login, "token", "000000")).Code)

		code, err := totp.GenerateCode(mongoimporttest.AdminTOTPSecret, time.Now())
		require.NoError(t, err)
		session := decode[tokens](t, a.login(t, with(login, "token", code)), http.StatusOK)
		a.accessToken = session.AccessToken
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin).Code)

		// The refresh token issued after the import works
		refreshed := decode[tokens](t, a.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refreshToken": session.RefreshToken}), http.StatusOK)
		assert.NotEmpty(t, refreshed.AccessToken)

		// A session from before the import carries the user's ObjectID, signed with the same secret.
		// Its refresh token finds no user, so the user signs in again, once.
		require.NoError(t, a.container.Invoke(func(maker *auth.TokenMaker) error {
			old, err := maker.CreateRefreshToken(context.Background(), &auth.Model{ID: in.AdminID, Email: mongoimporttest.AdminEmail})
			assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refreshToken": old}).Code)
			return err
		}))
	})

	t.Run("an API key handed out by MongoDB authenticates", func(t *testing.T) {
		a.apiKey = in.APIKeyToken
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.withAPIKey).Code)
		a.apiKey = in.APIKeyToken[:len(in.APIKeyToken)-2] + "xx"
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.withAPIKey).Code)
	})

	t.Run("a badge URL with a monitor's ObjectID renders", func(t *testing.T) {
		// The checkout monitor is on the published "shop" page
		for path, title := range map[string]string{
			"/api/v1/badge/" + in.ID("monitor", "checkout") + "/status": "status: Up",
			"/api/v1/badge/" + in.ID("monitor", "checkout") + "/ping":   "ping (24h): N/A",
		} {
			assertBadge(t, a.request(t, http.MethodGet, path, nil), title)
		}
		// The paused one is on that page too, but a paused monitor has no public badge
		w := a.request(t, http.MethodGet, "/api/v1/badge/"+in.ID("monitor", "paused")+"/status", nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("the status page and its monitors are served", func(t *testing.T) {
		page := decode[struct {
			Title string `json:"title"`
		}](t, a.request(t, http.MethodGet, "/api/v1/status-pages/slug/shop", nil), http.StatusOK)
		assert.Equal(t, "Shop status", page.Title)
		domain := decode[struct {
			Slug string `json:"slug"`
		}](t, a.request(t, http.MethodGet, "/api/v1/status-pages/domain/status.example.com", nil), http.StatusOK)
		assert.Equal(t, "shop", domain.Slug)

		monitors := decode[[]map[string]any](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK)
		var names []string
		for _, monitor := range monitors {
			names = append(names, monitor["name"].(string))
		}
		assert.ElementsMatch(t, []string{"Checkout API", "Nightly job", "Legacy database", mongoimporttest.LongName, "Broken config"}, names)
	})
}
