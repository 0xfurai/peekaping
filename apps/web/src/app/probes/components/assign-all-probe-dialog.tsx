import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  getMonitorsQueryKey,
  getProbesQueryKey,
  postProbesByIdAssignAllMonitorsMutation,
} from "@/api/@tanstack/react-query.gen";
import type { ProbeProbeResponse } from "@/api/types.gen";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { commonMutationErrorHandler } from "@/lib/utils";
import AssignAllWarning from "./assign-all-warning";

interface AssignAllProbeDialogProps {
  probe: ProbeProbeResponse;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const AssignAllProbeDialog = ({
  probe,
  open,
  onOpenChange,
}: AssignAllProbeDialogProps) => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();

  const assignMutation = useMutation(postProbesByIdAssignAllMonitorsMutation());

  const confirmAssign = () => {
    assignMutation.mutate(
      { path: { id: probe.id } },
      {
        onSuccess: (response) => {
          queryClient.invalidateQueries({ queryKey: getProbesQueryKey() });
          queryClient.invalidateQueries({ queryKey: getMonitorsQueryKey() });
          // A probe with no instance yet is only marked: the monitors are added when it connects.
          toast.success(
            response.data.pending
              ? t("probes.messages.assign_pending")
              : t("probes.messages.assigned_successfully", {
                  added: response.data.added,
                })
          );
          onOpenChange(false);
        },
        onError: commonMutationErrorHandler(
          t("probes.messages.failed_to_assign")
        ),
      }
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("probes.assign_all_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("probes.assign_all_dialog.description", { name: probe.name })}{" "}
            <AssignAllWarning />
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="button"
            onClick={confirmAssign}
            disabled={assignMutation.isPending}
          >
            {assignMutation.isPending
              ? t("probes.assign_all_dialog.assigning")
              : t("probes.assign_all_dialog.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default AssignAllProbeDialog;
