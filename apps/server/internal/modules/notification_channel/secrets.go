package notification_channel

import (
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// secretPlaceholder is what the API shows in place of a stored secret. A request that sends it
// back in a secret field keeps the stored secret.
const secretPlaceholder = "********"

// configField is a field of a provider's config that its struct marks with `secret:"true"`,
// `endpoint:"true"`, `urlCredentials:"true"` or a mix. A secret is hidden from responses. An
// endpoint decides where the channel sends its secrets, so a saved secret can only be kept while it
// stays as it is. A urlCredentials field is a URL the channel sends to, which isn't a secret itself
// but may hold credentials, so responses mask its user info and query values.
type configField struct {
	// key is the JSON key of the field
	key string
	// index is the position of the field in the config struct
	index          int
	secret         bool
	endpoint       bool
	urlCredentials bool
	// endpointDefault is, for an endpoint, the value the provider uses when the field is empty
	endpointDefault string
}

// configFields returns the secret, endpoint and URL credential fields of a provider's config, which it reads from
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
		urlCredentials := field.Tag.Get("urlCredentials") == "true"
		if !field.IsExported() || (!secret && !endpoint && !urlCredentials) {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields = append(fields, configField{
			key: name, index: i, secret: secret, endpoint: endpoint, urlCredentials: urlCredentials,
			endpointDefault: field.Tag.Get("endpointDefault"),
		})
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

// urlCredentialFields returns the fields that hold a URL whose credentials are masked
func urlCredentialFields(provider NotificationChannelProvider) []configField {
	var urls []configField
	for _, field := range configFields(provider) {
		if field.urlCredentials {
			urls = append(urls, field)
		}
	}
	return urls
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

// maskURLCredentials returns a URL with the credentials it carries replaced by the placeholder:
// the password of its user info (the user name stays), a user name that stands alone, since it is
// often a token, and every query value (the keys stay). The rest of the URL stays byte for byte as
// it was written, and a URL with nothing to mask comes back as it is. A value that can't be read
// as a URL with a scheme and a host comes back as the placeholder whole, since it may be a
// credential in any form. The empty string stays empty.
func maskURLCredentials(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	schemeEnd := strings.Index(value, "://")
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || schemeEnd < 0 {
		return secretPlaceholder
	}

	// The authority runs from after "://" up to the first "/", "?" or "#"
	authorityStart := schemeEnd + len("://")
	authorityEnd := len(value)
	if i := strings.IndexAny(value[authorityStart:], "/?#"); i >= 0 {
		authorityEnd = authorityStart + i
	}
	authority, rest := value[authorityStart:authorityEnd], value[authorityEnd:]

	// url.Parse ends the user info at the last "@" of the authority
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo := authority[:at]
		// A user name with an empty password ("sk_live_abc:@") stands alone too
		if user, password, _ := strings.Cut(userinfo, ":"); password != "" {
			userinfo = user + ":" + secretPlaceholder
		} else if user != "" {
			userinfo = secretPlaceholder
		}
		authority = userinfo + authority[at:]
	}

	// The query runs from the first "?" to the "#" that starts the fragment
	beforeFragment, fragment, hasFragment := strings.Cut(rest, "#")
	path, query, hasQuery := strings.Cut(beforeFragment, "?")
	if hasQuery {
		segments := strings.Split(query, "&")
		for i, segment := range segments {
			if segment == "" {
				continue
			}
			if key, queryValue, hasValue := strings.Cut(segment, "="); !hasValue {
				segments[i] = secretPlaceholder
			} else if queryValue != "" {
				segments[i] = key + "=" + secretPlaceholder
			}
		}
		query = "?" + strings.Join(segments, "&")
	}
	masked := value[:authorityStart] + authority + path + query
	if hasFragment {
		masked += "#" + fragment
	}
	return masked
}

// jsonString returns s as a JSON string, without escaping HTML so that it stays as written
func jsonString(s string) json.RawMessage {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(s); err != nil {
		return json.RawMessage(`"` + secretPlaceholder + `"`)
	}
	return json.RawMessage(strings.TrimSuffix(out.String(), "\n"))
}

// redactConfig returns config with each secret that is set replaced by the placeholder. A
// secret that is absent, null or an empty string stays as it is, so a response tells set from
// unset. Any other value under a secret's key (a number, a bool, an object, an array) is
// replaced too: a provider can't have read it as a secret, but it may still be one. A URL field
// that holds credentials (urlCredentials) keeps its URL with the credentials masked, as
// maskURLCredentials writes it, and any value of it that isn't a string (or null) is replaced
// whole. Fields that are neither are left alone.
//
// It fails closed. A config of a type with secrets or URL credentials that isn't a JSON object
// can't be read, and a config of a type with no provider (nil) can't be told secrets from the
// rest, so both come back empty rather than risk showing a secret in them.
func redactConfig(provider NotificationChannelProvider, config string) string {
	if provider == nil {
		return ""
	}
	secrets := secretFields(provider)
	urls := urlCredentialFields(provider)
	if len(secrets) == 0 && len(urls) == 0 {
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
		if _, isSecret := fieldForKey(secrets, key); isSecret {
			// stringValue reads null as the empty string
			if value, isString := stringValue(raw); !isString || value != "" {
				fields[key] = json.RawMessage(`"` + secretPlaceholder + `"`)
			}
			continue
		}
		if _, isURL := fieldForKey(urls, key); !isURL {
			continue
		}
		if value, isString := stringValue(raw); isString {
			if masked := maskURLCredentials(value); masked != value {
				fields[key] = jsonString(masked)
			}
		} else if string(bytes.TrimSpace(raw)) != "null" {
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
// A URL field that holds credentials (urlCredentials) is resolved first. The API shows it with
// the credentials masked, so one that holds the placeholder and is exactly the saved URL as the
// API shows it becomes the saved URL. That is the only way it resolves: changing any part of the
// URL means typing its credentials again. Then the secrets are resolved, with the endpoints
// checked against the URL as saved.
//
// A placeholder is never a value. One that can't be resolved is left in place, and its key is
// returned in unresolved, sorted: the saved config isn't there, or holds no such secret, or the
// incoming config changed an endpoint, a field that decides where the secrets are sent. A URL
// that holds the placeholder and isn't the saved URL as shown is unresolved too. An endpoint that
// is itself a secret counts as unchanged when it is sent as the placeholder.
//
// used are the saved secrets and URLs that were put in, for a caller that has to keep them out of
// a message. It is empty when anything is unresolved.
//
// An incoming config that isn't a JSON object is returned as it is, for validation to reject.
func resolveSecrets(provider NotificationChannelProvider, incoming string, saved *string) (resolved string, unresolved, used []string) {
	fields := configFields(provider)
	secrets := secretFields(provider)
	urls := urlCredentialFields(provider)
	if len(secrets) == 0 && len(urls) == 0 {
		return incoming, nil, nil
	}
	incomingFields, ok := parseConfig(incoming)
	if !ok || incomingFields == nil {
		return incoming, nil, nil
	}

	missing := map[string]bool{}

	// A URL that holds the placeholder is the saved URL as shown, or it is unresolved
	var savedFields map[string]json.RawMessage
	if saved != nil {
		savedFields, _ = parseConfig(*saved)
	}
	urlsResolved := false
	for key, raw := range incomingFields {
		field, isURL := fieldForKey(urls, key)
		if _, isSecret := fieldForKey(secrets, key); !isURL || isSecret {
			continue
		}
		value, isString := stringValue(raw)
		if !isString || !strings.Contains(value, secretPlaceholder) {
			continue
		}
		if savedRaw, ok := savedSecret(savedFields, key); ok {
			if savedValue, _ := stringValue(savedRaw); maskURLCredentials(savedValue) == value {
				incomingFields[key] = savedRaw
				used = append(used, savedValue)
				urlsResolved = true
				continue
			}
		}
		missing[field.key] = true
	}
	current := incoming
	if urlsResolved {
		current = encodeConfig(incomingFields)
	}

	placeholders := map[string]configField{}
	for key, raw := range incomingFields {
		field, isSecret := fieldForKey(secrets, key)
		if value, ok := stringValue(raw); isSecret && ok && value == secretPlaceholder {
			placeholders[key] = field
		}
	}

	var savedSecretFields map[string]json.RawMessage
	if len(placeholders) > 0 && saved != nil && endpointsUnchanged(provider, fields, current, *saved) {
		savedSecretFields, _ = parseConfig(*saved)
	}
	for key, field := range placeholders {
		secret, ok := savedSecret(savedSecretFields, key)
		if !ok {
			missing[field.key] = true
			continue
		}
		incomingFields[key] = secret
		if value, ok := stringValue(secret); ok {
			used = append(used, value)
		}
	}
	if len(missing) > 0 {
		for key := range missing {
			unresolved = append(unresolved, key)
		}
		sort.Strings(unresolved)
		return incoming, unresolved, nil
	}
	if len(placeholders) == 0 && !urlsResolved {
		return incoming, nil, nil
	}
	sort.Strings(used)
	return encodeConfig(incomingFields), nil, used
}

// maskSecrets returns text with every occurrence of each secret replaced by the placeholder, for
// an error message that may repeat what was sent. A failed request prints the URL it went to, so
// a secret is masked in the forms a URL and a quoted string write it in as well as the plain one.
func maskSecrets(text string, secrets []string) string {
	var forms []string
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		forms = append(forms, secret, url.PathEscape(secret), url.QueryEscape(secret), strings.Trim(strconv.Quote(secret), `"`), pathEscapedBySegment(secret))
		if parsed, err := url.Parse(secret); err == nil && parsed.Scheme != "" {
			forms = append(forms, urlForms(parsed)...)
		}
	}
	// A form that contains another is masked whole
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	for _, form := range forms {
		text = strings.ReplaceAll(text, form, secretPlaceholder)
	}
	return text
}

// pathEscapedBySegment escapes each segment of a path secret and keeps its slashes, the way a
// request URL prints it: url.PathEscape alone would turn the slashes into %2F
func pathEscapedBySegment(secret string) string {
	segments := strings.Split(secret, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// urlForms returns the ways a failed request prints a secret URL. net/http prints it with the
// password of its user info replaced by ***, and URL.Redacted writes xxxxx, so the secret is not
// in the error as it was entered. The path and the query are masked by themselves too, since
// they hold the token and an error may print only them. A path of just a slash is not masked,
// or every slash in the text would go.
func urlForms(parsed *url.URL) []string {
	forms := []string{parsed.String(), parsed.Redacted()}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		forms = append(forms, strings.Replace(parsed.String(), parsed.User.String()+"@", parsed.User.Username()+":***@", 1))
	}
	if path := parsed.EscapedPath(); path != "" && path != "/" {
		forms = append(forms, path, parsed.Path, parsed.RequestURI())
	}
	if parsed.RawQuery != "" {
		forms = append(forms, parsed.RawQuery)
	}
	return forms
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
// case doesn't matter, a field left out is its zero value (false, 0 or empty), and an empty field
// with an endpointDefault is that default. The web form fills in such defaults for a config that
// lacks them, and that is not a move. A secret endpoint sent as the placeholder is unchanged.
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
		if !reflect.DeepEqual(endpointValue(field, incomingValue), endpointValue(field, savedValue)) {
			return false
		}
	}
	return true
}

// endpointValue returns the value of an endpoint field as the provider sends with it: an empty
// field with an endpointDefault is that default.
func endpointValue(field configField, value reflect.Value) any {
	if field.endpointDefault != "" && value.Kind() == reflect.String && value.String() == "" {
		return field.endpointDefault
	}
	return value.Interface()
}

// redactModel returns a copy of channel with its secrets replaced, for a response. The channel
// itself is left as it is: it may be the one the service holds. A channel whose type has no
// provider shows no config.
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
