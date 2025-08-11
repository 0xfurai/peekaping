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
import { Textarea } from "@/components/ui/textarea";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("smtp"),
  smtp_secure: z.boolean(),
  smtp_host: z.string(),
  smtp_port: z.coerce.number().min(1, { message: "Port is required" }),
  username: z.string().min(1, { message: "Username is required" }),
  password: z.string().min(1, { message: "Password is required" }),
  from: z.string().email({ message: "Sender email is required" }),
  to: z.string().min(1, { message: "Recipient(s) required" }),
  cc: z.string().optional(),
  bcc: z.string().optional(),
  custom_subject: z.string().optional(),
  custom_body: z.string().optional(),
});

export type SmtpFormValues = z.infer<typeof schema>;

export const defaultValues: SmtpFormValues = {
  type: "smtp",
  smtp_secure: false,
  smtp_host: "example.com",
  smtp_port: 587,
  username: "username",
  password: "password",
  from: "sender@example.com",
  to: "recipient@example.com",
  cc: "cc@example.com",
  bcc: "bcc@example.com",
  custom_subject: "{{ msg }}",
  custom_body: "{{ msg }}",
};

export const displayName = "Email (SMTP)";

export default function SmtpForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="smtp_host"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.smtp.smtp_host_label")}</FormLabel>
            <FormControl>
              <Input placeholder="example.com" {...field} />
            </FormControl>
            <FormDescription>
              {t("notifications.form.smtp.smtp_host_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <div className="flex space-x-4">
        <FormField
          control={form.control}
          name="smtp_port"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("notifications.form.smtp.smtp_port_label")}</FormLabel>
              <FormControl>
                <Input placeholder="587" type="number" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="smtp_secure"
          render={({ field }) => (
            <FormItem>
              <FormLabel>SSL/TLS</FormLabel>
              <FormControl>
                <Switch
                  checked={field.value || false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
      <FormField
        control={form.control}
        name="username"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("forms.labels.username")}</FormLabel>
            <FormControl>
              <Input placeholder={t("notifications.form.smtp.smtp_username_placeholder")} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="password"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("forms.labels.password")}</FormLabel>
            <FormControl>
              <Input placeholder={t("notifications.form.smtp.smtp_password_placeholder")} type="password" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="from"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.smtp.smtp_from_label")}</FormLabel>
            <FormControl>
              <Input placeholder="sender@example.com" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="to"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.smtp.smtp_to_label")}</FormLabel>
            <FormControl>
              <Input placeholder="recipient@example.com, ..." {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="cc"
        render={({ field }) => (
          <FormItem>
            <FormLabel>CC</FormLabel>
            <FormControl>
              <Input placeholder="cc@example.com, ..." {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="bcc"
        render={({ field }) => (
          <FormItem>
            <FormLabel>BCC</FormLabel>
            <FormControl>
              <Input placeholder="bcc@example.com, ..." {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="custom_subject"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.smtp.custom_subject_label")}</FormLabel>

            <FormDescription>
              {t("notifications.form.smtp.custom_description")}
              <br />
              {t("notifications.form.smtp.custom_description_2")}
              <br />
              <b>{t("notifications.form.smtp.custom_description_3")}</b>
              <span className="block">
                <code className="text-pink-500">{"{{ msg }}"}</code>: {t("notifications.form.smtp.custom_description_4")}
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ name }}"}</code>: {t("notifications.form.smtp.custom_description_5")}
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ status }}"}</code>: {t("notifications.form.smtp.custom_description_6")}
              </span>
            </FormDescription>
            <FormControl>
              <Input placeholder="{{ msg }}" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="custom_body"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.smtp.custom_body_label")}</FormLabel>

            <FormDescription>
              {t("notifications.form.smtp.custom_description")}
              <br />
              {t("notifications.form.smtp.custom_description_2")}
              <br />
              <b>{t("notifications.form.smtp.custom_description_3")}</b>
              <span className="block">
                <code className="text-pink-500">{"{{ msg }}"}</code>: {t("notifications.form.smtp.custom_description_4")}
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ name }}"}</code>: {t("notifications.form.smtp.custom_description_5")}
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ status }}"}</code>: {t("notifications.form.smtp.custom_description_6")}
              </span>
            </FormDescription>
            <FormControl>
              <Textarea placeholder="{{ msg }}" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
