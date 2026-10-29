import { describe, expect, it } from "vitest";
import {
  assignableRoles,
  canLeave,
  canRemoveMember,
  invitableRoles,
  type MemberRole,
} from "./member-roles";

describe("invitableRoles", () => {
  it.each<[MemberRole | undefined, MemberRole[]]>([
    ["owner", ["member", "admin", "owner"]],
    ["admin", ["member", "admin"]],
    ["member", []],
    [undefined, []],
  ])("for %s offers %j", (actor, expected) => {
    expect(invitableRoles(actor)).toEqual(expected);
  });
});

describe("assignableRoles", () => {
  // actor, target, owners in the workspace, roles offered
  it.each<[MemberRole | undefined, MemberRole, number, MemberRole[]]>([
    ["owner", "member", 1, ["member", "admin", "owner"]],
    ["owner", "admin", 1, ["member", "admin", "owner"]],
    ["owner", "owner", 2, ["member", "admin", "owner"]],
    // The last owner stays an owner, even for an owner
    ["owner", "owner", 1, ["owner"]],
    ["admin", "member", 1, ["member", "admin"]],
    ["admin", "admin", 1, ["member", "admin"]],
    // An admin never touches an owner
    ["admin", "owner", 2, []],
    ["admin", "owner", 1, []],
    ["member", "member", 1, []],
    ["member", "admin", 1, []],
    [undefined, "member", 1, []],
  ])("%s changing a %s among %i owner(s) may give %j", (actor, target, owners, expected) => {
    expect(assignableRoles(actor, target, owners)).toEqual(expected);
  });
});

describe("canRemoveMember", () => {
  it.each<[MemberRole | undefined, MemberRole, number, boolean]>([
    ["owner", "member", 1, true],
    ["owner", "admin", 1, true],
    ["owner", "owner", 2, true],
    ["owner", "owner", 1, false],
    ["admin", "member", 1, true],
    ["admin", "admin", 1, true],
    ["admin", "owner", 2, false],
    ["member", "member", 1, false],
    ["member", "owner", 2, false],
    [undefined, "member", 1, false],
  ])("%s removing a %s among %i owner(s): %s", (actor, target, owners, expected) => {
    expect(canRemoveMember(actor, target, owners)).toBe(expected);
  });
});

describe("canLeave", () => {
  it.each<[MemberRole | undefined, number, boolean]>([
    ["member", 1, true],
    ["admin", 1, true],
    ["owner", 2, true],
    ["owner", 1, false],
    [undefined, 1, false],
  ])("%s among %i owner(s): %s", (role, owners, expected) => {
    expect(canLeave(role, owners)).toBe(expected);
  });
});
