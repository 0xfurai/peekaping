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

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
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
	// An earlier invite for the email is revoked, so that one link is live.
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
	// ErrInviteEmailMismatch or ErrAlreadyMember, and changes nothing then.
	AcceptInvite(ctx context.Context, token, userID string) (*Membership, error)

	// CreateWorkspace makes a workspace with the user as its owner and the settings of a new one
	CreateWorkspace(ctx context.Context, userID, name string) (*Membership, error)
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
	clientURL  string
	// signupOpen is SIGNUP_MODE=open: a workspace made by a user whose email is not verified is
	// suspended, as the one their signup made is
	signupOpen bool
	mailer     *mailer.Mailer
	monitors   MonitorWaker
	logger     *zap.SugaredLogger
	now        func() time.Time
}

func NewService(
	workspaces Repository,
	members MemberRepository,
	invites InviteRepository,
	transactor dbtx.Transactor,
	bus events.EventBus,
	cfg *config.Config,
	mailer *mailer.Mailer,
	monitors MonitorWaker,
	logger *zap.SugaredLogger,
) Service {
	return &ServiceImpl{
		workspaces: workspaces,
		members:    members,
		invites:    invites,
		transactor: transactor,
		bus:        bus,
		clientURL:  cfg.ClientURL,
		signupOpen: cfg.SignupOpen(),
		mailer:     mailer,
		monitors:   monitors,
		logger:     logger.Named("[workspace-service]"),
		now:        time.Now,
	}
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
		return s.invites.CreateInvite(ctx, &invite, hash)
	})
	if err != nil {
		return nil, err
	}
	created := &CreatedInvite{Invite: invite, Token: token, Link: s.inviteLink(token)}
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
		invite, err := s.lookupInvite(ctx, token)
		if err != nil {
			return err
		}
		email, err := s.members.UserEmail(ctx, userID)
		if err != nil {
			return err
		}
		if err := s.usable(invite, email); err != nil {
			return err
		}
		existing, err := s.members.FindMember(ctx, invite.WorkspaceID, userID)
		if err != nil {
			return err
		}
		if existing != nil {
			return ErrAlreadyMember
		}
		now := s.now().UTC()
		if err := s.workspaces.AddMember(ctx, invite.WorkspaceID, userID, invite.Role); err != nil {
			// The user became a member since the lookup above: the answer a later accept gets
			if dbtx.IsUniqueViolation(err) {
				return ErrAlreadyMember
			}
			return err
		}
		// The invite works once: of two accepts that race, the one that finds it used rolls back
		marked, err := s.invites.MarkAccepted(ctx, invite.ID, userID, now)
		if err != nil {
			return err
		}
		if !marked {
			return ErrInviteUsed
		}
		joined = &Membership{
			Workspace: Workspace{ID: invite.WorkspaceID, Name: invite.WorkspaceName},
			Role:      invite.Role,
			JoinedAt:  now,
		}
		if invite.Role == RoleOwner {
			lifted, err = s.liftForNewOwner(ctx, invite.WorkspaceID, userID)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	s.wake(ctx, lifted)
	return joined, nil
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
	var created *Membership
	err := s.transactor.WithTx(ctx, func(ctx context.Context) error {
		// In an open install a user must verify their email before any workspace of theirs runs: one
		// made by a user who has not is suspended, so that making workspaces is no way round it
		suspend := false
		if s.signupOpen {
			verified, err := s.members.EmailVerified(ctx, userID)
			if err != nil {
				return err
			}
			suspend = !verified
		}
		var made *Workspace
		var err error
		if suspend {
			made, err = s.workspaces.CreateSuspended(ctx, strings.TrimSpace(name), SuspendedUnverified)
		} else {
			made, err = s.workspaces.Create(ctx, strings.TrimSpace(name))
		}
		if err != nil {
			return err
		}
		if err := s.workspaces.AddMember(ctx, made.ID, userID, RoleOwner); err != nil {
			return err
		}
		if suspend {
			// The read above may be stale: a verification that committed since found no workspace of
			// the user to lift, and would leave this one suspended for good. Read again with the user's
			// row held, so that a verification either is seen now or waits for this workspace.
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
		}
		created = &Membership{Workspace: *made, Role: RoleOwner, JoinedAt: made.CreatedAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
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
func (s *ServiceImpl) inviteLink(token string) string {
	return strings.TrimRight(s.clientURL, "/") + "/invite/" + token
}
