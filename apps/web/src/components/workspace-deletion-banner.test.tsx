import { describe, expect, it, vi } from "vitest";
import { getMeQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, USER_EMAIL, WORKSPACE_ID } from "@/test-support/render-with-me";
import { SuspendedWorkspaceBanner } from "./suspended-workspace-banner";
import { WorkspaceDeletionBanner } from "./workspace-deletion-banner";

// The time zone the user picked in settings; a test sets it before it renders
let selectedTimezone = "UTC";
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: selectedTimezone, setTimezone: () => {} }),
}));

// The banner in the workspace of the address, with /me as the server answers it
const banner = (
  ui: React.ReactElement,
  opts: { role: "owner" | "admin" | "member"; deletionScheduledAt: string | null; suspendedReason?: string | null }
) =>
  renderAs({ instanceAdmin: false, role: opts.role }, ui, undefined, (queryClient) => {
    const reason = opts.suspendedReason ?? (opts.deletionScheduledAt ? "deleting" : null);
    queryClient.setQueryData(getMeQueryKey(), {
      data: {
        accountEmails: true,
        supportContact: null,
        user: { id: "u1", email: USER_EMAIL, instanceAdmin: false, emailVerified: true },
        workspaces: [
          {
            id: WORKSPACE_ID,
            name: "Main",
            role: opts.role,
            suspended: reason !== null,
            suspendedReason: reason,
            deletionScheduledAt: opts.deletionScheduledAt,
          },
        ],
        defaultWorkspaceId: WORKSPACE_ID,
      },
    });
  });

// Noon UTC, so that the date is the same day in every time zone but the farthest east
const AT = "2026-11-05T12:00:00Z";
// As the markup has it, apostrophes escaped
const escaped = (key: string, vars: Record<string, string> = {}) =>
  Object.entries(vars)
    .reduce((message, [name, value]) => message.replace(`{{${name}}}`, value), text(key))
    .replace(/'/g, "&#x27;");
const DATE = "November 5, 2026 at \\d+:00\\s[AP]M";

describe("WorkspaceDeletionBanner", () => {
  it("tells an owner the date and offers Cancel deletion", () => {
    const html = banner(<WorkspaceDeletionBanner />, { role: "owner", deletionScheduledAt: AT });
    expect(html).toMatch(new RegExp(escaped("workspaces.deletion.message", { date: DATE })));
    expect(html).toContain(">" + text("workspaces.deletion.cancel") + "<");
  });

  // 23:30 UTC on Nov 5 is already Nov 6 in Tokyo, whatever zone the browser is in
  it("tells the date in the time zone the user picked in settings", () => {
    selectedTimezone = "Asia/Tokyo";
    try {
      const html = banner(<WorkspaceDeletionBanner />, { role: "owner", deletionScheduledAt: "2026-11-05T23:30:00Z" });
      expect(html).toContain("November 6, 2026 at 8:30");
    } finally {
      selectedTimezone = "UTC";
    }
  });

  it("tells an admin and a member the date, says an owner can cancel it, and offers no button", () => {
    for (const role of ["admin", "member"] as const) {
      const html = banner(<WorkspaceDeletionBanner />, { role, deletionScheduledAt: AT });
      expect(html).toMatch(new RegExp(escaped("workspaces.deletion.member_message", { date: DATE })));
      expect(html).not.toContain("<button");
    }
  });

  it("shows nothing in a workspace with no deletion scheduled", () => {
    expect(banner(<WorkspaceDeletionBanner />, { role: "owner", deletionScheduledAt: null })).toBe("");
  });

  it("shows nothing outside a workspace, such as in the admin area", () => {
    const html = renderAs(
      { instanceAdmin: true, role: "owner" },
      <WorkspaceDeletionBanner />,
      "/admin/workspaces",
      (queryClient) => {
        queryClient.setQueryData(getMeQueryKey(), {
          data: {
            user: { id: "u1", email: USER_EMAIL, instanceAdmin: true },
            workspaces: [
              {
                id: WORKSPACE_ID,
                name: "Main",
                role: "owner",
                suspended: true,
                suspendedReason: "deleting",
                deletionScheduledAt: AT,
              },
            ],
            defaultWorkspaceId: WORKSPACE_ID,
          },
        });
      }
    );
    expect(html).toBe("");
  });

  // The deletion is told before the admin's suspension, so the two banners never show together
  it("leaves the suspension banner with nothing to say while the deletion is the reason", () => {
    const html = banner(<SuspendedWorkspaceBanner />, { role: "owner", deletionScheduledAt: AT });
    expect(html).toBe("");
  });
});
