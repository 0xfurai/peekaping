import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  getMonitorsByIdQueryKey,
  patchMonitorsByIdMutation,
  postNotificationChannelsMutation,
} from "@/api/@tanstack/react-query.gen";
import CreateEditNotificationChannel from "@/app/notification-channels/components/create-edit-notification-channel";
import type { NotificationChannelFormValues } from "@/app/notification-channels/channel-types/registry";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { invalidateChannelLists } from "@/app/notification-channels/channel-queries";
import { createChannelAndLink, getStartedChannelTypes } from "@/lib/get-started";
import { commonMutationErrorHandler } from "@/lib/utils";
import { SkipSetup } from "./skip-setup";
import { StepCard } from "./step-card";

// Step 3: where alerts go. Email (sent by the install, when it has a sender), Telegram, Slack,
// Discord or a webhook, each with the form the channel pages use. Saving makes the channel the
// default for new monitors and links it to the monitor step 2 made.
export function ChannelStep({ monitorId, onSaved }: { monitorId: string | null; onSaved: () => void }) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const accountEmails = useMe().data?.data?.accountEmails === true;
  const createChannel = useMutation(postNotificationChannelsMutation());
  const linkChannel = useMutation(patchMonitorsByIdMutation());

  const submit = async (values: NotificationChannelFormValues) => {
    try {
      const result = await createChannelAndLink({
        values,
        monitorId,
        create: async (body) => (await createChannel.mutateAsync({ body })).data,
        link: (id, channelId) => linkChannel.mutateAsync({ path: { id }, body: { notification_ids: [channelId] } }),
      });
      invalidateChannelLists(queryClient);
      if (monitorId) queryClient.removeQueries({ queryKey: getMonitorsByIdQueryKey({ path: { id: monitorId } }) });
      if (result.kind === "link_failed") toast.error(t("get_started.channel.link_failed"));
      else toast.success(t("notifications.messages.created_success"));
      onSaved();
    } catch (error) {
      commonMutationErrorHandler(t("notifications.messages.create_failed"))(error);
    }
  };

  return (
    <StepCard step={3} title={t("get_started.channel.title")} description={t("get_started.channel.description")}>
      <CreateEditNotificationChannel
        onSubmit={submit}
        types={getStartedChannelTypes(accountEmails)}
        hideTitle
        isLoading={createChannel.isPending || linkChannel.isPending}
      />
      <div>
        <SkipSetup />
      </div>
    </StepCard>
  );
}
