import { useEffect, useId, useRef } from "react";
import { useFormContext } from "react-hook-form";
import { Checkbox } from "@/components/ui/checkbox";
import { FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { useProbes } from "@/hooks/useProbes";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { runFromAfterTypeChange, runFromChoices, runFromDefaultIds, toggleRunFrom, type RunFromChoice } from "./run-from";

// The list itself: a checkbox for each choice, in a group named by the element labelledBy names
export const RunFromList = ({
  choices,
  onToggle,
  labelledBy,
}: {
  choices: RunFromChoice[];
  onToggle: (id: string, ticked: boolean) => void;
  labelledBy?: string;
}) => (
  <div className="flex flex-col gap-2" data-testid="monitor-run-from" role="group" aria-labelledby={labelledBy}>
    {choices.map((choice) => (
      <label key={choice.id} className="flex items-center gap-2 text-sm font-normal">
        <Checkbox checked={choice.ticked} onCheckedChange={(checked) => onToggle(choice.id, checked === true)} />
        {choice.label}
      </label>
    ))}
  </div>
);

// Where the monitor runs, for someone who isn't an instance admin: the probes that run its type as a
// plain list of names and locations. A new monitor starts on the default probes; with none ticked it
// is sent without probes, and the server puts it on the default ones. Hidden when there is one
// probe to run on. A location rule the monitor already has is left as it is, out of sight.
const RunFrom = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  const { presetsProbes, probesChosenByHand, monitor, mode } = useMonitorFormContext();
  const { probes } = useProbes();
  const type: string = form.watch("type");
  const probeIds: string[] = form.watch("probe_ids");
  const { choices, hidden } = runFromChoices(probes, type, probeIds);
  const { setValue, getValues, trigger } = form;
  const labelId = useId();

  // A new monitor starts on the default probes for its type: when the probes load, and when the type
  // changes, until the user has ticked something. After that, and for a saved monitor or a clone, a
  // type change drops only the probes that don't run the new type. A saved monitor's own probes
  // stay while its type does, even one the list leaves out.
  const savedType = monitor?.data.type;
  const previousType = useRef<string | undefined>(undefined);
  useEffect(() => {
    const typeChanged = previousType.current !== undefined && previousType.current !== type;
    previousType.current = type;
    if (probes.length === 0) return;
    if (presetsProbes && !probesChosenByHand.current) {
      setValue("probe_ids", runFromDefaultIds(probes, type));
      return;
    }
    if (!typeChanged && savedType === type) return;
    const current: string[] = getValues("probe_ids");
    const kept = runFromAfterTypeChange(probes, type, current);
    if (kept.join("\0") !== current.join("\0")) setValue("probe_ids", kept);
  }, [presetsProbes, probesChosenByHand, savedType, setValue, getValues, type, probes]);

  // The form's resolver reports a probe that doesn't run the type, or none ticked on a saved monitor
  useEffect(() => {
    void trigger("probe_ids");
  }, [trigger, type, probeIds, probes]);

  if (hidden) return null;

  const noneTicked = probeIds.length === 0;
  return (
    <FormField
      control={form.control}
      name="probe_ids"
      render={() => (
        <FormItem>
          <FormLabel id={labelId}>{t("monitors.form.shared.run_from.label")}</FormLabel>
          <RunFromList
            choices={choices}
            labelledBy={labelId}
            onToggle={(id, ticked) => {
              probesChosenByHand.current = true;
              setValue(
                "probe_ids",
                toggleRunFrom(
                  getValues("probe_ids"),
                  choices.map((choice) => choice.id),
                  id,
                  ticked
                ),
                { shouldDirty: true }
              );
            }}
          />
          {noneTicked && mode === "create" && (
            <FormDescription>{t("monitors.form.shared.run_from.default_locations")}</FormDescription>
          )}
          <FormMessage />
        </FormItem>
      )}
    />
  );
};

export default RunFrom;
