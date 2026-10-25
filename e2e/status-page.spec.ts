import { test, expect, type Page } from '@playwright/test';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID } from './support/api';
import { targetURL } from './support/fake-target';

test('A published status page shows visitors its own monitors, and a draft shows nothing', async ({ page, browser }) => {
  const id = randomID();
  const api = await adminAPI(page);
  const onPublished = `Published monitor ${id}`;
  const onDraft = `Draft monitor ${id}`;
  const created: string[] = [];

  try {
    for (const name of [onPublished, onDraft]) {
      created.push(`monitors/${(await createHTTPMonitor(api, name, targetURL(id))).id}`);
    }
    await createStatusPage(page, created, `Published ${id}`, `published-${id}`, onPublished, true);
    await createStatusPage(page, created, `Draft ${id}`, `draft-${id}`, onDraft, false);

    const visitor = await browser.newContext({
      baseURL: test.info().project.use.baseURL,
      storageState: { cookies: [], origins: [] },
    });
    const visitorPage = await visitor.newPage();

    await visitorPage.goto(`/status/published-${id}`);
    await expect(visitorPage.getByRole('heading', { level: 1, name: `Published ${id}` })).toBeVisible();
    // Each monitor on the page is a card headed by its name.
    const monitorCards = visitorPage.getByRole('heading', { level: 3 });
    await expect(monitorCards).toHaveText([onPublished]);

    await visitorPage.goto(`/status/draft-${id}`);
    await expect(visitorPage.getByText('Status page not found or is not published.')).toBeVisible();
    await expect(visitorPage.getByText(`Draft ${id}`)).toHaveCount(0);

    await visitor.close();
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});

test('A status page lists each location of a monitor only once it is asked to', async ({ page, browser }) => {
  const id = randomID();
  const api = await adminAPI(page);
  const probeName = `Frankfurt ${id}`;
  const monitorName = `Located monitor ${id}`;
  const slug = `located-${id}`;
  const created: string[] = [];

  try {
    // The probe has no instance, so it is offline and never reports. It is deleted last: a probe
    // that has monitors can't be deleted.
    const probe = await api.post('probes', { data: { name: probeName } });
    expect(probe.status(), await probe.text()).toBe(201);
    const probeID = (await probe.json()).data.id;
    created.push(`probes/${probeID}`);
    const monitor = await createHTTPMonitor(api, monitorName, targetURL(id), { probeIds: ['local', probeID] });
    created.push(`monitors/${monitor.id}`);

    await createStatusPage(page, created, `Located ${id}`, slug, monitorName, true);
    const statusPageID = created[created.length - 1].replace('status-pages/', '');

    const visitor = await browser.newContext({
      baseURL: test.info().project.use.baseURL,
      storageState: { cookies: [], origins: [] },
    });
    const visitorPage = await visitor.newPage();
    const locations = visitorPage.getByTestId('status-page-location');

    // The setting is off by default: the card is the monitor's own, with one bar.
    await visitorPage.goto(`/status/${slug}`);
    await expect(visitorPage.getByRole('heading', { level: 3 })).toHaveText([monitorName]);
    await expect(locations).toHaveCount(0);

    const setShowLocations = async (on: boolean) => {
      await page.goto(`/status-pages/${statusPageID}/edit`);
      const showLocations = page.getByRole('switch', { name: 'Show each location' });
      await expect(showLocations).toBeChecked({ checked: !on });
      await showLocations.setChecked(on);
      const saved = page.waitForResponse(
        (response) => response.url().endsWith(`/status-pages/${statusPageID}`) && response.request().method() === 'PATCH',
      );
      await page.getByRole('button', { name: 'Update Status Page' }).click();
      const response = await saved;
      expect(response.status(), await response.text()).toBe(200);
      await expect(page).toHaveURL(/\/status-pages$/);
    };

    // Turned on in the form, the card lists Local, then the remote probe, each with its own row.
    await setShowLocations(true);
    await visitorPage.reload();
    await expect(locations).toHaveCount(2);
    await expect(visitorPage.getByTestId('status-page-location-name')).toHaveText(['Local', probeName]);
    // Local checks the monitor, and its row follows. The page refreshes every five minutes, so reload.
    await expect(async () => {
      await visitorPage.reload();
      await expect(visitorPage.getByTestId('status-page-location-status').first()).toHaveText('Operational', {
        timeout: 2000,
      });
    }).toPass({ timeout: 60000 });

    // Turned off again, the single bar is back.
    await setShowLocations(false);
    await visitorPage.reload();
    await expect(visitorPage.getByRole('heading', { level: 3 })).toHaveText([monitorName]);
    await expect(locations).toHaveCount(0);

    await visitor.close();
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});

async function createStatusPage(
  page: Page,
  created: string[],
  title: string,
  slug: string,
  monitorName: string,
  published: boolean,
) {
  await page.goto('/status-pages/new');
  await page.locator('input[name="title"]').fill(title);
  await page.locator('input[name="slug"]').fill(slug);

  // The picker searches the server, which on Postgres matches only a lowercase term (PP-72).
  await page.getByPlaceholder('Select monitors...').fill(monitorName.toLowerCase());
  await page.getByRole('option', { name: monitorName, exact: true }).click();

  await page.getByRole('switch', { name: 'Published' }).setChecked(published);

  const saved = page.waitForResponse(
    (response) => response.url().endsWith('/status-pages') && response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create Status Page' }).click();
  const response = await saved;
  expect(response.status(), await response.text()).toBe(201);
  created.push(`status-pages/${(await response.json()).data.id}`);
  await expect(page).toHaveURL(/\/status-pages$/);
}
