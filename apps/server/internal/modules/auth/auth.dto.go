package auth

import "time"

type RegisterDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,password"`
	// InviteToken is the token of an invite link (PP-154). With a valid one whose email is the
	// body's, the user is created as a member of the inviting workspace, with the invite's role and
	// no workspace of their own, and is no instance admin, whatever the first-user rule says.
	// Without it, who may register depends on SIGNUP_MODE: in invite mode only the install's first
	// user, in open mode anyone, who then gets a workspace of their own.
	InviteToken string `json:"inviteToken,omitempty"`
	// TurnstileToken is the answer of the Cloudflare Turnstile widget (PP-155). An open signup
	// without an invite needs one when the install has Turnstile set up; the server checks it with
	// Cloudflare. An invite signup needs none.
	TurnstileToken string `json:"turnstileToken,omitempty"`
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
	// DisabledMonitorTypes are the monitor types the install turns off (DISABLED_MONITOR_TYPES),
	// empty by default: the app hides them from the monitor type list, and shows a monitor of one as
	// paused. The server refuses to create such a monitor or to change a monitor's type to one.
	DisabledMonitorTypes []string `json:"disabledMonitorTypes" nullable:"false"`
	// MinCheckInterval is the smallest check interval and retry interval, in seconds, the install
	// lets a monitor be saved with (MIN_CHECK_INTERVAL, PP-159): 20 by default. The app uses it as the
	// minimum and the default of the monitor form.
	MinCheckInterval int `json:"minCheckInterval" example:"20"`
	// VerifyCustomDomains is whether a custom domain must be verified before it serves its status
	// page (VERIFY_CUSTOM_DOMAINS, PP-165): the app tells a workspace that its domain waits for the
	// DNS records, and when it will be removed.
	VerifyCustomDomains bool `json:"verifyCustomDomains"`
	// CustomDomainCnameTarget is the host a custom domain may be a CNAME to, to be verified:
	// CUSTOM_DOMAIN_CNAME_TARGET, else the host of the public URL. Empty when the install has none (its
	// public URL is localhost or an IP address), and then only the TXT record verifies a domain.
	CustomDomainCNAMETarget string `json:"customDomainCnameTarget" example:"peekaping.example.com"`
	// SupportContact is how a member of a suspended workspace gets in touch with whoever runs the
	// install (SUPPORT_CONTACT, PP-372): free text, an email address or a URL, for the suspension
	// banner. Null when the install sets none.
	SupportContact *string `json:"supportContact" nullable:"true" example:"support@example.com"`
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

// DeleteAccountDto is the request to delete the signed-in user's own account
type DeleteAccountDto struct {
	Password string `json:"password" validate:"required"`
}

type MeWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role is what the user may do there: owner, admin or member
	Role string `json:"role"`
	// Suspended is whether the workspace's monitors are held back: they are kept and shown, but none
	// runs (PP-155)
	Suspended bool `json:"suspended"`
	// SuspendedReason says why a suspended workspace is: "admin" is a workspace an instance admin
	// suspended, which runs once they resume it; "deleting" is a workspace whose owner scheduled its
	// deletion (deletionScheduledAt says when); "unverified" is a new account's workspace, which
	// runs once its owner has verified their email address. "deleting" is told first, then "admin",
	// when more than one holds. Null when the workspace runs.
	SuspendedReason *string `json:"suspendedReason"`
	// DeletionScheduledAt is when the workspace and everything in it will be deleted, because an owner
	// scheduled its deletion (PP-170): the workspace is suspended with the reason "deleting" until
	// then, and an owner can cancel. Null when no deletion is scheduled.
	DeletionScheduledAt *time.Time `json:"deletionScheduledAt" nullable:"true"`
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
	// SignupMode is who may create an account (SIGNUP_MODE): "invite", where the install's first user
	// signs up and everyone after joins through an invite, or "open", where anyone may sign up and
	// gets a workspace of their own
	SignupMode string `json:"signupMode" enum:"invite,open"`
	// SignupOpen is whether the sign-up form should be offered: always in open mode, and in invite
	// mode while the install has no user to sign up first
	SignupOpen bool `json:"signupOpen"`
	// FirstSignup is whether the install has no user yet, in either mode. The next signup is then
	// its first user, who becomes the instance admin and chooses the install's usage statistics and
	// error reports; in open mode the signups after it are not instance admins.
	FirstSignup bool `json:"firstSignup"`
	// TurnstileSiteKey is the public key of the Cloudflare Turnstile widget that open signup shows,
	// or null when the install does not use it
	TurnstileSiteKey *string `json:"turnstileSiteKey"`
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
