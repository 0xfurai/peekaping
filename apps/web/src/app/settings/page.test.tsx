import { describe, expect, it, vi } from "vitest";
import { getSettingsKeyByKeyQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, WORKSPACE_ID } from "@/test-support/render-with-me";
import SettingsPage from "./page";

// The timezone card reads the browser's storage, which a static render doesn't have
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

const settings = (access: Parameters<typeof renderAs>[0]) =>
  renderAs(access, <SettingsPage />, undefined, (queryClient) => {
    queryClient.setQueryData(
      getSettingsKeyByKeyQueryKey({ path: { key: "KEEP_DATA_PERIOD_DAYS" } }),
      { data: { key: "KEEP_DATA_PERIOD_DAYS", type: "int", value: "90" } }
    );
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

  it("shows an instance admin who owns the workspace everything, as before", () => {
    const html = settings({ instanceAdmin: true, role: "owner" });
    expect(html).toContain(text("settings.telemetry.title"));
    expect(html).toContain(text("settings.error_reporting.title"));
    expect(html).not.toContain("readOnly");
    expect(html).toContain(`>${text("common.save")}<`);
  });

  it("lets an instance admin who is only a member of the workspace change the telemetry but not the retention period", () => {
    const html = settings({ instanceAdmin: true, role: "member" });
    expect(html).toContain(text("settings.telemetry.title"));
    expect(html).toContain(text("settings.data_retention.read_only"));
  });

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
