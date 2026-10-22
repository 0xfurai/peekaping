import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const sendGridSchema = z.object({
  type: z.literal("sendgrid"),
  api_key: withSavedSecret(z.string().min(1, { message: "API key is required" })),
  from_email: z.string().email({ message: "Valid sender email is required" }),
  to_email: z.string().min(1, { message: "Recipient email(s) required" }),
  cc_email: z.string().optional(),
  bcc_email: z.string().optional(),
  subject: z.string().optional(),
});

export type SendGridFormValues = z.infer<typeof sendGridSchema>;

export const sendGridDefaultValues: SendGridFormValues = {
  type: "sendgrid",
  api_key: "",
  from_email: "noreply@example.com",
  to_email: "recipient@example.com",
  cc_email: "",
  bcc_email: "",
  subject: "{{ name }} - {{ status }}",
};
