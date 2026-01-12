import { useAuthStore } from "@/store/auth";
import { useOrganizationStore } from "@/store/organization";
import type {
    AxiosError,
    AxiosResponse,
    InternalAxiosRequestConfig,
} from "axios";
import { client } from "./api/client.gen";
import axios from "axios";
import { getConfig } from "./lib/config";

interface CustomAxiosRequestConfig extends InternalAxiosRequestConfig {
    _retry?: boolean;
}

let isRefreshing = false;
let failedQueue: Array<{
    resolve: (value?: unknown) => void;
    reject: (reason?: unknown) => void;
}> = [];

const processQueue = (error: unknown = null, token: string | null = null) => {
    failedQueue.forEach((prom) => {
        if (error) {
            prom.reject(error);
        } else {
            prom.resolve(token);
        }
    });
    failedQueue = [];
};

export const setupInterceptors = () => {
    client.instance.interceptors.response.use(
        (response: AxiosResponse) => response,
        async (error: AxiosError) => {
            const originalRequest = error.config as
                | CustomAxiosRequestConfig
                | undefined;

            const accessToken = useAuthStore.getState().accessToken

            if (error.response?.status === 401 && !originalRequest?._retry && accessToken) {
                if (isRefreshing) {
                    return new Promise((resolve, reject) => {
                        failedQueue.push({ resolve, reject });
                    })
                        .then((token) => {
                            if (originalRequest?.headers) {
                                originalRequest.headers["Authorization"] = `Bearer ${token}`;
                            }

                            return client.instance.request(
                                originalRequest as InternalAxiosRequestConfig
                            );
                        })
                        .catch((err) => {
                            return Promise.reject(err);
                        });
                }

                if (originalRequest) {
                    originalRequest._retry = true;
                }
                isRefreshing = true;

                const refreshToken = useAuthStore.getState().refreshToken;

                if (!refreshToken) {
                    useAuthStore.getState().clearTokens();
                    return Promise.reject(error);
                }

                try {
                    const { data } = await axios.post(
                        `${getConfig().API_URL}/api/v1/auth/refresh`,
                        { refreshToken },
                        {
                            headers: {
                                "Content-Type": "application/json",
                            },
                        }
                    );

                    if (data?.data?.accessToken && data?.data?.refreshToken) {
                        useAuthStore
                            .getState()
                            .setTokens(data.data.accessToken, data.data.refreshToken);

                        client.setConfig({
                            headers: {
                                Authorization: `Bearer ${data.data.accessToken}`,
                            },
                        });

                        processQueue(null, data.data.accessToken);
                        if (originalRequest?.headers) {
                            originalRequest.headers[
                                "Authorization"
                            ] = `Bearer ${data.data.accessToken}`;
                        }
                        return client.instance.request(
                            originalRequest as InternalAxiosRequestConfig
                        );
                    } else {
                        throw new Error("No tokens received from refresh");
                    }
                } catch (refreshError) {
                    processQueue(refreshError, null);
                    useAuthStore.getState().clearTokens();
                    return Promise.reject(refreshError);
                } finally {
                    isRefreshing = false;
                }
            }

            return Promise.reject(error);
        }
    );

    client.instance.interceptors.request.use((config) => {
        const accessToken = useAuthStore.getState().accessToken;
        if (accessToken) {
            config.headers.Authorization = `Bearer ${accessToken}`;
        }

        const orgId = useOrganizationStore.getState().currentOrganization?.id;
        if (orgId) {
            config.headers["X-Organization-ID"] = orgId;
        }

        return config;
    });
};
