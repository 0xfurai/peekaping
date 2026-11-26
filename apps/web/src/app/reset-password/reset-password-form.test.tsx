import { describe, expect, it } from "vitest";
import { renderAs, text } from "@/test-support/render-with-me";
import { ResetPasswordView } from "./reset-password-form";

const view = (outcome: "idle" | "done" | "invalid_link" | "rate_limited" | "error") =>
  renderAs(
    { instanceAdmin: false, role: "owner" },
    <ResetPasswordView outcome={outcome} pending={false} onSubmit={() => {}} />,
    "/reset-password/rst_abc"
  );

describe("ResetPasswordView", () => {
  it("asks for the new password twice", () => {
    const html = view("idle");
    expect(html.match(/type="password"/g)).toHaveLength(2);
    expect(html).toContain(`>${text("auth.reset.submit")}<`);
    expect(html).not.toContain(text("auth.reset.invalid_link"));
  });

  it("on success says the sessions ended and links to sign in, without the form", () => {
    const html = view("done");
    expect(html).toContain(text("auth.reset.success"));
    expect(html).toContain('href="/login"');
    expect(html).not.toContain('type="password"');
  });

  it("on a used or expired link says so and links to ask for a new one, without the form", () => {
    const html = view("invalid_link");
    expect(html).toContain("This link is used or expired.");
    expect(html).toContain('href="/forgot-password"');
    expect(html).not.toContain('type="password"');
  });

  it("on a 429 keeps the form and tells the user to wait", () => {
    const html = view("rate_limited");
    expect(html).toContain(text("auth.errors.rate_limited"));
    expect(html).toContain('type="password"');
  });

  it("shows a generic message on another failure", () => {
    expect(view("error")).toContain(text("auth.errors.generic"));
  });
});
