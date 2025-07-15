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
import Tags, { tagsDefaultValues, tagsSchema } from "../shared/tags";
import {
  useMonitorFormContext,
  type MonitorForm,
} from "../../context/monitor-form-context";
import {
  Form,
  FormControl,
  FormDescription,
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

interface MongoDBConfig {
  connectionString: string;
  command?: string;
  jsonPath?: string;
  expectedValue?: string;
}

// MongoDB connection string regex pattern
// Format: mongodb://username:password@host:port/database or mongodb+srv://username:password@host/database
// Also supports: mongodb://host:port/database (without authentication)
const MONGODB_CONNECTION_STRING_REGEX = new RegExp(
  "^mongodb(\\+srv)?://(?:([^:@/]+)(?::([^@/]*))?@)?([^:/@]+)(?::(\\d+))?/([^?]+)(?:\\?(.*))?$"
);

export const mongodbSchema = z
  .object({
    type: z.literal("mongodb"),
    connectionString: z
      .string()
      .min(1, "Connection string is required")
      .regex(
        MONGODB_CONNECTION_STRING_REGEX,
        "Connection string must be in format: mongodb://[username:password@]host[:port]/database or mongodb+srv://[username:password@]host/database"
      ),
    command: z.string().optional(),
    jsonPath: z.string().optional(),
    expectedValue: z.string().optional(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type MongoDBForm = z.infer<typeof mongodbSchema>;

export const mongodbDefaultValues: MongoDBForm = {
  type: "mongodb",
  connectionString: "mongodb://username:password@host:27017/database",
  command: '{"ping": 1}',
  jsonPath: "$",
  expectedValue: "",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

export const deserialize = (data: MonitorMonitorResponseDto): MongoDBForm => {
  let config: MongoDBConfig = {
    connectionString: "mongodb://username:password@host:27017/database",
    command: '{"ping": 1}',
    jsonPath: "$",
    expectedValue: "",
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        connectionString:
          parsedConfig.connectionString ||
          "mongodb://username:password@host:27017/database",
        command: parsedConfig.command || '{"ping": 1}',
        jsonPath: parsedConfig.jsonPath || "$",
        expectedValue: parsedConfig.expectedValue || "",
      };
    } catch (error) {
      console.error("Failed to parse MongoDB monitor config:", error);
    }
  }

  return {
    type: "mongodb",
    name: data.name || "My MongoDB Monitor",
    connectionString: config.connectionString,
    command: config.command,
    jsonPath: config.jsonPath,
    expectedValue: config.expectedValue,
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
  };
};

export const serialize = (formData: MongoDBForm): MonitorCreateUpdateDto => {
  const config: MongoDBConfig = {
    connectionString: formData.connectionString,
    command: formData.command,
    jsonPath: formData.jsonPath,
    expectedValue: formData.expectedValue,
  };

  return {
    type: "mongodb",
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

const MongoDBForm = () => {
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

  const onSubmit = (data: MonitorForm) => {
    const payload = serialize(data as MongoDBForm);

    if (mode === "create") {
      createMonitorMutation.mutate({
        body: {
          ...payload,
          active: true,
        },
      });
    } else {
      editMonitorMutation.mutate({
        path: {
          id: monitorId!,
        },
        body: {
          ...payload,
          active: monitor?.data?.active,
        },
      });
    }
  };

  // Reset form with default values when in create mode
  useEffect(() => {
    if (mode === "create") {
      form.reset(mongodbDefaultValues);
    }
  }, [mode, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="pt-6">
            <TypographyH4 className="mb-4">MongoDB Configuration</TypographyH4>

            <div className="space-y-4">
              <FormField
                control={form.control}
                name="connectionString"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Connection String</FormLabel>
                    <FormControl>
                      <Input
                        placeholder="mongodb://username:password@host:27017/database"
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      MongoDB connection string. Supports both standard
                      (mongodb://) and DNS seedlist (mongodb+srv://) formats.
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="command"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Command (Optional)</FormLabel>
                    <FormControl>
                      <Textarea placeholder='{"ping": 1}' rows={3} {...field} />
                    </FormControl>
                    <FormDescription>
                      MongoDB command to execute as JSON. Defaults to ping if
                      not specified.
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="jsonPath"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>JSON Path (Optional)</FormLabel>
                    <FormControl>
                      <Input placeholder="$" {...field} />
                    </FormControl>
                    <FormDescription>
                      JSON path to extract value from response. Use $ for root
                      level.
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="expectedValue"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Expected Value (Optional)</FormLabel>
                    <FormControl>
                      <Input placeholder="Expected value" {...field} />
                    </FormControl>
                    <FormDescription>
                      Expected value to compare against the JSON path result.
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
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

        <Button type="submit" disabled={isPending}>
          {isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {mode === "create" ? "Create Monitor" : "Update Monitor"}
        </Button>
      </form>
    </Form>
  );
};

export default MongoDBForm;
