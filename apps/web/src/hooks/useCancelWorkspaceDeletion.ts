import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { deleteWorkspaceDeletionMutation, getMeQueryKey } from "@/api/@tanstack/react-query.gen";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { workspaceErrorMessage } from "@/lib/workspace-errors";

// An owner cancels the scheduled deletion of the workspace in the address, from the banner or the
// settings card. /me is fetched again, since it carries the date and the hold the deletion put on
// the workspace; it is fetched after a failure too, which may mean someone cancelled it already.
export const useCancelWorkspaceDeletion = () => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const cancel = useMutation({
    ...deleteWorkspaceDeletionMutation(),
    onSuccess: () => toast.success(t("workspaces.deletion.cancelled")),
    onError: (error) => toast.error(workspaceErrorMessage(error, t, t("workspaces.deletion.cancel_failed"))),
    onSettled: () => queryClient.invalidateQueries({ queryKey: getMeQueryKey() }),
  });
  return { cancel: () => cancel.mutate({}), pending: cancel.isPending };
};
