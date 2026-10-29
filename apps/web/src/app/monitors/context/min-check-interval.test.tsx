import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { getWorkspaceLimitsQueryKey } from "@/api/@tanstack/react-query.gen";
import "@/i18n";
import i18n from "i18next";
import { getMonitorsByIdQueryKey } from "@/api/@tanstack/react-query.gen";
import { useMinCheckInterval } from "@/hooks/useMinCheckInterval";
import { renderAs } from "@/test-support/render-with-me";
import { deserializeMonitor, monitorTypes, type MonitorFormValues } from "../monitor-types/registry";
import { INTERVAL_TOO_SHORT } from "../monitor-types/shared/schema";
import { MonitorFormProvider, useMonitorFormContext } from "./monitor-form-context";

// The intervals a new monitor's form starts with
const Starts = () => {
  const { form } = useMonitorFormContext();
  const { interval, retry_interval, timeout } = form.getValues();
  return <output>{JSON.stringify({ interval, retry_interval, timeout })}</output>;
};

const startedWith = (html: string) =>
  JSON.parse(html.match(/<output>(.*)<\/output>/)![1].replace(/&quot;/g, '"'));

// GET /workspace/limits as loaded, with the shortest interval that applies to the workspace
const seedLimits = (minimum: number) => (queryClient: QueryClient) => {
  const use = (max: number | null, used = 0) => ({ max, used, source: max === null ? "none" : "install" });
  queryClient.setQueryData(getWorkspaceLimitsQueryKey(), {
    message: "",
    data: {
      source: "install",
      limits: {
        monitors: use(null),
        min_check_interval: use(minimum, minimum),
        status_pages: use(null),
        custom_domains: use(null),
        members: use(null),
        owned_workspaces: use(null),
        probes_per_monitor: use(null),
        retention_days: use(null),
      },
    },
  });
};

const newMonitor = (minCheckInterval?: number, workspaceMinimum?: number) =>
  startedWith(
    renderAs(
      { instanceAdmin: false, role: "member", minCheckInterval },
      <MonitorFormProvider mode="create">
        <Starts />
      </MonitorFormProvider>,
      undefined,
      workspaceMinimum === undefined ? undefined : seedLimits(workspaceMinimum)
    )
  );

describe("A new monitor's form", () => {
  it("starts its interval and retry interval at the install's minimum", () => {
    expect(newMonitor(60)).toEqual({ interval: 60, retry_interval: 60, timeout: 16 });
  });

  it("starts at 20 when the install's minimum is 20", () => {
    expect(newMonitor(20)).toEqual({ interval: 20, retry_interval: 20, timeout: 16 });
  });

  it("starts at the workspace's minimum, from its limits, when that is stricter than the install's", () => {
    expect(newMonitor(60, 300)).toEqual({ interval: 300, retry_interval: 300, timeout: 16 });
  });
});

describe("A clone's form", () => {
  const source = monitorTypes[0].defaultValues as MonitorFormValues;
  const clone = (interval: number, retry_interval: number) =>
    startedWith(
      renderAs(
        { instanceAdmin: false, role: "member", minCheckInterval: 60 },
        <MonitorFormProvider mode="create" initialValues={{ ...source, interval, retry_interval, timeout: 16 }}>
          <Starts />
        </MonitorFormProvider>
      )
    );

  it("raises the intervals of a monitor stored below the install's minimum to it", () => {
    expect(clone(30, 40)).toEqual({ interval: 60, retry_interval: 60, timeout: 16 });
  });

  it("keeps intervals above the minimum", () => {
    expect(clone(120, 90)).toEqual({ interval: 120, retry_interval: 90, timeout: 16 });
  });
});

describe("An edit's form", () => {
  const saved = {
    id: "m1",
    name: "Site",
    type: "http",
    interval: 30,
    timeout: 16,
    max_retries: 3,
    retry_interval: 40,
    resend_interval: 10,
    config: JSON.stringify({ url: "https://example.com", method: "GET", encoding: "json", accepted_statuscodes: ["2XX"] }),
    notification_ids: [],
  };

  it("keeps the intervals the monitor is stored with, below the minimum", () => {
    const html = renderAs(
      { instanceAdmin: false, role: "member", minCheckInterval: 60 },
      <MonitorFormProvider mode="edit" monitorId="m1" initialValues={deserializeMonitor(saved as never)}>
        <Starts />
      </MonitorFormProvider>,
      undefined,
      (queryClient) => queryClient.setQueryData(getMonitorsByIdQueryKey({ path: { id: "m1" } }), { data: saved })
    );
    expect(startedWith(html)).toEqual({ interval: 30, retry_interval: 40, timeout: 16 });
  });
});

describe("useMinCheckInterval", () => {
  const Minimum = () => <output>{useMinCheckInterval()}</output>;

  it("is the shortest interval that applies to the workspace, from its limits", () => {
    const html = renderAs({ instanceAdmin: false, role: "member", minCheckInterval: 60 }, <Minimum />, undefined, seedLimits(120));
    expect(html).toContain("<output>120</output>");
  });

  it("is what /me says while the limits aren't loaded", () => {
    expect(renderAs({ instanceAdmin: false, role: "member", minCheckInterval: 90 }, <Minimum />)).toContain("<output>90</output>");
  });

  it("is 20 while /me isn't loaded", () => {
    const html = renderToStaticMarkup(
      <QueryClientProvider client={new QueryClient()}>
        <Minimum />
      </QueryClientProvider>
    );
    expect(html).toContain("<output>20</output>");
  });
});

describe("The message for an interval below the minimum", () => {
  it("names the minimum", () => {
    expect(i18n.t(INTERVAL_TOO_SHORT, { minimum: 60 })).toContain("60");
  });
});
