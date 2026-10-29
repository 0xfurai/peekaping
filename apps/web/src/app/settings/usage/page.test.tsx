import { describe, expect, it, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { getWorkspaceLimitsQueryKey } from "@/api/@tanstack/react-query.gen";
import type { LimitsReport } from "@/api/types.gen";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import UsagePage from "./page";

// A static render runs no effects, so a redirect would show nothing. Show where it goes.
vi.mock("react-router-dom", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router-dom")>()),
  Navigate: ({ to }: { to: string }) => <span data-redirect={to} />,
}));

type Use = LimitsReport["limits"]["monitors"];
const none: Use = { max: null, used: 0, source: "none" };
const SETTINGS = `/w/${WORKSPACE_ID}/settings`;

// GET /workspace/limits as loaded: 14 of 20 monitors, 4 of 5 members, 2 of 2 status pages, a
// shortest interval of 60 seconds and a retention of at most 30 days
const LIMITED: LimitsReport = {
  source: "install",
  installLimitsName: "Free tier",
  limits: {
    monitors: { max: 20, used: 14, source: "install" },
    min_check_interval: { max: 60, used: 60, source: "install" },
    status_pages: { max: 2, used: 2, source: "install" },
    custom_domains: none,
    members: { max: 5, used: 4, source: "install" },
    owned_workspaces: none,
    probes_per_monitor: none,
    retention_days: { max: 30, used: 30, source: "install" },
  },
};

const UNLIMITED: LimitsReport = {
  source: "none",
  installLimitsName: null,
  limits: {
    monitors: { ...none, used: 3 },
    min_check_interval: { max: null, used: 20, source: "none" },
    status_pages: none,
    custom_domains: none,
    members: { ...none, used: 1 },
    owned_workspaces: { ...none, used: 1 },
    probes_per_monitor: none,
    retention_days: { max: null, used: 365, source: "none" },
  },
};

const seed = (report: LimitsReport) => (queryClient: QueryClient) =>
  queryClient.setQueryData(getWorkspaceLimitsQueryKey(), { data: report, message: "" });

const usage = (role: "member" | "admin" | "owner", report: LimitsReport = LIMITED) =>
  renderAs({ instanceAdmin: false, role }, <UsagePage />, `${SETTINGS}/usage`, seed(report));

// The row of a limit, from its label up to the next row
const row = (html: string, name: string) => {
  const start = html.indexOf(`data-limit="${name}"`);
  const end = html.indexOf("data-limit=", start + 1);
  return start === -1 ? "" : html.slice(start, end === -1 ? undefined : end);
};

describe("UsagePage", () => {
  it.each(["owner", "admin"] as const)("lists each limit with its usage to an %s", (role) => {
    const html = usage(role);
    expect(html).toContain(text("usage.title"));
    expect(row(html, "monitors")).toContain(text("limits.names.monitors"));
    expect(row(html, "monitors")).toContain("14 of 20 monitors");
    expect(row(html, "members")).toContain("4 of 5 members");
    expect(row(html, "status_pages")).toContain("2 of 2 status pages");
    // A limit that isn't set has no row
    expect(row(html, "custom_domains")).toBe("");
    expect(row(html, "owned_workspaces")).toBe("");
  });

  it("draws a bar for each count, which warns at 80% and more, and more strongly at the limit", () => {
    const html = usage("owner");
    expect(row(html, "monitors")).toMatch(/role="progressbar"[^>]*aria-valuenow="14"[^>]*aria-valuemax="20"/);
    expect(row(html, "monitors")).toContain('data-level="ok"');
    expect(row(html, "monitors")).not.toContain(text("usage.near"));
    expect(row(html, "monitors")).not.toContain(text("usage.reached"));

    expect(row(html, "members")).toContain('data-level="near"');
    expect(row(html, "members")).toContain(text("usage.near"));

    expect(row(html, "status_pages")).toContain('data-level="reached"');
    expect(row(html, "status_pages")).toContain(text("usage.reached"));
  });

  it("keeps the bar's value at its maximum when the usage is above a lowered limit", () => {
    const html = usage("owner", {
      ...LIMITED,
      limits: { ...LIMITED.limits, monitors: { max: 20, used: 25, source: "install" } },
    });
    expect(row(html, "monitors")).toContain("25 of 20 monitors");
    expect(row(html, "monitors")).toMatch(
      /role="progressbar"[^>]*aria-valuenow="20"[^>]*aria-valuemax="20"[^>]*aria-valuetext="25 of 20 monitors"/
    );
    expect(row(html, "monitors")).toContain('data-level="reached"');
  });

  it("shows the shortest interval and the retention as values, with no bar", () => {
    const html = usage("owner");
    expect(row(html, "min_check_interval")).toContain(text("usage.labels.min_check_interval"));
    expect(row(html, "min_check_interval")).toContain("60 seconds");
    expect(row(html, "min_check_interval")).not.toContain("progressbar");
    expect(row(html, "retention_days")).toContain(text("usage.labels.retention_days"));
    expect(row(html, "retention_days")).toContain("30 days");
    expect(row(html, "retention_days")).not.toContain("progressbar");
  });

  it("says where the limits come from: the install's name for its defaults", () => {
    const html = usage("owner");
    expect(html).toContain(text("usage.plan"));
    expect(html).toContain("Free tier");
    expect(html).not.toContain(text("usage.set_by_admin"));
  });

  it("says the administrator set the limits when the install's defaults have no name", () => {
    const html = usage("owner", { ...LIMITED, installLimitsName: null });
    expect(html).toContain(text("usage.set_by_admin"));
  });

  it("says the administrator set the workspace's own limits", () => {
    const html = usage("owner", { ...LIMITED, source: "workspace" });
    expect(html).toContain(text("usage.set_by_admin"));
    expect(html).not.toContain("Free tier");
  });

  it("offers no upgrade and no checkout", () => {
    const html = usage("owner").toLowerCase();
    expect(html).not.toContain("upgrade");
    expect(html).not.toContain("checkout");
    expect(html).not.toContain("billing");
  });

  it("sends a workspace without limits to its settings", () => {
    const html = usage("owner", UNLIMITED);
    expect(html).toContain(`data-redirect="${SETTINGS}"`);
    expect(html).not.toContain(text("usage.title"));
  });

  it("sends a plain member to the settings", () => {
    const html = usage("member");
    expect(html).toContain(`data-redirect="${SETTINGS}"`);
    expect(html).not.toContain("14 of 20");
  });
});
