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
