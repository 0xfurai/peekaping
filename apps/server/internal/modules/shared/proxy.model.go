package shared

import "time"

type Proxy struct {
	ID        string    `json:"id"`
	Protocol  string    `json:"protocol"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Auth      bool      `json:"auth"`
	Username  string    `json:"username,omitempty"`
	Password  string    `json:"password,omitempty"`
	CreatedAt time.Time `json:"createdDate"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type UpdateProxy struct {
	Protocol *string `json:"protocol,omitempty"`
	Host     *string `json:"host,omitempty"`
	Port     *int    `json:"port,omitempty"`
	Auth     *bool   `json:"auth,omitempty"`
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}
