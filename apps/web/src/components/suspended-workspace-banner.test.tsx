import { describe, expect, it } from "vitest";
import { getMeQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, USER_EMAIL, WORKSPACE_ID } from "@/test-support/render-with-me";
import { SuspendedWorkspaceBanner } from "./suspended-workspace-banner";
import { VerificationBanner } from "./verification-banner";

// A banner in the workspace of the address, with /me as the server answers it
const banner = (
  ui: React.ReactElement,
  opts: { suspendedReason: string | null; supportContact?: string | null; role?: "owner" | "member" }
) =>
  renderAs({ instanceAdmin: false, role: "owner" }, ui, undefined, (queryClient) => {
    queryClient.setQueryData(getMeQueryKey(), {
      data: {
        accountEmails: true,
        supportContact: opts.supportContact ?? null,
        user: { id: "u1", email: USER_EMAIL, instanceAdmin: false, emailVerified: true },
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

describe("SuspendedWorkspaceBanner", () => {
  // As the markup has it, apostrophes escaped
  const message = text("workspaces.suspended_banner.message").replaceAll("'", "&#x27;");

  it("tells every member an admin suspended the workspace, with a mailto link for an email contact", () => {
    for (const role of ["owner", "member"] as const) {
      const html = banner(<SuspendedWorkspaceBanner />, {
        suspendedReason: "admin",
        supportContact: "support@example.com",
        role,
      });
      expect(html).toContain(message);
      expect(html).toContain(text("workspaces.suspended_banner.contact"));
      expect(html).toContain('href="mailto:support@example.com"');
      expect(html).toContain(">support@example.com<");
    }
  });

  it("links a web address contact", () => {
    const html = banner(<SuspendedWorkspaceBanner />, {
      suspendedReason: "admin",
      supportContact: "https://example.com/help",
    });
    expect(html).toContain('href="https://example.com/help"');
    expect(html).toContain('rel="noopener noreferrer"');
  });

  it("shows any other contact as text, with no link", () => {
    const html = banner(<SuspendedWorkspaceBanner />, {
      suspendedReason: "admin",
      supportContact: "Ask Dana at the front desk",
    });
    expect(html).toContain("Ask Dana at the front desk");
    expect(html).not.toContain("href=");
  });

  it("tells members to contact the person who runs the install when no contact is set", () => {
    const html = banner(<SuspendedWorkspaceBanner />, { suspendedReason: "admin" });
    expect(html).toContain(message);
    expect(html).toContain(text("workspaces.suspended_banner.no_contact"));
    expect(html).not.toContain(text("workspaces.suspended_banner.contact"));
  });

  it("shows nothing for the verification hold, or in a workspace that runs", () => {
    expect(banner(<SuspendedWorkspaceBanner />, { suspendedReason: "unverified" })).toBe("");
    expect(banner(<SuspendedWorkspaceBanner />, { suspendedReason: null })).toBe("");
  });

  it("shows nothing outside a workspace, such as in the admin area", () => {
    const html = renderAs(
      { instanceAdmin: true, role: "owner" },
      <SuspendedWorkspaceBanner />,
      "/admin/workspaces",
      (queryClient) => {
        queryClient.setQueryData(getMeQueryKey(), {
          data: {
            accountEmails: true,
            supportContact: null,
            user: { id: "u1", email: USER_EMAIL, instanceAdmin: true, emailVerified: true },
            workspaces: [
              { id: WORKSPACE_ID, name: "Main", role: "owner", suspended: true, suspendedReason: "admin" },
            ],
            defaultWorkspaceId: WORKSPACE_ID,
          },
        });
      }
    );
    expect(html).toBe("");
  });

  it("leaves the verification hold to the verification banner, which is unchanged", () => {
    const html = banner(<VerificationBanner />, { suspendedReason: "unverified" });
    expect(html).toContain(text("auth.suspension_notice.message"));
    expect(banner(<VerificationBanner />, { suspendedReason: "admin" })).toBe("");
  });
});
