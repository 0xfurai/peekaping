package config

import (
	"fmt"
	"slices"
	"strings"
)

// ParseDisabledMonitorTypes splits a DISABLED_MONITOR_TYPES list into monitor types: trimmed, in
// lower case, each once, in the order they were first listed. An unset or empty list, and empty
// entries between commas, give no types.
func ParseDisabledMonitorTypes(list string) []string {
	var types []string
	for _, entry := range strings.Split(list, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry != "" && !slices.Contains(types, entry) {
			types = append(types, entry)
		}
	}
	return types
}

// DisabledMonitorTypeList is DISABLED_MONITOR_TYPES as a list of types. It is empty by default, and
// ValidateDisabledMonitorTypes has checked the entries at startup.
func (c *Config) DisabledMonitorTypeList() []string {
	return ParseDisabledMonitorTypes(c.DisabledMonitorTypes)
}

// ValidateDisabledMonitorTypes checks that every type in the list is a monitor type the server
// knows. A mistyped type stops startup, with the types it could have meant: ignoring it would leave
// the install running a type its operator believes is off.
func ValidateDisabledMonitorTypes(list string, known []string) error {
	for _, monitorType := range ParseDisabledMonitorTypes(list) {
		if !slices.Contains(known, monitorType) {
			return fmt.Errorf("DISABLED_MONITOR_TYPES has an unknown monitor type %q: the known types are %s", monitorType, strings.Join(known, ", "))
		}
	}
	return nil
}
