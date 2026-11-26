import { Navigate, useLocation } from "react-router-dom";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { inWorkspace } from "@/lib/workspace-path";
import { Loading } from "@/components/loading";
import { NoWorkspace } from "@/components/no-workspace";

// An address without a workspace (`/monitors/<id>`, `/settings`, `/`) opens the
// same page in the user's default workspace, with its search and hash.
export function RedirectToWorkspace({ path }: { path?: string }) {
  const { pathname, search, hash } = useLocation();
  const { t } = useLocalizedTranslation();
  const { data, isPending, isError } = useMe();

  if (isPending) return <Loading />;
  if (isError || !data?.data) {
    return <div className="p-4 w-full">{t("messages.unexpected_error")}</div>;
  }

  const { defaultWorkspaceId } = data.data;
  if (!defaultWorkspaceId) return <NoWorkspace />;

  return (
    <Navigate
      to={`${inWorkspace(defaultWorkspaceId, path ?? pathname)}${search}${hash}`}
      replace
    />
  );
}
