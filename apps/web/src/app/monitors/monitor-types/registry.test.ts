import { describe, expect, it } from "vitest";
import type { MonitorMonitorResponseDto } from "@/api";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { monitorTypes, monitorFormSchema, deserializeMonitor, serializeMonitor, cloneMonitor } from "./registry";
import { withEffectiveProbes } from "./shared/probe-support";

// What the API returns for a monitor saved from the given request body
const savedMonitor = (body: ReturnType<typeof serializeMonitor>): MonitorMonitorResponseDto => ({
  ...body,
  id: "monitor-1",
  config: body.config ?? "{}",
  max_retries: body.max_retries ?? 0,
  resend_interval: body.resend_interval ?? 0,
  proxy_id: body.proxy_id ?? "",
  push_token: body.push_token ?? "",
  probe_ids: body.probe_ids ?? ["local"],
  locations: (body.probe_ids ?? ["local"]).map((probe_id) => ({
    probe_id,
    probe_name: probe_id,
    online: true,
    status: null,
    last_ping: null,
    last_heartbeat_at: null,
  })),
  tag_ids: body.tag_ids ?? [],
  active: true,
  status: 1,
  created_at: "2026-10-22T00:00:00Z",
  updated_at: "2026-10-22T00:00:00Z",
});

describe("monitor type registry", () => {
  it("lists each type once, under the type its schema declares", () => {
    const types = monitorTypes.map((definition) => definition.type);
    expect(new Set(types).size).toBe(types.length);
    for (const definition of monitorTypes) {
      expect(definition.schema.shape.type.value).toBe(definition.type);
    }
  });

  describe.each(monitorTypes.map((definition) => [definition.type, definition] as const))(
    "%s",
    (type, definition) => {
      it("is the type the form schema picks for its values", () => {
        const picked = monitorFormSchema.options.find(
          (option) => option.shape.type.value === definition.defaultValues.type
        );
        expect(picked).toBe(definition.schema);
      });

      it("reads back what it writes", () => {
        const body = definition.serialize(definition.defaultValues as never);
        expect(body.type).toBe(type);
        expect(() => JSON.parse(body.config ?? "{}")).not.toThrow();
        expect(deserializeMonitor(savedMonitor(body))).toEqual(definition.defaultValues);
      });

      it("starts on the local probe", () => {
        expect(definition.defaultValues.probe_ids).toEqual([LOCAL_PROBE_ID]);
      });

      it("reads back the probe it writes", () => {
        // A push monitor runs on the local probe whichever probe the form holds
        const probe = type === "push" ? LOCAL_PROBE_ID : "probe-1";
        const body = definition.serialize({ ...definition.defaultValues, probe_ids: ["probe-1"] } as never);
        expect(body.probe_ids).toEqual([probe]);
        expect(deserializeMonitor(savedMonitor(body)).probe_ids).toEqual([probe]);
      });

      it("reads back several probes in the order it writes them", () => {
        // A push monitor runs on the local probe whichever probes the form holds
        const probes = type === "push" ? [LOCAL_PROBE_ID] : ["probe-2", LOCAL_PROBE_ID, "probe-1"];
        const body = definition.serialize({
          ...definition.defaultValues,
          probe_ids: ["probe-2", LOCAL_PROBE_ID, "probe-1"],
        } as never);
        expect(body.probe_ids).toEqual(probes);
        expect(deserializeMonitor(savedMonitor(body)).probe_ids).toEqual(probes);
      });

      it("refuses a monitor on no probe", () => {
        const result = definition.schema.safeParse({ ...definition.defaultValues, probe_ids: [] });
        expect(result.success).toBe(false);
        expect(result.error?.issues.some((issue) => issue.path[0] === "probe_ids")).toBe(true);
      });

      it("serializes through the registry by the values' type", () => {
        expect(serializeMonitor(definition.defaultValues)).toEqual(
          definition.serialize(definition.defaultValues as never)
        );
      });
    }
  );

  it("clones a monitor under a copy's name", () => {
    const [http] = monitorTypes;
    const body = http.serialize(http.defaultValues);
    const clone = cloneMonitor(savedMonitor({ ...body, name: "Site" }));
    expect(clone?.name).toBe("Site Copy");
    expect(clone?.type).toBe("http");
  });

  it("puts a monitor whose response has no probe on the local probe", () => {
    const [http] = monitorTypes;
    const saved = savedMonitor(http.serialize(http.defaultValues));
    // A server from before probes sends no probe_ids at all
    const withoutProbes: Partial<MonitorMonitorResponseDto> = { ...saved };
    delete withoutProbes.probe_ids;
    expect(deserializeMonitor(withoutProbes as MonitorMonitorResponseDto).probe_ids).toEqual([LOCAL_PROBE_ID]);
    expect(deserializeMonitor({ ...saved, probe_ids: [] }).probe_ids).toEqual([LOCAL_PROBE_ID]);
  });

  it("keeps a push monitor on the local probe", () => {
    const push = monitorTypes.find((definition) => definition.type === "push")!;
    const body = push.serialize({ ...push.defaultValues, probe_ids: ["probe-1"] });
    expect(body.probe_ids).toEqual([LOCAL_PROBE_ID]);
  });

  it("takes a push monitor with no probe in the form as one on the local probe", () => {
    const push = monitorTypes.find((definition) => definition.type === "push")!;
    // The field shows Local for a push monitor, whatever the form holds
    const checked = withEffectiveProbes({ ...push.defaultValues, probe_ids: [] });
    const result = monitorFormSchema.safeParse(checked);
    expect(result.error?.issues).toBeUndefined();
    expect(serializeMonitor(result.data!).probe_ids).toEqual([LOCAL_PROBE_ID]);
    // Another type still needs a probe
    const [http] = monitorTypes;
    expect(monitorFormSchema.safeParse(withEffectiveProbes({ ...http.defaultValues, probe_ids: [] })).success).toBe(
      false
    );
  });

  it("refuses a monitor of an unknown type", () => {
    expect(() => deserializeMonitor({ id: "x", type: "carrier-pigeon" } as MonitorMonitorResponseDto)).toThrow(
      /carrier-pigeon/
    );
  });
});
