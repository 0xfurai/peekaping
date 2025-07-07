import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import MonitorsPage from "./app/monitors/page";
import NewMonitor from "./app/monitors/new/page";
import SettingsPage from "./app/settings/page";
import { Routes, Route, Navigate } from "react-router-dom";
import { client } from "./api/client.gen";
import ProxiesPage from "./app/proxies/page";
import NewProxy from "./app/proxies/new/page";
import NotificationChannelsPage from "./app/notification-channels/page";
import NewNotificationChannel from "./app/notification-channels/new/page";
import EditNotificationChannel from "./app/notification-channels/edit/page";
import MonitorPage from "./app/monitors/view/page";
import { ThemeProvider } from "@/components/theme-provider";
import EditMonitor from "./app/monitors/edit/page";
import SHLoginPage from "./app/login/page";
import SHRegisterPage from "./app/register/page";
import { useAuthStore } from "@/store/auth";
import { setupInterceptors } from "./interceptors";
import { WebSocketProvider } from "./context/websocket-context";
import StatusPagesPage from "./app/status-pages/page";
import NewStatusPage from "./app/status-pages/new/page";
import SecurityPage from "./app/security/page";
import EditProxy from "./app/proxies/edit/page";
import { TimezoneProvider } from "./context/timezone-context";
import { VersionMismatchAlert } from "./components/VersionMismatchAlert";
import { getConfig } from "./lib/config";
import MaintenancePage from "./app/maintenance/page";
import NewMaintenance from "./app/maintenance/new/page";
import EditMaintenance from "./app/maintenance/edit/page";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import PublicStatusPage from "./app/status/[slug]/page";

import dayjs from "dayjs";
import timezone from "dayjs/plugin/timezone";
import utc from "dayjs/plugin/utc";
import relativeTime from "dayjs/plugin/relativeTime";
import EditStatusPage from "./app/status-pages/edit/page";

dayjs.extend(utc);
dayjs.extend(timezone);
dayjs.extend(relativeTime);

export const configureClient = () => {
  const accessToken = useAuthStore.getState().accessToken;

  client.setConfig({
    baseURL: getConfig().API_URL + "/api/v1",
    headers: accessToken
      ? {
          Authorization: `Bearer ${accessToken}`,
        }
      : undefined,
  });
};

configureClient();
setupInterceptors();

useAuthStore.subscribe((state) => {
  client.setConfig({
    baseURL: getConfig().API_URL + "/api/v1",
    headers: state.accessToken
      ? {
          Authorization: `Bearer ${state.accessToken}`,
        }
      : undefined,
  });
});

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      staleTime: 1000 * 60 * 5, // 5 minutes
    },
    mutations: {
      retry: false,
    },
  },
});

export default function App() {
  const accessToken = useAuthStore((state) => state.accessToken);
  const isDev = import.meta.env.MODE === "development";

  return (
    <ThemeProvider defaultTheme="dark" storageKey="peekaping-ui-theme">
      <TimezoneProvider>
        <QueryClientProvider client={queryClient}>
          <VersionMismatchAlert />

          <WebSocketProvider>
            <Routes>
              {/* Public routes */}
              <Route path="/status/:slug" element={<PublicStatusPage />} />

              {!accessToken ? (
                <>
                  <Route path="/login" element={<SHLoginPage />} />
                  <Route path="/register" element={<SHRegisterPage />} />
                  <Route path="*" element={<Navigate to="/login" replace />} />
                </>
              ) : (
                <>
                  <Route path="/monitors" element={<MonitorsPage />} />
                  <Route path="/monitors/:id" element={<MonitorPage />} />
                  <Route path="/monitors/new" element={<NewMonitor />} />
                  <Route path="/monitors/:id/edit" element={<EditMonitor />} />

                  <Route path="/status-pages" element={<StatusPagesPage />} />
                  <Route path="/status-pages/new" element={<NewStatusPage />} />
                  <Route
                    path="/status-pages/:id/edit"
                    element={<EditStatusPage />}
                  />

                  <Route path="/proxies" element={<ProxiesPage />} />
                  <Route path="/proxies/new" element={<NewProxy />} />
                  <Route path="/proxies/:id/edit" element={<EditProxy />} />

                  <Route
                    path="/notification-channels"
                    element={<NotificationChannelsPage />}
                  />
                  <Route
                    path="/notification-channels/new"
                    element={<NewNotificationChannel />}
                  />
                  <Route
                    path="/notification-channels/:id/edit"
                    element={<EditNotificationChannel />}
                  />

                  <Route path="/maintenances" element={<MaintenancePage />} />
                  <Route path="/maintenances/new" element={<NewMaintenance />} />
                  <Route
                    path="/maintenances/:id/edit"
                    element={<EditMaintenance />}
                  />

                  <Route path="/settings" element={<SettingsPage />} />
                  <Route path="/security" element={<SecurityPage />} />

                  <Route
                    path="*"
                    element={<Navigate to="/monitors" replace />}
                  />
                </>
              )}
            </Routes>
          </WebSocketProvider>
          {isDev && <ReactQueryDevtools initialIsOpen={false} />}
        </QueryClientProvider>
      </TimezoneProvider>
    </ThemeProvider>
  );
}
