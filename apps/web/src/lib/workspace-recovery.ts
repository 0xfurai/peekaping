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

let handler: ((failedWorkspaceId: string | null) => void) | null = null;

// The response interceptor calls this for an answer that says Workspace not found, with the
// workspace the failed request named (its X-Workspace-ID). A component inside the router sets
// it, since moving to another address needs the router.
export const setWorkspaceNotFoundHandler = (next: ((failedWorkspaceId: string | null) => void) | null) => {
  handler = next;
};

export const notifyWorkspaceNotFound = (failedWorkspaceId: string | null) =>
  handler?.(failedWorkspaceId);

const sameWorkspace = (a: string | null, b: string | null): boolean =>
  a !== null && b !== null && a.toLowerCase() === b.toLowerCase();

// A page's request was refused because its workspace isn't the user's any more. Ask the server
// for the user's workspaces again and move to the default one's monitors. Requests that fail
// together share one refetch.
//
// The caller passes the workspace the failed request named. A request can outlive the page that
// sent it: the user may have switched to another workspace since. So it moves only when that
// workspace is still the one in the address, both before asking and again once /me has answered,
// and never replaces the page of a workspace the request wasn't about.
//
// It moves only when the default workspace isn't the failed one: if the server still says not
// found there, going again would fail the same way forever. When /me lists no workspace it
// moves nowhere, and the workspace layout shows the no-workspace page from the fresh /me.
export const recoverFromMissingWorkspace = (
  queryClient: QueryClient,
  navigate: (to: string, options: { replace: boolean }) => void
): ((failedWorkspaceId: string | null) => Promise<void>) => {
  // One recovery at a time per failed workspace; the refetch is shared by them (fetchQuery joins
  // a request in flight)
  const inFlight = new Map<string, Promise<void>>();
  return (failedWorkspaceId) => {
    if (failedWorkspaceId === null || !sameWorkspace(failedWorkspaceId, currentWorkspaceId())) {
      return Promise.resolve();
    }
    const key = failedWorkspaceId.toLowerCase();
    let running = inFlight.get(key);
    if (!running) {
      running = (async () => {
        try {
          const me = await queryClient.fetchQuery({ ...getMeOptions(), staleTime: 0 });
          const target = me.data?.defaultWorkspaceId;
          if (
            target &&
            !sameWorkspace(target, failedWorkspaceId) &&
            sameWorkspace(failedWorkspaceId, currentWorkspaceId())
          ) {
            navigate(inWorkspace(target, "/monitors"), { replace: true });
          }
        } catch (error) {
          console.error("Failed to refetch the workspaces after Workspace not found", error);
        } finally {
          inFlight.delete(key);
        }
      })();
      inFlight.set(key, running);
    }
    return running;
  };
};
