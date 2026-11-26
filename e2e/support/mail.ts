import { expect, request, type APIRequestContext } from '@playwright/test';

// The mail catcher of an e2e stack (Mailpit in docker-compose.e2e.yml): a sender that sends to it,
// set in the environment (the cloud suite's) or saved in Admin → Settings, delivers the install's
// account emails and install email notifications there, and the tests read them through its HTTP
// API. Unset where there is no mail catcher, which is how specs tell that there is no mail to read.
export const mailpitURL = process.env.E2E_MAILPIT_URL;

// How the install reaches Mailpit's SMTP server: by its compose service name on an e2e stack, or
// as E2E_MAILPIT_SMTP_HOST and E2E_MAILPIT_SMTP_PORT say, e.g. localhost for a server run natively
export const mailpitSMTP = {
  host: process.env.E2E_MAILPIT_SMTP_HOST ?? 'mailpit',
  port: Number(process.env.E2E_MAILPIT_SMTP_PORT ?? 1025),
};

export type Mail = { id: string; subject: string; to: string[]; text: string; created: Date };

// How long a message has to arrive. The server sends in the background after the request that
// caused it answered.
const mailRetry = { timeout: 30_000, intervals: [250, 500, 1000] };

async function mailpit(): Promise<APIRequestContext> {
  if (!mailpitURL) throw new Error('E2E_MAILPIT_URL is not set: start the stack with docker-compose.e2e.yml and set it to http://localhost:8025');
  return request.newContext({ baseURL: `${mailpitURL.replace(/\/$/, '')}/` });
}

// Waits for a message to `to` whose subject (and text, when given) match, and returns it with its
// text body. `since` leaves out older messages, such as the ones of an earlier run to the same
// address or the one sent before the step being tested. It polls instead of sleeping, until the
// deadline.
export async function waitForMail(
  to: string,
  subject: RegExp,
  {
    since = new Date(0),
    body,
    timeout = mailRetry.timeout,
  }: { since?: Date; body?: RegExp; timeout?: number } = {},
): Promise<Mail> {
  const api = await mailpit();
  try {
    let found: Mail | undefined;
    await expect(async () => {
      const search = await api.get('api/v1/search', { params: { query: `to:"${to}"`, limit: 50 } });
      expect(search.ok(), `Mailpit search: ${search.status()} ${await search.text()}`).toBeTruthy();
      const { messages } = (await search.json()) as {
        messages: { ID: string; Subject: string; Created: string; To: { Address: string }[] }[] | null;
      };
      // Newest first, as Mailpit lists them
      const candidates = (messages ?? []).filter(
        (message) =>
          subject.test(message.Subject) &&
          new Date(message.Created) >= since &&
          message.To.some((recipient) => recipient.Address.toLowerCase() === to.toLowerCase()),
      );
      let seen = '';
      for (const candidate of candidates) {
        const full = await api.get(`api/v1/message/${candidate.ID}`);
        expect(full.ok(), `Mailpit message: ${full.status()} ${await full.text()}`).toBeTruthy();
        const { Text: text } = (await full.json()) as { Text: string };
        if (body && !body.test(text)) {
          seen += `\n- ${candidate.Subject}: ${text.slice(0, 120)}`;
          continue;
        }
        found = {
          id: candidate.ID,
          subject: candidate.Subject,
          to: candidate.To.map((recipient) => recipient.Address),
          text,
          created: new Date(candidate.Created),
        };
        return;
      }
      expect(found, `a message to ${to} with a subject matching ${subject}${body ? ` and a text matching ${body}` : ''}${seen && `, got:${seen}`}`).toBeTruthy();
    }).toPass({ timeout, intervals: mailRetry.intervals });
    return found!;
  } finally {
    await api.dispose();
  }
}

// The first link in the message whose path starts with `pathPrefix`, e.g. "/verify-email/". It
// comes back as a path with its query, relative to the app, since the link names the CLIENT_URL
// the install was started with, which a test may reach by another address.
export function linkIn(mail: Mail, pathPrefix: string): string {
  for (const [candidate] of mail.text.matchAll(/https?:\/\/[^\s<>"')]+/g)) {
    const url = new URL(candidate);
    if (url.pathname.startsWith(pathPrefix)) return `${url.pathname}${url.search}`;
  }
  throw new Error(`no link starting with ${pathPrefix} in "${mail.subject}":\n${mail.text}`);
}
