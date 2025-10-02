import { useEffect } from "react";
import dayjs from "dayjs";
import timezone from "dayjs/plugin/timezone";
import utc from "dayjs/plugin/utc";
import relativeTime from "dayjs/plugin/relativeTime";
import duration from "dayjs/plugin/duration";
import { client } from "@/api/client.gen";
import { useAuthStore } from "@/store/auth";
import { getConfig } from "@/lib/config";
import { setupInterceptors } from "@/interceptors";

export const configureClient = () => {
  const accessToken = useAuthStore.getState().accessToken;
  const selectedWorkspaceId = useAuthStore.getState().selectedWorkspaceId;

  const headers: Record<string, string> = {};
  if (accessToken) {
    headers.Authorization = `Bearer ${accessToken}`;
  }
  if (selectedWorkspaceId) {
    headers['x-peekaping-workspace'] = selectedWorkspaceId;
  }

  client.setConfig({
    baseURL: getConfig().API_URL + "/api/v1",
    headers: Object.keys(headers).length > 0 ? headers : undefined,
  });
};

export const useAppInitialization = () => {
  useEffect(() => {
    // Initialize dayjs plugins
    dayjs.extend(utc);
    dayjs.extend(timezone);
    dayjs.extend(relativeTime);
    dayjs.extend(duration);

    configureClient();
    setupInterceptors();

    // Subscribe to auth state changes
    useAuthStore.subscribe((state) => {
      const headers: Record<string, string> = {};
      if (state.accessToken) {
        headers.Authorization = `Bearer ${state.accessToken}`;
      }
      if (state.selectedWorkspaceId) {
        headers['x-peekaping-workspace'] = state.selectedWorkspaceId;
      }

      client.setConfig({
        baseURL: getConfig().API_URL + "/api/v1",
        headers: Object.keys(headers).length > 0 ? headers : undefined,
      });
    });
  }, []);
};
