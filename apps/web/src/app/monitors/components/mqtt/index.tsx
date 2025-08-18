import { z } from "zod";
import { TypographyH4 } from "@/components/ui/typography";
import { Button } from "@/components/ui/button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
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

import { PasswordInput } from "@/components/ui/password-input";

interface MQTTConfig {
  hostname: string;
  port: number;
  topic: string;
  username?: string;
  password?: string;
  check_type: string;
  success_keyword?: string;
  json_path?: string;
  expected_value?: string;
}

export const mqttSchema = z
  .object({
    type: z.literal("mqtt"),
    hostname: z.string().min(1, "Hostname is required"),
    port: z.coerce.number().min(1).max(65535),
    topic: z.string().min(1, "Topic is required"),
    username: z.string().optional(),
    password: z.string().optional(),
    check_type: z.enum(["keyword", "json-query", "none"]),
    success_keyword: z.string().optional(),
    json_path: z.string().optional(),
    expected_value: z.string().optional(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type MQTTForm = z.infer<typeof mqttSchema>;

export const mqttDefaultValues: MQTTForm = {
  type: "mqtt",
  hostname: "localhost",
  port: 1883,
  topic: "test/topic",
  username: "",
  password: "",
  check_type: "none",
  success_keyword: "",
  json_path: "",
  expected_value: "",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

const serialize = (data: MQTTForm): MonitorCreateUpdateDto => {
  const config: MQTTConfig = {
    hostname: data.hostname,
    port: data.port,
    topic: data.topic,
    username: data.username,
    password: data.password,
    check_type: data.check_type,
    success_keyword: data.success_keyword,
    json_path: data.json_path,
    expected_value: data.expected_value,
  };

  return {
    type: data.type,
    name: data.name,
    interval: data.interval,
    timeout: data.timeout,
    max_retries: data.max_retries,
    retry_interval: data.retry_interval,
    resend_interval: data.resend_interval,
    config: JSON.stringify(config),
    notification_ids: data.notification_ids,
    tag_ids: data.tag_ids,
  };
};

export const deserialize = (data: MonitorMonitorResponseDto): MQTTForm => {
  const config: MQTTConfig = data.config ? JSON.parse(data.config) : {};

  return {
    type: "mqtt",
    name: data.name || "",
    hostname: config.hostname || "localhost",
    port: config.port || 1883,
    topic: config.topic || "test/topic",
    username: config.username || "",
    password: config.password || "",
    check_type: (config.check_type as "keyword" | "json-query" | "none") || "none",
    success_keyword: config.success_keyword || "",
    json_path: config.json_path || "",
    expected_value: config.expected_value || "",
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries || 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval || 10,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
  };
};

const MQTTForm = () => {
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

  const checkType = form.watch("check_type");

  const onSubmit = (data: MQTTForm) => {
    // Validate conditional required fields at submission time
    if (data.check_type === "keyword" && (!data.success_keyword || data.success_keyword.trim() === "")) {
      form.setError("success_keyword", {
        type: "manual",
        message: t("monitors.form.mqtt.validation.success_keyword_required"),
      });
      return;
    }

    if (data.check_type === "json-query") {
      if (!data.json_path || data.json_path.trim() === "") {
        form.setError("json_path", {
          type: "manual",
          message: t("monitors.form.mqtt.validation.json_path_required"),
        });
        return;
      }
      if (!data.expected_value || data.expected_value.trim() === "") {
        form.setError("expected_value", {
          type: "manual",
          message: t("monitors.form.mqtt.validation.expected_value_required"),
        });
        return;
      }
    }

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
      form.reset(mqttDefaultValues);
    }
  }, [mode, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as MQTTForm))}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>{t("monitors.form.mqtt.configuration_title")}</TypographyH4>
            <FormField
              control={form.control}
              name="hostname"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mqtt.hostname_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder="localhost" {...field} />
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
                  <FormLabel>{t("monitors.form.mqtt.port_label")}</FormLabel>
                  <FormControl>
                    <Input
                      type="number"
                      min="1"
                      max="65535"
                      placeholder="1883"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="topic"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>MQTT Topic</FormLabel>
                  <FormControl>
                    <Input placeholder="test/topic" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="username"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mqtt.username_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder={t("monitors.form.mqtt.username_placeholder")} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mqtt.password_label")}</FormLabel>
                  <FormControl>
                    <PasswordInput
                      placeholder={t("monitors.form.mqtt.password_placeholder")}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="check_type"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.mqtt.check_type_label")}</FormLabel>
                  <Select onValueChange={field.onChange} value={field.value}>
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder={t("monitors.form.mqtt.check_type_placeholder")} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="none">{t("monitors.form.mqtt.check_type_none")}</SelectItem>
                      <SelectItem value="keyword">{t("monitors.form.mqtt.check_type_keyword")}</SelectItem>
                      <SelectItem value="json-query">{t("monitors.form.mqtt.check_type_json_query")}</SelectItem>
                    </SelectContent>
                  </Select>
                  <FormMessage />
                </FormItem>
              )}
            />

            {checkType === "keyword" && (
              <FormField
                control={form.control}
                name="success_keyword"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("monitors.form.mqtt.success_keyword_label")}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t("monitors.form.mqtt.success_keyword_placeholder")}
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}

            {checkType === "json-query" && (
              <>
                <FormField
                  control={form.control}
                  name="json_path"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t("monitors.form.mqtt.json_path_label")}</FormLabel>
                      <FormControl>
                        <Input placeholder="$.status" {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="expected_value"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t("monitors.form.mqtt.expected_value_label")}</FormLabel>
                      <FormControl>
                        <Input placeholder="ok" {...field} />
                      </FormControl>
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

        <Button
          type="submit"
          className="w-full"
          disabled={isPending}
        >
          {isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
          {mode === "create" ? t("monitors.form.buttons.create") : t("monitors.form.buttons.update")}
        </Button>
      </form>
    </Form>
  );
};

export default MQTTForm;
