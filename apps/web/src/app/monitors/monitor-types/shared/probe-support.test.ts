import { describe, expect, it } from "vitest";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { probeOf, probeRunsType, probeTypeError, UNSUPPORTED_PROBE_MESSAGE } from "./probe-support";

const probe = (id: string, supportedTypes: string[]): ProbeProbeResponse => ({
  id,
  name: id,
  location: "",
  status: "online",
  supported_types: supportedTypes,
  disabled: false,
  instances: [],
  notification_ids: [],
  token_prefix: "",
  created_at: "2026-10-23T00:00:00Z",
  updated_at: "2026-10-23T00:00:00Z",
});

const probes = [probe("local", []), probe("web-only", ["http", "tcp"])];

describe("probeRunsType", () => {
  it("runs every type when the probe lists none", () => {
    expect(probeRunsType(probes[0], "docker")).toBe(true);
  });

  it("runs only the types the probe lists", () => {
    expect(probeRunsType(probes[1], "http")).toBe(true);
    expect(probeRunsType(probes[1], "docker")).toBe(false);
  });
});

describe("probeOf", () => {
  it("is the monitor's probe, or the local one without any", () => {
    expect(probeOf("http", ["web-only"])).toBe("web-only");
    expect(probeOf("http", [])).toBe("local");
  });

  it("is the local probe for a push monitor", () => {
    expect(probeOf("push", ["web-only"])).toBe("local");
  });
});

describe("probeTypeError", () => {
  it("flags a probe that doesn't run the type", () => {
    expect(probeTypeError({ type: "docker", probe_ids: ["web-only"] }, probes)).toBe(UNSUPPORTED_PROBE_MESSAGE);
  });

  it("accepts a probe that runs the type", () => {
    expect(probeTypeError({ type: "http", probe_ids: ["web-only"] }, probes)).toBeUndefined();
    expect(probeTypeError({ type: "docker", probe_ids: ["local"] }, probes)).toBeUndefined();
  });

  it("leaves a probe the list doesn't hold to the server", () => {
    expect(probeTypeError({ type: "docker", probe_ids: ["gone"] }, probes)).toBeUndefined();
    expect(probeTypeError({ type: "docker", probe_ids: ["web-only"] }, [])).toBeUndefined();
  });

  it("doesn't flag a push monitor, which runs on the local probe", () => {
    expect(probeTypeError({ type: "push", probe_ids: ["web-only"] }, [probe("local", []), probe("web-only", ["http"])]))
      .toBeUndefined();
  });

  it("lets a saved monitor keep a type and probe the probe no longer runs", () => {
    const saved = { type: "docker", probe_ids: ["web-only"] };
    expect(probeTypeError({ type: "docker", probe_ids: ["web-only"] }, probes, saved)).toBeUndefined();
  });

  it("flags a saved monitor whose type or probe changes to an unsupported pair", () => {
    const saved = { type: "http", probe_ids: ["local"] };
    expect(probeTypeError({ type: "docker", probe_ids: ["web-only"] }, probes, saved)).toBe(UNSUPPORTED_PROBE_MESSAGE);
    expect(probeTypeError({ type: "docker", probe_ids: ["web-only"] }, probes, { type: "http", probe_ids: ["web-only"] }))
      .toBe(UNSUPPORTED_PROBE_MESSAGE);
  });
});
