import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { getMeOptions } from "@/api/@tanstack/react-query.gen";
import { enterWorkspace } from "@/lib/enter-workspace";

// Goes to a workspace after the user's membership of it changed (joined by an invite, made it,
// left another): to its monitors, or to the page given.
export const useEnterWorkspace = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useCallback(
    (workspaceId: string, path?: string) =>
      enterWorkspace({
        workspaceId,
        path,
        refreshMe: () => queryClient.fetchQuery({ ...getMeOptions(), staleTime: 0 }),
        navigate: (to) => navigate(to),
      }),
    [queryClient, navigate]
  );
};
