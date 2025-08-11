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
import {
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
} from "@/components/ui/select";
import { useFormContext } from "react-hook-form";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { InfoIcon } from "lucide-react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("discord"),
  webhook_url: z.string().url({ message: "Valid webhook URL is required" }),
  bot_display_name: z.string().min(1, { message: "Bot display name is required" }),
  custom_message_prefix: z.string().optional(),
  message_type: z.enum(["send_to_channel", "send_to_new_forum_post", "send_to_thread"], { message: "Message type is required" }),
  thread_name: z.string().optional(),
  thread_id: z.string().optional(),
});

export type DiscordFormValues = z.infer<typeof schema>;

export const defaultValues: DiscordFormValues = {
  type: "discord",
  webhook_url: "",
  bot_display_name: "Peekaping",
  custom_message_prefix: "",
  message_type: "send_to_channel",
  thread_name: "",
  thread_id: "",
};

export const displayName = "Discord";

export default function DiscordForm() {
  const form = useFormContext();
  const messageType = form.watch("message_type");
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="webhook_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.discord.webhook_url_label")}
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://discord.com/api/webhooks/..."
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
                <Alert>
                  <InfoIcon className="mr-2 h-4 w-4"/>
                  <AlertTitle className="font-bold">{t("notifications.form.discord.setup_webhook_title")}</AlertTitle>
                  <AlertDescription>
                    <ul className="list-inside list-disc text-sm mt-2">
                      <li>{t("notifications.form.discord.setup_webhook_description_1")}</li>
                      <li>{t("notifications.form.discord.setup_webhook_description_2")}</li>
                    </ul>
                  </AlertDescription>
                </Alert>
              </FormDescription>
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="bot_display_name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.discord.bot_display_name_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="Peekaping"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.discord.bot_display_name_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="custom_message_prefix"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.discord.custom_message_prefix_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder={t("notifications.form.discord.custom_message_prefix_placeholder")}
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.discord.custom_message_prefix_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="message_type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.discord.sending_message_to_label")}</FormLabel>
            <Select onValueChange={field.onChange} value={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.discord.sending_message_to_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="send_to_channel">{t("notifications.form.discord.sending_message_to_channel")}</SelectItem>
                <SelectItem value="send_to_new_forum_post">{t("notifications.form.discord.sending_message_to_new_forum_post")}</SelectItem>
                <SelectItem value="send_to_thread">{t("notifications.form.discord.sending_message_to_thread")}</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.discord.sending_message_to_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {messageType === "send_to_new_forum_post" && (
        <FormField
          control={form.control}
          name="thread_name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("notifications.form.discord.forum_post_name_label")}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t("notifications.form.discord.forum_post_name_placeholder")}
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t("notifications.form.discord.forum_post_name_description")}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      {messageType === "send_to_thread" && (
        <FormField
          control={form.control}
          name="thread_id"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("notifications.form.discord.thread_id_label")}</FormLabel>
              <FormControl>
                <Input
                  placeholder={t("notifications.form.discord.thread_id_placeholder")}
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                <Alert>
                  <InfoIcon className="mr-2 h-4 w-4"/>
                  <AlertTitle className="font-bold">{t("notifications.form.discord.thread_id_description_title")}</AlertTitle>
                  <AlertDescription>
                    <ul className="list-inside list-disc text-sm mt-2">
                      <li>{t("notifications.form.discord.thread_id_description_1")}</li>
                      <li>{t("notifications.form.discord.thread_id_description_2")}</li>
                    </ul>
                  </AlertDescription>
                </Alert>
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </>
  );
}
