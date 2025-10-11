// MARK: - Imports
import { useState } from "react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { TypographyH4 } from "@/components/ui/typography";
import { Copy, Trash2, Plus, Key } from "lucide-react";
import { toast } from "sonner";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useTimezone } from "@/context/timezone-context";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import {
  getApiKeysOptions,
  deleteApiKeysByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  ApiKeyApiKeyResponse,
  PostApiKeysResponse,
} from "@/api/types.gen";
import CreateAPIKeyModal from "./create-api-key-modal";
import DeleteConfirmationModal from "./delete-confirmation-modal";

// MARK: - Main Component
const APIKeys = () => {
  // MARK: - Hooks & State
  const { t } = useLocalizedTranslation();
  const { timezone: userTimezone } = useTimezone();
  const queryClient = useQueryClient();
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [deleteConfirmation, setDeleteConfirmation] = useState<{
    open: boolean;
    id: string | null;
    name: string | null;
  }>({
    open: false,
    id: null,
    name: null,
  });
  const [newKeyState, setNewKeyState] = useState<{
    token: string | null;
    id: string | null;
  }>({
    token: null,
    id: null,
  });

  // MARK: - Data Fetching
  const { data: apiKeysResponse, isLoading } = useQuery(getApiKeysOptions());
  const deleteMutation = useMutation(deleteApiKeysByIdMutation());

  const apiKeys: ApiKeyApiKeyResponse[] = apiKeysResponse?.data || [];

  // MARK: - Event Handlers
  const handleCreateSuccess = (response: PostApiKeysResponse) => {
    setNewKeyState({
      token: response.data.token,
      id: response.data.id,
    });
  };

  const handleDelete = (id: string, name: string) => {
    setDeleteConfirmation({
      open: true,
      id,
      name,
    });
  };

  const confirmDelete = () => {
    if (deleteConfirmation.id) {
      deleteMutation.mutate(
        {
          path: { id: deleteConfirmation.id },
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

  // MARK: - Utility Functions
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

  // MARK: - Render
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

      <DeleteConfirmationModal
        open={deleteConfirmation.open}
        onOpenChange={(open) =>
          setDeleteConfirmation((prev) => ({ ...prev, open }))
        }
        onConfirm={confirmDelete}
        apiKeyName={deleteConfirmation.name || ""}
      />

      {newKeyState.token && (
        <Alert variant="success">
          <Key className="h-4 w-4" />
          <AlertTitle>{t("security.api_keys.success_alert.title")}</AlertTitle>
          <AlertDescription>
            <div className="mt-2">
              <p className="text-sm text-muted-foreground mb-2">
                {t("security.api_keys.success_alert.description")}
              </p>
              <div className="flex items-center gap-2">
                <code className="px-2 py-1 rounded text-sm font-mono bg-green-500/20">
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
                        onClick={() => handleDelete(apiKey.id, apiKey.name)}
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
