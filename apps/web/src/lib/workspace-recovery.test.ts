import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { AxiosResponse, InternalAxiosRequestConfig } from "axios";
import { QueryClient } from "@tanstack/react-query";
import { client } from "@/api/client.gen";
import { getMeQueryKey } from "@/api/@tanstack/react-query.gen";
import { setupInterceptors } from "@/interceptors";
import { recoverFromMissingWorkspace, setWorkspaceNotFoundHandler } from "./workspace-recovery";

const CURRENT = "0b9c2f5e-6c1a-4d5e-9a57-3f1d2c4b8a10";
const DEFAULT = "7f3e1a90-2b44-4c6d-8e15-a9d0c5b7e321";

type Me = { workspaces: { id: string; name: string; role: string }[]; defaultWorkspaceId: string | null };

// Answers /me with the given body and counts how often it is asked; answers anything else 404 "Workspace not found"
const serve = (me: Me) => {
  const asked = { me: 0 };
  client.instance.defaults.adapter = async (config: InternalAxiosRequestConfig) => {
    const respond = (status: number, data: unknown) => {
      const response = { data, status, statusText: "", headers: {}, config } as AxiosResponse;
      if (status >= 400) return Promise.reject(Object.assign(new Error("failed"), { isAxiosError: true, config, response }));
      return Promise.resolve(response);
    };
    if (config.url === "/api/v1/me") {
      asked.me++;
      return respond(200, { data: { user: { id: "u1", email: "a@b.c", instanceAdmin: false }, ...me } });
    }
    return respond(404, { message: "Workspace not found", data: null });
  };
  return asked;
};

const at = (pathname: string) =>
  vi.stubGlobal("window", { __CONFIG__: { API_URL: "" }, location: { pathname } });

describe("recoverFromMissingWorkspace", () => {
  beforeEach(() => at(`/w/${CURRENT}/monitors`));
  afterEach(() => {
    vi.unstubAllGlobals();
    client.instance.defaults.adapter = undefined;
  });

  it("refetches /me once for any number of answers at the same time and goes to the default workspace's monitors", async () => {
    const asked = serve({
      workspaces: [{ id: DEFAULT, name: "Main", role: "owner" }],
      defaultWorkspaceId: DEFAULT,
    });
    const queryClient = new QueryClient();
    const navigate = vi.fn();
    const recover = recoverFromMissingWorkspace(queryClient, navigate);

    await Promise.all([recover(), recover(), recover()]);

    expect(asked.me).toBe(1);
    expect(navigate).toHaveBeenCalledTimes(1);
    expect(navigate).toHaveBeenCalledWith(`/w/${DEFAULT}/monitors`, { replace: true });
    // The page's /me now lists what the server says
    expect(queryClient.getQueryData<{ data: Me }>(getMeQueryKey())?.data.defaultWorkspaceId).toBe(DEFAULT);
  });

  it("stays put when the default workspace is the one that answered not found, so it can't loop", async () => {
    at(`/w/${DEFAULT}/monitors`);
    const asked = serve({
      workspaces: [{ id: DEFAULT, name: "Main", role: "owner" }],
      defaultWorkspaceId: DEFAULT,
    });
    const navigate = vi.fn();

    await recoverFromMissingWorkspace(new QueryClient(), navigate)();

    expect(asked.me).toBe(1);
    expect(navigate).not.toHaveBeenCalled();
  });

  it("goes nowhere when /me has no workspace, and leaves the no-workspace page to show", async () => {
    const asked = serve({ workspaces: [], defaultWorkspaceId: null });
    const queryClient = new QueryClient();
    const navigate = vi.fn();

    await recoverFromMissingWorkspace(queryClient, navigate)();

    expect(asked.me).toBe(1);
    expect(navigate).not.toHaveBeenCalled();
    expect(queryClient.getQueryData<{ data: Me }>(getMeQueryKey())?.data.workspaces).toEqual([]);
  });

  it("recovers again for a later answer", async () => {
    const asked = serve({
      workspaces: [{ id: DEFAULT, name: "Main", role: "owner" }],
      defaultWorkspaceId: DEFAULT,
    });
    const recover = recoverFromMissingWorkspace(new QueryClient(), vi.fn());

    await recover();
    await recover();

    expect(asked.me).toBe(2);
  });
});

describe("the response interceptor", () => {
  beforeAll(() => {
    setupInterceptors();
  });
  beforeEach(() => at(`/w/${CURRENT}/monitors`));
  afterEach(() => {
    vi.unstubAllGlobals();
    client.instance.defaults.adapter = undefined;
    setWorkspaceNotFoundHandler(null);
  });

  it("calls the handler once for a 404 that says Workspace not found, and still fails the request", async () => {
    serve({ workspaces: [], defaultWorkspaceId: null });
    const handler = vi.fn();
    setWorkspaceNotFoundHandler(handler);

    await expect(client.get({ url: "/api/v1/monitors", throwOnError: true })).rejects.toBeDefined();

    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("ignores any other 404", async () => {
    client.instance.defaults.adapter = async (config: InternalAxiosRequestConfig) => {
      const response = { data: { message: "Invite not found" }, status: 404, statusText: "", headers: {}, config } as AxiosResponse;
      return Promise.reject(Object.assign(new Error("failed"), { isAxiosError: true, config, response }));
    };
    const handler = vi.fn();
    setWorkspaceNotFoundHandler(handler);

    await expect(client.get({ url: "/api/v1/invites/x", throwOnError: true })).rejects.toBeDefined();

    expect(handler).not.toHaveBeenCalled();
  });
});
