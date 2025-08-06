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
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("gotify"),
  server_url: z.string().url({ message: "Valid server URL is required" }),
  application_token: z.string().min(1, { message: "Application token is required" }),
  priority: z.coerce.number().min(0).max(10).optional(),
  title: z.string().optional(),
  custom_message: z.string().optional(),
});

export const defaultValues = {
  type: "gotify" as const,
  server_url: "",
  application_token: "",
  priority: 8,
  title: "",
  custom_message: "",
};

export const displayName = "Gotify";

export default function GotifyForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="server_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.gotify.server_url_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="https://gotify.yourdomain.com"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.gotify.server_url_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="application_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.gotify.application_token_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.gotify.application_token_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="priority"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.gotify.priority_label")}</FormLabel>
            <Select
              onValueChange={(val) => {
                if (!val) {
                  return;
                }
                field.onChange(parseInt(val));
              }}
              value={field.value?.toString()}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.gotify.priority_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="0">0 - {t("notifications.form.gotify.priority_0")}</SelectItem>
                <SelectItem value="1">1 - {t("notifications.form.gotify.priority_1")}</SelectItem>
                <SelectItem value="2">2 - {t("notifications.form.gotify.priority_2")}</SelectItem>
                <SelectItem value="3">3 - {t("notifications.form.gotify.priority_3")}</SelectItem>
                <SelectItem value="4">4 - {t("notifications.form.gotify.priority_4")}</SelectItem>
                <SelectItem value="5">5 - {t("notifications.form.gotify.priority_5")}</SelectItem>
                <SelectItem value="6">6 - {t("notifications.form.gotify.priority_6")}</SelectItem>
                <SelectItem value="7">7 - {t("notifications.form.gotify.priority_7")}</SelectItem>
                <SelectItem value="8">8 - {t("notifications.form.gotify.priority_8")}</SelectItem>
                <SelectItem value="9">9 - {t("notifications.form.gotify.priority_9")}</SelectItem>
                <SelectItem value="10">10 - {t("notifications.form.gotify.priority_10")}</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.gotify.priority_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="title"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.gotify.custom_title_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="Peekaping Alert - {{ monitor.name }}"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.gotify.custom_title_description")}
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
            <FormLabel>{t("notifications.form.gotify.custom_message_label")}</FormLabel>
            <FormControl>
              <Textarea
                placeholder="Alert: {{ monitor.name }} is {{ status }} - {{ msg }}"
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t('notifications.form.gotify.custom_message_description')}: {"{{ msg }}"}, {"{{ monitor.name }}"}, {"{{ status }}"}, {"{{ heartbeat.* }}"}.
              {t('notifications.form.gotify.custom_message_description_2')}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="space-y-4 p-4 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800">
        <p className="text-sm text-blue-800 dark:text-blue-200">
          <strong>{t("notifications.form.gotify.note_label")}</strong> {t("notifications.form.gotify.note_description")}
        </p>
        <p className="text-sm text-blue-800 dark:text-blue-200">
          {t("notifications.form.gotify.learn_more_label")}:
        </p>
        <p className="text-sm text-blue-800 dark:text-blue-200">
          <a
            href="https://gotify.net/"
            target="_blank"
            rel="noopener noreferrer"
            className="underline hover:text-blue-900 dark:hover:text-blue-100"
          >
            https://gotify.net/
          </a>
        </p>
      </div>
    </>
  );
}
