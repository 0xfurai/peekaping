import CreateEditNotificationChannel from "../components/create-edit-notification-channel";
import type { NotificationChannelFormValues } from "../channel-types/registry";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { postNotificationChannelsMutation } from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import type { NotificationChannelModel } from "@/api";
import { channelCreateBody } from "../channel-payload";
import { invalidateChannelLists } from "../channel-queries";

const CreateNotificationChannel = ({
  onSuccess,
}: {
  onSuccess: (notifier: NotificationChannelModel) => void;
}) => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();

  const createNotifierMutation = useMutation({
    ...postNotificationChannelsMutation(),
    onSuccess: (response) => {
      toast.success(t("notifications.messages.created_success"));

      invalidateChannelLists(queryClient);
      onSuccess(response.data);
    },
    onError: commonMutationErrorHandler(t("notifications.messages.create_failed")),
  });

  const handleSubmit = (data: NotificationChannelFormValues) => {
    createNotifierMutation.mutate({
      body: channelCreateBody(data),
    });
  };

  return <CreateEditNotificationChannel onSubmit={handleSubmit} />;
};

export default CreateNotificationChannel;
