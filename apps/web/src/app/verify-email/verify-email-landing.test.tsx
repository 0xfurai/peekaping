import { describe, expect, it } from "vitest";
import { renderAs, text } from "@/test-support/render-with-me";
import { VerifyEmailView } from "./verify-email-landing";

const view = (
  outcome: "pending" | "verified" | "invalid_link" | "rate_limited" | "error",
  signedIn: boolean
) =>
  renderAs(
    { instanceAdmin: false, role: "owner" },
    <VerifyEmailView outcome={outcome} signedIn={signedIn} />,
    "/verify-email/vfy_abc"
  );

describe("VerifyEmailView", () => {
  it("shows progress while the link is being checked", () => {
    const html = view("pending", true);
    expect(html).toContain(text("auth.verify.verifying"));
    expect(html).not.toContain(text("auth.resend.button"));
  });

  it("on success says so and links into the app, signed in or not", () => {
    for (const signedIn of [true, false]) {
      const html = view("verified", signedIn);
      expect(html).toContain(text("auth.verify.success"));
      expect(html).toContain(`href="/"`);
      expect(html).toContain(text("auth.verify.open_app"));
    }
  });

  it("on a bad link tells a signed-in user so and offers Resend", () => {
    const html = view("invalid_link", true);
    expect(html).toContain("This link is used or expired.");
    expect(html).toContain(`>${text("auth.resend.button")}<`);
  });

  it("on a bad link tells a visitor so and offers no Resend, but a way to sign in", () => {
    const html = view("invalid_link", false);
    expect(html).toContain("This link is used or expired.");
    expect(html).not.toContain(text("auth.resend.button"));
    expect(html).toContain('href="/login"');
  });

  it("says to wait on a 429 and shows a generic message on another failure", () => {
    expect(view("rate_limited", true)).toContain(text("auth.errors.rate_limited"));
    expect(view("error", true)).toContain(text("auth.errors.generic"));
  });
});
