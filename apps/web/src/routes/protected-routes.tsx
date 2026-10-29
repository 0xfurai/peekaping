import { Navigate, Route } from "react-router-dom";
import { WorkspaceLayout } from "@/components/workspace-layout";
import { RedirectToWorkspace } from "@/components/redirect-to-workspace";
import { RedirectAfterLogin } from "@/components/redirect-after-login";
import { WorkspaceHome } from "@/components/workspace-home";
import { GET_STARTED_PATH } from "@/lib/workspace-path";
import { RequireInstanceAdmin } from "@/components/require-instance-admin";
import { AdminLayout } from "@/components/admin-layout";
import { RedirectProbesToAdmin } from "@/components/redirect-probes-to-admin";
import UsersPage from "@/app/admin/users/page";
import WorkspacesPage from "@/app/admin/workspaces/page";
import InstallSettingsPage from "@/app/admin/settings/page";
import MonitorsPage from "@/app/monitors/page";
import NewMonitor from "@/app/monitors/new/page";
import GetStartedPage from "@/app/get-started/page";
import SettingsPage from "@/app/settings/page";
import MembersPage from "@/app/settings/members/page";
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
//
// The admin area (/admin) holds what belongs to the install and not to a workspace, and only an
// instance admin sees it (PP-371).
export const protectedRoutes = [
  <Route path="/admin" element={<AdminLayout />}>
    <Route path="users" element={<UsersPage />} />
    <Route path="workspaces" element={<WorkspacesPage />} />
    <Route path="probes" element={<ProbesPage />} />
    <Route path="probes/:id" element={<ProbePage />} />
    <Route path="settings" element={<InstallSettingsPage />} />
    <Route index element={<Navigate to="/admin/users" replace />} />
    <Route path="*" element={<Navigate to="/admin/users" replace />} />
  </Route>,

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

    {/* Probes belong to the install and live in the admin area; the old address leads there */}
    <Route path="probes" element={<RequireInstanceAdmin><RedirectProbesToAdmin /></RequireInstanceAdmin>} />
    <Route path="probes/:id" element={<RequireInstanceAdmin><RedirectProbesToAdmin /></RequireInstanceAdmin>} />

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
    <Route path="settings/members" element={<MembersPage />} />
    <Route path="security" element={<SecurityPage />} />

    {/* Tag routes */}
    <Route path="tags" element={<TagsPage />} />
    <Route path="tags/new" element={<NewTag />} />
    <Route path="tags/:id/edit" element={<EditTag />} />

    {/* First-run setup of a workspace without monitors */}
    <Route path="get-started" element={<GetStartedPage />} />

    {/* The workspace's own address, and an address nothing matches in it */}
    <Route index element={<WorkspaceHome />} />
    <Route path="*" element={<WorkspaceHome />} />
  </Route>,

  // Signing in leaves the app on /login: go to the app's start
  <Route path="/login" element={<RedirectAfterLogin />} />,
  // Signing up leaves the app on /register, with the new user's workspace of their own: open its
  // Get started page. An invite signup never passes here: it stays on the invite page.
  <Route path="/register" element={<RedirectToWorkspace path={GET_STARTED_PATH} />} />,

  // The default workspace's Get started page, for a link made before the workspace is known
  <Route path={GET_STARTED_PATH} element={<RedirectToWorkspace path={GET_STARTED_PATH} />} />,

  // Old addresses: /monitors/<id>, /settings, / ...
  <Route path="*" element={<RedirectToWorkspace />} />
];
