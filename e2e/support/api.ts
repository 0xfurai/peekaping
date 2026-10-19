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
  { maxRetries = 0 } = {},
): Promise<{ id: string }> {
  const response = await api.post('monitors', {
    data: {
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
    },
  });
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()).data;
}

// Deletes what a spec created, e.g. "monitors/<id>", so reruns start clean.
export async function deleteCreated(api: APIRequestContext, path: string) {
  const response = await api.delete(path);
  expect(response.ok(), `DELETE ${path}: ${await response.text()}`).toBeTruthy();
}

export const randomID = () => Math.random().toString(36).substring(2, 12);
