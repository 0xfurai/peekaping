import { describe, expect, it } from "vitest";
import { renderAs, text } from "@/test-support/render-with-me";
import { RegisterErrorAlert, RegisterForm } from "./register-form";

const register = (turnstileSiteKey: string | null, signupMode?: "invite" | "open", firstSignup?: boolean) =>
  renderAs(
    { instanceAdmin: false, role: "owner" },
    <RegisterForm turnstileSiteKey={turnstileSiteKey} signupMode={signupMode} firstSignup={firstSignup} />,
    "/register"
  );

const submitTag = (html: string) => html.match(/<button[^>]*type="submit"[^>]*>/)?.[0] ?? "";

describe("RegisterForm", () => {
  it("shows the Turnstile widget, and a submit that waits for it, when the install has a site key", () => {
    const html = register("0x4AAAAAAA");
    expect(html).toContain("data-turnstile");
    expect(submitTag(html)).toContain('disabled=""');
  });

  it("shows no widget and an enabled submit without a site key", () => {
    const html = register(null);
    expect(html).not.toContain("data-turnstile");
    expect(html).toContain(text("auth.register.submit"));
    expect(submitTag(html)).not.toContain('disabled=""');
  });
});

describe("RegisterForm description", () => {
  it("says \"Create your account\" on a later open signup", () => {
    const html = register(null, "open", false);
    expect(html).toContain("Create your account");
    expect(html).not.toContain("Create your admin account");
  });

  it("says \"Create your admin account\" on the first signup of an open install", () => {
    expect(register(null, "open", true)).toContain("Create your admin account");
  });

  it("says \"Create your admin account\" on an invite install", () => {
    expect(register(null, "invite", true)).toContain("Create your admin account");
  });
});

describe("RegisterForm install-wide choices", () => {
  // Only an instance admin may set the usage ping and the error reports, so only the install's
  // first signup, in either mode, is offered them
  it("offers the usage ping and error report choices to the first signup of an invite install", () => {
    const html = register(null, "invite", true);
    expect(html).toContain('id="send-usage-statistics"');
    expect(html).toContain('id="send-error-reports"');
    expect(html).toContain(text("auth.register.telemetry_note"));
  });

  it("offers them to the first signup of an open install", () => {
    const html = register(null, "open", true);
    expect(html).toContain('id="send-usage-statistics"');
    expect(html).toContain('id="send-error-reports"');
  });

  it("leaves them out of a later open signup, which is no instance admin", () => {
    const html = register(null, "open", false);
    expect(html).not.toContain("send-usage-statistics");
    expect(html).not.toContain("send-error-reports");
    expect(html).not.toContain(text("auth.register.telemetry_note"));
  });

  it("leaves them out while the config is unknown", () => {
    expect(register(null)).not.toContain("send-usage-statistics");
  });
});

describe("RegisterErrorAlert", () => {
  const alert = (message: string, signInLink: boolean) =>
    renderAs({ instanceAdmin: false, role: "owner" }, <RegisterErrorAlert message={message} signInLink={signInLink} />, "/register");

  it("says an account exists, with a link to sign in", () => {
    const html = alert(text("auth.register.errors.email_taken"), true);
    expect(html).toContain("An account with this email exists.");
    expect(html).toContain('href="/login"');
    expect(html).toContain("Sign in instead.");
  });

  it("has no link for another failure", () => {
    const html = alert(text("auth.register.errors.rate_limited"), false);
    expect(html).toContain("Too many signups from your network. Try again in an hour.");
    expect(html).not.toContain("/login");
  });

  it("words the failed check", () => {
    expect(alert(text("auth.register.errors.check_failed"), false)).toContain("The check didn&#x27;t pass. Try again.");
  });
});
