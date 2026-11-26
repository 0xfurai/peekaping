import { describe, expect, it, vi } from "vitest";
import { enterWorkspace } from "./enter-workspace";

const ID = "7f3e1a90-2b44-4c6d-8e15-a9d0c5b7e321";

describe("enterWorkspace", () => {
  it("refreshes /me, then opens the workspace's monitors", async () => {
    const calls: string[] = [];
    const refreshMe = vi.fn(async () => void calls.push("refresh"));
    const navigate = vi.fn((to: string) => void calls.push(`go ${to}`));

    await enterWorkspace({ workspaceId: ID, refreshMe, navigate });

    expect(calls).toEqual(["refresh", `go /w/${ID}/monitors`]);
  });

  it("opens the page it is given", async () => {
    const navigate = vi.fn();
    await enterWorkspace({ workspaceId: ID, path: "/get-started", refreshMe: async () => {}, navigate });
    expect(navigate).toHaveBeenCalledWith(`/w/${ID}/get-started`);
  });

  it("goes nowhere when /me can't be refreshed", async () => {
    const navigate = vi.fn();
    await expect(
      enterWorkspace({ workspaceId: ID, refreshMe: () => Promise.reject(new Error("offline")), navigate })
    ).rejects.toThrow("offline");
    expect(navigate).not.toHaveBeenCalled();
  });
});
