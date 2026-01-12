import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
    apisidebar: [
        {
            type: "doc",
            id: "api/vigi-api",
        },
        {
            type: "category",
            label: "Badges",
            items: [
                {
                    type: "doc",
                    id: "api/get-certificate-expiry-badge",
                    label: "Get certificate expiry badge (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-ping-badge",
                    label: "Get ping badge (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-response-time-badge",
                    label: "Get response time badge (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-status-badge",
                    label: "Get status badge (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-uptime-badge",
                    label: "Get uptime badge (Public)",
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
                    label: "Get server health (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-server-version",
                    label: "Get server version (Public)",
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
                    label: "Get maintenances (ApiKey)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/create-maintenance",
                    label: "Create maintenance (ApiKey)",
                    className: "api-method post",
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
                    label: "Get monitors (ApiKey + OrgID)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/create-monitor",
                    label: "Create monitor (ApiKey + OrgID)",
                    className: "api-method post",
                },
                {
                    type: "doc",
                    id: "api/get-monitor-by-id",
                    label: "Get monitor by ID (ApiKey + OrgID)",
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
                    id: "api/get-monitors-by-i-ds",
                    label: "Get monitors by IDs (ApiKey + OrgID)",
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
                    label: "Get notification channels (ApiKey)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/create-notification-channel",
                    label: "Create notification channel (ApiKey)",
                    className: "api-method post",
                },
                {
                    type: "doc",
                    id: "api/test-notification-channel",
                    label: "Test notification channel (ApiKey)",
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
                    label: "Get proxies (ApiKey)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/create-proxy",
                    label: "Create proxy (ApiKey)",
                    className: "api-method post",
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
                    label: "Delete setting by key (ApiKey)",
                    className: "api-method delete",
                },
                {
                    type: "doc",
                    id: "api/get-setting-by-key",
                    label: "Get setting by key (ApiKey)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/set-setting-by-key",
                    label: "Set setting by key (ApiKey)",
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
                    label: "Get all status pages (ApiKey)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/create-a-new-status-page",
                    label: "Create a new status page (ApiKey)",
                    className: "api-method post",
                },
                {
                    type: "doc",
                    id: "api/delete-a-status-page",
                    label: "Delete a status page (ApiKey)",
                    className: "api-method delete",
                },
                {
                    type: "doc",
                    id: "api/get-a-status-page-by-id",
                    label: "Get a status page by ID (ApiKey)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/update-a-status-page",
                    label: "Update a status page (ApiKey)",
                    className: "api-method patch",
                },
                {
                    type: "doc",
                    id: "api/get-a-status-page-by-domain-name",
                    label: "Get a status page by domain name (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-a-status-page-by-slug",
                    label: "Get a status page by slug (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-monitors-for-a-status-page-by-slug-with-heartbeats-and-uptime",
                    label: "Get monitors for a status page (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-monitors-for-a-status-page-by-slug-for-homepage",
                    label: "Get monitors for homepage (Public)",
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
                    label: "Get tags (ApiKey)",
                    className: "api-method get",
                },
            ],
        },
        {
            type: "category",
            label: "Organization",
            items: [
                {
                    type: "doc",
                    id: "api/create-organization",
                    label: "Create organization (ApiKey)",
                    className: "api-method post",
                },
                {
                    type: "doc",
                    id: "api/list-user-organizations",
                    label: "List user organizations (ApiKey)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/get-organization-by-id",
                    label: "Get organization by ID (ApiKey)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/add-member-to-organization",
                    label: "Add member to organization (ApiKey)",
                    className: "api-method post",
                },
                {
                    type: "doc",
                    id: "api/list-organization-members",
                    label: "List organization members (ApiKey)",
                    className: "api-method get",
                },
            ],
        },
        {
            type: "category",
            label: "Invitations",
            items: [
                {
                    type: "doc",
                    id: "api/get-invitation-details-public",
                    label: "Get invitation details (Public)",
                    className: "api-method get",
                },
                {
                    type: "doc",
                    id: "api/accept-invitation",
                    label: "Accept invitation (Cookie)",
                    className: "api-method post",
                },
                {
                    type: "doc",
                    id: "api/get-user-pending-invitations",
                    label: "Get user pending invitations (Cookie)",
                    className: "api-method get",
                },
            ],
        },
    ],
};

export default sidebar.apisidebar;
