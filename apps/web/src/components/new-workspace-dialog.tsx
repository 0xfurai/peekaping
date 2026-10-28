import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { postWorkspacesMutation } from "@/api/@tanstack/react-query.gen";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useEnterWorkspace } from "@/hooks/useEnterWorkspace";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { createAndEnterWorkspace } from "@/lib/create-and-enter-workspace";
import { workspaceErrorMessage } from "@/lib/workspace-errors";

const createSchema = (t: (key: string) => string) =>
  z.object({
    name: z.string().trim().min(1, t("workspaces.name_required")).max(255),
  });

type NewWorkspaceFormValues = z.infer<ReturnType<typeof createSchema>>;

// Asks for a name, makes the workspace and goes to it. The user becomes its owner.
//
// Making it and going to it can fail apart. When the workspace was made but going to it failed,
// the dialog stays open, says so, and its button goes to that workspace again: it doesn't make a
// second one.
export function NewWorkspaceDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useLocalizedTranslation();
  const enterWorkspace = useEnterWorkspace();
  const form = useForm<NewWorkspaceFormValues>({
    resolver: zodResolver(createSchema(t)),
    defaultValues: { name: "" },
  });

  // The workspace a try made but could not enter, until it is entered or the dialog closes
  const [createdId, setCreatedId] = useState<string | null>(null);
  const [entering, setEntering] = useState(false);

  const mutation = useMutation({
    ...postWorkspacesMutation(),
    onError: (error) =>
      toast.error(workspaceErrorMessage(error, t, t("workspaces.create_error"))),
  });

  const close = (next: boolean) => {
    if (!next) {
      setCreatedId(null);
      form.reset();
    }
    onOpenChange(next);
  };

  const submit = async (values: NewWorkspaceFormValues) => {
    setEntering(true);
    try {
      const result = await createAndEnterWorkspace({
        createdId,
        create: async () =>
          (await mutation.mutateAsync({ body: { name: values.name } })).data.id,
        enter: enterWorkspace,
      });
      if (result.kind === "entered") {
        close(false);
      } else if (result.kind === "enter_failed") {
        console.error("Failed to enter the new workspace", result.error);
        setCreatedId(result.workspaceId);
      }
    } finally {
      setEntering(false);
    }
  };

  const busy = mutation.isPending || entering;
  const entryFailed = createdId !== null;

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(submit)}
            className="grid gap-4"
          >
            <DialogHeader>
              <DialogTitle>{t("workspaces.new_title")}</DialogTitle>
              <DialogDescription>{t("workspaces.new_description")}</DialogDescription>
            </DialogHeader>
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t("workspaces.name_label")}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t("workspaces.name_placeholder")}
                      autoComplete="off"
                      disabled={entryFailed}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            {entryFailed && (
              <p role="alert" className="text-sm text-destructive">
                {t("workspaces.entry_failed")}
              </p>
            )}
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => close(false)}
              >
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={busy}>
                {entryFailed
                  ? busy
                    ? t("workspaces.entering")
                    : t("workspaces.enter")
                  : busy
                    ? t("workspaces.creating")
                    : t("workspaces.create")}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
