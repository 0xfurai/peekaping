import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { admin } from './support/admin';
import { adminAPI, deleteCreated, randomID } from './support/api';

// An owner deletes their workspace by typing its name: it is held for 7 days with a banner for its
// members, and the owner cancels it (PP-170). A user deletes their own account after entering their
// password, which is refused while they are the only owner of a workspace that isn't scheduled for
// deletion, and for the last instance admin.

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
const memberPassword = 'MemberPassword1234!';

async function createWorkspace(api: APIRequestContext, name: string): Promise<string> {
  const response = await api.post('workspaces', { data: { name } });
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()).data.id;
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

// The banner and the settings card both offer Cancel deletion, so the tests name the banner
const banner = (page: Page) => page.getByRole('alert').filter({ hasText: 'will be deleted on' });

// Deletes the account of the email if the test left it, and its invite if it never signed up. A
// listing that fails fails the test rather than skip its deletions unseen.
async function removeUser(api: APIRequestContext, email: string) {
  const users = await api.get('admin/users', { params: { q: email } });
  expect(users.ok(), `listing the users to clean up: ${await users.text()}`).toBeTruthy();
  const mine = ((await users.json()).data.users as { id: string; email: string }[]).filter((u) => u.email === email);
  await runAll(mine.map((user) => () => deleteCreated(api, `admin/users/${user.id}`)));
  const invites = await api.get('workspace/invites');
  expect(invites.ok(), `listing the invites to clean up: ${await invites.text()}`).toBeTruthy();
  const left = ((await invites.json()).data as { id: string; email: string }[]).filter((i) => i.email === email);
  await runAll(left.map((invite) => () => deleteCreated(api, `workspace/invites/${invite.id}`)));
}

test('@critical an owner schedules the deletion of their workspace by typing its name, sees the banner and cancels it', async ({
  page,
}) => {
  const name = `Doomed ${Date.now()}-${randomID()}`;
  const api = await adminAPI(page);
  const id = await createWorkspace(api, name);

  try {
    await page.goto(`/w/${id}/settings`);
    await expect(page.getByText('Danger zone')).toBeVisible();
    await expect(banner(page)).toHaveCount(0);

    // The button waits for the name, typed exactly
    await page.getByRole('button', { name: 'Delete workspace' }).click();
    const dialog = page.getByRole('dialog');
    const confirm = dialog.getByRole('button', { name: 'Delete workspace' });
    await expect(confirm).toBeDisabled();
    await dialog.getByRole('textbox').fill(name.toLowerCase());
    await expect(confirm).toBeDisabled();
    await dialog.getByRole('textbox').fill(name);
    await expect(confirm).toBeEnabled();
    await confirm.click();

    // The owner sees the date in the banner and on the card, which offers Cancel deletion instead of Delete
    await expect(dialog).toHaveCount(0);
    await expect(banner(page)).toContainText(/will be deleted on .*20\d\d/);
    await expect(banner(page).getByRole('button', { name: 'Cancel deletion' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Delete workspace' })).toHaveCount(0);

    // The hold survives a reload, since /me carries it
    await page.reload();
    await expect(banner(page)).toBeVisible();

    await banner(page).getByRole('button', { name: 'Cancel deletion' }).click();
    await expect(banner(page)).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Delete workspace' })).toBeVisible();
    const me = await (await api.get('me')).json();
    const workspace = me.data.workspaces.find((w: { id: string }) => w.id === id);
    expect(workspace.deletionScheduledAt).toBeNull();
    expect(workspace.suspended).toBe(false);
  } finally {
    try {
      await deleteCreated(api, `admin/workspaces/${id}`);
    } finally {
      await api.dispose();
    }
  }
});

test('@critical the last instance admin cannot delete their account', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL(new RegExp(`/w/${UUID}/monitors$`));
  await page.goto(page.url().replace(/\/monitors$/, '/security'));
  await expect(page).toHaveURL(new RegExp(`/w/${UUID}/security$`));

  const deleteButton = page.getByRole('button', { name: 'Delete account' });
  await expect(deleteButton).toBeDisabled();
  await page.getByLabel('Your password').fill(admin.password);
  await deleteButton.click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete account' }).click();

  await expect(page.getByRole('alert').filter({ hasText: 'last instance admin' })).toBeVisible();
  // Nothing happened: the admin is still signed in, on the same page
  await expect(page).toHaveURL(new RegExp(`/w/${UUID}/security$`));
});

test('@critical a user deletes their own account once their workspace is scheduled for deletion', async ({
  page,
  browser,
}) => {
  const run = `${Date.now()}-${randomID()}`;
  const email = `leaving-${run}@example.com`;
  const ownName = `Leaving ${run}`;
  const api = await adminAPI(page);
  const memberContext = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  let ownId: string | undefined;

  try {
    // An invited member signs up, and makes a workspace of their own
    const invited = await api.post('workspace/invites', { data: { email, role: 'member' } });
    expect(invited.status(), await invited.text()).toBe(201);
    const inviteLink = (await invited.json()).data.link as string;
    const member = await memberContext.newPage();
    await member.goto(new URL(inviteLink).pathname);
    await member.getByLabel('Password', { exact: true }).fill(memberPassword);
    await member.getByRole('button', { name: 'Create account and join' }).click();
    await expect(member).toHaveURL(new RegExp(`/w/${UUID}/monitors$`));
    const memberAPI = await adminAPI(member);
    ownId = await createWorkspace(memberAPI, ownName);
    await memberAPI.dispose();

    const deleteAccount = async () => {
      await member.goto(`/w/${ownId}/security`);
      await member.getByLabel('Your password').fill(memberPassword);
      await member.getByRole('button', { name: 'Delete account' }).click();
      await member.getByRole('dialog').getByRole('button', { name: 'Delete account' }).click();
    };

    // 1. Refused: they are the only owner of a workspace that isn't scheduled for deletion
    await deleteAccount();
    const refusal = member.getByRole('alert').filter({ hasText: 'only owner' });
    await expect(refusal).toContainText(`"${ownName}"`);
    await expect(refusal).toContainText('Make another member an owner, or delete the workspace first');
    await expect(member).toHaveURL(new RegExp(`/w/${ownId}/security$`));

    // 2. They schedule the deletion of that workspace, which no longer holds the account back
    await member.goto(`/w/${ownId}/settings`);
    await member.getByRole('button', { name: 'Delete workspace' }).click();
    await member.getByRole('dialog').getByRole('textbox').fill(ownName);
    await member.getByRole('dialog').getByRole('button', { name: 'Delete workspace' }).click();
    await expect(banner(member)).toBeVisible();

    await deleteAccount();
    await expect(member).toHaveURL(/\/login$/);

    // 3. The account is gone: the password no longer signs in
    const login = await api.post('auth/login', { data: { email, password: memberPassword } });
    expect(login.ok()).toBeFalsy();
  } finally {
    // The workspace stays after its owner is gone, held for 7 days: an instance admin deletes it
    try {
      await runAll([
        () => memberContext.close(),
        async () => {
          if (ownId) await deleteCreated(api, `admin/workspaces/${ownId}`);
        },
        () => removeUser(api, email),
      ]);
    } finally {
      await api.dispose();
    }
  }
});
