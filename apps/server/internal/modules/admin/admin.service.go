package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/workspace"

	"go.uber.org/zap"
)

// Service is what an instance admin does to the users of the install. Every method takes the ID of
// the admin who acts, and logs the action with it: who did what to whom, never a token or password.
type Service interface {
	// ListUsers returns a page of users with their workspaces, the oldest first, and how many
	// users match the search: an email that contains q, ignoring case
	ListUsers(ctx context.Context, q string, page, limit int) ([]*User, int, error)
	// UpdateUser gives or takes the instance admin flag, and disables or enables the user:
	// ErrUserNotFound, ErrSelf (disabling oneself) or ErrLastInstanceAdmin. A disabled user's
	// sessions end and their workspaces run on.
	UpdateUser(ctx context.Context, actorID, userID string, update UserUpdate) (*User, error)
	// CreateResetLink makes a single-use password reset link for the user: ErrUserNotFound. With a
	// sender for account emails the user is told, in the background, that it was made.
	CreateResetLink(ctx context.Context, actorID, userID string) (*ResetLink, error)
	// DisableTwoFA turns the user's 2FA off: ErrUserNotFound. With a sender for account emails a
	// user who had it on is told, in the background.
	DisableTwoFA(ctx context.Context, actorID, userID string) (*User, error)
	// DeleteUser deletes the user: ErrUserNotFound, ErrSelf, ErrLastInstanceAdmin, or ErrLastOwner
	// (a LastOwnerError, naming the workspaces) while they are the only owner of a workspace
	DeleteUser(ctx context.Context, actorID, userID string) error
}

// ResetLinks makes password reset links. auth.AccountService is one.
type ResetLinks interface {
	CreateResetLink(ctx context.Context, userID string) (link string, expiresAt time.Time, err error)
}

type ServiceImpl struct {
	repo       Repository
	transactor dbtx.Transactor
	links      ResetLinks
	bus        events.EventBus
	mailer     *mailer.Mailer
	installURL string
	logger     *zap.SugaredLogger
	now        func() time.Time
}

func NewService(
	repo Repository,
	transactor dbtx.Transactor,
	links ResetLinks,
	bus events.EventBus,
	mailer *mailer.Mailer,
	cfg *config.Config,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repo:       repo,
		transactor: transactor,
		links:      links,
		bus:        bus,
		mailer:     mailer,
		installURL: strings.TrimRight(cfg.ClientURL, "/"),
		logger:     logger.Named("[admin-service]"),
		now:        time.Now,
	}
}

// log writes the one line an admin action leaves
func (s *ServiceImpl) log(actorID, action, userID string) {
	s.logger.Infow("Admin action", "actorUserId", actorID, "action", action, "targetUserId", userID)
}

func (s *ServiceImpl) ListUsers(ctx context.Context, q string, page, limit int) ([]*User, int, error) {
	users, total, err := s.repo.ListUsers(ctx, q, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.repo.MembershipsAnyWorkspace(ctx, users); err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func (s *ServiceImpl) UpdateUser(ctx context.Context, actorID, userID string, update UserUpdate) (*User, error) {
	if update.Active != nil && !*update.Active && userID == actorID {
		return nil, ErrSelf
	}
	var (
		updated    *User
		actions    []string
		endedUsers bool
	)
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		// The active instance admins are locked first, so that two changes that race for the last
		// one take the same lock
		admins, err := s.repo.LockActiveInstanceAdmins(ctx)
		if err != nil {
			return err
		}
		user, err := s.repo.FindUser(ctx, userID)
		if err != nil {
			return err
		}
		if user == nil {
			return ErrUserNotFound
		}
		actions, endedUsers = nil, false

		// The user stops being an active instance admin by losing the flag or by being disabled
		flag, active := user.InstanceAdmin, user.Active
		if update.InstanceAdmin != nil {
			flag = *update.InstanceAdmin
		}
		if update.Active != nil {
			active = *update.Active
		}
		if user.InstanceAdmin && user.Active && !(flag && active) && len(admins) <= 1 {
			return ErrLastInstanceAdmin
		}

		if update.InstanceAdmin != nil && *update.InstanceAdmin != user.InstanceAdmin {
			if err := s.repo.SetInstanceAdmin(ctx, userID, *update.InstanceAdmin); err != nil {
				return err
			}
			actions = append(actions, ActionSetInstanceAdmin)
		}
		if update.Active != nil && *update.Active != user.Active {
			var endAt *time.Time
			if !*update.Active {
				// To the millisecond, as the tokens' issue times are, and rounded up: a token issued in
				// the same millisecond as the disabling ends with the rest
				at := s.now().UTC().Add(time.Millisecond).Truncate(time.Millisecond)
				endAt = &at
			}
			if err := s.repo.SetActive(ctx, userID, *update.Active, endAt); err != nil {
				return err
			}
			if *update.Active {
				actions = append(actions, ActionEnableUser)
			} else {
				actions = append(actions, ActionDisableUser)
				endedUsers = true
			}
		}
		updated, err = s.repo.FindUser(ctx, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, action := range actions {
		s.log(actorID, action, userID)
	}
	if endedUsers {
		// Their open sockets close, which ends the workspaces' updates too. The sessions ended and
		// the memberships did not, so nothing announces MemberLeft: its subscribers take it for a
		// membership that is gone.
		s.announceSessionsEnded(ctx, userID)
	}
	return updated, nil
}

func (s *ServiceImpl) CreateResetLink(ctx context.Context, actorID, userID string) (*ResetLink, error) {
	user, err := s.repo.FindUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	link, expiresAt, err := s.links.CreateResetLink(ctx, userID)
	if errors.Is(err, auth.ErrUserNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	s.log(actorID, ActionCreateResetLink, userID)
	s.notify(user.Email, mailer.ResetLinkMade(s.installURL))
	return &ResetLink{Link: link, ExpiresAt: expiresAt}, nil
}

func (s *ServiceImpl) DisableTwoFA(ctx context.Context, actorID, userID string) (*User, error) {
	var (
		updated *User
		wasOn   bool
	)
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		user, err := s.repo.FindUser(ctx, userID)
		if err != nil {
			return err
		}
		if user == nil {
			return ErrUserNotFound
		}
		wasOn = user.TwoFactor
		if err := s.repo.ClearTwoFA(ctx, userID); err != nil {
			return err
		}
		updated, err = s.repo.FindUser(ctx, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.log(actorID, ActionDisableTwoFA, userID)
	if wasOn {
		s.notify(updated.Email, mailer.TwoFactorTurnedOff(s.installURL))
	}
	return updated, nil
}

func (s *ServiceImpl) DeleteUser(ctx context.Context, actorID, userID string) error {
	if userID == actorID {
		return ErrSelf
	}
	var left []Membership
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		admins, err := s.repo.LockActiveInstanceAdmins(ctx)
		if err != nil {
			return err
		}
		user, err := s.repo.FindUser(ctx, userID)
		if err != nil {
			return err
		}
		if user == nil {
			return ErrUserNotFound
		}
		// A disabled instance admin is not counted: they can't sign in
		if user.InstanceAdmin && user.Active && len(admins) <= 1 {
			return ErrLastInstanceAdmin
		}
		lastOwned, err := s.repo.WorkspacesOwnedAloneAnyWorkspace(ctx, userID)
		if err != nil {
			return err
		}
		if len(lastOwned) > 0 {
			return &LastOwnerError{Workspaces: lastOwned}
		}
		left = user.Memberships
		return s.repo.DeleteUser(ctx, userID)
	})
	if err != nil {
		return err
	}
	s.log(actorID, ActionDeleteUser, userID)
	s.announceSessionsEnded(ctx, userID)
	s.announceLeft(ctx, userID, left)
	return nil
}

// notify emails the user that an instance admin did something that lets the admin act as them. It
// sends in the background and does nothing without a sender; a failure is logged without the
// address and does not fail the action (ADR 0005, section 10).
func (s *ServiceImpl) notify(to string, message mailer.Message) {
	s.mailer.Deliver(to, message.Subject, message.Text, message.HTML)
}

// announceSessionsEnded tells the rest of the install that the user's sessions ended, once that is
// stored, so that their open sockets close. A bus that fails is logged and not returned: the change
// stands.
func (s *ServiceImpl) announceSessionsEnded(ctx context.Context, userID string) {
	if err := events.Publish(ctx, s.bus, auth.SessionsEnded, auth.SessionsEndedEvent{UserID: userID}); err != nil {
		s.logger.Errorw("Failed to publish that the sessions of a user ended", "userId", userID, "error", err)
	}
}

// announceLeft tells the rest of the install that the user's places in the workspaces are gone,
// once that is stored. A bus that fails is logged and not returned: the change
// stands.
func (s *ServiceImpl) announceLeft(ctx context.Context, userID string, memberships []Membership) {
	for _, m := range memberships {
		if err := events.Publish(ctx, s.bus, workspace.MemberLeft, workspace.MemberLeftEvent{WorkspaceID: m.WorkspaceID, UserID: userID}); err != nil {
			s.logger.Errorw("Failed to publish that a member left a workspace", "workspaceId", m.WorkspaceID, "userId", userID, "error", err)
		}
	}
}
