import type { QueryClient } from "@tanstack/react-query";
import type { AxiosError } from "axios";
import { getMeOptions } from "@/api/@tanstack/react-query.gen";
import { currentWorkspaceId, inWorkspace } from "@/lib/workspace-path";

// What the server answers (404) for a request that names a workspace the user isn't a member
// of, such as one they were just removed from (workspace_auth.go).
export const WORKSPACE_NOT_FOUND = "Workspace not found";

export const isWorkspaceNotFound = (error: AxiosError): boolean =>
  error.response?.status === 404 &&
  (error.response.data as { message?: unknown } | undefined)?.message === WORKSPACE_NOT_FOUND;

let handler: (() => void) | null = null;

// The response interceptor calls this for an answer that says Workspace not found. A component
// inside the router sets it, since moving to another address needs the router.
export const setWorkspaceNotFoundHandler = (next: (() => void) | null) => {
  handler = next;
};

export const notifyWorkspaceNotFound = () => handler?.();

// A page's request was refused because its workspace isn't the user's any more. Ask the server
// for the user's workspaces again and move to the default one's monitors. Requests that fail
// together share one refetch.
//
// It moves only when the default workspace isn't the one in the address: if the server still says
// not found there, going again would fail the same way forever. When /me lists no workspace it
// moves nowhere, and the workspace layout shows the no-workspace page from the fresh /me.
export const recoverFromMissingWorkspace = (
  queryClient: QueryClient,
  navigate: (to: string, options: { replace: boolean }) => void
): (() => Promise<void>) => {
  let inFlight: Promise<void> | null = null;
  return () => {
    inFlight ??= (async () => {
      try {
        const me = await queryClient.fetchQuery({ ...getMeOptions(), staleTime: 0 });
        const target = me.data?.defaultWorkspaceId;
        if (target && target !== currentWorkspaceId()) {
          navigate(inWorkspace(target, "/monitors"), { replace: true });
        }
      } catch (error) {
        console.error("Failed to refetch the workspaces after Workspace not found", error);
      } finally {
        inFlight = null;
      }
    })();
    return inFlight;
  };
};
