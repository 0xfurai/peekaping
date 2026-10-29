import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { getAdminUsersQueryKey } from "@/api/@tanstack/react-query.gen";
import type { AdminUserResponse } from "@/api/types.gen";
import { renderAs, text, USER_EMAIL } from "@/test-support/render-with-me";
import i18n from "@/i18n";
import { AxiosError, AxiosHeaders } from "axios";
import { adminErrorMessage } from "@/lib/admin-users";
import UsersPage from "./page";
import { UserActionDialogContent } from "./user-action-dialog";
import { ResetLinkContent } from "./reset-link-dialog";

vi.mock("@/lib/config", () => ({ getConfig: () => ({ API_URL: "" }) }));
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

const user = (over: Partial<AdminUserResponse>): AdminUserResponse => ({
  id: "u2",
  email: "bob@example.com",
  active: true,
  instanceAdmin: false,
  twoFactorEnabled: false,
  emailVerified: true,
  createdAt: "2026-10-01T10:00:00Z",
  lastLoginAt: "2026-10-27T10:00:00Z",
  workspaces: [],
  ...over,
});

const USERS = [
  user({
    id: "u1",
    email: USER_EMAIL,
    instanceAdmin: true,
    twoFactorEnabled: true,
    workspaces: [{ id: "w1", name: "Main", role: "owner" }],
  }),
  user({
    id: "u2",
    email: "bob@example.com",
    workspaces: [
      { id: "w1", name: "Main", role: "member" },
      { id: "w2", name: "Side project", role: "admin" },
    ],
  }),
  user({ id: "u3", email: "carol@example.com", active: false, emailVerified: false, lastLoginAt: null }),
];

const page = () =>
  renderAs({ instanceAdmin: true, role: "owner" }, <UsersPage />, "/admin/users", (queryClient) => {
    queryClient.setQueryData(getAdminUsersQueryKey({ query: { page: 0, limit: 20 } }), {
      data: { users: USERS, total: USERS.length, page: 0, limit: 20 },
    });
  });

describe("UsersPage", () => {
  it("lists every user with email, workspaces and role in each, flag, status, 2FA, dates and verification", () => {
    const html = page();
    for (const email of [USER_EMAIL, "bob@example.com", "carol@example.com"]) {
      expect(html).toContain(email);
    }
    expect(html).toContain("Main");
    expect(html).toContain(`Side project (${text("members.roles.admin")})`);
    expect(html).toContain(`Main (${text("members.roles.member")})`);
    expect(html).toContain(`Main (${text("members.roles.owner")})`);
    for (const column of ["email", "workspaces", "instance_admin", "status", "two_factor", "created", "last_sign_in", "email_verified"]) {
      expect(html).toContain(text(`admin.users.columns.${column}`));
    }
    expect(html).toContain(text("admin.users.values.disabled"));
    expect(html).toContain(text("admin.users.values.two_factor_on"));
    // Carol never signed in; the others did on 10/27/2026
    expect(html).toContain(text("admin.users.never"));
    expect(html).toContain("10/27/2026");
    expect(html).toContain("10/01/2026");
  });

  it("marks your own row and has a search box and an actions menu per row", () => {
    const html = page();
    expect(html).toContain(`(${text("admin.users.you")})`);
    expect(html).toContain(`aria-label="${text("admin.users.search_label")}"`);
    for (const email of [USER_EMAIL, "bob@example.com", "carol@example.com"]) {
      expect(html).toContain(`aria-label="Actions for ${email}"`);
    }
  });

  it("shows no paging controls when everything fits on one page", () => {
    expect(page()).not.toContain(text("admin.users.paging.next"));
  });

  it("pages a list that doesn't fit", () => {
    const html = renderAs({ instanceAdmin: true, role: "owner" }, <UsersPage />, "/admin/users", (queryClient) => {
      queryClient.setQueryData(getAdminUsersQueryKey({ query: { page: 0, limit: 20 } }), {
        data: { users: USERS, total: 45, page: 0, limit: 20 },
      });
    });
    expect(html).toContain("Page 1 of 3");
    expect(html).toContain(text("admin.users.paging.next"));
  });
});

const render = (ui: React.ReactElement) =>
  renderToStaticMarkup(<MemoryRouter>{ui}</MemoryRouter>);

describe("UserActionDialogContent", () => {
  const base = { kind: "delete" as const, email: "bob@example.com", pending: false, onCancel: () => {}, onConfirm: () => {} };

  it("asks to confirm the delete, naming the user", () => {
    const html = render(<UserActionDialogContent {...base} error={null} />);
    expect(html).toContain("Delete bob@example.com?");
    expect(html).toContain(text("admin.users.confirm.delete.confirm"));
    expect(html).not.toContain('role="alert"');
  });

  it("lets the admin cancel while nothing is pending", () => {
    const html = render(<UserActionDialogContent {...base} error={null} />);
    expect(html).not.toContain('disabled=""');
  });

  it("disables Cancel and the confirm button while the action is pending", () => {
    const html = render(<UserActionDialogContent {...base} pending error={null} />);
    const buttons = html.match(/<button[^>]*>/g) ?? [];
    expect(buttons).toHaveLength(2);
    for (const button of buttons) expect(button).toContain('disabled=""');
  });

  it("shows the server's 409, with the workspaces that block the delete", () => {
    const config = { headers: new AxiosHeaders() };
    const refusal = new AxiosError("Request failed", "ERR_BAD_REQUEST", config, null, {
      status: 409,
      statusText: "Conflict",
      headers: {},
      config,
      data: {
        error: {
          code: "LAST_OWNER",
          workspaces: [
            { id: "w1", name: "Main" },
            { id: "w2", name: "Side project" },
          ],
        },
      },
    });
    const error = adminErrorMessage(refusal, i18n.t.bind(i18n), "failed");
    const html = render(<UserActionDialogContent {...base} error={error} />);
    expect(html).toContain('role="alert"');
    expect(html).toContain("is the last owner of");
    expect(html).toContain("&quot;Main&quot;, &quot;Side project&quot;");
  });
});

describe("ResetLinkContent", () => {
  const link = "https://peekaping.example/reset-password?token=abc123";
  const base = { email: "bob@example.com", accountEmails: false, onClose: () => {} };

  it("shows the link, its expiry and that using it signs the user out everywhere", () => {
    const html = render(
      <ResetLinkContent {...base} reset={{ link, expiresAt: "2026-10-28T13:00:00Z" }} />
    );
    expect(html).toContain(link);
    expect(html).toContain(text("admin.users.reset_link.copy"));
    expect(html).toContain("10/28/2026");
    expect(html).toContain(text("admin.users.reset_link.signs_out"));
    expect(html).not.toContain(text("admin.users.reset_link.emailed"));
  });

  it("says the user is told by email when the install sends account emails", () => {
    const html = render(
      <ResetLinkContent {...base} accountEmails reset={{ link, expiresAt: "2026-10-28T13:00:00Z" }} />
    );
    expect(html).toContain(text("admin.users.reset_link.emailed"));
  });

  it("shows no link while it is made, and none once the dialog was closed", () => {
    const html = render(<ResetLinkContent {...base} reset={null} />);
    expect(html).toContain(text("admin.users.reset_link.making"));
    expect(html).not.toContain("token=");
  });
});
