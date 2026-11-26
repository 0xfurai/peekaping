import { test, expect } from '@playwright/test';
import { adminAPI, createHTTPMonitor, deleteCreated, randomID } from './support/api';
import { targetURL } from './support/fake-target';

// A paused monitor has no heartbeats, so none of its uptime or response time figures has anything
// to measure. They read as a dash with a "No data" tooltip, never as 0 (PP-344).
test('A monitor with no heartbeats shows a dash with a "No data" tooltip instead of 0', async ({ page, browser }) => {
  const id = randomID();
  const api = await adminAPI(page);
  const monitorName = `No data monitor ${id}`;
  const slug = `no-data-${id}`;
  const created: string[] = [];

  try {
    const monitor = await createHTTPMonitor(api, monitorName, targetURL(id), { active: false });
    created.push(`monitors/${monitor.id}`);

    // The monitor's page: the 24 hour, 7 day, 30 day and 365 day uptime, then the chart footer's
    // minimum, maximum and average response time and uptime.
    await page.goto(`/monitors/${monitor.id}`);
    await expect(page.getByRole('heading', { name: monitorName }).first()).toBeVisible();
    const figures = [
      'Last 24 hours',
      'Last 7 days',
      'Last 30 days',
      'Last 365 days',
      'Minimum',
      'Maximum',
      'Average',
      'Uptime',
    ];
    for (const label of figures) {
      const figure = page.getByText(label, { exact: true }).locator('xpath=..');
      await expect(figure.getByTestId('no-data'), label).toHaveText('—');
      await expect(figure, label).not.toContainText('%');
      await expect(figure, label).not.toContainText('ms');
    }
    await expect(page.getByTestId('no-data')).toHaveCount(figures.length);

    const tooltip = page.getByRole('tooltip');
    await page.getByTestId('no-data').first().hover();
    await expect(tooltip).toHaveText('No data');

    // The public status page: the monitor's 24 hour badge.
    const response = await api.post('status-pages', {
      data: { slug, title: `No data ${id}`, published: true, monitor_ids: [monitor.id] },
    });
    expect(response.status(), await response.text()).toBe(201);
    created.push(`status-pages/${(await response.json()).data.id}`);

    const visitor = await browser.newContext({
      baseURL: test.info().project.use.baseURL,
      storageState: { cookies: [], origins: [] },
    });
    const visitorPage = await visitor.newPage();
    await visitorPage.goto(`/status/${slug}`);
    const badge = visitorPage.getByTestId('status-page-monitor').getByTestId('status-page-uptime');
    await expect(badge).toHaveText('24h —');
    await badge.getByTestId('no-data').hover();
    await expect(visitorPage.getByRole('tooltip')).toHaveText('No data');
    await visitor.close();
  } finally {
    for (const path of created.reverse()) await deleteCreated(api, path);
    await api.dispose();
  }
});
