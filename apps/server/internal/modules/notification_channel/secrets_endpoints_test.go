package notification_channel

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type endpointlessConfig struct {
	Token string `json:"token" secret:"true"`
}

type endpointTestConfig struct {
	Server string `json:"server" endpoint:"true"`
	Token  string `json:"token" secret:"true"`
}

// strictProvider reads a config into the struct, and fails for a config of the wrong shape
func strictProvider(newConfig func() any) NotificationChannelProvider {
	return targetTestProvider{unmarshal: func(config string) (any, error) {
		cfg := newConfig()
		if err := json.Unmarshal([]byte(config), cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}}
}

// A secret that is sent as the placeholder is the saved one when no endpoint decides where it goes
func TestResolveSecretsOfATypeWithNoEndpoint(t *testing.T) {
	provider := strictProvider(func() any { return &endpointlessConfig{} })

	resolved, unresolved, used := resolveSecrets(provider, `{"token":"********"}`, ptr(`{"token":"saved-token"}`))

	assert.JSONEq(t, `{"token":"saved-token"}`, resolved)
	assert.Empty(t, unresolved)
	assert.Equal(t, []string{"saved-token"}, used)
}

// A secret stays unresolved when the endpoints can't be compared: a config the provider can't read
// into its struct, or a provider whose config is no struct
func TestResolveSecretsLeavesThePlaceholderWhenTheEndpointsCantBeCompared(t *testing.T) {
	const saved = `{"server":"https://server.example","token":"saved-token"}`
	notAStruct := targetTestProvider{unmarshal: func(config string) (any, error) {
		if config == "{}" {
			return &endpointTestConfig{}, nil
		}
		return "not a struct", nil
	}}
	cases := []struct {
		name     string
		provider NotificationChannelProvider
		incoming string
		saved    string
	}{
		{"an incoming config of the wrong shape", strictProvider(func() any { return &endpointTestConfig{} }), `{"server":5,"token":"********"}`, saved},
		{"a saved config of the wrong shape", strictProvider(func() any { return &endpointTestConfig{} }), `{"server":"https://server.example","token":"********"}`, `{"server":5,"token":"saved-token"}`},
		{"a config that is no struct", notAStruct, `{"server":"https://server.example","token":"********"}`, saved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolved, unresolved, used := resolveSecrets(tc.provider, tc.incoming, ptr(tc.saved))

			assert.Equal(t, tc.incoming, resolved)
			assert.Equal(t, []string{"token"}, unresolved)
			assert.Empty(t, used)
		})
	}
}

func TestEndpointsUnchanged(t *testing.T) {
	provider := strictProvider(func() any { return &endpointTestConfig{} })
	fields := configFields(provider)
	require.NotEmpty(t, fields)

	t.Run("a type with no endpoint has none that moved", func(t *testing.T) {
		endpointless := strictProvider(func() any { return &endpointlessConfig{} })

		assert.True(t, endpointsUnchanged(endpointless, configFields(endpointless), `{"token":"a"}`, `not even JSON`))
	})
	t.Run("a saved config that is blank is an empty one", func(t *testing.T) {
		assert.True(t, endpointsUnchanged(provider, fields, `{}`, ``))
		assert.True(t, endpointsUnchanged(provider, fields, `{"token":"x"}`, "  \n"))
		assert.False(t, endpointsUnchanged(provider, fields, `{"server":"https://server.example"}`, ``))
	})
	t.Run("an endpoint that moved", func(t *testing.T) {
		assert.False(t, endpointsUnchanged(provider, fields, `{"server":"https://other.example"}`, `{"server":"https://server.example"}`))
		assert.True(t, endpointsUnchanged(provider, fields, `{"SERVER":"https://server.example"}`, `{"server":"https://server.example"}`))
	})
	t.Run("a config that can't be read", func(t *testing.T) {
		assert.False(t, endpointsUnchanged(provider, fields, `{"server":"a"}`, `{"server":5}`))
		assert.False(t, endpointsUnchanged(provider, fields, `{"server":5}`, `{"server":"a"}`))
	})
}
