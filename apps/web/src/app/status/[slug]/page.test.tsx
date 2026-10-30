import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AxiosError, AxiosHeaders } from "axios";
import {
  getStatusPagesSlugBySlugMonitorsQueryKey,
  getStatusPagesSlugBySlugQueryKey,
} from "@/api/@tanstack/react-query.gen";
import type {
  StatusPageMonitorWithHeartbeatsAndUptimeDto,
  StatusPagePublicHeartbeatDto,
} from "@/api/types.gen";
import { text } from "@/test-support/render-with-me";
import { createCustomDomainUnavailableRoute } from "@/routes/public-routes";
import PublicStatusPage from "./page";

vi.mock("@/lib/config", () => ({ getConfig: () => ({ API_URL: "" }) }));
// The bars read the timezone, and its provider needs a browser's localStorage
vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

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

const PAGE = {
  auto_refresh_interval: 30,
  created_at: "2026-10-01T00:00:00Z",
  description: "",
  footer_text: "",
  icon: "",
  id: "page-1",
  published: true,
  show_locations: true,
  slug: "acme",
  theme: "auto",
  title: "Acme status",
  updated_at: "2026-10-01T00:00:00Z",
};

const UP = 1;
const DOWN = 0;
const MAINTENANCE = 3;

const beat = (status: number): StatusPagePublicHeartbeatDto => ({
  id: `beat-${status}`,
  status,
  ping: 10,
  time: "2026-10-25T00:00:00Z",
  end_time: "2026-10-25T00:00:00Z",
});

const monitor = (
  name: string,
  status: number,
  extra: Partial<StatusPageMonitorWithHeartbeatsAndUptimeDto> = {}
): StatusPageMonitorWithHeartbeatsAndUptimeDto => ({
  active: true,
  heartbeats: [beat(UP), beat(status)],
  id: `id-${name}`,
  name,
  status,
  type: "http",
  uptime_24h: 99.5,
  ...extra,
});

// The opening tag and the text of every element with the test id in the markup
const elementsWith = (html: string, testId: string) =>
  [...html.matchAll(new RegExp(`<[^>]*data-testid="${testId}"[^>]*>([^<]*)`, "g"))].map((match) => ({
    tag: match[0],
    text: match[1],
  }));

// The public page of /status/acme once both requests answered
const pageWith = (monitors: StatusPageMonitorWithHeartbeatsAndUptimeDto[]) => {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const path = { slug: "acme" };
  queryClient.setQueryData(getStatusPagesSlugBySlugQueryKey({ path }), {
    data: PAGE,
    message: "",
  });
  queryClient.setQueryData(getStatusPagesSlugBySlugMonitorsQueryKey({ path }), {
    data: monitors,
    message: "",
  });
  const html = renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/status/acme"]}>
        <Routes>
          <Route path="/status/:slug" element={<PublicStatusPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
  return {
    overall: elementsWith(html, "status-page-overall-status")[0]?.text,
    cards: html
      .split('data-testid="status-page-monitor"')
      .slice(1)
      .map((card) => ({
        status: elementsWith(card, "status-page-monitor-status")[0]?.text,
        locations: elementsWith(card, "status-page-location-status").map((el) => el.text),
        history: elementsWith(card, "status-page-history").map((el) =>
          el.tag.includes("opacity-50")
        ),
      })),
  };
};

const LOCATIONS = [
  { name: "Local", status: DOWN, uptime_24h: 90, heartbeats: [beat(DOWN)] },
  { name: "Paris", status: UP, uptime_24h: 100, heartbeats: [beat(UP)] },
];

describe("PublicStatusPage, a monitor that isn't checked", () => {
  const NOT_MONITORED = text("status.messages.not_monitored");

  it("says Not monitored for a paused monitor instead of its last status", () => {
    const page = pageWith([monitor("api", DOWN, { active: false })]);
    expect(page.cards[0].status).toBe(NOT_MONITORED);
  });

  // The server sends active: false for a monitor of a turned-off type as well
  it("says Not monitored for a monitor of a turned-off type, whatever its last status", () => {
    const page = pageWith([
      monitor("docker", UP, { active: false, type: "docker" }),
      monitor("other", MAINTENANCE, { active: false, type: "docker" }),
    ]);
    expect(page.cards.map((card) => card.status)).toEqual([NOT_MONITORED, NOT_MONITORED]);
  });

  it("says Not monitored for each location too", () => {
    const page = pageWith([monitor("api", DOWN, { active: false, locations: LOCATIONS })]);
    expect(page.cards[0].status).toBe(NOT_MONITORED);
    expect(page.cards[0].locations).toEqual([NOT_MONITORED, NOT_MONITORED]);
  });

  it("keeps the locations' own statuses for a monitor that is checked", () => {
    const page = pageWith([monitor("api", UP, { locations: LOCATIONS })]);
    expect(page.cards[0].locations).toEqual([text("status.messages.down"), text("status.messages.operational")]);
  });

  it("still draws the history, dimmed, and leaves a checked monitor's history as it was", () => {
    const page = pageWith([
      monitor("paused", DOWN, { active: false }),
      monitor("live", UP),
      monitor("paused-locations", UP, { active: false, locations: LOCATIONS }),
    ]);
    expect(page.cards.map((card) => card.history)).toEqual([[true], [false], [true, true]]);
  });

  it("doesn't let a stale Down change the page's overall state", () => {
    const page = pageWith([monitor("live", UP), monitor("paused", DOWN, { active: false })]);
    expect(page.overall).toBe(text("status.messages.all_systems_operational"));
  });

  it("doesn't let a stale Maintenance change the page's overall state", () => {
    const page = pageWith([monitor("live", UP), monitor("paused", MAINTENANCE, { active: false })]);
    expect(page.overall).toBe(text("status.messages.all_systems_operational"));
  });

  it("still lets a monitor that is checked and Down make the page a partial outage", () => {
    const page = pageWith([monitor("live", DOWN), monitor("paused", UP, { active: false })]);
    expect(page.overall).toBe(text("status.messages.partial_system_outage"));
  });

  it("shows the overall state of a page with no monitors when none is checked", () => {
    const page = pageWith([
      monitor("a", DOWN, { active: false }),
      monitor("b", MAINTENANCE, { active: false }),
    ]);
    expect(page.overall).toBe(text("status.messages.all_systems_operational"));
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
