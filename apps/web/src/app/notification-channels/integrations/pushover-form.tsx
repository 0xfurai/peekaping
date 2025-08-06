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
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useMemo } from "react";

export const schema = z.object({
  type: z.literal("pushover"),
  pushover_user_key: z.string().min(1, { message: "User key is required" }),
  pushover_app_token: z.string().min(1, { message: "Application token is required" }),
  pushover_device: z.string().optional(),
  pushover_title: z.string().optional(),
  pushover_priority: z.coerce.number().min(-2).max(2).optional(),
  pushover_sounds: z.string().optional(),
  pushover_sounds_up: z.string().optional(),
  pushover_ttl: z.coerce.number().min(0).optional(),
});

export type PushoverFormValues = z.infer<typeof schema>;

export const defaultValues: PushoverFormValues = {
  type: "pushover",
  pushover_user_key: "",
  pushover_app_token: "",
  pushover_device: "",
  pushover_title: "",
  pushover_priority: 0,
  pushover_sounds: "pushover",
  pushover_sounds_up: "pushover",
  pushover_ttl: 0,
};

export const displayName = "Pushover";

export default function PushoverForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  const soundOptions = useMemo(() => [
    { value: "pushover", label: t("notifications.form.pushover.sound_pushover") },
    { value: "bike", label: t("notifications.form.pushover.sound_bike") },
    { value: "bugle", label: t("notifications.form.pushover.sound_bugle") },
    { value: "cashregister", label: t("notifications.form.pushover.sound_cashregister") },
    { value: "classical", label: t("notifications.form.pushover.sound_classical") },
    { value: "cosmic", label: t("notifications.form.pushover.sound_cosmic") },
    { value: "falling", label: t("notifications.form.pushover.sound_falling") },
    { value: "gamelan", label: t("notifications.form.pushover.sound_gamelan") },
    { value: "incoming", label: t("notifications.form.pushover.sound_incoming") },
    { value: "intermission", label: t("notifications.form.pushover.sound_intermission") },
    { value: "magic", label: t("notifications.form.pushover.sound_magic") },
    { value: "mechanical", label: t("notifications.form.pushover.sound_mechanical") },
    { value: "pianobar", label: t("notifications.form.pushover.sound_pianobar") },
    { value: "siren", label: t("notifications.form.pushover.sound_siren") },
    { value: "spacealarm", label: t("notifications.form.pushover.sound_spacealarm") },
    { value: "tugboat", label: t("notifications.form.pushover.sound_tugboat") },
    { value: "alien", label: t("notifications.form.pushover.sound_alien") },
    { value: "climb", label: t("notifications.form.pushover.sound_climb") },
    { value: "persistent", label: t("notifications.form.pushover.sound_persistent") },
    { value: "echo", label: t("notifications.form.pushover.sound_echo") },
    { value: "updown", label: t("notifications.form.pushover.sound_updown") },
    { value: "vibrate", label: t("notifications.form.pushover.sound_vibrate") },
    { value: "none", label: t("notifications.form.pushover.sound_none") },
  ], [t]);

  const priorityOptions = useMemo(() => [
    { value: -2, label: t("notifications.form.pushover.priority_1") },
    { value: -1, label: t("notifications.form.pushover.priority_2") },
    { value: 0, label: t("notifications.form.pushover.priority_3") },
    { value: 1, label: t("notifications.form.pushover.priority_4") },
    { value: 2, label: t("notifications.form.pushover.priority_5") },
  ], [t]);

  return (
    <>
      <FormField
        control={form.control}
        name="pushover_user_key"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.pushover.user_key_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder={t("notifications.form.pushover.user_key_placeholder")}
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_app_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.pushover.application_token_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder={t("notifications.form.pushover.application_token_placeholder")}
                type="password"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_device"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushover.device_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder={t("notifications.form.pushover.device_placeholder")}
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.pushover.device_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_title"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushover.message_title_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder={t("notifications.form.pushover.message_title_placeholder")}
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.pushover.message_title_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_priority"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushover.priority_label")}</FormLabel>
            <Select
              onValueChange={(value) => field.onChange(parseInt(value))}
              value={field.value?.toString()}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.pushover.priority_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {priorityOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value.toString()}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.pushover.priority_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_sounds"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushover.notification_sound_down_label")}</FormLabel>
            <Select
              onValueChange={field.onChange}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.pushover.notification_sound_down_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {soundOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.pushover.notification_sound_down_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_sounds_up"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushover.notification_sound_up_label")}</FormLabel>
            <Select
              onValueChange={field.onChange}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.pushover.notification_sound_up_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {soundOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormDescription>
              {t("notifications.form.pushover.notification_sound_up_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="pushover_ttl"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.pushover.message_ttl_label")}</FormLabel>
            <FormControl>
              <Input
                type="number"
                min="0"
                step="1"
                placeholder="0"
                {...field}
                onChange={(e) => field.onChange(parseInt(e.target.value) || 0)}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.pushover.message_ttl_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="mt-4 p-4 bg-gray-50 rounded-lg">
        <p className="text-sm text-gray-600">
          <span className="text-red-500">*</span> {t("common.required")}
        </p>
        <p className="text-sm text-gray-600 mt-2">
          {t("notifications.form.pushover.more_info_label")}:{" "}
          <a
            href="https://pushover.net/api"
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 underline"
          >
            https://pushover.net/api
          </a>
        </p>
        <p className="text-sm text-gray-600 mt-2">
          {t("notifications.form.pushover.more_info_label_2")}:{" "}
          <a
            href="https://pushover.net/apps/build"
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 underline"
          >
            https://pushover.net/apps/build
          </a>
        </p>
        <p className="text-sm text-gray-600 mt-2">
          {t("notifications.form.pushover.more_info_label_3")}{" "}
          <a
            href="https://pushover.net/"
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 underline"
          >
            https://pushover.net/
          </a>
        </p>
      </div>
    </>
  );
}
