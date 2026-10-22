import { z } from "zod";
import { monitorBaseSchema, monitorBaseDefaultValues } from "../shared/schema";

export const smtpSchema = z
  .object({
    type: z.literal("smtp"),
    host: z.string().min(1, "Host is required"),
    port: z.number().min(1, "Port must be at least 1").max(65535, "Port must be at most 65535"),
    from_email: z.string().email().optional().or(z.literal("")),
    rcpt_to_email: z.string().email().optional().or(z.literal("")),
    use_tls: z.boolean(),
    use_direct_tls: z.boolean().optional(),
    ignore_tls_errors: z.boolean(),
    check_cert_expiry: z.boolean(),
    read_timeout: z.number().min(1).max(300).optional(),
    username: z.string().optional(),
    password: z.string().optional(),
    test_open_relay: z.boolean().optional(),
    expect_secure_relay: z.boolean().optional(),
  })
  .merge(monitorBaseSchema);

export type SmtpFormValues = z.infer<typeof smtpSchema>;

export const smtpDefaultValues: SmtpFormValues = {
  type: "smtp",
  host: "smtp.example.com",
  port: 587,
  from_email: "",
  rcpt_to_email: "",
  use_tls: true,
  use_direct_tls: false,
  ignore_tls_errors: false,
  check_cert_expiry: true,
  read_timeout: undefined,
  username: "",
  password: "",
  test_open_relay: false,
  expect_secure_relay: true,
  ...monitorBaseDefaultValues,
};

// Validation schema for backend config responses
// Booleans are optional because the backend treats a missing one as false,
// except expect_secure_relay, which defaults to true
export const smtpConfigSchema = z.object({
  host: z.string(),
  port: z.number().min(1).max(65535),
  from_email: z.string().optional(),
  rcpt_to_email: z.string().optional(),
  use_tls: z.boolean().optional(),
  use_direct_tls: z.boolean().optional(),
  ignore_tls_errors: z.boolean().optional(),
  check_cert_expiry: z.boolean().optional(),
  read_timeout: z.number().optional(),
  username: z.string().optional(),
  password: z.string().optional(),
  test_open_relay: z.boolean().optional(),
  expect_secure_relay: z.boolean().optional(),
});
