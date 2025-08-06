import { Input } from "@/components/ui/input";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { z } from "zod";
import { Textarea } from "@/components/ui/textarea";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("signal"),
  signal_url: z.string().url({ message: "Valid Signal API URL is required" }),
  signal_number: z.string().min(1, { message: "Phone number is required" }),
  signal_recipients: z.string().min(1, { message: "Recipients are required" }),
  custom_message: z.string().optional(),
});

export type SignalFormValues = z.infer<typeof schema>;

export const defaultValues: SignalFormValues = {
  type: "signal",
  signal_url: "",
  signal_number: "",
  signal_recipients: "",
  custom_message: "{{ msg }}",
};

export const displayName = "Signal";

export default function SignalForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="signal_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.signal.post_url_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="http://localhost:8080/v2/send"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.signal.post_url_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="signal_number"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.signal.number_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="+1234567890"
                type="text"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.signal.number_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="signal_recipients"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.signal.recipients_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="+1234567890,+0987654321"
                type="text"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.signal.recipients_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="custom_message"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.signal.custom_message_label")}</FormLabel>
            <FormControl>
              <Textarea
                placeholder="Alert: {{ name }} is {{ status }}"
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.signal.custom_message_description")}: {"{{ msg }}"}, {"{{ name }}"}, {"{{ status }}"}, {"{{ monitor.* }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="space-y-4 p-4 bg-amber-50 dark:bg-amber-900/20 rounded-lg border border-amber-200 dark:border-amber-800">
        <p className="text-sm text-amber-800 dark:text-amber-200">
          <strong>{t("notifications.form.signal.note_label")}:</strong> {t("notifications.form.signal.note_description")}
        </p>
        <p className="text-sm text-amber-800 dark:text-amber-200">
          {t("notifications.form.signal.more_info_label")}:
        </p>
        <p className="text-sm text-amber-800 dark:text-amber-200">
          <a
            href="https://github.com/bbernhard/signal-cli-rest-api"
            target="_blank"
            rel="noopener noreferrer"
            className="underline hover:text-amber-900 dark:hover:text-amber-100"
          >
            https://github.com/bbernhard/signal-cli-rest-api
          </a>
        </p>
        <p className="text-sm text-amber-800 dark:text-amber-200">
          <strong>{t("notifications.form.signal.important_label")}:</strong> {t("notifications.form.signal.important_description")}
        </p>
      </div>
    </>
  );
}
