import { describe, expect, it, vi } from "vitest";
import {
  getWorkspaceInvitesQueryKey,
  getWorkspaceMembersQueryKey,
} from "@/api/@tanstack/react-query.gen";
import { renderAs, text, USER_EMAIL } from "@/test-support/render-with-me";
import MembersPage from "./page";

vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

type Role = "member" | "admin" | "owner";
const member = (userId: string, email: string, role: Role) => ({
  userId,
  email,
  role,
  joinedAt: "2026-10-01T10:00:00Z",
});

// The page as a user with the role sees it, in a workspace with a second owner (dave), an admin
// (carol) and a member (bob), unless the user is the only owner
const members = (role: Role, { secondOwner = true } = {}) =>
  renderAs({ instanceAdmin: false, role }, <MembersPage />, undefined, (queryClient) => {
    queryClient.setQueryData(getWorkspaceMembersQueryKey(), {
      data: [
        member("u1", USER_EMAIL, role),
        member("u2", "bob@example.com", "member"),
        member("u3", "carol@example.com", "admin"),
        ...(secondOwner || role !== "owner" ? [member("u4", "dave@example.com", "owner")] : []),
      ],
    });
    queryClient.setQueryData(getWorkspaceInvitesQueryKey(), {
      data: [
        {
          id: "i1",
          email: "erin@example.com",
          role: "member",
          status: "pending",
          createdAt: "2026-10-27T10:00:00Z",
          expiresAt: "2026-11-03T10:00:00Z",
          invitedBy: "u1",
        },
      ],
    });
  });

const changeRole = (email: string) => `aria-label="Change the role of ${email}"`;
const remove = (email: string) => `aria-label="Remove ${email}"`;

// The options of the select with the name, as the values it offers
const optionsOf = (html: string, name: string): string[] => {
  const select = new RegExp(`<select[^>]*name="${name}"[^>]*>(.*?)</select>`).exec(html)?.[1] ?? "";
  return [...select.matchAll(/<option[^>]*value="([^"]*)"/g)].map((m) => m[1]);
};

// The options of the select with the aria-label
const optionsLabelled = (html: string, label: string): string[] => {
  const select = new RegExp(`<select[^>]*aria-label="${label}"[^>]*>(.*?)</select>`).exec(html)?.[1] ?? "";
  return [...select.matchAll(/<option[^>]*value="([^"]*)"/g)].map((m) => m[1]);
};

describe("MembersPage", () => {
  it("shows an owner the invite form with every role, the pending invites, and a role select and remove for every member", () => {
    const html = members("owner");
    expect(html).toContain(text("members.invite.submit"));
    expect(optionsOf(html, "role")).toEqual(["member", "admin", "owner"]);
    expect(html).toContain("erin@example.com");
    expect(html).toContain(`>${text("members.pending.revoke")}<`);
    for (const email of ["bob@example.com", "carol@example.com", "dave@example.com"]) {
      expect(html).toContain(changeRole(email));
      expect(html).toContain(remove(email));
    }
    expect(optionsLabelled(html, "Change the role of bob@example.com")).toEqual(["member", "admin", "owner"]);
    // Leaving is their own business, so they are not offered removal of themselves
    expect(html).not.toContain(remove(USER_EMAIL));
    expect(html).toContain(text("members.leave.button"));
  });

  it("shows an admin the invite form without the owner role, and no controls on an owner", () => {
    const html = members("admin");
    expect(html).toContain(text("members.invite.submit"));
    expect(optionsOf(html, "role")).toEqual(["member", "admin"]);
    expect(html).toContain(changeRole("bob@example.com"));
    expect(optionsLabelled(html, "Change the role of bob@example.com")).toEqual(["member", "admin"]);
    expect(html).toContain(remove("bob@example.com"));
    expect(html).toContain(remove("carol@example.com"));
    // The owner is listed, with no way to change or remove them
    expect(html).toContain("dave@example.com");
    expect(html).not.toContain(changeRole("dave@example.com"));
    expect(html).not.toContain(remove("dave@example.com"));
  });

  it("shows a member the list and Leave, and none of the controls", () => {
    const html = members("member");
    expect(html).toContain("bob@example.com");
    expect(html).toContain("dave@example.com");
    expect(html).not.toContain(text("members.invite.submit"));
    expect(html).not.toContain(text("members.pending.title"));
    expect(html).not.toContain("erin@example.com");
    expect(html).not.toContain("Change the role of");
    expect(html).not.toContain('aria-label="Remove ');
    expect(html).toContain(text("members.leave.button"));
  });

  it("hides Leave from the last owner and says why", () => {
    const html = members("owner", { secondOwner: false });
    expect(html).not.toContain(text("members.leave.button"));
    // The markup escapes the apostrophe
    expect(html).toContain(text("members.leave.last_owner").replace("'", "&#x27;"));
  });

  it("offers a user with one workspace a New workspace action", () => {
    expect(members("member")).toContain(`>${text("workspaces.new")}<`);
  });
});
