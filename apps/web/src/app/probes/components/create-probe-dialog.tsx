import { useMutation, useQueryClient } from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import {
  getProbesQueryKey,
  postProbesMutation,
} from "@/api/@tanstack/react-query.gen";
import type { ProbeProbeWithTokenResponse } from "@/api/types.gen";
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
  createProbeSchema,
  type CreateProbeFormValues,
} from "./probe-form-fields";

interface CreateProbeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Called with the new probe and its token, which the API shows only this once.
  onCreated: (probe: ProbeProbeWithTokenResponse) => void;
}

const CreateProbeDialog = ({
  open,
  onOpenChange,
  onCreated,
}: CreateProbeDialogProps) => {
  const { t } = useLocalizedTranslation();
  const queryClient = useQueryClient();

  const form = useForm<CreateProbeFormValues>({
    resolver: zodResolver(createProbeSchema),
    defaultValues: {
      name: "",
      location: "",
    },
  });

  const createMutation = useMutation(postProbesMutation());

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) form.reset();
    onOpenChange(nextOpen);
  };

  const onSubmit = (values: CreateProbeFormValues) => {
    createMutation.mutate(
      {
        body: {
          name: values.name,
          location: values.location || undefined,
        },
      },
      {
        onSuccess: (response) => {
          queryClient.invalidateQueries({ queryKey: getProbesQueryKey() });
          toast.success(t("probes.messages.created_successfully"));
          handleOpenChange(false);
          onCreated(response.data);
        },
        onError: commonMutationErrorHandler(
          t("probes.messages.failed_to_create")
        ),
      }
    );
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("probes.create_dialog.title")}</DialogTitle>
          <DialogDescription>
            {t("probes.create_dialog.description")}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            <ProbeFormFields />
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => handleOpenChange(false)}
              >
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={createMutation.isPending}>
                {createMutation.isPending
                  ? t("probes.create_dialog.creating")
                  : t("probes.create_dialog.submit")}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
};

export default CreateProbeDialog;
