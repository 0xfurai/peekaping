import { Input } from "@/components/ui/input";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { z } from "zod";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("mattermost"),
  webhook_url: z.string().url({ message: "Valid webhook URL is required" }),
  username: z.string().optional(),
  channel: z.string().optional(),
  icon_url: z.union([z.string().url({ message: "Valid icon URL is required" }), z.literal("")]).optional(),
  icon_emoji: z.string().optional(),
  use_template: z.boolean().optional(),
  template: z.string().optional(),
});

export const defaultValues = {
  type: "mattermost" as const,
  webhook_url: "",
  username: "Peekaping",
  channel: "",
  icon_url: "",
  icon_emoji: "",
  use_template: false,
  template: "",
};

export const displayName = "Mattermost";

export default function MattermostForm() {
  const form = useFormContext();
  const useTemplate = form.watch("use_template");
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="webhook_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.mattermost.webhook_url_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://your-mattermost-server.com/hooks/..."
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
              <br />
              <span className="mt-2 block">
                {t("notifications.form.mattermost.learn_more_label")}:{" "}
                <a
                  href="https://developers.mattermost.com/integrate/webhooks/incoming/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline text-blue-600"
                >
                  https://developers.mattermost.com/integrate/webhooks/incoming/
                </a>
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="username"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("forms.labels.username")}</FormLabel>
            <FormControl>
              <Input placeholder="Peekaping" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.mattermost.username_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="channel"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.mattermost.channel_name_label")}</FormLabel>
            <FormControl>
              <Input placeholder="general" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.mattermost.channel_name_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="icon_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.mattermost.icon_url_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="https://example.com/icon.png"
                type="url"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.mattermost.icon_url_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="icon_emoji"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.mattermost.icon_emoji_label")}</FormLabel>
            <FormControl>
              <Input placeholder=":white_check_mark: :x:" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.mattermost.icon_emoji_description")}
              <br />
              <span className="mt-2 block">
                {t("notifications.form.mattermost.icon_emoji_cheat_sheet_label")}:{" "}
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
        name="use_template"
        render={({ field }) => (
          <FormItem>
            <div className="flex items-center gap-2">
              <FormControl>
                <Switch
                  checked={field.value || false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormLabel>{t("notifications.form.mattermost.use_message_template_label")}</FormLabel>
            </div>
            <FormDescription>
              {t("notifications.form.mattermost.use_message_template_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {useTemplate && (
        <FormField
          control={form.control}
          name="template"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("notifications.form.mattermost.message_template_label")}</FormLabel>
              <FormControl>
                <Textarea
                  placeholder="Enter your custom message template"
                  className="min-h-[100px]"
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t("notifications.form.mattermost.message_template_description")}:{" "}
                <code>{"{{ msg }}"}</code>, <code>{"{{ monitor.name }}"}</code>, <code>{"{{ status }}"}</code>, <code>{"{{ monitor.* }}"}</code>
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </>
  );
}
