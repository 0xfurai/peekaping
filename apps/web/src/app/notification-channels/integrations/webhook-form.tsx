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
import { Select, SelectTrigger, SelectContent, SelectItem, SelectValue } from "@/components/ui/select";
import { useFormContext } from "react-hook-form";
import * as React from "react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const schema = z.object({
  type: z.literal("webhook"),
  webhook_url: z.string().url({ message: "Valid URL is required" }),
  webhook_content_type: z.enum(["json", "form-data", "custom"]),
  webhook_custom_body: z.string().optional(),
  webhook_additional_headers: z.string().optional(),
});

export type WebhookFormValues = z.infer<typeof schema>;

export const defaultValues: WebhookFormValues = {
  type: "webhook",
  webhook_url: "https://example.com/webhook",
  webhook_content_type: "json",
  webhook_custom_body: `{
    "Title": "Uptime Alert - {{ monitor.name }}",
    "Body": "{{ msg }}"
}`,
  webhook_additional_headers: "",
};

export const displayName = "Webhook";

export default function WebhookForm() {
  const form = useFormContext();
  const contentType = form.watch("webhook_content_type");
  const { t } = useLocalizedTranslation();
  const [showAdditionalHeaders, setShowAdditionalHeaders] = React.useState(
    !!form.getValues("webhook_additional_headers")
  );

  React.useEffect(() => {
    if (!showAdditionalHeaders) {
      form.setValue("webhook_additional_headers", "");
    }
  }, [showAdditionalHeaders, form]);

  const headersPlaceholder = `Example:
{
    "Authorization": "Bearer your-token-here",
    "Content-Type": "application/json"
}`;

  const customBodyPlaceholder = `Example:
{
    "Title": "Uptime Alert - {{ monitor.name }}",
    "Body": "{{ msg }}",
    "Status": "{{ status }}",
    "Timestamp": "{{ timestamp }}"
}`;

  return (
    <>
      <FormField
        control={form.control}
        name="webhook_url"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.webhook.post_url_label")}</FormLabel>
            <FormControl>
              <Input
                placeholder="https://example.com/webhook"
                type="url"
                required
                {...field}
              />
            </FormControl>
            <FormDescription>
              {t("notifications.form.webhook.post_url_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="webhook_content_type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("notifications.form.webhook.request_body_label")}</FormLabel>
            <Select onValueChange={field.onChange} value={field.value}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.form.webhook.request_body_placeholder")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="json">application/json</SelectItem>
                <SelectItem value="form-data">multipart/form-data</SelectItem>
                <SelectItem value="custom">Custom</SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {contentType === "json" && (
                <>{t("notifications.form.webhook.request_body_description_json")}</>
              )}
              {contentType === "form-data" && (
                <>
                  {t("notifications.form.webhook.request_body_description_form_data")}
                </>
              )}
              {contentType === "custom" && (
                <>{t("notifications.form.webhook.request_body_description_custom")}</>
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {contentType === "custom" && (
        <FormField
          control={form.control}
          name="webhook_custom_body"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("notifications.form.webhook.custom_body_label")}</FormLabel>
              <FormControl>
                <Textarea
                  placeholder={customBodyPlaceholder}
                  className="min-h-[200px] font-mono text-sm"
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t("notifications.form.webhook.custom_body_description")}:
                <code className="text-pink-500 ml-1">{"{{ msg }}"}</code>,{" "}
                <code className="text-pink-500">{"{{ monitor.name }}"}</code>,{" "}
                <code className="text-pink-500">{"{{ status }}"}</code>,{" "}
                <code className="text-pink-500">{"{{ timestamp }}"}</code>
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      <FormField
        control={form.control}
        name="webhook_additional_headers"
        render={({ field }) => (
          <FormItem>
            <div className="flex items-center gap-2 mb-2">
              <Switch
                checked={showAdditionalHeaders}
                onCheckedChange={setShowAdditionalHeaders}
              />
              <FormLabel>{t("notifications.form.webhook.additional_headers_label")}</FormLabel>
            </div>
            <FormDescription>
              {t("notifications.form.webhook.additional_headers_description")}
            </FormDescription>
            {showAdditionalHeaders && (
              <FormControl>
                <Textarea
                  placeholder={headersPlaceholder}
                  className="min-h-[150px] font-mono text-sm"
                  {...field}
                />
              </FormControl>
            )}
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}
