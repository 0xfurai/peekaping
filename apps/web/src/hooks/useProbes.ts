import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { getProbesOptions } from "@/api/@tanstack/react-query.gen";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { monitorProbes, probeNames } from "@/app/monitors/locations";
import { unsupportedProbeIds } from "@/app/monitors/monitor-types/shared/probe-support";
import { useLocalizedTranslation } from "./useTranslation";

// The install's probes, for the places that show the probe a monitor runs on. Every caller shares
// the one probes query, so a list of monitors reads it once.
export const useProbes = () => {
  const { t } = useLocalizedTranslation();
  const { data, isPending } = useQuery({ ...getProbesOptions() });
  const probes = useMemo(() => data?.data ?? [], [data]);

  return {
    probes,
    isPending,
    // Until the install has a probe besides the local one there is nothing to choose, so the
    // monitor form and list leave probes out
    hasRemoteProbe: probes.some((probe) => probe.id !== LOCAL_PROBE_ID),
    // The names of the probes a monitor runs on, in the monitor's order. The local probe is named
    // even when the list is empty.
    probeNames: (probeIds: string[] | null | undefined): string[] =>
      probeNames(probeIds, probes, t("probes.local_name")),
    // The probes a monitor runs on, with their ids, for keying a list of them
    monitorProbes: (probeIds: string[] | null | undefined): { id: string; name: string }[] =>
      monitorProbes(probeIds, probes, t("probes.local_name")),
    // The ids of the probes a monitor runs on that don't run its type, so that nothing checks it there
    unsupportedProbeIds: (monitor: { type: string; probe_ids?: string[] | null }): string[] =>
      unsupportedProbeIds(monitor, probes),
    // The name of one probe, such as a location's
    probeName: (probeId: string): string => probeNames([probeId], probes, t("probes.local_name"))[0],
  };
};
