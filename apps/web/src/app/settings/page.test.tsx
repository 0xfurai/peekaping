import { describe, expect, it, vi } from "vitest";
import { getSettingsKeyByKeyQueryKey, getWorkspaceLimitsQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import SettingsPage from "./page";

// The timezone card reads the browser's storage, which a static render doesn't have
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

// retentionCap is the workspace's retention_days limit, null for none; left out, the limits aren't
// loaded
const settings = (access: Parameters<typeof renderAs>[0], retentionCap?: number | null) =>
  renderAs(access, <SettingsPage />, undefined, (queryClient) => {
    queryClient.setQueryData(
      getSettingsKeyByKeyQueryKey({ path: { key: "KEEP_DATA_PERIOD_DAYS" } }),
      { data: { key: "KEEP_DATA_PERIOD_DAYS", type: "int", value: "90" } }
    );
    if (retentionCap === undefined) return;
    // GET /workspace/limits as loaded, with the workspace's retention_days limit
    const use = (max: number | null, used = 0) => ({ max, used, source: max === null ? "none" : "install" });
    queryClient.setQueryData(getWorkspaceLimitsQueryKey(), {
      message: "",
      data: {
        source: "install",
        limits: {
          monitors: use(null),
          min_check_interval: use(null, 20),
          status_pages: use(null),
          custom_domains: use(null),
          members: use(null),
          owned_workspaces: use(null),
          probes_per_monitor: use(null),
          retention_days: use(retentionCap, 90),
        },
      },
    });
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
    const html = settings({ instanceAdmin: false, role: "admin" }, 120);
    expect(html).toContain("Up to 120 days on this workspace");
    expect(html).toMatch(/<input[^>]*max="120"/);
  });

  it("shows a member the cap beside the read-only period", () => {
    const html = settings({ instanceAdmin: false, role: "member" }, 120);
    expect(html).toContain("Up to 120 days on this workspace");
  });

  it.each([
    ["without a cap", null],
    ["while the limits load", undefined],
  ])("shows no cap %s, and keeps the 100 year maximum", (_, cap) => {
    const html = settings({ instanceAdmin: false, role: "admin" }, cap);
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
