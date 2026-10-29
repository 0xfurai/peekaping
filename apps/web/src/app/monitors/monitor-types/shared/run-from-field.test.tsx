import { describe, expect, it } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { getMonitorsByIdQueryKey, getProbesQueryKey } from "@/api/@tanstack/react-query.gen";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { renderAs, text, type Access } from "@/test-support/render-with-me";
import CreateEditForm from "../../components/create-edit-form";
import { MonitorFormProvider } from "../../context/monitor-form-context";
import { deserializeMonitor, monitorTypes, serializeMonitor, type MonitorFormValues } from "../registry";
import { RunFromList } from "./run-from-field";
import { runFromChoices, runFromDefaultIds } from "./run-from";

const probe = (id: string, over: Partial<ProbeProbeResponse> = {}): ProbeProbeResponse => ({
  id,
  name: id,
  location: "",
  status: "online",
  supported_types: [],
  disabled: false,
  instances: [{ id: "i1", hostname: "secret-host.internal" }] as ProbeProbeResponse["instances"],
  assign_all_pending: false,
  is_default: false,
  notification_ids: [],
  token_prefix: "",
  created_at: "2026-10-23T00:00:00Z",
  updated_at: "2026-10-23T00:00:00Z",
  ...over,
});

const local = probe("local", { name: "Local", location: "Frankfurt", is_default: true });
const eu = probe("eu", { name: "Berlin", supported_types: ["http", "tcp"] });
const us = probe("us", { name: "Virginia", location: "US East", supported_types: ["http"], is_default: true });

const member: Access = { instanceAdmin: false, role: "member" };
const admin: Access = { instanceAdmin: true, role: "owner" };

const defaults = monitorTypes[0].defaultValues as MonitorFormValues;

const create = (access: Access, probes: ProbeProbeResponse[], initial?: Partial<MonitorFormValues>) =>
  renderAs(
    access,
    <MonitorFormProvider mode="create" initialValues={initial ? ({ ...defaults, ...initial } as MonitorFormValues) : undefined}>
      <CreateEditForm />
    </MonitorFormProvider>,
    undefined,
    (queryClient: QueryClient) => queryClient.setQueryData(getProbesQueryKey(), { data: probes })
  );

// The run-from list as the page renders it: each checkbox's label and whether it is ticked
const RUN_FROM = 'data-testid="monitor-run-from"';
const boxes = (html: string): [string, boolean][] => {
  const list = html.split(RUN_FROM)[1]?.split("</div>")[0] ?? "";
  return [...list.matchAll(/<label[^>]*>.*?aria-checked="(true|false)".*?<\/button>(?:<input[^>]*\/>)?([^<]*)<\/label>/g)].map(
    (m) => [m[2], m[1] === "true"]
  );
};

describe("Run from, for someone who isn't an instance admin", () => {
  it("is a plain checkbox list of probe names and locations", () => {
    const html = create(member, [local, eu, us], { probe_ids: ["local"] });
    expect(html).toContain(text("monitors.form.shared.run_from.label"));
    expect(boxes(html)).toEqual([
      ["Local · Frankfurt", true],
      ["Berlin", false],
      ["Virginia · US East", false],
    ]);
  });

  it("shows no status, instance, link or advanced option of the probe", () => {
    const html = create(member, [local, eu], { probe_ids: ["local"] });
    expect(html).not.toContain("secret-host.internal");
    expect(html).not.toContain("online");
    expect(html).not.toContain("/probes");
    expect(html).not.toContain("monitor-probe-select");
    expect(html).not.toContain(text("monitors.form.shared.location_rule.any"));
  });

  describe("a saved monitor", () => {
    const monitor = {
      id: "m1",
      name: "Site",
      type: "http",
      interval: 60,
      timeout: 16,
      max_retries: 3,
      retry_interval: 60,
      resend_interval: 10,
      config: JSON.stringify({ url: "https://example.com", method: "GET", encoding: "json", accepted_statuscodes: ["2XX"] }),
      probe_ids: ["eu", "us"],
      location_rule: "any",
    };
    const edit = (initial: MonitorFormValues) =>
      renderAs(
        member,
        <MonitorFormProvider mode="edit" monitorId="m1" initialValues={initial}>
          <CreateEditForm />
        </MonitorFormProvider>,
        undefined,
        (queryClient) => {
          queryClient.setQueryData(getProbesQueryKey(), { data: [local, eu, us] });
          queryClient.setQueryData(getMonitorsByIdQueryKey({ path: { id: "m1" } }), { data: monitor });
        }
      );

    it("has the probes it runs on ticked", () => {
      const html = edit(deserializeMonitor(monitor as never));
      expect(boxes(html)).toEqual([
        ["Local · Frankfurt", false],
        ["Berlin", true],
        ["Virginia · US East", true],
      ]);
    });

    it("can move off a disabled probe to the one probe left, which the list then shows", () => {
      const paused = probe("paused", { name: "Paused", supported_types: ["http"], disabled: true });
      const onPaused = { ...monitor, probe_ids: ["paused"] };
      const html = renderAs(
        member,
        <MonitorFormProvider mode="edit" monitorId="m1" initialValues={deserializeMonitor(onPaused as never)}>
          <CreateEditForm />
        </MonitorFormProvider>,
        undefined,
        (queryClient) => {
          queryClient.setQueryData(getProbesQueryKey(), { data: [paused, us] });
          queryClient.setQueryData(getMonitorsByIdQueryKey({ path: { id: "m1" } }), { data: onPaused });
        }
      );
      expect(boxes(html)).toEqual([["Virginia · US East", false]]);
    });

    it("doesn't say it runs on the default locations when none is ticked, since that keeps its probes", () => {
      const html = edit({ ...deserializeMonitor(monitor as never), probe_ids: [] });
      expect(html).not.toContain(text("monitors.form.shared.run_from.default_locations"));
    });
  });

  it("lists only the probes that run the monitor's type", () => {
    const html = create(member, [local, eu, us], { type: "tcp", probe_ids: ["local"] } as Partial<MonitorFormValues>);
    expect(boxes(html).map(([label]) => label)).toEqual(["Local · Frankfurt", "Berlin"]);
  });

  it("is left out when the install has one probe", () => {
    const html = create(member, [local]);
    expect(html).not.toContain(text("monitors.form.shared.run_from.label"));
    expect(html).not.toContain(RUN_FROM);
  });

  it("is left out when one probe runs the type", () => {
    const html = create(member, [local, eu], { type: "docker", probe_ids: ["local"] } as Partial<MonitorFormValues>);
    expect(html).not.toContain(text("monitors.form.shared.run_from.label"));
  });

  it("says a monitor with nothing ticked runs on the default locations", () => {
    const html = create(member, [local, eu], { probe_ids: [] });
    expect(html).toContain(text("monitors.form.shared.run_from.default_locations"));
    const ticked = create(member, [local, eu], { probe_ids: ["eu"] });
    expect(ticked).not.toContain(text("monitors.form.shared.run_from.default_locations"));
  });
});

describe("Run from, the list of a new monitor", () => {
  it("has the default probes that run the type ticked", () => {
    const choices = runFromChoices([local, eu, us], "http", runFromDefaultIds([local, eu, us], "http")).choices;
    const html = renderAs(member, <RunFromList choices={choices} onToggle={() => {}} />);
    expect(boxes(html)).toEqual([
      ["Local · Frankfurt", true],
      ["Berlin", false],
      ["Virginia · US East", true],
    ]);
  });
});

describe("A monitor with nothing ticked", () => {
  it("is sent without probe_ids, so the server's defaults apply", () => {
    const body = serializeMonitor({ ...defaults, probe_ids: [] } as MonitorFormValues);
    expect(body.probe_ids).toBeUndefined();
    expect("probe_ids" in JSON.parse(JSON.stringify(body))).toBe(false);
  });

  it("is sent with the probes ticked", () => {
    const body = serializeMonitor({ ...defaults, probe_ids: ["eu", "us"] } as MonitorFormValues);
    expect(body.probe_ids).toEqual(["eu", "us"]);
  });
});

describe("Run from, for an instance admin", () => {
  it("is today's probe picker, with no checkbox list", () => {
    const html = create(admin, [local, eu, us], { probe_ids: ["local"] });
    expect(html).toContain("monitor-probe-select");
    expect(html).toContain(text("monitors.form.shared.probe.label"));
    expect(html).not.toContain(text("monitors.form.shared.run_from.label"));
    expect(html).not.toContain(RUN_FROM);
  });

  it("keeps the location rule beside the picker once two probes are chosen", () => {
    const html = create(admin, [local, eu, us], { probe_ids: ["local", "eu"] });
    expect(html).toContain(text("monitors.form.shared.location_rule.label"));
  });

  it("leaves the picker out until the install has a remote probe", () => {
    const html = create(admin, [local]);
    expect(html).not.toContain("monitor-probe-select");
  });
});
