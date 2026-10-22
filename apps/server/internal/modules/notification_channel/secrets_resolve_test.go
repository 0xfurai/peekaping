package notification_channel

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSecrets(t *testing.T) {
	provider := &secretTestProvider{}
	const saved = `{"url":"https://old.example","server":"https://server.example","token":"saved-token","password":"saved-password"}`
	const savedWithHook = `{"url":"https://old.example","server":"https://server.example","token":"saved-token","password":"saved-password","hook_url":"https://hook.example/saved"}`

	cases := []struct {
		name     string
		incoming string
		saved    *string
		want     string
		// wantUnresolved are the secrets left as the placeholder
		wantUnresolved []string
	}{
		{
			name:     "a placeholder becomes the saved secret",
			incoming: `{"url":"new","server":"https://server.example","token":"********","password":"********"}`,
			saved:    ptr(saved),
			want:     `{"url":"new","server":"https://server.example","token":"saved-token","password":"saved-password"}`,
		},
		{
			name:     "a new value stays",
			incoming: `{"server":"https://server.example","token":"fresh","password":"********"}`,
			saved:    ptr(saved),
			want:     `{"server":"https://server.example","token":"fresh","password":"saved-password"}`,
		},
		{
			name:     "an empty value stays empty",
			incoming: `{"server":"https://server.example","token":"********","password":""}`,
			saved:    ptr(saved),
			want:     `{"server":"https://server.example","token":"saved-token","password":""}`,
		},
		{
			name:     "an absent secret stays absent",
			incoming: `{"server":"https://server.example","token":"********"}`,
			saved:    ptr(saved),
			want:     `{"server":"https://server.example","token":"saved-token"}`,
		},
		{
			name:     "a secret endpoint sent as the placeholder is unchanged, and is resolved too",
			incoming: `{"server":"https://server.example","hook_url":"********","token":"********"}`,
			saved:    ptr(savedWithHook),
			want:     `{"server":"https://server.example","hook_url":"https://hook.example/saved","token":"saved-token"}`,
		},
		{
			name:     "a secret endpoint sent with its saved value is unchanged",
			incoming: `{"server":"https://server.example","hook_url":"https://hook.example/saved","token":"********"}`,
			saved:    ptr(savedWithHook),
			want:     `{"server":"https://server.example","hook_url":"https://hook.example/saved","token":"saved-token"}`,
		},
		{
			name:     "endpoints that are absent on both sides are unchanged",
			incoming: `{"token":"********"}`,
			saved:    ptr(`{"token":"saved-token"}`),
			want:     `{"token":"saved-token"}`,
		},
		{
			name:     "an endpoint under a key of another letter case is unchanged",
			incoming: `{"SERVER":"https://server.example","token":"********"}`,
			saved:    ptr(saved),
			want:     `{"SERVER":"https://server.example","token":"saved-token"}`,
		},
		{
			name:     "a placeholder is resolved under a key of another letter case",
			incoming: `{"server":"https://server.example","TOKEN":"********"}`,
			saved:    ptr(saved),
			want:     `{"server":"https://server.example","TOKEN":"saved-token"}`,
		},
		{
			name:     "a placeholder is resolved from a saved key of another letter case",
			incoming: `{"token":"********"}`,
			saved:    ptr(`{"Token":"saved-token"}`),
			want:     `{"token":"saved-token"}`,
		},
		{
			name:     "an endpoint with a default is unchanged when the form fills the default in",
			incoming: `{"server":"https://server.example","region":"us-east","token":"********"}`,
			saved:    ptr(saved),
			want:     `{"server":"https://server.example","region":"us-east","token":"saved-token"}`,
		},
		{
			name:     "an endpoint with a default is unchanged when the form leaves the default out",
			incoming: `{"server":"https://server.example","token":"********"}`,
			saved:    ptr(`{"server":"https://server.example","region":"us-east","token":"saved-token"}`),
			want:     `{"server":"https://server.example","token":"saved-token"}`,
		},
		{
			name:     "an endpoint with a default is unchanged when the form sends it empty",
			incoming: `{"server":"https://server.example","region":"","token":"********"}`,
			saved:    ptr(`{"server":"https://server.example","region":"us-east","token":"saved-token"}`),
			want:     `{"server":"https://server.example","region":"","token":"saved-token"}`,
		},
		{
			name:           "an endpoint with a default is changed by another value",
			incoming:       `{"server":"https://server.example","region":"eu-west","token":"********"}`,
			saved:          ptr(saved),
			want:           `{"server":"https://server.example","region":"eu-west","token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:           "an endpoint with a default is changed by a value saved other than the default",
			incoming:       `{"server":"https://server.example","token":"********"}`,
			saved:          ptr(`{"server":"https://server.example","region":"eu-west","token":"saved-token"}`),
			want:           `{"server":"https://server.example","token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:           "a changed endpoint leaves the placeholders",
			incoming:       `{"server":"https://elsewhere.example","token":"********","password":"********"}`,
			saved:          ptr(saved),
			want:           `{"server":"https://elsewhere.example","token":"********","password":"********"}`,
			wantUnresolved: []string{"password", "token"},
		},
		{
			name:           "an endpoint left out leaves the placeholders",
			incoming:       `{"token":"********"}`,
			saved:          ptr(saved),
			want:           `{"token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:           "a changed secret endpoint leaves the other placeholders",
			incoming:       `{"server":"https://server.example","hook_url":"https://hook.example/other","token":"********"}`,
			saved:          ptr(savedWithHook),
			want:           `{"server":"https://server.example","hook_url":"https://hook.example/other","token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:           "a cleared secret endpoint leaves the other placeholders",
			incoming:       `{"server":"https://server.example","hook_url":"","token":"********"}`,
			saved:          ptr(savedWithHook),
			want:           `{"server":"https://server.example","hook_url":"","token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:     "a changed secret endpoint with no placeholder to resolve is saved as it is",
			incoming: `{"server":"https://server.example","hook_url":"https://hook.example/other","token":"fresh"}`,
			saved:    ptr(saved),
			want:     `{"server":"https://server.example","hook_url":"https://hook.example/other","token":"fresh"}`,
		},
		{
			name:           "without a saved channel a placeholder is left",
			incoming:       `{"token":"********"}`,
			saved:          nil,
			want:           `{"token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:           "a placeholder for a secret that wasn't saved is left",
			incoming:       `{"server":"https://server.example","token":"********","password":"********"}`,
			saved:          ptr(`{"server":"https://server.example","token":"saved-token"}`),
			want:           `{"server":"https://server.example","token":"********","password":"********"}`,
			wantUnresolved: []string{"password"},
		},
		{
			name:           "a placeholder for an empty saved secret is left",
			incoming:       `{"token":"********"}`,
			saved:          ptr(`{"token":""}`),
			want:           `{"token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:           "a placeholder is left when the saved config isn't JSON",
			incoming:       `{"token":"********"}`,
			saved:          ptr(`{"token":`),
			want:           `{"token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:           "a placeholder is left when the saved config is empty",
			incoming:       `{"token":"********"}`,
			saved:          ptr(``),
			want:           `{"token":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:           "unresolved secrets are named by their key, once",
			incoming:       `{"Token":"********","TOKEN":"********"}`,
			saved:          nil,
			want:           `{"Token":"********","TOKEN":"********"}`,
			wantUnresolved: []string{"token"},
		},
		{
			name:     "the placeholder in a field that isn't a secret stays",
			incoming: `{"url":"********","server":"https://server.example","token":"abc"}`,
			saved:    ptr(saved),
			want:     `{"url":"********","server":"https://server.example","token":"abc"}`,
		},
		{
			name:     "unknown fields stay",
			incoming: `{"future":{"a":[1,2]},"server":"https://server.example","token":"********"}`,
			saved:    ptr(saved),
			want:     `{"future":{"a":[1,2]},"server":"https://server.example","token":"saved-token"}`,
		},
		{
			name:     "a config that isn't JSON is left for validation",
			incoming: `{"token":"********"`,
			saved:    ptr(saved),
			want:     `{"token":"********"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, unresolved, _ := resolveSecrets(provider, tc.incoming, tc.saved)
			requireSameConfig(t, tc.want, got)
			assert.Equal(t, tc.wantUnresolved, unresolved)
		})
	}

	t.Run("it reports the saved secrets it put in, sorted, and not the ones the request typed", func(t *testing.T) {
		_, unresolved, used := resolveSecrets(provider, `{"server":"https://server.example","token":"fresh","password":"********","hook_url":"********"}`, ptr(savedWithHook))
		assert.Empty(t, unresolved)
		assert.Equal(t, []string{"https://hook.example/saved", "saved-password"}, used)
	})

	t.Run("it reports nothing put in while a placeholder is unresolved", func(t *testing.T) {
		_, unresolved, used := resolveSecrets(provider, `{"server":"https://server.example","token":"********","password":"********"}`, ptr(`{"server":"https://server.example","token":"saved-token"}`))
		assert.Equal(t, []string{"password"}, unresolved)
		assert.Empty(t, used)
	})

	t.Run("a type with no secrets keeps its config", func(t *testing.T) {
		got, unresolved, _ := resolveSecrets(noSecretsProvider{}, `{"token":"********"}`, nil)
		assert.Equal(t, `{"token":"********"}`, got)
		assert.Empty(t, unresolved)
	})

	t.Run("an unknown type keeps its config", func(t *testing.T) {
		got, unresolved, _ := resolveSecrets(nil, `{"token":"********"}`, nil)
		assert.Equal(t, `{"token":"********"}`, got)
		assert.Empty(t, unresolved)
	})

	t.Run("a config with nothing to resolve comes back byte for byte", func(t *testing.T) {
		incoming := "{ \"token\" :  \"fresh\",\n \"url\": \"u\" }"
		got, unresolved, _ := resolveSecrets(provider, incoming, ptr(saved))
		assert.Equal(t, incoming, got)
		assert.Empty(t, unresolved)
	})

	t.Run("values stay as they were written", func(t *testing.T) {
		got, _, _ := resolveSecrets(provider, `{"url":"https://example.com/?a=1&b=<2>","big":12345678901234567890,"server":"https://server.example","token":"********"}`, ptr(saved))
		assert.Contains(t, got, `"url":"https://example.com/?a=1&b=<2>"`)
		assert.Contains(t, got, `"big":12345678901234567890`)
	})

	t.Run("a saved secret with characters JSON escapes comes back whole", func(t *testing.T) {
		got, _, _ := resolveSecrets(provider, `{"token":"********"}`, ptr(`{"token":"a\"b\\c<d>&e"}`))
		var fields map[string]string
		require.NoError(t, json.Unmarshal([]byte(got), &fields))
		assert.Equal(t, `a"b\c<d>&e`, fields["token"])
	})
}

func TestMaskSecrets(t *testing.T) {
	assert.Equal(t, "nothing to hide", maskSecrets("nothing to hide", nil))
	assert.Equal(t, "nothing to hide", maskSecrets("nothing to hide", []string{""}), "an empty secret masks nothing")
	assert.Equal(t, "a ******** b ********", maskSecrets("a tok b tok", []string{"tok"}), "every occurrence")
	assert.Equal(t, "********", maskSecrets("abcdef", []string{"abc", "abcdef"}), "a secret that contains another is masked whole")
	assert.Equal(t, "get ********/send", maskSecrets("get a%2Fb%20c/send", []string{"a/b c"}), "as a path segment")
	assert.Equal(t, "get ?t=********", maskSecrets("get ?t=a%2Fb+c%26d", []string{"a/b c&d"}), "as a query value")
	assert.Equal(t, `Post "********": refused`, maskSecrets(`Post "https://hook.example/a%20b": refused`, []string{"https://hook.example/a b"}), "as a URL net/url writes back")

	// A secret URL with user info: net/http prints it with the password replaced by ***, and
	// URL.Redacted writes xxxxx
	const hookWithUser = "http://user:pw@127.0.0.1:1/api/webhooks/1/TOKENPATH"
	assert.Equal(t, `Post "********": dial tcp`, maskSecrets(`Post "http://user:***@127.0.0.1:1/api/webhooks/1/TOKENPATH": dial tcp`, []string{hookWithUser}), "as net/http prints it")
	assert.Equal(t, `Post "********": dial tcp`, maskSecrets(`Post "http://user:xxxxx@127.0.0.1:1/api/webhooks/1/TOKENPATH": dial tcp`, []string{hookWithUser}), "as URL.Redacted writes it")
	assert.Equal(t, "status 404 for ********", maskSecrets("status 404 for /api/webhooks/1/TOKENPATH", []string{hookWithUser}), "as its path and query")
	assert.Equal(t, "got ?********", maskSecrets("got ?key=K&token=T", []string{"http://127.0.0.1:1/?key=K&token=T"}), "as its query")
	assert.Equal(t, "see /, not ********", maskSecrets("see /, not /x", []string{"http://127.0.0.1:1/x"}), "a URL with no path to speak of doesn't mask every slash")
	assert.Equal(t, "see /, host 127.0.0.1", maskSecrets("see /, host 127.0.0.1", []string{"http://127.0.0.1"}), "a bare host URL masks nothing but itself")

	// A path secret with a slash: the slash stays in a printed URL, and a space is escaped
	assert.Equal(t,
		`Get "https://api.telegram.org/bot********/sendMessage?chat_id=1": refused`,
		maskSecrets(`Get "https://api.telegram.org/bot123:ABC/def%20ghi/sendMessage?chat_id=1": refused`, []string{"123:ABC/def ghi"}),
		"escaped segment by segment")
}

func TestDuplicateKey(t *testing.T) {
	cases := []struct {
		name        string
		config      string
		wantKey     string
		wantEarlier string
	}{
		{"no repeat", `{"a":1,"b":2}`, "", ""},
		{"the same key twice", `{"a":1,"a":2}`, "a", "a"},
		{"another letter case", `{"server_url":"x","SERVER_URL":"y"}`, "SERVER_URL", "server_url"},
		{"the third key repeats the first", `{"a":1,"b":2,"A":3}`, "A", "a"},
		{"a repeat after a nested object", `{"a":{"x":1,"X":2},"b":[{"c":1,"C":2}],"B":3}`, "B", "b"},
		{"keys repeated only inside values", `{"a":{"x":1,"X":2},"b":[{"c":1,"C":2}]}`, "", ""},
		{"an empty object", `{}`, "", ""},
		{"an empty config", ``, "", ""},
		{"a config that isn't an object", `["a","A"]`, "", ""},
		{"the JSON null", `null`, "", ""},
		{"a syntax error", `{"a":1,"b":`, "", ""},
		{"a repeat before a syntax error", `{"a":1,"A":2,`, "A", "a"},
		{"whitespace around keys", " {\n \"a\" : 1 ,\n \"A\" : 2 }", "A", "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, earlier, found := duplicateKey(tc.config)
			assert.Equal(t, tc.wantKey != "", found)
			assert.Equal(t, tc.wantKey, key)
			assert.Equal(t, tc.wantEarlier, earlier)
		})
	}
}

func ptr(s string) *string { return &s }
