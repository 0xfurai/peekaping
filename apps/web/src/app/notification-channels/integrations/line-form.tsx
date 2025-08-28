import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
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
  type: z.literal("line"),
  channel_access_token: z.string().min(1, { message: "Channel access token is required" }),
  user_id: z.string().min(1, { message: "User ID is required" }),
  template: z.string().optional(),
});

export type LineFormValues = z.infer<typeof schema>;

export const defaultValues: LineFormValues = {
  type: "line",
  channel_access_token: "",
  user_id: "",
  template: `Peekaping Alert - {{ monitor.name }}

Status: {{ status }}
{{ msg }}`,
};

export const displayName = "LINE messaging";

export default function LineForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="channel_access_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.line.channel_access_token_label")}</FormLabel>
            <FormControl>
              <PasswordInput
                placeholder={t("notifications.form.line.channel_access_token_placeholder")}
                autoComplete="new-password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.line.channel_access_token_description")}{" "}
              <a
                href="https://developers.line.biz/console/"
                target="_blank"
                rel="noopener noreferrer"
                className="underline"
              >
                LINE Developers Console
              </a>
              . {t("notifications.form.line.channel_access_token_description_2")} <b>Messaging API</b>.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="user_id"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.line.user_id_label")}</FormLabel>
            <FormControl>
              <Input 
                placeholder={t("notifications.form.line.user_id_placeholder")} 
                required 
                {...field} 
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.line.user_id_description")}{" "}
              <a
                href="https://developers.line.biz/console/"
                target="_blank"
                rel="noopener noreferrer"
                className="underline"
              >
                LINE Developers Console
              </a>
              . {t("notifications.form.line.user_id_description_2")} <b>Basic Settings</b>.
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="template"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.line.template_label")}</FormLabel>
            <FormControl>
              <Textarea
                placeholder={t("notifications.form.line.template_placeholder")}
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.line.template_description")}:{" "}
              <code>{"{{ msg }}"}</code>, <code>{"{{ monitor }}"}</code>, <code>{"{{ heartbeat }}"}</code>, <code>{"{{ status }}"}</code>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}