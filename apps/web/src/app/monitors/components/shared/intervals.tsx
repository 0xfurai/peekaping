import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { TypographyH4 } from "@/components/ui/typography";
import { useFormContext } from "react-hook-form";
import { z } from "zod";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const intervalsSchema = z.object({
  interval: z.coerce.number().min(20),
  max_retries: z.coerce.number().min(0),
  retry_interval: z.coerce.number().min(20),
  timeout: z.coerce.number().min(16),
  resend_interval: z.coerce.number().min(0),
});

export type IntervalsForm = z.infer<typeof intervalsSchema>;

export const intervalsDefaultValues: IntervalsForm = {
  interval: 20,
  max_retries: 0,
  retry_interval: 20,
  timeout: 16,
  resend_interval: 0,
};

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
              Maximum retries before the service is marked as down and a
              notification is sent
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
              Heartbeat Retry Interval (Retry every 60 seconds)
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
            <FormLabel>Request Timeout (Timeout after 48 seconds)</FormLabel>
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
              Resend Notification if Down X times consecutively (Resend
              disabled)
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
