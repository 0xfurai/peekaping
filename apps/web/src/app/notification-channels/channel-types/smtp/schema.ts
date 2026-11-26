import { z } from "zod";
import { withSavedSecret } from "../../secrets";

export const smtpSchema = z.object({
  type: z.literal("smtp"),
  smtp_secure: z.boolean(),
  smtp_host: z.string(),
  smtp_port: z.coerce.number().min(1, { message: "Port is required" }),
  username: z.string().min(1, { message: "Username is required" }),
  password: withSavedSecret(z.string().min(1, { message: "Password is required" })),
  from: z.string().email({ message: "Sender email is required" }),
  to: z.string().min(1, { message: "Recipient(s) required" }),
  cc: z.string().optional(),
  bcc: z.string().optional(),
  custom_subject: z.string().optional(),
  custom_body: z.string().optional(),
});

export type SmtpFormValues = z.infer<typeof smtpSchema>;

export const smtpDefaultValues: SmtpFormValues = {
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
