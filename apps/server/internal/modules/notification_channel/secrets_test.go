package notification_channel

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// newRealRegistry returns the registry the app runs with
func newRealRegistry() *ProviderRegistry {
	return NewProviderRegistry(zap.NewNop().Sugar(), &config.Config{}, nil, nil)
}

// fieldKeys returns the sorted keys of each channel type's fields that match
func fieldKeys(registry *ProviderRegistry, match func(configField) bool) map[string][]string {
	keys := map[string][]string{}
	for _, channelType := range registry.Types() {
		provider, _ := registry.Get(channelType)
		keys[channelType] = []string{}
		for _, field := range configFields(provider) {
			if match(field) {
				keys[channelType] = append(keys[channelType], field.key)
			}
		}
		sort.Strings(keys[channelType])
	}
	return keys
}

// TestSecretKeysPerProvider pins which config keys of each notification channel type are hidden
// from responses. A new type, or a new secret field, has to be added here, so that tagging it
// is a decision somebody made.
func TestSecretKeysPerProvider(t *testing.T) {
	want := map[string][]string{
		"discord":        {"webhook_url"},
		"gotify":         {"application_token"},
		"google_chat":    {"webhook_url"},
		"grafana_oncall": {"grafana_oncall_url"},
		"install_email":  {},
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

	got := fieldKeys(newRealRegistry(), func(field configField) bool { return field.secret })
	assert.Equal(t, want, got)
}

// TestEndpointKeysPerProvider pins which config keys of each notification channel type decide
// where the channel sends its secrets. A secret is only kept while these stay as they are, so
// a field that sends secrets somewhere new has to be added here.
func TestEndpointKeysPerProvider(t *testing.T) {
	want := map[string][]string{
		"discord":        {"webhook_url"},
		"gotify":         {"server_url"},
		"google_chat":    {"webhook_url"},
		"grafana_oncall": {"grafana_oncall_url"},
		"install_email":  {},
		"line":           {},
		"matrix":         {"homeserver_url"},
		"mattermost":     {"webhook_url"},
		"ntfy":           {"server_url"},
		"opsgenie":       {},
		"pagerduty":      {"pagerduty_integration_url"},
		"pagertree":      {"integrationUrl"},
		"pushbullet":     {},
		"pushover":       {},
		"sendgrid":       {},
		"signal":         {},
		"slack":          {"slack_webhook_url"},
		"smtp":           {"smtp_host", "smtp_port", "smtp_secure"},
		"teams":          {"webhook_url"},
		"telegram":       {"server_url"},
		"twilio":         {},
		"webhook":        {"webhook_url"},
		"wecom":          {"webhook_url"},
		"whatsapp":       {"server_url"},
	}

	got := fieldKeys(newRealRegistry(), func(field configField) bool { return field.endpoint })
	assert.Equal(t, want, got)
}

// TestEndpointDefaultsPerProvider pins the endpoint fields whose sender treats an empty value as
// a default server. The endpoint check reads such a field as that default on both sides, so a
// stored config without it and a form that fills the default in are the same endpoint. A sender
// with a fallback like that has to be added here.
func TestEndpointDefaultsPerProvider(t *testing.T) {
	registry := newRealRegistry()
	got := map[string]map[string]string{}
	for _, channelType := range registry.Types() {
		provider, _ := registry.Get(channelType)
		for _, field := range configFields(provider) {
			if field.endpointDefault != "" {
				if got[channelType] == nil {
					got[channelType] = map[string]string{}
				}
				got[channelType][field.key] = field.endpointDefault
			}
		}
	}

	assert.Equal(t, map[string]map[string]string{
		"telegram": {"server_url": "https://api.telegram.org"},
	}, got)
}

// TestURLCredentialKeysPerProvider pins which config keys of each notification channel type are
// URLs the channel sends to whose credentials (user info and query values) the API masks. A
// new type with a URL field that can hold a credential has to be added here.
func TestURLCredentialKeysPerProvider(t *testing.T) {
	want := map[string][]string{
		"discord":        {},
		"gotify":         {"server_url"},
		"google_chat":    {},
		"grafana_oncall": {},
		"install_email":  {},
		"line":           {},
		"matrix":         {"homeserver_url"},
		"mattermost":     {},
		"ntfy":           {"server_url"},
		"opsgenie":       {},
		"pagerduty":      {"pagerduty_integration_url"},
		"pagertree":      {},
		"pushbullet":     {},
		"pushover":       {},
		"sendgrid":       {},
		"signal":         {"signal_url"},
		"slack":          {},
		"smtp":           {},
		"teams":          {},
		"telegram":       {"server_url"},
		"twilio":         {},
		"webhook":        {"webhook_url"},
		"wecom":          {},
		"whatsapp":       {"server_url"},
	}

	got := fieldKeys(newRealRegistry(), func(field configField) bool { return field.urlCredentials })
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

// secretTestProvider is a notification channel provider whose config has a required secret, the
// token, an optional one, the password, and a secret that is a URL, the hook URL. The server and
// the hook URL decide where it sends. It records the configs it sends to.
type secretTestProvider struct {
	NotificationChannelProvider
	sent []string
	// failWith, when set, makes Send fail with the error it returns for the config sent
	failWith func(config string) error
}

type secretTestConfig struct {
	URL      string `json:"url"`
	Server   string `json:"server" endpoint:"true"`
	Token    string `json:"token" secret:"true"`
	Password string `json:"password" secret:"true"`
	HookURL  string `json:"hook_url" secret:"true" endpoint:"true"`
	// Region is where the provider sends when it is empty
	Region string `json:"region" endpoint:"true" endpointDefault:"us-east"`
}

func (*secretTestProvider) Unmarshal(config string) (any, error) {
	var cfg secretTestConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *secretTestProvider) Validate(config string) error {
	cfg, err := p.Unmarshal(config)
	if err != nil {
		return err
	}
	if cfg.(*secretTestConfig).Token == "" {
		return errors.New("token is required")
	}
	return nil
}

func (p *secretTestProvider) Send(_ context.Context, configJSON, _ string, _ *monitor.Model, _ *heartbeat.Model) error {
	p.sent = append(p.sent, configJSON)
	if p.failWith != nil {
		return p.failWith(configJSON)
	}
	return nil
}

// urlTestProvider is a notification channel provider whose config has a URL it sends to that may
// hold credentials and is an endpoint, the webhook URL, one that is not an endpoint, the signal
// URL, and a secret, the token
type urlTestProvider struct {
	NotificationChannelProvider
}

type urlTestConfig struct {
	Plain     string `json:"plain"`
	Webhook   string `json:"webhook_url" endpoint:"true" urlCredentials:"true"`
	SignalURL string `json:"signal_url" urlCredentials:"true"`
	Token     string `json:"token" secret:"true"`
}

func (*urlTestProvider) Unmarshal(config string) (any, error) {
	var cfg urlTestConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// urlOnlyProvider is a notification channel provider whose config has a URL that may hold
// credentials and nothing else that is tagged, as the Signal config does
type urlOnlyProvider struct {
	NotificationChannelProvider
}

type urlOnlyConfig struct {
	SignalURL string `json:"signal_url" urlCredentials:"true"`
}

func (*urlOnlyProvider) Unmarshal(config string) (any, error) {
	var cfg urlOnlyConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// noSecretsProvider is a notification channel provider whose config has no secrets
type noSecretsProvider struct {
	NotificationChannelProvider
}

func (noSecretsProvider) Unmarshal(string) (any, error) { return &struct{ URL string }{}, nil }

// newSecretTestRegistry returns a registry with the channel types test-secrets and
// test-other-secrets, which share one provider, and test-no-secrets. It returns the shared provider.
func newSecretTestRegistry() (*ProviderRegistry, *secretTestProvider) {
	provider := &secretTestProvider{}
	return newTestRegistry(map[string]NotificationChannelProvider{
		"test-secrets":       provider,
		"test-other-secrets": provider,
		"test-no-secrets":    noSecretsProvider{},
	}), provider
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
	provider := &secretTestProvider{}
	cases := []struct {
		name     string
		provider NotificationChannelProvider
		config   string
		want     string
	}{
		{"set secrets are replaced", provider, `{"url":"https://example.com","token":"abc","password":"hunter2","hook_url":"https://hook.example/x"}`, `{"url":"https://example.com","token":"********","password":"********","hook_url":"********"}`},
		{"an endpoint that isn't a secret stays", provider, `{"server":"https://server.example","token":"abc"}`, `{"server":"https://server.example","token":"********"}`},
		{"an empty secret stays empty", provider, `{"url":"u","token":"","password":"hunter2"}`, `{"url":"u","token":"","password":"********"}`},
		{"an absent secret stays absent", provider, `{"url":"u"}`, `{"url":"u"}`},
		{"a null secret stays null", provider, `{"token":null}`, `{"token":null}`},
		{"a number in a secret is replaced", provider, `{"token":123}`, `{"token":"********"}`},
		{"a zero in a secret is replaced", provider, `{"token":0}`, `{"token":"********"}`},
		{"an object in a secret is replaced", provider, `{"token":{"abc":"def"},"password":"p"}`, `{"token":"********","password":"********"}`},
		{"an empty object in a secret is replaced", provider, `{"token":{}}`, `{"token":"********"}`},
		{"an array in a secret is replaced", provider, `{"token":["abc"]}`, `{"token":"********"}`},
		{"true in a secret is replaced", provider, `{"token":true}`, `{"token":"********"}`},
		{"false in a secret is replaced", provider, `{"token":false}`, `{"token":"********"}`},
		{"a value that isn't a string is replaced under a key of another letter case", provider, `{"TOKEN":123}`, `{"TOKEN":"********"}`},
		{"a key of another letter case is the same secret", provider, `{"Token":"abc","PASSWORD":"p"}`, `{"Token":"********","PASSWORD":"********"}`},
		{"unknown fields stay", provider, `{"future":{"a":[1,2]},"token":"abc"}`, `{"future":{"a":[1,2]},"token":"********"}`},
		{"a null config stays", provider, `null`, `null`},
		{"an empty config stays", provider, ``, ``},
		{"a config that isn't JSON comes back empty", provider, `{"token":"abc"`, ``},
		{"a config that isn't an object comes back empty", provider, `["abc"]`, ``},
		{"a type with no secrets keeps its config", noSecretsProvider{}, `{"url":"u","token":"abc"}`, `{"url":"u","token":"abc"}`},
		{"a type with no secrets keeps a config that isn't JSON", noSecretsProvider{}, `{"url":`, `{"url":`},
		{"an unknown type comes back empty", nil, `{"token":"abc"}`, ``},
		{"an unknown type comes back empty, whatever the config is", nil, `{"url":"u"}`, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireSameConfig(t, tc.want, redactConfig(tc.provider, tc.config))
		})
	}

	t.Run("values stay as they were written", func(t *testing.T) {
		got := redactConfig(provider, `{"url":"https://example.com/?a=1&b=<2>","big":12345678901234567890,"token":"abc"}`)
		assert.Contains(t, got, `"url":"https://example.com/?a=1&b=<2>"`)
		assert.Contains(t, got, `"big":12345678901234567890`)
	})
}

func TestMaskURLCredentials(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"an empty value stays empty", ``, ``},
		{"a password and query values", `https://user:hunter2@hooks.example/x?token=abc&channel=ops`, `https://user:********@hooks.example/x?token=********&channel=********`},
		{"a user name alone", `https://tok123@ntfy.example`, `https://********@ntfy.example`},
		{"a user name alone with a path and a port", `https://tok123@ntfy.example:8443/topic`, `https://********@ntfy.example:8443/topic`},
		{"a user name with an empty password", `https://sk_live_abc:@hooks.example/x`, `https://********@hooks.example/x`},
		{"an empty user name and password", `https://:@hooks.example/x`, `https://:@hooks.example/x`},
		{"an empty user name with a password", `https://:hunter2@hooks.example/x`, `https://:********@hooks.example/x`},
		{"an empty user info", `https://@hooks.example/x`, `https://@hooks.example/x`},
		{"a password with an at sign in it", `https://user:p@ss@hooks.example/x`, `https://user:********@hooks.example/x`},
		{"an @ in the path is not user info", `https://hooks.example/@user/x`, `https://hooks.example/@user/x`},
		{"an @ in the query is not user info", `https://hooks.example/x?mail=a@b.example`, `https://hooks.example/x?mail=********`},
		{"an @ in the fragment is not user info", `https://hooks.example/x#a@b`, `https://hooks.example/x#a@b`},
		{"a query value that is empty stays", `https://hooks.example/x?a=&b=2`, `https://hooks.example/x?a=&b=********`},
		{"a query key with no value is masked", `https://hooks.example/x?secrettoken&b=2`, `https://hooks.example/x?********&b=********`},
		{"a query value with an equals sign", `https://hooks.example/x?a=b=c`, `https://hooks.example/x?a=********`},
		{"a query that is a bare question mark stays", `https://hooks.example/a%20b/?`, `https://hooks.example/a%20b/?`},
		{"empty query segments stay", `https://hooks.example/x?a=1&&b=2&`, `https://hooks.example/x?a=********&&b=********&`},
		{"a query value written with escapes", `https://hooks.example/x?a=%7Etok%20en`, `https://hooks.example/x?a=********`},
		{"the fragment stays", `https://hooks.example/x?a=1#frag?b=2`, `https://hooks.example/x?a=********#frag?b=2`},
		{"a query before a fragment with no path", `https://hooks.example?a=1#f`, `https://hooks.example?a=********#f`},
		{"everything at once", `https://u:p@h.example:8080/a%20b/c?x=1&y=#f`, `https://u:********@h.example:8080/a%20b/c?x=********&y=#f`},
		{"a bare host stays", `https://ntfy.sh`, `https://ntfy.sh`},
		{"a path with escapes stays as written", `https://hooks.example/a%20b/c%2Fd`, `https://hooks.example/a%20b/c%2Fd`},
		{"an IPv6 host stays", `http://[::1]:8080/x?a=1`, `http://[::1]:8080/x?a=********`},
		{"an upper case scheme stays", `HTTPS://User:pw@Hooks.Example/X`, `HTTPS://User:********@Hooks.Example/X`},
		{"a URL with nothing to mask comes back as it is", `http://127.0.0.1:1/hook`, `http://127.0.0.1:1/hook`},
		{"not a URL", `not a url`, `********`},
		{"no scheme", `hooks.example/x?token=abc`, `********`},
		{"no host", `https:///x?token=abc`, `********`},
		{"a scheme with no slashes", `mailto:user@hooks.example`, `********`},
		{"a bad escape", `https://user:pw@hooks.example/%zz`, `********`},
		{"a control character", "https://hooks.example/x\n?token=abc", `********`},
		{"a bad port", `https://user:pw@hooks.example:port/x`, `********`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, maskURLCredentials(tc.value))
		})
	}
}

func TestRedactConfigMasksURLCredentials(t *testing.T) {
	provider := &urlTestProvider{}
	cases := []struct {
		name     string
		provider NotificationChannelProvider
		config   string
		want     string
	}{
		{"a password and a query value are masked", provider, `{"webhook_url":"https://user:hunter2@hooks.example/x?token=hunter2"}`, `{"webhook_url":"https://user:********@hooks.example/x?token=********"}`},
		{"a user name alone is masked", provider, `{"signal_url":"https://tok123@signal.example"}`, `{"signal_url":"https://********@signal.example"}`},
		{"a URL with no credentials stays", provider, `{"webhook_url":"https://hooks.example/x","signal_url":"http://127.0.0.1:8080"}`, `{"webhook_url":"https://hooks.example/x","signal_url":"http://127.0.0.1:8080"}`},
		{"a URL that can't be read is masked whole", provider, `{"webhook_url":"not a url?token=hunter2"}`, `{"webhook_url":"********"}`},
		{"an empty URL stays empty", provider, `{"webhook_url":"","signal_url":""}`, `{"webhook_url":"","signal_url":""}`},
		{"a null URL stays null", provider, `{"webhook_url":null}`, `{"webhook_url":null}`},
		{"an absent URL stays absent", provider, `{"plain":"https://user:pw@x.example/?a=1"}`, `{"plain":"https://user:pw@x.example/?a=1"}`},
		{"a number is masked whole", provider, `{"webhook_url":123}`, `{"webhook_url":"********"}`},
		{"an object is masked whole", provider, `{"signal_url":{"a":"https://user:pw@x.example"}}`, `{"signal_url":"********"}`},
		{"an array is masked whole", provider, `{"webhook_url":["https://x.example"]}`, `{"webhook_url":"********"}`},
		{"a bool is masked whole", provider, `{"webhook_url":false}`, `{"webhook_url":"********"}`},
		{"a key of another letter case is the same field", provider, `{"Webhook_URL":"https://user:pw@x.example/?a=1","SIGNAL_URL":7}`, `{"Webhook_URL":"https://user:********@x.example/?a=********","SIGNAL_URL":"********"}`},
		{"a field that is not a URL credential field stays", provider, `{"plain":"https://user:pw@x.example/?a=1","webhook_url":"https://x.example"}`, `{"plain":"https://user:pw@x.example/?a=1","webhook_url":"https://x.example"}`},
		{"secrets are replaced beside the URLs", provider, `{"webhook_url":"https://u:p@x.example","token":"abc"}`, `{"webhook_url":"https://u:********@x.example","token":"********"}`},
		{"unknown fields stay", provider, `{"future":{"a":[1,2]},"webhook_url":"https://u:p@x.example"}`, `{"future":{"a":[1,2]},"webhook_url":"https://u:********@x.example"}`},
		{"a null config stays", provider, `null`, `null`},
		{"a config that isn't JSON comes back empty", provider, `{"webhook_url":"https://u:p@x.example"`, ``},
		{"a config that isn't an object comes back empty", provider, `["https://u:p@x.example"]`, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireSameConfig(t, tc.want, redactConfig(tc.provider, tc.config))
		})
	}

	t.Run("a type with only URL credentials masks them", func(t *testing.T) {
		got := redactConfig(&urlOnlyProvider{}, `{"signal_url":"https://u:p@x.example/?a=1"}`)
		assert.JSONEq(t, `{"signal_url":"https://u:********@x.example/?a=********"}`, got)
		assert.Equal(t, ``, redactConfig(&urlOnlyProvider{}, `{"signal_url":`), "and fails closed when the config can't be read")
	})

	t.Run("values that are not masked stay as they were written", func(t *testing.T) {
		got := redactConfig(provider, `{"webhook_url":"https://x.example/a&b","plain":"a&b<c>","big":12345678901234567890}`)
		assert.Contains(t, got, `"webhook_url":"https://x.example/a&b"`)
		assert.Contains(t, got, `"plain":"a&b<c>"`)
		assert.Contains(t, got, `"big":12345678901234567890`)
	})

	t.Run("a masked URL is not HTML escaped", func(t *testing.T) {
		got := redactConfig(provider, `{"webhook_url":"https://u:p@x.example/?a=<b>&c=d"}`)
		assert.Contains(t, got, `"webhook_url":"https://u:********@x.example/?a=********&c=********"`)
	})
}

func TestRedactModelLeavesTheChannelAlone(t *testing.T) {
	providers, _ := newSecretTestRegistry()
	config := `{"token":"abc"}`
	channel := &Model{ID: "channel-1", Name: "Alerts", Type: "test-secrets", Config: &config}

	redacted := redactModel(providers, channel)

	assert.Equal(t, `{"token":"********"}`, *redacted.Config)
	assert.Equal(t, "channel-1", redacted.ID)
	assert.Equal(t, "Alerts", redacted.Name)
	assert.Equal(t, `{"token":"abc"}`, *channel.Config, "the channel the service holds is untouched")
}

func TestRedactModels(t *testing.T) {
	providers, _ := newSecretTestRegistry()
	config := `{"token":"abc"}`

	assert.Nil(t, redactModels(providers, nil))
	assert.Equal(t, []*Model{}, redactModels(providers, []*Model{}))
	assert.Nil(t, redactModel(providers, nil))

	noConfig := &Model{ID: "channel-1", Type: "test-secrets"}
	redacted := redactModels(providers, []*Model{noConfig, {ID: "channel-2", Type: "test-secrets", Config: &config}})
	assert.Nil(t, redacted[0].Config)
	assert.Equal(t, `{"token":"********"}`, *redacted[1].Config)
}

// A channel whose type has no provider (it was removed) can't be told which of its fields are
// secrets, so none of its config is shown
func TestRedactModelHidesTheConfigOfAnUnknownType(t *testing.T) {
	providers, _ := newSecretTestRegistry()
	config := `{"token":"abc"}`
	channel := &Model{ID: "channel-1", Name: "Alerts", Type: "test-missing", Config: &config}

	redacted := redactModel(providers, channel)

	assert.Equal(t, "", *redacted.Config)
	assert.Equal(t, "Alerts", redacted.Name)
	assert.Equal(t, `{"token":"abc"}`, *channel.Config, "the channel the service holds is untouched")

	noConfig := &Model{ID: "channel-2", Type: "test-missing"}
	assert.Nil(t, redactModel(providers, noConfig).Config, "a channel with no config has none to show")
}
