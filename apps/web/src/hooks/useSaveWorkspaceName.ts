import { useCallback } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { getMeOptions, getMeQueryKey, patchWorkspaceMutation } from "@/api/@tanstack/react-query.gen";
import type { UtilsApiResponseAuthMeResponse } from "@/api/types.gen";
import { meWithWorkspaceName, saveWorkspaceName } from "@/lib/save-workspace-name";

// Renames the workspace the address names (an admin or owner may; the server answers 403 to a
// member), puts the new name in the cached /me and refreshes /me, so that the switcher shows the
// new name even when the refresh fails. Rejects when the rename fails.
export const useSaveWorkspaceName = () => {
  const queryClient = useQueryClient();
  const mutation = useMutation(patchWorkspaceMutation());
  const { mutateAsync } = mutation;
  const save = useCallback(
    (name: string, current: string) =>
      saveWorkspaceName({
        name,
        current,
        rename: async (next) => (await mutateAsync({ body: { name: next } })).data,
        remember: (workspace) =>
          queryClient.setQueryData<UtilsApiResponseAuthMeResponse>(getMeQueryKey(), (me) =>
            meWithWorkspaceName(me, workspace)
          ),
        refreshMe: () => queryClient.fetchQuery({ ...getMeOptions(), staleTime: 0 }),
      }),
    [mutateAsync, queryClient]
  );
  return { save, pending: mutation.isPending };
};
