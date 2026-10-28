import { Navigate } from "react-router-dom";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";

// Where a workspace's own address, or one that matches no page, goes.
export function WorkspaceHome() {
  const workspacePath = useWorkspacePath();
  return <Navigate to={workspacePath("/monitors")} replace />;
}
