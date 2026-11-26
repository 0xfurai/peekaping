import type { MonitorMonitorResponseDto, MonitorCreateUpdateDto } from "@/api";
import { toast } from "sonner";
import { formatHostPort, getNumber, getString, textTarget, type MonitorConfig, type MonitorTarget } from "../monitor-target";
import { type SmtpFormValues, smtpConfigSchema } from "./schema";
import { deserializeMonitorBase, serializeMonitorBase } from "../shared/codec";

interface SmtpConfig {
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

export const formatTarget = (config: MonitorConfig): MonitorTarget | null =>
  textTarget(formatHostPort(getString(config, "host"), getNumber(config, "port")));

export const deserialize = (data: MonitorMonitorResponseDto): SmtpFormValues => {
  let config: Partial<SmtpConfig> = {
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
        use_tls: validatedConfig.use_tls ?? false,
        use_direct_tls: validatedConfig.use_direct_tls ?? false,
        ignore_tls_errors: validatedConfig.ignore_tls_errors ?? false,
        check_cert_expiry: validatedConfig.check_cert_expiry ?? false,
        read_timeout: validatedConfig.read_timeout,
        username: validatedConfig.username || "",
        // Keep the stored password: an update replaces the whole config
        password: validatedConfig.password || "",
        test_open_relay: validatedConfig.test_open_relay ?? false,
        expect_secure_relay: validatedConfig.expect_secure_relay ?? true,
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
    ...deserializeMonitorBase(data),
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
    expect_secure_relay: config.expect_secure_relay ?? true,
  };
};

export const serialize = (values: SmtpFormValues): MonitorCreateUpdateDto => {
  const config: SmtpConfig = {
    host: values.host,
    port: values.port,
    use_tls: values.use_tls,
    ignore_tls_errors: values.ignore_tls_errors,
    check_cert_expiry: values.check_cert_expiry,
  };

  // Only include optional fields if they have values
  if (values.from_email) {
    config.from_email = values.from_email;
  }
  if (values.rcpt_to_email) {
    config.rcpt_to_email = values.rcpt_to_email;
  }
  if (values.read_timeout) {
    config.read_timeout = values.read_timeout;
  }
  if (values.username) {
    config.username = values.username;
  }
  if (values.password && values.password.trim() !== "") {
    config.password = values.password;
  }
  // Always include boolean values explicitly to avoid ambiguity
  config.use_direct_tls = values.use_direct_tls ?? false;
  config.test_open_relay = values.test_open_relay ?? false;
  config.expect_secure_relay = values.expect_secure_relay ?? true;

  return {
    type: "smtp",
    ...serializeMonitorBase(values),
    config: JSON.stringify(config),
  };
};
