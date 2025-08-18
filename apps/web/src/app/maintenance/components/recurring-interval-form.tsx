import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import StartEndTime from "./start-end-time";
import Timezone from "./timezone";
import StartEndDateTime from "./start-end-date-time";
import { Input } from "@/components/ui/input";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const RecurringIntervalForm = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="intervalDay"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("maintenance.form.interval_label")}</FormLabel>
            <FormControl>
              <Input
                type="number"
                min="1"
                max="3650"
                step="1"
                {...field}
                onChange={(e) => field.onChange(parseInt(e.target.value) || 1)}
              />
            </FormControl>
            <FormDescription>
              {field.value &&
                field.value >= 1 &&
                t(field.value > 1 ? "maintenance.form.interval_description_plural" : "maintenance.form.interval_description", { days: field.value })}
            </FormDescription>
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

export default RecurringIntervalForm;
