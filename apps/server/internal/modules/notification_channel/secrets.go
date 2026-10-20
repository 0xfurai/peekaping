package notification_channel

import (
	"encoding/json"
	"strings"
)

// A notification channel's credentials live outside its config. Config is what every
// response returns, so a secret kept there would be readable by anyone who can list
// channels; the secret column is never serialized (Model.Secret carries json:"-").
//
// The split happens on write and the merge on send, so the providers keep reading the
// whole thing from one JSON string and none of them had to change.

// secretKeyParts are the fragments that mark a config key as a credential. They cover
// every provider's fields today and, being substrings, a new provider that follows the
// same naming is covered without touching this list.
var secretKeyParts = []string{
	"password",
	"token",
	"secret",
	"api_key",
	"apikey",
	"_key",
	// A webhook URL is a bearer credential: anyone holding it can post.
	"webhook_url",
	// Integration URLs for PagerDuty and Grafana OnCall work the same way.
	"integration_url",
	"grafana_oncall_url",
	// Extra headers usually carry an Authorization value.
	"additional_headers",
}

// IsSecretKey reports whether a config key holds a credential.
func IsSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, part := range secretKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// SplitSecrets takes the config a client sent and the secrets already stored for the
// channel, and returns the config without credentials plus the credentials to store.
//
// A secret the client leaves empty is not a secret the client wants to erase: the stored
// one is kept, so editing a channel does not need the credentials typed again. That also
// means an empty secret cannot clear one, which is the behaviour #257 asks for.
func SplitSecrets(configJSON, storedSecretJSON string) (config, secret string, err error) {
	configMap := map[string]any{}
	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &configMap); err != nil {
			// A config that is not JSON is already unusable for every provider. It is
			// stored back untouched instead of turning a channel edit into an error.
			return configJSON, storedSecretJSON, nil
		}
	}

	secretMap := map[string]any{}
	if storedSecretJSON != "" {
		if err := json.Unmarshal([]byte(storedSecretJSON), &secretMap); err != nil {
			return "", "", err
		}
	}

	for key, value := range configMap {
		if !IsSecretKey(key) {
			continue
		}
		delete(configMap, key)
		if text, ok := value.(string); ok && text != "" {
			secretMap[key] = text
		}
	}

	config, err = marshalMap(configMap)
	if err != nil {
		return "", "", err
	}
	if len(secretMap) == 0 {
		return config, "", nil
	}
	secret, err = marshalMap(secretMap)
	return config, secret, err
}

// MergeSecret returns the config the providers read: the stored config with the secrets
// merged back in, secrets winning over anything left in config.
func MergeSecret(configJSON, secretJSON string) string {
	if secretJSON == "" {
		return configJSON
	}
	merged := map[string]any{}
	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &merged); err != nil {
			return configJSON
		}
	}
	secrets := map[string]any{}
	if err := json.Unmarshal([]byte(secretJSON), &secrets); err != nil {
		return configJSON
	}
	for key, value := range secrets {
		merged[key] = value
	}
	out, err := marshalMap(merged)
	if err != nil {
		return configJSON
	}
	return out
}

// Redact drops credentials from a config about to be returned. Rows written before
// secrets had their own column still carry them inside config, so responses cannot rely
// on the split alone.
func Redact(configJSON *string) *string {
	if configJSON == nil || *configJSON == "" {
		return configJSON
	}
	configMap := map[string]any{}
	if err := json.Unmarshal([]byte(*configJSON), &configMap); err != nil {
		return configJSON
	}
	changed := false
	for key := range configMap {
		if IsSecretKey(key) {
			delete(configMap, key)
			changed = true
		}
	}
	if !changed {
		return configJSON
	}
	out, err := marshalMap(configMap)
	if err != nil {
		return configJSON
	}
	return &out
}

// FullConfig is what a provider reads: config plus the secrets stored beside it.
func (m *Model) FullConfig() string {
	if m == nil || m.Config == nil {
		if m == nil || m.Secret == nil {
			return ""
		}
		return *m.Secret
	}
	secret := ""
	if m.Secret != nil {
		secret = *m.Secret
	}
	return MergeSecret(*m.Config, secret)
}

// marshalMap renders a config map, using {} rather than null for an empty one so the
// column never holds a value that cannot be parsed back.
func marshalMap(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// WriteSecrets turns what a client sent into the config and the secret to store. An
// explicit secret wins over credentials still sent inside config, and an empty one keeps
// what is already stored.
func WriteSecrets(configJSON, explicitSecret, storedSecret string) (config, secret string, err error) {
	config, secret, err = SplitSecrets(configJSON, storedSecret)
	if err != nil || explicitSecret == "" {
		return config, secret, err
	}

	merged := map[string]any{}
	if secret != "" {
		if err := json.Unmarshal([]byte(secret), &merged); err != nil {
			return "", "", err
		}
	}
	extra := map[string]any{}
	if err := json.Unmarshal([]byte(explicitSecret), &extra); err != nil {
		return "", "", err
	}
	for key, value := range extra {
		merged[key] = value
	}
	secret, err = marshalMap(merged)
	return config, secret, err
}
