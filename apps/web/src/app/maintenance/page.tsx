import { useState, useCallback } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import {
  getMaintenancesInfiniteOptions,
  getMaintenancesQueryKey,
  deleteMaintenancesByIdMutation,
  patchMaintenancesByIdPauseMutation,
  patchMaintenancesByIdResumeMutation,
} from "@/api/@tanstack/react-query.gen";
import MaintenanceCard from "./components/maintenance-card";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import type { MaintenanceModel } from "@/api/types.gen";
import Layout from "@/layout";
import { Label } from "@/components/ui/label";
import { useDebounce } from "@/hooks/useDebounce";
import { Skeleton } from "@/components/ui/skeleton";
import { useIntersectionObserver } from "@/hooks/useIntersectionObserver";
import { commonMutationErrorHandler } from "@/lib/utils";
import EmptyList from "@/components/empty-list";

export default function MaintenancePage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [showConfirmDialog, setShowConfirmDialog] = useState(false);
  const [pendingAction, setPendingAction] = useState<"pause" | "resume" | null>(null);
  const [pendingMaintenanceId, setPendingMaintenanceId] = useState<string | null>(null);
  const debouncedSearch = useDebounce(search, 300);

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      ...getMaintenancesInfiniteOptions({
        query: {
          limit: 20,
          q: debouncedSearch || undefined,
        },
      }),
      getNextPageParam: (lastPage, pages) => {
        const lastLength = lastPage.data?.length || 0;
        if (lastLength < 20) return undefined;
        return pages.length;
      },
      initialPageParam: 0,
    });

  const maintenances = data?.pages.flatMap((page) => page.data || []) || [];

  const deleteMutation = useMutation({
    ...deleteMaintenancesByIdMutation(),
    onSuccess: () => {
      toast.success("Maintenance deleted successfully");
      setDeleteId(null);
      // Invalidate and refetch maintenances
      queryClient.invalidateQueries({
        queryKey: getMaintenancesQueryKey(),
      });
    },
    onError: commonMutationErrorHandler("Failed to delete maintenance"),
  });

  const updateMaintenanceState = (id: string, active: boolean) => {
    const allQueries = queryClient
      .getQueryCache()
      .findAll({ queryKey: getMaintenancesQueryKey() });

    allQueries.forEach(({ queryKey }) => {
      queryClient.setQueryData(
        queryKey,
        (
          oldData: { pages: Array<{ data: MaintenanceModel[] }> } | undefined
        ) => {
          if (!oldData) return oldData;
          return {
            ...oldData,
            pages: oldData.pages.map((page) => ({
              ...page,
              data: page.data?.map((maintenance: MaintenanceModel) =>
                maintenance.id === id ? { ...maintenance, active } : maintenance
              ),
            })),
          };
        }
      );
    });
  };

  const pauseMutation = useMutation({
    ...patchMaintenancesByIdPauseMutation(),
    onSuccess: () => {
      toast.success("Maintenance paused successfully");
      setShowConfirmDialog(false);
      setPendingAction(null);
      setPendingMaintenanceId(null);
    },
    onError: (err) => {
      // Revert optimistic update on error
      if (pendingMaintenanceId) {
        updateMaintenanceState(pendingMaintenanceId, true);
      }
      commonMutationErrorHandler("Failed to pause maintenance")(err);
      setShowConfirmDialog(false);
      setPendingAction(null);
      setPendingMaintenanceId(null);
    },
  });

  const resumeMutation = useMutation({
    ...patchMaintenancesByIdResumeMutation(),
    onSuccess: () => {
      toast.success("Maintenance resumed successfully");
      setShowConfirmDialog(false);
      setPendingAction(null);
      setPendingMaintenanceId(null);
    },
    onError: (err) => {
      // Revert optimistic update on error
      if (pendingMaintenanceId) {
        updateMaintenanceState(pendingMaintenanceId, false);
      }
      commonMutationErrorHandler("Failed to resume maintenance")(err);
      setShowConfirmDialog(false);
      setPendingAction(null);
      setPendingMaintenanceId(null);
    },
  });

  const handleDeleteClick = (id: string) => {
    setDeleteId(id);
  };

  const handleConfirmDelete = () => {
    if (!deleteId) return;

    deleteMutation.mutate({
      path: { id: deleteId },
    });
  };

  const handleCancelDelete = () => {
    setDeleteId(null);
  };

  const handleToggleActive = (maintenance: MaintenanceModel) => {
    if (!maintenance.id) {
      toast.error("Cannot toggle maintenance status: ID is missing");
      return;
    }

    const action = maintenance.active ? "pause" : "resume";
    setPendingAction(action);
    setPendingMaintenanceId(maintenance.id);
    setShowConfirmDialog(true);
  };

  const handleConfirmAction = () => {
    if (!pendingMaintenanceId || !pendingAction) return;

    // Optimistic update - update UI immediately
    const newActiveState = pendingAction === "resume";
    updateMaintenanceState(pendingMaintenanceId, newActiveState);

    if (pendingAction === "pause") {
      pauseMutation.mutate({
        path: { id: pendingMaintenanceId },
      });
    } else {
      resumeMutation.mutate({
        path: { id: pendingMaintenanceId },
      });
    }
  };

  const handleCancelAction = () => {
    setShowConfirmDialog(false);
    setPendingAction(null);
    setPendingMaintenanceId(null);
  };

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      handleCancelAction();
    }
  };

  const handleCreateClick = () => {
    navigate("/maintenances/new");
  };

  const handleEditClick = (id: string) => {
    navigate(`/maintenances/${id}/edit`);
  };

  const handleObserver = useCallback(
    (entries: IntersectionObserverEntry[]) => {
      const [entry] = entries;
      if (entry.isIntersecting && hasNextPage && !isFetchingNextPage) {
        fetchNextPage();
      }
    },
    [fetchNextPage, hasNextPage, isFetchingNextPage]
  );

  const { ref: sentinelRef } =
    useIntersectionObserver<HTMLDivElement>(handleObserver);

  return (
    <Layout pageName="Maintenance Windows" onCreate={handleCreateClick}>
      <div>
        <div className="mb-4 flex justify-center sm:justify-end gap-4">
          <div className="flex flex-col gap-1 w-full sm:w-auto">
            <Label htmlFor="search-maintenances">Search</Label>
            <Input
              id="search-maintenances"
              placeholder="Search maintenances..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full sm:w-[400px]"
            />
          </div>
        </div>

        {isLoading ? (
          <div className="flex flex-col space-y-2 mb-2">
            {Array.from({ length: 7 }, (_, id) => (
              <Skeleton className="h-[68px] w-full rounded-xl" key={id} />
            ))}
          </div>
        ) : maintenances.length > 0 ? (
          <div>
            {maintenances.map((maintenance: MaintenanceModel) => (
              <MaintenanceCard
                key={maintenance.id}
                maintenance={maintenance}
                onClick={() =>
                  maintenance.id && handleEditClick(maintenance.id)
                }
                onDelete={() =>
                  maintenance.id && handleDeleteClick(maintenance.id)
                }
                onToggleActive={() => handleToggleActive(maintenance)}
                isPending={pauseMutation.isPending || resumeMutation.isPending}
              />
            ))}
            {/* Sentinel for infinite scroll */}
            <div ref={sentinelRef} style={{ height: 1 }} />
            {isFetchingNextPage && (
              <div className="flex flex-col space-y-2 mb-2">
                {Array.from({ length: 3 }, (_, i) => (
                  <Skeleton key={i} className="h-[68px] w-full rounded-xl" />
                ))}
              </div>
            )}
          </div>
        ) : (
          <EmptyList
            title="No maintenance windows found"
            text="Get started by creating your first maintenance window to prevent scheduled downtimes."
            actionText="Create your first maintenance window"
            onClick={() => navigate("/maintenances/new")}
          />
        )}

        {deleteId && (
          <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
            <div className="bg-background p-6 rounded-lg shadow-lg max-w-md w-full border">
              <h2 className="text-xl font-bold mb-4">Confirm Delete</h2>
              <p className="mb-6">
                Are you sure you want to delete this maintenance window? This
                action cannot be undone.
              </p>
              <div className="flex justify-end gap-4">
                <Button variant="outline" onClick={handleCancelDelete}>
                  Cancel
                </Button>
                <Button variant="destructive" onClick={handleConfirmDelete}>
                  Delete
                </Button>
              </div>
            </div>
          </div>
        )}

        {showConfirmDialog && (
          <AlertDialog open={showConfirmDialog} onOpenChange={handleOpenChange}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Confirm Action</AlertDialogTitle>
                <AlertDialogDescription>
                  Are you sure you want to{" "}
                  {pendingAction === "pause" ? "pause" : "resume"} this
                  maintenance?
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel onClick={(e) => e.stopPropagation()}>
                  Cancel
                </AlertDialogCancel>
                <AlertDialogAction onClick={handleConfirmAction}>
                  Confirm
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
      </div>
    </Layout>
  );
}
