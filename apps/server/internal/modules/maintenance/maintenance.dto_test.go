package maintenance

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPartialUpdateDtoToModelPreservesPresence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		omitted    bool
	}{
		{"omitted", `{}`, true},
		{"explicit zero values", `{"title":"","active":false,"weekdays":[],"days_of_month":[],"interval_day":0,"monitor_ids":[]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dto PartialUpdateDto
			require.NoError(t, json.Unmarshal([]byte(tc.body), &dto))
			model := dto.toModel()
			if tc.omitted {
				assert.Nil(t, model.Title)
				assert.Nil(t, model.Active)
				assert.Nil(t, model.Weekdays)
				assert.Nil(t, model.DaysOfMonth)
				assert.Nil(t, model.IntervalDay)
				assert.Nil(t, dto.MonitorIds)
			} else {
				require.NotNil(t, model.Title)
				require.NotNil(t, model.Active)
				require.NotNil(t, model.IntervalDay)
				assert.Empty(t, *model.Title)
				assert.False(t, *model.Active)
				assert.Zero(t, *model.IntervalDay)
				assert.NotNil(t, model.Weekdays)
				assert.Empty(t, model.Weekdays)
				assert.NotNil(t, model.DaysOfMonth)
				assert.Empty(t, model.DaysOfMonth)
				assert.NotNil(t, dto.MonitorIds)
				assert.Empty(t, dto.MonitorIds)
			}
		})
	}
}

func TestCreateUpdateDtoToModelPreservesSchedule(t *testing.T) {
	var dto CreateUpdateDto
	body := `{"title":"Maintenance","description":"Description","active":true,"strategy":"recurring-weekday","start_date_time":"2026-01-01T00:00","end_date_time":"2026-12-31T00:00","start_time":"09:00","end_time":"10:30","weekdays":[1,3],"days_of_month":[5,10],"interval_day":2,"cron":"0 9 * * 1,3","timezone":"UTC","duration":90,"monitor_ids":["monitor1"]}`
	require.NoError(t, json.Unmarshal([]byte(body), &dto))
	model := dto.toModel()
	encoded, err := json.Marshal(model)
	require.NoError(t, err)
	var actual map[string]interface{}
	var expected map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &actual))
	require.NoError(t, json.Unmarshal([]byte(body), &expected))
	delete(expected, "monitor_ids")
	delete(actual, "id")
	delete(actual, "created_at")
	delete(actual, "updated_at")
	assert.Equal(t, expected, actual)
	assert.Equal(t, []string{"monitor1"}, dto.MonitorIds)
}

func TestPartialUpdateDtoToModelPreservesSchedule(t *testing.T) {
	var dto PartialUpdateDto
	body := `{"title":"Maintenance","description":"Description","active":false,"strategy":"recurring-weekday","start_date_time":"2026-01-01T00:00","end_date_time":"2026-12-31T00:00","start_time":"09:00","end_time":"10:30","weekdays":[1,3],"days_of_month":[5,10],"interval_day":2,"cron":"0 9 * * 1,3","timezone":"UTC","duration":90}`
	require.NoError(t, json.Unmarshal([]byte(body), &dto))
	var full CreateUpdateDto
	require.NoError(t, json.Unmarshal([]byte(body), &full))
	expected := full.toModel()
	actual := dto.toModel()
	assert.Equal(t, &expected.Title, actual.Title)
	assert.Equal(t, &expected.Description, actual.Description)
	assert.Equal(t, &expected.Active, actual.Active)
	assert.Equal(t, &expected.Strategy, actual.Strategy)
	assert.Equal(t, expected.StartDateTime, actual.StartDateTime)
	assert.Equal(t, expected.EndDateTime, actual.EndDateTime)
	assert.Equal(t, expected.StartTime, actual.StartTime)
	assert.Equal(t, expected.EndTime, actual.EndTime)
	assert.Equal(t, expected.Weekdays, actual.Weekdays)
	assert.Equal(t, expected.DaysOfMonth, actual.DaysOfMonth)
	assert.Equal(t, expected.IntervalDay, actual.IntervalDay)
	assert.Equal(t, expected.Cron, actual.Cron)
	assert.Equal(t, expected.Timezone, actual.Timezone)
	assert.Equal(t, expected.Duration, actual.Duration)
}

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
