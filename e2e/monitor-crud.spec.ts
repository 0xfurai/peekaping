import { test, expect } from '@playwright/test';

const randomMonitorName = () => `Test Monitor ${Math.random().toString(36).substring(2, 15)}`;

// Common helper functions for monitor CRUD operations
async function createMonitor(page: any, monitorName: string, monitorType: string, typeSpecificData: any) {
    await page.goto('/monitors');
    await page.waitForURL('**/monitors', { timeout: 10000 });
    await expect(page).toHaveURL(/.*\/monitors$/);

    // Create a new monitor
    await page.getByTestId('create-entity').click();
    await page.waitForURL('**/monitors/new', { timeout: 5000 });
    await expect(page).toHaveURL(/.*\/monitors\/new$/);

    // Fill basic monitor information

    // Select monitor type if not HTTP (which is default)
    if (monitorType !== 'http') {
        // Click on the select trigger to open the dropdown
        // next sibling after label having text "Type"
        // await page.locator('label:has-text("Type") + div').click();
        await page.getByRole('combobox', { name: 'Type' }).click();
        // Select the monitor type from the dropdown
        await page.getByRole('option', { name: new RegExp(monitorType, 'i') }).click();
        // Wait for form to update with new fields
        await page.waitForTimeout(1000);
    }

    await page.locator('input[name="name"]').fill(monitorName);

    // Fill type-specific fields
    for (const [fieldName, fieldValue] of Object.entries(typeSpecificData)) {
        const field = page.locator(`input[name="${fieldName}"], textarea[name="${fieldName}"], select[name="${fieldName}"]`);
        if (await field.count() > 0) {
            if (fieldName.includes('port') || fieldName.includes('packet_size')) {
                await field.fill(String(fieldValue));
            } else if (typeof fieldValue === 'boolean') {
                // Handle boolean fields (checkboxes)
                if (fieldValue) {
                    await field.check();
                } else {
                    await field.uncheck();
                }
            } else {
                await field.fill(String(fieldValue));
            }
        }
    }

    // Submit the form
    await page.getByRole('button', { name: 'Create' }).click();
    // await page.waitForURL(/.*\/monitors/, { timeout: 10000 });
    await page.waitForURL(/.*\/monitors\/[A-Za-z0-9-]+$/, { timeout: 10000 });

    // Go back to monitors list if we're on the detail page
    if (!page.url().endsWith('/monitors')) {
        await page.goto('/monitors');
        await page.waitForURL('**/monitors', { timeout: 5000 });
    }

    return monitorName;
}

async function verifyMonitorInList(page: any, monitorName: string) {
    await expect(page.getByText(monitorName)).toBeVisible({ timeout: 10000 });
}

async function editMonitor(page: any, monitorName: string, newMonitorName: string) {
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

async function deleteMonitor(page: any, monitorName: string) {
    // Delete the monitor
    await page.getByRole('button', { name: /delete/i }).click();

    // Confirm deletion in the dialog
    await expect(page.getByText(/are you absolutely sure/i)).toBeVisible({ timeout: 5000 });
    await page.getByRole('button', { name: 'Delete' }).click();

    // Wait for redirect back to monitors list
    await page.waitForURL('**/monitors', { timeout: 10000 });
    await expect(page).toHaveURL(/.*\/monitors$/);

    // Verify the monitor is no longer in the list
    await expect(page.getByText(monitorName)).not.toBeVisible();
}

test.describe('Monitor CRUD Operations', () => {
    test.beforeEach(async ({ page }) => {
        // Fetch user organizations
        const response = await page.request.get('/api/v1/user/organizations');
        expect(response.ok()).toBeTruthy();
        const json = await response.json();

        // Check if we have any organizations
        if (json.data && json.data.length > 0) {
            const orgId = json.data[0].organization_id;
            // Set default header for all requests in this page context
            await page.setExtraHTTPHeaders({
                'X-Organization-ID': orgId
            });
        }
    });
    test('HTTP Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            url: 'https://httpbin.org/status/200',
        };

        // Create HTTP monitor
        await createMonitor(page, monitorName, 'http', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('TCP Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            host: 'httpbin.org',
            port: 80
        };

        // Create TCP monitor
        await createMonitor(page, monitorName, 'tcp', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('Ping Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            host: 'httpbin.org',
            packet_size: 32
        };

        // Create Ping monitor
        await createMonitor(page, monitorName, 'ping', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('DNS Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            host: 'httpbin.org',
            resolver_server: '1.1.1.1',
            port: 53,
            resolve_type: 'A'
        };

        // Create DNS monitor
        await createMonitor(page, monitorName, 'dns', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('Push Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            // Push monitors auto-generate their token, so no additional fields needed
        };

        // Create Push monitor
        await createMonitor(page, monitorName, 'push', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('Docker Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            container_id: 'test-container',
            connection_type: 'socket',
            docker_daemon: '/var/run/docker.sock'
        };

        // Create Docker monitor
        await createMonitor(page, monitorName, 'docker', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('MySQL Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            connection_string: 'mysql://testuser:testpass@localhost:3306/testdb',
            query: 'SELECT 1'
        };

        // Create MySQL monitor
        await createMonitor(page, monitorName, 'mysql', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('PostgreSQL Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            database_connection_string: 'postgres://testuser:testpass@localhost:5432/testdb',
            database_query: 'SELECT 1'
        };

        // Create PostgreSQL monitor
        await createMonitor(page, monitorName, 'postgres', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('MongoDB Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            connectionString: 'mongodb://testuser:testpass@localhost:27017/testdb',
            command: '{"ping": 1}',
            jsonPath: '$',
            expectedValue: ''
        };

        // Create MongoDB monitor
        await createMonitor(page, monitorName, 'mongodb', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('Redis Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            databaseConnectionString: 'redis://testuser:testpass@localhost:6379',
            ignoreTls: false
        };

        // Create Redis monitor
        await createMonitor(page, monitorName, 'redis', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test.skip('SQL Server Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            connection_string: 'mssql://testuser:testpass@localhost:1433/testdb',
            query: 'SELECT 1'
        };

        // Create SQL Server monitor
        await createMonitor(page, monitorName, 'sqlserver', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test('SNMP Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            host: '192.168.1.1',
            port: 161,
            snmp_version: '2c',
            community: 'public',
            oid: '1.3.6.1.2.1.1.1.0'
        };

        // Create SNMP monitor
        await createMonitor(page, monitorName, 'snmp', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test.skip('GRPC-Keyword Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            url: 'grpc://localhost:50051',
            service: 'TestService',
            method: 'TestMethod',
            keyword: 'success'
        };

        // Create GRPC-Keyword monitor
        await createMonitor(page, monitorName, 'grpc-keyword', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test.skip('MQTT Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            host: 'localhost',
            port: 1883,
            topic: 'test/topic',
            username: 'testuser',
            password: 'testpass'
        };

        // Create MQTT monitor
        await createMonitor(page, monitorName, 'mqtt', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test.skip('RabbitMQ Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            connection_string: 'amqp://testuser:testpass@localhost:5672',
            queue: 'test-queue'
        };

        // Create RabbitMQ monitor
        await createMonitor(page, monitorName, 'rabbitmq', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });

    test.skip('Kafka Producer Monitor CRUD', async ({ page }) => {
        const monitorName = randomMonitorName();
        const typeSpecificData = {
            brokers: 'localhost:9092',
            topic: 'test-topic',
            message: '{"test": "message"}'
        };

        // Create Kafka Producer monitor
        await createMonitor(page, monitorName, 'kafka-producer', typeSpecificData);

        // Verify monitor in list
        await verifyMonitorInList(page, monitorName);

        // Edit monitor
        const updatedName = await editMonitor(page, monitorName, randomMonitorName());

        // Delete monitor
        await deleteMonitor(page, updatedName);
    });
});

