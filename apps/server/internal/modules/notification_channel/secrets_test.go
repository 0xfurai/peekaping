package notification_channel

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// newRealRegistry returns the registry the app runs with
func newRealRegistry() *ProviderRegistry {
	return NewProviderRegistry(zap.NewNop().Sugar(), &config.Config{})
}

// TestSecretKeysPerProvider pins which config keys of each notification channel type are hidden
// from responses. A new type, or a new secret field, has to be added here, so that tagging it
// is a decision somebody made.
func TestSecretKeysPerProvider(t *testing.T) {
	registry := newRealRegistry()

	want := map[string][]string{
		"discord":        {"webhook_url"},
		"gotify":         {"application_token"},
		"google_chat":    {"webhook_url"},
		"grafana_oncall": {"grafana_oncall_url"},
		"line":           {"channel_access_token"},
		"matrix":         {"access_token"},
		"mattermost":     {"webhook_url"},
		"ntfy":           {"password", "token"},
		"opsgenie":       {"api_key"},
		"pagerduty":      {"pagerduty_integration_key"},
		"pagertree":      {"authToken", "integrationUrl"},
		"pushbullet":     {"pushbullet_access_token"},
		"pushover":       {"pushover_app_token", "pushover_user_key"},
		"sendgrid":       {"api_key"},
		"signal":         {},
		"slack":          {"slack_bot_token", "slack_webhook_url"},
		"smtp":           {"password"},
		"teams":          {"webhook_url"},
		"telegram":       {"bot_token"},
		"twilio":         {"twilio_api_key", "twilio_auth_token"},
		"webhook":        {"webhook_additional_headers", "webhook_signing_secret"},
		"wecom":          {"webhook_url"},
		"whatsapp":       {"api_key"},
	}

	got := map[string][]string{}
	for _, channelType := range registry.Types() {
		provider, _ := registry.Get(channelType)
		keys := append([]string{}, secretKeys(provider)...)
		sort.Strings(keys)
		got[channelType] = keys
	}
	assert.Equal(t, want, got)
}

// TestUntaggedConfigKeysAreNotSecretsByName catches a secret field that was added without its
// tag: a key named like a credential has to be tagged, or listed here as the one that isn't.
func TestUntaggedConfigKeysAreNotSecretsByName(t *testing.T) {
	registry := newRealRegistry()

	looksSecret := regexp.MustCompile(`(?i)token|secret|password|key|auth`)
	notSecrets := map[string]bool{
		"authentication_type": true, // ntfy: none, basic or token
	}

	for _, channelType := range registry.Types() {
		provider, _ := registry.Get(channelType)
		cfg, err := provider.Unmarshal("{}")
		assert.NoError(t, err, channelType)
		fields := reflect.TypeOf(cfg).Elem()
		for i := 0; i < fields.NumField(); i++ {
			field := fields.Field(i)
			key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if field.Tag.Get("secret") != "true" && looksSecret.MatchString(key) {
				assert.True(t, notSecrets[key], "%s %s looks like a secret: tag it `secret:\"true\"`, or list it in notSecrets", channelType, key)
			}
		}
	}
}

// secretTestProvider is a notification channel provider whose config has two secrets
type secretTestProvider struct {
	NotificationChannelProvider
}

type secretTestConfig struct {
	URL      string `json:"url"`
	Token    string `json:"token" secret:"true"`
	Password string `json:"password" secret:"true"`
}

func (secretTestProvider) Unmarshal(string) (any, error) { return &secretTestConfig{}, nil }

// noSecretsProvider is a notification channel provider whose config has no secrets
type noSecretsProvider struct {
	NotificationChannelProvider
}

func (noSecretsProvider) Unmarshal(string) (any, error) { return &struct{ URL string }{}, nil }

// newSecretTestRegistry returns a registry with the channel types test-secrets and test-no-secrets
func newSecretTestRegistry() *ProviderRegistry {
	return newTestRegistry(map[string]NotificationChannelProvider{
		"test-secrets":    secretTestProvider{},
		"test-no-secrets": noSecretsProvider{},
	})
}

// requireSameConfig compares configs as JSON where they are, since the order of keys doesn't matter
func requireSameConfig(t *testing.T, want, got string) {
	t.Helper()
	if json.Valid([]byte(want)) {
		assert.JSONEq(t, want, got)
		return
	}
	assert.Equal(t, want, got)
}

func TestRedactConfig(t *testing.T) {
	cases := []struct {
		name     string
		provider NotificationChannelProvider
		config   string
		want     string
	}{
		{"set secrets are replaced", secretTestProvider{}, `{"url":"https://example.com","token":"abc","password":"hunter2"}`, `{"url":"https://example.com","token":"********","password":"********"}`},
		{"an empty secret stays empty", secretTestProvider{}, `{"url":"u","token":"","password":"hunter2"}`, `{"url":"u","token":"","password":"********"}`},
		{"an absent secret stays absent", secretTestProvider{}, `{"url":"u"}`, `{"url":"u"}`},
		{"a null secret stays null", secretTestProvider{}, `{"token":null}`, `{"token":null}`},
		{"a secret that isn't a string stays as it is", secretTestProvider{}, `{"token":123}`, `{"token":123}`},
		{"a key of another letter case is the same secret", secretTestProvider{}, `{"Token":"abc","PASSWORD":"p"}`, `{"Token":"********","PASSWORD":"********"}`},
		{"unknown fields stay", secretTestProvider{}, `{"future":{"a":[1,2]},"token":"abc"}`, `{"future":{"a":[1,2]},"token":"********"}`},
		{"a null config stays", secretTestProvider{}, `null`, `null`},
		{"an empty config stays", secretTestProvider{}, ``, ``},
		{"a config that isn't JSON comes back empty", secretTestProvider{}, `{"token":"abc"`, ``},
		{"a config that isn't an object comes back empty", secretTestProvider{}, `["abc"]`, ``},
		{"a type with no secrets keeps its config", noSecretsProvider{}, `{"url":"u","token":"abc"}`, `{"url":"u","token":"abc"}`},
		{"a type with no secrets keeps a config that isn't JSON", noSecretsProvider{}, `{"url":`, `{"url":`},
		{"an unknown type keeps its config", nil, `{"token":"abc"}`, `{"token":"abc"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireSameConfig(t, tc.want, redactConfig(tc.provider, tc.config))
		})
	}

	t.Run("values stay as they were written", func(t *testing.T) {
		got := redactConfig(secretTestProvider{}, `{"url":"https://example.com/?a=1&b=<2>","big":12345678901234567890,"token":"abc"}`)
		assert.Contains(t, got, `"url":"https://example.com/?a=1&b=<2>"`)
		assert.Contains(t, got, `"big":12345678901234567890`)
	})
}

func TestRedactModelLeavesTheChannelAlone(t *testing.T) {
	providers := newSecretTestRegistry()
	config := `{"token":"abc"}`
	channel := &Model{ID: "channel-1", Name: "Alerts", Type: "test-secrets", Config: &config}

	redacted := redactModel(providers, channel)

	assert.Equal(t, `{"token":"********"}`, *redacted.Config)
	assert.Equal(t, "channel-1", redacted.ID)
	assert.Equal(t, "Alerts", redacted.Name)
	assert.Equal(t, `{"token":"abc"}`, *channel.Config, "the channel the service holds is untouched")
}

func TestRedactModels(t *testing.T) {
	providers := newSecretTestRegistry()
	config := `{"token":"abc"}`

	assert.Nil(t, redactModels(providers, nil))
	assert.Equal(t, []*Model{}, redactModels(providers, []*Model{}))
	assert.Nil(t, redactModel(providers, nil))

	noConfig := &Model{ID: "channel-1", Type: "test-secrets"}
	redacted := redactModels(providers, []*Model{noConfig, {ID: "channel-2", Type: "test-secrets", Config: &config}})
	assert.Nil(t, redacted[0].Config)
	assert.Equal(t, `{"token":"********"}`, *redacted[1].Config)
}

func TestRedactModelKeepsTheConfigOfAnUnknownType(t *testing.T) {
	config := `{"token":"abc"}`
	redacted := redactModel(newSecretTestRegistry(), &Model{ID: "channel-1", Type: "test-missing", Config: &config})
	assert.Equal(t, `{"token":"abc"}`, *redacted.Config)
}
