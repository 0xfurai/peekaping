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
  type: z.literal("matrix"),
  homeserver_url: z.string().url({ message: "Valid homeserver URL is required" }),
  internal_room_id: z.string().min(1, { message: "Internal Room ID is required" }),
  access_token: z.string().min(1, { message: "Access Token is required" }),
  custom_message: z.string().optional(),
});

export const defaultValues = {
  type: "matrix" as const,
  homeserver_url: "",
  internal_room_id: "",
  access_token: "",
  custom_message: "{{ msg }}",
};

export const displayName = "Matrix";

export default function MatrixForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="homeserver_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.matrix.homeserver_url_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="https://matrix.org"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
              <br />
              {t("notifications.form.matrix.homeserver_url_description")} (e.g., https://matrix.org)
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="internal_room_id"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.matrix.internal_room_id_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <Input
                placeholder="!roomid:matrix.org"
                type="text"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
              <br />
              {t("notifications.form.matrix.internal_room_id_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="access_token"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("notifications.form.matrix.access_token_label")} <span className="text-red-500">*</span>
            </FormLabel>
            <FormControl>
              <PasswordInput
                placeholder="Your Matrix access token"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              <span className="text-red-500">*</span> {t("common.required")}
              <br />
              {t("notifications.form.matrix.access_token_description")}
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
            <FormLabel>{t("notifications.form.matrix.custom_message_label")}</FormLabel>
            <FormControl>
              <Textarea
                placeholder="Alert: {{ monitor.name }} is {{ status }}"
                className="min-h-[100px]"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.matrix.custom_message_description")}: {"{{ msg }}"}, {"{{ monitor.name }}"}, {"{{ status }}"}, {"{{ heartbeat.* }}"}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <div className="space-y-4 p-4 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800">
        <p className="text-sm text-blue-800 dark:text-blue-200">
          <strong>{t("notifications.form.matrix.setup_instructions_label")}</strong>
        </p>
        <div className="space-y-2 text-sm text-blue-800 dark:text-blue-200">
          <p>
            1. {t("notifications.form.matrix.setup_instructions_1")}
          </p>
          <p>
            2. {t("notifications.form.matrix.setup_instructions_2")}
          </p>
          <p>
            3. {t("notifications.form.matrix.setup_instructions_3")}:
          </p>
          <div className="bg-blue-100 dark:bg-blue-800 p-2 rounded font-mono text-xs overflow-x-auto">
            <code>
              curl -XPOST -d '{`"type": "m.login.password", "identifier": {"user": "botusername", "type": "m.id.user"}, "password": "passwordforuser"`}' "https://home.server/_matrix/client/v3/login"
            </code>
          </div>
          <p>
            4. {t("notifications.form.matrix.setup_instructions_4")}
          </p>
          <p>
            5. {t("notifications.form.matrix.setup_instructions_5")}
          </p>
        </div>
      </div>
    </>
  );
}
