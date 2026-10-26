import { useEffect, useState } from "react";
import { useFormContext } from "react-hook-form";
import { MultiSelect } from "@/components/multi-select";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { useProbes } from "@/hooks/useProbes";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { defaultProbeIds, probeIdsOf, probeRunsType } from "./probe-support";

// Where the monitor runs: one or more probes. It is left out until the install has a probe
// besides the local one.
const Probe = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  const { presetsProbes, probesChosenByHand } = useMonitorFormContext();
  const { probes, hasRemoteProbe } = useProbes();
  const [search, setSearch] = useState("");
  const type: string = form.watch("type");
  const probeIds: string[] = form.watch("probe_ids");
  const isPush = type === "push";

  // The form's resolver reports a probe that doesn't run the type (a type change can leave the
  // selected one in that state); check it when the type, the probes or the probe list change. Not
  // on `form`: the form context is a new object on every render, and the check re-renders the form.
  const { trigger, setValue } = form;
  useEffect(() => {
    if (hasRemoteProbe) void trigger("probe_ids");
  }, [trigger, hasRemoteProbe, type, probeIds, probes]);

  // A new monitor starts on the default probes for its type: when the probes load, and when the
  // type changes (the general section's reset keeps the old selection, and this field mounts again
  // for the new type). Not once the user has chosen probes, and not where the field is hidden.
  useEffect(() => {
    if (!presetsProbes || !hasRemoteProbe || probesChosenByHand.current) return;
    setValue("probe_ids", defaultProbeIds(type, probes));
  }, [presetsProbes, probesChosenByHand, hasRemoteProbe, setValue, type, probes]);

  if (!hasRemoteProbe) return null;

  const options = probes.map((probe) => {
    const runsType = probeRunsType(probe, type);
    return {
      value: probe.id,
      label: runsType ? probe.name : t("monitors.form.shared.probe.option_unsupported", { name: probe.name }),
      disabled: !runsType,
    };
  });

  return (
    <FormField
      control={form.control}
      name="probe_ids"
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t("monitors.form.shared.probe.label")}</FormLabel>
          <FormControl>
            <MultiSelect
              data-testid="monitor-probe-select"
              options={options}
              // A push monitor is fixed to the local probe
              disabled={isPush}
              value={probeIdsOf(type, field.value)}
              onValueChange={(value) => {
                probesChosenByHand.current = true;
                field.onChange(value);
              }}
              placeholder={t("monitors.form.shared.probe.placeholder")}
              search={search}
              onSearchChange={setSearch}
            />
          </FormControl>
          <FormDescription>
            {isPush ? t("monitors.form.shared.probe.push_fixed") : t("monitors.form.shared.probe.description")}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  );
};

export default Probe;
