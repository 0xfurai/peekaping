package notification_channel

import (
	"encoding/json"

	"peekaping/internal/utils"
)

type CreateUpdateDto struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Active    bool   `json:"active"`
	IsDefault bool   `json:"is_default"`
	Config    string `json:"config"`
	activeSet bool
}

// UnmarshalJSON records whether active was present so create requests can apply
// the historical default without changing full-update semantics.
func (dto *CreateUpdateDto) UnmarshalJSON(data []byte) error {
	type plain CreateUpdateDto
	*dto = CreateUpdateDto{}
	if err := json.Unmarshal(data, (*plain)(dto)); err != nil {
		return err
	}
	var err error
	dto.activeSet, err = utils.HasJSONKey(data, "active")
	return err
}

// ApplyCreateDefaults keeps omitted active fields active for create requests.
// Full updates keep their existing zero-value semantics.
func (dto *CreateUpdateDto) ApplyCreateDefaults() {
	if dto == nil {
		return
	}
	if !dto.activeSet {
		dto.Active = true
	}
}

type PartialUpdateDto struct {
	Name      *string `json:"name,omitempty"`
	Type      *string `json:"type,omitempty"`
	Active    *bool   `json:"active,omitempty"`
	IsDefault *bool   `json:"is_default,omitempty"`
	Config    *string `json:"config,omitempty"`
}
