package notification_channel

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// secretPlaceholder is what the API shows in place of a stored secret. A request that sends it
// back in a secret field keeps the stored secret.
const secretPlaceholder = "********"

// secretKeys returns the config keys of a provider that hold secrets. A provider marks them with
// a `secret:"true"` tag on the field of the config its Unmarshal returns. It returns nothing for
// a nil provider (an unknown channel type) or a provider that has no config struct.
func secretKeys(provider NotificationChannelProvider) []string {
	if provider == nil {
		return nil
	}
	cfg, err := provider.Unmarshal("{}")
	if err != nil || cfg == nil {
		return nil
	}
	t := reflect.TypeOf(cfg)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var keys []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() || field.Tag.Get("secret") != "true" {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		keys = append(keys, name)
	}
	return keys
}

// isSecretKey reports whether a config key is one of keys. encoding/json fills a field from a
// key of any letter case, so a secret under "Bot_Token" is as much a secret as "bot_token".
func isSecretKey(keys []string, key string) bool {
	for _, secret := range keys {
		if strings.EqualFold(secret, key) {
			return true
		}
	}
	return false
}

// parseConfig reads a config as a JSON object, keeping every value as it is written, so that
// fields no provider knows about survive a round trip. ok is false for a config that isn't a
// JSON object.
func parseConfig(config string) (fields map[string]json.RawMessage, ok bool) {
	if err := json.Unmarshal([]byte(config), &fields); err != nil {
		return nil, false
	}
	return fields, true
}

// encodeConfig writes fields back as a JSON object, without escaping HTML so that values
// stay as they were written. It returns an empty config if encoding fails, which it can't for
// values read from JSON; an empty config shows nothing and fails validation.
func encodeConfig(fields map[string]json.RawMessage) string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(fields); err != nil {
		return ""
	}
	return strings.TrimSuffix(out.String(), "\n")
}

// stringValue returns the value of a JSON string, and false for any other kind of value
func stringValue(raw json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// redactConfig returns config with each secret that is set replaced by the placeholder. A
// secret that is absent or empty stays as it is, so a response tells set from unset. Fields
// that aren't secrets are left alone.
//
// A config of a type with secrets that isn't a JSON object can't be read, so it comes back
// empty rather than risk showing a secret in it.
func redactConfig(provider NotificationChannelProvider, config string) string {
	keys := secretKeys(provider)
	if len(keys) == 0 {
		return config
	}
	fields, ok := parseConfig(config)
	if !ok {
		return ""
	}
	if fields == nil { // the JSON null
		return config
	}

	for key, raw := range fields {
		if !isSecretKey(keys, key) {
			continue
		}
		if value, ok := stringValue(raw); ok && value != "" {
			fields[key] = json.RawMessage(`"` + secretPlaceholder + `"`)
		}
	}
	return encodeConfig(fields)
}

// redactModel returns a copy of channel with its secrets replaced, for a response. The channel
// itself is left as it is: it may be the one the service holds.
func redactModel(providers *ProviderRegistry, channel *Model) *Model {
	if channel == nil || channel.Config == nil {
		return channel
	}
	provider, _ := providers.Get(channel.Type)
	redacted := *channel
	config := redactConfig(provider, *channel.Config)
	redacted.Config = &config
	return &redacted
}

func redactModels(providers *ProviderRegistry, channels []*Model) []*Model {
	if channels == nil {
		return nil
	}
	redacted := make([]*Model, len(channels))
	for i, channel := range channels {
		redacted[i] = redactModel(providers, channel)
	}
	return redacted
}
