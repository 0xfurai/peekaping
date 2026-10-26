import type { Page } from '@playwright/test';

// Peekaping has one admin per installation, so every run of the suite signs
// in as this one.
export const admin = {
  email: process.env.E2E_EMAIL ?? 'test@test.com',
  password: process.env.E2E_PASSWORD ?? 'TestPassword1234!',
};

// The API base the web app calls. The dev stack serves the API on its own
// origin (window.__CONFIG__.API_URL); single and cluster mode serve it under the web
// origin, with API_URL empty.
export async function apiBaseURL(page: Page): Promise<string> {
  const configured = await page.evaluate(
    () => (window as unknown as { __CONFIG__?: { API_URL?: string } }).__CONFIG__?.API_URL ?? '',
  );
  return `${configured || new URL(page.url()).origin}/api/v1`;
}
