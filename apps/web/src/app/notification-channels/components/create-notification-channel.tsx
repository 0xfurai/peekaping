import CreateEditNotificationChannel, {
  type NotificationForm,
} from "../components/create-edit-notification-channel";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getNotificationChannelsInfiniteQueryKey,
  postNotificationChannelsMutation,
} from "@/api/@tanstack/react-query.gen";
import { toast } from "sonner";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import type {
  NotificationChannelCreateUpdateDto,
  NotificationChannelModel,
} from "@/api";

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

      queryClient.invalidateQueries({
        queryKey: getNotificationChannelsInfiniteQueryKey(),
      });
      onSuccess(response.data);
    },
    onError: commonMutationErrorHandler(t("notifications.messages.create_failed")),
  });

  const handleSubmit = (data: NotificationForm) => {
    const payload: NotificationChannelCreateUpdateDto = {
      name: data.name,
      type: data.type,
      config: JSON.stringify(data),
      active: true,
      is_default: false,
    };

    createNotifierMutation.mutate({
      body: payload,
    });
  };

  return <CreateEditNotificationChannel onSubmit={handleSubmit} />;
};

export default CreateNotificationChannel;
