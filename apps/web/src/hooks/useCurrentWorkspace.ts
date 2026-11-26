import { useLocation } from "react-router-dom";
import { useMe } from "@/hooks/useMe";
import { workspaceIdFromPathname } from "@/lib/workspace-path";

// The workspace the address names, as /me lists it; undefined while /me isn't loaded or the
// address names none of the user's workspaces.
export const useCurrentWorkspace = () => {
  const { pathname } = useLocation();
  const workspaceId = workspaceIdFromPathname(pathname)?.toLowerCase();
  return useMe().data?.data?.workspaces?.find((w) => w.id.toLowerCase() === workspaceId);
};
