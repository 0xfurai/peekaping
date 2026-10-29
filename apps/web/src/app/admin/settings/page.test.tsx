import { describe, expect, it, vi } from "vitest";
import {
  getAdminSettingsQueryKey,
  getErrorReportingQueryKey,
  getTelemetryQueryKey,
} from "@/api/@tanstack/react-query.gen";
import type { AdminInstallSettingsResponse } from "@/api/types.gen";
import { SAVE_INSTALL_SETTINGS } from "@/lib/install-settings";
import { renderAs, text as english, USER_EMAIL } from "@/test-support/render-with-me";
import InstallSettingsPage from "./page";

vi.mock("@/lib/config", () => ({ getConfig: () => ({ API_URL: "" }) }));

// A key's English text as the markup escapes it
const text = (key: string) => english(key).replace(/'/g, "&#x27;");

const field = <T,>(value: T, source: "environment" | "saved" | "default", envVar: string) => ({
  value,
  source,
  locked: source === "environment",
  envVar,
});

const SETTINGS: AdminInstallSettingsResponse = {
  signupMode: field("invite", "default", "SIGNUP_MODE"),
  publicUrl: field("https://status.example.com", "saved", "CLIENT_URL"),
  accountSmtp: {
    host: field("smtp.example.com", "environment", "ACCOUNT_SMTP_HOST"),
    port: field(587, "default", "ACCOUNT_SMTP_PORT"),
    user: field("", "default", "ACCOUNT_SMTP_USER"),
    password: { set: true, source: "saved", locked: false, envVar: "ACCOUNT_SMTP_PASSWORD" },
    from: field("no-reply@example.com", "saved", "ACCOUNT_SMTP_FROM"),
    tls: field("starttls", "default", "ACCOUNT_SMTP_TLS"),
  },
  accountEmails: true,
  workspaceLimits: {
    monitors: field<number | null>(10, "environment", "WORKSPACE_LIMIT_MONITORS"),
    min_check_interval: field<number | null>(300, "saved", "WORKSPACE_LIMIT_MIN_CHECK_INTERVAL"),
    status_pages: field<number | null>(null, "default", "WORKSPACE_LIMIT_STATUS_PAGES"),
    custom_domains: field<number | null>(null, "default", "WORKSPACE_LIMIT_CUSTOM_DOMAINS"),
    members: field<number | null>(null, "default", "WORKSPACE_LIMIT_MEMBERS"),
    owned_workspaces: field<number | null>(null, "default", "WORKSPACE_LIMIT_OWNED_WORKSPACES"),
    probes_per_monitor: field<number | null>(null, "default", "WORKSPACE_LIMIT_PROBES_PER_MONITOR"),
    retention_days: field<number | null>(null, "default", "WORKSPACE_LIMIT_RETENTION_DAYS"),
  },
};

const page = (settings: AdminInstallSettingsResponse = SETTINGS) =>
  renderAs({ instanceAdmin: true, role: "owner" }, <InstallSettingsPage />, "/admin/settings", (queryClient) => {
    queryClient.setQueryData(getAdminSettingsQueryKey(), { data: settings, message: "" });
    queryClient.setQueryData(getTelemetryQueryKey(), { enabled: true, forced_off: false, notice_pending: false, payload: {} });
    queryClient.setQueryData(getErrorReportingQueryKey(), { enabled: true, forced_off: false });
  });

// The source label and lock note that follow a field's label, up to its next field
const around = (html: string, id: string) => {
  const start = html.indexOf(`for="${id}"`);
  const end = html.indexOf("<label", start + 1);
  return html.slice(start, end === -1 ? undefined : end);
};

// The tag of the field with this id
const tag = (html: string, id: string) =>
  html.match(new RegExp(`<(?:input|select)[^>]*id="${id}"[^>]*>`))?.[0] ?? "";

describe("InstallSettingsPage", () => {
  it("has the signup, sender and public URL sections, and the usage ping and error reports", () => {
    const html = page();
    for (const key of [
      "admin.settings.signup.title",
      "admin.settings.sender.title",
      "admin.settings.public_url.title",
      "admin.settings.test_email.title",
      "settings.telemetry.title",
      "settings.error_reporting.title",
    ]) {
      expect(html).toContain(text(key));
    }
    expect(html).toContain('id="usage-statistics"');
    expect(html).toContain('id="error-reports"');
    for (const option of ["invite", "open"]) {
      expect(html).toContain(text(`admin.settings.signup.${option}`));
    }
    for (const option of ["starttls", "tls", "none"]) {
      expect(html).toContain(`value="${option}"`);
    }
  });

  it("says where each value comes from", () => {
    const html = page();
    expect(around(html, "install-smtp-host")).toContain(text("admin.settings.source.environment"));
    expect(around(html, "install-public-url")).toContain(text("admin.settings.source.saved"));
    expect(around(html, "install-smtp-from")).toContain(text("admin.settings.source.saved"));
    expect(around(html, "install-smtp-port")).toContain(text("admin.settings.source.default"));
    expect(around(html, "install-signup-mode")).toContain(text("admin.settings.source.default"));
  });

  it("describes each field by where its value comes from, and a locked one by its lock note too", () => {
    const html = page();
    expect(tag(html, "install-smtp-host")).toContain('aria-describedby="install-smtp-host-source install-smtp-host-locked"');
    expect(html).toContain(`id="install-smtp-host-source" class="text-xs text-muted-foreground">${text("admin.settings.source.environment")}<`);
    expect(tag(html, "install-public-url")).toContain('aria-describedby="install-public-url-source"');
    expect(tag(html, "install-smtp-password")).toContain('aria-describedby="install-smtp-password-source"');
  });

  it("shows a field the environment sets read-only, with its variable", () => {
    const html = page();
    expect(tag(html, "install-smtp-host")).toContain('readOnly=""');
    expect(tag(html, "install-smtp-host")).toContain('value="smtp.example.com"');
    expect(around(html, "install-smtp-host")).toContain(text("admin.settings.locked"));
    expect(around(html, "install-smtp-host")).toContain("ACCOUNT_SMTP_HOST");
    // A field the environment doesn't set can be changed
    expect(tag(html, "install-public-url")).not.toContain("readOnly");
    expect(tag(html, "install-public-url")).toContain('value="https://status.example.com"');
    expect(around(html, "install-public-url")).not.toContain(text("admin.settings.locked"));
  });

  it("locks a select the environment sets", () => {
    const html = page({ ...SETTINGS, signupMode: field("open", "environment", "SIGNUP_MODE") });
    expect(tag(html, "install-signup-mode")).toContain('disabled=""');
    expect(around(html, "install-signup-mode")).toContain("SIGNUP_MODE");
  });

  it("shows the password field empty, saying one is set, with a way to remove it", () => {
    const html = page();
    expect(tag(html, "install-smtp-password")).toContain('value=""');
    expect(tag(html, "install-smtp-password")).toContain(`placeholder="${text("admin.settings.sender.password_set")}"`);
    expect(html).toContain(text("admin.settings.sender.remove_password"));
  });

  it("says when no password is set, and has nothing to remove", () => {
    const html = page({
      ...SETTINGS,
      accountSmtp: { ...SETTINGS.accountSmtp, password: { set: false, source: "default", locked: false, envVar: "ACCOUNT_SMTP_PASSWORD" } },
    });
    expect(html).toContain(`placeholder="${text("admin.settings.sender.password_not_set")}"`);
    expect(html).not.toContain(text("admin.settings.sender.remove_password"));
  });

  it("offers no removal of a password the environment sets", () => {
    const html = page({
      ...SETTINGS,
      accountSmtp: { ...SETTINGS.accountSmtp, password: { set: true, source: "environment", locked: true, envVar: "ACCOUNT_SMTP_PASSWORD" } },
    });
    expect(html).not.toContain(text("admin.settings.sender.remove_password"));
    expect(around(html, "install-smtp-password")).toContain("ACCOUNT_SMTP_PASSWORD");
  });

  it("sends the test email to the admin's own address unless they type another", () => {
    const html = page();
    expect(tag(html, "install-test-email-to")).toContain(`value="${USER_EMAIL}"`);
    expect(html).toContain(`>${text("admin.settings.test_email.send")}<`);
  });

  it("has a section of the default limits, a field for each", () => {
    const html = page();
    expect(html).toContain(text("admin.settings.limits.title"));
    for (const name of ["monitors", "min_check_interval", "status_pages", "custom_domains", "members", "owned_workspaces", "probes_per_monitor", "retention_days"]) {
      expect(html).toContain(`for="install-limit-${name}"`);
      expect(around(html, `install-limit-${name}`)).toContain(text(`limits.names.${name}`));
    }
  });

  it("says where each default limit comes from, and locks one the environment sets", () => {
    const html = page();
    expect(around(html, "install-limit-monitors")).toContain(text("admin.settings.source.environment"));
    expect(around(html, "install-limit-monitors")).toContain("WORKSPACE_LIMIT_MONITORS");
    expect(tag(html, "install-limit-monitors")).toContain('readOnly=""');
    expect(tag(html, "install-limit-monitors")).toContain('value="10"');
    expect(around(html, "install-limit-min_check_interval")).toContain(text("admin.settings.source.saved"));
    expect(tag(html, "install-limit-min_check_interval")).toContain('value="300"');
    expect(tag(html, "install-limit-min_check_interval")).not.toContain("readOnly");
    expect(tag(html, "install-limit-min_check_interval")).toContain('aria-describedby="install-limit-min_check_interval-source"');
  });

  it("lets each default limit's input take only what the server takes", () => {
    const html = page();
    expect(tag(html, "install-limit-monitors")).toContain('min="1"');
    expect(tag(html, "install-limit-monitors")).not.toContain("max=");
    expect(tag(html, "install-limit-retention_days")).toContain('min="1"');
    expect(tag(html, "install-limit-retention_days")).toContain('max="36500"');
    expect(tag(html, "install-limit-min_check_interval")).toContain('min="20"');
  });

  it("starts the shortest check interval at the install's minimum, as /me states it", () => {
    const html = renderAs({ instanceAdmin: true, role: "owner", minCheckInterval: 60 }, <InstallSettingsPage />, "/admin/settings", (queryClient) => {
      queryClient.setQueryData(getAdminSettingsQueryKey(), { data: SETTINGS, message: "" });
      queryClient.setQueryData(getTelemetryQueryKey(), { enabled: true, forced_off: false, notice_pending: false, payload: {} });
      queryClient.setQueryData(getErrorReportingQueryKey(), { enabled: true, forced_off: false });
    });
    expect(tag(html, "install-limit-min_check_interval")).toContain('min="60"');
  });

  it("shows a default limit of none blank", () => {
    const html = page();
    expect(around(html, "install-limit-members")).toContain(text("admin.settings.source.default"));
    expect(tag(html, "install-limit-members")).toContain('value=""');
    expect(tag(html, "install-limit-members")).toContain(`placeholder="${text("limits.none")}"`);
  });

  it("has nothing to save until a field changes", () => {
    expect(page()).toMatch(/<button[^>]*type="submit"[^>]*disabled=""/);
  });

  it("keeps the fields editable while no save is pending", () => {
    expect(page()).not.toMatch(/<fieldset[^>]*disabled/);
  });

  it("disables the fields while a save is pending, so that nothing typed then is lost", () => {
    const html = renderAs({ instanceAdmin: true, role: "owner" }, <InstallSettingsPage />, "/admin/settings", (queryClient) => {
      queryClient.setQueryData(getAdminSettingsQueryKey(), { data: SETTINGS, message: "" });
      queryClient.setQueryData(getTelemetryQueryKey(), { enabled: true, forced_off: false, notice_pending: false, payload: {} });
      queryClient.setQueryData(getErrorReportingQueryKey(), { enabled: true, forced_off: false });
      queryClient.getMutationCache().build(queryClient, { mutationKey: [...SAVE_INSTALL_SETTINGS] }, {
        context: undefined,
        data: undefined,
        error: null,
        failureCount: 0,
        failureReason: null,
        isPaused: false,
        status: "pending",
        variables: undefined,
        submittedAt: 0,
      });
    });
    const fieldset = html.match(/<fieldset[^>]*>/)?.[0] ?? "";
    expect(fieldset).toContain('disabled=""');
    // Every field of the form is inside it
    const inside = html.slice(html.indexOf(fieldset), html.indexOf("</fieldset>"));
    for (const id of ["install-signup-mode", "install-smtp-host", "install-smtp-password", "install-public-url"]) {
      expect(inside).toContain(`id="${id}"`);
    }
    expect(html).toMatch(new RegExp(`<button[^>]*type="submit"[^>]*disabled=""[^>]*>${text("common.saving")}<`));
  });
});
