import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { getProbesOptions } from "@/api/@tanstack/react-query.gen";
import { LOCAL_PROBE_ID } from "@/app/probes/components/local-probe";
import { useLocalizedTranslation } from "./useTranslation";

// The install's probes, for the places that show the probe a monitor runs on. Every caller shares
// the one probes query, so a list of monitors reads it once.
export const useProbes = () => {
  const { t } = useLocalizedTranslation();
  const { data, isPending } = useQuery({ ...getProbesOptions() });
  // MongoDB has no probes (ADR 0001): its list is empty
  const probes = useMemo(() => data?.data ?? [], [data]);

  return {
    probes,
    isPending,
    // Until the install has a probe besides the local one there is nothing to choose, so the
    // monitor form and list leave probes out
    hasRemoteProbe: probes.some((probe) => probe.id !== LOCAL_PROBE_ID),
    // The name of the probe a monitor runs on. The local probe is named even when the list is
    // empty.
    probeName: (probeIds: string[] | null | undefined): string => {
      const id = probeIds?.[0] ?? LOCAL_PROBE_ID;
      const probe = probes.find((candidate) => candidate.id === id);
      if (probe) return probe.name;
      return id === LOCAL_PROBE_ID ? t("probes.local_name") : id;
    },
  };
};
