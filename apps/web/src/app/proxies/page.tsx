import { useCallback, useState, useEffect } from "react";
import type { ProxyModel } from "@/api/types.gen";
import {
    getProxiesInfiniteOptions,
    deleteProxiesByIdMutation,
    getProxiesQueryKey,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import {
    useInfiniteQuery,
    useMutation,
    useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { useSearchParams } from "@/hooks/useSearchParams";
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
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
import { commonMutationErrorHandler } from "@/lib/utils";
import ProxyCard from "./components/proxy-card";
import { useIntersectionObserver } from "@/hooks/useIntersectionObserver";
import EmptyList from "@/components/empty-list";
import { useDebounce } from "@/hooks/useDebounce";
import { useDelayedLoading } from "@/hooks/useDelayedLoading";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const ProxiesPage = () => {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { getParam, updateSearchParams } = useSearchParams();
    const { t } = useLocalizedTranslation();

    // Initialize search state from URL parameter
    const [search, setSearch] = useState(getParam("search") || "");
    const debouncedSearch = useDebounce(search, 400);

    const [showConfirmDelete, setShowConfirmDelete] = useState(false);
    const [proxyToDelete, setProxyToDelete] = useState<ProxyModel | null>(null);

    // Update URL when search changes
    useEffect(() => {
        updateSearchParams({ search: debouncedSearch });
    }, [debouncedSearch, updateSearchParams]);

    const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
        useInfiniteQuery({
            ...getProxiesInfiniteOptions({
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

    const shouldShowSkeleton = useDelayedLoading(isLoading, 200);

    const deleteMutation = useMutation({
        ...deleteProxiesByIdMutation(),
        onSuccess: () => {
            toast.success("Proxy deleted successfully");
            queryClient.invalidateQueries({
                queryKey: getProxiesQueryKey(),
            });
            setShowConfirmDelete(false);
            setProxyToDelete(null);
        },
        onError: (err) => {
            commonMutationErrorHandler("Failed to delete proxy")(err);
            setShowConfirmDelete(false);
            setProxyToDelete(null);
        },
    });

    const handleDeleteClick = (proxy: ProxyModel) => {
        setProxyToDelete(proxy);
        setShowConfirmDelete(true);
    };

    const handleConfirmDelete = () => {
        if (proxyToDelete?.id) {
            deleteMutation.mutate({
                path: { id: proxyToDelete.id },
            });
        }
    };

    const proxies = (data?.pages.flatMap((page) => page.data || []) ||
        []) as ProxyModel[];

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
        <Layout pageName={t("proxies.page_name")} onCreate={() => navigate("new")}>
            <div>
                <div className="mb-4 flex justify-center sm:justify-end gap-4">
                    <div className="flex flex-col gap-1 w-full sm:w-auto">
                        <Label htmlFor="search-proxies">{t("proxies.search_label")}</Label>
                        <Input
                            id="search-proxies"
                            placeholder={t("proxies.search_placeholder")}
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            className="w-full sm:w-[400px]"
                        />
                    </div>
                </div>

                {proxies.length === 0 && shouldShowSkeleton && (
                    <div className="flex flex-col space-y-2 mb-2">
                        {Array.from({ length: 7 }, (_, id) => (
                            <Skeleton className="h-[68px] w-full rounded-xl" key={id} />
                        ))}
                    </div>
                )}

                {/* Proxies list */}
                {proxies.map((proxy) => (
                    <ProxyCard
                        key={proxy.id}
                        proxy={proxy}
                        onClick={() => navigate(`${proxy.id}/edit`)}
                        onDelete={() => handleDeleteClick(proxy)}
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

                {proxies.length === 0 && !isLoading && (
                    <EmptyList
                        title={t("proxies.empty_list.title")}
                        text={t("proxies.empty_list.text")}
                        actionText={t("proxies.empty_list.action_text")}
                        onClick={() => navigate("new")}
                    />
                )}
            </div>

            <AlertDialog open={showConfirmDelete} onOpenChange={setShowConfirmDelete}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t("proxies.delete_dialog.title")}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t("proxies.delete_dialog.description", {
                                host: proxyToDelete?.host,
                                port: proxyToDelete?.port,
                            })}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel onClick={() => setShowConfirmDelete(false)}>
                            {t("common.cancel")}
                        </AlertDialogCancel>
                        <AlertDialogAction
                            onClick={handleConfirmDelete}
                            disabled={deleteMutation.isPending}
                            className="bg-red-600 hover:bg-red-700 focus:ring-red-600"
                        >
                            {deleteMutation.isPending && (
                                <Loader2 className="animate-spin mr-2 h-4 w-4" />
                            )}
                            {t("common.delete")}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </Layout>
    );
};

export default ProxiesPage;
