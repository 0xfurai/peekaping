package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/limits"
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
	// ListWorkspaces returns a page of the install's workspaces, the oldest first, with their
	// counts and whether and why each is suspended, and how many match the search: a name or an
	// owner's email that contains q, ignoring case. An instance admin sees no workspace's records,
	// so nothing inside a workspace is read.
	ListWorkspaces(ctx context.Context, q string, page, limit int) ([]*Workspace, int, error)
	// SetWorkspaceSuspended puts the admin's hold on the workspace (suspended true) or takes it off:
	// ErrWorkspaceNotFound. The hold is its own, beside the one that waits for the owner's email
	// verification: resuming leaves that one, and the workspace's monitors run again once no hold
	// remains. Nothing is deleted. A change that changes nothing is not logged.
	SetWorkspaceSuspended(ctx context.Context, actorID, workspaceID string, suspended bool) (*Workspace, error)
	// SetWorkspaceLimits gives the workspace limits of its own (PP-160), which replace the install's
	// defaults whole: an empty Limits is no limits at all. nil removes them, and the install's
	// defaults apply again. ErrWorkspaceNotFound. Each set and clear is logged.
	SetWorkspaceLimits(ctx context.Context, actorID, workspaceID string, own *limits.Limits) (*Workspace, error)
	// DeleteWorkspace deletes the workspace and everything in it: ErrWorkspaceNotFound. Its
	// monitors stop at once and its members' open sockets leave its rooms. The members keep their
	// accounts. An admin may delete a workspace they are a member of.
	DeleteWorkspace(ctx context.Context, actorID, workspaceID string) error
}

// WorkspaceDeleter deletes a workspace with its records. workspace.Service is one.
type WorkspaceDeleter interface {
	DeleteAnyWorkspace(ctx context.Context, id workspace.ID) error
}

// MonitorWaker has the scheduler and the probes pick up the active monitors of a workspace whose
// suspension was lifted: the monitors were not changed, so nothing else would tell them.
// monitor.Service is one.
type MonitorWaker interface {
	PublishActiveUpdated(ctx context.Context, workspaceID workspace.ID) error
}

// AccountRemover deletes a user's account with the checks of the install. auth.AccountRemover is one.
type AccountRemover interface {
	Remove(ctx context.Context, userID string) error
}

// ResetLinks makes password reset links. auth.AccountService is one.
type ResetLinks interface {
	CreateResetLink(ctx context.Context, userID string) (link string, expiresAt time.Time, err error)
}

type ServiceImpl struct {
	repo       Repository
	transactor dbtx.Transactor
	links      ResetLinks
	monitors   MonitorWaker
	workspaces WorkspaceDeleter
	accounts   AccountRemover
	bus        events.EventBus
	mailer     *mailer.Mailer
	settings   *installsettings.Resolver
	logger     *zap.SugaredLogger
	now        func() time.Time
	// limits are the workspaces' limits, read for the list and written for one workspace (PP-160)
	limits *limits.Chain
}

func NewService(
	repo Repository,
	transactor dbtx.Transactor,
	links ResetLinks,
	monitors MonitorWaker,
	workspaces WorkspaceDeleter,
	accounts AccountRemover,
	bus events.EventBus,
	mailer *mailer.Mailer,
	settings *installsettings.Resolver,
	workspaceLimits *limits.Chain,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		repo:       repo,
		transactor: transactor,
		links:      links,
		monitors:   monitors,
		workspaces: workspaces,
		accounts:   accounts,
		bus:        bus,
		mailer:     mailer,
		settings:   settings,
		logger:     logger.Named("[admin-service]"),
		now:        time.Now,
		limits:     workspaceLimits,
	}
}

// installURL is the address of the install that the notices name: the public URL now
func (s *ServiceImpl) installURL(ctx context.Context) string {
	return strings.TrimRight(s.settings.PublicURL(ctx), "/")
}

// log writes the one line an admin action leaves
func (s *ServiceImpl) log(actorID, action, userID string) {
	s.logger.Infow("Admin action", "actorUserId", actorID, "action", action, "targetUserId", userID)
}

// logWorkspace writes the one line an admin action on a workspace leaves
func (s *ServiceImpl) logWorkspace(actorID, action string, workspaceID string) {
	s.logger.Infow("Admin action", "actorUserId", actorID, "action", action, "targetWorkspaceId", workspaceID)
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
	s.notify(user.Email, mailer.ResetLinkMade(s.installURL(ctx)))
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
		s.notify(updated.Email, mailer.TwoFactorTurnedOff(s.installURL(ctx)))
	}
	return updated, nil
}

func (s *ServiceImpl) DeleteUser(ctx context.Context, actorID, userID string) error {
	if userID == actorID {
		return ErrSelf
	}
	// The checks, the delete and the announcements are the ones of a user deleting their own account
	if err := s.accounts.Remove(ctx, userID); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	s.log(actorID, ActionDeleteUser, userID)
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

func (s *ServiceImpl) ListWorkspaces(ctx context.Context, q string, page, limit int) ([]*Workspace, int, error) {
	workspaces, total, err := s.repo.ListWorkspacesAnyWorkspace(ctx, q, page, limit)
	if err != nil {
		return nil, 0, err
	}
	if err := s.fillLimits(ctx, workspaces...); err != nil {
		return nil, 0, err
	}
	return workspaces, total, nil
}

// fillLimits gives the workspaces their limits and where they come from, with one read for all of
// them. owned_workspaces is the install's default for each: it is what a user's workspaces are
// counted against.
func (s *ServiceImpl) fillLimits(ctx context.Context, workspaces ...*Workspace) error {
	snapshot := limits.Snapshot{}
	if s.limits != nil {
		var err error
		if snapshot, err = s.limits.Snapshot(ctx); err != nil {
			return fmt.Errorf("read the workspaces' limits: %w", err)
		}
	}
	for _, w := range workspaces {
		if w == nil {
			continue
		}
		w.Limits, w.LimitsSource = snapshot.For(w.ID)
		w.Limits.OwnedWorkspaces = snapshot.Defaults.OwnedWorkspaces
	}
	return nil
}

func (s *ServiceImpl) SetWorkspaceLimits(ctx context.Context, actorID, workspaceID string, own *limits.Limits) (*Workspace, error) {
	found, err := s.repo.FindWorkspaceAnyWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, ErrWorkspaceNotFound
	}
	// Outside a transaction: the Chain forgets what it read once the change is stored, so that this
	// process can't cache what it read before the commit
	if err := s.limits.SetOwn(ctx, found.ID, own); err != nil {
		return nil, err
	}
	if own == nil {
		s.logWorkspace(actorID, ActionClearWorkspaceLimits, workspaceID)
	} else {
		s.logWorkspace(actorID, ActionSetWorkspaceLimits, workspaceID)
	}
	if err := s.fillLimits(ctx, found); err != nil {
		return nil, err
	}
	return found, nil
}

func (s *ServiceImpl) SetWorkspaceSuspended(ctx context.Context, actorID, workspaceID string, suspended bool) (*Workspace, error) {
	var (
		updated   *Workspace
		changed   bool
		remaining bool
	)
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		found, err := s.repo.FindWorkspaceAnyWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		if found == nil {
			return ErrWorkspaceNotFound
		}
		changed, remaining, err = s.repo.SetAdminSuspended(ctx, workspaceID, suspended)
		if err != nil {
			return err
		}
		updated, err = s.repo.FindWorkspaceAnyWorkspace(ctx, workspaceID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if changed {
		if suspended {
			s.logWorkspace(actorID, ActionSuspendWorkspace, workspaceID)
		} else {
			s.logWorkspace(actorID, ActionResumeWorkspace, workspaceID)
			if !remaining {
				s.wake(ctx, workspace.ID(workspaceID))
			}
		}
	}
	if err := s.fillLimits(ctx, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// wake has the scheduler and the probes pick up the monitors of a workspace that runs again. A
// failure is logged and does not undo the resuming; the scheduler reads the database again on its
// own schedule.
func (s *ServiceImpl) wake(ctx context.Context, id workspace.ID) {
	if s.monitors == nil {
		return
	}
	if err := s.monitors.PublishActiveUpdated(ctx, id); err != nil {
		s.logger.Errorw("Failed to wake the monitors of a workspace that is no longer suspended", "workspaceId", id, "error", err)
	}
}

func (s *ServiceImpl) DeleteWorkspace(ctx context.Context, actorID, workspaceID string) error {
	err := s.workspaces.DeleteAnyWorkspace(ctx, workspace.ID(workspaceID))
	if errors.Is(err, workspace.ErrNotFound) {
		return ErrWorkspaceNotFound
	}
	if err != nil {
		return err
	}
	s.logWorkspace(actorID, ActionDeleteWorkspace, workspaceID)
	return nil
}
