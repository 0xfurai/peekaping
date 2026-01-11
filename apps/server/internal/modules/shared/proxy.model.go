package shared

import "time"

type Proxy struct {
	ID        string    `json:"id" bson:"_id"`
	OrgID     string    `json:"org_id" bson:"org_id"`
	Protocol  string    `json:"protocol"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Auth      bool      `json:"auth"`
	Username  string    `json:"username,omitempty"`
	Password  string    `json:"password,omitempty"`
	CreatedAt time.Time `json:"createdDate" bson:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" bson:"updated_at"`
}

type UpdateProxy struct {
	Protocol *string `json:"protocol,omitempty"`
	Host     *string `json:"host,omitempty"`
	Port     *int    `json:"port,omitempty"`
	Auth     *bool   `json:"auth,omitempty"`
	Username *string `json:"username,omitempty"`
	Password *string `json:"password,omitempty"`
}
