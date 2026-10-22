import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { TypographyH4 } from "@/components/ui/typography";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const Intervals = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();

  return (
    <>
      <TypographyH4>{t("ui.intervals_retries")}</TypographyH4>

      <FormField
        control={form.control}
        name="interval"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("forms.labels.heartbeat_interval")}</FormLabel>
            <FormControl>
              <Input placeholder="60" {...field} type="number" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="max_retries"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("forms.labels.retries")}</FormLabel>
            <FormControl>
              <Input placeholder="60" {...field} type="number" />
            </FormControl>

            <FormDescription>
              {t("monitors.form.shared.intervals.max_retries_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="retry_interval"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("monitors.form.shared.intervals.heartbeat_retry", { seconds: field.value || 60 })}
            </FormLabel>
            <FormControl>
              <Input placeholder="60" {...field} type="number" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="timeout"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.shared.intervals.request_timeout", { seconds: field.value || 48 })}</FormLabel>
            <FormControl>
              <Input placeholder="60" {...field} type="number" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="resend_interval"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("monitors.form.shared.intervals.resend_notification")}
            </FormLabel>
            <FormControl>
              <Input placeholder="60" {...field} type="number" />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

export default Intervals;
