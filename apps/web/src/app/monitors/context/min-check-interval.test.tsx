import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { UseFormReturn } from "react-hook-form";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { getWorkspaceLimitsQueryKey } from "@/api/@tanstack/react-query.gen";
import "@/i18n";
import i18n from "i18next";
import { getMonitorsByIdQueryKey } from "@/api/@tanstack/react-query.gen";
import { useMinCheckInterval } from "@/hooks/useMinCheckInterval";
import { renderAs } from "@/test-support/render-with-me";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { deserializeMonitor, monitorTypes, serializeMonitor, type MonitorFormValues } from "../monitor-types/registry";
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
  const stored = (interval: number, retry_interval: number) => ({
    id: "m1",
    name: "Site",
    type: "http",
    interval,
    timeout: 16,
    max_retries: 3,
    retry_interval,
    resend_interval: 10,
    config: JSON.stringify({ url: "https://example.com", method: "GET", encoding: "json", accepted_statuscodes: ["2XX"] }),
    notification_ids: [],
    probe_ids: [LOCAL_PROBE_ID],
  });

  // The edit's form as it starts, kept to fill in and submit after the render
  const edit = (saved: ReturnType<typeof stored>) => {
    let form: UseFormReturn<MonitorFormValues> | undefined;
    const Keep = () => {
      form = useMonitorFormContext().form;
      return <Starts />;
    };
    const html = renderAs(
      { instanceAdmin: false, role: "member", minCheckInterval: 60 },
      <MonitorFormProvider mode="edit" monitorId="m1" initialValues={deserializeMonitor(saved as never)}>
        <Keep />
      </MonitorFormProvider>,
      undefined,
      (queryClient) => queryClient.setQueryData(getMonitorsByIdQueryKey({ path: { id: "m1" } }), { data: saved })
    );
    return { started: startedWith(html), form: form! };
  };

  // A monitor stored below a minimum raised since already runs at the minimum, and the API refuses
  // a save below it, even of the stored values
  it("raises the intervals of a monitor stored below the install's minimum to it", () => {
    expect(edit(stored(20, 20)).started).toEqual({ interval: 60, retry_interval: 60, timeout: 16 });
  });

  it("saves a rename of a monitor stored below the minimum at the minimum", async () => {
    const { form } = edit(stored(20, 20));
    form.setValue("name", "Renamed", { shouldDirty: true });
    const submitted = vi.fn();
    const refused = vi.fn();
    await form.handleSubmit(submitted, refused)();
    expect(refused).not.toHaveBeenCalled();
    expect(submitted).toHaveBeenCalledOnce();
    const values = submitted.mock.calls[0][0] as MonitorFormValues;
    expect(values).toMatchObject({ name: "Renamed", interval: 60, retry_interval: 60 });
    expect(serializeMonitor(values)).toMatchObject({ interval: 60, retry_interval: 60 });
  });

  it("keeps intervals above the minimum", () => {
    expect(edit(stored(120, 90)).started).toEqual({ interval: 120, retry_interval: 90, timeout: 16 });
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
