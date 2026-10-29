import { describe, expect, it } from "vitest";
import { AxiosError, AxiosHeaders } from "axios";
import type { AdminInstallSettingsResponse } from "@/api/types.gen";
import i18n from "@/i18n";
import {
  installSettingsChange,
  installSettingsErrorMessage,
  installSettingsFormValues,
} from "./install-settings";

const field = <T,>(value: T, source: "environment" | "saved" | "default" = "default", envVar = "X") => ({
  value,
  source,
  locked: source === "environment",
  envVar,
});

const SETTINGS: AdminInstallSettingsResponse = {
  signupMode: field("invite"),
  publicUrl: field("http://localhost:8383", "saved", "CLIENT_URL"),
  accountSmtp: {
    host: field("smtp.example.com", "saved", "ACCOUNT_SMTP_HOST"),
    port: field(587, "default", "ACCOUNT_SMTP_PORT"),
    user: field("mailer", "environment", "ACCOUNT_SMTP_USER"),
    password: { set: true, source: "saved", locked: false, envVar: "ACCOUNT_SMTP_PASSWORD" },
    from: field("no-reply@example.com", "saved", "ACCOUNT_SMTP_FROM"),
    tls: field("starttls"),
  },
  accountEmails: true,
};

const values = installSettingsFormValues(SETTINGS);
const t = i18n.t.bind(i18n) as unknown as (key: string, vars?: Record<string, unknown>) => string;

describe("installSettingsChange", () => {
  it("sends nothing when nothing changed", () => {
    expect(installSettingsChange(SETTINGS, values)).toEqual({});
  });

  it("sends only the fields the admin changed", () => {
    const body = installSettingsChange(SETTINGS, {
      ...values,
      signupMode: "open",
      port: 2525,
      tls: "none",
    });
    expect(body).toEqual({ signupMode: "open", accountSmtp: { port: 2525, tls: "none" } });
  });

  it("sends an emptied field empty, and an emptied port as 0, which clears the saved value", () => {
    const body = installSettingsChange(SETTINGS, { ...values, publicUrl: "  ", host: "", port: 0 });
    expect(body).toEqual({ publicUrl: "", accountSmtp: { host: "", port: 0 } });
  });

  it("trims what it sends, and a field typed back to its value is no change", () => {
    const body = installSettingsChange(SETTINGS, { ...values, host: " smtp.example.com ", from: " a@example.com " });
    expect(body).toEqual({ accountSmtp: { from: "a@example.com" } });
  });

  it("never sends a field the environment locks", () => {
    const locked: AdminInstallSettingsResponse = {
      ...SETTINGS,
      signupMode: field("open", "environment", "SIGNUP_MODE"),
      accountSmtp: {
        ...SETTINGS.accountSmtp,
        password: { set: true, source: "environment", locked: true, envVar: "ACCOUNT_SMTP_PASSWORD" },
      },
    };
    const body = installSettingsChange(
      locked,
      { ...installSettingsFormValues(locked), signupMode: "invite", user: "other", password: "typed" },
      { removePassword: true }
    );
    expect(body).toEqual({});
  });

  describe("the password", () => {
    it("is left out while nothing is typed: the one there is stays", () => {
      expect(installSettingsChange(SETTINGS, { ...values, password: "" })).toEqual({});
    });

    it("is replaced by what the admin typed, as typed", () => {
      expect(installSettingsChange(SETTINGS, { ...values, password: " s3cret " })).toEqual({
        accountSmtp: { password: " s3cret " },
      });
    });

    it("is sent empty when the admin removes it", () => {
      expect(installSettingsChange(SETTINGS, values, { removePassword: true })).toEqual({
        accountSmtp: { password: "" },
      });
    });

    it("is not removed when there is none", () => {
      const none = { ...SETTINGS, accountSmtp: { ...SETTINGS.accountSmtp, password: { ...SETTINGS.accountSmtp.password, set: false } } };
      expect(installSettingsChange(none, installSettingsFormValues(none), { removePassword: true })).toEqual({});
    });

    it("loads empty: the form never holds the saved one", () => {
      expect(values.password).toBe("");
    });
  });
});

const refusal = (status: number, data: unknown) =>
  new AxiosError("refused", String(status), undefined, undefined, {
    status,
    statusText: "",
    headers: {},
    config: { headers: new AxiosHeaders() },
    data,
  });

describe("installSettingsErrorMessage", () => {
  it("names the locked setting of a 409", () => {
    const error = refusal(409, {
      message: "signupMode: set by the server's environment (SIGNUP_MODE)",
      error: { code: "SETTING_LOCKED", field: "signupMode" },
    });
    expect(installSettingsErrorMessage(error, t, "fallback")).toBe(
      "Signup mode is set by the server's environment and can't be changed here."
    );
  });

  it("says open signup needs a sender", () => {
    const error = refusal(400, {
      message: "signupMode: open signup needs a sender for account emails",
      error: { code: "OPEN_SIGNUP_NEEDS_SENDER", field: "signupMode" },
    });
    expect(installSettingsErrorMessage(error, t, "fallback")).toBe(
      "Open signup needs an email sender, because new accounts verify their email address. Set up the sender first, or in the same save."
    );
  });

  it("says the sender can't go while signup is open", () => {
    const error = refusal(400, {
      message: "accountSmtp.host: signup is open",
      error: { code: "SENDER_NEEDED_FOR_OPEN_SIGNUP", field: "accountSmtp.host" },
    });
    expect(installSettingsErrorMessage(error, t, "fallback")).toBe(
      "Signup is open, and new accounts need the sender to verify their email address. Switch signup to invite only first."
    );
  });

  it("keeps the server's reason for an invalid value, after the setting's label", () => {
    const error = refusal(400, {
      message: "accountSmtp.port: must be a port number from 1 to 65535",
      error: { code: "INVALID_SETTING", field: "accountSmtp.port" },
    });
    expect(installSettingsErrorMessage(error, t, "fallback")).toBe("Port: must be a port number from 1 to 65535");
  });

  it("says a test email needs a sender", () => {
    const error = refusal(400, { message: "The install has no sender", error: { code: "NO_SENDER" } });
    expect(installSettingsErrorMessage(error, t, "fallback")).toBe("There is no email sender. Save a sender first.");
  });

  it("shows the server's message for anything else, else the fallback", () => {
    expect(installSettingsErrorMessage(refusal(502, { message: "The test email was not sent: timeout" }), t, "fallback")).toBe(
      "The test email was not sent: timeout"
    );
    expect(installSettingsErrorMessage(refusal(500, {}), t, "fallback")).toBe("fallback");
    expect(installSettingsErrorMessage(new Error("network"), t, "fallback")).toBe("fallback");
  });
});
