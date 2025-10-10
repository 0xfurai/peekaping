import { useState } from "react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { TypographyH4 } from "@/components/ui/typography";
import { Copy, Trash2, Plus, Key } from "lucide-react";
import { toast } from "sonner";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useTimezone } from "@/context/timezone-context";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import { convertDateTimeLocalToUTC } from "@/lib/timezone-utils";
import { z } from "zod";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  getApiKeysOptions,
  postApiKeysMutation,
  deleteApiKeysByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  ApiKeyApiKeyResponse,
  ApiKeyCreateApiKeyDto,
  PostApiKeysResponse,
} from "@/api/types.gen";

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

// MARK: CreateAPIKeyModal
const CreateAPIKeyModal = ({
  open,
  onOpenChange,
  onSuccess,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: (response: PostApiKeysResponse) => void;
}) => {
  const { t } = useLocalizedTranslation();
  const { timezone: userTimezone } = useTimezone();
  const queryClient = useQueryClient();

  const form = useForm<CreateAPIKeyForm>({
    resolver: zodResolver(createAPIKeySchema),
    defaultValues: {
      name: "",
      expiresAt: "",
      maxUsageCount: "",
    },
  });

  const createMutation = useMutation(postApiKeysMutation());

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

// MARK: APIKeys Component
const APIKeys = () => {
  const { t } = useLocalizedTranslation();
  const { timezone: userTimezone } = useTimezone();
  const queryClient = useQueryClient();
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [newKeyState, setNewKeyState] = useState<{
    token: string | null;
    id: string | null;
  }>({
    token: null,
    id: null,
  });

  const { data: apiKeysResponse, isLoading } = useQuery(getApiKeysOptions());
  const deleteMutation = useMutation(deleteApiKeysByIdMutation());

  const apiKeys: ApiKeyApiKeyResponse[] = apiKeysResponse?.data || [];

  const handleCreateSuccess = (response: PostApiKeysResponse) => {
    setNewKeyState({
      token: response.data.token,
      id: response.data.id,
    });
  };

  const handleDelete = (id: string) => {
    if (confirm(t("security.api_keys.messages.delete_confirm"))) {
      deleteMutation.mutate(
        {
          path: { id },
        },
        {
          onSuccess: () => {
            // Invalidate and refetch the API keys query to update the UI
            queryClient.invalidateQueries({
              queryKey: getApiKeysOptions().queryKey,
            });
            toast.success(t("security.api_keys.messages.deleted_successfully"));
          },
          onError: () => {
            toast.error(t("security.api_keys.messages.failed_to_delete"));
          },
        }
      );
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    toast.success(t("security.api_keys.messages.copy_success"));
  };

  const dismissNewToken = () => {
    setNewKeyState({
      token: null,
      id: null,
    });
  };

  const formatDate = (dateString: string | null | undefined) => {
    if (!dateString) return t("security.api_keys.table.never");
    // Convert UTC date from backend to user's timezone for display
    return formatDateToTimezone(dateString, userTimezone, {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    });
  };

  const formatDateTime = (dateString: string | null | undefined) => {
    if (!dateString) return t("security.api_keys.table.never");
    // Convert UTC date from backend to user's timezone for display
    return formatDateToTimezone(dateString, userTimezone, {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  };

  const isExpired = (expiresAt: string | null | undefined) => {
    if (!expiresAt) return false;
    // Compare UTC times for accurate expiration check
    return new Date(expiresAt) < new Date();
  };

  const isUsageExceeded = (
    usageCount: number,
    maxUsageCount: number | null | undefined
  ) => {
    if (!maxUsageCount) return false;
    return usageCount >= maxUsageCount;
  };

  // MARK: Render
  return (
    <div className="flex flex-col gap-4 mt-8">
      <div className="flex items-center justify-between">
        <TypographyH4>{t("security.api_keys.title")}</TypographyH4>
        <Button onClick={() => setShowCreateDialog(true)}>
          <Plus className="h-4 w-4 mr-2" />
          {t("security.api_keys.create_button")}
        </Button>
      </div>

      <CreateAPIKeyModal
        open={showCreateDialog}
        onOpenChange={setShowCreateDialog}
        onSuccess={handleCreateSuccess}
      />

      {newKeyState.token && (
        <Alert>
          <Key className="h-4 w-4" />
          <AlertTitle>{t("security.api_keys.success_alert.title")}</AlertTitle>
          <AlertDescription>
            <div className="mt-2">
              <p className="text-sm text-muted-foreground mb-2">
                {t("security.api_keys.success_alert.description")}
              </p>
              <div className="flex items-center gap-2">
                <code className="bg-muted px-2 py-1 rounded text-sm font-mono">
                  {newKeyState.token}
                </code>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() =>
                    newKeyState.token && copyToClipboard(newKeyState.token)
                  }
                >
                  <Copy className="h-4 w-4" />
                </Button>
                <Button size="sm" variant="ghost" onClick={dismissNewToken}>
                  Dismiss
                </Button>
              </div>
            </div>
          </AlertDescription>
        </Alert>
      )}

      {isLoading ? (
        <div>Loading API keys...</div>
      ) : apiKeys.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-8">
            <Key className="h-12 w-12 text-muted-foreground mb-4" />
            <h3 className="text-lg font-semibold mb-2">
              {t("security.api_keys.no_keys_title")}
            </h3>
            <p className="text-muted-foreground text-center mb-4">
              {t("security.api_keys.no_keys_description")}
            </p>
            <Button onClick={() => setShowCreateDialog(true)}>
              <Plus className="h-4 w-4 mr-2" />
              {t("security.api_keys.create_button")}
            </Button>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>{t("security.api_keys.title")}</CardTitle>
            <CardDescription>
              {t("security.api_keys.description")}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>
                    {t("security.api_keys.table.headers.name")}
                  </TableHead>
                  <TableHead>
                    {t("security.api_keys.table.headers.key")}
                  </TableHead>
                  <TableHead>
                    {t("security.api_keys.table.headers.usage")}
                  </TableHead>
                  <TableHead>
                    {t("security.api_keys.table.headers.last_used")}
                  </TableHead>
                  <TableHead>
                    {t("security.api_keys.table.headers.expires")}
                  </TableHead>
                  <TableHead>
                    {t("security.api_keys.table.headers.status")}
                  </TableHead>
                  <TableHead>
                    {t("security.api_keys.table.headers.actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {apiKeys.map((apiKey) => (
                  <TableRow key={apiKey.id}>
                    <TableCell className="font-medium">{apiKey.name}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <code className="bg-muted px-2 py-1 rounded text-sm font-mono">
                          {apiKey.display_key}
                        </code>
                        {newKeyState.id === apiKey.id && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => copyToClipboard(apiKey.display_key)}
                          >
                            <Copy className="h-4 w-4" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="text-sm">
                        {apiKey.usage_count}
                        {apiKey.max_usage_count &&
                          ` / ${apiKey.max_usage_count}`}
                      </div>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {formatDateTime(apiKey.last_used)}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {formatDate(apiKey.expires_at)}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        {isExpired(apiKey.expires_at) && (
                          <Badge variant="destructive">
                            {t("security.api_keys.table.status.expired")}
                          </Badge>
                        )}
                        {isUsageExceeded(
                          apiKey.usage_count,
                          apiKey.max_usage_count
                        ) && (
                          <Badge variant="destructive">
                            {t("security.api_keys.table.status.limit_exceeded")}
                          </Badge>
                        )}
                        {!isExpired(apiKey.expires_at) &&
                          !isUsageExceeded(
                            apiKey.usage_count,
                            apiKey.max_usage_count
                          ) && (
                            <Badge variant="default">
                              {t("security.api_keys.table.status.active")}
                            </Badge>
                          )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => handleDelete(apiKey.id)}
                        disabled={deleteMutation.isPending}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
};

export default APIKeys;
