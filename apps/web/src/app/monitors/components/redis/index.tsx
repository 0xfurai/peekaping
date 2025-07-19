import { z } from "zod";
import { TypographyH4 } from "@/components/ui/typography";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Textarea } from "@/components/ui/textarea";
import Intervals, {
  intervalsDefaultValues,
  intervalsSchema,
} from "../shared/intervals";
import General, {
  generalDefaultValues,
  generalSchema,
} from "../shared/general";
import Notifications, {
  notificationsDefaultValues,
  notificationsSchema,
} from "../shared/notifications";
import Tags, { tagsDefaultValues, tagsSchema } from "../shared/tags";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
  FormDescription,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Loader2 } from "lucide-react";
import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import { useEffect } from "react";

interface RedisConfig {
  databaseConnectionString: string;
  ignoreTls: boolean;
  caCert?: string;
  clientCert?: string;
  clientKey?: string;
}

// Redis connection string validation regex
// Updated to support IPv6 addresses in brackets [::1] or without brackets ::1
const redisConnectionStringRegex =
  /^(rediss?:\/\/)([^@]*@)?(\[[^\]]+\]|[^:/]+)(:\d{1,5})?(\/[0-9]*)?$/;

export const redisSchema = z
  .object({
    type: z.literal("redis"),
    databaseConnectionString: z
      .string()
      .min(1, "Database connection string is required")
      .regex(
        redisConnectionStringRegex,
        "Invalid Redis connection string format. Expected: redis://[user:password@]host[:port][/db] or rediss://[user:password@]host[:port][/db]"
      )
      .refine(
        (value) => {
          if (!value) return false;

          // Basic format check
          if (!redisConnectionStringRegex.test(value)) return false;

          // Parse the URL to validate components
          try {
            const url = new URL(value);

            // Check protocol
            if (!["redis:", "rediss:"].includes(url.protocol)) return false;

            // Check hostname
            if (!url.hostname || url.hostname.length === 0) return false;

            // Check port (optional, but if present should be valid)
            if (url.port) {
              const port = parseInt(url.port);
              if (isNaN(port) || port < 1 || port > 65535) return false;
            }

            // Check database number (optional, but if present should be valid)
            if (url.pathname && url.pathname !== "/") {
              const dbNumber = parseInt(url.pathname.slice(1));
              if (isNaN(dbNumber) || dbNumber < 0) return false;
            }

            return true;
          } catch {
            return false;
          }
        },
        {
          message:
            "Invalid Redis connection string. Please check the format and ensure host, port (1-65535), and database number (0+) are valid.",
        }
      ),
    ignoreTls: z.boolean(),
    caCert: z.string().optional(),
    clientCert: z.string().optional(),
    clientKey: z.string().optional(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type RedisForm = z.infer<typeof redisSchema>;

export const redisDefaultValues: RedisForm = {
  type: "redis",
  databaseConnectionString: "redis://user:password@host:port",
  ignoreTls: false,
  caCert: "",
  clientCert: "",
  clientKey: "",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

export const serialize = (formData: RedisForm): MonitorCreateUpdateDto => {
  const config: RedisConfig = {
    databaseConnectionString: formData.databaseConnectionString,
    ignoreTls: formData.ignoreTls,
    ...(formData.caCert && { caCert: formData.caCert }),
    ...(formData.clientCert && { clientCert: formData.clientCert }),
    ...(formData.clientKey && { clientKey: formData.clientKey }),
  };

  return {
    type: "redis",
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

export const deserialize = (data: MonitorMonitorResponseDto): RedisForm => {
  const config: RedisConfig = data.config ? JSON.parse(data.config) : {};

  return {
    type: "redis",
    name: data.name ?? "",
    interval: data.interval ?? 60,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval ?? 60,
    notification_ids: data.notification_ids ?? [],
    resend_interval: data.resend_interval ?? 0,
    timeout: data.timeout ?? 10,
    tag_ids: data.tag_ids ?? [],
    databaseConnectionString:
      config.databaseConnectionString ?? "redis://user:password@host:port",
    ignoreTls: config.ignoreTls ?? false,
    caCert: config.caCert ?? "",
    clientCert: config.clientCert ?? "",
    clientKey: config.clientKey ?? "",
  };
};

const RedisForm = () => {
  const {
    form,
    setNotifierSheetOpen,
    isPending,
    mode,
    createMonitorMutation,
    editMonitorMutation,
    monitorId,
    monitor,
  } = useMonitorFormContext();

  const onSubmit = (data: RedisForm) => {
    const payload = serialize(data);

    if (mode === "create") {
      createMonitorMutation.mutate({
        body: {
          ...payload,
          active: true,
        },
      });
    } else {
      editMonitorMutation.mutate({
        path: { id: monitorId! },
        body: {
          ...payload,
          active: monitor?.data?.active,
        },
      });
    }
  };

  useEffect(() => {
    if (mode === "create") {
      form.reset(redisDefaultValues);
    }
  }, [mode, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as RedisForm))}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>Redis Configuration</TypographyH4>

            <FormField
              control={form.control}
              name="databaseConnectionString"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Connection String</FormLabel>
                  <FormControl>
                    <Input
                      placeholder="redis://user:password@host:port"
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    Redis connection string format:
                    redis://[user:password@]host[:port][/db] or
                    rediss://[user:password@]host[:port][/db] for TLS
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="ignoreTls"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start space-x-3 space-y-0">
                  <FormControl>
                    <Checkbox
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel>Ignore TLS/SSL errors</FormLabel>
                    <FormDescription>
                      Skip TLS certificate verification (not recommended for
                      production)
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />

            {form.watch("databaseConnectionString")?.startsWith("rediss://") && (
              <>
                <FormField
                  control={form.control}
                  name="caCert"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>CA Certificate (Optional)</FormLabel>
                      <FormControl>
                        <Textarea
                          placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                          className="font-mono text-sm"
                          rows={6}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        Root CA certificate in PEM format. Required for proper TLS verification with self-signed certificates.
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="clientCert"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Client Certificate (Optional)</FormLabel>
                      <FormControl>
                        <Textarea
                          placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                          className="font-mono text-sm"
                          rows={6}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        Client certificate in PEM format. Required for mutual TLS authentication.
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="clientKey"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>Client Private Key (Optional)</FormLabel>
                      <FormControl>
                        <Textarea
                          placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                          className="font-mono text-sm"
                          rows={6}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        Client private key in PEM format. Must be provided together with client certificate.
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Intervals />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Notifications onNewNotifier={() => setNotifierSheetOpen(true)} />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Tags />
          </CardContent>
        </Card>

        <Button type="submit" disabled={isPending}>
          {isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {mode === "create" ? "Create Monitor" : "Update Monitor"}
        </Button>
      </form>
    </Form>
  );
};

export default RedisForm;
