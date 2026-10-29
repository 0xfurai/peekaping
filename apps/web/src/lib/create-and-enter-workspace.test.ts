import { describe, expect, it, vi } from "vitest";
import { createAndEnterWorkspace } from "./create-and-enter-workspace";

const ID = "7f3e1a90-2b44-4c6d-8e15-a9d0c5b7e321";

describe("createAndEnterWorkspace", () => {
  it("makes the workspace, then enters it", async () => {
    const create = vi.fn().mockResolvedValue(ID);
    const enter = vi.fn().mockResolvedValue(undefined);

    await expect(createAndEnterWorkspace({ createdId: null, create, enter })).resolves.toEqual({
      kind: "entered",
    });

    expect(create).toHaveBeenCalledTimes(1);
    expect(enter).toHaveBeenCalledWith(ID, "/get-started");
  });

  it("does not enter when the create failed", async () => {
    const create = vi.fn().mockRejectedValue(new Error("boom"));
    const enter = vi.fn();

    await expect(createAndEnterWorkspace({ createdId: null, create, enter })).resolves.toEqual({
      kind: "create_failed",
    });

    expect(enter).not.toHaveBeenCalled();
  });

  it("names the created workspace when entering it failed, and a retry enters it without making a second", async () => {
    const create = vi.fn().mockResolvedValue(ID);
    const failure = new Error("/me failed");
    const enter = vi.fn().mockRejectedValueOnce(failure).mockResolvedValue(undefined);

    const first = await createAndEnterWorkspace({ createdId: null, create, enter });
    expect(first).toEqual({ kind: "enter_failed", workspaceId: ID, error: failure });

    const retry = await createAndEnterWorkspace({
      createdId: first.kind === "enter_failed" ? first.workspaceId : null,
      create,
      enter,
    });

    expect(retry).toEqual({ kind: "entered" });
    expect(create).toHaveBeenCalledTimes(1);
    expect(enter).toHaveBeenCalledTimes(2);
    expect(enter).toHaveBeenLastCalledWith(ID, "/get-started");
  });

  it("can fail to enter again on the retry and still names the same workspace", async () => {
    const create = vi.fn();
    const enter = vi.fn().mockRejectedValue(new Error("still down"));

    const result = await createAndEnterWorkspace({ createdId: ID, create, enter });

    expect(result).toMatchObject({ kind: "enter_failed", workspaceId: ID });
    expect(create).not.toHaveBeenCalled();
  });
});
