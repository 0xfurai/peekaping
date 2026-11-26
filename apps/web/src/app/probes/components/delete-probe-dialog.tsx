import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  deleteProbesByIdMutation,
  getProbesQueryKey,
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

interface DeleteProbeDialogProps {
  probe: ProbeProbeResponse;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted: (probe: ProbeProbeResponse) => void;
}

const DeleteProbeDialog = ({
  probe,
  open,
  onOpenChange,
  onDeleted,
}: DeleteProbeDialogProps) => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();

  const deleteMutation = useMutation(deleteProbesByIdMutation());

  const confirmDelete = () => {
    deleteMutation.mutate(
      { path: { id: probe.id } },
      {
        onSuccess: () => {
          queryClient.invalidateQueries({ queryKey: getProbesQueryKey() });
          toast.success(t("probes.messages.deleted_successfully"));
          onOpenChange(false);
          onDeleted(probe);
        },
        onError: commonMutationErrorHandler(
          t("probes.messages.failed_to_delete")
        ),
      }
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("probes.delete_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("probes.delete_dialog.description", { name: probe.name })}
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
            variant="destructive"
            onClick={confirmDelete}
            disabled={deleteMutation.isPending}
          >
            {t("common.delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default DeleteProbeDialog;
