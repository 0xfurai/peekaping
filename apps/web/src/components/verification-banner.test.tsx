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

// The banner in a workspace, with /me carrying that workspace's suspension
const inWorkspace = (opts: {
  role?: "owner" | "admin" | "member";
  suspendedReason: string | null;
  emailVerified?: boolean;
  accountEmails?: boolean;
}) =>
  renderAs({ instanceAdmin: false, role: "owner" }, <VerificationBanner />, undefined, (queryClient) => {
    queryClient.setQueryData(getMeQueryKey(), {
      data: {
        accountEmails: opts.accountEmails ?? true,
        user: { id: "u1", email: USER_EMAIL, instanceAdmin: false, emailVerified: opts.emailVerified ?? false },
        workspaces: [
          {
            id: WORKSPACE_ID,
            name: "Main",
            role: opts.role ?? "owner",
            suspended: opts.suspendedReason !== null,
            suspendedReason: opts.suspendedReason,
          },
        ],
        defaultWorkspaceId: WORKSPACE_ID,
      },
    });
  });

describe("VerificationBanner in a suspended workspace", () => {
  const notice = text("auth.suspension_notice.message");

  it("tells an unverified owner their monitors start once they verify, with Resend", () => {
    const html = inWorkspace({ suspendedReason: "unverified" });
    expect(html).toContain("Your monitors start once you verify your email.");
    expect(html).toContain(`>${text("auth.resend.button")}<`);
  });

  it("replaces the generic banner instead of adding to it", () => {
    const html = inWorkspace({ suspendedReason: "unverified" });
    expect(html).not.toContain("Verify your email: we sent a link");
    expect(html.split(notice).length - 1).toBe(1);
  });

  it("shows the generic banner, not the notice, for another reason", () => {
    const html = inWorkspace({ suspendedReason: "something_else" });
    expect(html).toContain("Verify your email: we sent a link");
    expect(html).not.toContain(notice);
  });

  it("shows no notice in a workspace that runs", () => {
    const html = inWorkspace({ suspendedReason: null, emailVerified: true });
    expect(html).toBe("");
  });

  it("shows the notice without Resend to a verified member, whose own address is fine", () => {
    const html = inWorkspace({ suspendedReason: "unverified", emailVerified: true });
    expect(html).toContain(notice);
    expect(html).not.toContain(text("auth.resend.button"));
  });

  it("tells a member the monitors start once the owner verifies, not that they should", () => {
    for (const role of ["member", "admin"] as const) {
      const html = inWorkspace({ role, suspendedReason: "unverified", emailVerified: true });
      expect(html).toContain("This workspace&#x27;s monitors start once its owner verifies their email.");
      expect(html).not.toContain(notice);
    }
  });

  // Resend sends the member's own link, which does not start the owner's monitors
  it("tells an unverified member that Resend verifies their own address, beside the owner's notice", () => {
    for (const role of ["member", "admin"] as const) {
      const html = inWorkspace({ role, suspendedReason: "unverified", emailVerified: false });
      expect(html).toContain("This workspace&#x27;s monitors start once its owner verifies their email.");
      expect(html).toContain(`Verify your email: we sent a link to ${USER_EMAIL}.`);
      expect(html).toContain(`>${text("auth.resend.button")}<`);
      expect(html).not.toContain(notice);
    }
  });

  it("keeps a single notice for an unverified owner, whose own address is what starts the monitors", () => {
    const html = inWorkspace({ role: "owner", suspendedReason: "unverified", emailVerified: false });
    expect(html).toContain(notice);
    expect(html).not.toContain("Verify your email: we sent a link");
  });

  it("shows the notice in the workspace of the address, not another one", () => {
    const html = renderAs(
      { instanceAdmin: false, role: "owner" },
      <VerificationBanner />,
      "/w/22222222-2222-4222-8222-222222222222/monitors",
      (queryClient) => {
        queryClient.setQueryData(getMeQueryKey(), {
          data: {
            accountEmails: true,
            user: { id: "u1", email: USER_EMAIL, instanceAdmin: false, emailVerified: true },
            workspaces: [
              { id: WORKSPACE_ID, name: "Main", role: "owner", suspended: true, suspendedReason: "unverified" },
              { id: "22222222-2222-4222-8222-222222222222", name: "Other", role: "owner", suspended: false, suspendedReason: null },
            ],
            defaultWorkspaceId: WORKSPACE_ID,
          },
        });
      }
    );
    expect(html).toBe("");
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

  // /me is fetched again after the 409, and the workspace stays held back until it says otherwise
  it("keeps the suspension notice after a 409, without Resend", () => {
    const html = renderAs(
      { instanceAdmin: false, role: "owner" },
      <VerificationBannerView email={USER_EMAIL} suspended outcome="already_verified" pending={false} onResend={() => {}} />
    );
    expect(html).toContain(text("auth.suspension_notice.message"));
    expect(html).not.toContain(text("auth.resend.button"));
  });
});
