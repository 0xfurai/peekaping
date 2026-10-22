import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const whatsAppSchema = z.object({
  type: z.literal("whatsapp"),
  server_url: z.string().url({ message: "Server URL must be a valid URL" }),
  api_key: withSavedSecret(z.string()).optional(),
  phone_number: z.string().min(1, { message: "Phone number is required" }),
  session: z.string().min(1, { message: "Session is required" }),
  use_template: z.boolean().optional(),
  template: z.string().optional(),
  custom_message: z.string().optional(),
});

export type WhatsAppFormValues = z.infer<typeof whatsAppSchema>;

export const whatsAppDefaultValues: WhatsAppFormValues = {
  type: "whatsapp",
  server_url: "http://localhost:3000",
  api_key: "",
  phone_number: "",
  session: "",
  use_template: false,
  template: `🚨 Peekaping Alert

Monitor: {{ monitor.name }}
Status: {{ status }}
Message: {{ msg }}

Time: {{ heartbeat.created_at }}`,
  custom_message: "",
};
