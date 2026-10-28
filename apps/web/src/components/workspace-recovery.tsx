import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
  recoverFromMissingWorkspace,
  setWorkspaceNotFoundHandler,
} from "@/lib/workspace-recovery";

// Gives the response interceptor the router: an answer of Workspace not found moves the page to
// the user's default workspace (lib/workspace-recovery.ts). It renders nothing.
export function WorkspaceRecovery() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  useEffect(() => {
    const recover = recoverFromMissingWorkspace(queryClient, navigate);
    setWorkspaceNotFoundHandler((failedWorkspaceId) => void recover(failedWorkspaceId));
    return () => setWorkspaceNotFoundHandler(null);
  }, [queryClient, navigate]);

  return null;
}
