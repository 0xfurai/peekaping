import { useMemo } from "react";
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import StartEndTime from "./start-end-time";
import Timezone from "./timezone";
import StartEndDateTime from "./start-end-date-time";
import { Checkbox } from "@/components/ui/checkbox";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";


const RecurringWeekdayForm = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  
  const WEEKDAYS = useMemo(() => [
    { id: "0", label: t("maintenance.weekdays.sun"), value: 0 },
    { id: "1", label: t("maintenance.weekdays.mon"), value: 1 },
    { id: "2", label: t("maintenance.weekdays.tue"), value: 2 },
    { id: "3", label: t("maintenance.weekdays.wed"), value: 3 },
    { id: "4", label: t("maintenance.weekdays.thu"), value: 4 },
    { id: "5", label: t("maintenance.weekdays.fri"), value: 5 },
    { id: "6", label: t("maintenance.weekdays.sat"), value: 6 },
  ], [t]);

  return (
    <>
      <FormField
        control={form.control}
        name="weekdays"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("maintenance.form.day_of_week")}</FormLabel>
            <div className="flex gap-4">
              {WEEKDAYS.map((weekday) => (
                <FormItem
                  key={weekday.id}
                  className="flex flex-col items-center space-y-0.5"
                >
                  <FormLabel className="text-xs text-gray-600">{weekday.label}</FormLabel>
                  <FormControl>
                    <Checkbox
                      checked={field.value?.includes(weekday.value)}
                      onCheckedChange={(checked) => {
                        const current = field.value || [];
                        if (checked) {
                          field.onChange([...current, weekday.value]);
                        } else {
                          field.onChange(
                            current.filter((v: number) => v !== weekday.value)
                          );
                        }
                      }}
                    />
                  </FormControl>
                </FormItem>
              ))}
            </div>
            <FormMessage />
          </FormItem>
        )}
      />

      <StartEndTime />
      <Timezone />

      <div className="space-y-4">
        <FormLabel>{t("maintenance.form.effective_date_range")}</FormLabel>
        <StartEndDateTime />
      </div>
    </>
  );
};

export default RecurringWeekdayForm;
