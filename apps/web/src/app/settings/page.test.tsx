import { describe, expect, it, vi } from "vitest";
import { getSettingsKeyByKeyQueryKey, getWorkspaceLimitsQueryKey } from "@/api/@tanstack/react-query.gen";
import type { LimitsReport } from "@/api/types.gen";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import SettingsPage from "./page";

// The timezone card reads the browser's storage, which a static render doesn't have
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

// GET /workspace/limits with the monitors limited, or with no limit at all
const limitsReport = (limited: boolean): LimitsReport => {
  const none = { max: null, used: 0, source: "none" as const };
  return {
    source: limited ? "install" : "none",
    installLimitsName: null,
    limits: {
      monitors: limited ? { max: 20, used: 14, source: "install" } : none,
      min_check_interval: { ...none, used: 20 },
      status_pages: none,
      custom_domains: none,
      members: none,
      owned_workspaces: none,
      probes_per_monitor: none,
      retention_days: { ...none, used: 90 },
    },
  };
};

const settings = (access: Parameters<typeof renderAs>[0], { limited = false } = {}) =>
  renderAs(access, <SettingsPage />, undefined, (queryClient) => {
    queryClient.setQueryData(
      getSettingsKeyByKeyQueryKey({ path: { key: "KEEP_DATA_PERIOD_DAYS" } }),
      { data: { key: "KEEP_DATA_PERIOD_DAYS", type: "int", value: "90" } }
    );
    queryClient.setQueryData(getWorkspaceLimitsQueryKey(), { data: limitsReport(limited), message: "" });
  });

describe("SettingsPage", () => {
  it("hides the usage ping and error report cards from a user who isn't an instance admin", () => {
    const html = settings({ instanceAdmin: false, role: "owner" });
    expect(html).not.toContain(text("settings.telemetry.title"));
    expect(html).not.toContain(text("settings.error_reporting.title"));
    expect(html).not.toContain('id="usage-statistics"');
    expect(html).not.toContain('id="error-reports"');
    // The workspace's own settings are still there
    expect(html).toContain(text("settings.data_retention.title"));
  });

  it("shows the retention period to a member read-only, with a note and no Save button", () => {
    const html = settings({ instanceAdmin: false, role: "member" });
    expect(html).toContain(text("settings.data_retention.title"));
    expect(html).toMatch(/<input[^>]*readOnly=""[^>]*value="90"/);
    expect(html).toContain(text("settings.data_retention.read_only"));
    expect(html).not.toContain(`>${text("common.save")}<`);
  });

  it("lets an admin change the retention period", () => {
    const html = settings({ instanceAdmin: false, role: "admin" });
    expect(html).not.toContain("readOnly");
    expect(html).not.toContain(text("settings.data_retention.read_only"));
    expect(html).toContain(`>${text("common.save")}<`);
  });

  it.each(["member", "owner"] as const)(
    "shows an instance admin who is a %s no usage ping or error report switches: they moved to Admin → Settings",
    (role) => {
      const html = settings({ instanceAdmin: true, role });
      expect(html).not.toContain(text("settings.telemetry.title"));
      expect(html).not.toContain(text("settings.error_reporting.title"));
      expect(html).not.toContain('id="usage-statistics"');
      expect(html).not.toContain('id="error-reports"');
      expect(html).not.toContain('role="switch"');
      expect(html).toContain(text("settings.data_retention.title"));
    }
  );

  it.each(["member", "admin", "owner"] as const)("links a %s to the workspace's members", (role) => {
    const html = settings({ instanceAdmin: false, role });
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/settings/members"`);
    expect(html).toContain(text("members.settings_card.open"));
  });

  it.each(["admin", "owner"] as const)("links an %s of a workspace with limits to its usage, after the members", (role) => {
    const html = settings({ instanceAdmin: false, role }, { limited: true });
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/settings/usage"`);
    expect(html).toContain(text("usage.open"));
    expect(html.indexOf(text("usage.open"))).toBeGreaterThan(html.indexOf(text("members.settings_card.open")));
  });

  it("shows a workspace without limits no usage entry", () => {
    const html = settings({ instanceAdmin: false, role: "owner" }, { limited: false });
    expect(html).not.toContain("/settings/usage");
    expect(html).not.toContain(text("usage.open"));
  });

  it("shows a plain member no usage entry, though the workspace has limits", () => {
    const html = settings({ instanceAdmin: false, role: "member" }, { limited: true });
    expect(html).not.toContain("/settings/usage");
    expect(html).not.toContain(text("usage.open"));
  });

  it.each(["admin", "owner"] as const)("shows an %s the workspace's name in a field, with Save", (role) => {
    const html = settings({ instanceAdmin: false, role });
    expect(html).toContain(text("settings.workspace_name.title"));
    expect(html).toMatch(/<input[^>]*value="Main"/);
  });

  it("shows a member no field to rename the workspace", () => {
    const html = settings({ instanceAdmin: false, role: "member" });
    expect(html).not.toContain(text("settings.workspace_name.title"));
    expect(html).not.toContain('value="Main"');
  });
});
