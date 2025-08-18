import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useFormContext } from "react-hook-form";
import Timezone from "./timezone";
import StartEndDateTime from "./start-end-date-time";
import cronstrue from "cronstrue";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const CronExpressionForm = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="cron"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("maintenance.form.cron_expression")}</FormLabel>
            <FormDescription>
              {cronstrue.toString(field.value, {
                throwExceptionOnParseError: false
              })}
            </FormDescription>
            <FormControl>
              <Input placeholder="30 3 * * *" {...field} />
            </FormControl>
            <FormDescription>{t("maintenance.form.cron_description")}</FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="duration"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("maintenance.form.duration_minutes")}</FormLabel>
            <FormControl>
              <Input
                type="number"
                min="1"
                step="1"
                {...field}
                onChange={(e) => field.onChange(parseInt(e.target.value) || 0)}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <Timezone />

      <div className="space-y-4">
        <FormLabel>{t("maintenance.form.effective_date_range")}</FormLabel>
        <StartEndDateTime />
      </div>
    </>
  );
};

export default CronExpressionForm;
