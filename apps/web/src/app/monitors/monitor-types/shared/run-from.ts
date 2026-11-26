import type { ProbeProbeResponse } from "@/api/types.gen";
import { probeRunsType } from "./probe-support";

export type RunFromChoice = { id: string; label: string; ticked: boolean };

// The plain "Run from" list of someone who isn't an instance admin: each enabled probe that runs the
// type, named with its location when it has one, and nothing else about the probe.
const runFromProbes = (probes: ProbeProbeResponse[], type: string): ProbeProbeResponse[] =>
  probes.filter((probe) => !probe.disabled && probeRunsType(probe, type));

const runFromLabel = (probe: Pick<ProbeProbeResponse, "name" | "location">): string =>
  probe.location ? `${probe.name} · ${probe.location}` : probe.name;

// The list for a monitor of the type, with the probes in `ticked` ticked. It is hidden when there is
// nothing to choose: no probe to run on, one probe the monitor already runs on, or a push monitor,
// which is fixed to the local probe. One probe is shown when the monitor runs on a probe that isn't
// a choice (disabled, or no longer running the type), so that it can move to that probe.
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
  const offTheList = ticked.some((id) => !choices.some((choice) => choice.id === id));
  const nothingToChoose = choices.length === 0 || (choices.length === 1 && !offTheList);
  return { choices, hidden: type === "push" || nothingToChoose };
};

// The probes a new monitor of the type starts on: the default ones that run it. With a single probe
// to run on (the list is hidden) that probe, default or not. With no probe data (before the probes
// load, or when the probes query fails) none, so the monitor is sent without probes and the server
// puts it on the install's default ones.
export const runFromDefaultIds = (probes: ProbeProbeResponse[], type: string): string[] => {
  if (probes.length === 0) return [];
  const candidates = runFromProbes(probes, type);
  if (type !== "push" && candidates.length <= 1) return candidates.map((probe) => probe.id);
  return candidates.filter((probe) => probe.is_default).map((probe) => probe.id);
};

// The monitor's probes after a tick or untick, out of the ids the list shows. A probe the list
// doesn't show (disabled, or no longer running the type) stays on a saved monitor until the first
// change, and goes with it: the save would be refused with it, and the list has no box to untick it.
export const toggleRunFrom = (probeIds: string[], choiceIds: string[], id: string, ticked: boolean): string[] => {
  const shown = probeIds.filter((existing) => choiceIds.includes(existing));
  return ticked ? (shown.includes(id) ? shown : [...shown, id]) : shown.filter((existing) => existing !== id);
};

// The monitor's probes after its type changes to `type`: the ones that don't run it go. A probe that
// isn't loaded stays, as does a disabled one that runs the type. When none is left and one probe runs
// the type, the monitor moves to it: the list is hidden then, and a saved monitor with no probe ticked
// can't be saved.
export const runFromAfterTypeChange = (probes: ProbeProbeResponse[], type: string, probeIds: string[]): string[] => {
  const kept = probeIds.filter((id) => {
    const probe = probes.find((candidate) => candidate.id === id);
    return !probe || probeRunsType(probe, type);
  });
  if (kept.length > 0 || probeIds.length === 0) return kept;
  const candidates = runFromProbes(probes, type);
  return candidates.length === 1 ? [candidates[0].id] : kept;
};

// What the form's probes become when the probes load or the type changes, or undefined to leave them
// as they are. A new monitor the user hasn't ticked anything for (`presets`) starts on the default
// probes, and on none while there is no probe data. Otherwise a type change drops only the probes
// that don't run the new type, and a saved monitor keeps its own probes while its type does, even
// one the list leaves out. With no probe data a saved monitor or a clone keeps what it has.
export const runFromFormIds = (
  probes: ProbeProbeResponse[],
  type: string,
  current: string[],
  { presets, typeChanged, typeIsSaved }: { presets: boolean; typeChanged: boolean; typeIsSaved: boolean }
): string[] | undefined => {
  if (presets) {
    const ids = runFromDefaultIds(probes, type);
    return ids.join("\0") !== current.join("\0") ? ids : undefined;
  }
  if (probes.length === 0 || (!typeChanged && typeIsSaved)) return undefined;
  const kept = runFromAfterTypeChange(probes, type, current);
  return kept.join("\0") !== current.join("\0") ? kept : undefined;
};
