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

interface GRPCConfig {
  grpcUrl: string;
  grpcProtobuf: string;
  grpcServiceName: string;
  grpcMethod: string;
  grpcEnableTls: boolean;
  grpcBody: string;
  keyword: string;
  invertKeyword: boolean;
}

export const grpcKeywordSchema = z
  .object({
    type: z.literal("grpc-keyword"),
    grpcUrl: z.string().min(1, "gRPC URL is required"),
    grpcProtobuf: z.string().min(1, "Proto content is required"),
    grpcServiceName: z.string().min(1, "Proto service name is required"),
    grpcMethod: z.string().min(1, "Proto method is required"),
    grpcEnableTls: z.boolean(),
    grpcBody: z.string(),
    keyword: z.string(),
    invertKeyword: z.boolean(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema);

export type GRPCKeywordForm = z.infer<typeof grpcKeywordSchema>;

export const grpcKeywordDefaultValues: GRPCKeywordForm = {
  type: "grpc-keyword",
  grpcUrl: "localhost:50051",
  grpcProtobuf: `syntax = "proto3";

package grpc.health.v1;

service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}`,
  grpcServiceName: "Health",
  grpcMethod: "check",
  grpcEnableTls: false,
  grpcBody: `{
  "key": "value"
}`,
  keyword: "",
  invertKeyword: false,
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
};

export const deserialize = (
  data: MonitorMonitorResponseDto
): GRPCKeywordForm => {
  let config: GRPCConfig = {
    grpcUrl: "localhost:50051",
    grpcProtobuf: `syntax = "proto3";

package grpc.health.v1;

service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}`,
    grpcServiceName: "Health",
    grpcMethod: "check",
    grpcEnableTls: false,
    grpcBody: `{
  "key": "value"
}`,
    keyword: "",
    invertKeyword: false,
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        grpcUrl: parsedConfig.grpcUrl || "localhost:50051",
        grpcProtobuf: parsedConfig.grpcProtobuf || config.grpcProtobuf,
        grpcServiceName: parsedConfig.grpcServiceName || "Health",
        grpcMethod: parsedConfig.grpcMethod || "check",
        grpcEnableTls: parsedConfig.grpcEnableTls ?? false,
        grpcBody: parsedConfig.grpcBody || config.grpcBody,
        keyword: parsedConfig.keyword || "",
        invertKeyword: parsedConfig.invertKeyword ?? false,
      };
    } catch (error) {
      console.error("Failed to parse gRPC monitor config:", error);
    }
  }

  return {
    type: "grpc-keyword",
    name: data.name || "My gRPC Monitor",
    grpcUrl: config.grpcUrl,
    grpcProtobuf: config.grpcProtobuf,
    grpcServiceName: config.grpcServiceName,
    grpcMethod: config.grpcMethod,
    grpcEnableTls: config.grpcEnableTls,
    grpcBody: config.grpcBody,
    keyword: config.keyword,
    invertKeyword: config.invertKeyword,
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
  };
};

export const serialize = (
  formData: GRPCKeywordForm
): MonitorCreateUpdateDto => {
  const config: GRPCConfig = {
    grpcUrl: formData.grpcUrl,
    grpcProtobuf: formData.grpcProtobuf,
    grpcServiceName: formData.grpcServiceName,
    grpcMethod: formData.grpcMethod,
    grpcEnableTls: formData.grpcEnableTls,
    grpcBody: formData.grpcBody,
    keyword: formData.keyword,
    invertKeyword: formData.invertKeyword,
  };

  return {
    type: "grpc-keyword",
    name: formData.name,
    interval: formData.interval,
    max_retries: formData.max_retries,
    retry_interval: formData.retry_interval,
    notification_ids: formData.notification_ids,
    resend_interval: formData.resend_interval,
    timeout: formData.timeout,
    config: JSON.stringify(config),
  };
};

const GRPCKeywordForm = () => {
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

  const onSubmit = (data: GRPCKeywordForm) => {
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

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) =>
          onSubmit(data as GRPCKeywordForm)
        )}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <FormField
              control={form.control}
              name="grpcUrl"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>URL</FormLabel>
                  <FormControl>
                    <Input placeholder="localhost:50051" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="keyword"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Keyword</FormLabel>
                  <FormControl>
                    <Input
                      placeholder="Enter keyword to search for"
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    Search keyword in plain HTML or JSON response. The search is
                    case-sensitive.
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="invertKeyword"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start space-x-3 space-y-0">
                  <FormControl>
                    <Checkbox
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel>Invert Keyword</FormLabel>
                    <FormDescription>
                      Look for the keyword to be absent rather than present.
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>GRPC Options</TypographyH4>

            <FormField
              control={form.control}
              name="grpcEnableTls"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start space-x-3 space-y-0">
                  <FormControl>
                    <Checkbox
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel>Enable TLS</FormLabel>
                    <FormDescription>
                      Allow to send gRPC request with TLS connection
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="grpcServiceName"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Proto Service Name</FormLabel>
                  <FormControl>
                    <Input placeholder="Example: Health" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="grpcMethod"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Proto Method</FormLabel>
                  <FormControl>
                    <Input placeholder="Example: check" {...field} />
                  </FormControl>
                  <FormDescription>
                    Method name is convert to camelCase format such as sayHello,
                    check, etc.
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="grpcProtobuf"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Proto Content</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={`Example:
syntax = "proto3";

package grpc.health.v1;

service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}`}
                      className="min-h-[200px] font-mono text-sm"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="grpcBody"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Body</FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={`Example:
{
  "key": "value"
}`}
                      className="min-h-[100px] font-mono text-sm"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
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

export default GRPCKeywordForm;
