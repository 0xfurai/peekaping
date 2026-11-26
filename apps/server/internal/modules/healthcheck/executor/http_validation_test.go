package executor

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// reportedHTTPErrors validates an HTTP monitor's config and lists what it reports, as
// "<field>:<tag>" in the order it reports it.
func reportedHTTPErrors(t *testing.T, config string) []string {
	t.Helper()
	err := NewHTTPExecutor(zap.NewNop().Sugar()).Validate(config)
	if err == nil {
		return nil
	}
	var validationErrors validator.ValidationErrors
	require.True(t, errors.As(err, &validationErrors), err.Error())
	reported := make([]string, 0, len(validationErrors))
	for _, fe := range validationErrors {
		reported = append(reported, fe.Field()+":"+fe.Tag())
	}
	return reported
}

func TestHTTPConfigStructLevelValidation_Body(t *testing.T) {
	config := func(encoding, body string) string {
		quoted, err := json.Marshal(body)
		require.NoError(t, err)
		return `{"url": "http://example.com", "method": "POST", "encoding": "` + encoding + `", "body": ` + string(quoted) + `, "accepted_statuscodes": ["2XX"], "authMethod": "none"}`
	}

	tests := []struct {
		name     string
		encoding string
		body     string
		want     []string
	}{
		{name: "valid json", encoding: "json", body: `{"a": 1}`},
		{name: "invalid json", encoding: "json", body: `{"a": `, want: []string{"Body:json"}},
		{name: "empty json body", encoding: "json", body: ""},
		{name: "valid form", encoding: "form", body: "a=1&b=2"},
		{name: "invalid form", encoding: "form", body: "a=%zz", want: []string{"Body:form"}},
		{name: "empty form body", encoding: "form", body: ""},
		{name: "valid xml", encoding: "xml", body: "<a><b>1</b></a>"},
		{name: "invalid xml", encoding: "xml", body: "<a><b></a>", want: []string{"Body:xml"}},
		{name: "empty xml body", encoding: "xml", body: ""},
		{name: "text is never checked", encoding: "text", body: `{"a": `},
		{name: "unknown encoding is reported by its tag only", encoding: "yaml", body: `{"a": `, want: []string{"Encoding:oneof"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, reportedHTTPErrors(t, config(tt.encoding, tt.body)))
		})
	}
}

func TestHTTPConfigStructLevelValidation_Auth(t *testing.T) {
	config := func(auth string) string {
		return `{"url": "http://example.com", "method": "GET", "encoding": "json", "accepted_statuscodes": ["2XX"], ` + auth + `}`
	}

	tests := []struct {
		name string
		auth string
		want []string
	}{
		{name: "none", auth: `"authMethod": "none"`},
		{name: "unknown method is reported by its tag only", auth: `"authMethod": "digest"`, want: []string{"AuthMethod:oneof"}},
		{name: "basic complete", auth: `"authMethod": "basic", "basic_auth_user": "u", "basic_auth_pass": "p"`},
		{name: "basic without credentials", auth: `"authMethod": "basic"`, want: []string{"BasicAuthUser:required_with_auth_basic", "BasicAuthPass:required_with_auth_basic"}},
		{name: "basic without user", auth: `"authMethod": "basic", "basic_auth_pass": "p"`, want: []string{"BasicAuthUser:required_with_auth_basic"}},
		{name: "basic without password", auth: `"authMethod": "basic", "basic_auth_user": "u"`, want: []string{"BasicAuthPass:required_with_auth_basic"}},
		{name: "ntlm complete", auth: `"authMethod": "ntlm", "basic_auth_user": "u", "basic_auth_pass": "p", "authDomain": "d", "authWorkstation": "w"`},
		{
			name: "ntlm without fields",
			auth: `"authMethod": "ntlm"`,
			want: []string{"BasicAuthUser:required_with_auth_ntlm", "BasicAuthPass:required_with_auth_ntlm", "AuthDomain:required_with_auth_ntlm", "AuthWorkstation:required_with_auth_ntlm"},
		},
		{name: "ntlm without domain", auth: `"authMethod": "ntlm", "basic_auth_user": "u", "basic_auth_pass": "p", "authWorkstation": "w"`, want: []string{"AuthDomain:required_with_auth_ntlm"}},
		{name: "ntlm without workstation", auth: `"authMethod": "ntlm", "basic_auth_user": "u", "basic_auth_pass": "p", "authDomain": "d"`, want: []string{"AuthWorkstation:required_with_auth_ntlm"}},
		{
			name: "oauth2 complete",
			auth: `"authMethod": "oauth2-cc", "oauth_auth_method": "client_secret_post", "oauth_token_url": "https://auth.example.com/token", "oauth_client_id": "id", "oauth_client_secret": "secret"`,
		},
		{
			name: "oauth2 without fields",
			auth: `"authMethod": "oauth2-cc"`,
			want: []string{"OauthAuthMethod:oneof=client_secret_basic client_secret_post", "OauthTokenUrl:required_with_auth_oauth2cc", "OauthClientId:required_with_auth_oauth2cc", "OauthClientSecret:required_with_auth_oauth2cc"},
		},
		{
			name: "oauth2 with an unknown credentials placement and a bad token url",
			auth: `"authMethod": "oauth2-cc", "oauth_auth_method": "header", "oauth_token_url": "not a url", "oauth_client_id": "id", "oauth_client_secret": "secret"`,
			want: []string{"OauthAuthMethod:oneof=client_secret_basic client_secret_post", "OauthTokenUrl:url"},
		},
		{
			name: "oauth2 with the basic credentials placement",
			auth: `"authMethod": "oauth2-cc", "oauth_auth_method": "client_secret_basic", "oauth_token_url": "/token", "oauth_client_id": "id", "oauth_client_secret": "secret"`,
		},
		{name: "mtls complete", auth: `"authMethod": "mtls", "tlsCert": "c", "tlsKey": "k"`},
		{name: "mtls without fields", auth: `"authMethod": "mtls"`, want: []string{"TlsCert:required_with_auth_mtls", "TlsKey:required_with_auth_mtls"}},
		{name: "mtls without key", auth: `"authMethod": "mtls", "tlsCert": "c"`, want: []string{"TlsKey:required_with_auth_mtls"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, reportedHTTPErrors(t, config(tt.auth)))
		})
	}
}

func TestHTTPConfigStructLevelValidation_BodyAndAuthTogether(t *testing.T) {
	config := `{"url": "http://example.com", "method": "POST", "encoding": "json", "body": "{", "accepted_statuscodes": ["2XX"], "authMethod": "mtls"}`

	assert.Equal(t, []string{"Body:json", "TlsCert:required_with_auth_mtls", "TlsKey:required_with_auth_mtls"}, reportedHTTPErrors(t, config))
}
