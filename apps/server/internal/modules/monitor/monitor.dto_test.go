package monitor

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateUpdateDtoActiveDefaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"omitted defaults active", `{}`, true},
		{"explicit false stays inactive", `{"active":false}`, false},
		{"explicit true stays active", `{"active":true}`, true},
		{"explicit false in another case stays inactive", `{"Active":false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dto CreateUpdateDto
			require.NoError(t, json.Unmarshal([]byte(tc.body), &dto))
			dto.ApplyCreateDefaults()
			assert.Equal(t, tc.want, dto.Active)
		})
	}
}

func TestCreateUpdateDtoActiveUnmarshalErrors(t *testing.T) {
	for _, body := range []string{`[]`, `{"active":"false"}`} {
		var dto CreateUpdateDto
		assert.Error(t, json.Unmarshal([]byte(body), &dto))
	}
}

func TestCreateUpdateDtoApplyCreateDefaultsNil(t *testing.T) {
	var dto *CreateUpdateDto
	assert.NotPanics(t, dto.ApplyCreateDefaults)
}

func TestCreateUpdateDtoApplyCreateDefaultsPushTokenFromTheRequest(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dto        CreateUpdateDto
		wantToken  string
		wantConfig string
	}{
		{"top level only", CreateUpdateDto{Type: "push", PushToken: "abc"}, "abc", `{"pushToken":"abc"}`},
		{"top level with an empty object", CreateUpdateDto{Type: "push", PushToken: "abc", Config: `{}`}, "abc", `{"pushToken":"abc"}`},
		{"top level with an empty token in config", CreateUpdateDto{Type: "push", PushToken: "abc", Config: `{"pushToken":""}`}, "abc", `{"pushToken":"abc"}`},
		{"config only", CreateUpdateDto{Type: "push", Config: `{"pushToken":"abc"}`}, "abc", `{"pushToken":"abc"}`},
		{"both the same keep the config as sent", CreateUpdateDto{Type: "push", PushToken: "abc", Config: `{ "pushToken": "abc" }`}, "abc", `{ "pushToken": "abc" }`},
		{"both differ, top level wins", CreateUpdateDto{Type: "push", PushToken: "top", Config: `{"pushToken":"inner"}`}, "top", `{"pushToken":"top"}`},
		{"other config keys are kept", CreateUpdateDto{Type: "push", PushToken: "abc", Config: `{"other":{"x":[1,2]},"pushToken":""}`}, "abc", `{"other":{"x":[1,2]},"pushToken":"abc"}`},
		{"a token with quotes and backslashes", CreateUpdateDto{Type: "push", PushToken: `a"b\c`}, `a"b\c`, `{"pushToken":"a\"b\\c"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dto := tc.dto

			dto.ApplyCreateDefaults()

			assert.Equal(t, tc.wantToken, dto.PushToken)
			assert.Equal(t, tc.wantConfig, dto.Config)
		})
	}
}

func TestCreateUpdateDtoApplyCreateDefaultsGeneratesAPushToken(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
	}{
		{"no config", ""},
		{"blank config", "  "},
		{"empty object", `{}`},
		{"empty token", `{"pushToken":""}`},
		{"null config", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dto := CreateUpdateDto{Type: "push", Config: tc.config}

			dto.ApplyCreateDefaults()

			assert.Regexp(t, `^[A-Z2-7]{26}$`, dto.PushToken, "128 bits as base32")
			assert.JSONEq(t, `{"pushToken":"`+dto.PushToken+`"}`, dto.Config)
		})
	}
}

func TestCreateUpdateDtoApplyCreateDefaultsGeneratesAnotherTokenEachTime(t *testing.T) {
	first, second := CreateUpdateDto{Type: "push"}, CreateUpdateDto{Type: "push"}

	first.ApplyCreateDefaults()
	second.ApplyCreateDefaults()

	assert.NotEqual(t, first.PushToken, second.PushToken)
}

func TestCreateUpdateDtoApplyCreateDefaultsLeavesTheRestAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		dto  CreateUpdateDto
	}{
		{"a config that isn't JSON, so the validation rejects it", CreateUpdateDto{Type: "push", PushToken: "abc", Config: `not json`}},
		{"a config that isn't an object", CreateUpdateDto{Type: "push", Config: `[1]`}},
		{"a token in config that isn't text", CreateUpdateDto{Type: "push", Config: `{"pushToken":5}`}},
		{"an http monitor", CreateUpdateDto{Type: "http", Config: `{"url":"https://example.com"}`}},
		{"an http monitor with an empty config", CreateUpdateDto{Type: "http"}},
		{"an http monitor sent a push token", CreateUpdateDto{Type: "http", PushToken: "abc", Config: `{}`}},
		{"no type", CreateUpdateDto{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dto := tc.dto

			dto.ApplyCreateDefaults()

			assert.Equal(t, tc.dto.Config, dto.Config)
			assert.Equal(t, tc.dto.PushToken, dto.PushToken)
		})
	}
}

func TestCreateUpdateDtoApplyUpdateDefaultsSyncsThePushToken(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dto        CreateUpdateDto
		wantToken  string
		wantConfig string
	}{
		{"top level only", CreateUpdateDto{Type: "push", PushToken: "abc", Config: `{}`}, "abc", `{"pushToken":"abc"}`},
		{"config only", CreateUpdateDto{Type: "push", Config: `{"pushToken":"abc"}`}, "abc", `{"pushToken":"abc"}`},
		{"both differ, top level wins", CreateUpdateDto{Type: "push", PushToken: "top", Config: `{"pushToken":"inner"}`}, "top", `{"pushToken":"top"}`},
		{"neither generates none, so the validation rejects it", CreateUpdateDto{Type: "push", Config: `{"pushToken":""}`}, "", `{"pushToken":""}`},
		{"no config generates none", CreateUpdateDto{Type: "push"}, "", ""},
		{"an http monitor is left alone", CreateUpdateDto{Type: "http", PushToken: "abc", Config: `{}`}, "abc", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dto := tc.dto

			dto.ApplyUpdateDefaults()

			assert.Equal(t, tc.wantToken, dto.PushToken)
			assert.Equal(t, tc.wantConfig, dto.Config)
		})
	}
}
