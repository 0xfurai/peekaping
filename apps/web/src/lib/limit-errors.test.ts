import { describe, expect, it } from "vitest";
import { AxiosError } from "axios";
import i18n, { loadLanguage } from "@/i18n";
import { limitReached, limitReachedMessage } from "./limit-errors";
import { workspaceErrorMessage } from "./workspace-errors";

const t = i18n.t.bind(i18n) as unknown as (key: string, vars?: Record<string, unknown>) => string;

const refused = (status: number, data: unknown) =>
  new AxiosError(`Request failed with status code ${status}`, "ERR_BAD_REQUEST", undefined, undefined, {
    status,
    statusText: "Forbidden",
    headers: {},
    config: {} as never,
    data,
  });

// The server's 403 for a write over a limit (limits.ReachedResponse)
const reached = (limit: string, max: number) =>
  refused(403, {
    message: `the workspace has reached its limit of ${max} ${limit}`,
    data: null,
    error: { code: "LIMIT_REACHED", limit, max },
  });

describe("limitReachedMessage", () => {
  it.each([
    ["monitors", 10, "This workspace can have at most 10 monitors running"],
    ["status_pages", 2, "This workspace can have at most 2 status pages"],
    ["custom_domains", 3, "This workspace's status pages can have at most 3 custom domains together"],
    ["members", 5, "This workspace can have at most 5 members, pending invites included"],
    ["owned_workspaces", 1, "You can own at most 1 workspaces"],
    ["probes", 2, "A monitor in this workspace can run on at most 2 probes"],
    ["retention_days", 30, "This workspace can keep data for at most 30 days"],
  ])("words the %s limit with its value", (limit, max, message) => {
    expect(limitReachedMessage(reached(limit, max), t)).toBe(message);
  });

  it("is the translation of the current language", async () => {
    await loadLanguage("de-DE");
    await i18n.changeLanguage("de-DE");
    try {
      const message = limitReachedMessage(reached("monitors", 7), t);
      expect(message).toContain("7");
      expect(message).not.toBe("This workspace can have at most 7 monitors running");
    } finally {
      await i18n.changeLanguage("en-US");
    }
  });

  it("is nothing for a limit it doesn't know, or an answer that is no LIMIT_REACHED", () => {
    expect(limitReachedMessage(reached("robots", 1), t)).toBeUndefined();
    expect(limitReachedMessage(refused(403, { message: "Forbidden", error: { code: "FORBIDDEN" } }), t)).toBeUndefined();
    expect(limitReachedMessage(refused(403, { error: { code: "LIMIT_REACHED", limit: "monitors" } }), t)).toBeUndefined();
    expect(limitReachedMessage(new Error("boom"), t)).toBeUndefined();
  });
});

describe("limitReached", () => {
  it("reads the limit and its value", () => {
    expect(limitReached(reached("members", 4))).toEqual({ limit: "members", max: 4 });
    expect(limitReached(refused(400, { message: "bad" }))).toBeUndefined();
  });
});

describe("workspaceErrorMessage", () => {
  it("words a limit before the server's message", () => {
    expect(workspaceErrorMessage(reached("members", 3), t, "fallback")).toBe(
      "This workspace can have at most 3 members, pending invites included"
    );
    expect(workspaceErrorMessage(reached("owned_workspaces", 2), t, "fallback")).toBe("You can own at most 2 workspaces");
  });
});
