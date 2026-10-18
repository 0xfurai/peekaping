package setting

type CreateUpdateDto struct {
	Value string `json:"value"`
	Type  string `json:"type" validate:"required,oneof=string int bool json"`
}
