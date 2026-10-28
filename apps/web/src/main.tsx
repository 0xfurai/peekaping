import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./App.tsx";
import { BrowserRouter } from "react-router-dom";
import { Toaster } from "@/components/ui/sonner";
import "./i18n";
import "./i18n/types";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setupApiClient } from "@/lib/api-client";
import { useAuthStore } from "@/store/auth";
import { dropCachedData } from "@/lib/workspace-cache";

setupApiClient();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      staleTime: 1000 * 60 * 5, // 5 minutes
    },
    mutations: {
      retry: false,
    },
  },
});

// A login or a logout empties the cache, /me included: the next user's workspaces
// and data must not come from the last one's. /me is fetched again when the
// workspace layout mounts after a login.
let signedIn = Boolean(useAuthStore.getState().accessToken);
useAuthStore.subscribe((state) => {
  const nowSignedIn = Boolean(state.accessToken);
  if (nowSignedIn !== signedIn) {
    signedIn = nowSignedIn;
    dropCachedData(queryClient, { keepMe: false });
  }
});

createRoot(document.getElementById("root")!).render(
  <QueryClientProvider client={queryClient}>
    <BrowserRouter>
      <App />
      <Toaster richColors />
    </BrowserRouter>
  </QueryClientProvider>
);
