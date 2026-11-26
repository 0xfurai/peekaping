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

// The same form with the signup part of /auth/config: open, closed, or not fetched yet
const loginWithSignup = (signupOpen?: boolean) =>
  renderAs({ instanceAdmin: false, role: "owner" }, <LoginForm />, "/login", (queryClient) => {
    if (signupOpen !== undefined) {
      queryClient.setQueryData(getAuthConfigQueryKey(), {
        data: {
          accountEmails: false,
          signupMode: signupOpen ? "open" : "invite",
          signupOpen,
          turnstileSiteKey: null,
        },
      });
    }
  });

describe("LoginForm sign up", () => {
  it("links to the register page when signup is open", () => {
    const html = loginWithSignup(true);
    expect(html).toContain('href="/register"');
    expect(html).toContain(text("auth.login.sign_up"));
  });

  it("asks for an invite instead of linking when signup is closed", () => {
    const html = loginWithSignup(false);
    expect(html).not.toContain("/register");
    expect(html).not.toContain(text("auth.login.sign_up"));
    expect(html).toContain("Ask an admin of this install for an invite");
  });

  it("offers neither while /auth/config is loading", () => {
    const html = loginWithSignup();
    expect(html).not.toContain("/register");
    expect(html).not.toContain(text("auth.login.ask_admin"));
  });
});
