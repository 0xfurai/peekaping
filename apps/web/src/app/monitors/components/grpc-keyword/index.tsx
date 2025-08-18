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
import { Loader2 } from "lucide-react";
import type { MonitorCreateUpdateDto, MonitorMonitorResponseDto } from "@/api";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

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
  .merge(notificationsSchema)
  .merge(tagsSchema);

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
  ...tagsDefaultValues,
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
    tag_ids: data.tag_ids || [],
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
    tag_ids: formData.tag_ids,
  };
};

const GRPCKeywordForm = () => {
  const { t } = useLocalizedTranslation();
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
                  <FormLabel>{t("monitors.form.grpc.keyword")}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t("monitors.form.grpc.keyword_placeholder")}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.grpc.keyword_description")}
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
                    <FormLabel>{t("monitors.form.grpc.invert_keyword")}</FormLabel>
                    <FormDescription>
                      {t("monitors.form.grpc.invert_keyword_description")}
                    </FormDescription>
                  </div>
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>{t("monitors.form.grpc.options_title")}</TypographyH4>

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
                    <FormLabel>{t("monitors.form.grpc.enable_tls")}</FormLabel>
                    <FormDescription>
                      {t("monitors.form.grpc.enable_tls_description")}
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
                  <FormLabel>{t("monitors.form.grpc.proto_service_name")}</FormLabel>
                  <FormControl>
                    <Input placeholder={t("monitors.form.grpc.service_name_placeholder")} {...field} />
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
                  <FormLabel>{t("monitors.form.grpc.proto_method")}</FormLabel>
                  <FormControl>
                    <Input placeholder={t("monitors.form.grpc.method_placeholder")} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.grpc.method_description")}
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
                  <FormLabel>{t("monitors.form.grpc.proto_content")}</FormLabel>
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
                  <FormLabel>{t("monitors.form.grpc.body")}</FormLabel>
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
          {mode === "create" ? t("monitors.form.buttons.create") : t("monitors.form.buttons.update")}
        </Button>
      </form>
    </Form>
  );
};

export default GRPCKeywordForm;
