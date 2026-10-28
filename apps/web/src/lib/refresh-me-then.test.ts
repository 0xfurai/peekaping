import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { refreshMeThen } from "./refresh-me-then";

describe("refreshMeThen", () => {
  beforeEach(() => vi.spyOn(console, "error").mockImplementation(() => {}));
  afterEach(() => vi.restoreAllMocks());

  it("continues once /me is refreshed", async () => {
    const refresh = vi.fn().mockResolvedValue({});
    const proceed = vi.fn();
    const onFailed = vi.fn();

    await refreshMeThen({ refresh, proceed, onFailed });

    expect(proceed).toHaveBeenCalledTimes(1);
    expect(onFailed).not.toHaveBeenCalled();
  });

  it("does not continue when /me fails, and hands over a retry that refreshes and then continues", async () => {
    const refresh = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValue({});
    const proceed = vi.fn();
    const onFailed = vi.fn();

    await refreshMeThen({ refresh, proceed, onFailed });

    expect(proceed).not.toHaveBeenCalled();
    expect(onFailed).toHaveBeenCalledTimes(1);

    onFailed.mock.calls[0][0]();
    await vi.waitFor(() => expect(proceed).toHaveBeenCalledTimes(1));
    expect(refresh).toHaveBeenCalledTimes(2);
    expect(onFailed).toHaveBeenCalledTimes(1);
  });

  it("offers another retry when the retry fails too", async () => {
    const refresh = vi.fn().mockRejectedValue(new Error("offline"));
    const proceed = vi.fn();
    const onFailed = vi.fn();

    await refreshMeThen({ refresh, proceed, onFailed });
    onFailed.mock.calls[0][0]();
    await vi.waitFor(() => expect(onFailed).toHaveBeenCalledTimes(2));

    expect(proceed).not.toHaveBeenCalled();
  });
});
