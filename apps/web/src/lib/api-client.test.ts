import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AxiosResponse, InternalAxiosRequestConfig } from "axios";
import { client } from "@/api/client.gen";
import { configureClient } from "./api-client";

// Sends one request through the generated client and returns the
// Authorization header axios would put on the wire (undefined when absent).
const authorizationOfNextRequest = async () => {
  let authorization: unknown;
  await client.get({
    url: "/api/v1/version",
    adapter: async (config: InternalAxiosRequestConfig) => {
      authorization = config.headers.get("Authorization");
      return {
        data: {},
        status: 200,
        statusText: "OK",
        headers: {},
        config,
      } as AxiosResponse;
    },
  });
  return authorization ?? undefined;
};

describe("configureClient", () => {
  beforeEach(() => {
    vi.stubGlobal("window", { __CONFIG__: { API_URL: "" } });
  });

  afterEach(() => {
    configureClient(null);
    vi.unstubAllGlobals();
  });

  it("sends the access token as a Bearer Authorization header", async () => {
    configureClient("token-1");

    expect(await authorizationOfNextRequest()).toBe("Bearer token-1");
  });

  it("sends no Authorization header after the access token is cleared (logout)", async () => {
    configureClient("token-1");
    configureClient(null);

    expect(await authorizationOfNextRequest()).toBeUndefined();
    expect(client.getConfig().headers).not.toHaveProperty("Authorization");
    expect(client.instance.defaults.headers).not.toHaveProperty("Authorization");
  });

  it("sends the new access token when logging in again after a logout", async () => {
    configureClient("token-1");
    configureClient(null);
    configureClient("token-2");

    expect(await authorizationOfNextRequest()).toBe("Bearer token-2");
  });
});
