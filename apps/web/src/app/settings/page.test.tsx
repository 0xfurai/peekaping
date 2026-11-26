import { describe, expect, it, vi } from "vitest";
import { getSettingsKeyByKeyQueryKey, getWorkspaceLimitsQueryKey } from "@/api/@tanstack/react-query.gen";
import type { LimitsReport } from "@/api/types.gen";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import SettingsPage from "./page";

// The timezone card reads the browser's storage, which a static render doesn't have
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

type Limits = {
  // the monitors limited, or no limit
  limited?: boolean;
  // the workspace's retention_days limit, null for none
  retentionCap?: number | null;
  // the limits not loaded yet
  loading?: boolean;
};

// GET /workspace/limits with the monitors limited or not, and the workspace's retention_days limit
const limitsReport = ({ limited = false, retentionCap = null }: Limits): LimitsReport => {
  const none = { max: null, used: 0, source: "none" as const };
  return {
    source: limited || retentionCap !== null ? "install" : "none",
    installLimitsName: null,
    limits: {
      monitors: limited ? { max: 20, used: 14, source: "install" } : none,
      min_check_interval: { ...none, used: 20 },
      status_pages: none,
      custom_domains: none,
      members: none,
      owned_workspaces: none,
      probes_per_monitor: none,
      retention_days:
        retentionCap === null ? { ...none, used: 90 } : { max: retentionCap, used: 90, source: "install" },
    },
  };
};

const settings = (access: Parameters<typeof renderAs>[0], limits: Limits = {}) =>
  renderAs(access, <SettingsPage />, undefined, (queryClient) => {
    queryClient.setQueryData(
      getSettingsKeyByKeyQueryKey({ path: { key: "KEEP_DATA_PERIOD_DAYS" } }),
      { data: { key: "KEEP_DATA_PERIOD_DAYS", type: "int", value: "90" } }
    );
    if (limits.loading) return;
    queryClient.setQueryData(getWorkspaceLimitsQueryKey(), { data: limitsReport(limits), message: "" });
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

  // PP-161: the workspace's plan can cap the period
  it("shows an admin the cap next to the field, and takes it as the field's maximum", () => {
    const html = settings({ instanceAdmin: false, role: "admin" }, { retentionCap: 120 });
    expect(html).toContain("Up to 120 days on this workspace");
    expect(html).toMatch(/<input[^>]*max="120"/);
  });

  it("shows a member the cap beside the read-only period", () => {
    const html = settings({ instanceAdmin: false, role: "member" }, { retentionCap: 120 });
    expect(html).toContain("Up to 120 days on this workspace");
  });

  it.each([
    ["without a cap", {}],
    ["while the limits load", { loading: true }],
  ])("shows no cap %s, and keeps the 100 year maximum", (_, limits) => {
    const html = settings({ instanceAdmin: false, role: "admin" }, limits);
    expect(html).not.toContain("on this workspace");
    expect(html).toMatch(/<input[^>]*max="36500"/);
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

  // PP-170: the workspace export and import are for an owner, above the danger zone
  it("shows an owner the export, with its secrets warning and both downloads, and the import, above the danger zone", () => {
    const html = settings({ instanceAdmin: false, role: "owner" });
    expect(html).toContain(text("settings.workspace_export.warning_title"));
    expect(html).toContain(text("settings.workspace_export.download_workspace"));
    expect(html).toContain(text("settings.workspace_export.download_heartbeats"));
    expect(html).toContain(text("settings.workspace_import.title"));
    expect(html).toMatch(/<input[^>]*type="file"[^>]*accept="\.json,application\/json"/);
    const danger = html.indexOf(text("settings.danger_zone.title"));
    expect(html.indexOf(text("settings.workspace_export.title"))).toBeLessThan(danger);
    expect(html.indexOf(text("settings.workspace_import.title"))).toBeLessThan(danger);
  });

  it.each(["admin", "member"] as const)("shows %s no export or import, which are an owner's", (role) => {
    const html = settings({ instanceAdmin: false, role });
    expect(html).not.toContain(text("settings.workspace_export.download_workspace"));
    expect(html).not.toContain(text("settings.workspace_export.warning_title"));
    expect(html).not.toContain(text("settings.workspace_import.file_label"));
  });
});
