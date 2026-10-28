package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/ratelimit"
	"peekaping/internal/turnstile"

	"go.uber.org/zap"
)

// maxOpenSignupsPerClient is the most open signups one client address may make in an hour
const maxOpenSignupsPerClient = 5

var (
	// ErrSignupRateLimited is for a client address that has signed up too often. It answers 429.
	ErrSignupRateLimited = errors.New("too many signups from this address")
	// ErrTurnstileRefused is for a signup whose Turnstile answer is missing, was not accepted, or
	// could not be checked. It answers 400.
	ErrTurnstileRefused = errors.New("the Turnstile answer was refused")
)

// SignupGate is what stands before an open signup (PP-155): a limit per client address, and the
// Turnstile check when the install has it set up. Invite signups do not pass it: the invite is the
// proof that the visitor is wanted. The limit counts in this process's memory (see package
// ratelimit).
type SignupGate struct {
	open      bool
	siteKey   string
	perClient *ratelimit.Limiter
	turnstile *turnstile.Verifier
	users     Repository
	logger    *zap.SugaredLogger
}

// NewSignupGate builds the gate from the install's settings
func NewSignupGate(cfg *config.Config, users Repository, logger *zap.SugaredLogger) *SignupGate {
	g := &SignupGate{
		open:      cfg.SignupOpen(),
		perClient: ratelimit.New(maxOpenSignupsPerClient, time.Hour),
		users:     users,
		logger:    logger.Named("[signup]"),
	}
	if g.open && strings.TrimSpace(cfg.TrustedProxies) == "" {
		// Behind a reverse proxy every request connects from the proxy, so with no trusted proxy
		// the limit sees one client for all of them
		g.logger.Warn("SIGNUP_MODE=open without TRUSTED_PROXIES: behind a reverse proxy every signup comes from the proxy's address, so all of them share one limit of " +
			strconv.Itoa(maxOpenSignupsPerClient) + " an hour. Set TRUSTED_PROXIES to the proxy's address or range, such as 127.0.0.1,10.0.0.0/8, so that each visitor is counted by their own address")
	}
	if cfg.SignupTurnstileEnabled() {
		g.siteKey = cfg.SignupTurnstileSiteKey
		g.turnstile = turnstile.New(cfg.SignupTurnstileSecret, cfg.SignupTurnstileVerifyURL)
	}
	return g
}

// Open is whether the install lets anyone sign up. A nil gate is an install that does not.
func (g *SignupGate) Open() bool { return g != nil && g.open }

// Mode is the install's SIGNUP_MODE
func (g *SignupGate) Mode() string {
	if g.Open() {
		return config.SignupModeOpen
	}
	return config.SignupModeInvite
}

// FirstSignup is whether the install has no user yet, in either mode: the next signup is its first
// user, who becomes the instance admin
func (g *SignupGate) FirstSignup(ctx context.Context) (bool, error) {
	if g == nil || g.users == nil {
		return false, nil
	}
	count, err := g.users.FindAllCount(ctx)
	return count == 0, err
}

// TurnstileSiteKey is the public key of the widget the app shows, or nil when open signup does not
// use Turnstile. Invite mode never does.
func (g *SignupGate) TurnstileSiteKey() *string {
	if !g.Open() || g.turnstile == nil {
		return nil
	}
	key := g.siteKey
	return &key
}

// Admit lets an open signup through, or returns ErrSignupRateLimited, with how long until the client
// may sign up again, or ErrTurnstileRefused. client
// is what the limit counts the signup under, and remoteIP the visitor's address for Cloudflare
// (empty when it can't be read). The limit is checked first, so that a flood of signups spends no
// call to Cloudflare; a signup that passed the limit and not Turnstile still counts. An unreachable
// Cloudflare refuses the signup too (fail closed), and the reason is logged.
func (g *SignupGate) Admit(ctx context.Context, client, remoteIP, answer string) (retryAfter time.Duration, err error) {
	if !g.Open() {
		return 0, nil
	}
	if allowed, wait := g.perClient.Allow(client); !allowed {
		g.logger.Infow("An open signup was refused: the client address signed up too often")
		return wait, ErrSignupRateLimited
	}
	if g.turnstile == nil {
		return 0, nil
	}
	if err := g.turnstile.Verify(ctx, answer, remoteIP); err != nil {
		g.logger.Warnw("An open signup was refused: Turnstile did not accept it", "error", err)
		return 0, ErrTurnstileRefused
	}
	return 0, nil
}
