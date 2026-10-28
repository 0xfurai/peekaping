import { Navigate, Outlet } from "react-router-dom";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { inWorkspace } from "@/lib/workspace-path";
import { Loading } from "@/components/loading";
import { NoWorkspace } from "@/components/no-workspace";

// The element of /admin: the install's own pages, outside every workspace. Only an instance admin
// sees them: anyone else goes to the monitors of their default workspace, and nothing of the area
// renders for them. The server refuses them as well; this only keeps the app from showing it.
export function AdminLayout() {
  const { t } = useLocalizedTranslation();
  const { data, isPending, isError } = useMe();

  if (isPending) return <Loading />;
  if (isError || !data?.data) {
    return <div className="p-4 w-full">{t("messages.unexpected_error")}</div>;
  }

  const { user, defaultWorkspaceId } = data.data;
  if (!user.instanceAdmin) {
    if (!defaultWorkspaceId) return <NoWorkspace />;
    return <Navigate to={inWorkspace(defaultWorkspaceId, "/monitors")} replace />;
  }
  return <Outlet />;
}
