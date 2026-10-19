package auth

type RegisterDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,password"`
}

type LoginDto struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
	Token    string `json:"token"`
}

type RefreshTokenDto struct {
	RefreshToken string `json:"refreshToken" validate:"required"`
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
