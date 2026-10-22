package notification_channel

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// secretPlaceholder is what the API shows in place of a stored secret. A request that sends it
// back in a secret field keeps the stored secret.
const secretPlaceholder = "********"

// configField is a field of a provider's config that its struct marks with `secret:"true"`,
// `endpoint:"true"` or both. A secret is hidden from responses. An endpoint decides where the
// channel sends its secrets, so a saved secret can only be kept while it stays as it is.
type configField struct {
	// key is the JSON key of the field
	key string
	// index is the position of the field in the config struct
	index    int
	secret   bool
	endpoint bool
}

// configFields returns the secret and endpoint fields of a provider's config, which it reads from
// the struct the provider's Unmarshal returns. It returns nothing for a nil provider (an unknown
// channel type) or a provider that has no config struct.
func configFields(provider NotificationChannelProvider) []configField {
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

	var fields []configField
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		secret := field.Tag.Get("secret") == "true"
		endpoint := field.Tag.Get("endpoint") == "true"
		if !field.IsExported() || (!secret && !endpoint) {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields = append(fields, configField{key: name, index: i, secret: secret, endpoint: endpoint})
	}
	return fields
}

// secretFields returns the fields that hold secrets
func secretFields(provider NotificationChannelProvider) []configField {
	var secrets []configField
	for _, field := range configFields(provider) {
		if field.secret {
			secrets = append(secrets, field)
		}
	}
	return secrets
}

// secretKeys returns the config keys of a provider that hold secrets
func secretKeys(provider NotificationChannelProvider) []string {
	var keys []string
	for _, field := range secretFields(provider) {
		keys = append(keys, field.key)
	}
	return keys
}

// fieldForKey returns the field a config key fills. encoding/json fills a field from a key of
// any letter case, so a secret under "Bot_Token" is as much a secret as "bot_token".
func fieldForKey(fields []configField, key string) (configField, bool) {
	for _, field := range fields {
		if strings.EqualFold(field.key, key) {
			return field, true
		}
	}
	return configField{}, false
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

// duplicateKey finds two keys of a config that encoding/json fills the same field from: equal
// under case folding, or the very same key. It reports the first key that repeats an earlier one,
// and that earlier key. It reports nothing for a config that isn't a JSON object, or has a syntax
// error before a repeat, which validation rejects.
//
// Such a config is read one way by a provider (the last key wins) and another way by the
// helpers here, which hold each key apart. A request could use that to pass an endpoint check
// with one value and send with another, so it is refused before any secret is resolved.
func duplicateKey(config string) (key, earlier string, found bool) {
	decoder := json.NewDecoder(strings.NewReader(config))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return "", "", false
	}
	var seen []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return "", "", false
		}
		name, isKey := token.(string)
		if !isKey {
			return "", "", false
		}
		for _, previous := range seen {
			if strings.EqualFold(previous, name) {
				return name, previous, true
			}
		}
		seen = append(seen, name)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return "", "", false
		}
	}
	return "", "", false
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
	secrets := secretFields(provider)
	if len(secrets) == 0 {
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
		if _, isSecret := fieldForKey(secrets, key); !isSecret {
			continue
		}
		if value, ok := stringValue(raw); ok && value != "" {
			fields[key] = json.RawMessage(`"` + secretPlaceholder + `"`)
		}
	}
	return encodeConfig(fields)
}

// resolveSecrets returns incoming with each secret sent as the placeholder replaced by the
// secret the saved config holds, so that saving or testing a channel the API showed with
// placeholders keeps its secrets. saved is nil when there is no saved channel of the same type
// to resolve from.
//
// A placeholder is never a value. One that can't be resolved is left in place, and its key is
// returned in unresolved, sorted: the saved config isn't there, or holds no such secret, or the
// incoming config changed an endpoint, a field that decides where the secrets are sent. An
// endpoint that is itself a secret counts as unchanged when it is sent as the placeholder.
//
// An incoming config that isn't a JSON object is returned as it is, for validation to reject.
func resolveSecrets(provider NotificationChannelProvider, incoming string, saved *string) (resolved string, unresolved []string) {
	fields := configFields(provider)
	secrets := secretFields(provider)
	if len(secrets) == 0 {
		return incoming, nil
	}
	incomingFields, ok := parseConfig(incoming)
	if !ok || incomingFields == nil {
		return incoming, nil
	}

	placeholders := map[string]configField{}
	for key, raw := range incomingFields {
		field, isSecret := fieldForKey(secrets, key)
		if value, ok := stringValue(raw); isSecret && ok && value == secretPlaceholder {
			placeholders[key] = field
		}
	}
	if len(placeholders) == 0 {
		return incoming, nil
	}

	var savedFields map[string]json.RawMessage
	if saved != nil && endpointsUnchanged(provider, fields, incoming, *saved) {
		savedFields, _ = parseConfig(*saved)
	}

	missing := map[string]bool{}
	for key, field := range placeholders {
		secret, ok := savedSecret(savedFields, key)
		if !ok {
			missing[field.key] = true
			continue
		}
		incomingFields[key] = secret
	}
	if len(missing) > 0 {
		for key := range missing {
			unresolved = append(unresolved, key)
		}
		sort.Strings(unresolved)
		return incoming, unresolved
	}
	return encodeConfig(incomingFields), nil
}

// savedSecret returns the value of a secret in a saved config, as the JSON string it is written
// as. It is false when the saved config has no such secret, or an empty one.
func savedSecret(saved map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	if raw, ok := saved[key]; ok {
		value, isString := stringValue(raw)
		return raw, isString && value != ""
	}
	// A key of another letter case is the same field to encoding/json. Go through the keys in
	// order so that the same saved config always gives the same answer.
	savedKeys := make([]string, 0, len(saved))
	for savedKey := range saved {
		savedKeys = append(savedKeys, savedKey)
	}
	sort.Strings(savedKeys)
	for _, savedKey := range savedKeys {
		if !strings.EqualFold(savedKey, key) {
			continue
		}
		if value, isString := stringValue(saved[savedKey]); isString && value != "" {
			return saved[savedKey], true
		}
	}
	return nil, false
}

// endpointsUnchanged reports whether every endpoint field of the incoming config has the value
// it has in the saved config. The configs are read the way the provider reads them, so letter
// case and defaults don't matter. A secret endpoint sent as the placeholder is unchanged.
func endpointsUnchanged(provider NotificationChannelProvider, fields []configField, incoming, saved string) bool {
	var endpoints []configField
	for _, field := range fields {
		if field.endpoint {
			endpoints = append(endpoints, field)
		}
	}
	if len(endpoints) == 0 {
		return true
	}

	if strings.TrimSpace(saved) == "" {
		saved = "{}"
	}
	savedConfig, err := provider.Unmarshal(saved)
	if err != nil {
		return false
	}
	incomingConfig, err := provider.Unmarshal(incoming)
	if err != nil {
		return false
	}
	savedValues, incomingValues := reflect.Indirect(reflect.ValueOf(savedConfig)), reflect.Indirect(reflect.ValueOf(incomingConfig))
	if savedValues.Kind() != reflect.Struct || incomingValues.Kind() != reflect.Struct {
		return false
	}

	for _, field := range endpoints {
		incomingValue, savedValue := incomingValues.Field(field.index), savedValues.Field(field.index)
		if field.secret && incomingValue.Kind() == reflect.String && incomingValue.String() == secretPlaceholder {
			continue
		}
		if !reflect.DeepEqual(incomingValue.Interface(), savedValue.Interface()) {
			return false
		}
	}
	return true
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
