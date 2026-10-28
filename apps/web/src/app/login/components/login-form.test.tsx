import { describe, expect, it } from "vitest";
import { getAuthConfigQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text } from "@/test-support/render-with-me";
import { LoginForm } from "./login-form";

// The login form as a visitor sees it, with the install's /auth/config fetched (or not yet)
const login = (accountEmails?: boolean) =>
  renderAs({ instanceAdmin: false, role: "owner" }, <LoginForm />, "/login", (queryClient) => {
    if (accountEmails !== undefined) {
      queryClient.setQueryData(getAuthConfigQueryKey(), { data: { accountEmails } });
    }
  });

describe("LoginForm", () => {
  it("links to the forgot password page when the install sends account emails", () => {
    const html = login(true);
    expect(html).toContain('href="/forgot-password"');
    expect(html).toContain(text("auth.login.forgot_password"));
  });

  it("has no forgot password link without a sender", () => {
    const html = login(false);
    expect(html).not.toContain("/forgot-password");
    expect(html).not.toContain(text("auth.login.forgot_password"));
  });

  it("has no forgot password link while /auth/config is loading", () => {
    expect(login()).not.toContain("/forgot-password");
  });
});
