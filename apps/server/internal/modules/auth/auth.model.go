package auth

import (
	"time"
)

type Model struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Password       string `json:"-"`
	Active         bool   `json:"active"`
	TwoFASecret    string `json:"-"`
	TwoFAStatus    bool   `json:"twofa_status"`
	TwoFALastToken string `json:"-"`
	// InstanceAdmin is whether the user manages what belongs to the install and not to a workspace.
	// It is not in the API yet.
	InstanceAdmin bool `json:"-"`
	// EmailVerifiedAt is when the user followed the link in the verification email, or when an
	// upgrade counted the account as verified. Nil means it was not. It is not in the API apart
	// from GET /me.
	EmailVerifiedAt *time.Time `json:"-"`
	// TokensValidAfter ends the sessions issued before it: an access or refresh token issued
	// earlier is refused. A password reset sets it. Nil means no session was ended.
	TokensValidAfter *time.Time `json:"-"`
	// LastLoginAt is when the user last signed in, by a login or by the signup that signed them in.
	// Nil means they have not since it was recorded. It is shown to instance admins only.
	LastLoginAt *time.Time `json:"-"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type UpdateModel struct {
	Email          *string `json:"email"`
	Password       *string `json:"password"`
	Active         *bool   `json:"active"`
	TwoFASecret    *string `json:"twofa_secret"`
	TwoFAStatus    *bool   `json:"twofa_status"`
	TwoFALastToken *string `json:"twofa_last_token"`
}
