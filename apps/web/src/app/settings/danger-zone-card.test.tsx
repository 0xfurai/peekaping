import { describe, expect, it, vi } from "vitest";
import { getMeQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, USER_EMAIL, WORKSPACE_ID } from "@/test-support/render-with-me";
import { DangerZoneCard } from "./danger-zone-card";

vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "Asia/Tokyo", setTimezone: () => {} }),
}));

describe("DangerZoneCard", () => {
  // 23:30 UTC on Nov 5 is already Nov 6 in Tokyo, whatever zone the browser is in
  it("tells the scheduled date in the time zone the user picked in settings", () => {
    const html = renderAs({ instanceAdmin: false, role: "owner" }, <DangerZoneCard />, undefined, (queryClient) => {
      queryClient.setQueryData(getMeQueryKey(), {
        data: {
          user: { id: "u1", email: USER_EMAIL, instanceAdmin: false },
          workspaces: [
            {
              id: WORKSPACE_ID,
              name: "Main",
              role: "owner",
              suspended: true,
              suspendedReason: "deleting",
              deletionScheduledAt: "2026-11-05T23:30:00Z",
            },
          ],
          defaultWorkspaceId: WORKSPACE_ID,
        },
      });
    });
    expect(html).toContain("November 6, 2026 at 8:30");
  });
});
