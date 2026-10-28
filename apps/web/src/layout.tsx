import {
  SidebarInset,
  SidebarProvider,
  // SidebarTrigger
} from "@/components/ui/sidebar";
import { useLocation } from "react-router-dom";
import { AdminSidebar } from "@/components/admin-sidebar";
import { AppSidebar } from "@/components/app-sidebar";
import { isAdminPath } from "@/lib/workspace-path";
import { SiteHeader } from "./components/app-header";
import { TelemetryNotice } from "./components/telemetry-notice";
import { VerificationBanner } from "./components/verification-banner";
import { useLocalizedTranslation } from "./hooks/useTranslation";

export default function Layout({
  children,
  pageName,
  error,
  isLoading,
  onCreate,
}: {
  children: React.ReactNode;
  pageName: string;
  onCreate?: () => void;
  error?: React.ReactNode;
  isLoading?: boolean;
  }) {
  const { t } = useLocalizedTranslation();
  // The admin area has a sidebar of its own, since it is outside every workspace
  const { pathname } = useLocation();

  return (
    <SidebarProvider>
      {isAdminPath(pathname) ? (
        <AdminSidebar variant="inset" />
      ) : (
        <AppSidebar variant="inset" />
      )}
      <SidebarInset>
        <SiteHeader pageName={pageName} onCreate={onCreate} />
        <div className="empty:hidden px-4 pt-4">
          <VerificationBanner />
        </div>
        <div className="empty:hidden px-4 pt-4">
          <TelemetryNotice />
        </div>
        {isLoading ? (
          <div className="p-4 w-full">{t("common.loading")}</div>
        ) : (
          error || <main className="p-4 w-full">{children}</main>
        )}
      </SidebarInset>
    </SidebarProvider>
  );
}
