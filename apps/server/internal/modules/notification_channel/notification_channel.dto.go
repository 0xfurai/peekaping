package notification_channel

type CreateUpdateDto struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Active    bool   `json:"active"`
	IsDefault bool   `json:"is_default"`
	Config    string `json:"config"`
}

type PartialUpdateDto struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Active    bool   `json:"active"`
	IsDefault bool   `json:"is_default"`
	Config    string `json:"config"`
}
