import { useCallback, useEffect } from "react";
import type { NotificationChannelModel } from "@/api";
import {
  getNotificationChannelsInfiniteOptions,
  deleteNotificationChannelsByIdMutation,
  getNotificationChannelsInfiniteQueryKey,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { useState } from "react";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Label } from "@/components/ui/label";
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
import NotificationChannelCard from "./components/notification-channel-card";
import { useDebounce } from "@/hooks/useDebounce";
import { useSearchParams } from "@/hooks/useSearchParams";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
import { commonMutationErrorHandler } from "@/lib/utils";
import { useIntersectionObserver } from "@/hooks/useIntersectionObserver";
import EmptyList from "@/components/empty-list";
import { Button } from "@/components/ui/button";

const NotifiersPage = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { getParam, updateSearchParams, clearAllParams, hasParams } =
    useSearchParams();

  // Initialize search from URL params
  const [search, setSearch] = useState(getParam("q") || "");
  const debouncedSearch = useDebounce(search, 400);

  // Update URL when debounced search changes
  useEffect(() => {
    updateSearchParams({ q: debouncedSearch });
  }, [debouncedSearch, updateSearchParams]);

  const clearAllFilters = () => {
    setSearch("");
    clearAllParams();
  };
  const [showConfirmDelete, setShowConfirmDelete] = useState(false);
  const [notifierToDelete, setNotifierToDelete] =
    useState<NotificationChannelModel | null>(null);

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      ...getNotificationChannelsInfiniteOptions({
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
      enabled: true,
    });

  const deleteMutation = useMutation({
    ...deleteNotificationChannelsByIdMutation(),
    onSuccess: () => {
      toast.success("Notifier deleted successfully");
      queryClient.invalidateQueries({
        queryKey: getNotificationChannelsInfiniteQueryKey(),
      });
      setShowConfirmDelete(false);
      setNotifierToDelete(null);
    },
    onError: (err) => {
      commonMutationErrorHandler("Failed to delete notifier")(err);
      setShowConfirmDelete(false);
      setNotifierToDelete(null);
    },
  });

  const handleDeleteClick = (notifier: NotificationChannelModel) => {
    setNotifierToDelete(notifier);
    setShowConfirmDelete(true);
  };

  const handleConfirmDelete = () => {
    if (notifierToDelete?.id) {
      deleteMutation.mutate({
        path: { id: notifierToDelete.id },
      });
    }
  };

  const notificationChannels = (data?.pages.flatMap(
    (page) => page.data || []
  ) || []) as NotificationChannelModel[];

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
    <Layout
      pageName="Notification channels"
      onCreate={() => navigate("/notification-channels/new")}
    >
      <div>
        <div className="mb-4 space-y-4">
          <div className="flex flex-col gap-4 sm:flex-row sm:justify-end sm:gap-4 items-end">
            {hasParams() && (
              <div className="flex justify-start">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={clearAllFilters}
                  className="w-fit h-[36px]"
                >
                  Clear all filters
                </Button>
              </div>
            )}
            <div className="flex flex-col gap-1 w-full sm:w-auto">
              <Label htmlFor="search-notification-channels">Search</Label>
              <Input
                id="search-notification-channels"
                placeholder="Search notification channels by name..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full sm:w-[400px]"
              />
            </div>
          </div>
        </div>

        {notificationChannels.length === 0 && isLoading && (
          <div className="flex flex-col space-y-2 mb-2">
            {Array.from({ length: 7 }, (_, id) => (
              <Skeleton className="h-[68px] w-full rounded-xl" key={id} />
            ))}
          </div>
        )}

        {notificationChannels.map((notifier) => (
          <NotificationChannelCard
            key={notifier.id}
            notifier={notifier}
            onClick={() =>
              navigate(`/notification-channels/${notifier.id}/edit`)
            }
            onDelete={() => handleDeleteClick(notifier)}
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
        {notificationChannels.length === 0 && !isLoading && (
          <EmptyList
            title="No notification channels found"
            text="Get started by creating your first notification channel to send alerts to your team."
            actionText="Create your first notification channel"
            onClick={() => navigate("/notification-channels/new")}
          />
        )}
      </div>

      <AlertDialog open={showConfirmDelete} onOpenChange={setShowConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Are you absolutely sure?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete the
              notifier "{notifierToDelete?.name}" and remove it from all
              monitors that use it.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => setShowConfirmDelete(false)}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleConfirmDelete}
              disabled={deleteMutation.isPending}
              className="bg-red-600 hover:bg-red-700 focus:ring-red-600"
            >
              {deleteMutation.isPending && (
                <Loader2 className="animate-spin mr-2 h-4 w-4" />
              )}
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Layout>
  );
};

export default NotifiersPage;
