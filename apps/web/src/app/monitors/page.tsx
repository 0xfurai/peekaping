import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import {
    getMonitorsByIdHeartbeatsQueryKey,
    getMonitorsInfiniteOptions,
    getTagsOptions,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import {
    type HeartbeatModel,
    type MonitorModel,
    type UtilsApiResponseArrayHeartbeatModel,
    type TagModel,
} from "@/api";
import { useWebSocket, WebSocketStatus } from "@/context/websocket-context";
import { useEffect, useState, useRef, useCallback } from "react";
import { useDebounce } from "@/hooks/useDebounce";
import { useSearchParams } from "@/hooks/useSearchParams";
import { useIntersectionObserver } from "@/hooks/useIntersectionObserver";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import MonitorCard from "./components/monitor-card";
import {
    Select,
    SelectTrigger,
    SelectValue,
    SelectContent,
    SelectItem,
} from "@/components/ui/select";
import { Label } from "@/components/ui/label";
import EmptyList from "@/components/empty-list";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { getContrastingTextColor } from "@/lib/utils";
import { useDelayedLoading } from "@/hooks/useDelayedLoading";

const MonitorsPage = () => {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const { t } = useLocalizedTranslation();
    const { getParam, updateSearchParams, clearAllParams, hasParams } =
        useSearchParams();

    // Initialize state from URL parameters
    const [search, setSearch] = useState(getParam("search") || "");
    const debouncedSearch = useDebounce(search, 400);

    const [activeFilter, setActiveFilter] = useState<
        "all" | "active" | "inactive"
    >((getParam("active") as "all" | "active" | "inactive") || "all");

    const [statusFilter, setStatusFilter] = useState<
        "all" | "up" | "down" | "maintenance"
    >((getParam("status") as "all" | "up" | "down" | "maintenance") || "all");

    const [selectedTagIds, setSelectedTagIds] = useState<string[]>(
        getParam("tags")?.split(",").filter(Boolean) || []
    );

    const [tagPopoverOpen, setTagPopoverOpen] = useState(false);

    // Update URL when search changes
    useEffect(() => {
        updateSearchParams({ search: debouncedSearch });
    }, [debouncedSearch, updateSearchParams]);

    // Update URL when active filter changes
    useEffect(() => {
        updateSearchParams({ active: activeFilter });
    }, [activeFilter, updateSearchParams]);

    // Update URL when status filter changes
    useEffect(() => {
        updateSearchParams({ status: statusFilter });
    }, [statusFilter, updateSearchParams]);

    // Update URL when tag selection changes
    useEffect(() => {
        updateSearchParams({
            tags: selectedTagIds.length > 0 ? selectedTagIds.join(",") : null,
        });
    }, [selectedTagIds, updateSearchParams]);

    // Load tags for filtering
    const { data: tagsData } = useQuery({
        ...getTagsOptions({
            query: {
                limit: 100, // Load more tags for filtering
            },
        }),
    });

    const availableTags = (tagsData?.data || []) as TagModel[];

    const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
        useInfiniteQuery({
            ...getMonitorsInfiniteOptions({
                query: {
                    limit: 20,
                    q: debouncedSearch || undefined,
                    active:
                        activeFilter === "all"
                            ? undefined
                            : activeFilter === "active"
                                ? true
                                : false,
                    status:
                        statusFilter === "all"
                            ? undefined
                            : statusFilter === "up"
                                ? 1
                                : statusFilter === "down"
                                    ? 0
                                    : statusFilter === "maintenance"
                                        ? 3
                                        : undefined,
                    tag_ids:
                        selectedTagIds.length > 0 ? selectedTagIds.join(",") : undefined,
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

    const allMonitors = (data?.pages.flatMap((page) => page.data || []) ||
        []) as MonitorModel[];

    // No more client-side filtering needed since API now supports tag filtering
    const monitors = allMonitors;

    const { socket, status: socketStatus } = useWebSocket();
    const subscribedRef = useRef(false);

    useEffect(() => {
        if (!socket || socketStatus !== WebSocketStatus.CONNECTED) return;
        if (subscribedRef.current) return;
        subscribedRef.current = true;

        const roomName = "monitor:all";

        const handleHeartbeat = (newHeartbeat: HeartbeatModel) => {
            queryClient.setQueryData(
                getMonitorsByIdHeartbeatsQueryKey({
                    path: {
                        id: newHeartbeat.monitor_id!,
                    },
                    query: {
                        limit: 50,
                        reverse: true,
                    },
                }),
                (oldData: UtilsApiResponseArrayHeartbeatModel) => {
                    if (!oldData) return oldData;
                    return {
                        ...oldData,
                        data: [...(oldData.data || []), newHeartbeat].slice(-50),
                    };
                }
            );
        };

        socket.on(`${roomName}:heartbeat`, handleHeartbeat);
        socket.emit("join_room", roomName);
        console.log("Subscribed to heartbeat", roomName);

        return () => {
            socket.off(`${roomName}:heartbeat`, handleHeartbeat);
            console.log("Unsubscribed from heartbeat", `${roomName}:heartbeat`);

            if (socketStatus === WebSocketStatus.CONNECTED) {
                socket.emit("leave_room", roomName);
            }
        };
    }, [socket, socketStatus, queryClient]);

    // Infinite scroll logic using the reusable hook
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

    const handleTagToggle = (tagId: string) => {
        setSelectedTagIds((prev) =>
            prev.includes(tagId)
                ? prev.filter((id) => id !== tagId)
                : [...prev, tagId]
        );
    };

    const handleTagRemove = (tagId: string) => {
        setSelectedTagIds((prev) => prev.filter((id) => id !== tagId));
    };

    const clearAllTags = () => {
        setSelectedTagIds([]);
    };

    const clearAllFilters = () => {
        setSearch("");
        setActiveFilter("all");
        setStatusFilter("all");
        setSelectedTagIds([]);
        clearAllParams();
    };

    const selectedTags = availableTags.filter((tag) =>
        selectedTagIds.includes(tag.id!)
    );

    const delayedLoader = useDelayedLoading(isLoading, 300);
    const delayedLoaderNextPage = useDelayedLoading(isFetchingNextPage, 300);

    return (
        <Layout
            pageName={t("monitors.title")}
            onCreate={() => {
                navigate("new");
            }}
        >
            <div>
                <div className="mb-4 space-y-4">
                    <div className="flex flex-col gap-4 md:flex-row sm:justify-end sm:gap-4 items-end">
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
                            <Label htmlFor="active-filter">{t("common.active")}</Label>
                            <Select
                                value={activeFilter}
                                onValueChange={(v) =>
                                    setActiveFilter(v as "all" | "active" | "inactive")
                                }
                            >
                                <SelectTrigger className="w-full sm:w-[140px]">
                                    <SelectValue placeholder={t("monitors.filters.status_placeholder")} />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="all">{t("common.all")}</SelectItem>
                                    <SelectItem value="active">{t("common.active")}</SelectItem>
                                    <SelectItem value="inactive">
                                        {t("common.inactive")}
                                    </SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        <div className="flex flex-col gap-1 w-full sm:w-auto">
                            <Label htmlFor="status-filter">
                                {t("monitors.filters.monitor_status")}
                            </Label>
                            <Select
                                value={statusFilter}
                                onValueChange={(v) =>
                                    setStatusFilter(v as "all" | "up" | "down" | "maintenance")
                                }
                            >
                                <SelectTrigger className="w-full md:w-[160px]">
                                    <SelectValue placeholder={t("monitors.filters.monitor_status_placeholder")} />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="all">{t("common.all")}</SelectItem>
                                    <SelectItem value="up">{t("common.up")}</SelectItem>
                                    <SelectItem value="down">{t("common.down")}</SelectItem>
                                    <SelectItem value="maintenance">
                                        {t("common.maintenance")}
                                    </SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        <div className="flex flex-col gap-1 w-full sm:w-auto">
                            <Label htmlFor="tag-filter">{t("common.tags")}</Label>
                            <Popover open={tagPopoverOpen} onOpenChange={setTagPopoverOpen}>
                                <PopoverTrigger asChild>
                                    <Button variant="outline" className="px-3 font-normal">
                                        <span className="text-muted-foreground">{t("common.select_tags")}</span>
                                    </Button>
                                </PopoverTrigger>
                                <PopoverContent className="w-auto p-0" align="end">
                                    <div className="max-h-60 overflow-y-auto">
                                        <div className="">
                                            {availableTags.map((tag) => (
                                                <div
                                                    key={tag.id}
                                                    className="flex items-center space-x-2 p-2 hover:bg-accent hover:text-accent-foreground rounded-sm cursor-pointer"
                                                    onClick={() => handleTagToggle(tag.id!)}
                                                >
                                                    <Checkbox
                                                        checked={selectedTagIds.includes(tag.id!)}
                                                        onChange={() => handleTagToggle(tag.id!)}
                                                    />
                                                    <Badge
                                                        variant="secondary"
                                                        className="text-xs"
                                                        style={{
                                                            backgroundColor: tag.color,
                                                            color: getContrastingTextColor(tag.color!),
                                                        }}
                                                    >
                                                        {tag.name}
                                                    </Badge>
                                                </div>
                                            ))}
                                            {availableTags.length === 0 && (
                                                <div className="text-center text-muted-foreground text-sm p-4">
                                                    {t("common.no_tags_available")}
                                                </div>
                                            )}
                                        </div>
                                    </div>
                                </PopoverContent>
                            </Popover>
                        </div>

                        <div className="flex flex-col gap-1 w-full sm:w-auto">
                            <Label htmlFor="search-maintenances">{t("common.search")}</Label>
                            <Input
                                id="search-maintenances"
                                placeholder={t("monitors.filters.search_placeholder")}
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                className="w-full lg:w-[400px]"
                            />
                        </div>
                    </div>
                </div>

                {/* Show selected tags */}
                {selectedTags.length > 0 && (
                    <div className="mb-4 flex flex-wrap gap-2">
                        <span className="text-sm text-muted-foreground">
                            {t("monitors.filtering_by_tags")}:
                        </span>
                        {selectedTags.map((tag) => (
                            <Badge
                                key={tag.id}
                                variant="secondary"
                                className="flex items-center gap-1"
                                style={{
                                    backgroundColor: tag.color,
                                    color: getContrastingTextColor(tag.color!),
                                }}
                            >
                                {tag.name}
                                <div role="button" onClick={() => handleTagRemove(tag.id!)}>
                                    <X className="h-3 w-3 cursor-pointer" />
                                </div>
                            </Badge>
                        ))}
                        <Button
                            variant="ghost"
                            size="sm"
                            onClick={clearAllTags}
                            className="h-6 text-xs cursor-pointer"
                        >
                            {t("common.clear_all")}
                        </Button>
                    </div>
                )}

                {/* Monitors list */}
                {monitors.length === 0 && delayedLoader && (
                    <div className="flex flex-col space-y-2 mb-2">
                        {Array.from({ length: 7 }, (_, id) => (
                            <Skeleton className="h-[68px] w-full rounded-xl" key={id} />
                        ))}
                    </div>
                )}

                {/* No monitors state */}
                {monitors.length === 0 && !isLoading && (
                    <EmptyList
                        title={t("monitors.empty_state.title")}
                        text={t("monitors.empty_state.description")}
                        actionText={t("monitors.empty_state.action")}
                        onClick={() => navigate("new")}
                    />
                )}

                {/* Monitors list */}
                {monitors.map((monitor) => (
                    <MonitorCard key={monitor.id} monitor={monitor} />
                ))}

                {/* Sentinel for infinite scroll */}
                <div ref={sentinelRef} style={{ height: 1 }} />
                {delayedLoaderNextPage && (
                    <div className="flex flex-col space-y-2 mb-2">
                        {Array.from({ length: 3 }, (_, i) => (
                            <Skeleton key={i} className="h-[68px] w-full rounded-xl" />
                        ))}
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default MonitorsPage;
