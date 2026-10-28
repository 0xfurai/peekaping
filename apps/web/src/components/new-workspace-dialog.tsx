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
import { workspaceErrorMessage } from "@/lib/workspace-errors";

const createSchema = (t: (key: string) => string) =>
  z.object({
    name: z.string().trim().min(1, t("workspaces.name_required")).max(255),
  });

type NewWorkspaceFormValues = z.infer<ReturnType<typeof createSchema>>;

// Asks for a name, makes the workspace and goes to it. The user becomes its owner.
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

  const mutation = useMutation({
    ...postWorkspacesMutation(),
    onSuccess: async (response) => {
      onOpenChange(false);
      form.reset();
      await enterWorkspace(response.data.id);
    },
    onError: (error) =>
      toast.error(workspaceErrorMessage(error, t, t("workspaces.create_error"))),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <Form {...form}>
          <form
            onSubmit={form.handleSubmit((values) =>
              mutation.mutate({ body: { name: values.name } })
            )}
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
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => onOpenChange(false)}
              >
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? t("workspaces.creating") : t("workspaces.create")}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
