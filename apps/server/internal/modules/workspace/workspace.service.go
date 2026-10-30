package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"

	"peekaping/internal/dbtx"
	"peekaping/internal/installsettings"
	"peekaping/internal/limits"
	"peekaping/internal/mailer"
	"peekaping/internal/modules/events"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// inviteTokenBytes is how many random bytes an invite's token holds, as many as an API key's
const inviteTokenBytes = 32

// maxInviteTokenLength bounds what a request may make the server hash and look up
const maxInviteTokenLength = 200

// Service is what the people of a workspace do: invite, join, change roles, leave and be removed
// (docs/adr/0005 section 1 and 11). Its methods that act in a workspace read it, and the caller's
// role there, from the context (WithContext), and return ErrNotFound for a context with none and
// ErrForbidden for a role that is too low. A user who is removed or leaves loses access at once:
// the next request they make to the workspace answers 404, and MemberLeft is published once the
// change is stored.
type Service interface {
	// ListMembers returns the members of the workspace, for any member of it
	ListMembers(ctx context.Context) ([]Member, error)
	// ChangeRole gives a member a role: ErrMemberNotFound, ErrInvalidRole, ErrForbidden or ErrLastOwner
	ChangeRole(ctx context.Context, userID string, role Role) (*Member, error)
	// RemoveMember removes a member: ErrMemberNotFound, ErrForbidden or ErrLastOwner
	RemoveMember(ctx context.Context, userID string) error
	// Leave removes the user from the workspace the context runs in: ErrLastOwner for its last owner
	Leave(ctx context.Context, userID string) error

	// ListInvites returns the invites that are neither used nor revoked, expired ones included
	ListInvites(ctx context.Context) ([]Invite, error)
	// CreateInvite invites the email with the role. userID is the inviter, empty for an API key.
	// An earlier invite for the email is revoked, so that one link is live. A workspace whose
	// members and pending invites are as many as its members limit is refused (limits.ReachedError).
	CreateInvite(ctx context.Context, userID, email string, role Role) (*CreatedInvite, error)
	// RevokeInvite revokes a pending invite of the workspace: ErrInviteNotFound
	RevokeInvite(ctx context.Context, inviteID string) error

	// PreviewInvite returns what the holder of a token may see: ErrInviteNotFound for an unknown one
	PreviewInvite(ctx context.Context, token string) (*InvitePreview, error)
	// CheckInvite returns the error AcceptInvite would give a user whose account has the email,
	// and nil when the invite can be used by them. It changes nothing: signup asks before it
	// spends time on a password.
	CheckInvite(ctx context.Context, token, email string) error
	// AcceptInvite makes the user a member of the invite's workspace with its role, if the email of
	// their account is the one the invite names. It joins the transaction the context carries, so
	// that signup can create the user and the membership together. It returns ErrInviteNotFound,
	// an ErrInviteUnavailable (ErrInviteUsed, ErrInviteExpired, ErrInviteRevoked),
	// ErrInviteEmailMismatch, ErrAlreadyMember, or a limits.ReachedError when the workspace has as
	// many members as its limit, and changes nothing then.
	AcceptInvite(ctx context.Context, token, userID string) (*Membership, error)

	// CreateWorkspace makes a workspace with the user as its owner and the settings of a new one. A
	// user who owns as many workspaces as the install's limit is refused (limits.ReachedError).
	CreateWorkspace(ctx context.Context, userID, name string) (*Membership, error)
	// RenameWorkspace renames the workspace the context runs in, for an admin or an owner:
	// ErrNotFound, ErrForbidden or ErrInvalidName (a name that is blank once trimmed). The
	// membership it returns holds the caller's role there; its JoinedAt is not read.
	RenameWorkspace(ctx context.Context, name string) (*Membership, error)
	// ScheduleDeletion holds the workspace the context runs in for deletion, for an owner who types
	// its exact name (PP-170): ErrNotFound, ErrForbidden, ErrNameMismatch or ErrDeletionNotScheduled
	// (a cancel committed at the same moment). It returns when the workspace will be deleted, 7 days
	// from now (DeletionGracePeriod); scheduling again keeps the first date. userID is the owner, for
	// the log.
	ScheduleDeletion(ctx context.Context, userID, name string) (time.Time, error)
	// CancelDeletion lifts the deletion hold of the workspace the context runs in, for an owner:
	// ErrNotFound, ErrForbidden or ErrDeletionNotScheduled. Another hold on the workspace stays, and
	// its monitors run again at once when none does.
	CancelDeletion(ctx context.Context, userID string) error
	// DeleteAnyWorkspace deletes the workspace with the ID and everything in it by cascade, and
	// returns ErrNotFound for one that does not exist. It checks no role and reads no workspace
	// from the context: the caller decides who may delete which workspace (an instance admin any,
	// an owner their own). Once the delete is stored it publishes events.MonitorDeleted for each of
	// the workspace's monitors, so that they stop at once, and MemberLeft for each member, so that
	// their open sockets leave its rooms. Call it outside a transaction, or the events go before the commit.
	DeleteAnyWorkspace(ctx context.Context, id ID) error
	// DeleteDueAnyWorkspace is DeleteAnyWorkspace for the cleanup job: it deletes the workspace only
	// if its scheduled deletion is due at now when the delete runs, and says whether it did. A
	// workspace that is gone, or whose deletion was cancelled or scheduled again since the job
	// listed it, is no error: it returns false, deletes nothing and publishes nothing.
	DeleteDueAnyWorkspace(ctx context.Context, id ID, now time.Time) (bool, error)
}

// MonitorWaker makes the scheduler and the probes pick up the monitors of a workspace whose
// suspension was lifted. monitor.Service is one; the monitor module imports this one, so the
// wiring joins them.
type MonitorWaker interface {
	PublishActiveUpdated(ctx context.Context, workspaceID ID) error
}

type ServiceImpl struct {
	workspaces Repository
	members    MemberRepository
	invites    InviteRepository
	transactor dbtx.Transactor
	bus        events.EventBus
	// settings give the public URL of invite links, and whether signup is open: then a workspace made
	// by a user whose email is not verified is suspended, as the one their signup made is
	settings *installsettings.Resolver
	mailer   *mailer.Mailer
	monitors MonitorWaker
	logger   *zap.SugaredLogger
	now      func() time.Time
	// limits are the workspaces' limits (PP-160). Without them no workspace has any.
	limits limits.Source
}

// ServiceOption changes how the workspace service behaves, for an install that has a reason to
type ServiceOption func(*ServiceImpl)

// WithLimits tells the service each workspace's limits, which it enforces: how many members a
// workspace may have, and how many workspaces a user may own
func WithLimits(source limits.Source) ServiceOption {
	return func(s *ServiceImpl) { s.limits = source }
}

func NewService(
	workspaces Repository,
	members MemberRepository,
	invites InviteRepository,
	transactor dbtx.Transactor,
	bus events.EventBus,
	settings *installsettings.Resolver,
	mailer *mailer.Mailer,
	monitors MonitorWaker,
	logger *zap.SugaredLogger,
	opts ...ServiceOption,
) Service {
	s := &ServiceImpl{
		workspaces: workspaces,
		members:    members,
		invites:    invites,
		transactor: transactor,
		bus:        bus,
		settings:   settings,
		mailer:     mailer,
		monitors:   monitors,
		logger:     logger.Named("[workspace-service]"),
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// limitsOf are the workspace's limits (PP-160), none without a source
func (s *ServiceImpl) limitsOf(ctx context.Context, id ID) (limits.Limits, error) {
	if s.limits == nil {
		return limits.Limits{}, nil
	}
	l, _, err := s.limits.For(ctx, id)
	if err != nil {
		return limits.Limits{}, fmt.Errorf("read the workspace's limits: %w", err)
	}
	return l, nil
}

// requireRoomForMember refuses one more member of a workspace that has as many as its limit, or
// more. An invite counts the pending invites beside the members (withInvites), since each can
// become one; accepting one counts the members alone, so that an invite counted when it was made
// can be accepted. It holds the workspace first, in the caller's transaction, so that two such
// writes at once count one after the other (limits.LockWorkspace).
func (s *ServiceImpl) requireRoomForMember(ctx context.Context, id ID, l limits.Limits, withInvites bool) error {
	if l.Members == nil {
		return nil
	}
	if err := s.workspaces.LockWorkspace(ctx, id); err != nil {
		return fmt.Errorf("lock the workspace: %w", err)
	}
	have, err := s.members.CountMembers(ctx, id)
	if err != nil {
		return fmt.Errorf("count the members: %w", err)
	}
	if withInvites {
		pending, err := s.invites.CountPending(ctx, id, s.now())
		if err != nil {
			return fmt.Errorf("count the pending invites: %w", err)
		}
		have += pending
	}
	if have >= *l.Members {
		return limits.Reached(limits.Members, *l.Members)
	}
	return nil
}

// requireRoomForOwned refuses a workspace the user creates when they own as many as the limit, or
// more. The limit is the install's default: the workspace they create has no limits of its own yet,
// and the user's other workspaces each have their own. It holds the user first, in the caller's
// transaction (limits.LockUser).
func (s *ServiceImpl) requireRoomForOwned(ctx context.Context, userID string, most *int) error {
	if most == nil {
		return nil
	}
	if err := s.members.LockUser(ctx, userID); err != nil {
		return fmt.Errorf("lock the user: %w", err)
	}
	owned, err := s.members.CountOwned(ctx, userID)
	if err != nil {
		return fmt.Errorf("count the owned workspaces: %w", err)
	}
	if owned >= *most {
		return limits.Reached(limits.OwnedWorkspaces, *most)
	}
	return nil
}

// scope returns the workspace the context runs in and the caller's role, after checking the role
func scope(ctx context.Context, min Role) (ID, Role, error) {
	if err := RequireRole(ctx, min); err != nil {
		return "", "", err
	}
	id, role, _ := FromContext(ctx)
	return id, role, nil
}

// MARK: Members

func (s *ServiceImpl) ListMembers(ctx context.Context) ([]Member, error) {
	id, _, err := scope(ctx, RoleMember)
	if err != nil {
		return nil, err
	}
	return s.members.ListMembers(ctx, id)
}

func (s *ServiceImpl) ChangeRole(ctx context.Context, userID string, role Role) (*Member, error) {
	id, actor, err := scope(ctx, RoleAdmin)
	if err != nil {
		return nil, err
	}
	var changed *Member
	var lifted []ID
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		target, owners, err := s.targetAndOwners(ctx, id, userID)
		if err != nil {
			return err
		}
		if err := CanChange(actor, target.Role, &role, len(owners)); err != nil {
			return err
		}
		if _, err := s.members.UpdateRole(ctx, id, userID, role); err != nil {
			return err
		}
		updated := *target
		updated.Role = role
		changed = &updated
		if role == RoleOwner {
			lifted, err = s.liftForNewOwner(ctx, id, userID)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	s.wake(ctx, lifted)
	return changed, nil
}

// liftForNewOwner ends the workspace's unverified suspension, inside the transaction that made
// userID one of its owners, when an owner has a verified email address. The new owner's row is
// held before the lift reads, as CreateWorkspace does: on PostgreSQL a verification in flight that committed after
// the lift's read would otherwise find the owner role uncommitted and lift nothing, and the
// workspace would stay suspended for good. Now it is either seen by the lift or waits for the
// commit, and then finds the owner.
func (s *ServiceImpl) liftForNewOwner(ctx context.Context, id ID, userID string) ([]ID, error) {
	if _, err := s.members.LockEmailVerified(ctx, userID); err != nil {
		return nil, err
	}
	return s.workspaces.LiftUnverifiedWithVerifiedOwner(ctx, id)
}

func (s *ServiceImpl) RemoveMember(ctx context.Context, userID string) error {
	id, actor, err := scope(ctx, RoleAdmin)
	if err != nil {
		return err
	}
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		target, owners, err := s.targetAndOwners(ctx, id, userID)
		if err != nil {
			return err
		}
		if err := CanChange(actor, target.Role, nil, len(owners)); err != nil {
			return err
		}
		_, err = s.members.RemoveMember(ctx, id, userID)
		return err
	})
	if err != nil {
		return err
	}
	s.announceLeft(ctx, id, userID)
	return nil
}

func (s *ServiceImpl) Leave(ctx context.Context, userID string) error {
	id, _, err := scope(ctx, RoleMember)
	if err != nil {
		return err
	}
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		target, owners, err := s.targetAndOwners(ctx, id, userID)
		if err != nil {
			return err
		}
		if err := CanLeave(target.Role, len(owners)); err != nil {
			return err
		}
		_, err = s.members.RemoveMember(ctx, id, userID)
		return err
	})
	if err != nil {
		return err
	}
	s.announceLeft(ctx, id, userID)
	return nil
}

// wake has the scheduler and the probes pick up the monitors of workspaces whose suspension a
// change of owners lifted: the monitors were not changed, so nothing else would tell them. A failure
// is logged and does not undo the change.
func (s *ServiceImpl) wake(ctx context.Context, lifted []ID) {
	if s.monitors == nil {
		return
	}
	for _, id := range lifted {
		if err := s.monitors.PublishActiveUpdated(ctx, id); err != nil {
			s.logger.Errorw("Failed to wake the monitors of a workspace that is no longer suspended", "workspaceId", id, "error", err)
		}
	}
}

// targetAndOwners reads the member a change is about and the workspace's owners, which are locked
// until the transaction ends. The owners come first, so that two changes that race for the last
// owner take the same lock.
func (s *ServiceImpl) targetAndOwners(ctx context.Context, id ID, userID string) (*Member, []string, error) {
	owners, err := s.members.LockOwners(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	target, err := s.members.FindMember(ctx, id, userID)
	if err != nil {
		return nil, nil, err
	}
	if target == nil {
		return nil, nil, ErrMemberNotFound
	}
	return target, owners, nil
}

// announceLeft tells the rest of the install that the user's place in the workspace is gone, once
// it is stored. A bus that fails is logged and not returned: the change stands.
func (s *ServiceImpl) announceLeft(ctx context.Context, id ID, userID string) {
	if err := events.Publish(ctx, s.bus, MemberLeft, MemberLeftEvent{WorkspaceID: id, UserID: userID}); err != nil {
		s.logger.Errorw("Failed to publish that a member left a workspace", "workspaceId", id, "userId", userID, "error", err)
	}
}

func (s *ServiceImpl) DeleteAnyWorkspace(ctx context.Context, id ID) error {
	deleted, err := s.deleteAndAnnounce(ctx, id, s.workspaces.Delete)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}

func (s *ServiceImpl) DeleteDueAnyWorkspace(ctx context.Context, id ID, now time.Time) (bool, error) {
	return s.deleteAndAnnounce(ctx, id, func(ctx context.Context, id ID) (*Deleted, error) {
		return s.workspaces.DeleteDue(ctx, id, now)
	})
}

// deleteAndAnnounce runs the delete in a transaction and, once it is committed and deleted
// something, announces it. It says whether the workspace was deleted.
func (s *ServiceImpl) deleteAndAnnounce(ctx context.Context, id ID, del func(context.Context, ID) (*Deleted, error)) (bool, error) {
	// An ID that is no UUID names no workspace, and PostgreSQL refuses to compare it with one
	if _, err := uuid.Parse(string(id)); err != nil {
		return false, nil
	}
	var deleted *Deleted
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		var err error
		deleted, err = del(ctx, id)
		return err
	})
	if err != nil || deleted == nil {
		return false, err
	}
	s.announceDeleted(ctx, id, deleted)
	return true, nil
}

func (s *ServiceImpl) ScheduleDeletion(ctx context.Context, userID, name string) (time.Time, error) {
	id, _, err := scope(ctx, RoleOwner)
	if err != nil {
		return time.Time{}, err
	}
	found, err := s.workspaces.FindByID(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	if found == nil {
		return time.Time{}, ErrNotFound
	}
	if name != found.Name {
		return time.Time{}, ErrNameMismatch
	}
	scheduled, changed, err := s.workspaces.ScheduleDeletion(ctx, id, s.now().UTC().Add(DeletionGracePeriod))
	if err != nil {
		return time.Time{}, err
	}
	if scheduled == nil {
		return time.Time{}, ErrNotFound
	}
	if changed {
		s.logAction(userID, ActionScheduleDeletion, id)
	}
	// An owner's cancel may have committed between the repository's update and its read
	if scheduled.DeletionScheduledAt == nil {
		return time.Time{}, ErrDeletionNotScheduled
	}
	// The workspace's monitors need no event to stop: each check is loaded as it is due, and a
	// suspended workspace's is dropped, as when an instance admin suspends it
	return *scheduled.DeletionScheduledAt, nil
}

func (s *ServiceImpl) CancelDeletion(ctx context.Context, userID string) error {
	id, _, err := scope(ctx, RoleOwner)
	if err != nil {
		return err
	}
	cancelled, remaining, err := s.workspaces.CancelDeletion(ctx, id)
	if err != nil {
		return err
	}
	if !cancelled {
		return ErrDeletionNotScheduled
	}
	s.logAction(userID, ActionCancelDeletion, id)
	if !remaining {
		s.wake(ctx, []ID{id})
	}
	return nil
}

// The names of the actions of an owner that are logged, as the log says them
const (
	ActionScheduleDeletion = "schedule_deletion"
	ActionCancelDeletion   = "cancel_deletion"
)

// logAction writes the one line an owner's action on their workspace leaves, as the admin area's
// actions do: who did what to which workspace
func (s *ServiceImpl) logAction(userID, action string, id ID) {
	s.logger.Infow("Workspace action", "actorUserId", userID, "action", action, "targetWorkspaceId", string(id))
}

// announceDeleted tells the rest of the install that the workspace is gone, once that is stored:
// the scheduler, the probes and the metrics drop its monitors, and the websocket takes its members
// out of its rooms. A bus that fails is logged and not returned: the delete stands.
func (s *ServiceImpl) announceDeleted(ctx context.Context, id ID, deleted *Deleted) {
	for _, monitorID := range deleted.MonitorIDs {
		if err := events.Publish(ctx, s.bus, events.MonitorDeleted, monitorID); err != nil {
			s.logger.Errorw("Failed to publish that a monitor of a deleted workspace was deleted", "workspaceId", id, "monitorId", monitorID, "error", err)
		}
	}
	for _, userID := range deleted.UserIDs {
		s.announceLeft(ctx, id, userID)
	}
}

// MARK: Invites

func (s *ServiceImpl) ListInvites(ctx context.Context) ([]Invite, error) {
	id, _, err := scope(ctx, RoleAdmin)
	if err != nil {
		return nil, err
	}
	return s.invites.ListPending(ctx, id)
}

func (s *ServiceImpl) CreateInvite(ctx context.Context, userID, email string, role Role) (*CreatedInvite, error) {
	if err := RequireInvite(ctx, role); err != nil {
		return nil, err
	}
	id, _, _ := FromContext(ctx)
	email = normalizeEmail(email)
	token, hash, err := newInviteToken()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	invite := Invite{
		ID: uuid.NewString(), WorkspaceID: id, Email: email, Role: role,
		CreatedAt: now, ExpiresAt: now.Add(InviteLifetime),
	}
	if userID != "" {
		invite.InvitedBy = &userID
	}
	workspaceLimits, err := s.limitsOf(ctx, id)
	if err != nil {
		return nil, err
	}
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		member, err := s.members.HasMemberWithEmail(ctx, id, email)
		if err != nil {
			return err
		}
		if member {
			return ErrAlreadyMember
		}
		if err := s.invites.RevokePendingForEmail(ctx, id, email, now); err != nil {
			return err
		}
		// Counted once the earlier invite for the email is revoked: inviting it again takes no
		// more room
		if err := s.requireRoomForMember(ctx, id, workspaceLimits, true); err != nil {
			return err
		}
		return s.invites.CreateInvite(ctx, &invite, hash)
	})
	if err != nil {
		return nil, err
	}
	created := &CreatedInvite{Invite: invite, Token: token, Link: s.inviteLink(ctx, token)}
	s.emailInvite(ctx, created)
	return created, nil
}

// emailInvite emails the invite's link to the invited address, when the install has a sender for
// account emails. The invite is made already and its answer carries the link, so a failure is
// logged and nothing more.
func (s *ServiceImpl) emailInvite(ctx context.Context, created *CreatedInvite) {
	if !s.mailer.Enabled() {
		return
	}
	// The name of the workspace and the inviter are looked up in the request's context, before it ends
	workspaceName := ""
	if stored, err := s.workspaces.FindByID(ctx, created.WorkspaceID); err != nil {
		s.logger.Errorw("Failed to read the workspace of an invite to email it", "inviteId", created.ID, "error", err)
	} else if stored != nil {
		workspaceName = singleLine(stored.Name)
	}
	inviter := ""
	if created.InvitedBy != nil {
		email, err := s.members.UserEmail(ctx, *created.InvitedBy)
		if err != nil {
			s.logger.Errorw("Failed to read the inviter of an invite to email it", "inviteId", created.ID, "error", err)
		}
		inviter = email
	}
	if workspaceName == "" {
		workspaceName = "a workspace"
	}
	message := mailer.Invite(workspaceName, inviter, string(created.Role), created.Link, InviteLifetime)
	s.mailer.Deliver(created.Email, message.Subject, message.Text, message.HTML)
}

// singleLine turns each control character (a line break, a tab) into a space. The name of a workspace
// goes into the subject of the invite email, and the mailer refuses a subject with a line break
// (it would add headers), which would leave the invite unsent.
func singleLine(name string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name))
}

func (s *ServiceImpl) RevokeInvite(ctx context.Context, inviteID string) error {
	id, _, err := scope(ctx, RoleAdmin)
	if err != nil {
		return err
	}
	revoked, err := s.invites.Revoke(ctx, id, inviteID, s.now())
	if err != nil {
		return err
	}
	if !revoked {
		return ErrInviteNotFound
	}
	return nil
}

func (s *ServiceImpl) PreviewInvite(ctx context.Context, token string) (*InvitePreview, error) {
	invite, err := s.lookupInvite(ctx, token)
	if err != nil {
		return nil, err
	}
	return &InvitePreview{
		WorkspaceName: invite.WorkspaceName, Email: invite.Email, Role: invite.Role, Status: invite.StatusAt(s.now()),
	}, nil
}

func (s *ServiceImpl) AcceptInvite(ctx context.Context, token, userID string) (*Membership, error) {
	var joined *Membership
	var lifted []ID
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		invite, err := s.acceptable(ctx, token, userID)
		if err != nil {
			return err
		}
		joined, lifted, err = s.joinThroughInvite(ctx, invite, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.wake(ctx, lifted)
	return joined, nil
}

// acceptable finds the invite the token names and checks that the user may take it: it can be
// used, it is for their email, they are no member yet, and the workspace has room
func (s *ServiceImpl) acceptable(ctx context.Context, token, userID string) (*Invite, error) {
	invite, err := s.lookupInvite(ctx, token)
	if err != nil {
		return nil, err
	}
	email, err := s.members.UserEmail(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.usable(invite, email); err != nil {
		return nil, err
	}
	existing, err := s.members.FindMember(ctx, invite.WorkspaceID, userID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrAlreadyMember
	}
	workspaceLimits, err := s.limitsOf(ctx, invite.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := s.requireRoomForMember(ctx, invite.WorkspaceID, workspaceLimits, false); err != nil {
		return nil, err
	}
	return invite, nil
}

// joinThroughInvite makes the user a member through the invite and marks it accepted. A new owner lifts the
// unverified suspension of the workspace if they are verified, and the workspaces that lifted are
// returned.
func (s *ServiceImpl) joinThroughInvite(ctx context.Context, invite *Invite, userID string) (*Membership, []ID, error) {
	now := s.now().UTC()
	if err := s.workspaces.AddMember(ctx, invite.WorkspaceID, userID, invite.Role); err != nil {
		// The user became a member since the lookup above: the answer a later accept gets
		if dbtx.IsUniqueViolation(err) {
			return nil, nil, ErrAlreadyMember
		}
		return nil, nil, err
	}
	// The invite works once: of two accepts that race, the one that finds it used rolls back
	marked, err := s.invites.MarkAccepted(ctx, invite.ID, userID, now)
	if err != nil {
		return nil, nil, err
	}
	if !marked {
		return nil, nil, ErrInviteUsed
	}
	joined := &Membership{
		Workspace: Workspace{ID: invite.WorkspaceID, Name: invite.WorkspaceName},
		Role:      invite.Role,
		JoinedAt:  now,
	}
	var lifted []ID
	if invite.Role == RoleOwner {
		lifted, err = s.liftForNewOwner(ctx, invite.WorkspaceID, userID)
	}
	return joined, lifted, err
}

func (s *ServiceImpl) CheckInvite(ctx context.Context, token, email string) error {
	invite, err := s.lookupInvite(ctx, token)
	if err != nil {
		return err
	}
	return s.usable(invite, email)
}

// usable is nil when the invite can be used by an account with the email: it was neither used,
// revoked nor has it expired, and it names that email
func (s *ServiceImpl) usable(invite *Invite, email string) error {
	switch invite.StatusAt(s.now()) {
	case InviteAccepted:
		return ErrInviteUsed
	case InviteRevoked:
		return ErrInviteRevoked
	case InviteExpired:
		return ErrInviteExpired
	}
	if normalizeEmail(email) != invite.Email {
		return ErrInviteEmailMismatch
	}
	return nil
}

// lookupInvite finds the invite a token names, or returns ErrInviteNotFound. The token alone says
// the workspace, so this is the one place that reads an invite across workspaces.
func (s *ServiceImpl) lookupInvite(ctx context.Context, token string) (*Invite, error) {
	if !strings.HasPrefix(token, InviteTokenPrefix) || len(token) > maxInviteTokenLength {
		return nil, ErrInviteNotFound
	}
	invite, err := s.invites.FindByTokenHashAnyWorkspace(ctx, hashInviteToken(token))
	if err != nil {
		return nil, err
	}
	if invite == nil {
		return nil, ErrInviteNotFound
	}
	return invite, nil
}

// MARK: Workspaces

func (s *ServiceImpl) CreateWorkspace(ctx context.Context, userID, name string) (*Membership, error) {
	mostOwned, err := s.mostOwned(ctx)
	if err != nil {
		return nil, err
	}
	// Read before the transaction: the install settings are read outside it
	open := s.settings.SignupOpen(ctx)
	var created *Membership
	err = s.transactor.WithTx(ctx, func(ctx context.Context) error {
		created, err = s.createOwned(ctx, userID, name, mostOwned, open)
		return err
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// createOwned makes the workspace and its owner, in the transaction ctx carries
func (s *ServiceImpl) createOwned(ctx context.Context, userID, name string, mostOwned *int, open bool) (*Membership, error) {
	if err := s.requireRoomForOwned(ctx, userID, mostOwned); err != nil {
		return nil, err
	}
	// In an open install a user must verify their email before any workspace of theirs runs: one
	// made by a user who has not is suspended, so that making workspaces is no way round it
	suspend, err := s.mustSuspend(ctx, userID, open)
	if err != nil {
		return nil, err
	}
	made, err := s.makeWorkspace(ctx, name, suspend)
	if err != nil {
		return nil, err
	}
	if err := s.workspaces.AddMember(ctx, made.ID, userID, RoleOwner); err != nil {
		return nil, err
	}
	if suspend {
		if err := s.liftIfVerified(ctx, userID, made); err != nil {
			return nil, err
		}
	}
	return &Membership{Workspace: *made, Role: RoleOwner, JoinedAt: made.CreatedAt}, nil
}

// mostOwned is how many workspaces a user may own, nil without a limit
func (s *ServiceImpl) mostOwned(ctx context.Context) (*int, error) {
	if s.limits == nil {
		return nil, nil
	}
	snapshot, err := s.limits.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the install's limits: %w", err)
	}
	return snapshot.Defaults.OwnedWorkspaces, nil
}

// mustSuspend is whether a workspace the user makes starts suspended: the install is open and their
// email is not verified
func (s *ServiceImpl) mustSuspend(ctx context.Context, userID string, open bool) (bool, error) {
	if !open {
		return false, nil
	}
	verified, err := s.members.EmailVerified(ctx, userID)
	if err != nil {
		return false, err
	}
	return !verified, nil
}

// makeWorkspace adds the workspace with no member, suspended as unverified when asked to
func (s *ServiceImpl) makeWorkspace(ctx context.Context, name string, suspend bool) (*Workspace, error) {
	if suspend {
		return s.workspaces.CreateSuspended(ctx, strings.TrimSpace(name), SuspendedUnverified)
	}
	return s.workspaces.Create(ctx, strings.TrimSpace(name))
}

// liftIfVerified lifts the suspension of the workspace just made if the user is verified by now.
// The read before may be stale: a verification that committed since found no workspace of the user
// to lift, and would leave this one suspended for good. Read again with the user's row held, so
// that a verification either is seen now or waits for this workspace.
func (s *ServiceImpl) liftIfVerified(ctx context.Context, userID string, made *Workspace) error {
	verified, err := s.members.LockEmailVerified(ctx, userID)
	if err != nil {
		return err
	}
	if verified {
		if _, err := s.workspaces.LiftUnverifiedOwnedBy(ctx, userID); err != nil {
			return err
		}
		made.SuspendedAt, made.SuspendedReason = nil, ""
	}
	return nil
}

// MARK: Tokens

// normalizeEmail is the form an invite stores an email in, and compares it by
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// newInviteToken makes a token and the hash that is stored for it. The token is random, so a fast
// hash is enough, as it is for an API key.
func newInviteToken() (token, hash string, err error) {
	random := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("error generating an invite token: %w", err)
	}
	token = InviteTokenPrefix + base64.RawURLEncoding.EncodeToString(random)
	return token, hashInviteToken(token), nil
}

func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256$" + hex.EncodeToString(sum[:])
}

// inviteLink is the address in the web app that opens the invite
func (s *ServiceImpl) inviteLink(ctx context.Context, token string) string {
	return strings.TrimRight(s.settings.PublicURL(ctx), "/") + "/invite/" + token
}

func (s *ServiceImpl) RenameWorkspace(ctx context.Context, name string) (*Membership, error) {
	id, role, err := scope(ctx, RoleAdmin)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	renamed, err := s.workspaces.Rename(ctx, id, name)
	if err != nil {
		return nil, err
	}
	if renamed == nil {
		return nil, ErrNotFound
	}
	return &Membership{Workspace: *renamed, Role: role}, nil
}
