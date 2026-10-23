import { describe, expect, it } from "vitest";
import type { MonitorMonitorResponseDto } from "@/api";
import { monitorTypes, monitorFormSchema, deserializeMonitor, serializeMonitor, cloneMonitor } from "./registry";

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

  it("refuses a monitor of an unknown type", () => {
    expect(() => deserializeMonitor({ id: "x", type: "carrier-pigeon" } as MonitorMonitorResponseDto)).toThrow(
      /carrier-pigeon/
    );
  });
});
