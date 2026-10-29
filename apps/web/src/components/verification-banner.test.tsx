import { describe, expect, it } from "vitest";
import { getMeQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, USER_EMAIL, WORKSPACE_ID } from "@/test-support/render-with-me";
import { VerificationBanner, VerificationBannerView } from "./verification-banner";

// The banner inside the signed-in app, with /me as the server answers it
const banner = (accountEmails: boolean, emailVerified: boolean) =>
  renderAs({ instanceAdmin: false, role: "owner" }, <VerificationBanner />, undefined, (queryClient) => {
    queryClient.setQueryData(getMeQueryKey(), {
      data: {
        accountEmails,
        user: { id: "u1", email: USER_EMAIL, instanceAdmin: false, emailVerified },
        workspaces: [{ id: WORKSPACE_ID, name: "Main", role: "owner" }],
        defaultWorkspaceId: WORKSPACE_ID,
      },
    });
  });

describe("VerificationBanner", () => {
  it("asks the user to verify their address, with Resend, when the install sends emails", () => {
    const html = banner(true, false);
    expect(html).toContain(`Verify your email: we sent a link to ${USER_EMAIL}.`);
    expect(html).toContain(`>${text("auth.resend.button")}<`);
  });

  it("is hidden when the install has no sender", () => {
    expect(banner(false, false)).toBe("");
  });

  it("is hidden once the address is verified", () => {
    expect(banner(true, true)).toBe("");
  });
});

describe("VerificationBannerView", () => {
  const view = (outcome: "idle" | "sent" | "rate_limited" | "error" | "already_verified") =>
    renderAs(
      { instanceAdmin: false, role: "owner" },
      <VerificationBannerView email={USER_EMAIL} outcome={outcome} pending={false} onResend={() => {}} />
    );

  it("confirms a sent link and keeps Resend", () => {
    const html = view("sent");
    expect(html).toContain(text("auth.resend.sent"));
    expect(html).toContain(`>${text("auth.resend.button")}<`);
  });

  it("tells the user to wait on a 429, and says so on another failure", () => {
    expect(view("rate_limited")).toContain(text("auth.errors.rate_limited"));
    expect(view("error")).toContain(text("auth.errors.generic"));
  });

  it("hides itself after a 409, the address already verified", () => {
    expect(view("already_verified")).toBe("");
  });
});
