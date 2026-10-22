import {
  SidebarInset,
  SidebarProvider,
  // SidebarTrigger
} from "@/components/ui/sidebar";
import { AppSidebar } from "@/components/app-sidebar";
import { SiteHeader } from "./components/app-header";
import { TelemetryNotice } from "./components/telemetry-notice";
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

  return (
    <SidebarProvider>
      <AppSidebar variant="inset" />
      <SidebarInset>
        <SiteHeader pageName={pageName} onCreate={onCreate} />
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
