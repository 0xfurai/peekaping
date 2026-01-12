package auth

import (
	"context"
	"errors"
	"time"
	"vigi/internal/config"

	"github.com/pquerna/otp/totp"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type Service interface {
	Register(ctx context.Context, dto RegisterDto) (*LoginResponse, error)
	Login(ctx context.Context, dto LoginDto) (*LoginResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error)
	UpdatePassword(ctx context.Context, userId string, dto UpdatePasswordDto) error

	// 2FA methods
	SetupTwoFA(ctx context.Context, userId, password string) (secret string, provisioningURI string, err error)
	VerifyTwoFA(ctx context.Context, userId, code string) (bool, error)
	DisableTwoFA(ctx context.Context, userId, password string) error
}

type ServiceImpl struct {
	repo       Repository
	tokenMaker *TokenMaker
	logger     *zap.SugaredLogger
	cfg        *config.Config
}

func NewService(
	repo Repository,
	tokenMaker *TokenMaker,
	logger *zap.SugaredLogger,
	cfg *config.Config,
) Service {
	return &ServiceImpl{
		repo:       repo,
		tokenMaker: tokenMaker,
		logger:     logger.Named("[auth-service]"),
		cfg:        cfg,
	}
}

func (s *ServiceImpl) Register(ctx context.Context, dto RegisterDto) (*LoginResponse, error) {
	if s.cfg.EnableSingleAdmin {
		count, err := s.repo.FindAllCount(ctx)
		if err != nil {
			return nil, err
		}

		if count > 0 {
			return nil, errors.New("admin already exists")
		}
	}
	// Check if admin with this email already exists
	existingAdmin, err := s.repo.FindByEmail(ctx, dto.Email)
	if err == nil && existingAdmin != nil {
		return nil, errors.New("admin with this email already exists")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(dto.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Create new admin
	user := &Model{
		Email:     dto.Email,
		Password:  string(hashedPassword),
		Active:    true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Save to database
	user, err = s.repo.Create(ctx, user)
	if err != nil {
		return nil, err
	}

	// Generate access token
	accessToken, err := s.tokenMaker.CreateAccessToken(ctx, user)
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshToken, err := s.tokenMaker.CreateRefreshToken(ctx, user)
	if err != nil {
		return nil, err
	}

	return &LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
	}, nil
}

func (s *ServiceImpl) Login(ctx context.Context, dto LoginDto) (*LoginResponse, error) {
	// Find admin by email
	user, err := s.repo.FindByEmail(ctx, dto.Email)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}
	if user == nil {
		return nil, errors.New("invalid credentials")
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(dto.Password))
	if err != nil {
		return nil, errors.New("invalid credentials")
	}

	// Enforce 2FA if enabled
	if user.TwoFASecret != "" && user.TwoFAStatus {
		if dto.Token == "" {
			return nil, errors.New("2FA token required")
		}
		if !totp.Validate(dto.Token, user.TwoFASecret) {
			return nil, errors.New("invalid 2FA token")
		}
	}

	// Generate access token
	accessToken, err := s.tokenMaker.CreateAccessToken(ctx, user)
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshToken, err := s.tokenMaker.CreateRefreshToken(ctx, user)
	if err != nil {
		return nil, err
	}

	return &LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
	}, nil
}

func (s *ServiceImpl) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	// Verify refresh token
	claims, err := s.tokenMaker.VerifyToken(ctx, refreshToken, "refresh")
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	// Check if it's a refresh token
	if claims.Type != "refresh" {
		return nil, errors.New("invalid token type")
	}

	// Find admin by ID
	user, err := s.repo.FindByID(ctx, claims.UserID)
	if err != nil || user == nil {
		return nil, errors.New("user not found")
	}

	// Generate new access token
	accessToken, err := s.tokenMaker.CreateAccessToken(ctx, user)
	if err != nil {
		return nil, err
	}

	// Generate new refresh token
	newRefreshToken, err := s.tokenMaker.CreateRefreshToken(ctx, user)
	if err != nil {
		return nil, err
	}

	return &LoginResponse{
		User:         user,
		RefreshToken: newRefreshToken,
		AccessToken:  accessToken,
	}, nil
}

func (s *ServiceImpl) UpdatePassword(ctx context.Context, userId string, dto UpdatePasswordDto) error {
	// Find user by ID
	user, err := s.repo.FindByID(ctx, userId)
	if err != nil || user == nil {
		return errors.New("user not found")
	}

	// Verify current password
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(dto.CurrentPassword))
	if err != nil {
		return errors.New("current password is incorrect")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(dto.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("failed to hash new password")
	}

	password := string(hashedPassword)
	updateModel := &UpdateModel{
		Password: &password,
	}

	// Update user in DB
	err = s.repo.Update(ctx, userId, updateModel)
	if err != nil {
		return errors.New("failed to update password")
	}

	return nil
}

func (s *ServiceImpl) SetupTwoFA(ctx context.Context, userId, password string) (string, string, error) {
	user, err := s.repo.FindByID(ctx, userId)
	if err != nil || user == nil {
		return "", "", errors.New("user not found")
	}

	// Require password verification
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return "", "", errors.New("invalid password")
	}
	var secretStr, provisioningURI string
	if user.TwoFASecret == "" {
		// Generate new secret and provisioning URI
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "vigi",
			AccountName: user.Email,
		})
		if err != nil {
			return "", "", err
		}
		secretStr = key.Secret()
		provisioningURI = key.URL()
		updateModel := &UpdateModel{
			TwoFASecret: &secretStr,
		}
		err = s.repo.Update(ctx, userId, updateModel)
		if err != nil {
			return "", "", err
		}
	} else {
		// If already set, just return existing
		secretStr = user.TwoFASecret
		// Recreate the provisioning URI
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "vigi",
			AccountName: user.Email,
			Secret:      []byte(user.TwoFASecret),
		})
		if err != nil {
			return "", "", err
		}
		provisioningURI = key.URL()
	}
	return secretStr, provisioningURI, nil
}

func (s *ServiceImpl) VerifyTwoFA(ctx context.Context, userId, code string) (bool, error) {
	user, err := s.repo.FindByID(ctx, userId)
	if err != nil || user == nil {
		return false, errors.New("user not found")
	}
	if user.TwoFASecret == "" {
		return false, errors.New("2FA not setup")
	}
	valid := totp.Validate(code, user.TwoFASecret)
	if valid {
		status := true
		updateModel := &UpdateModel{
			TwoFAStatus: &status,
		}
		s.repo.Update(ctx, userId, updateModel)
	}
	return valid, nil
}

func (s *ServiceImpl) DisableTwoFA(ctx context.Context, userId, password string) error {
	user, err := s.repo.FindByID(ctx, userId)
	if err != nil || user == nil {
		return errors.New("user not found")
	}
	// Require password verification
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return errors.New("invalid password")
	}

	// Prepare values for pointers
	secret := ""
	status := false

	updateModel := &UpdateModel{
		TwoFASecret: &secret,
		TwoFAStatus: &status,
	}

	err = s.repo.Update(ctx, userId, updateModel)
	if err != nil {
		return errors.New("failed to disable 2FA")
	}

	return nil
}
