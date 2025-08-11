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
} from "@/api/@tanstack/react-query.gen";
import React from "react";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

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
    defaultValues: { value: 30 },
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

const SettingsPage = () => {
  const { t } = useLocalizedTranslation();
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
      </div>
    </Layout>
  );
};

export default SettingsPage;
