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
import { Switch } from "@/components/ui/switch";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

// Base schema for discriminated union (without refinements)
export const baseSchema = z.object({
  type: z.literal("slack"),
  slack_webhook_url: z
    .union([z.string().url({ message: "Valid webhook URL is required" }), z.literal("")])
    .optional(),
  slack_bot_token: z.string().optional(),
  slack_username: z.string().optional(),
  slack_icon_emoji: z.string().optional(),
  slack_channel: z.string().optional(),
  slack_rich_message: z.boolean().optional(),
  slack_channel_notify: z.boolean().optional(),
});

// Full schema with refinements for validation
export const schema = baseSchema
  .refine((data) => data.slack_webhook_url || data.slack_bot_token, {
    message: "Either Webhook URL or Bot Token must be provided",
    path: ["slack_webhook_url"],
  })
  .refine((data) => !data.slack_bot_token || data.slack_channel, {
    message: "Channel is required when using Bot Token",
    path: ["slack_channel"],
  });

export type SlackFormValues = z.infer<typeof schema>;

export const defaultValues: SlackFormValues = {
  type: "slack",
  slack_webhook_url: "",
  slack_bot_token: "",
  slack_username: "",
  slack_icon_emoji: "",
  slack_channel: "",
  slack_rich_message: false,
  slack_channel_notify: false,
};

export const displayName = "Slack";

export default function SlackForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="slack_webhook_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.slack.webhook_url_label")}
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://hooks.slack.com/services/..."
                type="url"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.slack.webhook_url_description")}:{" "}
              <a
                href="https://api.slack.com/messaging/webhooks"
                target="_blank"
                rel="noopener noreferrer"
                className="underline text-blue-600"
              >
                https://api.slack.com/messaging/webhooks
              </a>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="slack_bot_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.slack.bot_token_label")}
            </FormLabel>
            <FormControl>
              <Input placeholder="xoxb-..." type="password" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.slack.bot_token_description")}:{" "}
              <a
                href="https://api.slack.com/authentication/token-types#bot"
                target="_blank"
                rel="noopener noreferrer"
                className="underline text-blue-600"
              >
                https://api.slack.com/authentication/token-types#bot
              </a>
              <br />
              <span className="mt-2 block text-amber-600">
                {t("notifications.form.slack.bot_token_note")}
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="slack_username"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("forms.labels.username")}</FormLabel>
            <FormControl>
              <Input placeholder="Uptime Monitor" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.slack.username_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="slack_icon_emoji"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.slack.icon_emoji_label")}
            </FormLabel>
            <FormControl>
              <Input placeholder=":warning:" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.slack.icon_emoji_description")}
              <br />
              <span className="mt-2 block">
                {t("notifications.form.slack.icon_emoji_cheat_sheet_label")}:{" "}
                <a
                  href="https://www.webfx.com/tools/emoji-cheat-sheet/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  https://www.webfx.com/tools/emoji-cheat-sheet/
                </a>
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="slack_channel"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.slack.channel_name_label")}
            </FormLabel>
            <FormControl>
              <Input placeholder="#general" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.slack.channel_name_description")}
              <br />
              <span className="mt-2 block">
                {t("notifications.form.slack.channel_name_description_2")}
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="slack_rich_message"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.slack.message_format_label")}
            </FormLabel>
            <div className="flex items-center gap-2 mt-2">
              <FormControl>
                <Switch
                  checked={field.value || false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel className="text-sm font-normal">
                {t("notifications.form.slack.message_format_description")}
              </FormLabel>
            </div>
            <FormDescription>
              {t("notifications.form.slack.message_format_description_2")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="slack_channel_notify"
        render={({ field }) => (
          <FormItem>
            <div className="flex items-center gap-2">
              <FormControl>
                <Switch
                  checked={field.value || false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel>
                {t("notifications.form.slack.channel_notify_label")}
              </FormLabel>
            </div>
            <FormDescription>
              {t("notifications.form.slack.channel_notify_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
