import { describe, expect, it } from "vitest";
import { workspaceErrorKey } from "./workspace-errors";

describe("workspaceErrorKey", () => {
  it.each<[number, string, string | undefined]>([
    [409, "A workspace needs at least one owner", "members.errors.last_owner"],
    [409, "Already a member of this workspace", "members.errors.already_member"],
    [409, "An account with this email exists: sign in and accept the invite", "members.errors.account_exists"],
    [410, "This invite can't be used: it was already used", "invite.unavailable.accepted"],
    [410, "This invite can't be used: it has expired", "invite.unavailable.expired"],
    [410, "This invite can't be used: it was revoked", "invite.unavailable.revoked"],
    [410, "This invite can't be used", "invite.unavailable.unknown"],
    [403, "This invite is for another email address", "invite.errors.other_email"],
    [403, "Your role in this workspace doesn't allow this", "members.errors.forbidden"],
    [404, "Invite not found", "invite.unavailable.not_found"],
    [404, "Member not found", "members.errors.member_not_found"],
    [500, "boom", undefined],
    [409, "something else", undefined],
  ])("%i %j is %s", (status, message, key) => {
    expect(workspaceErrorKey(status, message)).toBe(key);
  });
});
