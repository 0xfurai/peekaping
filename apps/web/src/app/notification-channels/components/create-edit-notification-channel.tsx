import { zodResolver } from "@hookform/resolvers/zod";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { z } from "zod";
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
import * as SmtpForm from "../integrations/smtp-form";
import { Button } from "@/components/ui/button";
import { postNotificationChannelsTestMutation } from "@/api/@tanstack/react-query.gen";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import * as TelegramForm from "../integrations/telegram-form";
import * as WebhookForm from "../integrations/webhook-form";
import * as SlackForm from "../integrations/slack-form";
import * as NtfyForm from "../integrations/ntfy-form";
import * as PagerDutyForm from "../integrations/pagerduty-form";
import * as OpsgenieForm from "../integrations/opsgenie-form";
import * as GoogleChatForm from "../integrations/google-chat-form";
import * as GrafanaOncallForm from "../integrations/grafana-oncall-form";
import * as SignalForm from "../integrations/signal-form";
import * as GotifyForm from "../integrations/gotify-form";
import * as PushoverForm from "../integrations/pushover-form";
import * as MattermostForm from "../integrations/mattermost-form";
import * as MatrixForm from "../integrations/matrix-form";
import * as DiscordForm from "../integrations/discord-form";
import * as WecomForm from "../integrations/wecom-form";
import * as WhatsAppForm from "../integrations/whatsapp-form";
import * as TwilioForm from "../integrations/twilio-form";
import * as SendGridForm from "../integrations/sendgrid-form";
import * as PushbulletForm from "../integrations/pushbullet-form";
import * as PagerTreeForm from "../integrations/pagertree";
import * as LineForm from "../integrations/line-form";
import * as TeamsForm from "../integrations/teams-form";

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

const typeFormRegistry = {
  smtp: SmtpForm,
  telegram: TelegramForm,
  webhook: WebhookForm,
  slack: SlackForm,
  ntfy: NtfyForm,
  pagerduty: PagerDutyForm,
  opsgenie: OpsgenieForm,
  google_chat: GoogleChatForm,
  grafana_oncall: GrafanaOncallForm,
  signal: SignalForm,
  gotify: GotifyForm,
  pushover: PushoverForm,
  mattermost: MattermostForm,
  matrix: MatrixForm,
  discord: DiscordForm,
  wecom: WecomForm,
  whatsapp: WhatsAppForm,
  twilio: TwilioForm,
  sendgrid: SendGridForm,
  pushbullet: PushbulletForm,
  pagertree: PagerTreeForm,
  line: LineForm,
  teams: TeamsForm,
};

const notificationSchema = z
  .object({
    name: z.string().min(1, {
      message: "Name is required",
    }),
  })
  .and(
    z.discriminatedUnion("type", [
      SmtpForm.schema,
      TelegramForm.schema,
      WebhookForm.schema,
      SlackForm.schema,
      NtfyForm.schema,
      PagerDutyForm.schema,
      OpsgenieForm.schema,
      GoogleChatForm.schema,
      GrafanaOncallForm.schema,
      SignalForm.schema,
      GotifyForm.schema,
      PushoverForm.schema,
      MattermostForm.schema,
      MatrixForm.schema,
      DiscordForm.schema,
      WecomForm.schema,
      WhatsAppForm.schema,
      TwilioForm.schema,
      SendGridForm.schema,
      PushbulletForm.schema,
      PagerTreeForm.schema,
      LineForm.schema,
      TeamsForm.schema,
    ] as const)
  )
  .superRefine((data, ctx) => {
    if (data.type === "slack") SlackForm.refine(data, ctx);
  });

export type NotificationForm = z.infer<typeof notificationSchema>;

// validate map components
Object.values(typeFormRegistry).forEach((component) => {
  if (typeof component.default !== "function") {
    throw new Error("Type components must be exported as default");
  }
  if (!component.displayName) {
    throw new Error("Type components must have a displayName");
  }
  if (!component.schema) {
    throw new Error("Type components must have a schema");
  }
  if (!component.defaultValues) {
    throw new Error("Type components must have default values");
  }
});

const notificationTypes = Object.keys(typeFormRegistry).map((key) => ({
  label: typeFormRegistry[key as keyof typeof typeFormRegistry].displayName,
  value: key,
}));

export default function CreateEditNotificationChannel({
  onSubmit,
  initialValues = {
    name: "",
    ...SmtpForm.defaultValues,
  },
  isLoading = false,
  mode = "create",
  channelId,
}: {
  onSubmit: (data: NotificationForm) => void;
  initialValues?: NotificationForm;
  isLoading?: boolean;
  mode?: "create" | "edit";
  // The saved channel being edited
  channelId?: string;
}) {
  const { t } = useLocalizedTranslation();
  // A saved config lacks any field added to its type since it was stored
  const [openingValues] = useState(() => ({
    ...typeFormRegistry[initialValues.type]?.defaultValues,
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
  const resolver = useMemo<Resolver<NotificationForm>>(() => {
    const validate = zodResolver(notificationSchema);
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
      return { values: {}, errors: errors as FieldErrors<NotificationForm> };
    };
  }, [savedValues]);
  const baseForm = useForm<NotificationForm>({
    resolver,
    defaultValues: openingValues,
  });

  const type = baseForm.watch("type");

  const TypeFormComponent =
    typeFormRegistry[type as keyof typeof typeFormRegistry]?.default || null;

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
        : typeFormRegistry[type as keyof typeof typeFormRegistry]
            .defaultValues || {};
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
                if (!val) return;

                baseForm.setValue(
                  "type",
                  val as
                    | "smtp"
                    | "telegram"
                    | "webhook"
                    | "slack"
                    | "ntfy"
                    | "pagerduty"
                    | "signal"
                    | "google_chat"
                    | "grafana_oncall"
                    | "opsgenie"
                    | "gotify"
                    | "pushover"
                    | "mattermost"
                    | "matrix"
                    | "discord"
                    | "wecom"
                    | "whatsapp"
                    | "twilio"
                    | "sendgrid"
                    | "pushbullet"
                    | "pagertree"
                    | "line"
                    | "teams"
                );
              }}
              value={type}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={t("notifications.placeholders.select_type")} />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {notificationTypes.map((item) => (
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
