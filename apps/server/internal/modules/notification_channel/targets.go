package notification_channel

import (
	"net/url"
	"reflect"
)

// targetFields returns the fields of a provider's config that name an address the provider connects
// to, which its struct marks with `target:"url"` (a URL) or `target:"host"` (a host name). A field
// that holds a link the provider only sends along, such as Teams' server URL, is not one.
func targetFields(provider NotificationChannelProvider) []configField {
	var targets []configField
	for _, field := range configFields(provider) {
		if field.target != "" {
			targets = append(targets, field)
		}
	}
	return targets
}

// CheckTargets is the check at save time of BLOCK_PRIVATE_TARGETS (PP-158): it refuses a config whose
// URL or host is a blocked IP address, with the *netguard.TargetError the API answers as 400. A name
// passes, since what it resolves to can change and is checked when the provider dials. A config it
// can't read, a type without a provider and a field that is empty or isn't a URL pass too: the
// provider's own validation refuses those. The check is off while the guard is.
func (r *ProviderRegistry) CheckTargets(channelType, config string) error {
	if !r.guard.Enabled() {
		return nil
	}
	provider, ok := r.Get(channelType)
	if !ok {
		return nil
	}
	value, ok := configStruct(provider, config)
	if !ok {
		return nil
	}
	for _, field := range targetFields(provider) {
		if err := r.checkTarget(field, value); err != nil {
			return err
		}
	}
	return nil
}

// configStruct returns the struct the provider reads the config into, and false for a config it
// can't read, or that isn't a struct
func configStruct(provider NotificationChannelProvider, config string) (reflect.Value, bool) {
	parsed, err := provider.Unmarshal(config)
	if err != nil || parsed == nil {
		return reflect.Value{}, false
	}
	value := reflect.ValueOf(parsed)
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value, value.Kind() == reflect.Struct
}

// checkTarget refuses a field that is a blocked IP address, or a URL of one. A field that isn't a
// string, or a URL, passes.
func (r *ProviderRegistry) checkTarget(field configField, config reflect.Value) error {
	text, ok := config.Field(field.index).Interface().(string)
	if !ok {
		return nil
	}
	host := text
	if field.target == "url" {
		u, err := url.Parse(text)
		if err != nil {
			return nil
		}
		host = u.Hostname()
	}
	return r.guard.CheckLiteral(host)
}
