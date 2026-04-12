import type { MonitorMonitorResponseDto, MonitorCreateUpdateDto } from "@/api";
import { z } from "zod";
import { toast } from "sonner";
import { generalSchema, generalDefaultValues } from "../shared/general";
import { intervalsSchema, intervalsDefaultValues } from "../shared/intervals";
import { notificationsSchema, notificationsDefaultValues } from "../shared/notifications";
import { tagsSchema, tagsDefaultValues } from "../shared/tags";

interface SMTPConfig {
  host: string;
  port: number;
  from_email?: string;
  rcpt_to_email?: string;
  use_tls: boolean;
  use_direct_tls?: boolean;
  ignore_tls_errors: boolean;
  check_cert_expiry: boolean;
  read_timeout?: number;
  username?: string;
  password?: string;
  test_open_relay?: boolean;
  expect_secure_relay?: boolean;
}

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
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type SMTPForm = z.infer<typeof smtpSchema>;

export const smtpDefaultValues: SMTPForm = {
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
  expect_secure_relay: false,
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

// Validation schema for backend config responses
const smtpConfigSchema = z.object({
  host: z.string(),
  port: z.number().min(1).max(65535),
  from_email: z.string().optional(),
  rcpt_to_email: z.string().optional(),
  use_tls: z.boolean(),
  use_direct_tls: z.boolean().optional(),
  ignore_tls_errors: z.boolean(),
  check_cert_expiry: z.boolean(),
  read_timeout: z.number().optional(),
  username: z.string().optional(),
  // password should never be returned from backend
  test_open_relay: z.boolean().optional(),
  expect_secure_relay: z.boolean().optional(),
});

export const deserialize = (data: MonitorMonitorResponseDto): SMTPForm => {
  let config: Partial<SMTPConfig> = {
    host: "smtp.example.com",
    port: 587,
    use_tls: true,
    ignore_tls_errors: false,
    check_cert_expiry: true,
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      
      // Validate the parsed config against expected schema
      const validatedConfig = smtpConfigSchema.parse(parsedConfig);
      
      config = {
        host: validatedConfig.host || "smtp.example.com",
        port: validatedConfig.port ?? 587,
        from_email: validatedConfig.from_email || "",
        rcpt_to_email: validatedConfig.rcpt_to_email || "",
        use_tls: validatedConfig.use_tls ?? true,
        use_direct_tls: validatedConfig.use_direct_tls ?? false,
        ignore_tls_errors: validatedConfig.ignore_tls_errors ?? false,
        check_cert_expiry: validatedConfig.check_cert_expiry ?? true,
        read_timeout: validatedConfig.read_timeout,
        username: validatedConfig.username || "",
        password: "", // Never return password from backend for security
        test_open_relay: validatedConfig.test_open_relay ?? false,
        expect_secure_relay: validatedConfig.expect_secure_relay ?? false,
      };
    } catch (error) {
      console.error("Failed to parse or validate SMTP monitor config:", error);
      // Show user-friendly error message
      toast.error("Failed to load monitor configuration. Using default values.");
      // In production, you might want to report this to error tracking service
    }
  }

  return {
    type: "smtp",
    name: data.name || "My SMTP Monitor",
    host: config.host!,
    port: config.port!,
    from_email: config.from_email || "",
    rcpt_to_email: config.rcpt_to_email || "",
    use_tls: config.use_tls!,
    use_direct_tls: config.use_direct_tls ?? false,
    ignore_tls_errors: config.ignore_tls_errors!,
    check_cert_expiry: config.check_cert_expiry!,
    read_timeout: config.read_timeout,
    username: config.username || "",
    password: config.password || "",
    test_open_relay: config.test_open_relay ?? false,
    expect_secure_relay: config.expect_secure_relay ?? false,
    interval: data.interval || 60,
    timeout: data.timeout || 10,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
  };
};

export const serialize = (formData: SMTPForm): MonitorCreateUpdateDto => {
  const config: SMTPConfig = {
    host: formData.host,
    port: formData.port,
    use_tls: formData.use_tls,
    ignore_tls_errors: formData.ignore_tls_errors,
    check_cert_expiry: formData.check_cert_expiry,
  };

  // Only include optional fields if they have values
  if (formData.from_email) {
    config.from_email = formData.from_email;
  }
  if (formData.rcpt_to_email) {
    config.rcpt_to_email = formData.rcpt_to_email;
  }
  if (formData.read_timeout) {
    config.read_timeout = formData.read_timeout;
  }
  if (formData.username) {
    config.username = formData.username;
  }
  // Only send password if user has entered a new one (security: don't send empty string)
  if (formData.password && formData.password.trim() !== "") {
    config.password = formData.password;
  }
  // Always include boolean values explicitly to avoid ambiguity
  config.use_direct_tls = formData.use_direct_tls ?? false;
  config.test_open_relay = formData.test_open_relay ?? false;
  config.expect_secure_relay = formData.expect_secure_relay ?? false;

  return {
    type: "smtp",
    name: formData.name,
    interval: formData.interval,
    max_retries: formData.max_retries,
    retry_interval: formData.retry_interval,
    notification_ids: formData.notification_ids,
    resend_interval: formData.resend_interval,
    timeout: formData.timeout,
    config: JSON.stringify(config),
    tag_ids: formData.tag_ids,
  };
};
