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
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { PasswordInput } from "@/components/ui/password-input";

export const schema = z.object({
  type: z.literal("sendgrid"),
  api_key: z.string().min(1, { message: "API key is required" }),
  from_email: z.string().email({ message: "Valid sender email is required" }),
  to_email: z.string().min(1, { message: "Recipient email(s) required" }),
  cc_email: z.string().optional(),
  bcc_email: z.string().optional(),
  subject: z.string().optional(),
});

export type SendGridFormValues = z.infer<typeof schema>;

export const defaultValues: SendGridFormValues = {
  type: "sendgrid",
  api_key: "",
  from_email: "noreply@example.com",
  to_email: "recipient@example.com",
  cc_email: "",
  bcc_email: "",
  subject: "{{ name }} - {{ status }}",
};

export const displayName = "SendGrid";

export default function SendGridForm() {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  return (
    <>
      <FormField
        control={form.control}
        name="api_key"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("sendgrid.api_key_label")}</FormLabel>
            <FormControl>
              <PasswordInput
                placeholder="SG.xxxxxxxxxxxxxxxxxxxx"
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("sendgrid.api_key_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="from_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("sendgrid.from_email_label")}</FormLabel>
            <FormControl>
              <Input placeholder="noreply@example.com" {...field} />
            </FormControl>
            <FormDescription>
              {t("sendgrid.from_email_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="to_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("sendgrid.to_email_label")}</FormLabel>
            <FormControl>
              <Input placeholder="recipient@example.com" {...field} />
            </FormControl>
            <FormDescription>
              {t("sendgrid.to_email_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="cc_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("sendgrid.cc_email_label")}</FormLabel>
            <FormControl>
              <Input placeholder="cc1@example.com, cc2@example.com" {...field} />
            </FormControl>
            <FormDescription>
              {t("sendgrid.cc_email_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="bcc_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("sendgrid.bcc_email_label")}</FormLabel>
            <FormControl>
              <Input placeholder="bcc1@example.com, bcc2@example.com" {...field} />
            </FormControl>
            <FormDescription>
              {t("sendgrid.bcc_email_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="subject"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("sendgrid.subject_label")}</FormLabel>
            <FormControl>
              <Input placeholder="{{ name }} - {{ status }}" {...field} />
            </FormControl>
            <FormDescription>
              {t("sendgrid.subject_description")}
              <br />
              <b>{t("sendgrid.subject_variables_label")}</b>
              <span className="block">
                <code className="text-pink-500">{"{{ msg }}"}</code>: {t("sendgrid.subject_variables_msg")}
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ name }}"}</code>: {t("sendgrid.subject_variables_name")}
              </span>
              <span className="block">
                <code className="text-pink-500">{"{{ status }}"}</code>: {t("sendgrid.subject_variables_status")}
              </span>
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}