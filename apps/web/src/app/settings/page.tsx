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
import { Link } from "react-router-dom";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useCurrentAccess } from "@/hooks/useCurrentAccess";
import { useWorkspacePath } from "@/hooks/useWorkspacePath";
import { Label } from "@/components/ui/label";
import { WorkspaceNameForm } from "@/components/workspace-name-form";
import { useCurrentWorkspace } from "@/hooks/useCurrentWorkspace";
import { useSaveWorkspaceName } from "@/hooks/useSaveWorkspaceName";
import { workspaceErrorMessage } from "@/lib/workspace-errors";

// The workspace's name, which an admin or owner changes. A member doesn't get the card: the
// switcher shows them the name.
const WorkspaceNameSetting = () => {
  const { t } = useLocalizedTranslation();
  const workspace = useCurrentWorkspace();
  const { save, pending } = useSaveWorkspaceName();
  if (!workspace) return null;

  const submit = async (name: string) => {
    try {
      const result = await save(name, workspace.name);
      if (result.kind === "renamed") toast.success(t("workspaces.rename_success"));
    } catch (error) {
      console.error(error);
      toast.error(workspaceErrorMessage(error, t, t("workspaces.rename_error")));
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("settings.workspace_name.title")}</CardTitle>
        <CardDescription>{t("settings.workspace_name.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        {/* Keyed by the name so that a rename shown in /me resets the field to it */}
        <WorkspaceNameForm
          key={workspace.name}
          name={workspace.name}
          submitLabel={pending ? t("common.saving") : t("common.save")}
          pending={pending}
          onSubmit={submit}
        />
      </CardContent>
    </Card>
  );
};

const KeepDataPeriodSetting = () => {
  const { t } = useLocalizedTranslation();
  const keepDataPeriodSchema = z.object({
    value: z.coerce
      .number()
      .int()
      .min(1, { message: t("settings.validation.min_days") }),
  });
  const KEEP_DATA_KEY = "KEEP_DATA_PERIOD_DAYS";
  // A member sees the period but only an admin or owner changes it
  const { canManageWorkspace } = useCurrentAccess();
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
        ) : !canManageWorkspace ? (
          <div className="flex flex-col gap-2 max-w-xs">
            <Label htmlFor="keep-data-period">
              {t("settings.data_retention.days_label")}
            </Label>
            <Input
              id="keep-data-period"
              type="number"
              readOnly
              value={data?.data?.value ?? ""}
            />
            <p className="text-sm text-muted-foreground">
              {t("settings.data_retention.read_only")}
            </p>
          </div>
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


const SettingsPage = () => {
  const { t } = useLocalizedTranslation();
  const { canManageWorkspace } = useCurrentAccess();
  const workspacePath = useWorkspacePath();
  return (
    <Layout pageName={t("navigation.settings")}>
      <div className="max-w-xl flex flex-col gap-6">
        {canManageWorkspace && <WorkspaceNameSetting />}
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
        <Card>
          <CardHeader>
            <CardTitle>{t("members.settings_card.title")}</CardTitle>
            <CardDescription>
              {t("members.settings_card.description")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button asChild variant="outline">
              <Link to={workspacePath("/settings/members")}>
                {t("members.settings_card.open")}
              </Link>
            </Button>
          </CardContent>
        </Card>
        {/* The usage ping and error reports belong to the install: they are on Admin → Settings */}
      </div>
    </Layout>
  );
};

export default SettingsPage;
