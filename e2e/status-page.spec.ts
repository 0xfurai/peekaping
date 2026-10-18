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
