import { AxiosError, AxiosHeaders } from "axios";
import { describe, expect, it } from "vitest";
import {
  canSubmitRegister,
  loginSignupOffer,
  registerBody,
  registerDescriptionKey,
  registerFailureKey,
  registerPageView,
  showInstallPreferences,
} from "./signup";

const config = (signupMode: "invite" | "open", signupOpen: boolean) => ({
  accountEmails: false,
  signupMode,
  signupOpen,
  turnstileSiteKey: null,
});

const failure = (status: number, message: string) =>
  new AxiosError("failed", "ERR_BAD_REQUEST", undefined, undefined, {
    status,
    statusText: "",
    headers: {},
    config: { headers: new AxiosHeaders() },
    data: { message },
  });

describe("registerPageView", () => {
  it("shows the form in open mode", () => {
    expect(registerPageView(config("open", true), false)).toBe("form");
  });

  it("shows the form in invite mode while the install has no user", () => {
    expect(registerPageView(config("invite", true), false)).toBe("form");
  });

  it("sends a visitor to login once signup is closed", () => {
    expect(registerPageView(config("invite", false), false)).toBe("login");
  });

  it("waits for /auth/config, and gives up to login when it fails", () => {
    expect(registerPageView(undefined, false)).toBe("loading");
    expect(registerPageView(undefined, true)).toBe("login");
  });
});

describe("loginSignupOffer", () => {
  it("offers the register link when signup is open", () => {
    expect(loginSignupOffer(config("open", true))).toBe("register");
    expect(loginSignupOffer(config("invite", true))).toBe("register");
  });

  it("tells the visitor to ask an admin for an invite when signup is closed", () => {
    expect(loginSignupOffer(config("invite", false))).toBe("ask_admin");
  });

  it("offers nothing while /auth/config is unknown", () => {
    expect(loginSignupOffer(undefined)).toBe("none");
  });
});

describe("canSubmitRegister", () => {
  it("needs no token without a site key", () => {
    expect(canSubmitRegister(null, null)).toBe(true);
    expect(canSubmitRegister(undefined, null)).toBe(true);
  });

  it("waits for the widget's token when there is a site key", () => {
    expect(canSubmitRegister("0x4AAA", null)).toBe(false);
    expect(canSubmitRegister("0x4AAA", "")).toBe(false);
    expect(canSubmitRegister("0x4AAA", "tok")).toBe(true);
  });
});

describe("registerBody", () => {
  const values = { email: "a@example.com", password: "password1", confirmPassword: "password1" };

  it("sends the widget's token as turnstileToken", () => {
    expect(registerBody(values, "tok")).toEqual({ ...values, turnstileToken: "tok" });
  });

  it("sends none without a token", () => {
    expect(registerBody(values, null)).toEqual(values);
    expect("turnstileToken" in registerBody(values, null)).toBe(false);
  });
});

describe("registerFailureKey", () => {
  it("429 is too many signups from the network", () => {
    expect(registerFailureKey(failure(429, "Too many requests, try again later"))).toBe(
      "auth.register.errors.rate_limited"
    );
  });

  it("409 is an existing account", () => {
    expect(
      registerFailureKey(failure(409, "An account with this email address exists already: sign in instead"))
    ).toBe("auth.register.errors.email_taken");
  });

  it("400 for the Turnstile check is a failed check", () => {
    expect(
      registerFailureKey(
        failure(400, "We could not verify that you are not a robot. Reload the page and try again.")
      )
    ).toBe("auth.register.errors.check_failed");
  });

  it("leaves any other answer to the server's own message", () => {
    expect(registerFailureKey(failure(400, "Password too weak"))).toBeUndefined();
    expect(registerFailureKey(failure(500, "boom"))).toBeUndefined();
    expect(registerFailureKey(new Error("network"))).toBeUndefined();
  });
});

describe("registerDescriptionKey", () => {
  it("calls the account an ordinary one in open mode", () => {
    expect(registerDescriptionKey("open")).toBe("auth.register.description_open");
  });

  it("calls it the admin account in invite mode, and while the mode is unknown", () => {
    expect(registerDescriptionKey("invite")).toBe("auth.register.description");
    expect(registerDescriptionKey(undefined)).toBe("auth.register.description");
  });
});

describe("showInstallPreferences", () => {
  it("shows them on an invite install, whose form is only offered to its first user", () => {
    expect(showInstallPreferences("invite")).toBe(true);
  });

  it("hides them in open mode and while the mode is unknown", () => {
    expect(showInstallPreferences("open")).toBe(false);
    expect(showInstallPreferences(undefined)).toBe(false);
  });
});
