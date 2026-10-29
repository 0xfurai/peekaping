import { describe, expect, it } from "vitest";
import { AxiosError, AxiosHeaders } from "axios";
import type { AdminWorkspaceResponse } from "@/api/types.gen";
import {
  canDeleteWorkspace,
  contactLink,
  isStatusPageUnavailable,
  isSuspendedByAdmin,
  workspaceActions,
  workspacesPageWithin,
} from "./admin-workspaces";

const workspace = (over: Partial<AdminWorkspaceResponse> = {}): AdminWorkspaceResponse =>
  ({
    id: "w1",
    name: "Main",
    createdAt: "2026-10-01T10:00:00Z",
    owners: ["alice@example.com"],
    memberCount: 2,
    monitors: { active: 3, paused: 1 },
    statusPages: 1,
    customDomains: 0,
    suspended: false,
    suspendedReason: null,
    suspendedAt: null,
    ...over,
  }) as AdminWorkspaceResponse;

describe("workspaceActions", () => {
  it("offers Suspend and Delete for a workspace that runs", () => {
    expect(workspaceActions(workspace())).toEqual(["suspend", "delete"]);
  });

  it("offers Resume, not Suspend, for a workspace an admin suspended", () => {
    const suspended = workspace({ suspended: true, suspendedReason: "admin", suspendedAt: "2026-10-27T10:00:00Z" });
    expect(workspaceActions(suspended)).toEqual(["resume", "delete"]);
  });

  // Resume lifts only an admin's hold: the verification one stays until the owner verifies
  it("offers Suspend, not Resume, for a workspace held for its owner's email verification", () => {
    const held = workspace({ suspended: true, suspendedReason: "unverified", suspendedAt: "2026-10-27T10:00:00Z" });
    expect(workspaceActions(held)).toEqual(["suspend", "delete"]);
  });
});

describe("isSuspendedByAdmin", () => {
  const me = (reason: string | null) => [
    { id: "W1", suspended: reason !== null, suspendedReason: reason },
  ];

  it("is true in a workspace an admin suspended, ignoring the case of the id", () => {
    expect(isSuspendedByAdmin(me("admin"), "w1")).toBe(true);
  });

  it("is false for the verification hold, a running workspace, and another workspace", () => {
    expect(isSuspendedByAdmin(me("unverified"), "w1")).toBe(false);
    expect(isSuspendedByAdmin(me(null), "w1")).toBe(false);
    expect(isSuspendedByAdmin(me("admin"), "w2")).toBe(false);
    expect(isSuspendedByAdmin(me("admin"), null)).toBe(false);
  });
});

describe("canDeleteWorkspace", () => {
  it("is true only when the typed name is the workspace's name exactly", () => {
    expect(canDeleteWorkspace("Main", "Main")).toBe(true);
    expect(canDeleteWorkspace("", "Main")).toBe(false);
    expect(canDeleteWorkspace("main", "Main")).toBe(false);
    expect(canDeleteWorkspace("Mai", "Main")).toBe(false);
    expect(canDeleteWorkspace("Main ", "Main")).toBe(false);
  });
});

describe("contactLink", () => {
  it("makes a mailto link of an email address", () => {
    expect(contactLink("support@example.com")).toEqual({
      text: "support@example.com",
      href: "mailto:support@example.com",
    });
  });

  it("links a web address", () => {
    expect(contactLink("https://example.com/help")).toEqual({
      text: "https://example.com/help",
      href: "https://example.com/help",
    });
  });

  it("shows anything else as text, a script address among it", () => {
    expect(contactLink("Ask Dana in the office")).toEqual({ text: "Ask Dana in the office", href: null });
    expect(contactLink("javascript:alert(1)")).toEqual({ text: "javascript:alert(1)", href: null });
  });

  it("trims the contact", () => {
    expect(contactLink("  support@example.com ")?.href).toBe("mailto:support@example.com");
  });

  it("is null without a contact", () => {
    expect(contactLink(null)).toBeNull();
    expect(contactLink(undefined)).toBeNull();
    expect(contactLink("   ")).toBeNull();
  });
});

describe("isStatusPageUnavailable", () => {
  const answer = (status: number, data: unknown) => {
    const config = { headers: new AxiosHeaders() };
    return new AxiosError("Request failed", "ERR_BAD_RESPONSE", config, null, {
      status,
      statusText: "",
      headers: {},
      config,
      data,
    });
  };

  it("is true for the 503 of a suspended workspace's page", () => {
    const error = answer(503, {
      message: "This status page is unavailable",
      data: null,
      error: { code: "STATUS_PAGE_UNAVAILABLE" },
    });
    expect(isStatusPageUnavailable(error)).toBe(true);
  });

  it("is false for a not-found page, another 503 and anything that is not an answer", () => {
    expect(isStatusPageUnavailable(answer(404, { message: "Status page not found" }))).toBe(false);
    expect(isStatusPageUnavailable(answer(503, { message: "Service Unavailable" }))).toBe(false);
    expect(isStatusPageUnavailable(new Error("network"))).toBe(false);
    expect(isStatusPageUnavailable(null)).toBe(false);
  });
});

describe("workspacesPageWithin", () => {
  it("goes back to the last page when the list shrinks under the page", () => {
    // 21 workspaces, on the second page of 20, and its one workspace deleted
    expect(workspacesPageWithin(1, 20, 20)).toBe(0);
    expect(workspacesPageWithin(4, 45, 20)).toBe(2);
  });

  it("keeps a page that still has workspaces, and the first page of an empty list", () => {
    expect(workspacesPageWithin(1, 21, 20)).toBe(1);
    expect(workspacesPageWithin(0, 0, 20)).toBe(0);
  });
});
