import type { MonitorMonitorResponseDto } from "@/api";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { monitorProbeIds } from "./codec";

// The i18n key of the message the form shows for a probe that doesn't run the monitor's type
export const UNSUPPORTED_PROBE_MESSAGE = "monitors.form.shared.probe.unsupported";

// A probe that lists no types runs every type
export const probeRunsType = (probe: Pick<ProbeProbeResponse, "supported_types">, type: string): boolean =>
  probe.supported_types.length === 0 || probe.supported_types.includes(type);

// The probes a monitor of the type runs on. A push monitor is fixed to the local probe, whichever
// probes the form holds.
export const probeIdsOf = (type: string, probeIds: string[]): string[] =>
  type === "push" ? [LOCAL_PROBE_ID] : probeIds;

// Whether the lists hold the same probes, whatever their order
const sameProbes = (a: string[], b: string[]): boolean =>
  a.length === b.length && [...a].sort().join("\0") === [...b].sort().join("\0");

// The message for a monitor on a probe that doesn't run its type, if it is on one. The type's
// schema can't tell, since which probes run which types comes from the probes query. A probe the
// list doesn't hold is left to the server.
export const probeTypeError = (
  values: { type: string; probe_ids: string[] },
  probes: ProbeProbeResponse[],
  saved?: Pick<MonitorMonitorResponseDto, "type" | "probe_ids">
): string | undefined => {
  const probeIds = probeIdsOf(values.type, values.probe_ids);
  // The server doesn't check an update that keeps the saved type and probes, so that a monitor
  // whose probe stopped running its type can still be renamed
  if (saved && saved.type === values.type && sameProbes(probeIds, monitorProbeIds(saved))) {
    return undefined;
  }
  const unsupported = probeIds.some((id) => {
    const probe = probes.find((candidate) => candidate.id === id);
    return probe && !probeRunsType(probe, values.type);
  });
  return unsupported ? UNSUPPORTED_PROBE_MESSAGE : undefined;
};
