import type { QueryClient } from "@tanstack/react-query";

// Queries that don't depend on who is signed in or which workspace is open.
// The custom-domain lookup gates the router, so it stays mounted and cached.
const KEPT = new Set(["getStatusPagesDomainByDomain"]);

const idOf = (queryKey: readonly unknown[]): unknown =>
  (queryKey[0] as { _id?: unknown } | undefined)?._id;

// React-query keys don't carry the workspace, so what is cached under one
// workspace would show under the next. Call this when the workspace changes
// (keepMe) or the signed-in user does (everything, /me included).
export const dropCachedData = (
  queryClient: QueryClient,
  { keepMe }: { keepMe: boolean }
) => {
  queryClient.removeQueries({
    predicate: (query) => {
      const id = idOf(query.queryKey);
      if (typeof id === "string" && KEPT.has(id)) return false;
      return !(keepMe && id === "getMe");
    },
  });
};
