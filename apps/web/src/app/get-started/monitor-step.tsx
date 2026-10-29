import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Link } from "react-router-dom";
import {
  getMonitorsInfiniteQueryKey,
  getMonitorsQueryKey,
  postMonitorsMutation,
} from "@/api/@tanstack/react-query.gen";
import { Button } from "@/components/ui/button";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useDisabledMonitorTypes } from "@/hooks/useDisabledMonitorTypes";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";
import { monitorTypeDisabledReason } from "@/app/monitors/monitor-types/disabled";
import { firstMonitorBody, parseMonitorUrl } from "@/lib/get-started";
import { commonMutationErrorHandler } from "@/lib/utils";
import { SkipSetup } from "./skip-setup";
import { StepCard } from "./step-card";

const createSchema = (t: (key: string) => string) =>
  z.object({
    url: z.string().refine((value) => parseMonitorUrl(value) !== null, t("get_started.monitor.url_invalid")),
  });

type MonitorStepValues = z.infer<ReturnType<typeof createSchema>>;

// Step 2: an HTTP(S) address to watch. Creating the monitor takes the full form's defaults and no
// probe choice, and hands its id on. On an install that turns HTTP monitors off there is nothing to
// create here, so the step says so and leads to the full form, where the other types are.
export function MonitorStep({ onCreated }: { onCreated: (monitorId: string) => void }) {
  const { t } = useLocalizedTranslation();
  const workspacePath = useWorkspacePath();
  const queryClient = useQueryClient();
  const httpDisabled = useDisabledMonitorTypes().includes("http");
  const form = useForm<MonitorStepValues>({
    resolver: zodResolver(createSchema(t)),
    defaultValues: { url: "" },
  });

  const create = useMutation({
    ...postMonitorsMutation(),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: getMonitorsInfiniteQueryKey() });
      queryClient.invalidateQueries({ queryKey: getMonitorsQueryKey() });
      onCreated(response.data.id);
    },
    onError: commonMutationErrorHandler(t("get_started.monitor.create_failed")),
  });

  const submit = (values: MonitorStepValues) => {
    const parsed = parseMonitorUrl(values.url);
    if (parsed) create.mutate({ body: firstMonitorBody(parsed) });
  };

  if (httpDisabled) {
    return (
      <StepCard step={2} title={t("get_started.monitor.title")} description={monitorTypeDisabledReason(t, "http")}>
        <div className="flex flex-wrap items-center gap-2">
          <Button asChild>
            <Link to={workspacePath("/monitors/new")}>{t("get_started.monitor.full_form")}</Link>
          </Button>
          <SkipSetup />
        </div>
      </StepCard>
    );
  }

  return (
    <StepCard step={2} title={t("get_started.monitor.title")} description={t("get_started.monitor.description")}>
      <Form {...form}>
        <form onSubmit={form.handleSubmit(submit)} noValidate className="flex flex-col gap-4">
          <FormField
            control={form.control}
            name="url"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("get_started.monitor.url_label")}</FormLabel>
                <FormControl>
                  <Input type="url" inputMode="url" placeholder="https://example.com" autoComplete="off" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className="flex flex-wrap items-center gap-2">
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? t("get_started.monitor.creating") : t("get_started.monitor.create")}
            </Button>
            <SkipSetup />
          </div>
        </form>
      </Form>
      <Link to={workspacePath("/monitors/new")} className="text-sm underline">
        {t("get_started.monitor.full_form")}
      </Link>
    </StepCard>
  );
}
