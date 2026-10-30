package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/modules/workspace"

	"github.com/pquerna/otp/totp"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ErrAccountDisabled is for a sign-in or a refresh by a user an instance admin disabled. It answers
// 401. The sign-in says so only to someone who knew the password.
var ErrAccountDisabled = errors.New("this account is disabled")

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

// Invites is what signup through an invite needs of the workspace module. workspace.Service is one.
type Invites interface {
	CheckInvite(ctx context.Context, token, email string) error
	AcceptInvite(ctx context.Context, token, userID string) (*workspace.Membership, error)
}

// Verifier is what signup needs of the account emails: whether the install sends them, and to
// send the verification link of a new account. *AccountService is one. A nil Verifier sends nothing.
type Verifier interface {
	Enabled() bool
	SendVerification(ctx context.Context, user *Model) error
}

type ServiceImpl struct {
	repo       Repository
	transactor dbtx.Transactor
	workspaces workspace.Repository
	invites    Invites
	tokenMaker *TokenMaker
	verifier   Verifier
	logger     *zap.SugaredLogger
	// openSignup is whether signup is open now: anyone may register, and gets a workspace of their
	// own. Nil is an install whose signup is by invite.
	openSignup func(ctx context.Context) bool
	// disabledMonitorTypes are the monitor types the install turns off (DISABLED_MONITOR_TYPES), as
	// /me lists them
	disabledMonitorTypes []string
	// minCheckInterval is MIN_CHECK_INTERVAL in seconds, as /me states it
	minCheckInterval int
	// supportContact is SUPPORT_CONTACT, as /me states it: empty when unset
	supportContact string
}

// ServiceOption changes how the service behaves
type ServiceOption func(*ServiceImpl)

// WithOpenSignup lets anyone register, as SIGNUP_MODE=open does. Without it only the install's
// first user may, apart from those who register through an invite.
func WithOpenSignup(open bool) ServiceOption {
	return func(s *ServiceImpl) { s.openSignup = func(context.Context) bool { return open } }
}

// WithSignupMode reads whether signup is open from the install settings at each registration, so
// that a mode an instance admin saves takes effect without a restart
func WithSignupMode(settings *installsettings.Resolver) ServiceOption {
	return func(s *ServiceImpl) { s.openSignup = settings.SignupOpen }
}

// WithDisabledMonitorTypes sets the monitor types /me tells the app to hide, as
// DISABLED_MONITOR_TYPES does. Without it none is.
func WithDisabledMonitorTypes(types []string) ServiceOption {
	return func(s *ServiceImpl) { s.disabledMonitorTypes = types }
}

// WithMinCheckInterval sets the smallest check interval, in seconds, that /me tells the app to offer,
// as MIN_CHECK_INTERVAL does. Without it /me says 20, the smallest any install allows.
func WithMinCheckInterval(seconds int) ServiceOption {
	return func(s *ServiceImpl) { s.minCheckInterval = seconds }
}

// WithSupportContact sets the contact /me gives a suspended workspace's members, as SUPPORT_CONTACT
// does. Without it /me says null.
func WithSupportContact(contact string) ServiceOption {
	return func(s *ServiceImpl) { s.supportContact = strings.TrimSpace(contact) }
}

func NewService(
	repo Repository,
	transactor dbtx.Transactor,
	workspaces workspace.Repository,
	invites Invites,
	tokenMaker *TokenMaker,
	verifier Verifier,
	logger *zap.SugaredLogger,
	opts ...ServiceOption,
) Service {
	s := &ServiceImpl{
		repo:       repo,
		transactor: transactor,
		workspaces: workspaces,
		invites:    invites,
		tokenMaker: tokenMaker,
		verifier:   verifier,
		logger:     logger.Named("[auth-service]"),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *ServiceImpl) Register(ctx context.Context, dto RegisterDto) (*LoginResponse, error) {
	if dto.InviteToken != "" {
		return s.registerThroughInvite(ctx, dto)
	}
	if s.openSignup != nil && s.openSignup(ctx) {
		return s.registerOpen(ctx, dto)
	}
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
		return s.joinFirstWorkspace(ctx, user.ID, false)
	})
	if err != nil {
		return nil, err
	}
	s.askToVerify(ctx, user)
	s.recordLogin(ctx, user)

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

// registerOpen creates the account of anyone who signs up when the install is open, with a workspace
// of their own that they own. The user and the workspace are made in one transaction, so a user is
// never left without the place they were promised. Only the install's first user is an instance
// admin. The workspace is suspended until the user's email address is verified, and the verification
// link is sent once the signup has committed.
func (s *ServiceImpl) registerOpen(ctx context.Context, dto RegisterDto) (*LoginResponse, error) {
	// Accounts keep the case their owner typed: the same person in another case is the same address
	if existing, err := s.repo.FindByEmailIgnoringCase(ctx, dto.Email); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrEmailTaken
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(dto.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	user := &Model{Email: strings.TrimSpace(dto.Email), Password: string(hashedPassword), Active: true, CreatedAt: now, UpdatedAt: now}
	// Counted before the transaction starts: the count reads outside it, and a database with one
	// connection (SQLite) is held by the transaction
	count, err := s.repo.FindAllCount(ctx)
	if err != nil {
		return nil, err
	}
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		created, first, err := s.createOpenUser(ctx, user, count == 0)
		if err != nil {
			return err
		}
		user = created
		if first {
			// An install that has records and no user has a workspace already, the one its upgrade
			// made: the user joins that, and it runs as it did. Without one, theirs is suspended.
			return s.joinFirstWorkspace(ctx, user.ID, user.EmailVerifiedAt == nil)
		}
		var made *workspace.Workspace
		if user.EmailVerifiedAt == nil {
			made, err = s.workspaces.CreateSuspended(ctx, workspace.FirstWorkspaceName, workspace.SuspendedUnverified)
		} else {
			made, err = s.workspaces.Create(ctx, workspace.FirstWorkspaceName)
		}
		if err != nil {
			return err
		}
		return s.workspaces.AddMember(ctx, made.ID, user.ID, workspace.RoleOwner)
	})
	if err != nil {
		return nil, err
	}
	s.askToVerify(ctx, user)
	return s.signedIn(ctx, user)
}

// createOpenUser inserts the user, as the install's first (an instance admin) when it has none, and
// reports whether it did. Of any number of concurrent signups on a new install exactly one is the
// first (CreateFirst), and the others are strangers.
func (s *ServiceImpl) createOpenUser(ctx context.Context, user *Model, installIsEmpty bool) (created *Model, first bool, err error) {
	if installIsEmpty {
		created, err = s.repo.CreateFirst(ctx, user)
		if err == nil {
			return created, true, nil
		}
		if !errors.Is(err, ErrAdminExists) {
			return nil, false, err
		}
	}
	created, err = s.repo.Create(ctx, user)
	if err != nil {
		if dbtx.IsUniqueViolation(err) {
			return nil, false, ErrEmailTaken
		}
		return nil, false, err
	}
	return created, false, nil
}

// askToVerify emails the new user a verification link, when the install has a sender. It runs after
// the signup committed and never fails it: the user can ask for the link again.
func (s *ServiceImpl) askToVerify(ctx context.Context, user *Model) {
	if s.verifier == nil {
		return
	}
	if err := s.verifier.SendVerification(ctx, user); err != nil {
		s.logger.Errorw("Failed to send the verification email of a new account", "userId", user.ID, "error", err)
	}
}

// registerThroughInvite creates the account of someone an invite names. The user is no instance
// admin and gets no workspace of their own: they join the inviting workspace with the invite's
// role, in the transaction that creates them, so a user is never left without the place the
// invite gave. The first-user rule does not apply, and neither does the signup mode: the invite
// is the permission.
func (s *ServiceImpl) registerThroughInvite(ctx context.Context, dto RegisterDto) (*LoginResponse, error) {
	// Ask before spending time on a password: a link that can't be used says why
	if err := s.invites.CheckInvite(ctx, dto.InviteToken, dto.Email); err != nil {
		return nil, err
	}
	// Accounts keep the case their owner typed, and the invite is lower-cased: the same person may
	// have an account in another case, and a second one would be a stranger to their workspaces
	if existing, err := s.repo.FindByEmailIgnoringCase(ctx, dto.Email); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrEmailTaken
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(dto.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	user := &Model{Email: strings.TrimSpace(dto.Email), Password: string(hashedPassword), Active: true, CreatedAt: now, UpdatedAt: now}
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		created, err := s.repo.Create(ctx, user)
		if err != nil {
			if dbtx.IsUniqueViolation(err) {
				return ErrEmailTaken
			}
			return err
		}
		user = created
		// The invite is checked again here, and spent, in the same transaction as the user
		_, err = s.invites.AcceptInvite(ctx, dto.InviteToken, user.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.askToVerify(ctx, user)
	return s.signedIn(ctx, user)
}

// signedIn issues the tokens of a user who just registered
func (s *ServiceImpl) signedIn(ctx context.Context, user *Model) (*LoginResponse, error) {
	s.recordLogin(ctx, user)
	accessToken, err := s.tokenMaker.CreateAccessToken(ctx, user)
	if err != nil {
		return nil, err
	}
	refreshToken, err := s.tokenMaker.CreateRefreshToken(ctx, user)
	if err != nil {
		return nil, err
	}
	return &LoginResponse{AccessToken: accessToken, RefreshToken: refreshToken, User: user}, nil
}

// recordLogin stores when the user signed in. A failure is logged and never fails the sign-in.
func (s *ServiceImpl) recordLogin(ctx context.Context, user *Model) {
	if err := s.repo.RecordLogin(ctx, user.ID, time.Now().UTC()); err != nil {
		s.logger.Errorw("Failed to record when a user signed in", "userId", user.ID, "error", err)
	}
}

// joinFirstWorkspace makes the user the owner of the install's oldest workspace, or of a new one
// when it has none, which is suspended when suspendNew says so
func (s *ServiceImpl) joinFirstWorkspace(ctx context.Context, userID string, suspendNew bool) error {
	target, err := s.workspaces.FindOldest(ctx)
	if err != nil {
		return err
	}
	if target == nil {
		if suspendNew {
			target, err = s.workspaces.CreateSuspended(ctx, workspace.FirstWorkspaceName, workspace.SuspendedUnverified)
		} else {
			target, err = s.workspaces.Create(ctx, workspace.FirstWorkspaceName)
		}
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
		User:       &MeUser{ID: user.ID, Email: user.Email, InstanceAdmin: user.InstanceAdmin, EmailVerified: user.EmailVerifiedAt != nil},
		Workspaces: make([]MeWorkspace, len(memberships)),

		AccountEmails: s.verifier != nil && s.verifier.Enabled(),
		// A list, never null, so that the app can read it without a check
		DisabledMonitorTypes: append([]string{}, s.disabledMonitorTypes...),
		MinCheckInterval:     max(s.minCheckInterval, config.AbsoluteMinCheckInterval),
	}
	for i, m := range memberships {
		me.Workspaces[i] = MeWorkspace{ID: string(m.Workspace.ID), Name: m.Workspace.Name, Role: string(m.Role), Suspended: m.Workspace.Suspended(), DeletionScheduledAt: m.Workspace.DeletionScheduledAt}
		if m.Workspace.Suspended() {
			reason := m.Workspace.ShownReason()
			me.Workspaces[i].SuspendedReason = &reason
		}
	}
	if s.supportContact != "" {
		contact := s.supportContact
		me.SupportContact = &contact
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

	// A disabled account can't sign in. It is said only after the password was right, so that the
	// answer tells nobody which addresses have accounts.
	if !user.Active {
		return nil, ErrAccountDisabled
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

	s.recordLogin(ctx, user)

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

	// A disabled account gets no new session, whatever token it still holds
	if !user.Active {
		return nil, ErrAccountDisabled
	}

	// The new tokens belong to the session the refresh token belongs to, and carry its start: a
	// reset that commits after the user was read here ends that session, whenever these are made.
	// A token from before auth_time was added falls back to when it was issued.
	began := time.Now().UTC()
	if start := claims.SessionStart(); start != nil {
		began = *start
	}

	// Generate new access token
	accessToken, err := s.tokenMaker.CreateAccessTokenForSession(ctx, user, began)
	if err != nil {
		return nil, err
	}

	// Generate new refresh token
	newRefreshToken, err := s.tokenMaker.CreateRefreshTokenForSession(ctx, user, began)
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
