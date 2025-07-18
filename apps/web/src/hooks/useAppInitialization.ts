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

  client.setConfig({
    baseURL: getConfig().API_URL + "/api/v1",
    headers: accessToken
      ? {
          Authorization: `Bearer ${accessToken}`,
        }
      : undefined,
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
      client.setConfig({
        baseURL: getConfig().API_URL + "/api/v1",
        headers: state.accessToken
          ? {
              Authorization: `Bearer ${state.accessToken}`,
            }
          : undefined,
      });
    });
  }, []);
};
