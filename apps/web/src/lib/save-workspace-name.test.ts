import { describe, expect, it, vi } from "vitest";
import { saveWorkspaceName } from "./save-workspace-name";

describe("saveWorkspaceName", () => {
  it("renames with the trimmed name, then refreshes /me", async () => {
    const calls: string[] = [];
    const rename = vi.fn(async () => void calls.push("rename"));
    const refreshMe = vi.fn(async () => void calls.push("refresh"));

    const result = await saveWorkspaceName({ name: "  Acme  ", current: "My workspace", rename, refreshMe });

    expect(result).toEqual({ kind: "renamed", name: "Acme" });
    expect(rename).toHaveBeenCalledWith("Acme");
    expect(calls).toEqual(["rename", "refresh"]);
  });

  it("sends nothing when the name is the current one", async () => {
    const rename = vi.fn();
    const refreshMe = vi.fn();

    await expect(
      saveWorkspaceName({ name: "My workspace ", current: "My workspace", rename, refreshMe })
    ).resolves.toEqual({ kind: "unchanged" });

    expect(rename).not.toHaveBeenCalled();
    expect(refreshMe).not.toHaveBeenCalled();
  });

  it("throws the rename's failure and does not refresh", async () => {
    const failure = new Error("403");
    const rename = vi.fn().mockRejectedValue(failure);
    const refreshMe = vi.fn();

    await expect(saveWorkspaceName({ name: "Acme", current: "Main", rename, refreshMe })).rejects.toBe(failure);
    expect(refreshMe).not.toHaveBeenCalled();
  });

  it("keeps the rename when the refresh fails", async () => {
    const rename = vi.fn().mockResolvedValue(undefined);
    const refreshMe = vi.fn().mockRejectedValue(new Error("offline"));
    const log = vi.spyOn(console, "error").mockImplementation(() => {});

    await expect(saveWorkspaceName({ name: "Acme", current: "Main", rename, refreshMe })).resolves.toEqual({
      kind: "renamed",
      name: "Acme",
    });
    log.mockRestore();
  });
});
