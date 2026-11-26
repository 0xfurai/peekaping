import { describe, expect, it, vi } from "vitest";
import { acceptInviteAndEnter } from "./accept-invite-and-enter";

const ID = "7f3e1a90-2b44-4c6d-8e15-a9d0c5b7e321";

describe("acceptInviteAndEnter", () => {
  it("accepts the invite, then enters the workspace it joined", async () => {
    const accept = vi.fn().mockResolvedValue(ID);
    const enter = vi.fn().mockResolvedValue(undefined);

    await expect(acceptInviteAndEnter({ acceptedId: null, accept, enter })).resolves.toEqual({
      kind: "entered",
    });

    expect(accept).toHaveBeenCalledTimes(1);
    expect(enter).toHaveBeenCalledWith(ID);
  });

  it("does not enter when accepting failed, and gives the error", async () => {
    const failure = new Error("410");
    const accept = vi.fn().mockRejectedValue(failure);
    const enter = vi.fn();

    await expect(acceptInviteAndEnter({ acceptedId: null, accept, enter })).resolves.toEqual({
      kind: "accept_failed",
      error: failure,
    });

    expect(enter).not.toHaveBeenCalled();
  });

  it("keeps the joined workspace's id when entering failed, and a retry enters that workspace without sending the single-use invite again", async () => {
    const accept = vi.fn().mockResolvedValue(ID);
    const failure = new Error("/me failed");
    const enter = vi.fn().mockRejectedValueOnce(failure).mockResolvedValue(undefined);

    const first = await acceptInviteAndEnter({ acceptedId: null, accept, enter });
    expect(first).toEqual({ kind: "enter_failed", workspaceId: ID, error: failure });

    const retry = await acceptInviteAndEnter({
      acceptedId: first.kind === "enter_failed" ? first.workspaceId : null,
      accept,
      enter,
    });

    expect(retry).toEqual({ kind: "entered" });
    expect(accept).toHaveBeenCalledTimes(1);
    expect(enter).toHaveBeenNthCalledWith(2, ID);
  });
});
