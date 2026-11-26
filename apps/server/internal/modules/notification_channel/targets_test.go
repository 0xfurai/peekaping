package notification_channel

import (
	"errors"
	"reflect"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/installsettings"
	"peekaping/internal/netguard"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func guardedRegistry(on bool) *ProviderRegistry {
	return NewProviderRegistry(zap.NewNop().Sugar(), installsettings.Static(&config.Config{}), nil, nil, netguard.New(on))
}

// A channel whose URL or SMTP host is a blocked address is refused when it is saved or tested
func TestProviderRegistry_CheckTargetsRefusesABlockedLiteral(t *testing.T) {
	tests := []struct {
		name, channelType, config, host string
	}{
		{"webhook on loopback", "webhook", `{"webhook_url":"http://127.0.0.1/x","webhook_content_type":"json"}`, "127.0.0.1"},
		{"webhook on the metadata address", "webhook", `{"webhook_url":"http://169.254.169.254/latest","webhook_content_type":"json"}`, "169.254.169.254"},
		{"webhook on IPv6 loopback", "webhook", `{"webhook_url":"http://[::1]:8080/x","webhook_content_type":"json"}`, "::1"},
		{"ntfy", "ntfy", `{"server_url":"http://10.0.0.5:8080","topic":"t","authentication_type":"none","priority":3}`, "10.0.0.5"},
		{"gotify", "gotify", `{"server_url":"http://192.168.1.2","gotify_application_token":"x"}`, "192.168.1.2"},
		{"matrix", "matrix", `{"homeserver_url":"http://172.16.0.9"}`, "172.16.0.9"},
		{"signal", "signal", `{"signal_url":"http://127.0.0.1:8080"}`, "127.0.0.1"},
		{"slack", "slack", `{"slack_webhook_url":"http://127.0.0.1/hook"}`, "127.0.0.1"},
		{"smtp", "smtp", `{"smtp_host":"10.0.0.1","smtp_port":25}`, "10.0.0.1"},
		{"smtp on IPv6 loopback", "smtp", `{"smtp_host":"::1","smtp_port":25}`, "::1"},
	}
	registry := guardedRegistry(true)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := registry.CheckTargets(tc.channelType, tc.config)

			var blocked *netguard.TargetError
			require.True(t, errors.As(err, &blocked), "err: %v", err)
			assert.Equal(t, netguard.CodeTargetNotAllowed, blocked.Code)
			assert.Equal(t, tc.host, blocked.Host)
		})
	}
}

func TestProviderRegistry_CheckTargetsLetsAThroughWhatItCannotJudge(t *testing.T) {
	registry := guardedRegistry(true)
	for name, tc := range map[string]struct{ channelType, config string }{
		"a name":                              {"webhook", `{"webhook_url":"http://hooks.corp.example/x","webhook_content_type":"json"}`},
		"localhost, a name":                   {"webhook", `{"webhook_url":"http://localhost/x","webhook_content_type":"json"}`},
		"a public address":                    {"webhook", `{"webhook_url":"https://8.8.8.8/x","webhook_content_type":"json"}`},
		"a field that is empty":               {"telegram", `{"bot_token":"x","chat_id":"1"}`},
		"a link the channel only sends along": {"teams", `{"webhook_url":"https://corp.webhook.office.com/x","server_url":"http://127.0.0.1"}`},
		"a smtp host that is a name":          {"smtp", `{"smtp_host":"smtp.example.com","smtp_port":587}`},
		"a config that is not JSON":           {"webhook", `nope`},
		"a URL that doesn't parse":            {"webhook", `{"webhook_url":"http://[::1","webhook_content_type":"json"}`},
		"an unknown type":                     {"carrier-pigeon", `{"webhook_url":"http://127.0.0.1"}`},
		"the install email channel":           {"install_email", `{"to":"a@example.com"}`},
	} {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, registry.CheckTargets(tc.channelType, tc.config))
		})
	}
}

func TestProviderRegistry_CheckTargetsDoesNothingWhileTheGuardIsOffOrMissing(t *testing.T) {
	blocked := `{"webhook_url":"http://127.0.0.1/x","webhook_content_type":"json"}`

	assert.NoError(t, guardedRegistry(false).CheckTargets("webhook", blocked))
	assert.NoError(t, NewProviderRegistry(zap.NewNop().Sugar(), installsettings.Static(&config.Config{}), nil, nil, nil).CheckTargets("webhook", blocked))
}

// Every endpoint of a provider that is a URL or host must be marked as a target, or the guard's check
// at save time would miss it. The provider's own dial still refuses it, but a save would not.
func TestProviderRegistry_EveryEndpointThatIsAddressedByNameIsATarget(t *testing.T) {
	registry := guardedRegistry(true)
	for _, channelType := range registry.Types() {
		provider, _ := registry.Get(channelType)
		cfg, err := provider.Unmarshal("{}")
		require.NoError(t, err)
		typ := reflect.TypeOf(cfg).Elem()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Tag.Get("endpoint") != "true" || field.Type.Kind() != reflect.String {
				continue
			}
			assert.NotEmpty(t, field.Tag.Get("target"), "%s.%s is an endpoint with no target tag", channelType, field.Name)
		}
	}
}

// targetTestProvider is a provider whose config is whatever unmarshal returns
type targetTestProvider struct {
	NotificationChannelProvider
	unmarshal func(config string) (any, error)
}

func (p targetTestProvider) Unmarshal(config string) (any, error) { return p.unmarshal(config) }

type targetTestConfig struct {
	Port int    `json:"port" target:"host"`
	Host string `json:"host" target:"host"`
}

// What the check cannot read as a struct of strings it lets through, and it goes on to the targets
// after it
func TestProviderRegistry_CheckTargetsLetsThroughAConfigItCannotRead(t *testing.T) {
	provider := targetTestProvider{unmarshal: func(config string) (any, error) {
		switch config {
		case "null":
			return nil, nil
		case "list":
			return []string{"127.0.0.1"}, nil
		default:
			return &targetTestConfig{Port: 25, Host: "127.0.0.1"}, nil
		}
	}}
	registry := &ProviderRegistry{byType: map[string]NotificationChannelProvider{"test-targets": provider}, guard: netguard.New(true)}

	assert.NoError(t, registry.CheckTargets("test-targets", "null"))
	assert.NoError(t, registry.CheckTargets("test-targets", "list"))

	// A target that is not a string is skipped, and the host after it is checked
	var blocked *netguard.TargetError
	require.True(t, errors.As(registry.CheckTargets("test-targets", "{}"), &blocked))
	assert.Equal(t, "127.0.0.1", blocked.Host)
}
