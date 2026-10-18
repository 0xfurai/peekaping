// The e2e target (e2e/fake-target/server.mjs), run by docker-compose.e2e.yml.
// Monitors reach it by its compose service name.
export const targetHost = process.env.E2E_TARGET_HOST ?? 'e2e-target';
export const targetPort = 3033;

// A URL a monitor can check: it answers 200 until setTargetStatus changes it.
export const targetURL = (id: string) => `http://${targetHost}:${targetPort}/targets/${id}`;
