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

interface DockerConfig {
  container_id: string;
  connection_type: string;
  docker_daemon: string;
}

export const dockerSchema = z
  .object({
    type: z.literal("docker"),
    container_id: z.string().min(1, "Container Name/ID is required"),
    connection_type: z.enum(["socket", "tcp"], {
      required_error: "Connection type is required",
    }),
    docker_daemon: z.string().min(1, "Docker Daemon is required"),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(proxiesSchema);

export type DockerForm = z.infer<typeof dockerSchema>;

export const dockerDefaultValues: DockerForm = {
  type: "docker",
  container_id: "my-container",
  connection_type: "socket",
  docker_daemon: "/var/run/docker.sock",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
};

export const deserialize = (data: MonitorMonitorResponseDto): DockerForm => {
  let config: DockerConfig = {
    container_id: "my-container",
    connection_type: "socket",
    docker_daemon: "/var/run/docker.sock",
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        container_id: parsedConfig.container_id || "my-container",
        connection_type: parsedConfig.connection_type || "socket",
        docker_daemon: parsedConfig.docker_daemon || "/var/run/docker.sock",
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
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    proxy_id: data.proxy_id || "",
  };
};

export const serialize = (formData: DockerForm): MonitorCreateUpdateDto => {
  const config: DockerConfig = {
    container_id: formData.container_id,
    connection_type: formData.connection_type,
    docker_daemon: formData.docker_daemon,
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
  };
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
                      <SelectItem value="tcp">TCP</SelectItem>
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
                      placeholder="/var/run/docker.sock or tcp://host:2375"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                  <div className="text-sm text-muted-foreground">
                    <p className="font-medium mb-1">Examples:</p>
                    <ul className="list-disc list-inside space-y-0.5">
                      <li>/var/run/docker.sock</li>
                      <li>tcp://localhost:2375</li>
                      <li>tcp://localhost:2376 (TLS)</li>
                    </ul>
                  </div>
                </FormItem>
              )}
            />
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
