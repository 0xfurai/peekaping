import { zodResolver } from "@hookform/resolvers/zod";
import type { Resolver } from "react-hook-form";
import type { MonitorMonitorResponseDto } from "@/api";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { checkedMonitorFormSchema, type MonitorFormValues } from "../registry";
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
  }: {
    probes: ProbeProbeResponse[];
    saved?: Pick<MonitorMonitorResponseDto, "type" | "probe_ids">;
    // A new monitor with no probe ticked is valid: it is sent without probe_ids and runs on the
    // default probes. It is checked as the local probe, then put back.
    mayUseDefaultProbes: boolean;
  }
) => {
  const onDefaults = mayUseDefaultProbes && values.probe_ids.length === 0;
  // A push monitor is checked and submitted on the local probe, as its probe field shows it (the
  // form keeps the earlier selection for when the type changes back)
  const effective = withEffectiveProbes(onDefaults ? { ...values, probe_ids: [LOCAL_PROBE_ID] } : values);
  const result = await zodResolver(checkedMonitorFormSchema)(effective, context, options);
  if (onDefaults) {
    return Object.keys(result.errors).length === 0
      ? { ...result, values: { ...result.values, probe_ids: [] } }
      : result;
  }
  const message = probeTypeError(effective, probes, saved);
  if (!message) return result;
  return { values: {}, errors: { ...result.errors, probe_ids: { type: "probe", message } } };
};
