import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  getMonitorsByIdQueryKey,
  patchMonitorsByIdMutation,
  postNotificationChannelsMutation,
} from "@/api/@tanstack/react-query.gen";
import CreateEditNotificationChannel from "@/app/notification-channels/components/create-edit-notification-channel";
import type { NotificationChannelFormValues } from "@/app/notification-channels/channel-types/registry";
import { Button } from "@/components/ui/button";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { invalidateChannelLists } from "@/app/notification-channels/channel-queries";
import { createChannelAndLink, getStartedChannelTypes, linkChannel as linkChannelToMonitor } from "@/lib/get-started";
import { commonMutationErrorHandler } from "@/lib/utils";
import { SkipSetup } from "./skip-setup";
import { StepCard } from "./step-card";

// Step 3: where alerts go. Email (sent by the install, when it has a sender), Telegram, Slack,
// Discord or a webhook, each with the form the channel pages use. Saving makes the channel the
// default for new monitors and links it to the monitor step 2 made. A channel that was made but not
// linked keeps the step open, with a retry that only links it and a way on without the link.
export function ChannelStep({ monitorId, onSaved }: { monitorId: string | null; onSaved: () => void }) {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const accountEmails = useMe().data?.data?.accountEmails === true;
  const createChannel = useMutation(postNotificationChannelsMutation());
  const linkChannel = useMutation(patchMonitorsByIdMutation());
  const [unlinkedChannelId, setUnlinkedChannelId] = useState<string | null>(null);

  const link = (id: string, channelId: string) =>
    linkChannel.mutateAsync({ path: { id }, body: { notification_ids: [channelId] } });

  // The monitor's page reads its channels afresh once the step has linked one
  const forgetMonitor = () => {
    if (monitorId) queryClient.removeQueries({ queryKey: getMonitorsByIdQueryKey({ path: { id: monitorId } }) });
  };

  const submit = async (values: NotificationChannelFormValues) => {
    try {
      const result = await createChannelAndLink({
        values,
        monitorId,
        create: async (body) => (await createChannel.mutateAsync({ body })).data,
        link,
      });
      invalidateChannelLists(queryClient);
      if (result.kind === "link_failed") {
        setUnlinkedChannelId(result.channelId);
        toast.error(t("get_started.channel.link_failed"));
        return;
      }
      forgetMonitor();
      toast.success(t("notifications.messages.created_success"));
      onSaved();
    } catch (error) {
      commonMutationErrorHandler(t("notifications.messages.create_failed"))(error);
    }
  };

  const retryLink = async () => {
    if (!monitorId || !unlinkedChannelId) return;
    const result = await linkChannelToMonitor({ monitorId, channelId: unlinkedChannelId, link });
    if (result.kind === "link_failed") {
      toast.error(t("get_started.channel.link_failed"));
      return;
    }
    forgetMonitor();
    toast.success(t("notifications.messages.created_success"));
    onSaved();
  };

  return (
    <StepCard step={3} title={t("get_started.channel.title")} description={t("get_started.channel.description")}>
      {unlinkedChannelId ? (
        <>
          <p className="text-sm" role="alert">
            {t("get_started.channel.link_failed")}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button onClick={retryLink} disabled={linkChannel.isPending}>
              {t("get_started.channel.retry_link")}
            </Button>
            <Button variant="outline" onClick={onSaved} disabled={linkChannel.isPending}>
              {t("get_started.channel.continue_unlinked")}
            </Button>
          </div>
        </>
      ) : (
        <CreateEditNotificationChannel
          onSubmit={submit}
          types={getStartedChannelTypes(accountEmails)}
          hideTitle
          isLoading={createChannel.isPending || linkChannel.isPending}
        />
      )}
      <div>
        <SkipSetup />
      </div>
    </StepCard>
  );
}
