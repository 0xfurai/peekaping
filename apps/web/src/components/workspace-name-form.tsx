import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const createSchema = (t: (key: string) => string) =>
  z.object({ name: z.string().trim().min(1, t("workspaces.name_required")).max(255) });

type WorkspaceNameValues = z.infer<ReturnType<typeof createSchema>>;

// A field with the workspace's name and a submit button. It only asks; the caller saves
// (see useSaveWorkspaceName) and shows what went wrong.
export function WorkspaceNameForm({
  name,
  submitLabel,
  pending,
  onSubmit,
  children,
}: {
  name: string;
  submitLabel: string;
  pending: boolean;
  onSubmit: (name: string) => void;
  // More buttons, beside the submit button
  children?: React.ReactNode;
}) {
  const { t } = useLocalizedTranslation();
  const form = useForm<WorkspaceNameValues>({
    resolver: zodResolver(createSchema(t)),
    defaultValues: { name },
  });
  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit((values) => onSubmit(values.name))} className="flex flex-col gap-4 max-w-xs">
        <FormField
          control={form.control}
          name="name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t("workspaces.name_label")}</FormLabel>
              <FormControl>
                <Input autoComplete="off" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className="flex gap-2">
          <Button type="submit" disabled={pending}>
            {submitLabel}
          </Button>
          {children}
        </div>
      </form>
    </Form>
  );
}
