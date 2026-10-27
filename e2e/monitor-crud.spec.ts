import { test, expect, type Page } from '@playwright/test';
import { targetHost, targetPort, targetURL } from './support/fake-target';

const randomMonitorName = () => `Test Monitor ${Math.random().toString(36).substring(2, 15)}`;

// How a test fills one monitor type's create form. Every listed field must
// exist: a renamed or missing field fails the test instead of being skipped.
// Targets are the e2e target or addresses nothing listens on, never a public host.
type MonitorForm = {
  // The type's option text in the Type select. HTTP, the default, has none.
  option?: string;
  // Radix selects: label → option text. Filled first, since they can change the fields shown.
  selects?: Record<string, string>;
  // Inputs and textareas: `name` → value.
  inputs?: Record<string, string | number>;
  // Checkboxes: label → checked.
  checkboxes?: Record<string, boolean>;
  // Inputs without a `name`: placeholder → value.
  placeholders?: Record<string, string>;
};

const grpcProtobuf = `syntax = "proto3";
package test;
service TestService {
  rpc Echo(EchoRequest) returns (EchoResponse);
}
message EchoRequest { string message = 1; }
message EchoResponse { string response = 1; }`;

const monitorForms: Record<string, MonitorForm> = {
  HTTP: {
    inputs: { url: targetURL('monitor-crud') },
  },
  'HTTP mTLS (no CA)': {
    // The CA is left empty on purpose: it is optional, and the monitor then
    // verifies the server against the system's trusted CAs (PP-217). The server
    // only checks that the certificate and key are not empty when it saves them.
    selects: { 'Authentication Method': 'mTLS' },
    inputs: {
      url: targetURL('monitor-crud-mtls'),
      'authentication.tlsCert': 'placeholder certificate',
      'authentication.tlsKey': 'placeholder key',
    },
  },
  TCP: {
    option: 'TCP Port Monitor',
    inputs: { host: targetHost, port: targetPort },
  },
  Ping: {
    option: 'Ping Monitor (ICMP)',
    inputs: { host: targetHost, packet_size: 32 },
  },
  DNS: {
    option: 'DNS Monitor',
    selects: { 'Resource Record Type': 'A' },
    inputs: { host: targetHost, resolver_server: '127.0.0.1', port: 53 },
  },
  Push: {
    // Push monitors generate their token, so there's nothing to fill.
    option: 'Push Monitor (external service calls a generated URL)',
  },
  Docker: {
    option: 'Docker Container',
    selects: { 'Connection Type': 'Socket' },
    inputs: { container_id: 'test-container', docker_daemon: '/var/run/docker.sock' },
  },
  MySQL: {
    option: 'MySQL/MariaDB Monitor',
    inputs: { connection_string: `mysql://testuser:testpass@${targetHost}:3306/testdb`, query: 'SELECT 1' },
  },
  PostgreSQL: {
    option: 'PostgreSQL Database Monitor',
    inputs: {
      database_connection_string: `postgres://testuser:testpass@${targetHost}:5432/testdb`,
      database_query: 'SELECT 1',
    },
  },
  MongoDB: {
    option: 'MongoDB Database Monitor',
    inputs: {
      connectionString: `mongodb://testuser:testpass@${targetHost}:27017/testdb`,
      command: '{"ping": 1}',
      jsonPath: '$',
    },
  },
  Redis: {
    option: 'Redis Database Monitor',
    inputs: { databaseConnectionString: `redis://testuser:testpass@${targetHost}:6379` },
    checkboxes: { 'Ignore TLS/SSL errors': false },
  },
  'SQL Server': {
    option: 'Microsoft SQL Server Monitor',
    inputs: {
      database_connection_string: `Server=${targetHost},1433;Database=testdb;User Id=testuser;Password=testpass;`,
      database_query: 'SELECT 1',
    },
  },
  SNMP: {
    option: 'SNMP Monitor',
    selects: { 'SNMP Version': 'SNMPv2c' },
    inputs: { host: targetHost, port: 161, community: 'public', oid: '1.3.6.1.2.1.1.1.0' },
  },
  SMTP: {
    option: 'SMTP Mail Server Monitor',
    inputs: {
      host: targetHost,
      port: 587,
      from_email: 'test@example.com',
      rcpt_to_email: 'recipient@example.com',
      username: 'testuser',
      password: 'testpass',
    },
    checkboxes: { 'Use TLS': true },
  },
  'GRPC-Keyword': {
    option: 'gRPC Keyword Monitor',
    inputs: {
      grpcUrl: `${targetHost}:50051`,
      grpcProtobuf,
      grpcServiceName: 'TestService',
      grpcMethod: 'Echo',
      grpcBody: '{"message": "ping"}',
      keyword: 'success',
    },
    // The gRPC form sets no defaults when the type changes (PP-206), so both
    // checkboxes start unset and the form won't submit until each one is clicked.
    checkboxes: { 'Enable TLS': true, 'Invert Keyword': true },
  },
  MQTT: {
    option: 'MQTT Broker Monitor',
    selects: { 'MQTT Check Type': 'None (Any Message)' },
    inputs: { hostname: targetHost, port: 1883, topic: 'test/topic', username: 'testuser', password: 'testpass' },
  },
  RabbitMQ: {
    option: 'RabbitMQ Monitor',
    inputs: { 'nodes.0.url': `http://${targetHost}:15672`, username: 'testuser', password: 'testpass' },
  },
  'Kafka Producer': {
    option: 'Kafka Producer Monitor',
    // Broker inputs have no name; the first one is found by its placeholder.
    placeholders: { 'localhost:9092': `${targetHost}:9092` },
    inputs: { topic: 'test-topic', message: '{"test": "message"}' },
  },
};

async function createMonitor(page: Page, monitorName: string, form: MonitorForm) {
  await page.goto('/monitors');
  await page.getByTestId('create-entity').click();
  await expect(page).toHaveURL(/\/monitors\/new$/);

  if (form.option) {
    const type = page.getByRole('combobox', { name: 'Type', exact: true });
    await type.click();
    await page.getByRole('option', { name: form.option, exact: true }).click();
    await expect(type).toHaveText(form.option);
  }

  await page.locator('input[name="name"]').fill(monitorName);

  for (const [label, option] of Object.entries(form.selects ?? {})) {
    const select = page.getByRole('combobox', { name: label, exact: true });
    await select.click();
    await page.getByRole('option', { name: option, exact: true }).click();
    await expect(select).toHaveText(option);
  }
  for (const [fieldName, value] of Object.entries(form.inputs ?? {})) {
    const field = page.locator(`[name="${fieldName}"]`);
    await field.fill(String(value));
    // A type switch resets the form to that type's defaults; make sure the value stuck.
    await expect(field).toHaveValue(String(value));
  }
  for (const [label, checked] of Object.entries(form.checkboxes ?? {})) {
    await page.getByRole('checkbox', { name: label, exact: true }).setChecked(checked);
  }
  for (const [placeholder, value] of Object.entries(form.placeholders ?? {})) {
    const field = page.getByPlaceholder(placeholder, { exact: true });
    await field.fill(value);
    await expect(field).toHaveValue(value);
  }

  await page.getByRole('button', { name: 'Create', exact: true }).click();
  // The monitor's page, not /monitors/new: a rejected form stays there.
  await expect(page).toHaveURL(/\/monitors\/(?!new$)[A-Za-z0-9-]+$/, { timeout: 10000 });
}

// The list loads 20 monitors at a time, so find the monitor by searching for
// its name. On Postgres the search matches only a lowercase term (PP-72).
const searchMonitors = (page: Page, monitorName: string) =>
  page.goto(`/monitors?search=${encodeURIComponent(monitorName.toLowerCase())}`);

async function verifyMonitorInList(page: Page, monitorName: string) {
  await searchMonitors(page, monitorName);
  await expect(page.getByTestId('monitor-card').filter({ hasText: monitorName })).toBeVisible({ timeout: 10000 });
}

async function editMonitor(page: Page, monitorName: string, newMonitorName: string) {
  // Click on the monitor to view details
  await page.getByText(monitorName).click();
  await page.waitForURL(/.*\/monitors\/[A-Za-z0-9-]+$/, { timeout: 5000 });

  // Verify we're on the monitor detail page
  await expect(page.getByText(monitorName)).toBeVisible();

  // Edit the monitor
  await page.getByRole('button', { name: /edit/i }).click();
  await page.waitForURL(/.*\/monitors\/[A-Za-z0-9-]+\/edit$/, { timeout: 5000 });

  // Update the monitor name
  const nameField = page.locator('input[name="name"]');
  await nameField.clear();
  await nameField.fill(newMonitorName);

  // Submit the update
  await page.getByRole('button', { name: 'Update' }).click();
  await page.waitForURL(/.*\/monitors\/[A-Za-z0-9-]+$/, { timeout: 10000 });

  // Verify the monitor name was updated
  await expect(page.getByText(newMonitorName)).toBeVisible();

  return newMonitorName;
}

async function deleteMonitor(page: Page, monitorName: string) {
  // Delete the monitor
  await page.getByRole('button', { name: /delete/i }).click();

  // Confirm deletion in the dialog
  await expect(page.getByText(/are you absolutely sure/i)).toBeVisible({ timeout: 5000 });
  await page.getByRole('button', { name: 'Delete' }).click();

  // Wait for redirect back to monitors list
  await page.waitForURL('**/monitors', { timeout: 10000 });
  await expect(page).toHaveURL(/.*\/monitors$/);

  // Verify the monitor is no longer in the list
  await searchMonitors(page, monitorName);
  await expect(page.getByText('No monitors found')).toBeVisible();
}

test.describe('Monitor CRUD Operations', () => {
  test('HTTP monitor forms start without a Content-Type header', async ({ page }) => {
    for (const type of [null, 'HTTP(s) - Keyword', 'HTTP(s) - Json Query']) {
      await page.goto('/monitors/new');
      if (type) {
        const select = page.getByRole('combobox', { name: 'Type', exact: true });
        await select.click();
        await page.getByRole('option', { name: type, exact: true }).click();
        await expect(select).toHaveText(type);
      }
      await expect(page.locator('[name="httpOptions.headers"]')).toHaveValue('');
    }
  });

  test('A timeout error clears when the interval is raised enough', async ({ page }) => {
    const monitorName = randomMonitorName();
    const tooLong = page.getByText("Can't be more than 80% of the check interval");
    await page.goto('/monitors/new');
    await page.locator('input[name="name"]').fill(monitorName);
    await page.locator('input[name="url"]').fill(targetURL('monitor-crud-timeout'));
    await page.locator('input[name="interval"]').fill('60');
    await page.locator('input[name="timeout"]').fill('49');

    // 49 s is above 80% of 60 s: the form doesn't submit and says why under the timeout
    await page.getByRole('button', { name: 'Create', exact: true }).click();
    await expect(tooLong).toBeVisible();
    await expect(page).toHaveURL(/\/monitors\/new$/);

    // A longer interval makes the timeout fit, and the message goes without touching the timeout
    await page.locator('input[name="interval"]').fill('100');
    await expect(tooLong).toBeHidden();

    await page.getByRole('button', { name: 'Create', exact: true }).click();
    await expect(page).toHaveURL(/\/monitors\/(?!new$)[A-Za-z0-9-]+$/, { timeout: 10000 });
    await deleteMonitor(page, monitorName);
  });

  for (const [type, form] of Object.entries(monitorForms)) {
    test(`${type} Monitor CRUD`, async ({ page }) => {
      const monitorName = randomMonitorName();

      await createMonitor(page, monitorName, form);
      await verifyMonitorInList(page, monitorName);

      const updatedName = await editMonitor(page, monitorName, randomMonitorName());
      await deleteMonitor(page, updatedName);
    });
  }
});
