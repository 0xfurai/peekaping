package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDisabledMonitorTypes(t *testing.T) {
	tests := []struct {
		name string
		list string
		want []string
	}{
		{name: "unset", list: "", want: nil},
		{name: "one type", list: "docker", want: []string{"docker"}},
		{name: "two types", list: "docker,snmp", want: []string{"docker", "snmp"}},
		{name: "spaces are trimmed", list: " docker , snmp ", want: []string{"docker", "snmp"}},
		{name: "empty entries are skipped", list: "docker,,snmp,", want: []string{"docker", "snmp"}},
		{name: "only separators", list: " , ,", want: nil},
		{name: "case is folded", list: "Docker,SNMP", want: []string{"docker", "snmp"}},
		{name: "a type listed twice is kept once", list: "docker,snmp,Docker", want: []string{"docker", "snmp"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseDisabledMonitorTypes(tc.list))
		})
	}
}

func TestValidateDisabledMonitorTypes(t *testing.T) {
	known := []string{"dns", "docker", "http", "push"}
	tests := []struct {
		name                string
		list                string
		wantErrorContaining string
	}{
		{name: "unset", list: ""},
		{name: "a known type", list: "docker"},
		{name: "known types with spaces", list: " docker , push "},
		{name: "an unknown type", list: "dockr", wantErrorContaining: `unknown monitor type "dockr": the known types are dns, docker, http, push`},
		{name: "an unknown type among known ones", list: "docker,nope", wantErrorContaining: `unknown monitor type "nope"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDisabledMonitorTypes(tc.list, known)
			if tc.wantErrorContaining == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, "DISABLED_MONITOR_TYPES")
			assert.ErrorContains(t, err, tc.wantErrorContaining)
		})
	}
}

func TestLoadConfig_DisabledMonitorTypesAreEmptyByDefault(t *testing.T) {
	cfg, err := loadTestConfig(t, nil)

	require.NoError(t, err)
	assert.Empty(t, cfg.DisabledMonitorTypeList())
}

func TestLoadConfig_DisabledMonitorTypesAreSetByTheEnvironment(t *testing.T) {
	cfg, err := loadTestConfig(t, map[string]string{"DISABLED_MONITOR_TYPES": "docker, snmp"})

	require.NoError(t, err)
	assert.Equal(t, []string{"docker", "snmp"}, cfg.DisabledMonitorTypeList())
}
