import { describe, expect, it, vi } from "vitest";
import { getMeQueryKey, getMonitorsQueryKey } from "@/api/@tanstack/react-query.gen";
import { renderAs, text, USER_EMAIL, WORKSPACE_ID } from "@/test-support/render-with-me";
import GetStartedPage from "./page";
import { ChannelStep } from "./channel-step";
import { DoneStep } from "./done-step";
import { MonitorStep } from "./monitor-step";
import { NameStep } from "./name-step";

// A static render runs no effects, so a redirect would show nothing. Show where it goes.
vi.mock("react-router-dom", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router-dom")>()),
  Navigate: ({ to }: { to: string }) => <span data-redirect={to} />,
}));

vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

type Access = Parameters<typeof renderAs>[0];
const owner: Access = { instanceAdmin: false, role: "owner" };

// /me as the server answers it, for a workspace that may wait for its owner's email verification
const withMe = (opts: { accountEmails?: boolean; instanceAdmin?: boolean; suspendedReason?: string | null; role?: "member" | "admin" | "owner" }) =>
  (queryClient: import("@tanstack/react-query").QueryClient) => {
    queryClient.setQueryData(getMeQueryKey(), {
      data: {
        accountEmails: opts.accountEmails ?? false,
        user: { id: "u1", email: USER_EMAIL, instanceAdmin: opts.instanceAdmin ?? false, emailVerified: true },
        workspaces: [
          {
            id: WORKSPACE_ID,
            name: "My workspace",
            role: opts.role ?? "owner",
            suspended: opts.suspendedReason != null,
            suspendedReason: opts.suspendedReason ?? null,
          },
        ],
        defaultWorkspaceId: WORKSPACE_ID,
      },
    });
  };

const page = (monitors: unknown[] | undefined, access: Access = owner) =>
  renderAs(access, <GetStartedPage />, `/w/${WORKSPACE_ID}/get-started`, (queryClient) => {
    if (monitors) queryClient.setQueryData(getMonitorsQueryKey({ query: { limit: 1 } }), { data: monitors });
  });

describe("Get started page", () => {
  it("shows a loader until the workspace's monitors are known", () => {
    const html = page(undefined);
    expect(html).toContain(text("common.loading"));
    expect(html).not.toContain(text("get_started.name.title"));
    expect(html).not.toContain("data-redirect");
  });

  it("opens on the workspace's name when the workspace has no monitor", () => {
    const html = page([]);
    expect(html).toContain(text("get_started.name.title"));
    expect(html).not.toContain("data-redirect");
  });

  it("sends a workspace that already has a monitor to the monitors list", () => {
    const html = page([{ id: "m1" }]);
    expect(html).toContain(`data-redirect="/w/${WORKSPACE_ID}/monitors"`);
    expect(html).not.toContain(text("get_started.name.title"));
  });

  it("starts a member, who can't rename the workspace, at the monitor", () => {
    const html = page([], { instanceAdmin: false, role: "member" });
    expect(html).toContain(text("get_started.monitor.title"));
    expect(html).not.toContain(text("get_started.name.title"));
  });
});

describe("step 1, the name", () => {
  it("shows the workspace's current name, with Continue", () => {
    const html = renderAs(owner, <NameStep onDone={() => {}} />, undefined, withMe({}));
    expect(html).toMatch(/<input[^>]*value="My workspace"/);
    expect(html).toContain(`>${text("common.continue")}<`);
    expect(html).toContain("Step 1 of 4");
  });
});

describe("step 2, the monitor", () => {
  const html = renderAs(owner, <MonitorStep onCreated={() => {}} />);

  it("asks for an address and creates the monitor from it", () => {
    expect(html).toContain(text("get_started.monitor.url_label"));
    expect(html).toContain(`>${text("get_started.monitor.create")}<`);
    expect(html).toContain("Step 2 of 4");
  });

  it("links to the full monitor form", () => {
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/monitors/new"`);
    expect(html).toContain(text("get_started.monitor.full_form"));
  });

  it("can be skipped, to the monitors list", () => {
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/monitors"`);
    expect(html).toContain(`>${text("get_started.skip")}<`);
  });
});

describe("step 3, the channel", () => {
  const channel = (accountEmails: boolean) =>
    renderAs(owner, <ChannelStep monitorId="m1" onSaved={() => {}} />, undefined, withMe({ accountEmails }));

  it("starts on Email, whose form picks a recipient, when the install has a sender", () => {
    const html = channel(true);
    expect(html).toContain(text("notifications.form.install_email.to_label"));
    expect(html).not.toContain(text("notifications.form.telegram.bot_token_label"));
  });

  it("starts on Telegram without a sender", () => {
    const html = channel(false);
    expect(html).toContain(text("notifications.form.telegram.bot_token_label"));
    expect(html).not.toContain(text("notifications.form.install_email.to_label"));
  });

  it("can be skipped, to the monitors list", () => {
    const html = channel(false);
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/monitors"`);
    expect(html).toContain(`>${text("get_started.skip")}<`);
  });
});

describe("step 4, what next", () => {
  const done = (monitorId: string | null, opts: { instanceAdmin?: boolean; suspendedReason?: string | null } = {}) =>
    renderAs(
      { instanceAdmin: opts.instanceAdmin ?? false, role: "owner" },
      <DoneStep monitorId={monitorId} />,
      undefined,
      withMe({ suspendedReason: opts.suspendedReason, instanceAdmin: opts.instanceAdmin })
    );

  it("links to the new monitor, status pages and members", () => {
    const html = done("m1");
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/monitors/m1"`);
    expect(html).toContain(text("get_started.done.links.monitor"));
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/status-pages"`);
    expect(html).toContain(`href="/w/${WORKSPACE_ID}/settings/members"`);
    expect(html).not.toContain("/admin/probes");
  });

  it("goes to the monitors list when step 2 made no monitor", () => {
    const html = done(null);
    expect(html).toContain(text("get_started.done.links.monitors"));
    expect(html).not.toContain(text("get_started.done.links.monitor"));
  });

  it("adds the admin area's probes for an instance admin", () => {
    const html = done("m1", { instanceAdmin: true });
    expect(html).toContain('href="/admin/probes"');
    expect(html).toContain(text("get_started.done.links.probes"));
  });

  it("says the monitor runs once the email is verified, in a workspace that waits for it", () => {
    expect(done("m1", { suspendedReason: "unverified" })).toContain(text("get_started.done.unverified"));
  });

  it("says nothing of verification in a workspace that isn't held back, or is held back for another reason", () => {
    expect(done("m1")).not.toContain(text("get_started.done.unverified"));
    expect(done("m1", { suspendedReason: "billing" })).not.toContain(text("get_started.done.unverified"));
  });
});
