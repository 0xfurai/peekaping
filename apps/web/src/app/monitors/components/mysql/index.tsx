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
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Loader2 } from "lucide-react";
import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import { useEffect } from "react";

interface MySQLConfig {
  connection_string: string;
  query?: string;
}

// MySQL connection string regex pattern
// Format: mysql://username:password@host:port/database?params
const MYSQL_CONNECTION_STRING_REGEX = new RegExp(
  "^mysql://([^:@]+)(?::([^@]*))?@([^:/]+)(?::(\\d+))?/([^?]+)(?:\\?(.*))?$"
);

export const mysqlSchema = z
  .object({
    type: z.literal("mysql"),
    connection_string: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        MYSQL_CONNECTION_STRING_REGEX,
        "Connection string must be in format: mysql://username:password@host:port/database"
      ),
    query: z.string().optional(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type MySQLForm = z.infer<typeof mysqlSchema>;

export const mysqlDefaultValues: MySQLForm = {
  type: "mysql",
  connection_string: "mysql://username:password@host:3306/database",
  query: "SELECT 1",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

export const deserialize = (data: MonitorMonitorResponseDto): MySQLForm => {
  let config: MySQLConfig = {
    connection_string: "mysql://username:password@host:3306/database",
    query: "SELECT 1",
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        connection_string: parsedConfig.connection_string || "mysql://username:password@host:3306/database",
        query: parsedConfig.query || "SELECT 1",
      };
    } catch (error) {
      console.error("Failed to parse MySQL monitor config:", error);
    }
  }

  return {
    type: "mysql",
    name: data.name || "My MySQL Monitor",
    connection_string: config.connection_string,
    query: config.query,
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
  };
};

export const serialize = (formData: MySQLForm): MonitorCreateUpdateDto => {
  const config: MySQLConfig = {
    connection_string: formData.connection_string,
    query: formData.query,
  };

  return {
    type: "mysql",
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

const MySQLForm = () => {
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

  const onSubmit = (data: MySQLForm) => {
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
      form.reset(mysqlDefaultValues);
    }
  }, [mode, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as MySQLForm))}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>MySQL Connection</TypographyH4>
            <FormField
              control={form.control}
              name="connection_string"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Connection String</FormLabel>
                  <FormControl>
                    <Input placeholder="mysql://username:password@host:3306/database" {...field} />
                  </FormControl>
                  <div className="text-sm text-muted-foreground">
                    Examples:
                    <ul className="list-disc list-inside mt-1 space-y-1">
                      <li><code>mysql://user:pass@localhost:3306/mydb</code></li>
                      <li><code>mysql://user:pass@host/database</code> (default port 3306)</li>
                      <li><code>mysql://user:pass@host:3306/db?charset=utf8</code></li>
                    </ul>
                  </div>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="query"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Query</FormLabel>
                  <FormControl>
                    <Textarea placeholder="SELECT 1" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

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

export default MySQLForm;
