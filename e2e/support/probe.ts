import { execFileSync, spawnSync } from 'node:child_process';
import { expect, type APIRequestContext, type Page } from '@playwright/test';
import { adminAPI, cleanupRetry, deleteCreated } from './api';

export async function createProbe(api: APIRequestContext, name: string): Promise<{ id: string; token: string }> {
  const response = await api.post('probes', { data: { name } });
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()).data;
}

// Deletes what a spec made, whatever state it was left in, so reruns start clean. The monitors go
// first: a probe that has monitors can't be deleted (it answers 409). A form that was submitted
// when the spec failed can still be saving its monitor, so a refused probe delete lists the
// monitors again and tries once more, instead of failing the spec with a message that hides why
// it failed in the first place.
export async function cleanUp(page: Page, { monitor, probe }: { monitor: string; probe: string | string[] }) {
  const probeNames = [probe].flat();
  const api = await adminAPI(page);
  try {
    await expect(async () => {
      const monitors = await api.get('monitors', { params: { q: monitor, limit: 100 } });
      expect(monitors.ok(), `GET monitors: ${await monitors.text()}`).toBeTruthy();
      // A list with no match has null for data
      for (const found of ((await monitors.json()).data ?? []).filter((m: { name: string }) => m.name === monitor)) {
        await deleteCreated(api, `monitors/${found.id}`);
      }
      const probes = await (await api.get('probes')).json();
      for (const found of probes.data.filter((p: { name: string }) => probeNames.includes(p.name))) {
        const response = await api.delete(`probes/${found.id}`);
        expect(response.ok(), `DELETE probes/${found.id}: ${response.status()} ${await response.text()}`).toBeTruthy();
      }
    }).toPass(cleanupRetry);
  } finally {
    await api.dispose();
  }
}

// Running a probe needs the Docker network the stack's services share, and the server's address on
// it. A dev (or prod) stack has an `api` service on its `appnet` network; a bundle has one
// `peekaping` service on the default network. The e2e overlay adds an `appnet` to a bundle too,
// with only the e2e target on it, so a network can't be told from its name. The stack is found
// from the e2e target, which every e2e stack runs. E2E_PROBE_NETWORK and E2E_PROBE_URL set both
// when that lookup can't.
const probeImage = process.env.E2E_PROBE_IMAGE ?? 'peekaping-probe';

const docker = (...args: string[]) =>
  execFileSync('docker', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();

function stackNetwork(): { network: string; url: string } {
  if (process.env.E2E_PROBE_NETWORK && process.env.E2E_PROBE_URL) {
    return { network: process.env.E2E_PROBE_NETWORK, url: process.env.E2E_PROBE_URL };
  }
  const target = docker('ps', '--filter', 'label=com.docker.compose.service=e2e-target', '--format', '{{.ID}}').split(
    '\n',
  )[0];
  if (!target) {
    throw new Error(
      'no e2e-target container with a docker compose label is running, so the stack cannot be found: ' +
        'start it with docker compose, or set E2E_PROBE_NETWORK (the Docker network the server is on, ' +
        'e.g. peekaping-e2e) and E2E_PROBE_URL (the server\'s address on that network, ' +
        'e.g. http://peekaping-bundle-test-sqlite:8383)',
    );
  }
  const project = docker('inspect', '--format', '{{index .Config.Labels "com.docker.compose.project"}}', target);
  const hasAPI = !!docker(
    'ps', '--filter', `label=com.docker.compose.project=${project}`, '--filter', 'label=com.docker.compose.service=api', '-q',
  );
  return {
    network: process.env.E2E_PROBE_NETWORK ?? `${project}_${hasAPI ? 'appnet' : 'default'}`,
    url: process.env.E2E_PROBE_URL ?? (hasAPI ? 'http://api:8034' : 'http://peekaping:8383'),
  };
}

// Starts a peekaping-probe container with the probe's token, on the stack's network. The name is
// the caller's, so that the cleanup can remove the container whatever state the spec left it in.
export function startProbeContainer(name: string, token: string) {
  const { network, url } = stackNetwork();
  docker(
    'run', '-d', '--name', name, '--network', network,
    '-e', `PEEKAPING_URL=${url}`, '-e', `PROBE_TOKEN=${token}`, '-e', 'PROBE_ALLOW_INSECURE=true',
    probeImage,
  );
}

// docker stop sends SIGTERM, which the probe answers by saying goodbye to the server.
export const stopProbeContainer = (name: string) => void docker('stop', name);

// Removes the container if there is one, running or not. A spec whose `docker run` failed has none,
// which is not an error: `docker rm -f` succeeds for a missing name, and other runtimes' "No such
// container" is let through here too. Anything else it fails with is real.
export function removeProbeContainer(name: string) {
  try {
    docker('rm', '-f', name);
  } catch (error) {
    if (!/no such container/i.test(String((error as { stderr?: unknown }).stderr ?? error))) {
      throw error;
    }
  }
}

// Removes the spec's container, then its monitor and probe. The second step runs whether or not the
// first one failed, so a container that can't be removed doesn't leave the probe in the database.
async function tearDown(page: Page, names: { monitor: string; probe: string; container: string }) {
  try {
    removeProbeContainer(names.container);
  } finally {
    await cleanUp(page, names);
  }
}

// Runs a spec's body and then tears down what it made, even when it throws. The body's own error is
// the one that reaches the report: a teardown error is only raised when the body passed, and is
// logged otherwise, so that a failed startup is not reported as a failed cleanup.
export async function withTearDown(
  page: Page,
  names: { monitor: string; probe: string; container: string },
  body: () => Promise<void>,
) {
  try {
    await body();
  } catch (error) {
    await tearDown(page, names).catch((teardownError) => console.warn('teardown also failed:', teardownError));
    throw error;
  }
  await tearDown(page, names);
}

// The end of the container's output, for the message of a spec that waited for it in vain.
export function probeContainerLogs(name: string): string {
  const logs = spawnSync('docker', ['logs', '--tail', '30', name], { encoding: 'utf8' });
  return logs.status === 0 ? `${logs.stdout}${logs.stderr}`.trim() : '(no container)';
}
