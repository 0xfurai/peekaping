import { describe, expect, it } from "vitest";
import type { ProbeProbeResponse } from "@/api/types.gen";
import {
  probeIdsOf,
  probeRunsType,
  probeTypeError,
  UNSUPPORTED_PROBE_MESSAGE,
  unsupportedProbeIds,
} from "./probe-support";

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

  // peekaping-probe lists every type it can check, and push is none of them: pushes arrive at the
  // server, so a push monitor needs no executor on a probe
  it("runs a push monitor on any probe, whatever types it lists", () => {
    expect(probeRunsType(probes[1], "push")).toBe(true);
    expect(probeRunsType(probe("local", ["http", "tcp", "dns"]), "push")).toBe(true);
  });
});

describe("probeIdsOf", () => {
  it("is the monitor's probes in their order", () => {
    expect(probeIdsOf("http", ["web-only", "local"])).toEqual(["web-only", "local"]);
  });

  it("keeps a monitor with none on none, for the form to refuse", () => {
    expect(probeIdsOf("http", [])).toEqual([]);
  });

  it("is the local probe alone for a push monitor", () => {
    expect(probeIdsOf("push", ["web-only", "other"])).toEqual(["local"]);
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

  it("flags a monitor when any of its probes doesn't run the type", () => {
    expect(probeTypeError({ type: "docker", probe_ids: ["local", "web-only"] }, probes)).toBe(UNSUPPORTED_PROBE_MESSAGE);
    expect(probeTypeError({ type: "http", probe_ids: ["local", "web-only"] }, probes)).toBeUndefined();
  });

  it("lets a saved monitor keep its probes, whatever their order", () => {
    const saved = { type: "docker", probe_ids: ["web-only", "local"] };
    expect(probeTypeError({ type: "docker", probe_ids: ["local", "web-only"] }, probes, saved)).toBeUndefined();
  });

  it("leaves a form with no probe chosen to the server", () => {
    expect(probeTypeError({ type: "docker", probe_ids: [] }, [probe("local", ["http"])])).toBeUndefined();
  });

  it("flags a saved monitor that adds a probe that doesn't run its type", () => {
    const saved = { type: "docker", probe_ids: ["local"] };
    expect(probeTypeError({ type: "docker", probe_ids: ["local", "web-only"] }, probes, saved)).toBe(
      UNSUPPORTED_PROBE_MESSAGE
    );
  });
});

describe("unsupportedProbeIds", () => {
  const several = [probe("local", []), probe("web-only", ["http", "tcp"]), probe("dns-only", ["dns"])];

  it("names the probes the monitor runs on that don't run its type, in the monitor's order", () => {
    expect(unsupportedProbeIds({ type: "docker", probe_ids: ["dns-only", "local", "web-only"] }, several)).toEqual([
      "dns-only",
      "web-only",
    ]);
  });

  it("names none when every probe runs the type", () => {
    expect(unsupportedProbeIds({ type: "http", probe_ids: ["local", "web-only"] }, several)).toEqual([]);
  });

  it("names only the probes the monitor is on", () => {
    expect(unsupportedProbeIds({ type: "docker", probe_ids: ["local"] }, several)).toEqual([]);
  });

  it("puts a monitor with no probes, or none listed, on the local probe, which runs every type", () => {
    expect(unsupportedProbeIds({ type: "docker", probe_ids: [] }, several)).toEqual([]);
    expect(unsupportedProbeIds({ type: "docker", probe_ids: null }, several)).toEqual([]);
  });

  it("leaves out a probe the list doesn't hold", () => {
    expect(unsupportedProbeIds({ type: "docker", probe_ids: ["gone", "web-only"] }, several)).toEqual(["web-only"]);
    expect(unsupportedProbeIds({ type: "docker", probe_ids: ["web-only"] }, [])).toEqual([]);
  });

  it("names none for a push monitor, which runs on the local probe", () => {
    expect(unsupportedProbeIds({ type: "push", probe_ids: ["web-only"] }, several)).toEqual([]);
  });
});
