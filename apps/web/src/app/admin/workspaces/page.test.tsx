import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import { getAdminWorkspacesQueryKey } from "@/api/@tanstack/react-query.gen";
import type { AdminWorkspaceResponse } from "@/api/types.gen";
import { renderAs, text } from "@/test-support/render-with-me";
import WorkspacesPage from "./page";
import { limitsDraft } from "@/lib/admin-workspaces";
import { WorkspaceActionDialogContent } from "./workspace-action-dialog";
import { WorkspaceLimitsDialogContent } from "./workspace-limits-dialog";

vi.mock("@/lib/config", () => ({ getConfig: () => ({ API_URL: "" }) }));
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

const NO_LIMITS: AdminWorkspaceResponse["limits"]["values"] = {
  monitors: null,
  min_check_interval: null,
  status_pages: null,
  custom_domains: null,
  members: null,
  owned_workspaces: null,
  probes_per_monitor: null,
  retention_days: null,
};

const limits = (
  source: AdminWorkspaceResponse["limits"]["source"],
  values: Partial<AdminWorkspaceResponse["limits"]["values"]> = {}
): AdminWorkspaceResponse["limits"] => ({ source, values: { ...NO_LIMITS, ...values } });

const workspace = (over: Partial<AdminWorkspaceResponse>): AdminWorkspaceResponse =>
  ({
    id: "w1",
    name: "Main",
    createdAt: "2026-10-01T10:00:00Z",
    owners: ["alice@example.com"],
    memberCount: 1,
    monitors: { active: 0, paused: 0 },
    statusPages: 0,
    customDomains: 0,
    suspended: false,
    // The server answers null for a workspace that runs
    suspendedReason: null,
    suspendedAt: null,
    limits: limits("none"),
    ...over,
  }) as AdminWorkspaceResponse;

const WORKSPACES = [
  workspace({
    id: "w1",
    name: "Main",
    owners: ["alice@example.com", "dana@example.com"],
    memberCount: 4,
    monitors: { active: 12, paused: 3 },
    statusPages: 2,
    customDomains: 1,
    limits: limits("install", { monitors: 10, status_pages: 2, members: 5, owned_workspaces: 3 }),
  }),
  workspace({
    id: "w2",
    name: "Spam farm",
    createdAt: "2026-10-27T10:00:00Z",
    owners: ["mallory@example.com"],
    memberCount: 1,
    monitors: { active: 0, paused: 250 },
    suspended: true,
    suspendedReason: "admin",
    suspendedAt: "2026-10-27T12:00:00Z",
    limits: limits("workspace"),
  }),
  workspace({
    id: "w3",
    name: "New signup",
    owners: [],
    suspended: true,
    suspendedReason: "unverified",
    suspendedAt: "2026-10-27T12:00:00Z",
  }),
];

const page = (total = WORKSPACES.length) =>
  renderAs({ instanceAdmin: true, role: "owner" }, <WorkspacesPage />, "/admin/workspaces", (queryClient) => {
    queryClient.setQueryData(getAdminWorkspacesQueryKey({ query: { page: 0, limit: 20 } }), {
      data: { workspaces: WORKSPACES, total, page: 0, limit: 20 },
    });
  });

// A key's English text as the markup escapes it
const escaped = (key: string) => text(key).replace(/'/g, "&#x27;");

const cells = (html: string) =>
  [...html.matchAll(/<td[^>]*>(.*?)<\/td>/g)].map((match) => match[1].replace(/<[^>]+>/g, ""));

describe("WorkspacesPage", () => {
  it("lists every workspace with name, created, owners, members, monitors, status pages and custom domains", () => {
    const html = page();
    for (const column of ["name", "created", "owners", "members", "monitors", "status_pages", "custom_domains", "limits", "status"]) {
      expect(html).toContain(text(`admin.workspaces.columns.${column}`));
    }
    // The first row, cell by cell: name, created, owners, members, monitors, status pages,
    // custom domains, limits, status, and the actions menu
    expect(cells(html).slice(0, 10)).toEqual([
      "Main",
      "10/01/2026",
      "alice@example.comdana@example.com",
      "4",
      "12 active, 3 paused",
      "2",
      "1",
      `Active monitors: 10, Status pages: 2, +1 more${escaped("admin.workspaces.limits.source.install")}`,
      text("admin.workspaces.status.running"),
      "",
    ]);
    expect(html).toContain("0 active, 250 paused");
    expect(html).toContain("10/27/2026");
  });

  it("shows a suspended badge with its reason, and None for a workspace without an owner", () => {
    const html = page();
    expect(html).toContain(text("admin.workspaces.status.suspended_admin"));
    expect(html).toContain(text("admin.workspaces.status.suspended_unverified"));
    // The third row's owners
    expect(cells(html)[10 * 2 + 2]).toBe(text("admin.workspaces.none"));
  });

  it("shows a workspace's own limits of none as no limits of its own, and one without any as none", () => {
    const html = page();
    // The second row is unlimited by its own limits, the third has none from anywhere
    expect(cells(html)[10 + 7]).toBe(
      `${text("admin.workspaces.limits.unlimited")}${text("admin.workspaces.limits.source.workspace")}`
    );
    expect(cells(html)[10 * 2 + 7]).toBe(
      `${text("admin.workspaces.limits.unlimited")}${text("admin.workspaces.limits.source.none")}`
    );
  });

  it("has a search box for a name or an owner's email, and an actions menu per row", () => {
    const html = page();
    expect(html).toContain(`aria-label="${text("admin.workspaces.search_label")}"`);
    expect(html).toContain(`placeholder="${text("admin.workspaces.search_placeholder")}"`);
    for (const name of ["Main", "Spam farm", "New signup"]) {
      expect(html).toContain(`aria-label="Actions for ${name}"`);
    }
    expect(html).toContain("3 found");
  });

  it("shows no paging controls when everything fits on one page", () => {
    expect(page()).not.toContain(text("admin.workspaces.paging.next"));
  });

  it("pages a list that doesn't fit", () => {
    const html = page(45);
    expect(html).toContain("Page 1 of 3");
    expect(html).toContain(text("admin.workspaces.paging.next"));
  });
});

const render = (ui: React.ReactElement) => renderToStaticMarkup(<MemoryRouter>{ui}</MemoryRouter>);

const buttons = (html: string) => html.match(/<button[^>]*>.*?<\/button>/g) ?? [];
const confirmButton = (html: string, label: string) =>
  buttons(html).find((button) => button.includes(`>${label}<`)) ?? "";

describe("WorkspaceActionDialogContent", () => {
  const base = {
    name: "Spam farm",
    typed: "",
    onTypedChange: () => {},
    pending: false,
    error: null,
    onCancel: () => {},
    onConfirm: () => {},
  };

  it("asks to confirm a suspend and a resume, naming the workspace", () => {
    const suspend = render(<WorkspaceActionDialogContent {...base} kind="suspend" />);
    expect(suspend).toContain("The monitors of Spam farm stop running");
    expect(confirmButton(suspend, text("admin.workspaces.confirm.suspend.confirm"))).not.toContain('disabled=""');
    const resume = render(<WorkspaceActionDialogContent {...base} kind="resume" />);
    expect(resume).toContain("The monitors of Spam farm run again");
    expect(confirmButton(resume, text("admin.workspaces.confirm.resume.confirm"))).not.toContain('disabled=""');
  });

  it("asks for the workspace's name before a delete, and keeps Delete off until it matches", () => {
    const label = text("admin.workspaces.confirm.delete.confirm");
    const empty = render(<WorkspaceActionDialogContent {...base} kind="delete" />);
    expect(empty).toContain("Type the workspace&#x27;s name to confirm: Spam farm");
    expect(empty).toContain("<input");
    expect(confirmButton(empty, label)).toContain('disabled=""');

    const wrong = render(<WorkspaceActionDialogContent {...base} kind="delete" typed="spam farm" />);
    expect(confirmButton(wrong, label)).toContain('disabled=""');

    const matching = render(<WorkspaceActionDialogContent {...base} kind="delete" typed="Spam farm" />);
    expect(confirmButton(matching, label)).not.toContain('disabled=""');
  });

  it("disables Cancel, the confirm button and the name while the action is pending", () => {
    const html = render(
      <WorkspaceActionDialogContent {...base} kind="delete" typed="Spam farm" pending />
    );
    expect(buttons(html)).toHaveLength(2);
    for (const button of buttons(html)) expect(button).toContain('disabled=""');
    expect(html).toMatch(/<input[^>]*disabled=""/);
  });

  it("shows what went wrong with the last try", () => {
    const html = render(
      <WorkspaceActionDialogContent {...base} kind="suspend" error="Couldn't change the workspace." />
    );
    expect(html).toContain('role="alert"');
    expect(html).toContain("Couldn&#x27;t change the workspace.");
  });
});

describe("WorkspaceLimitsDialogContent", () => {
  const base = {
    invalid: [],
    pending: false,
    error: null,
    onDraftChange: () => {},
    onCancel: () => {},
    onSave: () => {},
  };
  const radio = (html: string, choice: string) =>
    html.match(new RegExp(`<input[^>]*value="${choice}"[^>]*>`))?.[0] ?? "";

  it("offers its own limits, no limits and the install's defaults", () => {
    const html = render(<WorkspaceLimitsDialogContent {...base} draft={limitsDraft(limits("install", { monitors: 10 }))} />);
    for (const choice of ["own", "unlimited", "defaults"]) {
      expect(html).toContain(escaped(`admin.workspaces.limits.dialog.${choice}`));
    }
    expect(radio(html, "defaults")).toContain('checked=""');
    expect(radio(html, "own")).not.toContain('checked=""');
    // No fields until it gets its own
    expect(html).not.toContain(text("limits.names.monitors"));
  });

  it("shows a field for each limit of its own, blank for none, and no owned workspaces", () => {
    const html = render(
      <WorkspaceLimitsDialogContent {...base} draft={limitsDraft(limits("workspace", { monitors: 50, retention_days: 30 }))} />
    );
    expect(radio(html, "own")).toContain('checked=""');
    for (const name of ["monitors", "min_check_interval", "status_pages", "custom_domains", "members", "probes_per_monitor", "retention_days"]) {
      expect(html).toContain(text(`limits.names.${name}`));
    }
    expect(html).not.toContain(text("limits.names.owned_workspaces"));
    expect(html).toMatch(/<input[^>]*type="number"[^>]*value="50"/);
    expect(html).toContain(`placeholder="${text("limits.none")}"`);
  });

  it("checks No limits for a workspace whose own limits are none", () => {
    const html = render(<WorkspaceLimitsDialogContent {...base} draft={limitsDraft(limits("workspace"))} />);
    expect(radio(html, "unlimited")).toContain('checked=""');
  });

  it("marks a limit that is no whole number of 1 or more", () => {
    const draft = limitsDraft(limits("workspace", { monitors: 5 }));
    const html = render(<WorkspaceLimitsDialogContent {...base} draft={draft} invalid={["monitors"]} />);
    expect(html).toMatch(/<input[^>]*aria-invalid="true"[^>]*value="5"/);
    expect(html).toContain(text("limits.invalid"));
  });

  it("disables everything while the save is pending, and shows what went wrong", () => {
    const html = render(
      <WorkspaceLimitsDialogContent {...base} draft={limitsDraft(limits("none"))} pending error="Couldn't save the limits." />
    );
    for (const button of buttons(html)) expect(button).toContain('disabled=""');
    expect(html).toMatch(/<fieldset[^>]*disabled=""/);
    expect(html).toContain('role="alert"');
  });
});
