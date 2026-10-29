import { describe, expect, it } from "vitest";
import { renderAs, text } from "@/test-support/render-with-me";
import { ForgotPasswordView } from "./forgot-password-form";

const view = (outcome: "idle" | "sent" | "rate_limited" | "error") =>
  renderAs(
    { instanceAdmin: false, role: "owner" },
    <ForgotPasswordView outcome={outcome} pending={false} onSubmit={() => {}} />,
    "/forgot-password"
  );

describe("ForgotPasswordView", () => {
  it("asks for an email and links back to sign in", () => {
    const html = view("idle");
    expect(html).toContain('type="email"');
    expect(html).toContain(`>${text("auth.forgot.submit")}<`);
    expect(html).toContain('href="/login"');
    expect(html).not.toContain(text("auth.forgot.sent"));
  });

  it("says a link was sent, the same for an unknown email as for a known one, and hides the form", () => {
    const html = view("sent");
    expect(html).toContain(
      "If an account exists for this email, we sent a link. It works for an hour."
    );
    expect(html).not.toContain('type="email"');
    expect(html).toContain('href="/login"');
  });

  it("tells the user to wait on a 429 and keeps the form", () => {
    const html = view("rate_limited");
    expect(html).toContain(text("auth.errors.rate_limited"));
    expect(html).toContain('type="email"');
    expect(html).not.toContain(text("auth.forgot.sent"));
  });

  it("shows a generic message on another failure", () => {
    expect(view("error")).toContain(text("auth.errors.generic"));
  });
});
