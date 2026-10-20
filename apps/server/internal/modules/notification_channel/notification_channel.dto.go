package notification_channel

type CreateUpdateDto struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Active    bool   `json:"active"`
	IsDefault bool   `json:"is_default"`
	Config    string `json:"config"`
	// Secret is write-only. Credentials sent inside config are moved here as well, so an
	// older client keeps working.
	Secret string `json:"secret"`
}

type PartialUpdateDto struct {
	Name      *string `json:"name,omitempty"`
	Type      *string `json:"type,omitempty"`
	Active    *bool   `json:"active,omitempty"`
	IsDefault *bool   `json:"is_default,omitempty"`
	Config    *string `json:"config,omitempty"`
	Secret    *string `json:"secret,omitempty"`
}
