import { describe, expect, it } from "vitest";
import { AxiosError, AxiosHeaders } from "axios";
import type { AdminUserResponse } from "@/api/types.gen";
import {
  adminErrorMessage,
  isLastInstanceAdmin,
  userActions,
} from "./admin-users";

const user = (over: Partial<AdminUserResponse> = {}): AdminUserResponse => ({
  id: "u2",
  email: "bob@example.com",
  active: true,
  instanceAdmin: false,
  twoFactorEnabled: true,
  emailVerified: true,
  createdAt: "2026-10-01T10:00:00Z",
  lastLoginAt: "2026-10-27T10:00:00Z",
  workspaces: [],
  ...over,
});

describe("userActions", () => {
  it("offers another active user with 2FA every action, with the flag to give", () => {
    expect(userActions(user(), { self: false, onlyInstanceAdmin: false })).toEqual([
      "grantAdmin",
      "disable",
      "disableTwoFactor",
      "resetLink",
      "delete",
    ]);
  });

  it("offers to take the flag from another instance admin", () => {
    expect(
      userActions(user({ instanceAdmin: true }), { self: false, onlyInstanceAdmin: false })
    ).toContain("revokeAdmin");
    expect(
      userActions(user({ instanceAdmin: true }), { self: false, onlyInstanceAdmin: false })
    ).not.toContain("grantAdmin");
  });

  it("leaves out turning off 2FA for a user who has none", () => {
    const actions = userActions(user({ twoFactorEnabled: false }), {
      self: false,
      onlyInstanceAdmin: false,
    });
    expect(actions).not.toContain("disableTwoFactor");
    expect(actions).toContain("resetLink");
  });

  it("offers enabling, not disabling, an inactive user", () => {
    const actions = userActions(user({ active: false }), { self: false, onlyInstanceAdmin: false });
    expect(actions).toContain("enable");
    expect(actions).not.toContain("disable");
  });

  it("gives your own row no disable and no delete, whatever else it has", () => {
    const actions = userActions(user({ instanceAdmin: true }), {
      self: true,
      onlyInstanceAdmin: false,
    });
    expect(actions).toEqual(["revokeAdmin", "disableTwoFactor", "resetLink"]);
  });

  it("gives your own row no way to take the flag while you are the only instance admin", () => {
    const actions = userActions(user({ instanceAdmin: true }), {
      self: true,
      onlyInstanceAdmin: true,
    });
    expect(actions).not.toContain("revokeAdmin");
    expect(actions).not.toContain("grantAdmin");
  });
});

describe("isLastInstanceAdmin", () => {
  const alice = user({ id: "u1", instanceAdmin: true });
  const bob = user({ id: "u2" });
  const carol = user({ id: "u3", instanceAdmin: true, active: false });

  it("is true when the whole list has no other active instance admin", () => {
    expect(isLastInstanceAdmin(alice, [alice, bob, carol], true)).toBe(true);
  });

  it("is false when another active instance admin is listed", () => {
    const dave = user({ id: "u4", instanceAdmin: true });
    expect(isLastInstanceAdmin(alice, [alice, bob, dave], true)).toBe(false);
  });

  it("is false when only part of the list is on screen, since the rest may hold another", () => {
    expect(isLastInstanceAdmin(alice, [alice, bob], false)).toBe(false);
  });
});

const refusalWith = (status: number, data: unknown) => {
  const config = { headers: new AxiosHeaders() };
  return new AxiosError("Request failed", "ERR_BAD_REQUEST", config, null, {
    status,
    statusText: "",
    headers: {},
    config,
    data,
  });
};

const refusal = (status: number, message: string) => refusalWith(status, { data: null, message });

const t = (key: string, vars?: Record<string, unknown>) =>
  vars ? `${key} ${JSON.stringify(vars)}` : key;

describe("adminErrorMessage", () => {
  it("names the workspaces that block a delete, from the list in the body", () => {
    const error = refusalWith(409, {
      error: {
        code: "LAST_OWNER",
        workspaces: [
          { id: "w1", name: "Main" },
          { id: "w2", name: 'Side, "project". Make someone else an owner' },
        ],
      },
    });
    expect(adminErrorMessage(error, t, "fallback")).toBe(
      'admin.users.errors.last_owner {"workspaces":"\\"Main\\", \\"Side, \\"project\\". Make someone else an owner\\""}'
    );
  });

  it("does not read workspaces out of a message", () => {
    const error = refusal(
      409,
      'The user is the last owner of: "Main". Make someone else an owner, or delete the workspace, first'
    );
    expect(adminErrorMessage(error, t, "fallback")).toBe(
      'The user is the last owner of: "Main". Make someone else an owner, or delete the workspace, first'
    );
  });

  it("translates the refusals about yourself and the last instance admin", () => {
    expect(
      adminErrorMessage(refusal(409, "You can't do this to your own account"), t, "fallback")
    ).toBe("admin.users.errors.self");
    expect(
      adminErrorMessage(refusal(409, "The install needs at least one instance admin"), t, "fallback")
    ).toBe("admin.users.errors.last_instance_admin");
  });

  it("shows the server's own message for an answer it doesn't know, and the fallback for none", () => {
    expect(adminErrorMessage(refusal(500, "Internal server error"), t, "fallback")).toBe(
      "Internal server error"
    );
    expect(adminErrorMessage(new Error("network"), t, "fallback")).toBe("fallback");
  });
});
