import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { AxiosResponse, InternalAxiosRequestConfig } from "axios";
import { client } from "@/api/client.gen";
import { setupInterceptors } from "./interceptors";

const WORKSPACE = "0b9c2f5e-6c1a-4d5e-9a57-3f1d2c4b8a10";
const OTHER = "7f3e1a90-2b44-4c6d-8e15-a9d0c5b7e321";

// Sends one request from the given address and returns the X-Workspace-ID
// header axios would put on the wire (undefined when absent).
const workspaceHeaderFrom = async (pathname: string) => {
  vi.stubGlobal("window", { __CONFIG__: { API_URL: "" }, location: { pathname } });
  let header: unknown;
  await client.get({
    url: "/api/v1/monitors",
    adapter: async (config: InternalAxiosRequestConfig) => {
      header = config.headers.get("X-Workspace-ID");
      return {
        data: {},
        status: 200,
        statusText: "OK",
        headers: {},
        config,
      } as AxiosResponse;
    },
  });
  return header ?? undefined;
};

describe("workspace header", () => {
  beforeAll(() => {
    setupInterceptors();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("sends the workspace of the address", async () => {
    expect(await workspaceHeaderFrom(`/w/${WORKSPACE}/monitors`)).toBe(WORKSPACE);
  });

  it("follows the address from one request to the next", async () => {
    expect(await workspaceHeaderFrom(`/w/${WORKSPACE}/monitors`)).toBe(WORKSPACE);
    expect(await workspaceHeaderFrom(`/w/${OTHER}/monitors`)).toBe(OTHER);
    expect(await workspaceHeaderFrom("/login")).toBeUndefined();
  });

  it("sends none from an address without a workspace", async () => {
    expect(await workspaceHeaderFrom("/monitors")).toBeUndefined();
    expect(await workspaceHeaderFrom("/")).toBeUndefined();
  });
});
