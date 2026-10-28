import { AppProviders } from "@/components/app-providers";
import { AppRouter } from "@/components/app-router";
import { WorkspaceRecovery } from "@/components/workspace-recovery";
import { useAppInitialization } from "@/hooks/useAppInitialization";

export default function App() {
  useAppInitialization();

  return (
    <AppProviders>
      <WorkspaceRecovery />
      <AppRouter />
    </AppProviders>
  );
}
