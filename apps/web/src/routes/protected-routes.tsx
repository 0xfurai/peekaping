import { Route, Navigate } from "react-router-dom";
import MonitorsPage from "@/app/monitors/page";
import NewMonitor from "@/app/monitors/new/page";
import SettingsPage from "@/app/settings/page";
import ProxiesPage from "@/app/proxies/page";
import NewProxy from "@/app/proxies/new/page";
import NotificationChannelsPage from "@/app/notification-channels/page";
import NewNotificationChannel from "@/app/notification-channels/new/page";
import EditNotificationChannel from "@/app/notification-channels/edit/page";
import MonitorPage from "@/app/monitors/view/page";
import EditMonitor from "@/app/monitors/edit/page";
import StatusPagesPage from "@/app/status-pages/page";
import NewStatusPage from "@/app/status-pages/new/page";
import SecurityPage from "@/app/security/page";
import EditProxy from "@/app/proxies/edit/page";
import MaintenancePage from "@/app/maintenance/page";
import NewMaintenance from "@/app/maintenance/new/page";
import EditMaintenance from "@/app/maintenance/edit/page";
import EditStatusPage from "@/app/status-pages/edit/page";
import TagsPage from "@/app/tags/page";
import NewTag from "@/app/tags/new/page";
import EditTag from "@/app/tags/edit/page";
import { OrganizationLayout } from "@/components/organization-layout";
import CreateOrganizationPage from "@/app/create-organization/page";
import OrganizationSettingsPage from "@/app/organization/settings/page";
import OrganizationMembersPage from "@/app/organization/members/page";
import { RootRedirect } from "@/components/root-redirect";
import UserInvitationsPage from "@/app/user/invitations/page";
import OnboardingPage from "@/app/onboarding/page";

export const protectedRoutes = [
    <Route path="/" element={<RootRedirect />} />,
    <Route path="/onboarding" element={<OnboardingPage />} />,
    <Route path="/create-organization" element={<CreateOrganizationPage />} />,
    // Account routes (Global)
    <Route path="/account" element={<OrganizationLayout isGlobal={true} />}>
        <Route path="settings" element={<SettingsPage />} />
        <Route path="security" element={<SecurityPage />} />
        <Route path="invitations" element={<UserInvitationsPage />} />
    </Route>,
    <Route path="/:slug" element={<OrganizationLayout />}>
        <Route index element={<Navigate to="monitors" replace />} />

    // Monitor routes
        <Route path="monitors" element={<MonitorsPage />} />
        <Route path="monitors/:id" element={<MonitorPage />} />
        <Route path="monitors/new" element={<NewMonitor />} />
        <Route path="monitors/:id/edit" element={<EditMonitor />} />

    // Status page routes
        <Route path="status-pages" element={<StatusPagesPage />} />
        <Route path="status-pages/new" element={<NewStatusPage />} />
        <Route path="status-pages/:id/edit" element={<EditStatusPage />} />

    // Proxy routes
        <Route path="proxies" element={<ProxiesPage />} />
        <Route path="proxies/new" element={<NewProxy />} />
        <Route path="proxies/:id/edit" element={<EditProxy />} />

    // Notification channel routes
        <Route path="notification-channels" element={<NotificationChannelsPage />} />
        <Route path="notification-channels/new" element={<NewNotificationChannel />} />
        <Route path="notification-channels/:id/edit" element={<EditNotificationChannel />} />

    // Maintenance routes
        <Route path="maintenances" element={<MaintenancePage />} />
        <Route path="maintenances/new" element={<NewMaintenance />} />
        <Route path="maintenances/:id/edit" element={<EditMaintenance />} />

    // Organization routes
        <Route path="settings/organization" element={<OrganizationSettingsPage />} />
        <Route path="settings/members" element={<OrganizationMembersPage />} />



    // Tag routes
        <Route path="tags" element={<TagsPage />} />
        <Route path="tags/new" element={<NewTag />} />
        <Route path="tags/:id/edit" element={<EditTag />} />

    // Default redirect
        <Route path="*" element={<Navigate to="monitors" replace />} />
    </Route>
]; 