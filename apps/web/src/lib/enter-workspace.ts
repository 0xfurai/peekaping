import { inWorkspace } from "@/lib/workspace-path";

// Goes to a page of a workspace after the user's membership of it changed. /me is refreshed first:
// the workspace layout sends an address whose workspace /me doesn't list to the default one. The
// page is the monitors unless given.
export const enterWorkspace = async ({
  workspaceId,
  path = "/monitors",
  refreshMe,
  navigate,
}: {
  workspaceId: string;
  path?: string;
  refreshMe: () => Promise<unknown>;
  navigate: (to: string) => void;
}): Promise<void> => {
  await refreshMe();
  navigate(inWorkspace(workspaceId, path));
};
