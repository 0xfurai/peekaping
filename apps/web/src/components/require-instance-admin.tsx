import type { ReactNode } from "react";
import { Navigate } from "react-router-dom";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";

// A page only an instance admin may open, such as probe management. Anyone else goes to the
// workspace's monitors, as for an address that names nothing.
export function RequireInstanceAdmin({ children }: { children: ReactNode }) {
  const { instanceAdmin } = useCurrentAccess();
  const workspacePath = useWorkspacePath();
  if (!instanceAdmin) return <Navigate to={workspacePath("/monitors")} replace />;
  return children;
}
