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

// TestDto is a request to send a test notification. It carries a channel as it is being edited.
// ID is the saved channel it comes from, if there is one: secrets the config leaves as the
// placeholder are the ones that channel has stored.
type TestDto struct {
	CreateUpdateDto
	ID string `json:"id,omitempty"`
}

// UnmarshalJSON reads the id as well as the channel. Without it TestDto would take on the
// UnmarshalJSON of CreateUpdateDto, which knows nothing of the id.
func (dto *TestDto) UnmarshalJSON(data []byte) error {
	*dto = TestDto{}
	if err := dto.CreateUpdateDto.UnmarshalJSON(data); err != nil {
		return err
	}
	var withID struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &withID); err != nil {
		return err
	}
	dto.ID = withID.ID
	return nil
}

type PartialUpdateDto struct {
	Name      *string `json:"name,omitempty"`
	Type      *string `json:"type,omitempty"`
	Active    *bool   `json:"active,omitempty"`
	IsDefault *bool   `json:"is_default,omitempty"`
	Config    *string `json:"config,omitempty"`
}
