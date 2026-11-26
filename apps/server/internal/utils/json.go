package utils

import (
	"encoding/json"
	"strings"
)

// HasJSONKey reports whether a JSON object has the key. It matches keys
// case-insensitively, as encoding/json does when it decodes into a struct.
func HasJSONKey(data []byte, key string) (bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false, err
	}
	for k := range fields {
		if strings.EqualFold(k, key) {
			return true, nil
		}
	}
	return false, nil
}
