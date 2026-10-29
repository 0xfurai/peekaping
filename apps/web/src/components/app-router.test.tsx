import { afterEach, describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AxiosError, AxiosHeaders } from "axios";
import { getStatusPagesDomainByDomainQueryKey } from "@/api/@tanstack/react-query.gen";
import { text } from "@/test-support/render-with-me";
import { AppRouter } from "./app-router";

vi.mock("@/lib/config", () => ({ getConfig: () => ({ API_URL: "" }) }));
// Static markup reads a zustand store's initial state, so the signed-in user's token comes from here
const auth = vi.hoisted(() => ({ accessToken: null as string | null }));
vi.mock("@/store/auth", () => ({
  useAuthStore: (select: (state: typeof auth) => unknown) => select(auth),
}));
// The pages behind each route set are stand-ins: the test is about which set mounts
vi.mock("@/app/status/[slug]/page", () => ({
  default: ({ incomingSlug }: { incomingSlug?: string }) => <p>status page {incomingSlug}</p>,
}));
vi.mock("@/routes/auth-routes", () => ({
  authRoutes: [<Route key="auth" path="*" element={<p>auth routes</p>} />],
}));
vi.mock("@/routes/protected-routes", () => ({
  protectedRoutes: [<Route key="app" path="*" element={<p>protected routes</p>} />],
}));

const DOMAIN = "status.acme.example";

const answer = (status: number, data: unknown) => {
  const config = { headers: new AxiosHeaders() };
  return new AxiosError("Request failed", "ERR_BAD_RESPONSE", config, null, {
    status,
    statusText: "",
    headers: {},
    config,
    data,
  });
};

const UNAVAILABLE = answer(503, {
  message: "This status page is unavailable",
  data: null,
  error: { code: "STATUS_PAGE_UNAVAILABLE" },
});
const NOT_FOUND = answer(404, { message: "Status page not found" });
const ACME = { data: { slug: "acme" } };

// The app at / on DOMAIN once its custom-domain lookup has settled: with the data of a lookup that
// found a status page, the error of one that failed, or both when a refetch failed after a success
// (TanStack Query keeps the data then). Static markup runs no effects, so nothing is fetched.
const appAfter = ({ data, error }: { data?: unknown; error?: AxiosError }) => {
  vi.stubGlobal("window", { location: { hostname: DOMAIN } });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, retryOnMount: false } },
  });
  const queryKey = getStatusPagesDomainByDomainQueryKey({ path: { domain: DOMAIN } });
  queryClient.getQueryCache().build(queryClient, { queryKey }).setState({
    status: error ? "error" : "success",
    data,
    dataUpdatedAt: data ? Date.now() : 0,
    dataUpdateCount: data ? 1 : 0,
    error: error ?? null,
    errorUpdatedAt: error ? Date.now() : 0,
    errorUpdateCount: error ? 1 : 0,
    fetchStatus: "idle",
  });
  return renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/"]}>
        <AppRouter />
      </MemoryRouter>
    </QueryClientProvider>
  );
};

afterEach(() => {
  vi.unstubAllGlobals();
  auth.accessToken = null;
});

describe("AppRouter", () => {
  it("shows a custom domain's status page", () => {
    const html = appAfter({ data: ACME });
    expect(html).toContain("status page acme");
    expect(html).not.toContain(text("status.messages.status_page_unavailable"));
    expect(html).not.toContain("auth routes");
  });

  it("shows a suspended workspace's custom domain as unavailable", () => {
    const html = appAfter({ error: UNAVAILABLE });
    expect(html).toContain(text("status.messages.status_page_unavailable"));
    expect(html).not.toContain("auth routes");
  });

  it("shows the sign-in routes on a host that is no status page's domain", () => {
    const html = appAfter({ error: NOT_FOUND });
    expect(html).toContain("auth routes");
    expect(html).not.toContain(text("status.messages.status_page_unavailable"));
  });

  it("shows the app to a signed-in user, whatever the domain", () => {
    auth.accessToken = "token";
    expect(appAfter({ data: ACME })).toContain("protected routes");
    expect(appAfter({ error: UNAVAILABLE })).toContain("protected routes");
  });
});
