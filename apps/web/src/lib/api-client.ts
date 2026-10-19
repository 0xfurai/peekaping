import { client } from "@/api/client.gen";
import { useAuthStore } from "@/store/auth";
import { getConfig } from "@/lib/config";
import { setupInterceptors } from "@/interceptors";

const configureClient = (accessToken: string | null) => {
  client.setConfig({
    // The generated paths already start with /api/v1.
    baseURL: getConfig().API_URL,
    headers: accessToken
      ? {
          Authorization: `Bearer ${accessToken}`,
        }
      : undefined,
  });
};

// Call once, before the first render: queries start in the children's effects,
// which run before App's, and would go out with the generated client's defaults.
export const setupApiClient = () => {
  configureClient(useAuthStore.getState().accessToken);
  setupInterceptors();

  // Subscribe to auth state changes
  useAuthStore.subscribe((state) => configureClient(state.accessToken));
};
