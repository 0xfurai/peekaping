package notification_channel

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type configFieldsTestConfig struct {
	Token string `json:"token,omitempty" secret:"true"`
	// A key of "-" is no key of the config
	Skipped string `json:"-" secret:"true"`
	// With no key in its tag, a field is named as it is in the struct
	Untitled string `secret:"true"`
	Plain    string `json:"plain"`
	// Only an exported field is read from the config
	hidden string `secret:"true"`
	Host   string `json:"host" target:"host"`
	Link   string `json:"link" endpoint:"true" endpointDefault:"https://link.example" urlCredentials:"true" target:"url"`
}

func TestConfigFields(t *testing.T) {
	configOf := func(cfg any) NotificationChannelProvider {
		return targetTestProvider{unmarshal: func(string) (any, error) { return cfg, nil }}
	}
	pointer := &configFieldsTestConfig{hidden: "set"}

	t.Run("the tagged fields of a config, by their keys", func(t *testing.T) {
		want := []configField{
			{key: "token", index: 0, secret: true},
			{key: "Untitled", index: 2, secret: true},
			{key: "host", index: 5, target: "host"},
			{key: "link", index: 6, endpoint: true, urlCredentials: true, target: "url", endpointDefault: "https://link.example"},
		}

		assert.Equal(t, want, configFields(configOf(pointer)))
		assert.Equal(t, want, configFields(configOf(&pointer)), "whatever the number of pointers to it")
		assert.Equal(t, want, configFields(configOf(*pointer)), "and without one")
	})

	t.Run("nothing for a config that is not a struct, or that can't be read", func(t *testing.T) {
		assert.Nil(t, configFields(nil), "a type with no provider")
		assert.Nil(t, configFields(configOf(nil)))
		assert.Nil(t, configFields(configOf(map[string]string{"token": ""})))
		assert.Nil(t, configFields(configOf([]string{"token"})))
		assert.Nil(t, configFields(targetTestProvider{unmarshal: func(string) (any, error) { return nil, errors.New("not a config") }}))
	})
}
