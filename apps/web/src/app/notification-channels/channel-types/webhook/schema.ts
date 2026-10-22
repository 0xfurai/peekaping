import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const webhookSchema = z.object({
  type: z.literal("webhook"),
  webhook_url: z.string().url({ message: "Valid URL is required" }),
  webhook_content_type: z.enum(["json", "form-data", "custom"]),
  webhook_custom_body: z.string().optional(),
  webhook_additional_headers: withSavedSecret(z.string()).optional(),
  webhook_signing_secret: withSavedSecret(z.string()).optional(),
});

export type WebhookFormValues = z.infer<typeof webhookSchema>;

export const webhookDefaultValues: WebhookFormValues = {
  type: "webhook",
  webhook_url: "https://example.com/webhook",
  webhook_content_type: "json",
  webhook_custom_body: `{
    "Title": "Uptime Alert - {{ monitor.name }}",
    "Body": "{{ msg }}"
}`,
  webhook_additional_headers: "",
  webhook_signing_secret: "",
};
