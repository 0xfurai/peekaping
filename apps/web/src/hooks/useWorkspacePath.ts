import { useCallback } from "react";
import { useLocation } from "react-router-dom";
import { inWorkspace, workspaceIdFromPathname } from "@/lib/workspace-path";

/**
 * Returns a function that puts a signed-in page's path in the workspace the
 * address names: `path("/monitors")` is `/w/<id>/monitors`. Public pages
 * (status pages, login, register) keep their own addresses and don't use it.
 */
export const useWorkspacePath = () => {
  const { pathname } = useLocation();
  const workspaceId = workspaceIdFromPathname(pathname);
  return useCallback(
    (path: string) => (workspaceId ? inWorkspace(workspaceId, path) : path),
    [workspaceId]
  );
};
