import { useEffect } from "react";
import { useFormContext } from "react-hook-form";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useProbes } from "@/hooks/useProbes";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { probeOf, probeRunsType } from "./probe-support";

// Where the monitor runs. It is left out until the install has a probe besides the local one.
const Probe = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  const { probes, hasRemoteProbe } = useProbes();
  const type: string = form.watch("type");
  const probeIds: string[] = form.watch("probe_ids");
  const isPush = type === "push";

  // The form's resolver reports a probe that doesn't run the type (a type change can leave the
  // selected one in that state); check it when the type, the probe or the probes change. Not on
  // `form`: the form context is a new object on every render, and the check re-renders the form.
  const { trigger } = form;
  useEffect(() => {
    if (hasRemoteProbe) void trigger("probe_ids");
  }, [trigger, hasRemoteProbe, type, probeIds, probes]);

  if (!hasRemoteProbe) return null;

  return (
    <FormField
      control={form.control}
      name="probe_ids"
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t("monitors.form.shared.probe.label")}</FormLabel>
          <Select
            // A push monitor is fixed to the local probe
            disabled={isPush}
            value={probeOf(type, field.value)}
            // Radix reports an empty value when the form sets one the items haven't registered
            // with the select yet, as the edit form does when it loads the saved monitor
            onValueChange={(id) => id && field.onChange([id])}
          >
            <FormControl>
              <SelectTrigger className="w-full" data-testid="monitor-probe-select">
                <SelectValue placeholder={t("monitors.form.shared.probe.placeholder")} />
              </SelectTrigger>
            </FormControl>

            <SelectContent>
              {probes.map((probe) => {
                const runsType = probeRunsType(probe, type);
                return (
                  <SelectItem key={probe.id} value={probe.id} disabled={!runsType}>
                    {runsType
                      ? probe.name
                      : t("monitors.form.shared.probe.option_unsupported", { name: probe.name })}
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
          {isPush && <FormDescription>{t("monitors.form.shared.probe.push_fixed")}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  );
};

export default Probe;
