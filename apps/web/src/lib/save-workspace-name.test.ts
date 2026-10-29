import { describe, expect, it, vi } from "vitest";
import type { UtilsApiResponseAuthMeResponse } from "@/api/types.gen";
import { meWithWorkspaceName, saveWorkspaceName } from "./save-workspace-name";

describe("saveWorkspaceName", () => {
  it("renames with the trimmed name, then refreshes /me", async () => {
    const calls: string[] = [];
    const rename = vi.fn(async (name: string) => {
      calls.push("rename");
      return { id: "w1", name };
    });
    const remember = vi.fn(() => void calls.push("remember"));
    const refreshMe = vi.fn(async () => void calls.push("refresh"));

    const result = await saveWorkspaceName({ name: "  Acme  ", current: "My workspace", rename, remember, refreshMe });

    expect(result).toEqual({ kind: "renamed", name: "Acme" });
    expect(rename).toHaveBeenCalledWith("Acme");
    expect(remember).toHaveBeenCalledWith({ id: "w1", name: "Acme" });
    expect(calls).toEqual(["rename", "remember", "refresh"]);
  });

  it("sends nothing when the name is the current one", async () => {
    const rename = vi.fn();
    const remember = vi.fn();
    const refreshMe = vi.fn();

    await expect(
      saveWorkspaceName({ name: "My workspace ", current: "My workspace", rename, remember, refreshMe })
    ).resolves.toEqual({ kind: "unchanged" });

    expect(rename).not.toHaveBeenCalled();
    expect(remember).not.toHaveBeenCalled();
    expect(refreshMe).not.toHaveBeenCalled();
  });

  it("throws the rename's failure and does not refresh", async () => {
    const failure = new Error("403");
    const rename = vi.fn().mockRejectedValue(failure);
    const remember = vi.fn();
    const refreshMe = vi.fn();

    await expect(saveWorkspaceName({ name: "Acme", current: "Main", rename, remember, refreshMe })).rejects.toBe(
      failure
    );
    expect(remember).not.toHaveBeenCalled();
    expect(refreshMe).not.toHaveBeenCalled();
  });

  it("keeps the rename when the refresh fails, and the name it remembered then counts as the current one", async () => {
    let cached = "Main";
    // The name as the rename's answer has it
    const rename = vi.fn(async (name: string) => ({ id: "w1", name }));
    const remember = (workspace: { name: string }) => void (cached = workspace.name);
    const refreshMe = vi.fn().mockRejectedValue(new Error("offline"));
    const log = vi.spyOn(console, "error").mockImplementation(() => {});

    await expect(saveWorkspaceName({ name: "Acme", current: cached, rename, remember, refreshMe })).resolves.toEqual({
      kind: "renamed",
      name: "Acme",
    });
    expect(cached).toBe("Acme");

    // Typing the old name back renames again
    await expect(saveWorkspaceName({ name: "Main", current: cached, rename, remember, refreshMe })).resolves.toEqual({
      kind: "renamed",
      name: "Main",
    });
    expect(rename).toHaveBeenLastCalledWith("Main");
    log.mockRestore();
  });
});

describe("meWithWorkspaceName", () => {
  const me: UtilsApiResponseAuthMeResponse = {
    message: "OK",
    data: {
      accountEmails: false,
      defaultWorkspaceId: "w1",
      disabledMonitorTypes: [],
      minCheckInterval: 20,
      supportContact: null,
      user: { id: "u1", email: "user@example.com", emailVerified: true, instanceAdmin: false },
      workspaces: [
        { id: "w1", name: "Main", role: "owner", suspended: false, suspendedReason: null },
        { id: "w2", name: "Other", role: "member", suspended: false, suspendedReason: null },
      ],
    },
  };

  it("renames the one workspace in /me", () => {
    const next = meWithWorkspaceName(me, { id: "W1", name: "Acme" });
    expect(next?.data.workspaces?.map((w) => w.name)).toEqual(["Acme", "Other"]);
    expect(me.data.workspaces?.[0].name).toBe("Main");
  });

  it("leaves a /me that isn't loaded alone", () => {
    expect(meWithWorkspaceName(undefined, { id: "w1", name: "Acme" })).toBeUndefined();
  });
});
