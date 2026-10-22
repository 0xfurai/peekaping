import Layout from "@/layout";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardDescription,
} from "@/components/ui/card";
import TimezoneSelector from "../../components/TimezoneSelector";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Form,
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";
import { toast } from "sonner";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getSettingsKeyByKeyOptions,
  putSettingsKeyByKeyMutation,
  getSettingsKeyByKeyQueryKey,
  getErrorReportingOptions,
  getErrorReportingQueryKey,
  putErrorReportingMutation,
  getTelemetryOptions,
  getTelemetryQueryKey,
  putTelemetryMutation,
} from "@/api/@tanstack/react-query.gen";
import React from "react";
import { useLocation } from "react-router-dom";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { TelemetryDocsLink } from "@/components/telemetry-docs-link";

const KeepDataPeriodSetting = () => {
  const { t } = useLocalizedTranslation();
  const keepDataPeriodSchema = z.object({
    value: z.coerce
      .number()
      .int()
      .min(1, { message: t("settings.validation.min_days") }),
  });
  const KEEP_DATA_KEY = "KEEP_DATA_PERIOD_DAYS";
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery(
    getSettingsKeyByKeyOptions({ path: { key: KEEP_DATA_KEY } })
  );

  const form = useForm({
    resolver: zodResolver(keepDataPeriodSchema),
    defaultValues: { value: 365 },
  });

  // Reset form when data is loaded
  React.useEffect(() => {
    if (data?.data?.value) {
      form.reset({ value: Number(data.data.value) });
    }
  }, [data, form]);

  const mutation = useMutation({
    ...putSettingsKeyByKeyMutation(),
    onSuccess: () => {
      toast.success(t("messages.setting_update_success"));
      queryClient.invalidateQueries({
        queryKey: getSettingsKeyByKeyQueryKey({
          path: { key: KEEP_DATA_KEY },
        }),
      });
    },
    onError: commonMutationErrorHandler(t("messages.setting_update_error")),
  });

  function onSubmit(values: { value: number }) {
    mutation.mutate({
      path: { key: KEEP_DATA_KEY },
      body: { type: "int", value: String(values.value) },
    });
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("settings.data_retention.title")}</CardTitle>
        <CardDescription>
          {t("settings.data_retention.description")}
        </CardDescription>
      </CardHeader>

      <CardContent>
        {isLoading ? (
          <div className="h-10 flex items-center">{t("common.loading")}</div>
        ) : (
          <Form {...form}>
            <form
              onSubmit={form.handleSubmit(onSubmit)}
              className="flex gap-2 items-end max-w-xs"
            >
              <FormField
                control={form.control}
                name="value"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t("settings.data_retention.days_label")}</FormLabel>
                    <FormControl>
                      <Input
                        type="number"
                        min={1}
                        max={36500}
                        {...field}
                        disabled={isLoading}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <Button type="submit" disabled={mutation.isPending || isLoading}>
                {mutation.isPending ? t("common.saving") : t("common.save")}
              </Button>
            </form>
          </Form>
        )}
      </CardContent>
    </Card>
  );
};

const TelemetrySetting = () => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  // The payload follows the install's monitors and channels, so show the current one.
  const { data, isLoading, isError } = useQuery({
    ...getTelemetryOptions(),
    staleTime: 0,
  });

  const mutation = useMutation({
    ...putTelemetryMutation(),
    onSuccess: (state) => {
      toast.success(t("messages.setting_update_success"));
      queryClient.setQueryData(getTelemetryQueryKey(), state);
      queryClient.invalidateQueries({ queryKey: getTelemetryQueryKey() });
    },
    onError: commonMutationErrorHandler(t("messages.setting_update_error")),
  });

  return (
    <Card id="usage-statistics">
      <CardHeader>
        <CardTitle>{t("settings.telemetry.title")}</CardTitle>
        <CardDescription>
          {t("settings.telemetry.description")}
        </CardDescription>
      </CardHeader>

      <CardContent className="flex flex-col gap-4">
        {isError ? (
          <div className="h-10 flex items-center">{t("common.error")}</div>
        ) : isLoading || !data ? (
          <div className="h-10 flex items-center">{t("common.loading")}</div>
        ) : (
          <>
            <div className="flex items-center gap-2">
              <Switch
                id="telemetry-enabled"
                checked={data.enabled && !data.forced_off}
                disabled={data.forced_off || mutation.isPending}
                onCheckedChange={(enabled) =>
                  mutation.mutate({ body: { enabled } })
                }
              />
              <Label htmlFor="telemetry-enabled">{t("telemetry.label")}</Label>
            </div>

            {data.forced_off && (
              <Alert>
                <AlertDescription>
                  {t("settings.telemetry.forced_off")}
                </AlertDescription>
              </Alert>
            )}

            <div className="flex flex-col gap-2">
              <p className="text-sm font-medium">
                {t("settings.telemetry.payload_title")}
              </p>
              <pre
                dir="ltr"
                className="rounded-md bg-muted p-3 text-xs overflow-x-auto"
              >
                {JSON.stringify(data.payload, null, 2)}
              </pre>
              {!data.enabled && !data.forced_off && (
                <p className="text-sm text-muted-foreground">
                  {t("settings.telemetry.payload_off")}
                </p>
              )}
              <p className="text-sm text-muted-foreground">
                {t("settings.telemetry.id_note")}
              </p>
            </div>

            <div className="text-sm">
              <TelemetryDocsLink />
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
};

const ErrorReportingSetting = () => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const { data, isLoading, isError } = useQuery({
    ...getErrorReportingOptions(),
    staleTime: 0,
  });

  const mutation = useMutation({
    ...putErrorReportingMutation(),
    onSuccess: (state) => {
      toast.success(t("messages.setting_update_success"));
      queryClient.setQueryData(getErrorReportingQueryKey(), state);
      queryClient.invalidateQueries({ queryKey: getErrorReportingQueryKey() });
      // Any choice marks the upgrade notice seen, and the usage statistics state says whether
      // it is still pending
      queryClient.invalidateQueries({ queryKey: getTelemetryQueryKey() });
    },
    onError: commonMutationErrorHandler(t("messages.setting_update_error")),
  });

  return (
    <Card id="error-reports">
      <CardHeader>
        <CardTitle>{t("settings.error_reporting.title")}</CardTitle>
        <CardDescription>
          {t("settings.error_reporting.description")}
        </CardDescription>
      </CardHeader>

      <CardContent className="flex flex-col gap-4">
        {isError ? (
          <div className="h-10 flex items-center">{t("common.error")}</div>
        ) : isLoading || !data ? (
          <div className="h-10 flex items-center">{t("common.loading")}</div>
        ) : (
          <>
            <div className="flex items-center gap-2">
              <Switch
                id="error-reporting-enabled"
                checked={data.enabled && !data.forced_off}
                disabled={data.forced_off || mutation.isPending}
                onCheckedChange={(enabled) =>
                  mutation.mutate({ body: { enabled } })
                }
              />
              <Label htmlFor="error-reporting-enabled">
                {t("error_reporting.label")}
              </Label>
            </div>

            {data.forced_off && (
              <Alert>
                <AlertDescription>
                  {t("settings.error_reporting.forced_off")}
                </AlertDescription>
              </Alert>
            )}

            <div className="flex flex-col gap-2 text-sm text-muted-foreground">
              <p>{t("error_reporting.sent")}</p>
              <p>{t("error_reporting.never_sent")}</p>
              <p>{t("settings.error_reporting.cluster_note")}</p>
            </div>

            <div className="text-sm">
              <TelemetryDocsLink section="error-reporting" />
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
};

const SettingsPage = () => {
  const { t } = useLocalizedTranslation();
  // The upgrade notice links to a card, such as /settings#error-reports
  const { hash } = useLocation();
  React.useEffect(() => {
    if (hash) document.getElementById(hash.slice(1))?.scrollIntoView();
  }, [hash]);
  return (
    <Layout pageName={t("navigation.settings")}>
      <div className="max-w-xl flex flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>{t("settings.timezone.title")}</CardTitle>
            <CardDescription>
              {t("settings.timezone.description")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <TimezoneSelector />
          </CardContent>
        </Card>
        <KeepDataPeriodSetting />
        <TelemetrySetting />
        <ErrorReportingSetting />
      </div>
    </Layout>
  );
};

export default SettingsPage;
