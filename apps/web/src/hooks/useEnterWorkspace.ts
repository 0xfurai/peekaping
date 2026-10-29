import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { getMeOptions } from "@/api/@tanstack/react-query.gen";
import { inWorkspace } from "@/lib/workspace-path";

// Goes to a workspace's monitors after the user's membership of it changed (joined by an invite,
// made it, left another). /me is fetched first: the workspace layout sends an address whose
// workspace /me doesn't list to the default one.
export const useEnterWorkspace = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useCallback(
    async (workspaceId: string) => {
      await queryClient.fetchQuery({ ...getMeOptions(), staleTime: 0 });
      navigate(inWorkspace(workspaceId, "/monitors"));
    },
    [queryClient, navigate]
  );
};
