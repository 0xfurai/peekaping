import { test, expect, type Page } from '@playwright/test';
import { adminAPI } from './support/api';

const switchName = 'Send anonymous usage statistics';
const errorReportsSwitchName = 'Send error reports';

test('Usage statistics are off by default; turning them on shows the instance ID in the payload, and turning them off removes it', async ({ page }) => {
  await page.goto('/settings');
  const toggle = page.getByRole('switch', { name: switchName });
  const preview = page.locator('pre');

  try {
    // A new install sends nothing, so the payload has no instance ID. A run that
    // went before this one left it off too.
    await expect(toggle).not.toBeChecked();
    await expect(preview).toContainText('"instance_id": ""');

    await toggle.click();
    await expect(toggle).toBeChecked();
    await expect(preview).toContainText(/"instance_id": "[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}"/);

    // The choice is stored, not just shown.
    await page.reload();
    await expect(toggle).toBeChecked();

    await toggle.click();
    await expect(toggle).not.toBeChecked();
    await expect(preview).toContainText('"instance_id": ""');
  } finally {
    // CI runs the suite twice on one database, and the admin is shared with
    // every other spec, so leave it off however this test ends.
    const api = await adminAPI(page);
    const off = await api.put('telemetry', { data: { enabled: false } });
    expect(off.ok(), `turn usage statistics off: ${await off.text()}`).toBeTruthy();
    await api.dispose();
  }
});

// The e2e stack doesn't set TELEMETRY or DO_NOT_TRACK, so the server's answer is
// faked here to see how Settings shows an install the environment switches off.
test('Settings shows the switch as forced off when the environment switches usage statistics off', async ({ page }) => {
  await page.route('**/api/v1/telemetry', (route) =>
    route.fulfill({
      json: {
        enabled: true,
        forced_off: true,
        payload: {
          instance_id: '0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50',
          version: '0.0.48',
          mode: 'cluster',
          db: 'sqlite',
          os: 'linux/arm64',
          monitors: '1-10',
          monitor_types: ['http'],
          notification_providers: [],
          features: { status_pages: false, maintenance: false, metrics: false },
        },
      },
    }),
  );

  await page.goto('/settings');

  const toggle = page.getByRole('switch', { name: switchName });
  await expect(toggle).toBeDisabled();
  await expect(toggle).not.toBeChecked();
  await expect(page.getByText('TELEMETRY=off')).toBeVisible();
  await expect(page.getByText('DO_NOT_TRACK=1')).toBeVisible();
});

test('Error reports are off by default, separate from usage statistics; turning them on and off is stored', async ({ page }) => {
  await page.goto('/settings');
  const toggle = page.getByRole('switch', { name: errorReportsSwitchName });
  const usageToggle = page.getByRole('switch', { name: switchName });

  try {
    // A new install sends no reports. A run that went before this one left it off too.
    await expect(toggle).not.toBeChecked();

    await toggle.click();
    await expect(toggle).toBeChecked();
    // Opting in to error reports is not opting in to the usage ping.
    await expect(usageToggle).not.toBeChecked();

    // The choice is stored, not just shown.
    await page.reload();
    await expect(toggle).toBeChecked();

    await toggle.click();
    await expect(toggle).not.toBeChecked();
    await page.reload();
    await expect(toggle).not.toBeChecked();
  } finally {
    // CI runs the suite twice on one database, and the admin is shared with
    // every other spec, so leave it off however this test ends.
    const api = await adminAPI(page);
    const off = await api.put('error-reporting', { data: { enabled: false } });
    expect(off.ok(), `turn error reports off: ${await off.text()}`).toBeTruthy();
    await api.dispose();
  }
});

// The e2e stack doesn't set ERROR_REPORTING or DO_NOT_TRACK, so the server's answer
// is faked here to see how Settings shows an install the environment switches off.
test('Settings shows error reports as forced off when the environment switches them off', async ({ page }) => {
  await page.route('**/api/v1/error-reporting', (route) =>
    route.fulfill({ json: { enabled: true, forced_off: true } }),
  );

  await page.goto('/settings');

  const toggle = page.getByRole('switch', { name: errorReportsSwitchName });
  await expect(toggle).toBeDisabled();
  await expect(toggle).not.toBeChecked();
  await expect(page.getByText('ERROR_REPORTING=off')).toBeVisible();
  await expect(page.getByText('DO_NOT_TRACK=1')).toBeVisible();
});

// An admin already exists on the e2e stack, so the setup screen is shown signed out
// with the register request faked. The two requests that follow it are what the
// checkboxes decide.
test.describe('Setup screen', () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  const fakeRegister = (page: Page) =>
    page.route('**/api/v1/auth/register', (route) =>
      route.fulfill({
        status: 201,
        json: {
          status: 'success',
          message: 'User registered successfully',
          data: { accessToken: 'fake-access', refreshToken: 'fake-refresh', user: { id: '1', email: 'new@admin.test' } },
        },
      }),
    );

  const fillAndSubmit = async (page: Page) => {
    await page.goto('/register');
    await page.getByRole('textbox', { name: 'Email' }).fill('new@admin.test');
    await page.locator('input[name="password"]').fill('TestPassword1234!');
    await page.locator('input[name="confirmPassword"]').fill('TestPassword1234!');
  };

  const choices = [
    { name: 'neither box ticked', usage: false, errors: false },
    { name: 'only error reports ticked', usage: false, errors: true },
    { name: 'only usage statistics ticked', usage: true, errors: false },
    { name: 'both boxes ticked', usage: true, errors: true },
  ];

  for (const choice of choices) {
    test(`The setup screen turns on only what was ticked: ${choice.name}`, async ({ page }) => {
      const puts: Record<string, unknown[]> = { telemetry: [], 'error-reporting': [] };
      for (const endpoint of Object.keys(puts)) {
        await page.route(`**/api/v1/${endpoint}`, (route) => {
          puts[endpoint].push(route.request().postDataJSON());
          return route.fulfill({ json: { enabled: true, forced_off: false } });
        });
      }
      await fakeRegister(page);

      await fillAndSubmit(page);
      const errorReports = page.getByRole('checkbox', { name: errorReportsSwitchName });
      const usage = page.getByRole('checkbox', { name: switchName });
      await expect(errorReports).not.toBeChecked();
      await expect(usage).not.toBeChecked();

      if (choice.errors) await errorReports.click();
      if (choice.usage) await usage.click();
      const registered = page.waitForResponse((response) => response.url().endsWith('/auth/register'));
      await page.getByRole('button', { name: 'Create' }).click();
      await registered;
      await expect(page.getByText('Registered successfully')).toBeVisible();

      if (choice.errors) await expect.poll(() => puts['error-reporting']).toEqual([{ enabled: true }]);
      if (choice.usage) await expect.poll(() => puts.telemetry).toEqual([{ enabled: true }]);
      // The request for a box left unticked is never sent
      await page.waitForTimeout(500);
      if (!choice.errors) expect(puts['error-reporting']).toEqual([]);
      if (!choice.usage) expect(puts.telemetry).toEqual([]);
    });
  }

  // Setup has succeeded by the time the opt-in request goes out, so a failure there
  // doesn't undo it. The admin is told the choice wasn't saved and where to make it again.
  const failures = [
    {
      name: 'usage statistics',
      endpoint: 'telemetry',
      box: switchName,
      warning: "Your choice to send usage statistics wasn't saved. Turn it on in Settings.",
      other: "Your choice to send error reports wasn't saved. Turn it on in Settings.",
    },
    {
      name: 'error reports',
      endpoint: 'error-reporting',
      box: errorReportsSwitchName,
      warning: "Your choice to send error reports wasn't saved. Turn it on in Settings.",
      other: "Your choice to send usage statistics wasn't saved. Turn it on in Settings.",
    },
  ];

  for (const failure of failures) {
    test(`The setup screen warns when the choice to send ${failure.name} isn't saved`, async ({ page }) => {
      const puts: unknown[] = [];
      await page.route(`**/api/v1/${failure.endpoint}`, (route) => {
        puts.push(route.request().postDataJSON());
        return route.fulfill({ status: 500, json: { status: 'error', message: 'Internal server error' } });
      });
      await fakeRegister(page);

      await fillAndSubmit(page);
      await page.getByRole('checkbox', { name: failure.box }).click();
      await page.getByRole('button', { name: 'Create' }).click();

      await expect(page.getByText(failure.warning)).toBeVisible();
      // Setup itself went through
      await expect(page.getByText('Registered successfully')).toBeVisible();
      expect(puts).toEqual([{ enabled: true }]);
      // Only the box that was ticked has anything to warn about
      await expect(page.getByText(failure.other)).toHaveCount(0);
    });
  }
});
