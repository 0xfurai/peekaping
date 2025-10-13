package auth

import (
	"time"
)

type Model struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Password       string    `json:"-"`
	Active         bool      `json:"active"`
	TwoFASecret    string    `json:"-"`
	TwoFAStatus    bool      `json:"twofa_status"`
	TwoFALastToken string    `json:"-"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type UpdateModel struct {
	Email          *string `json:"email"`
	Password       *string `json:"password"`
	Active         *bool   `json:"active"`
	TwoFASecret    *string `json:"twofa_secret"`
	TwoFAStatus    *bool   `json:"twofa_status"`
	TwoFALastToken *string `json:"twofa_last_token"`
}
