import { AppProviders } from "@/components/app-providers";
import { AppRouter } from "@/components/app-router";
import { useAppInitialization } from "@/hooks/useAppInitialization";

export default function App() {
  useAppInitialization();

  return (
    <AppProviders>
      <AppRouter />
    </AppProviders>
  );
}
