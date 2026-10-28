import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import {
  recoverFromMissingWorkspace,
  setWorkspaceNotFoundHandler,
} from "@/lib/workspace-recovery";

// Gives the response interceptor the router: an answer of Workspace not found moves the page to
// the user's default workspace (lib/workspace-recovery.ts). When the workspaces can't be fetched
// it says so, with a Retry that runs the recovery again. It renders nothing.
export function WorkspaceRecovery() {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  useEffect(() => {
    const recover = recoverFromMissingWorkspace(queryClient, navigate, (retry, failedWorkspaceId) =>
      toast.error(t("workspaces.refresh_failed"), {
        id: `workspace-recovery-${failedWorkspaceId.toLowerCase()}`,
        action: { label: t("common.retry"), onClick: retry },
      })
    );
    setWorkspaceNotFoundHandler((failedWorkspaceId) => void recover(failedWorkspaceId));
    return () => setWorkspaceNotFoundHandler(null);
  }, [queryClient, navigate, t]);

  return null;
}
