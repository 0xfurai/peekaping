import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api/vigi-api",
    },
    {
      type: "category",
      label: "api-keys",
      items: [
        {
          type: "doc",
          id: "api/get-api-keys",
          label: "Get API keys",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/create-api-key",
          label: "Create API key",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/delete-api-key",
          label: "Delete API key",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api/get-api-key",
          label: "Get API key",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/update-api-key",
          label: "Update API key",
          className: "api-method put",
        },
        {
          type: "doc",
          id: "api/get-api-key-configuration",
          label: "Get API key configuration",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "Auth",
      items: [
        {
          type: "doc",
          id: "api/disable-2-fa-totp-for-user",
          label: "Disable 2FA (TOTP) for user",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/enable-2-fa-totp-for-user",
          label: "Enable 2FA (TOTP) for user",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/verify-2-fa-totp-code-for-user",
          label: "Verify 2FA (TOTP) code for user",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/login-admin",
          label: "Login admin",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/update-user-password",
          label: "Update user password",
          className: "api-method put",
        },
        {
          type: "doc",
          id: "api/refresh-access-token",
          label: "Refresh access token",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/register-new-admin",
          label: "Register new admin",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Badges",
      items: [
        {
          type: "doc",
          id: "api/get-certificate-expiry-badge",
          label: "Get certificate expiry badge",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-ping-badge",
          label: "Get ping badge",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-response-time-badge",
          label: "Get response time badge",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-status-badge",
          label: "Get status badge",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-uptime-badge",
          label: "Get uptime badge",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "System",
      items: [
        {
          type: "doc",
          id: "api/get-server-health",
          label: "Get server health",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-server-version",
          label: "Get server version",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "Maintenances",
      items: [
        {
          type: "doc",
          id: "api/get-maintenances",
          label: "Get maintenances",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/create-maintenance",
          label: "Create maintenance",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/delete-maintenance",
          label: "Delete maintenance",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api/get-maintenance-by-id",
          label: "Get maintenance by ID",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/update-maintenance",
          label: "Update maintenance",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api/pause-maintenance",
          label: "Pause maintenance",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api/resume-maintenance",
          label: "Resume maintenance",
          className: "api-method patch",
        },
      ],
    },
    {
      type: "category",
      label: "Monitors",
      items: [
        {
          type: "doc",
          id: "api/get-monitors",
          label: "Get monitors",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/create-monitor",
          label: "Create monitor",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/delete-monitor",
          label: "Delete monitor",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api/get-monitor-by-id",
          label: "Get monitor by ID",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/update-monitor",
          label: "Update monitor",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api/get-paginated-heartbeats-for-a-monitor",
          label: "Get paginated heartbeats for a monitor",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/reset-monitor-data-heartbeats-and-stats",
          label: "Reset monitor data (heartbeats and stats)",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/get-monitor-stat-points-ping-up-down-from-stats-tables",
          label: "Get monitor stat points (ping/up/down) from stats tables",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-monitor-uptime-stats-24-h-30-d-365-d",
          label: "Get monitor uptime stats (24h, 30d, 365d)",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-monitor-tls-certificate-information",
          label: "Get monitor TLS certificate information",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-monitors-by-i-ds",
          label: "Get monitors by IDs",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "Notification channels",
      items: [
        {
          type: "doc",
          id: "api/get-notification-channels",
          label: "Get notification channels",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/create-notification-channel",
          label: "Create notification channel",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/delete-notification-channel",
          label: "Delete notification channel",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api/get-notification-channel-by-id",
          label: "Get notification channel by ID",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/update-notification-channel",
          label: "Update notification channel",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api/test-notification-channel",
          label: "Test notification channel",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "Proxies",
      items: [
        {
          type: "doc",
          id: "api/get-proxies",
          label: "Get proxies",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/create-proxy",
          label: "Create proxy",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/delete-proxy",
          label: "Delete proxy",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api/get-proxy-by-id",
          label: "Get proxy by ID",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/update-proxy",
          label: "Update proxy",
          className: "api-method patch",
        },
      ],
    },
    {
      type: "category",
      label: "Settings",
      items: [
        {
          type: "doc",
          id: "api/delete-setting-by-key",
          label: "Delete setting by key",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api/get-setting-by-key",
          label: "Get setting by key",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/set-setting-by-key",
          label: "Set setting by key",
          className: "api-method put",
        },
      ],
    },
    {
      type: "category",
      label: "Status Pages",
      items: [
        {
          type: "doc",
          id: "api/get-all-status-pages",
          label: "Get all status pages",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/create-a-new-status-page",
          label: "Create a new status page",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/delete-a-status-page",
          label: "Delete a status page",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api/get-a-status-page-by-id",
          label: "Get a status page by ID",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/update-a-status-page",
          label: "Update a status page",
          className: "api-method patch",
        },
        {
          type: "doc",
          id: "api/get-a-status-page-by-domain-name",
          label: "Get a status page by domain name",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-a-status-page-by-slug",
          label: "Get a status page by slug",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-monitors-for-a-status-page-by-slug-with-heartbeats-and-uptime",
          label: "Get monitors for a status page by slug with heartbeats and uptime",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/get-monitors-for-a-status-page-by-slug-for-homepage",
          label: "Get monitors for a status page by slug for homepage",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "Tags",
      items: [
        {
          type: "doc",
          id: "api/get-tags",
          label: "Get tags",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/create-tag",
          label: "Create tag",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/delete-tag",
          label: "Delete tag",
          className: "api-method delete",
        },
        {
          type: "doc",
          id: "api/get-tag-by-id",
          label: "Get tag by ID",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/update-tag",
          label: "Update tag",
          className: "api-method patch",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
