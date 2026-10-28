import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { adminAPI, deleteCreated, randomID } from './support/api';

// An instance admin resets another user's password from the admin area on an install that sends
// no account emails: the dialog shows the link once, the user opens it, chooses a new password
// and signs in with it (PP-371). The user isn't an instance admin, so the admin area is closed to
// them.

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
const oldPassword = 'OldPassword1234!';
const newPassword = 'NewPassword5678!';

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

// Removes what the test left if it failed before the UI did: the user (deleting the user takes
// their place in the workspace with it) and the invite, if the user never signed up.
async function removeUser(api: APIRequestContext, email: string) {
  await runAll([
    async () => {
      const users = await api.get('admin/users', { params: { q: email } });
      if (!users.ok()) return;
      const mine = ((await users.json()).data.users as { id: string; email: string }[]).filter((u) => u.email === email);
      await runAll(mine.map((user) => () => deleteCreated(api, `admin/users/${user.id}`)));
    },
    async () => {
      const invites = await api.get('workspace/invites');
      if (!invites.ok()) return;
      const mine = ((await invites.json()).data as { id: string; email: string }[]).filter((i) => i.email === email);
      await runAll(mine.map((invite) => () => deleteCreated(api, `workspace/invites/${invite.id}`)));
    },
  ]);
}

async function signIn(page: Page, email: string, password: string) {
  await page.goto('/login');
  await page.getByRole('textbox', { name: 'Email' }).fill(email);
  await page.locator('input[name="password"]').fill(password);
  const answered = page.waitForResponse((response) => response.url().endsWith('/auth/login'));
  await page.getByRole('button', { name: 'Login' }).click();
  return answered;
}

test('an instance admin resets a user password with no email sender, and the user signs in with the new one', async ({
  page,
  browser,
}) => {
  const email = `reset-${Date.now()}-${randomID()}@example.com`;

  const api = await adminAPI(page);
  const workspace = await adminWorkspace(api);
  // Signed out: a context made here would otherwise start with the e2e admin's saved session
  const signedOut = { cookies: [], origins: [] };
  const userContext = await browser.newContext({ storageState: signedOut });
  const resetContext = await browser.newContext({ storageState: signedOut });
  const loginContext = await browser.newContext({ storageState: signedOut });

  try {
    // 1. A second user: invited to the admin's workspace, signed up through the link with the old password
    await page.goto(`/w/${workspace.id}/settings/members`);
    await page.getByRole('textbox', { name: 'Email' }).fill(email);
    await page.getByRole('button', { name: 'Create invite link' }).click();
    const invite = page.getByRole('alert').filter({ hasText: 'Invite link' }).getByRole('textbox');
    await expect(invite).toHaveValue(/\/invite\/.+/);
    const inviteLink = await invite.inputValue();

    const user = await userContext.newPage();
    await user.goto(inviteLink);
    await user.getByLabel('Password', { exact: true }).fill(oldPassword);
    await user.getByRole('button', { name: 'Create account and join' }).click();
    await expect(user).toHaveURL(new RegExp(`/w/${workspace.id}/monitors$`));

    // 5. The user isn't an instance admin: no Admin entry, and /admin sends them to their workspace
    await expect(user.getByRole('link', { name: 'Monitors' }).first()).toBeVisible();
    await expect(user.getByRole('link', { name: 'Admin', exact: true })).toHaveCount(0);
    await user.goto('/admin/users');
    await expect(user).toHaveURL(new RegExp(`/w/${workspace.id}/monitors$`));
    await user.goto('/admin');
    await expect(user).toHaveURL(new RegExp(`/w/${workspace.id}/monitors$`));
    await expect(user.getByRole('link', { name: 'Admin', exact: true })).toHaveCount(0);

    // 2. The admin opens Admin from the sidebar, finds the user and makes a reset link
    await page.goto(`/w/${workspace.id}/monitors`);
    await page.getByRole('link', { name: 'Admin', exact: true }).click();
    await expect(page).toHaveURL(/\/admin(\/users)?$/);
    await page.getByRole('link', { name: 'Users', exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/users$/);

    await page.getByRole('searchbox', { name: 'Search users by email' }).fill(email);
    const row = page.getByRole('row', { name: new RegExp(email) });
    await expect(row).toBeVisible();
    // The search narrowed the list to this one user
    await expect(page.getByRole('row').filter({ hasText: '@example.com' })).toHaveCount(1);

    await page.getByRole('button', { name: `Actions for ${email}` }).click();
    await page.getByRole('menuitem', { name: 'Make reset link' }).click();
    const dialog = page.getByRole('dialog', { name: 'Password reset link' });
    const shown = dialog.getByTestId('reset-link');
    await expect(shown).toContainText('/reset-password/');
    // No sender for account emails, so the dialog doesn't say the user was emailed
    await expect(dialog.getByText('the user is told by email')).toHaveCount(0);
    const resetLink = (await shown.innerText()).trim();
    expect(new URL(resetLink).pathname).toMatch(/^\/reset-password\/[^/]+$/);

    // The link is shown once: closing the dialog forgets it
    await dialog.getByRole('button', { name: 'Close', exact: true }).first().click();
    await expect(dialog).toHaveCount(0);
    await page.getByRole('button', { name: `Actions for ${email}` }).click();
    await expect(page.getByRole('menuitem', { name: 'Make reset link' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByTestId('reset-link')).toHaveCount(0);

    // 3. A signed-out visitor opens the link and sets the new password
    const reset = await resetContext.newPage();
    await reset.goto(resetLink);
    await reset.getByLabel('Password', { exact: true }).fill(newPassword);
    await reset.getByLabel('Confirm Password').fill(newPassword);
    await reset.getByRole('button', { name: 'Set password' }).click();
    await expect(reset.getByText('Your password is changed')).toBeVisible();

    // 4. The old password is refused, the new one signs the user in to the workspace
    const login = await loginContext.newPage();
    const refused = await signIn(login, email, oldPassword);
    expect(refused.status()).toBe(401);
    await expect(login).toHaveURL(/\/login$/);

    const accepted = await signIn(login, email, newPassword);
    expect(accepted.status(), await accepted.text()).toBe(200);
    await expect(login).toHaveURL(new RegExp(`/w/${workspace.id}/monitors$`));
    await expect(login.getByRole('link', { name: 'Monitors' }).first()).toBeVisible();
    await expect(login.getByRole('link', { name: 'Admin', exact: true })).toHaveCount(0);

    // The link works once
    await reset.reload();
    await reset.getByLabel('Password', { exact: true }).fill(newPassword);
    await reset.getByLabel('Confirm Password').fill(newPassword);
    await reset.getByRole('button', { name: 'Set password' }).click();
    await expect(reset.getByText('This link is used or expired.')).toBeVisible();
  } finally {
    // Each step runs even if an earlier one throws; the failures are reported together after
    try {
      await runAll([
        () => userContext.close(),
        () => resetContext.close(),
        () => loginContext.close(),
        () => removeUser(api, email),
      ]);
    } finally {
      await api.dispose();
    }
  }
});
