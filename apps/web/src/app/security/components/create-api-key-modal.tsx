// MARK: - Imports
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
import { toast } from "sonner";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useTimezone } from "@/context/timezone-context";
import { convertDateTimeLocalToUTC } from "@/lib/timezone-utils";
import { z } from "zod";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  getApiKeysOptions,
  postApiKeysMutation,
} from "@/api/@tanstack/react-query.gen";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  ApiKeyCreateApiKeyDto,
  PostApiKeysResponse,
} from "@/api/types.gen";

// MARK: - Schema & Types
const createAPIKeySchema = z.object({
  name: z.string().min(1, "Name is required").max(255, "Name too long"),
  expiresAt: z
    .string()
    .optional()
    .refine(
      (val) => {
        if (val === undefined || val === "") return true; // Optional field
        const date = new Date(val);
        return !isNaN(date.getTime()) && date > new Date(); // Must be valid date and in future
      },
      {
        message: "Must be a valid future date",
      }
    ),
  maxUsageCount: z
    .string()
    .optional()
    .refine(
      (val) => {
        if (val === undefined || val === "") return true; // Optional field
        const num = parseInt(val, 10);
        return !isNaN(num) && num > 0; // Must be positive integer
      },
      {
        message: "Must be a positive number",
      }
    ),
});

type CreateAPIKeyForm = z.infer<typeof createAPIKeySchema>;

// MARK: - Component Interface
interface CreateAPIKeyModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: (response: PostApiKeysResponse) => void;
}

// MARK: - Main Component
const CreateAPIKeyModal = ({
  open,
  onOpenChange,
  onSuccess,
}: CreateAPIKeyModalProps) => {
  // MARK: - Hooks & State
  const { t } = useLocalizedTranslation();
  const { timezone: userTimezone } = useTimezone();
  const queryClient = useQueryClient();

  // MARK: - Form Setup & Mutations
  const form = useForm<CreateAPIKeyForm>({
    resolver: zodResolver(createAPIKeySchema),
    defaultValues: {
      name: "",
      expiresAt: "",
      maxUsageCount: "",
    },
  });

  const createMutation = useMutation(postApiKeysMutation());

  // MARK: - Event Handlers
  const handleCreateError = () => {
    toast.error(t("security.api_keys.messages.failed_to_create"));
  };

  const onSubmit = (data: CreateAPIKeyForm) => {
    const createData: ApiKeyCreateApiKeyDto = {
      name: data.name,
      expires_at: data.expiresAt
        ? convertDateTimeLocalToUTC(data.expiresAt, userTimezone)
        : undefined,
      max_usage_count: data.maxUsageCount
        ? parseInt(data.maxUsageCount, 10)
        : undefined,
    };

    // Additional validation before submission
    if (
      createData.expires_at &&
      new Date(createData.expires_at) <= new Date()
    ) {
      form.setError("expiresAt", {
        type: "manual",
        message: "Expiration date must be in the future",
      });
      return;
    }

    createMutation.mutate(
      {
        body: createData,
      },
      {
        onSuccess: (response) => {
          onSuccess(response);
          onOpenChange(false);
          queryClient.invalidateQueries({
            queryKey: getApiKeysOptions().queryKey,
          });
          toast.success(t("security.api_keys.messages.created_successfully"));
        },
        onError: handleCreateError,
      }
    );
  };

  // MARK: - Render
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t("security.api_keys.create_dialog.title")}
          </DialogTitle>
          <DialogDescription>
            {t("security.api_keys.create_dialog.description")}
          </DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t("security.api_keys.create_dialog.form.name_label")}
                  </FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      placeholder={t(
                        "security.api_keys.create_dialog.form.name_placeholder"
                      )}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="expiresAt"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t("security.api_keys.create_dialog.form.expires_at_label")}
                  </FormLabel>
                  <FormControl>
                    <Input {...field} type="datetime-local" />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="maxUsageCount"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t("security.api_keys.create_dialog.form.max_usage_label")}
                  </FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      type="number"
                      min="1"
                      placeholder={t(
                        "security.api_keys.create_dialog.form.max_usage_placeholder"
                      )}
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
                {t("security.api_keys.create_dialog.buttons.cancel")}
              </Button>
              <Button type="submit" disabled={createMutation.isPending}>
                {createMutation.isPending
                  ? t("security.api_keys.create_dialog.buttons.creating")
                  : t("security.api_keys.create_dialog.buttons.create")}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
};

export default CreateAPIKeyModal;
