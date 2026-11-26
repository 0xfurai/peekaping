import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { TypographyH4 } from "@/components/ui/typography";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { getMonitorType, type MonitorFormValues } from "../registry";
import { useMonitorTypeOptions } from "./type-options";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import { useMinCheckInterval } from "@/hooks/useMinCheckInterval";
import { withMinimumIntervals } from "./intervals-support";
import { valuesForNewType } from "./type-change";

const General = () => {
  const { form, mode } = useMonitorFormContext();
  const { t } = useLocalizedTranslation();
  const typeOptions = useMonitorTypeOptions();
  const minCheckInterval = useMinCheckInterval();

  return (
    <>
      <TypographyH4>{t('ui.general')}</TypographyH4>
      <FormField
        control={form.control}
        name="name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('forms.labels.monitor_name')}</FormLabel>
            <FormControl>
              <Input placeholder={t('forms.placeholders.monitor_name')} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('forms.labels.monitor_type')}</FormLabel>
            <Select
              onValueChange={(val) => {
                // A new monitor that changes type starts from the new type's defaults, keeping
                // the name, the probes and the channels (see valuesForNewType). Done here, before
                // the type's form mounts, so that what its effects set (a push token) stays. An
                // edited monitor keeps its values.
                const definition = getMonitorType(val);
                if (mode === "create" && definition) {
                  form.reset(
                    valuesForNewType(
                      withMinimumIntervals(definition.defaultValues, minCheckInterval) as MonitorFormValues,
                      form.getValues()
                    )
                  );
                  return;
                }
                field.onChange(val);
              }}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t('common.select')} />
                </SelectTrigger>
              </FormControl>

              <SelectContent>
                {typeOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

export default General;
