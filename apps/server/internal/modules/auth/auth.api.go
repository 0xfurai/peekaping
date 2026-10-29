package auth

import (
	"peekaping/internal/httpx"
)

type RegisterParams struct {
}
type RegisterInput = httpx.Input[RegisterDto, RegisterParams]

type LoginParams struct {
}
type LoginInput = httpx.Input[LoginDto, LoginParams]

type RefreshTokenParams struct {
}
type RefreshTokenInput = httpx.Input[RefreshTokenDto, RefreshTokenParams]

type UpdatePasswordParams struct {
}
type UpdatePasswordInput = httpx.Input[UpdatePasswordDto, UpdatePasswordParams]

type MeParams struct {
}
type MeInput = httpx.Input[httpx.NoBody, MeParams]

type SetupTwoFAParams struct {
}
type SetupTwoFAInput = httpx.Input[TwoFASetupRequestDto, SetupTwoFAParams]

type VerifyTwoFAParams struct {
}
type VerifyTwoFAInput = httpx.Input[TwoFAVerifyRequestDto, VerifyTwoFAParams]

type DisableTwoFAParams struct {
}
type DisableTwoFAInput = httpx.Input[TwoFADisableRequestDto, DisableTwoFAParams]

type AuthConfigParams struct {
}
type AuthConfigInput = httpx.Input[httpx.NoBody, AuthConfigParams]

type ForgotPasswordParams struct {
}
type ForgotPasswordInput = httpx.Input[ForgotPasswordDto, ForgotPasswordParams]

type ResetPasswordParams struct {
}
type ResetPasswordInput = httpx.Input[ResetPasswordDto, ResetPasswordParams]

type VerifyEmailParams struct {
}
type VerifyEmailInput = httpx.Input[VerifyEmailDto, VerifyEmailParams]

type ResendVerificationParams struct {
}
type ResendVerificationInput = httpx.Input[httpx.NoBody, ResendVerificationParams]
