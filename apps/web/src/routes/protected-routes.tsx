import { Route } from "react-router-dom";
import { WorkspaceLayout } from "@/components/workspace-layout";
import { RedirectToWorkspace } from "@/components/redirect-to-workspace";
import { WorkspaceHome } from "@/components/workspace-home";
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
import ProbesPage from "@/app/probes/page";
import ProbePage from "@/app/probes/view/page";
import MaintenancePage from "@/app/maintenance/page";
import NewMaintenance from "@/app/maintenance/new/page";
import EditMaintenance from "@/app/maintenance/edit/page";
import EditStatusPage from "@/app/status-pages/edit/page";
import TagsPage from "@/app/tags/page";
import NewTag from "@/app/tags/new/page";
import EditTag from "@/app/tags/edit/page";

// The signed-in app lives under /w/<workspaceId>/ (docs/adr/0005, section 7).
// Any other address is an old one, or names no workspace, and redirects into
// the user's default workspace.
export const protectedRoutes = [
  <Route path="/w/:workspaceId" element={<WorkspaceLayout />}>
    {/* Monitor routes */}
    <Route path="monitors" element={<MonitorsPage />} />
    <Route path="monitors/:id" element={<MonitorPage />} />
    <Route path="monitors/new" element={<NewMonitor />} />
    <Route path="monitors/:id/edit" element={<EditMonitor />} />

    {/* Status page routes */}
    <Route path="status-pages" element={<StatusPagesPage />} />
    <Route path="status-pages/new" element={<NewStatusPage />} />
    <Route path="status-pages/:id/edit" element={<EditStatusPage />} />

    {/* Proxy routes */}
    <Route path="proxies" element={<ProxiesPage />} />
    <Route path="proxies/new" element={<NewProxy />} />
    <Route path="proxies/:id/edit" element={<EditProxy />} />

    {/* Probe routes */}
    <Route path="probes" element={<ProbesPage />} />
    <Route path="probes/:id" element={<ProbePage />} />

    {/* Notification channel routes */}
    <Route path="notification-channels" element={<NotificationChannelsPage />} />
    <Route path="notification-channels/new" element={<NewNotificationChannel />} />
    <Route path="notification-channels/:id/edit" element={<EditNotificationChannel />} />

    {/* Maintenance routes */}
    <Route path="maintenances" element={<MaintenancePage />} />
    <Route path="maintenances/new" element={<NewMaintenance />} />
    <Route path="maintenances/:id/edit" element={<EditMaintenance />} />

    {/* Settings and security */}
    <Route path="settings" element={<SettingsPage />} />
    <Route path="security" element={<SecurityPage />} />

    {/* Tag routes */}
    <Route path="tags" element={<TagsPage />} />
    <Route path="tags/new" element={<NewTag />} />
    <Route path="tags/:id/edit" element={<EditTag />} />

    {/* The workspace's own address, and an address nothing matches in it */}
    <Route index element={<WorkspaceHome />} />
    <Route path="*" element={<WorkspaceHome />} />
  </Route>,

  // Signing in leaves the app on /login: go to the app's start
  <Route path="/login" element={<RedirectToWorkspace path="/" />} />,
  <Route path="/register" element={<RedirectToWorkspace path="/" />} />,

  // Old addresses: /monitors/<id>, /settings, / ...
  <Route path="*" element={<RedirectToWorkspace />} />
];
