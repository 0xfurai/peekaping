import { describe, expect, it } from "vitest";
import { renderAs, text } from "@/test-support/render-with-me";
import { RegisterErrorAlert, RegisterForm } from "./register-form";

const register = (turnstileSiteKey: string | null, signupMode?: "invite" | "open") =>
  renderAs(
    { instanceAdmin: false, role: "owner" },
    <RegisterForm turnstileSiteKey={turnstileSiteKey} signupMode={signupMode} />,
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
  it("says \"Create your account\" on an open signup", () => {
    const html = register(null, "open");
    expect(html).toContain("Create your account");
    expect(html).not.toContain("Create your admin account");
  });

  it("says \"Create your admin account\" on an invite install", () => {
    expect(register(null, "invite")).toContain("Create your admin account");
  });
});

describe("RegisterForm install-wide choices", () => {
  // Only an instance admin may set the usage ping and the error reports, so only the install's
  // first signup (invite mode, while it has no user) is offered them
  it("offers the usage ping and error report choices to the first signup of an invite install", () => {
    const html = register(null, "invite");
    expect(html).toContain('id="send-usage-statistics"');
    expect(html).toContain('id="send-error-reports"');
    expect(html).toContain(text("auth.register.telemetry_note"));
  });

  it("leaves them out of an open signup, which can't tell it is the first", () => {
    const html = register(null, "open");
    expect(html).not.toContain("send-usage-statistics");
    expect(html).not.toContain("send-error-reports");
    expect(html).not.toContain(text("auth.register.telemetry_note"));
  });

  it("leaves them out while the mode is unknown", () => {
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
