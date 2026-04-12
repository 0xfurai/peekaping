import { TypographyH4 } from "@/components/ui/typography";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { useEffect } from "react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const SMTPSettings = () => {
  const { t } = useLocalizedTranslation();
  const { form } = useMonitorFormContext();

  const useTls = form.watch("use_tls");
  const useDirectTls = form.watch("use_direct_tls") ?? false;
  const port = form.watch("port");

  // Show warning if port 465 is used without direct TLS
  // Port 465 (SMTPS) typically requires direct TLS, not STARTTLS
  const showPort465Warning = port === 465 && !useDirectTls;

  // Handle TLS checkbox dependencies
  // Note: Mutual exclusivity is already handled in Checkbox onChange handlers below
  useEffect(() => {
    // Disable dependent checkboxes when both TLS options are off
    if (!useTls && !useDirectTls) {
      const ignoreTlsErrors = form.getValues("ignore_tls_errors");
      const checkCertExpiry = form.getValues("check_cert_expiry");
      if (ignoreTlsErrors) {
        form.setValue("ignore_tls_errors", false);
      }
      if (checkCertExpiry) {
        form.setValue("check_cert_expiry", false);
      }
    }
  }, [useTls, useDirectTls, form]);

  return (
    <>
      <TypographyH4>{t("monitors.form.smtp.title")}</TypographyH4>

      <FormField
        control={form.control}
        name="host"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.smtp.host")}</FormLabel>
            <FormControl>
              <Input placeholder="smtp.example.com" {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="port"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.smtp.port")}</FormLabel>
            <FormControl>
              <Input
                type="number"
                min="1"
                max="65535"
                placeholder="587"
                {...field}
                onChange={(e) => {
                  const value = e.target.value;
                  if (value === "") {
                    field.onChange(1);
                    return;
                  }
                  const parsed = parseInt(value, 10);
                  if (!isNaN(parsed) && parsed >= 1 && parsed <= 65535) {
                    field.onChange(parsed);
                  }
                  // Else: don't update (reject invalid input)
                }}
              />
            </FormControl>
            <FormDescription
              className={showPort465Warning ? "text-yellow-800 dark:text-yellow-300 font-medium" : ""}
            >
              {showPort465Warning
                ? t("monitors.form.smtp.port_465_warning")
                : t("monitors.form.smtp.port_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {/* Read Timeout */}
      <FormField
        control={form.control}
        name="read_timeout"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.smtp.read_timeout")}</FormLabel>
            <FormControl>
              <Input
                type="number"
                min="1"
                max="300"
                placeholder="10"
                {...field}
                value={field.value || ""}
                onChange={(e) => {
                  const value = e.target.value;
                  if (value === "") {
                    field.onChange(undefined);
                    return;
                  }
                  const parsed = parseInt(value, 10);
                  if (!isNaN(parsed) && parsed >= 1 && parsed <= 300) {
                    field.onChange(parsed);
                  }
                  // Else: don't update (reject invalid input)
                }}
              />
            </FormControl>
            <FormDescription>
              {t("monitors.form.smtp.read_timeout_description")}
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
            <FormLabel>{t("monitors.form.smtp.from_email")}</FormLabel>
            <FormControl>
              <Input placeholder="sender@example.com" {...field} />
            </FormControl>
            <FormDescription>
              {t("monitors.form.smtp.from_email_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="rcpt_to_email"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t("monitors.form.smtp.rcpt_to_email")}</FormLabel>
            <FormControl>
              <Input placeholder="test@external-domain.com" {...field} />
            </FormControl>
            <FormDescription>
              {t("monitors.form.smtp.rcpt_to_email_description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="use_tls"
        render={({ field }) => (
          <FormItem className="flex flex-row items-start space-x-3 space-y-0">
            <FormControl>
              <Checkbox
                checked={field.value}
                disabled={useDirectTls}
                onCheckedChange={(checked) => {
                  field.onChange(checked);
                  // Uncheck useDirectTls when useTls is checked
                  if (checked) {
                    form.setValue("use_direct_tls", false);
                  }
                }}
              />
            </FormControl>
            <div className="space-y-1 leading-none">
              <FormLabel>{t("monitors.form.smtp.use_tls")}</FormLabel>
              <FormDescription>
                {t("monitors.form.smtp.use_tls_description")}
              </FormDescription>
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="use_direct_tls"
        render={({ field }) => (
          <FormItem className="flex flex-row items-start space-x-3 space-y-0">
            <FormControl>
              <Checkbox
                checked={field.value}
                disabled={useTls}
                onCheckedChange={(checked) => {
                  field.onChange(checked);
                  // Uncheck useTls when useDirectTls is checked
                  if (checked) {
                    form.setValue("use_tls", false);
                  }
                }}
              />
            </FormControl>
            <div className="space-y-1 leading-none">
              <FormLabel>{t("monitors.form.smtp.use_direct_tls")}</FormLabel>
              <FormDescription>
                {t("monitors.form.smtp.use_direct_tls_description")}
              </FormDescription>
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="ignore_tls_errors"
        render={({ field }) => (
          <FormItem className="flex flex-row items-start space-x-3 space-y-0">
            <FormControl>
              <Checkbox
                checked={field.value}
                disabled={!useTls && !useDirectTls}
                onCheckedChange={field.onChange}
              />
            </FormControl>
            <div className="space-y-1 leading-none">
              <FormLabel>{t("monitors.form.smtp.ignore_tls_errors")}</FormLabel>
              <FormDescription>
                {t("monitors.form.smtp.ignore_tls_errors_description")}
              </FormDescription>
            </div>
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="check_cert_expiry"
        render={({ field }) => (
          <FormItem className="flex flex-row items-start space-x-3 space-y-0">
            <FormControl>
              <Checkbox
                checked={field.value}
                disabled={!useTls && !useDirectTls}
                onCheckedChange={field.onChange}
              />
            </FormControl>
            <div className="space-y-1 leading-none">
              <FormLabel>{t("monitors.form.smtp.check_cert_expiry")}</FormLabel>
              <FormDescription>
                {t("monitors.form.smtp.check_cert_expiry_description")}
              </FormDescription>
            </div>
          </FormItem>
        )}
      />
    </>
  );
};

export default SMTPSettings;
