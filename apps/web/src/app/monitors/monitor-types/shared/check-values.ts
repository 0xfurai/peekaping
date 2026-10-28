import { zodResolver } from "@hookform/resolvers/zod";
import type { Resolver } from "react-hook-form";
import type { MonitorMonitorResponseDto } from "@/api";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { checkedMonitorFormSchema, type MonitorFormValues } from "../registry";
import { intervalMinimumErrors } from "./intervals-support";
import { probeTypeError, withEffectiveProbes } from "./probe-support";

// The monitor form's check: the schema, and a check of the probe that it can't make, since which
// probes run which types comes from the probes query.
export const checkMonitorValues = async (
  values: MonitorFormValues,
  context: Parameters<Resolver<MonitorFormValues>>[1],
  options: Parameters<Resolver<MonitorFormValues>>[2],
  {
    probes,
    saved,
    mayUseDefaultProbes,
    minCheckInterval,
  }: {
    probes: ProbeProbeResponse[];
    saved?: Pick<MonitorMonitorResponseDto, "type" | "probe_ids">;
    // A new monitor with no probe ticked is valid: it is sent without probe_ids and runs on the
    // default probes. It is checked as the local probe, then put back.
    mayUseDefaultProbes: boolean;
    // The install's smallest check interval and retry interval, in seconds. The schema knows only
    // 20, the smallest any install allows.
    minCheckInterval: number;
  }
) => {
  const onDefaults = mayUseDefaultProbes && values.probe_ids.length === 0;
  // A push monitor is checked and submitted on the local probe, as its probe field shows it (the
  // form keeps the earlier selection for when the type changes back)
  const effective = withEffectiveProbes(onDefaults ? { ...values, probe_ids: [LOCAL_PROBE_ID] } : values);
  const result = await zodResolver(checkedMonitorFormSchema)(effective, context, options);
  // The install's minimum comes before the schema's own message for a value below 20: it names the
  // number the user has to reach
  const intervals = intervalMinimumErrors(effective, minCheckInterval);
  if (onDefaults) {
    const errors = { ...result.errors, ...intervals };
    return Object.keys(errors).length === 0
      ? { ...result, values: { ...result.values, probe_ids: [] } }
      : { values: {}, errors };
  }
  const message = probeTypeError(effective, probes, saved);
  const errors = { ...result.errors, ...intervals, ...(message ? { probe_ids: { type: "probe", message } } : {}) };
  if (Object.keys(errors).length === 0) return result;
  return { values: {}, errors };
};
