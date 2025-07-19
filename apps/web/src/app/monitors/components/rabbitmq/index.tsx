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
  FormDescription,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";

import { Loader2, Plus, Trash2 } from "lucide-react";
import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import { useEffect } from "react";
import { useFieldArray } from "react-hook-form";

interface RabbitMQConfig {
  nodes: string[]; // Server expects array of strings
  username: string;
  password: string;
}

export const rabbitMQSchema = z
  .object({
    type: z.literal("rabbitmq"),
    nodes: z.array(z.object({
      url: z.string().url("Must be a valid URL")
    })).min(1, "At least one node is required"),
    username: z.string().min(1, "Username is required"),
    password: z.string().min(1, "Password is required"),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export const rabbitMQDefaultValues: RabbitMQForm = {
  type: "rabbitmq",
  nodes: [{ url: "https://localhost:15672" }],
  username: "",
  password: "",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

export type RabbitMQForm = z.infer<typeof rabbitMQSchema>;

export const serialize = (data: RabbitMQForm): MonitorCreateUpdateDto => {
  const config: RabbitMQConfig = {
    nodes: data.nodes.map(node => node.url), // Convert [{ url: "" }] to string[]
    username: data.username,
    password: data.password,
  };

  return {
    type: data.type,
    name: data.name,
    interval: data.interval,
    timeout: data.timeout,
    max_retries: data.max_retries,
    retry_interval: data.retry_interval,
    resend_interval: data.resend_interval,
    notification_ids: data.notification_ids,
    tag_ids: data.tag_ids,
    config: JSON.stringify(config),
  };
};

export const deserialize = (data: MonitorMonitorResponseDto): RabbitMQForm => {
  const config: RabbitMQConfig = data.config ? JSON.parse(data.config) : {};

  return {
    type: "rabbitmq",
    name: data.name || "",
    interval: data.interval || 60,
    timeout: data.timeout || 30,
    max_retries: data.max_retries || 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval || 0,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
    nodes: config.nodes ? config.nodes.map((url: string) => ({ url })) : [{ url: "https://localhost:15672" }], // Convert string[] to [{ url: "" }]
    username: config.username || "",
    password: config.password || "",
  };
};

const RabbitMQForm = () => {
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

  const { fields, append, remove } = useFieldArray({
    control: form.control,
    name: "nodes",
  });

  const onSubmit = (data: RabbitMQForm) => {
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
      form.reset(rabbitMQDefaultValues);
    }
  }, [mode, form]);

  const addNode = () => {
    append({ url: "https://" });
  };

  const removeNode = (index: number) => {
    if (fields.length > 1) {
      remove(index);
    }
  };

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as RabbitMQForm))}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>RabbitMQ Configuration</TypographyH4>

            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h5 className="text-sm font-medium">Management Nodes</h5>
                  <p className="text-xs text-muted-foreground">
                    Enter the URL for the RabbitMQ management nodes including protocol and port. Example: https://node1.rabbitmq.com:15672
                  </p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={addNode}
                >
                  <Plus className="h-4 w-4 mr-1" />
                  Add Node
                </Button>
              </div>

              {fields.map((field, index) => (
                <div key={field.id} className="flex items-end gap-2">
                  <FormField
                    control={form.control}
                    name={`nodes.${index}.url`}
                    render={({ field }) => (
                      <FormItem className="flex-1">
                        <FormLabel className={index > 0 ? "sr-only" : ""}>
                          Node {index + 1} URL
                        </FormLabel>
                        <FormControl>
                          <Input
                            placeholder="https://node1.rabbitmq.com:15672"
                            {...field}
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  {fields.length > 1 && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => removeNode(index)}
                      className="text-destructive hover:text-destructive"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </div>
              ))}
            </div>

            <FormField
              control={form.control}
              name="username"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Username</FormLabel>
                  <FormControl>
                    <Input placeholder="admin" {...field} />
                  </FormControl>
                  <FormDescription>
                    Username for RabbitMQ management interface authentication
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Password</FormLabel>
                  <FormControl>
                    <Input type="password" placeholder="password" {...field} />
                  </FormControl>
                  <FormDescription>
                    Password for RabbitMQ management interface authentication
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <div className="bg-muted/50 p-4 rounded-lg">
              <h6 className="text-sm font-medium mb-2">Setup Information</h6>
              <p className="text-xs text-muted-foreground mb-2">
                To use the RabbitMQ monitor, you need to enable the Management Plugin in your RabbitMQ setup.
                For more information, please consult the{" "}
                <a
                  href="https://www.rabbitmq.com/management.html"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary underline"
                >
                  RabbitMQ documentation
                </a>
                .
              </p>
              <p className="text-xs text-muted-foreground">
                The monitor checks the <code>/api/health/checks/alarms/</code> endpoint on each node and returns UP if any node is healthy.
              </p>
            </div>
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

        <Button type="submit" className="w-full" disabled={isPending}>
          {isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {mode === "create" ? "Create Monitor" : "Update Monitor"}
        </Button>
      </form>
    </Form>
  );
};

export default RabbitMQForm;
