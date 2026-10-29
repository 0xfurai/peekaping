import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AxiosError, AxiosHeaders } from "axios";
import { getStatusPagesSlugBySlugQueryKey } from "@/api/@tanstack/react-query.gen";
import { text } from "@/test-support/render-with-me";
import { createCustomDomainUnavailableRoute } from "@/routes/public-routes";
import PublicStatusPage from "./page";

vi.mock("@/lib/config", () => ({ getConfig: () => ({ API_URL: "" }) }));

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

// The public page of /status/acme after the server answered its request with the error. Static
// markup runs no effects, and retryOnMount off keeps the failed request from looking pending again.
const pageAfter = (error: AxiosError) => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, retryOnMount: false } },
  });
  const queryKey = getStatusPagesSlugBySlugQueryKey({ path: { slug: "acme" } });
  queryClient.getQueryCache().build(queryClient, { queryKey }).setState({
    status: "error",
    error,
    fetchStatus: "idle",
    errorUpdateCount: 1,
    errorUpdatedAt: Date.now(),
  });
  return renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/status/acme"]}>
        <Routes>
          <Route path="/status/:slug" element={<PublicStatusPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
};

describe("PublicStatusPage", () => {
  it("says the page is unavailable, neither an error nor not found, for a suspended workspace's page", () => {
    const html = pageAfter(UNAVAILABLE);
    expect(html).toContain(text("status.messages.status_page_unavailable"));
    expect(html).not.toContain(text("status.messages.status_page_not_found"));
    expect(html).not.toContain('role="alert"');
  });

  it("still says not found for a page that isn't there", () => {
    const html = pageAfter(NOT_FOUND);
    expect(html).toContain(text("status.messages.status_page_not_found"));
    expect(html).not.toContain(text("status.messages.status_page_unavailable"));
  });
});

describe("createCustomDomainUnavailableRoute", () => {
  // A custom domain whose lookup answered unavailable shows the same page at every address
  it("shows the unavailable page on a custom domain", () => {
    const html = renderToStaticMarkup(
      <MemoryRouter initialEntries={["/"]}>
        <Routes>{createCustomDomainUnavailableRoute()}</Routes>
      </MemoryRouter>
    );
    expect(html).toContain(text("status.messages.status_page_unavailable"));
  });
});
