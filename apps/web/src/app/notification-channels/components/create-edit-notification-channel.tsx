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
import { useForm } from "react-hook-form";
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

import { useEffect } from "react";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

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
    ] as const)
  );

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
}: {
  onSubmit: (data: NotificationForm) => void;
  initialValues?: NotificationForm;
  isLoading?: boolean;
  mode?: "create" | "edit";
}) {
  const { t } = useLocalizedTranslation();
  const baseForm = useForm<NotificationForm>({
    resolver: zodResolver(notificationSchema),
    defaultValues: initialValues,
  });

  const type = baseForm.watch("type");

  const TypeFormComponent =
    typeFormRegistry[type as keyof typeof typeFormRegistry]?.default || null;

  useEffect(() => {
    if (type === initialValues?.type) return;
    if (!type) return;

    const values = baseForm.getValues();
    baseForm.reset({
      ...values,
      ...(typeFormRegistry[type as keyof typeof typeFormRegistry]
        .defaultValues || {}),
    });
  }, [type, baseForm, initialValues?.type]);

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
    const { name, type, ...typeConfig } = values;
    testNotifierMutation.mutate({
      body: {
        name,
        type,
        config: JSON.stringify(typeConfig),
        active: true,
        is_default: false,
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
          onSubmit={baseForm.handleSubmit(onSubmit)}
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

          {TypeFormComponent && <TypeFormComponent />}

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
