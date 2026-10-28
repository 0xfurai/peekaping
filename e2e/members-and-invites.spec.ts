import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { admin } from './support/admin';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID } from './support/api';

// An admin invites someone by a link, the invited person signs up through it and works in the
// workspace as a member, makes a workspace of their own, and is removed from the first one (PP-154).

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
const memberPassword = 'MemberPassword1234!';

async function adminWorkspace(api: APIRequestContext): Promise<{ id: string; name: string }> {
  const me = await api.get('me');
  expect(me.ok(), await me.text()).toBeTruthy();
  const { defaultWorkspaceId, workspaces } = (await me.json()).data as {
    defaultWorkspaceId: string;
    workspaces: { id: string; name: string }[];
  };
  expect(defaultWorkspaceId).toMatch(new RegExp(`^${UUID}$`));
  const workspace = workspaces.find((w) => w.id === defaultWorkspaceId);
  expect(workspace, 'the e2e admin is in their default workspace').toBeTruthy();
  return workspace!;
}

// Runs every step even when an earlier one throws, then reports all the failures at once, so a
// failed cleanup step can't leave the ones after it undone.
async function runAll(steps: (() => Promise<void>)[]) {
  const failures: unknown[] = [];
  for (const step of steps) {
    try {
      await step();
    } catch (error) {
      failures.push(error);
    }
  }
  if (failures.length === 1) throw failures[0];
  if (failures.length > 1) throw new AggregateError(failures, `${failures.length} cleanup steps failed`);
}

// Removes what the test left in the admin's workspace if it failed before the UI did: the member
// (who stays a user, with a workspace of their own, since there is no API to delete a user) and
// the invite. Every deletion is tried, whichever of the others fails.
async function removeFromWorkspace(api: APIRequestContext, email: string) {
  await runAll([
    async () => {
      const invites = await api.get('workspace/invites');
      if (!invites.ok()) return;
      const mine = ((await invites.json()).data as { id: string; email: string }[]).filter((i) => i.email === email);
      await runAll(mine.map((invite) => () => deleteCreated(api, `workspace/invites/${invite.id}`)));
    },
    async () => {
      const members = await api.get('workspace/members');
      if (!members.ok()) return;
      const mine = ((await members.json()).data as { userId: string; email: string }[]).filter((m) => m.email === email);
      await runAll(mine.map((member) => () => deleteCreated(api, `workspace/members/${member.userId}`)));
    },
  ]);
}

const switcher = (page: Page) => page.getByRole('button', { name: 'Switch workspace' });

test('an admin invites a member by a link, who works in the workspace, makes their own and is removed', async ({
  page,
  browser,
}) => {
  const run = `${Date.now()}-${randomID()}`;
  const email = `member-${run}@example.com`;
  const monitorName = `Members ${run}`;
  const ownWorkspaceName = `Own ${run}`;

  const api = await adminAPI(page);
  const first = await adminWorkspace(api);
  // Paused, so that nothing checks it
  const monitor = await createHTTPMonitor(api, monitorName, 'http://127.0.0.1:9/', { active: false });
  // Signed out: a context made here would otherwise start with the e2e admin's saved session
  const memberContext = await browser.newContext({ storageState: { cookies: [], origins: [] } });

  try {
    // What the member won't see, the admin does
    await page.goto(`/w/${first.id}/security`);
    await expect(page.getByText('API Keys').first()).toBeVisible();
    await expect(page.getByRole('link', { name: 'Admin', exact: true })).toBeVisible();

    // 1. The admin makes an invite link
    await page.goto(`/w/${first.id}/settings/members`);
    await page.getByRole('textbox', { name: 'Email' }).fill(email);
    await page.getByRole('button', { name: 'Create invite link' }).click();
    const link = page.getByRole('alert').filter({ hasText: 'Invite link' }).getByRole('textbox');
    await expect(link).toHaveValue(/\/invite\/.+/);
    const inviteLink = await link.inputValue();
    expect(new URL(inviteLink).pathname).toMatch(/^\/invite\/[^/]+$/);
    await expect(page.getByRole('row', { name: new RegExp(email) })).toBeVisible();

    // 2. A signed-out visitor opens it, signs up, and is in the workspace
    const member = await memberContext.newPage();
    await member.goto(inviteLink);
    await expect(member.getByText(`Join ${first.name}`).first()).toBeVisible();
    await expect(member.getByLabel('Email')).toHaveValue(email);
    await member.getByLabel('Password', { exact: true }).fill(memberPassword);
    await member.getByRole('button', { name: 'Create account and join' }).click();
    await expect(member).toHaveURL(new RegExp(`/w/${first.id}/monitors$`));
    await expect(member.getByText(monitorName)).toBeVisible();

    // 3. A member has no Admin link, no API keys, no invite or role controls, and no switcher
    await expect(member.getByRole('link', { name: 'Monitors' }).first()).toBeVisible();
    await expect(member.getByRole('link', { name: 'Admin', exact: true })).toHaveCount(0);
    await expect(switcher(member)).toHaveCount(0);

    await member.goto(`/w/${first.id}/security`);
    await expect(member.getByText('Update Password').first()).toBeVisible();
    await expect(member.getByText('API Keys')).toHaveCount(0);

    await member.goto(`/w/${first.id}/settings/members`);
    await expect(member.getByRole('row', { name: new RegExp(email) })).toBeVisible();
    await expect(member.getByRole('row', { name: new RegExp(admin.email) })).toBeVisible();
    await expect(member.getByText('Invite someone')).toHaveCount(0);
    await expect(member.getByRole('button', { name: 'Create invite link' })).toHaveCount(0);
    await expect(member.getByText('Pending invites')).toHaveCount(0);
    await expect(member.getByRole('combobox', { name: /^Change the role of / })).toHaveCount(0);
    await expect(member.getByRole('button', { name: /^Remove / })).toHaveCount(0);
    await expect(switcher(member)).toHaveCount(0);

    // 4. The member makes a workspace of their own; the switcher shows and keeps the two apart
    await member.getByRole('button', { name: 'New workspace' }).click();
    await member.getByRole('dialog').getByRole('textbox', { name: 'Name' }).fill(ownWorkspaceName);
    await member.getByRole('dialog').getByRole('button', { name: 'Create workspace' }).click();
    // A new workspace opens on Get started, with the name just typed; skipping it leaves the monitors list
    await expect(member).toHaveURL(new RegExp(`/w/(?!${first.id})${UUID}/get-started$`));
    const ownWorkspace = new URL(member.url()).pathname.split('/')[2];
    await expect(member.getByRole('textbox', { name: 'Name' })).toHaveValue(ownWorkspaceName);
    await member.getByRole('button', { name: 'Continue' }).click();
    await member.getByRole('link', { name: 'Skip setup' }).click();
    await expect(member).toHaveURL(new RegExp(`/w/${ownWorkspace}/monitors$`));
    await expect(switcher(member)).toContainText(ownWorkspaceName);
    await expect(member.getByText('No monitors found')).toBeVisible();
    await expect(member.getByText(monitorName)).toHaveCount(0);

    await switcher(member).click();
    await member.getByRole('menuitem', { name: first.name }).click();
    await expect(member).toHaveURL(new RegExp(`/w/${first.id}/monitors$`));
    await expect(member.getByText(monitorName)).toBeVisible();

    // 5. The admin removes the member. The member's next move in the first workspace is refused as
    // Workspace not found, and the app takes them to the workspace they made.
    await page.reload();
    await page.getByRole('button', { name: `Remove ${email}` }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Remove' }).click();
    await expect(page.getByRole('row', { name: new RegExp(email) })).toHaveCount(0);

    await member.getByRole('link', { name: 'Proxies' }).click();
    await expect(member).toHaveURL(new RegExp(`/w/${ownWorkspace}/monitors$`));
    await expect(member.getByText('No monitors found')).toBeVisible();
    await expect(member.getByText(monitorName)).toHaveCount(0);
    // /me was fetched again: the first workspace is gone from the switcher, which leaves one
    await expect(switcher(member)).toHaveCount(0);
  } finally {
    // Each step runs even if an earlier one throws; the failures are reported together after
    try {
      await runAll([
        () => memberContext.close(),
        () => deleteCreated(api, `monitors/${monitor.id}`),
        () => removeFromWorkspace(api, email),
      ]);
    } finally {
      await api.dispose();
    }
  }
});
