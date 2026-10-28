import type { ProbeProbeResponse } from "@/api/types.gen";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { probeRunsType } from "./probe-support";

export type RunFromChoice = { id: string; label: string; ticked: boolean };

// The plain "Run from" list of someone who isn't an instance admin: each enabled probe that runs the
// type, named with its location when it has one, and nothing else about the probe.
const runFromProbes = (probes: ProbeProbeResponse[], type: string): ProbeProbeResponse[] =>
  probes.filter((probe) => !probe.disabled && probeRunsType(probe, type));

const runFromLabel = (probe: Pick<ProbeProbeResponse, "name" | "location">): string =>
  probe.location ? `${probe.name} · ${probe.location}` : probe.name;

// The list for a monitor of the type, with the probes in `ticked` ticked. It is hidden when there is
// nothing to choose: one probe to run on, or a push monitor, which is fixed to the local probe.
export const runFromChoices = (
  probes: ProbeProbeResponse[],
  type: string,
  ticked: string[]
): { choices: RunFromChoice[]; hidden: boolean } => {
  const choices = runFromProbes(probes, type).map((probe) => ({
    id: probe.id,
    label: runFromLabel(probe),
    ticked: ticked.includes(probe.id),
  }));
  return { choices, hidden: type === "push" || choices.length <= 1 };
};

// The probes a new monitor of the type starts on: the default ones that run it. With a single probe
// to run on (the list is hidden) that probe, default or not. Before the probes load, the local probe.
export const runFromDefaultIds = (probes: ProbeProbeResponse[], type: string): string[] => {
  if (probes.length === 0) return [LOCAL_PROBE_ID];
  const candidates = runFromProbes(probes, type);
  if (type !== "push" && candidates.length <= 1) return candidates.map((probe) => probe.id);
  return candidates.filter((probe) => probe.is_default).map((probe) => probe.id);
};

// The monitor's probes after a tick or untick. Probes the list doesn't show (a disabled one the
// monitor still runs on) stay as they were.
export const toggleRunFrom = (probeIds: string[], id: string, ticked: boolean): string[] =>
  ticked ? (probeIds.includes(id) ? probeIds : [...probeIds, id]) : probeIds.filter((existing) => existing !== id);
