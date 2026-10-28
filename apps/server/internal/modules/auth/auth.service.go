package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/modules/workspace"

	"github.com/pquerna/otp/totp"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type Service interface {
	Register(ctx context.Context, dto RegisterDto) (*LoginResponse, error)
	Login(ctx context.Context, dto LoginDto) (*LoginResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error)
	UpdatePassword(ctx context.Context, userId string, dto UpdatePasswordDto) error
	// Me returns the user with their workspaces, or nil for a user that doesn't exist
	Me(ctx context.Context, userId string) (*MeResponse, error)

	// 2FA methods
	SetupTwoFA(ctx context.Context, userId, password string) (secret string, provisioningURI string, err error)
	VerifyTwoFA(ctx context.Context, userId, code string) (bool, error)
	DisableTwoFA(ctx context.Context, userId, password string) error
}

type ServiceImpl struct {
	repo       Repository
	transactor dbtx.Transactor
	workspaces workspace.Repository
	tokenMaker *TokenMaker
	logger     *zap.SugaredLogger
}

func NewService(
	repo Repository,
	transactor dbtx.Transactor,
	workspaces workspace.Repository,
	tokenMaker *TokenMaker,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repo:       repo,
		transactor: transactor,
		workspaces: workspaces,
		tokenMaker: tokenMaker,
		logger:     logger.Named("[auth-service]"),
	}
}

func (s *ServiceImpl) Register(ctx context.Context, dto RegisterDto) (*LoginResponse, error) {
	count, err := s.repo.FindAllCount(ctx)
	if err != nil {
		return nil, err
	}

	if count > 0 {
		return nil, ErrAdminExists
	}
	// The count found no admin, so one with this email is a concurrent
	// registration that committed since: this one lost the race
	existingAdmin, err := s.repo.FindByEmail(ctx, dto.Email)
	if err == nil && existingAdmin != nil {
		return nil, ErrAdminExists
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

	// Save to database, unless another registration got there first. The user is the install's
	// first, so it is an instance admin and the owner of the first workspace, which is made with
	// the user and not left for later. An install that has records and no user has a workspace
	// already, the one its upgrade made, and the user joins that instead of starting another.
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		created, err := s.repo.CreateFirst(ctx, user)
		if err != nil {
			return err
		}
		user = created
		return s.joinFirstWorkspace(ctx, user.ID)
	})
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

// joinFirstWorkspace makes the user the owner of the install's oldest workspace, or of a new one
// when it has none
func (s *ServiceImpl) joinFirstWorkspace(ctx context.Context, userID string) error {
	target, err := s.workspaces.FindOldest(ctx)
	if err != nil {
		return err
	}
	if target == nil {
		target, err = s.workspaces.Create(ctx, workspace.FirstWorkspaceName)
		if err != nil {
			return err
		}
	}
	return s.workspaces.AddMember(ctx, target.ID, userID, workspace.RoleOwner)
}

func (s *ServiceImpl) Me(ctx context.Context, userId string) (*MeResponse, error) {
	user, err := s.repo.FindByID(ctx, userId)
	if err != nil || user == nil {
		return nil, err
	}
	memberships, err := s.workspaces.ListMemberships(ctx, userId)
	if err != nil {
		return nil, err
	}
	me := &MeResponse{
		User:       &MeUser{ID: user.ID, Email: user.Email, InstanceAdmin: user.InstanceAdmin},
		Workspaces: make([]MeWorkspace, len(memberships)),
	}
	for i, m := range memberships {
		me.Workspaces[i] = MeWorkspace{ID: string(m.Workspace.ID), Name: m.Workspace.Name, Role: string(m.Role)}
	}
	if len(memberships) > 0 {
		defaultID := me.Workspaces[0].ID
		me.DefaultWorkspaceID = &defaultID
	}
	return me, nil
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

	// A refresh token issued before the last password change, or by a release
	// that didn't set the fingerprint, no longer works
	if subtle.ConstantTimeCompare([]byte(claims.PasswordFingerprint), []byte(passwordFingerprint(user.Password))) != 1 {
		return nil, errors.New("invalid refresh token")
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
			Issuer:      "peekaping",
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
		// Recreate the provisioning URI. Generate base32-encodes the raw
		// secret, so decode the stored one first.
		rawSecret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(user.TwoFASecret)
		if err != nil {
			return "", "", err
		}
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "peekaping",
			AccountName: user.Email,
			Secret:      rawSecret,
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
		if err := s.repo.Update(ctx, userId, updateModel); err != nil {
			return false, errors.New("failed to enable 2FA")
		}
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
