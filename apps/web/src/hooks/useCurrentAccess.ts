import { useLocation } from "react-router-dom";
import { useMe } from "@/hooks/useMe";
import { workspaceIdFromPathname } from "@/lib/workspace-path";

export type WorkspaceRole = "member" | "admin" | "owner";

export type CurrentAccess = {
  // Manages what belongs to the install: probes, the usage ping and error reports
  instanceAdmin: boolean;
  // The user's role in the workspace the address names; undefined while /me isn't loaded
  // or the address names no workspace the user belongs to
  role: WorkspaceRole | undefined;
  // The admin role and above: the workspace's settings
  canManageWorkspace: boolean;
};

// What the signed-in user may do here, from the /me query the workspace layout loads. Every
// page that hides or locks a control for lack of access asks this hook, so that nothing else
// decides. The server enforces the same rules; this only keeps the app from offering what
// it would answer 403 to. Without /me nobody has access.
export const useCurrentAccess = (): CurrentAccess => {
  const { data } = useMe();
  const { pathname } = useLocation();
  const workspaceId = workspaceIdFromPathname(pathname)?.toLowerCase();

  const me = data?.data;
  const role = me?.workspaces?.find((w) => w.id.toLowerCase() === workspaceId)?.role;
  const known = role === "member" || role === "admin" || role === "owner";
  return {
    instanceAdmin: me?.user.instanceAdmin === true,
    role: known ? role : undefined,
    canManageWorkspace: role === "admin" || role === "owner",
  };
};
