import { type HeartbeatModel, type UtilsApiResponseMonitorModel } from "@/api";
import {
  deleteMonitorsByIdMutation,
  getMonitorsByIdHeartbeatsInfiniteQueryKey,
  getMonitorsByIdHeartbeatsOptions,
  getMonitorsByIdOptions,
  getMonitorsByIdQueryKey,
  getMonitorsByIdStatsUptimeOptions,
  getMonitorsByIdStatsUptimeQueryKey,
  getMonitorsInfiniteQueryKey,
  patchMonitorsByIdMutation,
  postMonitorsByIdResetMutation,
} from "@/api/@tanstack/react-query.gen";
import { Chart } from "@/components/app-chart-example";
import BarHistory from "@/components/bars";
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
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useWebSocket, WebSocketStatus } from "@/context/websocket-context";
import Layout from "@/layout";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Copy,
  Edit,
  Loader2,
  Pause,
  PlayIcon,
  RotateCcw,
  Trash,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { cn, commonMutationErrorHandler } from "@/lib/utils";
import ImportantNotificationsList from "../components/important-notifications-list";
import { BackButton } from "@/components/back-button";

const MonitorPage = () => {
  const { id } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { socket, status: socketStatus } = useWebSocket();

  const [showConfirmDelete, setShowConfirmDelete] = useState(false);
  const [showConfirmPause, setShowConfirmPause] = useState(false);
  const [showConfirmReset, setShowConfirmReset] = useState(false);

  const [heartbeatData, setHeartbeatData] = useState<HeartbeatModel[]>([]);

  const { data, error, isLoading } = useQuery({
    ...getMonitorsByIdOptions({
      path: {
        id: id!,
      },
    }),
    enabled: !!id,
  });

  const monitor = data?.data;

  // Safe JSON parsing with error handling
  const config = useMemo(() => {
    try {
      return JSON.parse(monitor?.config ?? "{}");
    } catch (error) {
      console.error("Failed to parse monitor config:", error);
      return {};
    }
  }, [monitor?.config]);

  const deleteMutation = useMutation({
    ...deleteMonitorsByIdMutation({
      path: {
        id: id!,
      },
    }),
    onSuccess: () => {
      console.log("deleted");
      toast.success("Monitor deleted");
      queryClient.invalidateQueries({
        queryKey: getMonitorsInfiniteQueryKey(),
      });
      navigate("/monitors");
    },
    onError: commonMutationErrorHandler("Failed to delete monitor"),
  });

  const pauseMutation = useMutation({
    ...patchMonitorsByIdMutation(),
    onSuccess: (res) => {
      toast.success(!res.data?.active ? "Monitor paused" : "Monitor resumed");
      setShowConfirmPause(false);

      queryClient.setQueryData(
        getMonitorsByIdQueryKey({
          path: {
            id: id!,
          },
        }),
        (oldData: UtilsApiResponseMonitorModel) => {
          if (!oldData) return oldData;
          return {
            ...oldData,
            data: {
              ...oldData.data,
              active: res.data?.active,
            },
          };
        }
      );
    },
    onError: commonMutationErrorHandler("Failed to pause monitor"),
  });

  const { data: heartbeatsResponse } = useQuery({
    ...getMonitorsByIdHeartbeatsOptions({
      path: {
        id: id!,
      },
      query: {
        limit: 150,
        reverse: true,
      },
    }),
    staleTime: 0,
    enabled: !!id,
    refetchOnMount: true,
  });

  useEffect(() => {
    if (heartbeatsResponse?.data) {
      setHeartbeatData(heartbeatsResponse.data!);
    }
  }, [heartbeatsResponse]);

  const handleDelete = () => {
    setShowConfirmDelete(true);
  };

  const { data: stats, refetch: refetchUptimeStats } = useQuery({
    ...getMonitorsByIdStatsUptimeOptions({
      path: {
        id: id!,
      },
    }),
  });

  useEffect(() => {
    if (!socket || !heartbeatsResponse) return;

    const roomName = `monitor:${id}`;

    const handleHeartbeat = (newHeartbeat: HeartbeatModel) => {
      // TODO: new heartbeats have different timestamp then from the api response
      setHeartbeatData((p) => [...p, newHeartbeat].slice(-150));

      // If it's important, update the react-query cache for important heartbeats
      if (newHeartbeat.important) {
        const queryKey = getMonitorsByIdHeartbeatsInfiniteQueryKey({
          path: { id: id! },
          query: { important: true, limit: 20 },
        });

        queryClient.setQueryData(
          queryKey,
          (oldData: {
            pageParams: number[];
            pages: { data: HeartbeatModel[] }[];
          }) => {
            if (!oldData) {
              // If no data, create a new structure
              return {
                pageParams: [0],
                pages: [{ data: [newHeartbeat] }],
              };
            }

            const flat = oldData.pages.flatMap((page) => page.data);
            const filtered = flat.filter((hb) => hb.id !== newHeartbeat.id);
            const newData = [newHeartbeat, ...filtered];

            // convert array to pages by 20
            const pages = [];
            for (let i = 0; i < newData.length; i += 20) {
              pages.push({ data: newData.slice(i, i + 20) });
            }

            return {
              pageParams: oldData.pageParams,
              pages: pages,
            };
          }
        );
      }

      refetchUptimeStats();
    };

    if (socketStatus === WebSocketStatus.CONNECTED) {
      socket.on(`${roomName}:heartbeat`, handleHeartbeat);
      socket.emit("join_room", roomName);
      console.log("Subscribed to heartbeat", roomName);
    }

    return () => {
      socket.off(`${roomName}:heartbeat`, handleHeartbeat);
      if (socketStatus === WebSocketStatus.CONNECTED) {
        socket.emit("leave_room", roomName);
      }
    };
  }, [
    socket,
    socketStatus,
    id,
    heartbeatsResponse,
    queryClient,
    refetchUptimeStats,
  ]);

  const lastHeartbeat =
    heartbeatData?.length > 0 ? heartbeatData[heartbeatData.length - 1] : null;

  const dataStats = useMemo(() => {
    if (!stats) return [];
    return [
      {
        label: "Last 24 hours",
        value: stats.data?.["24h"],
      },
      {
        label: "Last 7 days",
        value: stats.data?.["7d"],
      },
      {
        label: "Last 30 days",
        value: stats.data?.["30d"],
      },
      {
        label: "Last 365 days",
        value: stats.data?.["365d"],
      },
    ];
  }, [stats]);

  const resetMutation = useMutation({
    ...postMonitorsByIdResetMutation({
      path: {
        id: id!,
      },
    }),
    onSuccess: () => {
      toast.success("Monitor data reset successfully");
      setShowConfirmReset(false);

      queryClient.invalidateQueries({
        queryKey: getMonitorsByIdQueryKey({
          path: { id: id! },
        }),
      });

      queryClient.invalidateQueries({
        queryKey: getMonitorsByIdStatsUptimeQueryKey({
          path: { id: id! },
        }),
      });

      queryClient.invalidateQueries({
        predicate: (query) =>
          Array.isArray(query.queryKey) &&
          query.queryKey[0]?._id === "getMonitorsByIdStatsPoints" &&
          query.queryKey[0]?.path?.id === id,
      });

      queryClient.invalidateQueries({
        predicate: (query) =>
          Array.isArray(query.queryKey) &&
          query.queryKey[0]?._id === "getMonitorsByIdHeartbeats" &&
          query.queryKey[0]?.path?.id === id,
      });

      // Clear local heartbeat data
      setHeartbeatData([]);
    },
    onError: commonMutationErrorHandler("Failed to reset monitor data"),
  });

  return (
    <Layout
      pageName={`Monitors: ${monitor?.name ?? ""}`}
      isLoading={isLoading}
      error={error && <div>Error: {error.message}</div>}
    >
      <div>
        <BackButton to="/monitors" />
        <div className="pl-4">
          <span className="text-sm text-muted-foreground mr-2">
            {monitor?.type} monitor for
          </span>
          <a
            href={config?.url ?? "#"}
            className="text-blue-500 hover:underline"
            target="_blank"
            rel="noopener noreferrer"
          >
            {config?.url ?? ""}
          </a>
        </div>

        <div className="mt-4 mb-4">
          <div className="inline-flex border rounded-md overflow-hidden">
            <Button
              variant="ghost"
              className="rounded-none border-r"
              disabled={pauseMutation.isPending}
              onClick={() => {
                if (monitor?.active) {
                  setShowConfirmPause(true);
                } else {
                  pauseMutation.mutate({
                    path: {
                      id: id!,
                    },
                    body: {
                      active: !monitor?.active,
                    },
                  });
                }
              }}
            >
              {monitor?.active ? (
                <>
                  {pauseMutation.isPending ? (
                    <Loader2 className="animate-spin" />
                  ) : (
                    <Pause />
                  )}
                  Pause
                </>
              ) : (
                <>
                  {pauseMutation.isPending ? (
                    <Loader2 className="animate-spin" />
                  ) : (
                    <PlayIcon />
                  )}
                  Resume
                </>
              )}
            </Button>
            <Button
              variant="ghost"
              className="rounded-none border-r"
              onClick={() => navigate("/monitors/" + id + "/edit")}
            >
              <Edit />
              Edit
            </Button>
            <Button
              variant="ghost"
              className="rounded-none border-r"
              onClick={() => {
                navigate("/monitors/new", {
                  state: {
                    cloneData: monitor,
                  },
                });
              }}
            >
              <Copy />
              Clone
            </Button>
            <Button
              variant="destructive"
              className="rounded-none border-r"
              disabled={resetMutation.isPending}
              onClick={() => setShowConfirmReset(true)}
            >
              {resetMutation.isPending ? (
                <Loader2 className="animate-spin" />
              ) : (
                <RotateCcw />
              )}
              Reset Data
            </Button>
            <Button
              variant="destructive"
              className="rounded-none"
              onClick={handleDelete}
            >
              <Trash />
              Delete
            </Button>
          </div>
        </div>

        <div className="text-white space-y-6 mt-4">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-y-4 md:gap-4 mb-4">
            <Card className="p-4 rounded-xl gap-1">
              <div className="font-semibold">Current status</div>
              <div
                className={cn(
                  "font-semibold text-2xl",
                  lastHeartbeat?.status === 1 && "text-green-400",
                  lastHeartbeat?.status === 0 && "text-red-400",
                  lastHeartbeat?.status === 2 && "text-red-400",
                  lastHeartbeat?.status === 3 && "text-blue-400"
                )}
              >
                {lastHeartbeat?.status === 1 && "Up"}
                {lastHeartbeat?.status === 0 && "Down"}
                {lastHeartbeat?.status === 2 && "Down"}
                {lastHeartbeat?.status === 3 && "Maintenance"}
              </div>
            </Card>

            <Card className="p-4 rounded-xl col-span-2 gap-2">
              <div className="text-white font-semibold">Live Status</div>
              <BarHistory data={heartbeatData} />
              <div className="text-sm text-gray-400">
                Check every {monitor?.interval} seconds
              </div>
            </Card>
          </div>
        </div>

        <Card className="mb-4">
          <CardContent className="">
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4">
              {dataStats.map((item) => {
                return (
                  <div
                    key={item.label}
                    className="flex flex-1 flex-col justify-center gap-1 px-4 py-2 text-left md:border-l md:odd:border-l-0 lg:first:border-l-0"
                  >
                    <span className="text-xs text-muted-foreground">
                      {item.label}
                    </span>
                    <span className="text-xl font-bold leading-none sm:text-3xl">
                      {item.value?.toLocaleString()}{" "}
                      <span className="text-sm font-normal text-muted-foreground">
                        %
                      </span>
                    </span>
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>

        <Chart id={id!} />

        {id && <ImportantNotificationsList monitorId={id} />}
      </div>

      <AlertDialog open={showConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Are you absolutely sure?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete monitor
              and all related data.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => setShowConfirmDelete(false)}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                deleteMutation.mutate({
                  path: {
                    id: id!,
                  },
                })
              }
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending && <Loader2 className="animate-spin" />}
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={showConfirmPause}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirmation</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure want to pause?
            </AlertDialogDescription>
          </AlertDialogHeader>

          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => setShowConfirmPause(false)}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                pauseMutation.mutate({
                  path: {
                    id: id ?? "",
                  },
                  body: {
                    active: !data?.data?.active,
                  },
                })
              }
              disabled={pauseMutation.isPending}
            >
              {pauseMutation.isPending && <Loader2 className="animate-spin" />}
              Pause
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={showConfirmReset}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Reset Monitor Data</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently delete all heartbeat history and uptime
              statistics for this monitor. The monitor will appear as if it was
              just created. This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>

          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => setShowConfirmReset(false)}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                resetMutation.mutate({
                  path: {
                    id: id!,
                  },
                })
              }
              disabled={resetMutation.isPending}
              className="bg-orange-600 hover:bg-orange-700"
            >
              {resetMutation.isPending && <Loader2 className="animate-spin" />}
              Reset Data
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Layout>
  );
};

export default MonitorPage;
