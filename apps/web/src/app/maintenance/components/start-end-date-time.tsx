import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const StartEndDateTime = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  return (
    <div className="grid grid-cols-2 gap-4 items-start">
      <FormField
        control={form.control}
        name="startDateTime"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("maintenance.form.start_date_time")}</FormLabel>
            <FormControl>
              <Input type="datetime-local" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="endDateTime"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("maintenance.form.end_date_time")}</FormLabel>
            <FormControl>
              <Input type="datetime-local" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  );
};

export default StartEndDateTime;
