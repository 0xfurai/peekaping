import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import {
    getTagsInfiniteOptions,
    deleteTagsByIdMutation,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import { type TagModel } from "@/api";
import { useState, useCallback, useEffect } from "react";
import { useDebounce } from "@/hooks/useDebounce";
import { useSearchParams } from "@/hooks/useSearchParams";
import { useIntersectionObserver } from "@/hooks/useIntersectionObserver";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Label } from "@/components/ui/label";
import EmptyList from "@/components/empty-list";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Pencil, Trash2 } from "lucide-react";
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
import { useMutation } from "@tanstack/react-query";
import {
    commonMutationErrorHandler,
    getContrastingTextColor,
    invalidateByPartialQueryKey,
} from "@/lib/utils";
import { useNavigate } from "react-router-dom";

const TagsPage = () => {
    const queryClient = useQueryClient();
    const navigate = useNavigate();
    const { t } = useLocalizedTranslation();
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

    // Dialog states
    const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
    const [selectedTag, setSelectedTag] = useState<TagModel | null>(null);

    const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
        useInfiniteQuery({
            ...getTagsInfiniteOptions({
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

    const tags = (data?.pages.flatMap((page) => page.data || []) ||
        []) as TagModel[];

    // Delete mutation
    const deleteMutation = useMutation({
        ...deleteTagsByIdMutation({ path: { id: selectedTag?.id || "" } }),
        onSuccess: () => {
            toast.success(t("messages.tag_delete_success"));
            setDeleteDialogOpen(false);
            setSelectedTag(null);
            invalidateByPartialQueryKey(queryClient, { _id: "getTags" });
        },
        onError: commonMutationErrorHandler(t("messages.tag_delete_error")),
    });

    // Infinite scroll logic
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

    const handleEdit = (tag: TagModel) => {
        navigate(`${tag.id}/edit`);
    };

    const handleDelete = (tag: TagModel) => {
        setSelectedTag(tag);
        setDeleteDialogOpen(true);
    };

    const handleCreate = () => {
        navigate("new");
    };

    return (
        <Layout pageName={t("navigation.tags")} onCreate={handleCreate}>
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
                                    {t("common.clear_all_filters")}
                                </Button>
                            </div>
                        )}
                        <div className="flex flex-col gap-1 w-full sm:w-auto">
                            <Label htmlFor="search-tags">{t("common.search")}</Label>
                            <Input
                                id="search-tags"
                                placeholder={t("tags.search_placeholder")}
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                className="w-full sm:w-[400px]"
                            />
                        </div>
                    </div>
                </div>

                {/* Tags list */}
                {tags.length === 0 && isLoading && (
                    <div className="space-y-4">
                        {Array.from({ length: 6 }, (_, id) => (
                            <Skeleton className="h-[68px] w-full rounded-xl" key={id} />
                        ))}
                    </div>
                )}

                {/* No tags state */}
                {tags.length === 0 && !isLoading && (
                    <EmptyList
                        title={t("tags.empty_state.title")}
                        text={t("tags.empty_state.description")}
                        actionText={t("tags.empty_state.action")}
                        onClick={handleCreate}
                    />
                )}

                {/* Tags grid */}
                <div className="space-y-4">
                    {tags.map((tag) => (
                        <Card
                            key={tag.id}
                            className="hover:shadow-md transition-shadow py-2"
                        >
                            <CardContent className="px-3">
                                <div className="flex items-center justify-between">
                                    <div className="flex items-center gap-4 flex-1">
                                        <Badge
                                            className="px-3 py-1 text-white flex-shrink-0"
                                            style={{
                                                backgroundColor: tag.color,
                                                color: getContrastingTextColor(tag.color!),
                                            }}
                                        >
                                            {tag.name}
                                        </Badge>
                                        <div className="flex-1 min-w-0  hidden sm:block">
                                            {tag.description && (
                                                <p className="text-sm text-muted-foreground truncate">
                                                    {tag.description}
                                                </p>
                                            )}
                                        </div>
                                        <div className="text-xs text-muted-foreground mt-1 mr-4 hidden sm:block">
                                            {t("common.created")}: {new Date(tag.created_at!).toLocaleDateString()}
                                        </div>
                                    </div>
                                    <div className="flex gap-2 flex-shrink-0">
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => handleEdit(tag)}
                                            className="h-8 w-8 p-0"
                                        >
                                            <Pencil className="h-4 w-4" />
                                        </Button>
                                        <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => handleDelete(tag)}
                                            className="h-8 w-8 p-0 text-red-500 hover:text-red-600"
                                        >
                                            <Trash2 className="h-4 w-4" />
                                        </Button>
                                    </div>
                                </div>
                            </CardContent>
                        </Card>
                    ))}
                </div>

                {/* Sentinel for infinite scroll */}
                <div ref={sentinelRef} style={{ height: 1 }} />
                {isFetchingNextPage && (
                    <div className="space-y-4">
                        {Array.from({ length: 3 }, (_, i) => (
                            <Skeleton key={i} className="h-[68px] w-full rounded-xl" />
                        ))}
                    </div>
                )}

                {/* Delete Confirmation Dialog */}
                <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
                    <AlertDialogContent>
                        <AlertDialogHeader>
                            <AlertDialogTitle>{t("common.confirm_delete_title_short")}</AlertDialogTitle>
                            <AlertDialogDescription>
                                {t("tags.confirm_delete_description", { name: selectedTag?.name })}
                            </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
                            <AlertDialogAction
                                onClick={() =>
                                    deleteMutation.mutate({ path: { id: selectedTag?.id || "" } })
                                }
                                className="bg-red-500 hover:bg-red-600"
                                disabled={deleteMutation.isPending}
                            >
                                {t("common.delete")}
                            </AlertDialogAction>
                        </AlertDialogFooter>
                    </AlertDialogContent>
                </AlertDialog>
            </div>
        </Layout>
    );
};

export default TagsPage;
