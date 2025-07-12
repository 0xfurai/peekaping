import { z } from "zod";
import { TypographyH4 } from "@/components/ui/typography";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
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
import { proxiesSchema } from "../shared/proxies";
import Tags, {
  tagsDefaultValues,
  tagsSchema,
} from "../shared/tags";
import { useMonitorFormContext } from "../../context/monitor-form-context";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2 } from "lucide-react";
import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import { useEffect } from "react";
import { useWatch } from "react-hook-form";

interface DockerConfig {
  container_id: string;
  connection_type: string;
  docker_daemon: string;
  // TLS fields
  tls_enabled?: boolean;
  tls_cert?: string;
  tls_key?: string;
  tls_ca?: string;
  tls_verify?: boolean;
}

export const dockerSchema = z
  .object({
    type: z.literal("docker"),
    container_id: z.string().min(1, "Container Name/ID is required"),
    connection_type: z.enum(["socket", "tcp"], {
      required_error: "Connection type is required",
    }),
    docker_daemon: z.string().min(1, "Docker Daemon is required"),
    // TLS fields with proper defaults
    tls_enabled: z.boolean(),
    tls_cert: z.string(),
    tls_key: z.string(),
    tls_ca: z.string(),
    tls_verify: z.boolean(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(proxiesSchema)
  .merge(tagsSchema);

export type DockerForm = z.infer<typeof dockerSchema>;

export const dockerDefaultValues: DockerForm = {
  type: "docker",
  container_id: "",
  connection_type: "socket",
  docker_daemon: "/var/run/docker.sock",
  tls_enabled: false,
  tls_cert: "",
  tls_key: "",
  tls_ca: "",
  tls_verify: true,
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

export const deserialize = (data: MonitorMonitorResponseDto): DockerForm => {
  let config: DockerConfig = {
    container_id: "",
    connection_type: "socket",
    docker_daemon: "/var/run/docker.sock",
    tls_enabled: false,
    tls_cert: "",
    tls_key: "",
    tls_ca: "",
    tls_verify: true,
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        container_id: parsedConfig.container_id || "",
        connection_type: parsedConfig.connection_type || "socket",
        docker_daemon: parsedConfig.docker_daemon || "/var/run/docker.sock",
        tls_enabled: parsedConfig.tls_enabled || false,
        tls_cert: parsedConfig.tls_cert || "",
        tls_key: parsedConfig.tls_key || "",
        tls_ca: parsedConfig.tls_ca || "",
        tls_verify: parsedConfig.tls_verify !== undefined ? parsedConfig.tls_verify : true,
      };
    } catch (error) {
      console.error("Failed to parse Docker monitor config:", error);
    }
  }

  return {
    type: "docker",
    name: data.name || "My Docker Monitor",
    container_id: config.container_id,
    connection_type: config.connection_type as DockerForm["connection_type"],
    docker_daemon: config.docker_daemon,
    tls_enabled: config.tls_enabled || false,
    tls_cert: config.tls_cert || "",
    tls_key: config.tls_key || "",
    tls_ca: config.tls_ca || "",
    tls_verify: config.tls_verify !== undefined ? config.tls_verify : true,
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    proxy_id: data.proxy_id || "",
    tag_ids: data.tag_ids || [],
  };
};

export const serialize = (formData: DockerForm): MonitorCreateUpdateDto => {
  const config: DockerConfig = {
    container_id: formData.container_id,
    connection_type: formData.connection_type,
    docker_daemon: formData.docker_daemon,
    tls_enabled: formData.tls_enabled,
    ...(formData.tls_enabled && {
      tls_cert: formData.tls_cert,
      tls_key: formData.tls_key,
      tls_ca: formData.tls_ca,
      tls_verify: formData.tls_verify,
    }),
  };

  return {
    type: "docker",
    name: formData.name,
    interval: formData.interval,
    max_retries: formData.max_retries,
    retry_interval: formData.retry_interval,
    notification_ids: formData.notification_ids,
    proxy_id: formData.proxy_id,
    resend_interval: formData.resend_interval,
    timeout: formData.timeout,
    config: JSON.stringify(config),
    tag_ids: formData.tag_ids,
  };
};

const TLSSection = () => {
  const { form } = useMonitorFormContext();
  const connectionType = useWatch({
    control: form.control,
    name: "connection_type",
  });
  const tlsEnabled = useWatch({
    control: form.control,
    name: "tls_enabled",
  });

  // Don't show TLS section for socket connections
  if (connectionType !== "tcp") {
    return null;
  }

  return (
    <Card>
      <CardContent className="space-y-4">
        <TypographyH4>TLS Configuration</TypographyH4>

        <FormField
          control={form.control}
          name="tls_enabled"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Enable TLS</FormLabel>
              <Select
                onValueChange={(val) => {
                  field.onChange(val === "true");
                }}
                value={field.value ? "true" : "false"}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select TLS option" />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="false">Disabled</SelectItem>
                  <SelectItem value="true">Enabled</SelectItem>
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />

        {tlsEnabled && (
          <>
            <FormField
              control={form.control}
              name="tls_verify"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Verify TLS</FormLabel>
                  <Select
                    onValueChange={(val) => {
                      field.onChange(val === "true");
                    }}
                    value={field.value ? "true" : "false"}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder="Select verification option" />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="true">Verify certificates</SelectItem>
                      <SelectItem value="false">Skip verification</SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="tls_cert"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Client Certificate (Optional)</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                      {...field}
                      rows={6}
                      className="h-[250px]"
                    />
                  </FormControl>
                  <FormMessage />
                  <div className="text-sm text-muted-foreground">
                    PEM-formatted client certificate for mTLS authentication
                  </div>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="tls_key"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Client Private Key (Optional)</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                      {...field}
                      rows={6}
                      className="h-[250px]"
                    />
                  </FormControl>
                  <FormMessage />
                  <div className="text-sm text-muted-foreground">
                    PEM-formatted private key for the client certificate
                  </div>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="tls_ca"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>CA Certificate (Optional)</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                      {...field}
                      rows={6}
                      className="h-[250px]"
                    />
                  </FormControl>
                  <FormMessage />
                  <div className="text-sm text-muted-foreground">
                    PEM-formatted CA certificate to verify the Docker daemon's certificate
                  </div>
                </FormItem>
              )}
            />

            <div className="bg-amber-50 border border-amber-200 rounded-md p-4">
              <div className="text-sm text-amber-800">
                <strong>Note:</strong> For mutual TLS (mTLS), provide both client certificate and private key.
                For server-only TLS, certificates are optional.
              </div>
            </div>

            <div className="bg-blue-50 border border-blue-200 rounded-md p-4">
              <div className="text-sm text-blue-800">
                <strong>Common TLS Issues:</strong>
                <ul className="mt-2 space-y-1 list-disc list-inside">
                  <li><strong>Legacy Certificate Error:</strong> If you see "certificate relies on legacy Common Name field",
                  set "Verify TLS" to false or update your Docker daemon with a certificate that includes Subject Alternative Names (SANs).</li>
                  <li><strong>Unknown Authority:</strong> If you see "certificate signed by unknown authority",
                  provide the CA certificate above or disable TLS verification for testing.</li>
                  <li><strong>Hostname Mismatch:</strong> Ensure the Docker daemon URL matches the certificate's Common Name or SAN entries.</li>
                </ul>
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
};

const DockerForm = () => {
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

  const onSubmit = (data: DockerForm) => {
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
      form.reset(dockerDefaultValues);
    }
  }, [mode, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as DockerForm))}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>Docker Container</TypographyH4>
            <FormField
              control={form.control}
              name="container_id"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Container Name / ID</FormLabel>
                  <FormControl>
                    <Input placeholder="my-container" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>Docker Host</TypographyH4>
            <FormField
              control={form.control}
              name="connection_type"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Connection Type</FormLabel>
                  <Select
                    onValueChange={(val) => {
                      if (!val) {
                        return;
                      }
                      field.onChange(val);
                      // Reset TLS settings when switching from TCP to socket
                      if (val === "socket") {
                        form.setValue("tls_enabled", false);
                        form.setValue("docker_daemon", "/var/run/docker.sock");
                      } else if (val === "tcp") {
                        form.setValue("docker_daemon", "http://localhost:2375");
                      }
                    }}
                    value={field.value}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder="Select connection type" />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="socket">Socket</SelectItem>
                      <SelectItem value="tcp">TCP/HTTP</SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="docker_daemon"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Docker Daemon</FormLabel>
                  <FormControl>
                    <Input
                      placeholder="/var/run/docker.sock or http://host:2375"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                  <div className="text-sm text-muted-foreground">
                    <p className="font-medium mb-1">Examples:</p>
                    <ul className="list-disc list-inside space-y-0.5">
                      <li>/var/run/docker.sock</li>
                      <li>http://localhost:2375</li>
                      <li>https://localhost:2376 (TLS)</li>
                    </ul>
                  </div>
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <TLSSection />

        <Card>
          <CardContent className="space-y-4">
            <Tags />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Notifications onNewNotifier={() => setNotifierSheetOpen(true)} />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <Intervals />
          </CardContent>
        </Card>

        <Button type="submit">
          {isPending && <Loader2 className="animate-spin" />}
          {mode === "create" ? "Create" : "Update"}
        </Button>
      </form>
    </Form>
  );
};

export default DockerForm;
