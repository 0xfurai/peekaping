import { useEffect } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import {
  getProbesQueryKey,
  putProbesByIdMutation,
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
import { Form } from "@/components/ui/form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { commonMutationErrorHandler } from "@/lib/utils";
import ProbeFormFields, {
  editProbeSchema,
  type EditProbeFormValues,
} from "./probe-form-fields";
import { LOCAL_PROBE_ID } from "./local-probe";

const valuesOf = (probe: ProbeProbeResponse): EditProbeFormValues => ({
  name: probe.name,
  location: probe.location,
  disabled: probe.disabled,
  notification_ids: probe.notification_ids,
});

interface EditProbeDialogProps {
  probe: ProbeProbeResponse;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const EditProbeDialog = ({ probe, open, onOpenChange }: EditProbeDialogProps) => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();
  const isLocal = probe.id === LOCAL_PROBE_ID;

  const form = useForm<EditProbeFormValues>({
    resolver: zodResolver(editProbeSchema),
    defaultValues: valuesOf(probe),
  });

  // Start from the probe's current values every time the dialog opens, so an
  // edit that was cancelled doesn't come back.
  const { reset } = form;
  useEffect(() => {
    if (open) reset(valuesOf(probe));
  }, [open, probe, reset]);

  const updateMutation = useMutation(putProbesByIdMutation());

  const onSubmit = (values: EditProbeFormValues) => {
    updateMutation.mutate(
      {
        path: { id: probe.id },
        body: {
          name: values.name,
          location: values.location,
          notification_ids: values.notification_ids,
          // The API answers 409 when the local probe is disabled.
          ...(isLocal ? {} : { disabled: values.disabled }),
        },
      },
      {
        onSuccess: () => {
          queryClient.invalidateQueries({ queryKey: getProbesQueryKey() });
          toast.success(t("probes.messages.updated_successfully"));
          onOpenChange(false);
        },
        onError: commonMutationErrorHandler(
          t("probes.messages.failed_to_update")
        ),
      }
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("probes.edit_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("probes.edit_dialog.description", { name: probe.name })}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            <ProbeFormFields showDisabled={!isLocal} showNotifications />
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
              >
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={updateMutation.isPending}>
                {updateMutation.isPending
                  ? t("common.saving")
                  : t("common.save")}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
};

export default EditProbeDialog;
