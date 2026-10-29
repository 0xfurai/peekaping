package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const (
	// ResetTokenPrefix and VerifyTokenPrefix start the tokens of the two links, so that a leaked
	// one is recognised for what it is
	ResetTokenPrefix  = "pkr_"
	VerifyTokenPrefix = "pkv_"

	// ResetTokenLifetime and VerifyTokenLifetime are how long a link works. The emails say it.
	ResetTokenLifetime  = time.Hour
	VerifyTokenLifetime = 72 * time.Hour

	// accountTokenBytes is the size of the random part of a token
	accountTokenBytes = 32
	// maxAccountTokenLength bounds what a request may make the server hash
	maxAccountTokenLength = 200
)

var (
	// ErrAccountEmailsOff is for a request that needs a sender when the install has none. It answers 404.
	ErrAccountEmailsOff = errors.New("account emails are not set up on this server")
	// ErrInvalidLink is for a reset or verification link that does not exist, was used or has
	// expired. It does not say which: a visitor learns nothing about tokens they do not hold. It
	// answers 400.
	ErrInvalidLink = errors.New("this link is invalid or has expired")
	// ErrAlreadyVerified is for a request to send a verification link to an address that was verified. It answers 409.
	ErrAlreadyVerified = errors.New("this email address is verified already")
)

// MonitorWaker makes the scheduler and the probes pick up the monitors of a workspace that started to
// run. monitor.Service is one.
type MonitorWaker interface {
	PublishActiveUpdated(ctx context.Context, workspaceID workspace.ID) error
}

// AccountService sends and spends the links of account emails: a password reset, and the
// verification of an email address. Without a sender (ACCOUNT_SMTP_HOST) it does nothing.
type AccountService struct {
	repo       Repository
	accounts   AccountRepository
	transactor dbtx.Transactor
	mailer     *mailer.Mailer
	workspaces workspace.Repository
	monitors   MonitorWaker
	bus        events.EventBus
	settings   *installsettings.Resolver
	logger     *zap.SugaredLogger
	now        func() time.Time
}

func NewAccountService(
	repo Repository,
	accounts AccountRepository,
	transactor dbtx.Transactor,
	mailer *mailer.Mailer,
	workspaces workspace.Repository,
	monitors MonitorWaker,
	bus events.EventBus,
	settings *installsettings.Resolver,
	logger *zap.SugaredLogger,
) *AccountService {
	return &AccountService{
		repo:       repo,
		accounts:   accounts,
		transactor: transactor,
		mailer:     mailer,
		workspaces: workspaces,
		monitors:   monitors,
		bus:        bus,
		settings:   settings,
		logger:     logger.Named("[account-service]"),
		now:        time.Now,
	}
}

// Enabled is whether the install has a sender for account emails
func (s *AccountService) Enabled() bool { return s.mailer.Enabled() }

// RequestPasswordReset emails a reset link to the account with the address, if there is one. It
// returns at once and does the work in the background, for every address alike, so that how long
// it takes (a lookup, a token, an SMTP conversation) does not tell the caller whether the
// address has an account. A failure is logged.
func (s *AccountService) RequestPasswordReset(email string) {
	s.mailer.Background(func(ctx context.Context) {
		user, err := s.repo.FindByEmailIgnoringCase(ctx, email)
		if err != nil {
			s.logger.Errorw("Failed to look up the account of a password reset", "error", err)
			return
		}
		if user == nil {
			return
		}
		token, err := s.issue(ctx, user.ID, PurposeReset, ResetTokenPrefix, ResetTokenLifetime)
		if err != nil {
			s.logger.Errorw("Failed to make a password reset token", "userId", user.ID, "error", err)
			return
		}
		message := mailer.ResetPassword(s.link(ctx, "reset-password", token), ResetTokenLifetime)
		if err := s.mailer.Send(ctx, user.Email, message.Subject, message.Text, message.HTML); err != nil {
			s.logger.Errorw("Failed to send a password reset email", "userId", user.ID, "reason", mailer.Describe(err))
		}
	})
}

// CreateResetLink makes a single-use reset link for the user, for an instance admin to hand over: it
// needs no sender for account emails and sends nothing. The link opens the web app's reset page and
// works once, until it expires ResetTokenLifetime from now; using it ends every session of the
// user. The token is in the link only: the service stores its hash. ErrUserNotFound is for a user
// that does not exist.
func (s *AccountService) CreateResetLink(ctx context.Context, userID string) (link string, expiresAt time.Time, err error) {
	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if user == nil {
		return "", time.Time{}, ErrUserNotFound
	}
	token, err := s.issue(ctx, userID, PurposeReset, ResetTokenPrefix, ResetTokenLifetime)
	if err != nil {
		return "", time.Time{}, err
	}
	return s.link(ctx, "reset-password", token), s.now().UTC().Add(ResetTokenLifetime), nil
}

// ResetPassword spends a reset token: it sets the user's password, ends every session the user
// has (see Model.TokensValidAfter), marks the address verified if it was not, and invalidates
// their other unused reset links. The password
// has been validated by the caller. A token that is unknown, used or expired returns ErrInvalidLink
// and changes nothing, and so does a failure.
func (s *AccountService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if !validTokenShape(token, ResetTokenPrefix) {
		return ErrInvalidLink
	}
	// Look the token up before the costly hash, so that a request with a token that is no good
	// costs a lookup and not a bcrypt. The consume below stays the guard: of two requests that
	// both pass this, one wins.
	live, err := s.accounts.FindLiveToken(ctx, PurposeReset, hashAccountToken(token), s.now().UTC())
	if err != nil {
		return err
	}
	if live == "" {
		return ErrInvalidLink
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash the new password: %w", err)
	}
	var (
		lifted  []workspace.ID
		resetID string
	)
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		now := s.now().UTC()
		userID, err := s.accounts.ConsumeToken(ctx, PurposeReset, hashAccountToken(token), now)
		if err != nil {
			return err
		}
		if userID == "" {
			return ErrInvalidLink
		}
		resetID = userID
		// To the millisecond, as the tokens' issue times are: a session started right after this
		// instant must be later than it
		if err := s.accounts.SetPassword(ctx, userID, string(hashed), now.Truncate(time.Millisecond)); err != nil {
			return err
		}
		// The link reached the address, which proves it as a verification link does (the first
		// time stays when it was verified already)
		if err := s.accounts.MarkEmailVerified(ctx, userID, now); err != nil {
			return err
		}
		if lifted, err = s.workspaces.LiftUnverifiedOwnedBy(ctx, userID); err != nil {
			return err
		}
		return s.accounts.DeleteUnusedTokens(ctx, userID, PurposeReset)
	})
	if err != nil {
		return err
	}
	s.wake(ctx, lifted)
	// Their open sockets close: a socket is a session no token check reaches again. A bus that
	// fails is logged: the reset stands.
	if err := events.Publish(ctx, s.bus, SessionsEnded, SessionsEndedEvent{UserID: resetID}); err != nil {
		s.logger.Errorw("Failed to publish that the sessions of a user ended", "userId", resetID, "error", err)
	}
	return nil
}

// wake has the scheduler and the probes pick up the monitors of workspaces whose suspension was
// lifted: they were not changed, so nothing else would tell them. A failure is logged and does not
// undo the verification; the scheduler reads the database again on its own schedule.
func (s *AccountService) wake(ctx context.Context, lifted []workspace.ID) {
	for _, id := range lifted {
		if err := s.monitors.PublishActiveUpdated(ctx, id); err != nil {
			s.logger.Errorw("Failed to wake the monitors of a workspace that is no longer suspended", "workspaceId", id, "error", err)
		}
	}
}

// SendVerification emails the user a verification link, unless the install has no sender or the
// user is verified already. A new link replaces the earlier ones. It does not wait for the SMTP
// server. An error means the link could not be made.
func (s *AccountService) SendVerification(ctx context.Context, user *Model) error {
	if !s.Enabled() || user.EmailVerifiedAt != nil {
		return nil
	}
	token, err := s.issue(ctx, user.ID, PurposeVerify, VerifyTokenPrefix, VerifyTokenLifetime)
	if err != nil {
		return err
	}
	message := mailer.VerifyEmail(s.link(ctx, "verify-email", token), VerifyTokenLifetime)
	s.mailer.Deliver(user.Email, message.Subject, message.Text, message.HTML)
	return nil
}

// ResendVerification is SendVerification for a signed-in user who asks for the link again. It
// returns ErrAccountEmailsOff without a sender, ErrAlreadyVerified for a verified address and
// ErrUserNotFound for a user that does not exist.
func (s *AccountService) ResendVerification(ctx context.Context, userID string) error {
	if !s.Enabled() {
		return ErrAccountEmailsOff
	}
	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrUserNotFound
	}
	if user.EmailVerifiedAt != nil {
		return ErrAlreadyVerified
	}
	return s.SendVerification(ctx, user)
}

// EmailOf returns the email of the user, for the key a rate limit counts under
func (s *AccountService) EmailOf(ctx context.Context, userID string) (string, error) {
	user, err := s.repo.FindByID(ctx, userID)
	if err != nil || user == nil {
		return "", err
	}
	return user.Email, nil
}

// VerifyEmail spends a verification token and records that the user's address was verified.
// A token that is unknown, used or expired returns ErrInvalidLink.
func (s *AccountService) VerifyEmail(ctx context.Context, token string) error {
	if !validTokenShape(token, VerifyTokenPrefix) {
		return ErrInvalidLink
	}
	var lifted []workspace.ID
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		now := s.now().UTC()
		userID, err := s.accounts.ConsumeToken(ctx, PurposeVerify, hashAccountToken(token), now)
		if err != nil {
			return err
		}
		if userID == "" {
			return ErrInvalidLink
		}
		if err := s.accounts.MarkEmailVerified(ctx, userID, now); err != nil {
			return err
		}
		// A new account's workspaces run from the moment the address is verified
		lifted, err = s.workspaces.LiftUnverifiedOwnedBy(ctx, userID)
		return err
	})
	if err != nil {
		return err
	}
	s.wake(ctx, lifted)
	return nil
}

// issue makes a token for the user and stores its hash. A verification link replaces the user's
// earlier unused ones; reset links are all kept until one is used, which removes the others.
func (s *AccountService) issue(ctx context.Context, userID, purpose, prefix string, lifetime time.Duration) (string, error) {
	random := make([]byte, accountTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("error generating an account token: %w", err)
	}
	token := prefix + base64.RawURLEncoding.EncodeToString(random)
	now := s.now().UTC()
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		if purpose == PurposeVerify {
			if err := s.accounts.DeleteUnusedTokens(ctx, userID, purpose); err != nil {
				return err
			}
		}
		return s.accounts.CreateToken(ctx, &AccountToken{
			ID: uuid.NewString(), UserID: userID, Purpose: purpose, TokenHash: hashAccountToken(token),
			CreatedAt: now, ExpiresAt: now.Add(lifetime),
		})
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// link is the address in the web app that opens a link of the email
func (s *AccountService) link(ctx context.Context, page, token string) string {
	return strings.TrimRight(s.settings.PublicURL(ctx), "/") + "/" + page + "/" + token
}

func validTokenShape(token, prefix string) bool {
	return strings.HasPrefix(token, prefix) && len(token) <= maxAccountTokenLength
}

// hashAccountToken is what is stored of a token: the same SHA-256 form as the invites'
func hashAccountToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256$" + hex.EncodeToString(sum[:])
}
