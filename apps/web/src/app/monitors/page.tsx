import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import {
  getMonitorsByIdHeartbeatsQueryKey,
  getMonitorsInfiniteOptions,
} from "@/api/@tanstack/react-query.gen";
import Layout from "@/layout";
import { useNavigate } from "react-router-dom";
import {
  type HeartbeatModel,
  type MonitorModel,
  type UtilsApiResponseArrayHeartbeatModel,
} from "@/api";
import { useWebSocket, WebSocketStatus } from "@/context/websocket-context";
import { useEffect, useState, useRef, useCallback } from "react";
import { useDebounce } from "@/hooks/useDebounce";
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

const MonitorsPage = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { t } = useLocalizedTranslation();

  // Add state for search query
  const [search, setSearch] = useState("");
  const debouncedSearch = useDebounce(search, 400);

  // Add state for active filter
  const [activeFilter, setActiveFilter] = useState<
    "all" | "active" | "inactive"
  >("all");
  const [statusFilter, setStatusFilter] = useState<
    "all" | "up" | "down" | "maintenance"
  >("all");

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

  const monitors = (data?.pages.flatMap((page) => page.data || []) ||
    []) as MonitorModel[];

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

  return (
    <Layout
      pageName={t('monitors.title')}
      onCreate={() => {
        navigate("/monitors/new");
      }}
    >
      <div>
        <div className="mb-4 flex flex-col gap-4 sm:flex-row sm:justify-end sm:gap-4">
          <div className="flex flex-col gap-1">
            <Label htmlFor="active-filter">{t('common.active')}</Label>
            <Select
              value={activeFilter}
              onValueChange={(v) =>
                setActiveFilter(v as "all" | "active" | "inactive")
              }
            >
              <SelectTrigger className="w-full sm:w-[140px]">
                <SelectValue placeholder="Status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('common.all')}</SelectItem>
                <SelectItem value="active">{t('common.active')}</SelectItem>
                <SelectItem value="inactive">{t('common.inactive')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="status-filter">
              {t('monitors.filters.monitor_status')}
            </Label>
            <Select
              value={statusFilter}
              onValueChange={(v) =>
                setStatusFilter(v as "all" | "up" | "down" | "maintenance")
              }
            >
              <SelectTrigger className="w-full sm:w-[160px]">
                <SelectValue placeholder="Monitor Status" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('common.all')}</SelectItem>
                <SelectItem value="up">{t('common.up')}</SelectItem>
                <SelectItem value="down">{t('common.down')}</SelectItem>
                <SelectItem value="maintenance">{t('common.maintenance')}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1 w-full sm:w-auto">
            <Label htmlFor="search-maintenances">{t('common.search')}</Label>
            <Input
              id="search-maintenances"
              placeholder={t('monitors.filters.search_placeholder')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full sm:w-[400px]"
            />
          </div>
        </div>

        {/* Monitors list */}
        {monitors.length === 0 && isLoading && (
          <div className="flex flex-col space-y-2 mb-2">
            {Array.from({ length: 7 }, (_, id) => (
              <Skeleton className="h-[68px] w-full rounded-xl" key={id} />
            ))}
          </div>
        )}

        {/* No monitors state */}
        {monitors.length === 0 && !isLoading && (
          <EmptyList
            title="No monitors found"
            text="Get started by creating your first monitor to track the health of your services."
            actionText="Create your first monitor"
            onClick={() => navigate("/monitors/new")}
          />
        )}

        {/* Monitors list */}
        {monitors.map((monitor) => (
          <MonitorCard key={monitor.id} monitor={monitor} />
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
    </Layout>
  );
};

export default MonitorsPage;
