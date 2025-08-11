import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import StartEndTime from "./start-end-time";
import Timezone from "./timezone";
import StartEndDateTime from "./start-end-date-time";
import { Checkbox } from "@/components/ui/checkbox";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const DAYS_OF_MONTH = Array.from({ length: 31 }, (_, i) => i + 1);

const RecurringDayOfMonthForm = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  
  const LAST_DAYS = [
    { id: "lastDay1", label: t("maintenance.form.last_day_of_month"), value: "lastDay1" },
  ];
  return (
    <>
      <FormField
        control={form.control}
        name="daysOfMonth"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("maintenance.form.day_of_month")}</FormLabel>
            <div className="grid grid-cols-8 gap-2">
              {DAYS_OF_MONTH.map((day) => (
                <FormItem
                  key={day}
                  className="flex flex-col items-center space-y-1"
                >
                  <FormLabel className="text-xs text-gray-600">{day}</FormLabel>
                  <FormControl>
                    <Checkbox
                      checked={field.value?.includes(day)}
                      onCheckedChange={(checked) => {
                        const current = field.value || [];
                        if (checked) {
                          field.onChange([...current, day]);
                        } else {
                          field.onChange(current.filter((v: number) => v !== day));
                        }
                      }}
                    />
                  </FormControl>
                </FormItem>
              ))}
            </div>

            <div className="mt-4">
              <FormLabel className="text-sm">{t("maintenance.form.last_day")}</FormLabel>
              <div className="mt-2">
                {LAST_DAYS.map((lastDay) => (
                  <FormItem
                    key={lastDay.id}
                    className="flex flex-row items-start space-x-3 space-y-0"
                  >
                    <FormControl>
                      <Checkbox
                        checked={field.value?.includes(lastDay.value)}
                        onCheckedChange={(checked) => {
                          const current = field.value || [];
                          if (checked) {
                            field.onChange([...current, lastDay.value]);
                          } else {
                            field.onChange(
                              current.filter((v: string) => v !== lastDay.value)
                            );
                          }
                        }}
                      />
                    </FormControl>
                    <FormLabel className="text-sm">{lastDay.label}</FormLabel>
                  </FormItem>
                ))}
              </div>
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

export default RecurringDayOfMonthForm;
