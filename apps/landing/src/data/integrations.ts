export type Integration = {
  name: string;
  description: string;
};

export type IntegrationGroup = {
  title: string;
  items: Integration[];
};

// Keep in step with apps/server/internal/modules/healthcheck/executor
export const monitorGroups: IntegrationGroup[] = [
  {
    title: "Web & Network",
    items: [
      { name: "HTTP/HTTPS", description: "Websites, APIs and web services" },
      { name: "TCP", description: "Port connectivity and availability" },
      { name: "Ping (ICMP)", description: "Reachability and round-trip latency" },
      { name: "DNS", description: "Query responses and resolution times" },
      { name: "SMTP", description: "Mail server connectivity and authentication" },
    ],
  },
  {
    title: "Application & Infrastructure",
    items: [
      { name: "Push", description: "Heartbeats from cron jobs and services" },
      { name: "Docker", description: "Container status and health" },
      { name: "gRPC", description: "Health and service checks with latency" },
      { name: "SNMP", description: "Device availability and key OIDs" },
    ],
  },
  {
    title: "Databases & Caches",
    items: [
      { name: "PostgreSQL", description: "Connect and run a lightweight query" },
      { name: "MySQL/MariaDB", description: "Connect and run a lightweight query" },
      { name: "SQL Server", description: "Connect and run a lightweight query" },
      { name: "MongoDB", description: "Ping, handshake and a simple read" },
      { name: "Redis", description: "PING, latency and basic health" },
    ],
  },
  {
    title: "Messaging & Streaming",
    items: [
      { name: "MQTT", description: "Connect, subscribe and publish smoke test" },
      { name: "RabbitMQ", description: "Connection and queue health" },
      { name: "Kafka Producer", description: "Produce a test message to a topic" },
    ],
  },
];

// Keep in step with apps/server/internal/modules/notification_channel/providers
export const channelGroups: IntegrationGroup[] = [
  {
    title: "Chat & Collaboration",
    items: [
      { name: "Slack", description: "Incoming webhook to channels" },
      { name: "Discord", description: "Channel webhooks" },
      { name: "Telegram", description: "Bot messages to users and channels" },
      { name: "Microsoft Teams", description: "Workflows webhooks to channels" },
      { name: "Google Chat", description: "Space webhooks" },
      { name: "Mattermost", description: "Incoming webhook to channels" },
      { name: "Matrix", description: "Send to rooms with an access token" },
      { name: "Signal", description: "Messages through signal-cli REST" },
      { name: "WhatsApp", description: "Through a WAHA gateway" },
      { name: "LINE", description: "Messages through the LINE Messaging API" },
      { name: "WeCom", description: "Enterprise messages to groups" },
    ],
  },
  {
    title: "On-call & Incidents",
    items: [
      { name: "PagerDuty", description: "Trigger and resolve incidents" },
      { name: "Opsgenie", description: "Alerts, routing and on-call" },
      { name: "Grafana OnCall", description: "Integrate with on-call schedules" },
      { name: "PagerTree", description: "Create and resolve alerts" },
    ],
  },
  {
    title: "Email, SMS & Webhooks",
    items: [
      { name: "Email (SMTP)", description: "Through your own mail server" },
      { name: "SendGrid", description: "Email through the SendGrid API" },
      { name: "Twilio", description: "SMS to any phone" },
      { name: "Webhook", description: "JSON to any HTTP endpoint" },
    ],
  },
  {
    title: "Push notifications",
    items: [
      { name: "ntfy", description: "Simple pub/sub push" },
      { name: "Gotify", description: "Self-hosted push server" },
      { name: "Pushover", description: "Mobile and desktop push" },
      { name: "Pushbullet", description: "Push to your devices" },
    ],
  },
];

export const monitorCount = monitorGroups.reduce((n, g) => n + g.items.length, 0);
export const channelCount = channelGroups.reduce((n, g) => n + g.items.length, 0);
