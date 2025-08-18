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
import { useLocalizedTranslation } from "@/hooks/useTranslation";

interface SnmpConfig {
  host: string;
  port: number;
  community: string;
  snmp_version: string;
  oid: string;
  json_path?: string;
  json_path_operator?: string;
  expected_value?: string;
}

export const snmpSchema = z
  .object({
    type: z.literal("snmp"),
    host: z.string().min(1, "Host is required"),
    port: z
      .number()
      .min(1, "Port must be greater than 0")
      .max(65535, "Port must be less than 65536")
      .optional(),
    community: z.string().min(1, "Community is required"),
    snmp_version: z.enum(["v1", "v2c", "v3"], {
      required_error: "SNMP version is required",
    }),
    oid: z.string().min(1, "OID is required"),
    json_path: z.string().optional(),
    json_path_operator: z.enum(["eq", "ne", "lt", "gt", "le", "ge"]).optional(),
    expected_value: z.string().optional(),
  })
  .merge(generalSchema)
  .merge(intervalsSchema)
  .merge(notificationsSchema)
  .merge(tagsSchema);

export type SnmpForm = z.infer<typeof snmpSchema>;

export const snmpDefaultValues: SnmpForm = {
  type: "snmp",
  host: "127.0.0.1",
  port: 161,
  community: "public",
  snmp_version: "v2c",
  oid: "1.3.6.1.2.1.1.1.0",
  json_path: "$",
  json_path_operator: "eq",
  expected_value: "",
  ...generalDefaultValues,
  ...intervalsDefaultValues,
  ...notificationsDefaultValues,
  ...tagsDefaultValues,
};

export const deserialize = (data: MonitorMonitorResponseDto): SnmpForm => {
  let config: SnmpConfig = {
    host: "127.0.0.1",
    port: 161,
    community: "public",
    snmp_version: "v2c",
    oid: "1.3.6.1.2.1.1.1.0",
    json_path: "$",
    json_path_operator: "eq",
    expected_value: "",
  };

  if (data.config) {
    try {
      const parsedConfig = JSON.parse(data.config);
      config = {
        host: parsedConfig.host || "127.0.0.1",
        port: parsedConfig.port ?? 161,
        community: parsedConfig.community || "public",
        snmp_version: parsedConfig.snmp_version || "v2c",
        oid: parsedConfig.oid || "1.3.6.1.2.1.1.1.0",
        json_path: parsedConfig.json_path || "$",
        json_path_operator: parsedConfig.json_path_operator || "eq",
        expected_value: parsedConfig.expected_value || "",
      };
    } catch (error) {
      console.error("Failed to parse SNMP monitor config:", error);
    }
  }

  return {
    type: "snmp",
    name: data.name || "My SNMP Monitor",
    host: config.host,
    port: config.port,
    community: config.community,
    snmp_version: config.snmp_version as "v1" | "v2c" | "v3",
    oid: config.oid,
    json_path: config.json_path,
    json_path_operator: config.json_path_operator as
      | "eq"
      | "ne"
      | "lt"
      | "gt"
      | "le"
      | "ge"
      | undefined,
    expected_value: config.expected_value,
    interval: data.interval || 60,
    timeout: data.timeout || 16,
    max_retries: data.max_retries ?? 3,
    retry_interval: data.retry_interval || 60,
    resend_interval: data.resend_interval ?? 10,
    notification_ids: data.notification_ids || [],
    tag_ids: data.tag_ids || [],
  };
};

export const serialize = (formData: SnmpForm): MonitorCreateUpdateDto => {
  const config: SnmpConfig = {
    host: formData.host,
    port: formData.port ?? 161,
    community: formData.community,
    snmp_version: formData.snmp_version,
    oid: formData.oid,
    json_path: formData.json_path || "$",
    json_path_operator: formData.json_path_operator || "eq",
    expected_value: formData.expected_value || "",
  };

  return {
    type: "snmp",
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

const SnmpForm = () => {
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
  const { t } = useLocalizedTranslation();

  const onSubmit = (data: SnmpForm) => {
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
      form.reset(snmpDefaultValues);
    }
  }, [mode, form]);

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((data) => onSubmit(data as SnmpForm))}
        className="space-y-6 max-w-[600px]"
      >
        <Card>
          <CardContent className="space-y-4">
            <General />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>{t("monitors.form.snmp.title")}</TypographyH4>

            <FormField
              control={form.control}
              name="host"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.snmp.host_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder="127.0.0.1" {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.snmp.host_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="port"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.snmp.port_label")}</FormLabel>
                  <FormControl>
                    <Input
                      type="number"
                      placeholder="161"
                      min="1"
                      max="65535"
                      {...field}
                      onChange={(e) =>
                        field.onChange(parseInt(e.target.value, 10) || 161)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.snmp.port_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="community"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.snmp.community_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder="public" {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.snmp.community_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="snmp_version"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.snmp.snmp_version_label")}</FormLabel>
                  <Select
                    onValueChange={val => {
                      if (!val) {
                        return;
                      }
                      field.onChange(val);
                    }}
                    value={field.value}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue placeholder="Select SNMP version" />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="v1">SNMPv1</SelectItem>
                      <SelectItem value="v2c">SNMPv2c</SelectItem>
                      <SelectItem value="v3">SNMPv3</SelectItem>
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t("monitors.form.snmp.snmp_version_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="oid"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.snmp.oid_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder="1.3.6.1.2.1.1.1.0" {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.snmp.oid_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-4">
            <TypographyH4>{t("monitors.form.snmp.value_validation_label")}</TypographyH4>

            <FormField
              control={form.control}
              name="json_path"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.snmp.json_path_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder="$" {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.snmp.json_path_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="json_path_operator"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.snmp.json_path_operator_label")}</FormLabel>
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
                        <SelectValue placeholder="Select condition" />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectItem value="eq">== ({t("monitors.form.snmp.json_path_operator_eq")})</SelectItem>
                      <SelectItem value="ne">!= ({t("monitors.form.snmp.json_path_operator_ne")})</SelectItem>
                      <SelectItem value="lt">&lt; ({t("monitors.form.snmp.json_path_operator_lt")})</SelectItem>
                      <SelectItem value="gt">&gt; ({t("monitors.form.snmp.json_path_operator_gt")})</SelectItem>
                      <SelectItem value="le">
                        &le; ({t("monitors.form.snmp.json_path_operator_le")})
                      </SelectItem>
                      <SelectItem value="ge">
                        &ge; ({t("monitors.form.snmp.json_path_operator_ge")})
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <FormDescription>
                    {t("monitors.form.snmp.json_path_operator_description")}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="expected_value"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("monitors.form.snmp.expected_value_label")}</FormLabel>
                  <FormControl>
                    <Input placeholder="Expected value" {...field} />
                  </FormControl>
                  <FormDescription>
                    {t("monitors.form.snmp.expected_value_description")}
                  </FormDescription>
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
          {mode === "create" ? t("common.create") : t("common.update")}
        </Button>
      </form>
    </Form>
  );
};

export default SnmpForm;
