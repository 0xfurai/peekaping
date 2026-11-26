import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  getProbesQueryKey,
  postProbesByIdRotateTokenMutation,
} from "@/api/@tanstack/react-query.gen";
import type {
  ProbeProbeResponse,
  ProbeProbeWithTokenResponse,
} from "@/api/types.gen";
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

interface RotateProbeDialogProps {
  probe: ProbeProbeResponse;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Called with the probe and its new token, which the API shows only this once.
  onRotated: (probe: ProbeProbeWithTokenResponse) => void;
}

const RotateProbeDialog = ({
  probe,
  open,
  onOpenChange,
  onRotated,
}: RotateProbeDialogProps) => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();

  const rotateMutation = useMutation(postProbesByIdRotateTokenMutation());

  const confirmRotate = () => {
    rotateMutation.mutate(
      { path: { id: probe.id } },
      {
        onSuccess: (response) => {
          queryClient.invalidateQueries({ queryKey: getProbesQueryKey() });
          toast.success(t("probes.messages.rotated_successfully"));
          onOpenChange(false);
          onRotated(response.data);
        },
        onError: commonMutationErrorHandler(
          t("probes.messages.failed_to_rotate")
        ),
      }
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("probes.rotate_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("probes.rotate_dialog.description", { name: probe.name })}
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
            onClick={confirmRotate}
            disabled={rotateMutation.isPending}
          >
            {rotateMutation.isPending
              ? t("probes.rotate_dialog.rotating")
              : t("probes.rotate_dialog.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default RotateProbeDialog;
