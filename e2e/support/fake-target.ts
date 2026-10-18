import { expect, type APIRequestContext } from '@playwright/test';

// The e2e target (e2e/fake-target/server.mjs), run by docker-compose.e2e.yml.
// Monitors reach it by its compose service name; the tests control it
// through its published port.
export const targetHost = process.env.E2E_TARGET_HOST ?? 'e2e-target';
export const targetPort = 3033;
const targetControlURL = process.env.E2E_TARGET_URL ?? 'http://localhost:3033';

// A URL a monitor can check: it answers 200 until setTargetStatus changes it.
export const targetURL = (id: string) => `http://${targetHost}:${targetPort}/targets/${id}`;

// A URL a webhook notification channel can post to; receivedWebhooks reads it back.
export const webhookURL = (id: string) => `http://${targetHost}:${targetPort}/webhooks/${id}`;

export async function setTargetStatus(request: APIRequestContext, id: string, status: number) {
  const response = await request.put(`${targetControlURL}/targets/${id}`, { data: { status } });
  expect(response.ok(), `set e2e target ${id} to ${status}`).toBeTruthy();
}

export type ReceivedWebhook = {
  receivedAt: string;
  headers: Record<string, string>;
  // Webhook channels with the JSON body send {heartbeat, monitor, msg}.
  body: { heartbeat: { status: number; msg: string }; monitor: { name: string }; msg: string };
};

export async function receivedWebhooks(request: APIRequestContext, id: string): Promise<ReceivedWebhook[]> {
  const response = await request.get(`${targetControlURL}/webhooks/${id}`);
  expect(response.ok(), `read webhooks ${id} from the e2e target`).toBeTruthy();
  return response.json();
}
