import { describe, expect, it } from "vitest";
import { AxiosError, AxiosHeaders } from "axios";
import type { AdminWorkspaceLimitsResponse, AdminWorkspaceResponse } from "@/api/types.gen";
import {
  canDeleteWorkspace,
  contactLink,
  isStatusPageUnavailable,
  isSuspendedByAdmin,
  limitsBody,
  limitsDraft,
  limitsSummary,
  workspaceActions,
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
  it("offers Limits, Suspend and Delete for a workspace that runs", () => {
    expect(workspaceActions(workspace())).toEqual(["limits", "suspend", "delete"]);
  });

  it("offers Resume, not Suspend, for a workspace an admin suspended", () => {
    const suspended = workspace({ suspended: true, suspendedReason: "admin", suspendedAt: "2026-10-27T10:00:00Z" });
    expect(workspaceActions(suspended)).toEqual(["limits", "resume", "delete"]);
  });

  // Resume lifts only an admin's hold: the verification one stays until the owner verifies
  it("offers only Suspend for a workspace held for its owner's email verification", () => {
    const held = workspace({ suspended: true, suspendedReason: "unverified", suspendedAt: "2026-10-27T10:00:00Z" });
    expect(workspaceActions(held)).toEqual(["limits", "suspend", "delete"]);
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

const NO_VALUES: AdminWorkspaceLimitsResponse["values"] = {
  monitors: null,
  min_check_interval: null,
  status_pages: null,
  custom_domains: null,
  members: null,
  owned_workspaces: null,
  probes_per_monitor: null,
  retention_days: null,
};

const limits = (
  source: AdminWorkspaceLimitsResponse["source"],
  values: Partial<AdminWorkspaceLimitsResponse["values"]> = {}
): AdminWorkspaceLimitsResponse => ({ source, values: { ...NO_VALUES, ...values } });

describe("limitsSummary", () => {
  it("shows the first two limits that have a value, and how many more there are", () => {
    expect(limitsSummary(limits("install", { members: 5, monitors: 10, retention_days: 30, status_pages: 1 }))).toEqual({
      shown: [
        { name: "monitors", value: 10 },
        { name: "status_pages", value: 1 },
      ],
      more: 2,
    });
  });

  it("shows nothing for a workspace without limits, and leaves out the owned workspaces", () => {
    expect(limitsSummary(limits("none"))).toEqual({ shown: [], more: 0 });
    expect(limitsSummary(limits("install", { owned_workspaces: 3 }))).toEqual({ shown: [], more: 0 });
  });
});

describe("limitsDraft", () => {
  it("starts from a workspace's own limits", () => {
    const draft = limitsDraft(limits("workspace", { monitors: 50, members: 10 }));
    expect(draft.choice).toBe("own");
    expect(draft.values.monitors).toBe("50");
    expect(draft.values.members).toBe("10");
    expect(draft.values.status_pages).toBe("");
  });

  it("starts from No limits for a workspace whose own limits are none", () => {
    expect(limitsDraft(limits("workspace")).choice).toBe("unlimited");
  });

  it("starts from the install's defaults, with their values to start its own from", () => {
    const draft = limitsDraft(limits("install", { monitors: 10 }));
    expect(draft.choice).toBe("defaults");
    expect(draft.values.monitors).toBe("10");
    expect(limitsDraft(limits("none")).choice).toBe("defaults");
  });
});

describe("limitsBody", () => {
  const values = limitsDraft(limits("none")).values;

  it("sets its own limits, leaving a blank one out, which is no limit", () => {
    expect(
      limitsBody({ choice: "own", values: { ...values, monitors: " 25 ", min_check_interval: "300", members: "" } })
    ).toEqual({ body: { limits: { monitors: 25, min_check_interval: 300 } } });
  });

  it("sets no limits at all", () => {
    expect(limitsBody({ choice: "unlimited", values: { ...values, monitors: "25" } })).toEqual({ body: { limits: {} } });
  });

  it("clears its own limits back to the install's defaults", () => {
    expect(limitsBody({ choice: "defaults", values: { ...values, monitors: "25" } })).toEqual({
      body: { clearLimits: true },
    });
  });

  it("names a limit that is no whole number of 1 or more, and sends nothing", () => {
    expect(
      limitsBody({ choice: "own", values: { ...values, monitors: "0", members: "2.5", status_pages: "-1", probes_per_monitor: "x" } })
    ).toEqual({ invalid: ["monitors", "status_pages", "members", "probes_per_monitor"] });
    // Only its own limits are checked: the other choices send none
    expect(limitsBody({ choice: "unlimited", values: { ...values, monitors: "0" } })).toEqual({ body: { limits: {} } });
  });
});
