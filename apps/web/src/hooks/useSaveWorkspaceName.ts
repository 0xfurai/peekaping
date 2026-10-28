import { useCallback } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { getMeOptions, patchWorkspaceMutation } from "@/api/@tanstack/react-query.gen";
import { saveWorkspaceName } from "@/lib/save-workspace-name";

// Renames the workspace the address names (an admin or owner may; the server answers 403 to a
// member) and refreshes /me so that the switcher shows the new name. Rejects when the rename fails.
export const useSaveWorkspaceName = () => {
  const queryClient = useQueryClient();
  const mutation = useMutation(patchWorkspaceMutation());
  const { mutateAsync } = mutation;
  const save = useCallback(
    (name: string, current: string) =>
      saveWorkspaceName({
        name,
        current,
        rename: (next) => mutateAsync({ body: { name: next } }),
        refreshMe: () => queryClient.fetchQuery({ ...getMeOptions(), staleTime: 0 }),
      }),
    [mutateAsync, queryClient]
  );
  return { save, pending: mutation.isPending };
};
