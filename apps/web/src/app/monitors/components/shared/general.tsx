import { useMemo } from "react";
import {
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
import { TypographyH4 } from "@/components/ui/typography";
import { useFormContext } from "react-hook-form";
import { z } from "zod";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

export const generalDefaultValues = {
  name: "My monitor",
};

export const generalSchema = z.object({
  name: z.string(),
});

const General = () => {
  const form = useFormContext();
  const { t } = useLocalizedTranslation();

  const monitorTypes = useMemo(() => [
    {
      type: "http",
      description: t("monitors.form.type.http"),
    },
    {
      type: "http-keyword",
      description: "HTTP(s) - Keyword",
    },
    {
      type: "http-json-query",
      description: "HTTP(s) - Json Query",
    },
    {
      type: "tcp",
      description: t("monitors.form.type.tcp"),
    },
    {
      type: "ping",
      description: t("monitors.form.type.ping"),
    },
    {
      type: "dns",
      description: t("monitors.form.type.dns"),
    },
    {
      type: "push",
      description: t("monitors.form.type.push"),
    },
    {
      type: "docker",
      description: t("monitors.form.type.docker"),
    },
    {
      type: "grpc-keyword",
      description: t("monitors.form.type.grpc"),
    },
    {
      type: "snmp",
      description: t("monitors.form.type.snmp"),
    },
    {
      type: "mysql",
      description: t("monitors.form.type.mysql"),
    },
    {
      type: "postgres",
      description: t("monitors.form.type.postgres"),
    },
    {
      type: "sqlserver",
      description: t("monitors.form.type.sqlserver"),
    },
    {
      type: "mongodb",
      description: t("monitors.form.type.mongodb"),
    },
    {
      type: "redis",
      description: t("monitors.form.type.redis"),
    },
    {
      type: "mqtt",
      description: t("monitors.form.type.mqtt"),
    },
    {
      type: "rabbitmq",
      description: t("monitors.form.type.rabbitmq"),
    },
    {
      type: "kafka-producer",
      description: t("monitors.form.type.kafka"),
    },
  ], [t]);

  return (
    <>
      <TypographyH4>{t('ui.general')}</TypographyH4>
      <FormField
        control={form.control}
        name="name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('forms.labels.monitor_name')}</FormLabel>
            <FormControl>
              <Input placeholder={t('forms.placeholders.monitor_name')} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('forms.labels.monitor_type')}</FormLabel>
            <Select
              onValueChange={(val) => {
                field.onChange(val);
              }}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t('common.select')} />
                </SelectTrigger>
              </FormControl>

              <SelectContent>
                {monitorTypes.map((monitor) => (
                  <SelectItem key={monitor.type} value={monitor.type}>
                    {monitor.description}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

export default General;
