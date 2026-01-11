package proxy

// CreateUpdateDto is used for both create and full update operations.
type CreateUpdateDto struct {
	OrgID    string `json:"org_id,omitempty"`
	Protocol string `json:"protocol" validate:"required,oneof=http https socks socks4 socks5 socks5h"`
	Host     string `json:"host" validate:"required"`
	Port     int    `json:"port" validate:"required,min=1,max=65535"`
	Auth     bool   `json:"auth"`
	Username string `json:"username,omitempty" validate:"required_if=Auth true"`
	Password string `json:"password,omitempty" validate:"required_if=Auth true"`
}

// PartialUpdateDto is used for PATCH/partial update operations.
type PartialUpdateDto struct {
	OrgID    *string `json:"org_id,omitempty"`
	Protocol *string `json:"protocol,omitempty" validate:"omitempty,oneof=http https socks socks4 socks5 socks5h"`
	Host     *string `json:"host,omitempty"`
	Port     *int    `json:"port,omitempty" validate:"omitempty,min=1,max=65535"`
	Auth     *bool   `json:"auth,omitempty"`
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}
