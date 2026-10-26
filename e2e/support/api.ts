import { expect, request, type APIRequestContext, type Page } from '@playwright/test';
import { apiBaseURL } from './admin';

// An API client signed in as the e2e admin, for test data a spec doesn't need
// to click through, and for cleanup. It trades the saved session's refresh
// token for an access token, which keeps working while a spec has 2FA on.
export async function adminAPI(page: Page): Promise<APIRequestContext> {
  if (page.url() === 'about:blank') await page.goto('/');
  const baseURL = `${await apiBaseURL(page)}/`;
  const refreshToken = await page.evaluate(
    () => JSON.parse(localStorage.getItem('auth-storage') ?? '{}').state?.refreshToken,
  );

  const anonymous = await request.newContext({ baseURL });
  const refreshed = await anonymous.post('auth/refresh', { data: { refreshToken } });
  expect(refreshed.ok(), 'refresh the saved e2e session').toBeTruthy();
  const { accessToken } = (await refreshed.json()).data;
  await anonymous.dispose();

  return request.newContext({ baseURL, extraHTTPHeaders: { Authorization: `Bearer ${accessToken}` } });
}

// Creates an HTTP monitor checked every 20 seconds, the minimum. A monitor that
// is Up and then starts failing spends its max retries in Pending, one failed
// check each, before it is Down. One whose first check fails is Down at once, and
// that failure already counts as one of its retries (PP-135).
export async function createHTTPMonitor(
  api: APIRequestContext,
  name: string,
  url: string,
  {
    maxRetries = 0,
    probeIds,
    notificationIds = [],
  }: { maxRetries?: number; probeIds?: string[]; notificationIds?: string[] } = {},
): Promise<{ id: string }> {
  const response = await api.post('monitors', {
    data: { ...httpMonitorBody(name, url, maxRetries), notification_ids: notificationIds, probe_ids: probeIds },
  });
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()).data;
}

// Creates an active webhook notification channel that posts JSON to url (see webhookURL in
// fake-target.ts, which reads back what it received). Delete it with
// deleteCreated(api, `notification-channels/${id}`).
export async function createWebhookChannel(api: APIRequestContext, name: string, url: string): Promise<{ id: string }> {
  const response = await api.post('notification-channels', {
    data: {
      name,
      type: 'webhook',
      active: true,
      is_default: false,
      config: JSON.stringify({ webhook_url: url, webhook_content_type: 'json' }),
    },
  });
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()).data;
}

// Replaces a monitor createHTTPMonitor made with a full update (PUT), the way
// the web edit form and API syncs do. It sends the whole body, so pass the same
// url and max retries the monitor was created with to change only the name.
export async function updateHTTPMonitor(
  api: APIRequestContext,
  id: string,
  name: string,
  url: string,
  { maxRetries = 0 } = {},
) {
  const response = await api.put(`monitors/${id}`, { data: httpMonitorBody(name, url, maxRetries) });
  expect(response.status(), await response.text()).toBe(200);
}

function httpMonitorBody(name: string, url: string, maxRetries: number) {
  return {
    type: 'http',
    name,
    interval: 20,
    timeout: 16,
    max_retries: maxRetries,
    retry_interval: 20,
    resend_interval: 0,
    active: true,
    notification_ids: [],
    config: JSON.stringify({
      url,
      method: 'GET',
      encoding: 'json',
      accepted_statuscodes: ['2XX'],
      max_redirects: 10,
      authMethod: 'none',
    }),
  };
}

// How long cleanup keeps trying: a monitor delete can answer 500 "database is locked" while
// heartbeats are being written (#261), and the delete is safe to send again.
export const cleanupRetry = { timeout: 15000, intervals: [250, 500, 1000, 2000] };

// Deletes what a spec created, e.g. "monitors/<id>", so reruns start clean. A failed delete is
// sent again for a few seconds; a 404 on a later attempt means an earlier one got through.
export async function deleteCreated(api: APIRequestContext, path: string) {
  let attempts = 0;
  await expect(async () => {
    attempts++;
    const response = await api.delete(path);
    if (response.status() === 404 && attempts > 1) return;
    expect(response.ok(), `DELETE ${path}: ${response.status()} ${await response.text()}`).toBeTruthy();
  }).toPass(cleanupRetry);
}

export const randomID = () => Math.random().toString(36).substring(2, 12);
