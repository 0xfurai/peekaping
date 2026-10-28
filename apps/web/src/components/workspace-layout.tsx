import { useEffect, useState } from "react";
import { Navigate, Outlet, useLocation, useParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { dropCachedData } from "@/lib/workspace-cache";
import { inWorkspace, withoutWorkspace } from "@/lib/workspace-path";
import { NoWorkspace } from "@/components/no-workspace";
import { Loading } from "@/components/loading";

// The element of /w/:workspaceId. It shows the pages below only while the
// address names a workspace the user is a member of; any other address goes to
// the same page in their default workspace.
export function WorkspaceLayout() {
  const { workspaceId = "" } = useParams();
  const { pathname, search, hash } = useLocation();
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const { data, isPending, isError } = useMe();

  // The workspace whose data the cache holds. Pages render only while it is the
  // address's workspace; on a change they unmount, the cache is emptied of
  // everything but /me, and the new workspace's pages mount with nothing stale.
  const [cachedFor, setCachedFor] = useState(workspaceId);
  const changed = cachedFor !== workspaceId;
  useEffect(() => {
    if (changed) {
      dropCachedData(queryClient, { keepMe: true });
      setCachedFor(workspaceId);
    }
  }, [changed, queryClient, workspaceId]);

  if (isPending) return <Loading />;
  if (isError || !data?.data) {
    return <div className="p-4 w-full">{t("messages.unexpected_error")}</div>;
  }

  const { defaultWorkspaceId, workspaces } = data.data;
  if (!defaultWorkspaceId || !workspaces?.length) return <NoWorkspace />;

  // Compare without regard to case; an upper-case copy of an id goes to the canonical one.
  const workspace = workspaces.find(
    (w) => w.id.toLowerCase() === workspaceId.toLowerCase()
  );
  if (!workspace || workspace.id !== workspaceId) {
    const target = workspace?.id ?? defaultWorkspaceId;
    return (
      <Navigate
        to={`${inWorkspace(target, withoutWorkspace(pathname))}${search}${hash}`}
        replace
      />
    );
  }

  if (changed) return <Loading />;
  return <Outlet />;
}
