package auth

type RegisterDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,password"`
	// InviteToken is the token of an invite link (PP-154). With a valid one whose email is the
	// body's, the user is created as a member of the inviting workspace, with the invite's role and
	// no workspace of their own, and is no instance admin, whatever the first-user rule says.
	// Without it, only the install's first user can register.
	InviteToken string `json:"inviteToken,omitempty"`
}

type LoginDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
	Token    string `json:"token"`
}

type RefreshTokenDto struct {
	RefreshToken string `json:"refreshToken" validate:"required"`
}

// MeResponse is the signed-in user, the workspaces they are in and the one their requests run in
// without an X-Workspace-ID header
type MeResponse struct {
	User *MeUser `json:"user"`
	// Workspaces are the user's places, the one they joined first at the front
	Workspaces []MeWorkspace `json:"workspaces"`
	// DefaultWorkspaceID is the workspace they joined first, or null for a user in none
	DefaultWorkspaceID *string `json:"defaultWorkspaceId"`
	// AccountEmails is whether the install sends account emails (ACCOUNT_SMTP_HOST is set): when it
	// is, the app shows the verification banner to a user whose address is not verified
	AccountEmails bool `json:"accountEmails"`
}

type MeUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	// InstanceAdmin is whether the user manages what belongs to the install and not to a workspace
	InstanceAdmin bool `json:"instanceAdmin"`
	// EmailVerified is whether the user followed the link in the verification email. Accounts that
	// existed before account emails count as verified.
	EmailVerified bool `json:"emailVerified"`
}

type MeWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role is what the user may do there: owner, admin or member
	Role string `json:"role"`
}

type LoginResponse struct {
	User         *Model `json:"user"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// DTO for updating user password
// Used in password update endpoint
// Both fields are required, new password must pass password validation
type UpdatePasswordDto struct {
	CurrentPassword string `json:"currentPassword" validate:"required"`
	NewPassword     string `json:"newPassword" validate:"required,password"`
}

// DTO for 2FA setup request
// Used to initiate 2FA setup and get QR code/secret
type TwoFASetupRequestDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// DTO for 2FA setup response
// Contains the secret and provisioning URI for QR code
type TwoFASetupResponseDto struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioningUri"`
}

// DTO for 2FA verification request
// Used to verify a TOTP code
type TwoFAVerifyRequestDto struct {
	Email string `json:"email" validate:"required,email"`
	Code  string `json:"code" validate:"required"`
}

// DTO for 2FA verification response
// Indicates if verification was successful
type TwoFAVerifyResponseDto struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DTO for 2FA disable request
// Used to disable 2FA for a user
type TwoFADisableRequestDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// AuthConfigResponse is what the app needs to know about the install before anyone signs in
type AuthConfigResponse struct {
	// AccountEmails is whether the install can send account emails: a password reset link, an
	// email verification link and emailed invites. When it is false the app offers none of them.
	AccountEmails bool `json:"accountEmails"`
}

// ForgotPasswordDto asks for a password reset link to be sent to an address
type ForgotPasswordDto struct {
	Email string `json:"email" validate:"required,email"`
}

// ResetPasswordDto sets a new password with the token of a reset link. The password must pass the
// same rules as at signup.
type ResetPasswordDto struct {
	Token    string `json:"token" validate:"required"`
	Password string `json:"password" validate:"required,password"`
}

// VerifyEmailDto verifies an address with the token of a verification link
type VerifyEmailDto struct {
	Token string `json:"token" validate:"required"`
}
