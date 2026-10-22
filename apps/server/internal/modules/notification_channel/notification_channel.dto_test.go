package notification_channel

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

func TestTestDtoReadsTheChannelAndItsID(t *testing.T) {
	var dto TestDto
	require.NoError(t, json.Unmarshal([]byte(`{"id":"channel-1","name":"Alerts","type":"webhook","active":true,"is_default":true,"config":"{}"}`), &dto))

	assert.Equal(t, TestDto{
		CreateUpdateDto: CreateUpdateDto{Name: "Alerts", Type: "webhook", Active: true, IsDefault: true, Config: "{}", activeSet: true},
		ID:              "channel-1",
	}, dto)
}

func TestTestDtoIDIsOptional(t *testing.T) {
	for _, body := range []string{`{"type":"webhook"}`, `{"type":"webhook","id":null}`, `{"type":"webhook","id":""}`} {
		var dto TestDto
		require.NoError(t, json.Unmarshal([]byte(body), &dto), body)
		assert.Empty(t, dto.ID, body)
		assert.Equal(t, "webhook", dto.Type, body)
	}
}

func TestTestDtoUnmarshalErrors(t *testing.T) {
	for _, body := range []string{`[]`, `{"active":"false"}`, `{"id":7}`, `{"id":["channel-1"]}`} {
		var dto TestDto
		assert.Error(t, json.Unmarshal([]byte(body), &dto), body)
	}
}

func TestCreateUpdateDtoApplyCreateDefaultsNil(t *testing.T) {
	var dto *CreateUpdateDto
	assert.NotPanics(t, dto.ApplyCreateDefaults)
}
