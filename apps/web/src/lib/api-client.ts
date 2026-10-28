import { client } from "@/api/client.gen";
import { useAuthStore } from "@/store/auth";
import { getConfig } from "@/lib/config";
import { setupInterceptors } from "@/interceptors";

export const configureClient = (accessToken: string | null) => {
  client.setConfig({
    // The generated paths already start with /api/v1.
    baseURL: getConfig().API_URL,
    // setConfig merges headers and skips undefined, so a logout has to ask for
    // the removal: null deletes the header from the client's own config.
    headers: { Authorization: accessToken ? `Bearer ${accessToken}` : null },
  });

  if (!accessToken) {
    // setConfig also copies the headers into the axios instance's defaults,
    // merging them into what is there, so the old token stays until we drop it.
    delete client.instance.defaults.headers.Authorization;
  }
};

// Call once, before the first render: queries start in the children's effects,
// which run before App's, and would go out with the generated client's defaults.
export const setupApiClient = () => {
  configureClient(useAuthStore.getState().accessToken);
  setupInterceptors();

  // Subscribe to auth state changes
  useAuthStore.subscribe((state) => configureClient(state.accessToken));
};
