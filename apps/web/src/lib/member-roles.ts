// What a member may do to the people of their workspace, mirroring the server's rules
// (apps/server/internal/modules/workspace/workspace.roles.go, docs/adr/0005). The server decides
// and answers 403 or 409; these only keep the page from offering what it would refuse.

export type MemberRole = "member" | "admin" | "owner";

export const ROLES: readonly MemberRole[] = ["member", "admin", "owner"];

// Whether a member holding the actor role may give the target member the new role, or remove
// them when newRole is undefined. owners is how many owners the workspace has now.
const mayChange = (
  actor: MemberRole | undefined,
  target: MemberRole,
  newRole: MemberRole | undefined,
  owners: number
): boolean => {
  if (actor !== "admin" && actor !== "owner") return false;
  const touchesOwner = target === "owner" || newRole === "owner";
  if (touchesOwner && actor !== "owner") return false;
  if (target === "owner" && newRole !== "owner" && owners <= 1) return false;
  return true;
};

// The roles an invite can be made for: an admin or owner invites, and only an owner invites an owner
export const invitableRoles = (actor: MemberRole | undefined): MemberRole[] =>
  actor === "owner"
    ? ["member", "admin", "owner"]
    : actor === "admin"
      ? ["member", "admin"]
      : [];

// The roles the actor may give the target member, in the order of ROLES. It always holds the
// target's own role when the actor may change them at all, and is empty when they may not.
export const assignableRoles = (
  actor: MemberRole | undefined,
  target: MemberRole,
  owners: number
): MemberRole[] =>
  mayChange(actor, target, target, owners)
    ? ROLES.filter((role) => role === target || mayChange(actor, target, role, owners))
    : [];

export const canRemoveMember = (
  actor: MemberRole | undefined,
  target: MemberRole,
  owners: number
): boolean => mayChange(actor, target, undefined, owners);

// Anyone may leave their workspace unless they are its last owner
export const canLeave = (role: MemberRole | undefined, owners: number): boolean =>
  role !== undefined && !(role === "owner" && owners <= 1);
