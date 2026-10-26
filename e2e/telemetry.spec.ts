import { test, expect, type Page } from '@playwright/test';
import { adminAPI } from './support/api';

const switchName = 'Send anonymous usage statistics';
const errorReportsSwitchName = 'Send error reports';
const instanceID = /"instance_id": "([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})"/;

const payload = {
  instance_id: '0b8f6c1e-5d3a-4b7e-9c2a-1f4e8d7a6b50',
  version: '0.0.48',
  mode: 'cluster',
  db: 'sqlite',
  os: 'linux/arm64',
  monitors: '1-10',
  monitor_types: ['http'],
  notification_providers: [],
  features: { status_pages: false, maintenance: false, metrics: false },
};

// The usage ping and error reports are on by default, and these two tests change what the install
// has stored. They run one after the other because each reads the other's switch.
test.describe('Choices on the server', () => {
  test.describe.configure({ mode: 'serial' });

  test('Usage statistics are on by default; turning them off removes the instance ID, and turning them on again makes a new one', async ({ page }) => {
    await page.goto('/settings');
    const toggle = page.getByRole('switch', { name: switchName });
    const preview = page.locator('pre');

    try {
      // An install that never chose sends the ping with an ID made on first use. A run that
      // went before this one left it on too.
      await expect(toggle).toBeChecked();
      await expect(preview).toContainText(instanceID);
      const first = (await preview.textContent())!.match(instanceID)![1];

      await toggle.click();
      await expect(toggle).not.toBeChecked();
      await expect(preview).toContainText('"instance_id": ""');

      // The opt-out is stored, not just shown.
      await page.reload();
      await expect(toggle).not.toBeChecked();
      await expect(preview).toContainText('"instance_id": ""');

      await toggle.click();
      await expect(toggle).toBeChecked();
      await expect(preview).toContainText(instanceID);
      const second = (await preview.textContent())!.match(instanceID)![1];
      expect(second, 'opting back in makes a new instance ID').not.toBe(first);
    } finally {
      // CI runs the suite twice on one database, and the admin is shared with every other spec.
      // Leave it on, as an install that never chose is, however this test ends. A stored choice
      // can't be taken back, but "on" is what the default says.
      const api = await adminAPI(page);
      const on = await api.put('telemetry', { data: { enabled: true } });
      expect(on.ok(), `turn usage statistics on: ${await on.text()}`).toBeTruthy();
      await api.dispose();
    }
  });

  test('Error reports are on by default, separate from usage statistics; turning them off and on is stored', async ({ page }) => {
    await page.goto('/settings');
    const toggle = page.getByRole('switch', { name: errorReportsSwitchName });
    const usageToggle = page.getByRole('switch', { name: switchName });

    try {
      await expect(toggle).toBeChecked();
      await expect(usageToggle).toBeChecked();

      await toggle.click();
      await expect(toggle).not.toBeChecked();
      // Opting out of error reports is not opting out of the usage ping.
      await expect(usageToggle).toBeChecked();

      // The choice is stored, not just shown.
      await page.reload();
      await expect(toggle).not.toBeChecked();

      await toggle.click();
      await expect(toggle).toBeChecked();
      await page.reload();
      await expect(toggle).toBeChecked();
    } finally {
      const api = await adminAPI(page);
      const on = await api.put('error-reporting', { data: { enabled: true } });
      expect(on.ok(), `turn error reports on: ${await on.text()}`).toBeTruthy();
      await api.dispose();
    }
  });
});

// The e2e stack doesn't set TELEMETRY, so the server's answer is
// faked here to see how Settings shows an install the environment switches off.
test('Settings shows the switch as forced off when the environment switches usage statistics off', async ({ page }) => {
  await page.route('**/api/v1/telemetry', (route) =>
    route.fulfill({ json: { enabled: true, forced_off: true, notice_pending: false, payload } }),
  );

  await page.goto('/settings');

  const toggle = page.getByRole('switch', { name: switchName });
  await expect(toggle).toBeDisabled();
  await expect(toggle).not.toBeChecked();
  await expect(page.getByText('TELEMETRY=off')).toBeVisible();
  await expect(page.getByText('DO_NOT_TRACK')).toHaveCount(0);
});

// The e2e stack doesn't set ERROR_REPORTING, so the server's answer
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
  await expect(page.getByText('DO_NOT_TRACK')).toHaveCount(0);
});

// The notice for an install that upgraded. A server can show it only once: nothing but the
// admin's dismissal or a choice takes it back, and the settings API can't reach it. So these
// tests fake the server's answers, which also keeps them from using up the real install's notice.
test.describe('Upgrade notice', () => {
  type Feature = { enabled: boolean; forced_off: boolean };
  const on: Feature = { enabled: true, forced_off: false };
  const forcedOff: Feature = { enabled: true, forced_off: true };
  const optedOut: Feature = { enabled: false, forced_off: false };

  const bannerName = 'Sharing anonymous data is on by default';
  const banner = (page: Page) => page.getByRole('region', { name: bannerName });

  // Fakes the server: the notice is pending until it is dismissed or either feature is changed
  const fakeServer = async (page: Page, { usage = on, errors = on, pending = true } = {}) => {
    const server = { pending, dismissals: 0, choices: [] as unknown[] };
    const telemetry = () => ({ ...usage, notice_pending: server.pending, payload });
    await page.route('**/api/v1/telemetry', (route) => {
      if (route.request().method() === 'PUT') {
        server.choices.push(route.request().postDataJSON());
        server.pending = false;
      }
      return route.fulfill({ json: telemetry() });
    });
    await page.route('**/api/v1/telemetry/notice', (route) => {
      server.dismissals++;
      server.pending = false;
      return route.fulfill({ json: telemetry() });
    });
    await page.route('**/api/v1/error-reporting', (route) => {
      if (route.request().method() === 'PUT') {
        server.choices.push(route.request().postDataJSON());
        server.pending = false;
        return route.fulfill({ json: { ...errors, enabled: route.request().postDataJSON().enabled } });
      }
      return route.fulfill({ json: errors });
    });
    return server;
  };

  test('The notice says what is on, and dismissing it hides it for good', async ({ page }) => {
    const server = await fakeServer(page);

    await page.goto('/monitors');

    await expect(banner(page)).toBeVisible();
    await expect(banner(page).getByText('Usage statistics are on.')).toBeVisible();
    await expect(banner(page).getByText('Error reports are on.')).toBeVisible();
    await expect(banner(page).getByText('It never sends names, URLs, hosts or settings.')).toBeVisible();
    await expect(banner(page).getByText('It never sends error messages, URLs or hostnames.')).toBeVisible();

    await banner(page).getByRole('button', { name: 'Dismiss' }).click();
    await expect(banner(page)).toHaveCount(0);
    expect(server.dismissals).toBe(1);
    expect(server.choices, 'dismissing the notice is no choice').toEqual([]);

    const answered = page.waitForResponse((response) => response.url().endsWith('/api/v1/telemetry'));
    await page.reload();
    await answered;
    await expect(banner(page)).toHaveCount(0);
  });

  test('The notice links to the Settings card of each feature', async ({ page }) => {
    await fakeServer(page);
    await page.goto('/monitors');

    await banner(page).getByRole('link', { name: 'Turn off error reports in Settings' }).click();

    await expect(page).toHaveURL(/\/settings#error-reports$/);
    await expect(page.getByRole('switch', { name: errorReportsSwitchName })).toBeVisible();
    await expect(page.locator('#error-reports')).toBeInViewport();
    await expect(banner(page).getByRole('link', { name: 'Turn off usage statistics in Settings' })).toHaveAttribute(
      'href',
      '/settings#usage-statistics',
    );
  });

  test('The notice names only the features that are on', async ({ page }) => {
    await fakeServer(page, { usage: forcedOff });

    await page.goto('/monitors');

    await expect(banner(page).getByText('Error reports are on.')).toBeVisible();
    await expect(page.getByText('Usage statistics are on.')).toHaveCount(0);
    await expect(banner(page).getByRole('link', { name: 'Turn off usage statistics in Settings' })).toHaveCount(0);
  });

  const quiet = [
    { name: 'it is not pending', options: { pending: false } },
    { name: 'neither feature is on', options: { usage: optedOut, errors: forcedOff } },
  ];
  for (const { name, options } of quiet) {
    test(`There is no notice when ${name}`, async ({ page }) => {
      await fakeServer(page, options);

      // The notice reads the error reports too, but only while it is pending
      const answers = [page.waitForResponse((response) => response.url().endsWith('/api/v1/telemetry'))];
      if (options.pending !== false) {
        answers.push(page.waitForResponse((response) => response.url().endsWith('/api/v1/error-reporting')));
      }
      await page.goto('/monitors');
      await Promise.all(answers);

      await expect(banner(page)).toHaveCount(0);
    });
  }

  const cards = [
    { name: 'usage statistics', switchName, endpoint: 'telemetry' },
    { name: 'error reports', switchName: errorReportsSwitchName, endpoint: 'error-reporting' },
  ];
  for (const card of cards) {
    test(`Changing the ${card.name} card hides the notice`, async ({ page }) => {
      const server = await fakeServer(page);
      await page.goto('/settings');
      await expect(banner(page)).toBeVisible();

      await page.getByRole('switch', { name: card.switchName }).click();

      await expect(banner(page)).toHaveCount(0);
      expect(server.choices).toEqual([{ enabled: false }]);
      expect(server.dismissals, 'a choice needs no dismissal').toBe(0);
    });
  }
});

// An admin already exists on the e2e stack, so the setup screen is shown signed out
// with the register request faked. The two requests that follow it carry what the
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

  // Answers the two requests as the server does, and keeps the bodies it was sent
  const fakeChoices = async (page: Page, failing: string[] = []) => {
    const puts: Record<string, unknown[]> = { telemetry: [], 'error-reporting': [] };
    for (const endpoint of Object.keys(puts)) {
      await page.route(`**/api/v1/${endpoint}`, (route) => {
        // The app reads the state too, once it is signed in
        if (route.request().method() !== 'PUT') {
          return route.fulfill({ json: { enabled: true, forced_off: false, notice_pending: false, payload } });
        }
        const body = route.request().postDataJSON();
        puts[endpoint].push(body);
        if (failing.includes(endpoint)) {
          return route.fulfill({ status: 500, json: { status: 'error', message: 'Internal server error' } });
        }
        return route.fulfill({ json: { enabled: body.enabled, forced_off: false, notice_pending: false, payload } });
      });
    }
    return puts;
  };

  test('Both boxes start ticked and sit under one note, and their labels toggle them', async ({ page }) => {
    await fillAndSubmit(page);

    const usage = page.getByRole('checkbox', { name: switchName });
    const errors = page.getByRole('checkbox', { name: errorReportsSwitchName });
    await expect(errors).toBeChecked();
    await expect(usage).toBeChecked();

    // Clicking a label toggles its own box
    await page.getByText('Send anonymous usage statistics', { exact: true }).click();
    await expect(usage).not.toBeChecked();
    await expect(errors).toBeChecked();
    await page.getByText('Send error reports', { exact: true }).click();
    await expect(errors).not.toBeChecked();

    await expect(page.getByText('You can change this later in Settings.')).toHaveCount(1);
    const link = page.getByRole('link', { name: "What's sent" });
    await expect(link).toHaveAttribute('href', 'https://docs.peekaping.com/self-hosting/telemetry');
    await expect(link).toHaveAttribute('target', '_blank');
    // The per-feature hints are gone from the setup screen
    await expect(page.getByText('On by default')).toHaveCount(0);
    await expect(page.getByText('Never sent')).toHaveCount(0);
  });

  const choices = [
    { name: 'neither box unticked', usage: true, errors: true },
    { name: 'only error reports unticked', usage: true, errors: false },
    { name: 'only usage statistics unticked', usage: false, errors: true },
    { name: 'both boxes unticked', usage: false, errors: false },
  ];

  for (const choice of choices) {
    test(`The setup screen sends the choice for both: ${choice.name}`, async ({ page }) => {
      const puts = await fakeChoices(page);
      await fakeRegister(page);

      await fillAndSubmit(page);
      if (!choice.errors) await page.getByRole('checkbox', { name: errorReportsSwitchName }).click();
      if (!choice.usage) await page.getByRole('checkbox', { name: switchName }).click();
      const registered = page.waitForResponse((response) => response.url().endsWith('/auth/register'));
      await page.getByRole('button', { name: 'Create' }).click();
      await registered;
      await expect(page.getByText('Registered successfully')).toBeVisible();

      // An unticked box is an opt-out to store, so a request goes out for every box
      await expect.poll(() => puts['error-reporting']).toEqual([{ enabled: choice.errors }]);
      await expect.poll(() => puts.telemetry).toEqual([{ enabled: choice.usage }]);
    });
  }

  // Setup has succeeded by the time the choice requests go out, so a failure there doesn't
  // undo it. The admin is told the choice wasn't saved and where to look at it again.
  const failures = [
    {
      name: 'usage statistics',
      endpoint: 'telemetry',
      warning: "Your choice about usage statistics wasn't saved. Check it in Settings.",
      other: "Your choice about error reports wasn't saved. Check it in Settings.",
    },
    {
      name: 'error reports',
      endpoint: 'error-reporting',
      warning: "Your choice about error reports wasn't saved. Check it in Settings.",
      other: "Your choice about usage statistics wasn't saved. Check it in Settings.",
    },
  ];

  for (const failure of failures) {
    test(`The setup screen warns when the choice about ${failure.name} isn't saved`, async ({ page }) => {
      const puts = await fakeChoices(page, [failure.endpoint]);
      await fakeRegister(page);

      await fillAndSubmit(page);
      await page.getByRole('button', { name: 'Create' }).click();

      await expect(page.getByText(failure.warning)).toBeVisible();
      // Setup itself went through
      await expect(page.getByText('Registered successfully')).toBeVisible();
      expect(puts[failure.endpoint]).toEqual([{ enabled: true }]);
      // Only the choice that failed has anything to warn about
      await expect(page.getByText(failure.other)).toHaveCount(0);
    });
  }
});
