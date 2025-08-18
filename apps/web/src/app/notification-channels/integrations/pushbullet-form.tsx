
import { useFormContext } from "react-hook-form";
import { z } from "zod";

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("pushbullet"),
  pushbullet_access_token: z.string().min(1, {
    message: "Access Token is required",
  }),
  pushbullet_device_id: z.string().optional(),
  pushbullet_channel_tag: z.string().optional(),
  pushbullet_custom_template: z.string().optional(),
});

export type PushbulletFormValues = z.infer<typeof schema>;

export const defaultValues: PushbulletFormValues = {
  type: "pushbullet",
  pushbullet_access_token: "",
  pushbullet_device_id: "",
  pushbullet_channel_tag: "",
  pushbullet_custom_template: "{{ msg }}\n\nMonitor: {{ name }}\nStatus: {{ status }}\nTime: {{ heartbeat.time }}",
};

export const displayName = "Pushbullet";

export default function PushbulletForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <div className="space-y-6">
      <FormField
        control={form.control}
        name="pushbullet_access_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushbullet.access_token")}</FormLabel>
            <FormControl>
              <Input
                type="password"
                placeholder="o.XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
                autoComplete="new-password"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.pushbullet.access_token_description")}{" "}
              <a
                href="https://www.pushbullet.com/#settings/account"
                target="_blank"
                rel="noopener noreferrer"
                className="underline text-primary"
              >
                {t("notifications.form.pushbullet.access_token_link")}
              </a>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushbullet_device_id"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushbullet.device_id")}</FormLabel>
            <FormControl>
              <Input
                placeholder="ujpah72o0sjAoRtnM0jc"
                {...field}
                value={field.value ?? ""}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.pushbullet.device_id_description")}{" "}
              <a
                href="https://docs.pushbullet.com/#list-devices"
                target="_blank"
                rel="noopener noreferrer"
                className="underline text-primary"
              >
                {t("notifications.form.pushbullet.device_id_link")}
              </a>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushbullet_channel_tag"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushbullet.channel_tag")}</FormLabel>
            <FormControl>
              <Input
                placeholder="my-channel"
                {...field}
                value={field.value ?? ""}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.pushbullet.channel_tag_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushbullet_custom_template"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushbullet.custom_message_template")}</FormLabel>
            <FormDescription>
              {t("notifications.form.pushbullet.custom_message_template_description")}
            </FormDescription>
            <div className="space-y-1 text-xs text-muted-foreground mb-2">
              <div className="grid grid-cols-2 gap-2">
                <div><code className="text-pink-500">{"{{ msg }}"}</code> - {t("notifications.form.pushbullet.custom_message_template_msg")}</div>
                <div><code className="text-pink-500">{"{{ name }}"}</code> - {t("notifications.form.pushbullet.custom_message_template_name")}</div>
                <div><code className="text-pink-500">{"{{ status }}"}</code> - {t("notifications.form.pushbullet.custom_message_template_status")}</div>
                <div><code className="text-pink-500">{"{{ monitor.url }}"}</code> - {t("notifications.form.pushbullet.custom_message_template_monitor_url")}</div>
                <div><code className="text-pink-500">{"{{ heartbeat.ping }}"}</code> - {t("notifications.form.pushbullet.custom_message_template_heartbeat_ping")}</div>
                <div><code className="text-pink-500">{"{{ heartbeat.time }}"}</code> - {t("notifications.form.pushbullet.custom_message_template_heartbeat_time")}</div>
              </div>
            </div>
            <FormControl>
              <Textarea
                placeholder={t("notifications.form.pushbullet.custom_message_template_placeholder")}
                rows={4}
                {...field}
                value={field.value ?? ""}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="text-sm text-muted-foreground">
        {t("notifications.form.pushbullet.more_info_on")}{" "}
        <a
          href="https://docs.pushbullet.com"
          target="_blank"
          rel="noopener noreferrer"
          className="underline"
        >
          https://docs.pushbullet.com
        </a>
      </div>
    </div>
  );
}