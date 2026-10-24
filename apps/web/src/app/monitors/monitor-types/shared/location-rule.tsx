import { useEffect } from "react";
import { useFormContext } from "react-hook-form";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useProbes } from "@/hooks/useProbes";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { probeIdsOf } from "./probe-support";
import { LOCATION_RULES, type LocationRule } from "./schema";

// When the monitor is Down: when any of its locations is, all of them, or at least a number. Shown
// below the probe select once more than one probe is chosen; with one location there is nothing to
// choose. A push monitor has one location always.
const LocationRuleField = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  const { hasRemoteProbe } = useProbes();
  const type: string = form.watch("type");
  const rule: LocationRule = form.watch("location_rule");
  const threshold: number | null = form.watch("location_threshold");
  const probeCount = probeIdsOf(type, form.watch("probe_ids")).length;
  const isPush = type === "push";
  const visible = hasRemoteProbe && !isPush && probeCount > 1;

  // A rule whose number is missing, not a whole number from 1 up, or more than the monitor's
  // locations can't be saved, and with one probe the
  // field is hidden, so there is nothing to correct it with: it goes back to any. A hidden rule that
  // can be saved, such as "all" or "at least 1", is left as it is. A push monitor keeps what the
  // form holds (the serializer sends it on the any rule) for when the type changes back.
  const { setValue, trigger } = form;
  useEffect(() => {
    if (isPush || probeCount > 1 || rule !== "at_least") return;
    const n = Number(threshold);
    if (threshold === null || !Number.isInteger(n) || n < 1 || n > probeCount) {
      setValue("location_rule", "any");
      setValue("location_threshold", null);
    }
  }, [isPush, probeCount, rule, threshold, setValue]);

  // The threshold's limit moves with the probes: check it when they or the number change, as the
  // probe field does, so that a message clears as soon as the number is right (the form only
  // rechecks a field on change after a first submit)
  useEffect(() => {
    if (visible && rule === "at_least") void trigger("location_threshold");
  }, [trigger, visible, rule, probeCount, threshold]);

  if (!visible) return null;

  const labels: Record<LocationRule, string> = {
    any: t("monitors.form.shared.location_rule.any"),
    all: t("monitors.form.shared.location_rule.all"),
    at_least: t("monitors.form.shared.location_rule.at_least"),
  };

  return (
    <>
      <FormField
        control={form.control}
        name="location_rule"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.shared.location_rule.label")}</FormLabel>
            <Select
              value={field.value}
              onValueChange={(value: LocationRule) => {
                field.onChange(value);
                // Start "at least" on a number that is a rule: 2 of the locations
                if (value === "at_least" && form.getValues("location_threshold") === null) {
                  form.setValue("location_threshold", 2);
                }
              }}
            >
              <FormControl>
                <SelectTrigger data-testid="monitor-location-rule">
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {LOCATION_RULES.map((value) => (
                  <SelectItem key={value} value={value}>
                    {labels[value]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>{t("monitors.form.shared.location_rule.description")}</FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {rule === "at_least" && (
        <FormField
          control={form.control}
          name="location_threshold"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("monitors.form.shared.location_rule.threshold_label")}</FormLabel>
              {/* No min or max on the input: the browser would block the submit with its own bubble,
                  and the form's message says what is wrong, in the app's language */}
              <FormControl>
                <Input
                  data-testid="monitor-location-threshold"
                  type="number"
                  {...field}
                  value={field.value ?? ""}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </>
  );
};

export default LocationRuleField;
