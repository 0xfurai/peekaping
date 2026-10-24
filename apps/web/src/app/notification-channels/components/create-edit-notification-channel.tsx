import { zodResolver } from "@hookform/resolvers/zod";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { useForm, type FieldErrors, type Resolver } from "react-hook-form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { postNotificationChannelsTestMutation } from "@/api/@tanstack/react-query.gen";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";

import { useEffect, useMemo, useRef, useState } from "react";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import {
  REENTER_ERROR_MESSAGE,
  REENTER_ERROR_TYPE,
  secretsToReenter,
  withoutSavedSecrets,
  withoutUnaskedSecrets,
} from "../secrets";
import { SavedSecrets } from "./saved-secrets";
import {
  channelFormSchema,
  channelTypeOptions,
  channelTypes,
  getChannelType,
  isChannelType,
  type NotificationChannelFormValues,
} from "../channel-types/registry";

export default function CreateEditNotificationChannel({
  onSubmit,
  initialValues = {
    name: "",
    ...channelTypes[0].defaultValues,
  },
  isLoading = false,
  mode = "create",
  channelId,
}: {
  onSubmit: (data: NotificationChannelFormValues) => void;
  initialValues?: NotificationChannelFormValues;
  isLoading?: boolean;
  mode?: "create" | "edit";
  // The saved channel being edited
  channelId?: string;
}) {
  const { t } = useLocalizedTranslation();
  // A saved config lacks any field added to its type since it was stored
  const [openingValues] = useState(() => ({
    ...getChannelType(initialValues.type)?.defaultValues,
    ...initialValues,
  }));
  // The values the saved channel opened the form with: its secrets are the placeholder
  const savedValues = mode === "edit" ? openingValues : undefined;
  // The secrets whose fields are on the form, kept up to date by SavedSecrets
  const shownSecrets = useRef(new Set<string>());
  // The server keeps a saved secret only while the channel sends to the same place. A secret
  // still holding the placeholder after an endpoint moved is refused with an error on its field,
  // until it is typed again or removed. One whose field isn't shown is not asked for: it is sent
  // empty (see withoutUnaskedSecrets).
  const resolver = useMemo<Resolver<NotificationChannelFormValues>>(() => {
    const validate = zodResolver(channelFormSchema);
    return async (values, context, options) => {
      const result = await validate(values, context, options);
      const reenter = savedValues
        ? secretsToReenter(values, savedValues).filter((key) => shownSecrets.current.has(key))
        : [];
      if (reenter.length === 0) return result;

      const errors: Record<string, unknown> = { ...result.errors };
      for (const key of reenter) {
        errors[key] = { type: REENTER_ERROR_TYPE, message: REENTER_ERROR_MESSAGE };
      }
      return { values: {}, errors: errors as FieldErrors<NotificationChannelFormValues> };
    };
  }, [savedValues]);
  const baseForm = useForm<NotificationChannelFormValues>({
    resolver,
    defaultValues: openingValues,
  });

  const type = baseForm.watch("type");

  const TypeFormComponent = getChannelType(type)?.Form ?? null;

  const previousType = useRef(type);
  useEffect(() => {
    if (!type || type === previousType.current) return;
    previousType.current = type;

    // A saved secret shows as a placeholder, which only stands for a secret of the saved
    // channel's type: no other type keeps it
    const values = withoutSavedSecrets(baseForm.getValues());
    // Back to the type the form opened with, it has the values the form opened with
    const typeValues =
      type === openingValues.type
        ? openingValues
        : getChannelType(type)?.defaultValues ?? {};
    baseForm.reset({ ...values, ...typeValues, name: values.name });
  }, [type, baseForm, openingValues]);

  const testNotifierMutation = useMutation({
    ...postNotificationChannelsTestMutation(),
    onSuccess: () => {
      toast.success(t("notifications.messages.test_success"));
    },
    onError: commonMutationErrorHandler(t("notifications.messages.test_failed")),
  });

  // Handle test notification
  function handleTest() {
    const values = baseForm.getValues();

    // The same refusal as saving: a saved secret that can't be kept is not sent
    const reenter = savedValues
      ? secretsToReenter(values, savedValues).filter((key) => shownSecrets.current.has(key))
      : [];
    if (reenter.length > 0) {
      reenter.forEach((key, index) =>
        baseForm.setError(
          key as Parameters<typeof baseForm.setError>[0],
          { type: REENTER_ERROR_TYPE, message: REENTER_ERROR_MESSAGE },
          { shouldFocus: index === 0 }
        )
      );
      return;
    }

    const { name, type, ...typeConfig } = withoutUnaskedSecrets(
      values,
      savedValues,
      shownSecrets.current
    );
    testNotifierMutation.mutate({
      body: {
        name,
        type,
        config: JSON.stringify(typeConfig),
        active: true,
        is_default: false,
        // The saved channel: secrets the form leaves as placeholders are the ones it has stored
        id: channelId,
      },
    });
  }

  return (
    <div className="flex flex-col gap-6 max-w-[600px]">
      <CardTitle className="text-xl">
        {mode === "edit" ? t("notifications.edit_title") : t("notifications.create_title")}
      </CardTitle>

      <Form {...baseForm}>
        <form
          onSubmit={baseForm.handleSubmit((data) =>
            onSubmit(withoutUnaskedSecrets(data, savedValues, shownSecrets.current))
          )}
          className="space-y-6 max-w-[600px]"
        >
          <FormItem>
            <FormLabel>{t("notifications.labels.type")}</FormLabel>
            <Select
              onValueChange={(val) => {
                if (isChannelType(val)) baseForm.setValue("type", val);
              }}
              value={type}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.placeholders.select_type")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {channelTypeOptions.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>

          <FormField
            control={baseForm.control}
            name="name"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("notifications.labels.friendly_name")}</FormLabel>
                <FormControl>
                  <Input placeholder={t("notifications.labels.friendly_name")} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <SavedSecrets saved={savedValues} shown={shownSecrets}>
            {TypeFormComponent && <TypeFormComponent />}
          </SavedSecrets>

          <div className="flex gap-2">
            <Button type="submit" disabled={isLoading}>
              {isLoading ? t("common.saving") : t("common.save")}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleTest}
              disabled={isLoading || testNotifierMutation.isPending}
            >
              {testNotifierMutation.isPending ? t("notifications.actions.testing") : t("notifications.actions.test")}
            </Button>
          </div>
        </form>
      </Form>
    </div>
  );
}
