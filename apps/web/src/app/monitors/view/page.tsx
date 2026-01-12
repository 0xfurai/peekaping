import { type HeartbeatModel, type UtilsApiResponseMonitorModel } from "@/api";
import {
    deleteMonitorsByIdMutation,
    getMonitorsByIdHeartbeatsInfiniteQueryKey,
    getMonitorsByIdHeartbeatsOptions,
    getMonitorsByIdOptions,
    getMonitorsByIdQueryKey,
    getMonitorsByIdStatsUptimeOptions,
    getMonitorsByIdStatsUptimeQueryKey,
    getMonitorsByIdTlsOptions,
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
    Shield,
    AlertTriangle,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { cn, commonMutationErrorHandler } from "@/lib/utils";
import ImportantNotificationsList from "../components/important-notifications-list";
import { BackButton } from "@/components/back-button";
import dayjs from "dayjs";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import clsx from "clsx";
import { useOrganizationStore } from "@/store/organization";

function formatDuration(ms: number, t: (key: string) => string): string {
    // Handle negative durations (clock skew) by returning empty string
    if (ms <= 0) {
        return "";
    }

    const d = dayjs.duration(ms);

    const parts: string[] = [];

    if (d.days()) parts.push(`${d.days()}d`);
    if (d.hours()) parts.push(`${d.hours()}h`);
    if (d.minutes()) parts.push(`${d.minutes()}m`);

    if (parts.length === 0) {
        return "";
    }

    return t("monitors.view.for") + " " + parts.join(" ");
}

// TLS response types based on the backend certificate model
interface CertificateInfo {
    subject: string;
    issuer: string;
    fingerprint: string;
    fingerprint256: string;
    serialNumber: string;
    validFrom: string;
    validTo: string;
    daysRemaining: number;
    certType: string;
    validFor?: string[];
    signatureAlgorithm: string;
    publicKeyAlgorithm: string;
    keySize?: number;
    version: number;
}

interface TLSInfo {
    valid: boolean;
    certInfo?: CertificateInfo;
}

const MonitorPage = () => {
    const { id } = useParams();
    const navigate = useNavigate();
    const { t } = useLocalizedTranslation();
    const queryClient = useQueryClient();
    const { socket, status: socketStatus } = useWebSocket();
    const { currentOrganization } = useOrganizationStore();
    const slug = currentOrganization?.slug;
    const listPath = slug ? `/${slug}/monitors` : "/monitors";

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

    const hasCertCheckExpire = useMemo(() => {
        if (!monitor) return false;
        if (!monitor?.type?.toLowerCase().startsWith("http")) return false;

        try {
            const config = JSON.parse(monitor?.config ?? "{}");
            return config?.check_cert_expiry ?? false;
        } catch (err) {
            console.error("Failed to parse monitor config:", err);
            return false;
        }
    }, [monitor]);

    // Fetch TLS info for HTTP monitors using React Query
    const { data: tlsData, isLoading: tlsLoading } = useQuery({
        ...getMonitorsByIdTlsOptions({
            path: {
                id: id!,
            },
        }),
        enabled:
            !!id && !!monitor && monitor.type?.toLowerCase().startsWith("http"),
    });

    // Transform TLS data to match the expected format
    const tlsInfo = useMemo(() => {
        if (!tlsData?.data) return null;

        // The API returns the TLS info as a generic object, so we need to type it properly
        const tlsInfoData = tlsData.data as TLSInfo;

        if (!tlsInfoData || typeof tlsInfoData !== "object") return null;

        return {
            valid: tlsInfoData.valid,
            certInfo: tlsInfoData.certInfo
                ? {
                    subject: tlsInfoData.certInfo.subject,
                    issuer: tlsInfoData.certInfo.issuer,
                    validTo: tlsInfoData.certInfo.validTo,
                    daysRemaining: tlsInfoData.certInfo.daysRemaining,
                }
                : undefined,
        };
    }, [tlsData]);

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
            toast.success(t("monitors.toasts.deleted"));
            queryClient.invalidateQueries({
                queryKey: getMonitorsInfiniteQueryKey({}),
            });
            navigate(listPath);
        },
        onError: commonMutationErrorHandler("Failed to delete monitor"),
    });

    const pauseMutation = useMutation({
        ...patchMonitorsByIdMutation(),
        onSuccess: (res) => {
            toast.success(!res.data?.active ? t("monitors.toasts.paused") : t("monitors.toasts.resumed"));
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

    const lastHeartbeat =
        heartbeatData?.length > 0 ? heartbeatData[heartbeatData.length - 1] : null;

    const { data: stats, refetch: refetchUptimeStats } = useQuery({
        ...getMonitorsByIdStatsUptimeOptions({
            path: {
                id: id!,
            },
        }),
    });

    const {
        data: lastImportantHeartbeatData,
        refetch: refetchLastImportantHeartbeat,
    } = useQuery({
        ...getMonitorsByIdHeartbeatsOptions({
            path: { id: id! },
            query: {
                limit: 1,
                important: true,
                reverse: true,
            },
        }),
        enabled: !!id,
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
            refetchLastImportantHeartbeat();
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
        refetchLastImportantHeartbeat,
    ]);

    const dataStats = useMemo(() => {
        if (!stats) return [];
        return [
            {
                label: t("monitors.view.stats.last_24_hours"),
                value: stats.data?.["24h"],
            },
            {
                label: t("monitors.view.stats.last_7_days"),
                value: stats.data?.["7d"],
            },
            {
                label: t("monitors.view.stats.last_30_days"),
                value: stats.data?.["30d"],
            },
            {
                label: t("monitors.view.stats.last_365_days"),
                value: stats.data?.["365d"],
            },
        ];
    }, [stats, t]);

    const resetMutation = useMutation({
        ...postMonitorsByIdResetMutation({
            path: {
                id: id!,
            },
        }),
        onSuccess: () => {
            toast.success(t("monitors.toasts.reset_success"));
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

    const lastImportantHeartbeat = lastImportantHeartbeatData?.data?.[0];
    const lastImportantHeartbeatTime = lastImportantHeartbeat?.time;
    const lastImportantHeartbeatDuration = lastImportantHeartbeatTime
        ? dayjs().diff(dayjs(lastImportantHeartbeatTime), "milliseconds")
        : 0;

    const lihText = formatDuration(lastImportantHeartbeatDuration, t);

    return (
        <Layout
            pageName={`Monitors: ${monitor?.name ?? ""}`}
            isLoading={isLoading}
            error={error && <div>Error: {error.message}</div>}
        >
            <div>
                <BackButton to={listPath} />
                <div className="pl-4">
                    <span className="text-sm text-muted-foreground mr-2">
                        {monitor?.type} {t("monitors.view.monitor_for")}
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
                                    {t("monitors.view.buttons.pause")}
                                </>
                            ) : (
                                <>
                                    {pauseMutation.isPending ? (
                                        <Loader2 className="animate-spin" />
                                    ) : (
                                        <PlayIcon />
                                    )}
                                    {t("monitors.view.buttons.resume")}
                                </>
                            )}
                        </Button>
                        <Button
                            variant="ghost"
                            className="rounded-none border-r"
                            onClick={() => navigate("edit")}
                        >
                            <Edit />
                            {t("monitors.view.buttons.edit")}
                        </Button>
                        <Button
                            variant="ghost"
                            className="rounded-none border-r"
                            onClick={() => {
                                navigate("../new", {
                                    state: {
                                        cloneData: monitor,
                                    },
                                });
                            }}
                        >
                            <Copy />
                            {t("monitors.view.buttons.clone")}
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
                            {t("monitors.view.buttons.reset_data")}
                        </Button>
                        <Button
                            variant="destructive"
                            className="rounded-none"
                            onClick={handleDelete}
                        >
                            <Trash />
                            {t("monitors.view.buttons.delete")}
                        </Button>
                    </div>
                </div>

                <div className="text-white space-y-6 mt-4">
                    <div className="grid grid-cols-4 gap-4 mb-4">
                        <Card
                            className={clsx(
                                "p-4 rounded-xl gap-1 col-span-4",
                                hasCertCheckExpire ? "lg:col-span-1" : "lg:col-span-1"
                            )}
                        >
                            <div className="font-semibold">{t("monitors.view.current_status")}</div>
                            {monitor?.active ? (
                                <div
                                    className={cn(
                                        "font-semibold text-2xl",
                                        lastHeartbeat?.status === 1 && "text-green-400",
                                        lastHeartbeat?.status === 0 && "text-red-400",
                                        lastHeartbeat?.status === 2 && "text-red-400",
                                        lastHeartbeat?.status === 3 && "text-blue-400"
                                    )}
                                >
                                    {lastHeartbeat?.status === 1 && t("monitors.view.status.up")}
                                    {lastHeartbeat?.status === 0 && t("monitors.view.status.down")}
                                    {lastHeartbeat?.status === 2 && t("monitors.view.status.down")}
                                    {lastHeartbeat?.status === 3 && t("monitors.view.status.maintenance")}
                                </div>
                            ) : (
                                <div className="font-semibold text-2xl">{t("monitors.view.status.paused")}</div>
                            )}

                            {monitor?.active && lastImportantHeartbeatDuration > 0 && (
                                <div className="text-sm text-gray-400">{lihText}</div>
                            )}
                            {!monitor?.active && lastHeartbeat?.time && (
                                <div className="text-sm text-gray-400">
                                    {formatDuration(
                                        dayjs().diff(dayjs(lastHeartbeat?.time), "milliseconds"),
                                        t
                                    )}
                                </div>
                            )}
                        </Card>

                        {/* Certificate Information Card - Only show for HTTPS monitors */}
                        {hasCertCheckExpire && (
                            <Card className="p-4 rounded-xl gap-1 col-span-4 lg:col-span-1">
                                <div className="font-semibold flex items-center gap-2">
                                    <Shield className="w-4 h-4" />
                                    {t("monitors.view.certificate")}
                                </div>
                                {tlsLoading ? (
                                    <div className="flex items-center gap-2 text-sm text-gray-400">
                                        <Loader2 className="w-4 h-4 animate-spin" />
                                        {t("common.loading")}
                                    </div>
                                ) : tlsInfo?.certInfo ? (
                                    <div className="space-y-1">
                                        <div
                                            className={cn(
                                                "font-semibold text-lg",
                                                tlsInfo.certInfo.daysRemaining &&
                                                tlsInfo.certInfo.daysRemaining > 30 &&
                                                "text-green-400",
                                                tlsInfo.certInfo.daysRemaining &&
                                                tlsInfo.certInfo.daysRemaining <= 30 &&
                                                tlsInfo.certInfo.daysRemaining > 7 &&
                                                "text-yellow-400",
                                                tlsInfo.certInfo.daysRemaining &&
                                                tlsInfo.certInfo.daysRemaining <= 7 &&
                                                "text-red-400"
                                            )}
                                        >
                                            {tlsInfo.certInfo.daysRemaining
                                                ? `${tlsInfo.certInfo.daysRemaining} ${t("common.days")}`
                                                : t("common.unknown")}
                                        </div>
                                        <div className="text-xs text-gray-400">
                                            {tlsInfo.certInfo.validTo && (
                                                <>
                                                    {t("common.expires")}{" "}
                                                    {dayjs(tlsInfo.certInfo.validTo).format(
                                                        "MMM D, YYYY"
                                                    )}
                                                </>
                                            )}
                                        </div>
                                        {tlsInfo.certInfo.daysRemaining &&
                                            tlsInfo.certInfo.daysRemaining <= 30 && (
                                                <div className="flex items-center gap-1 text-xs text-yellow-400">
                                                    <AlertTriangle className="w-3 h-3" />
                                                    {t("monitors.view.expiring_soon")}
                                                </div>
                                            )}
                                    </div>
                                ) : (
                                    <div className="text-sm text-gray-400">
                                        {t("monitors.view.no_certificate_data")}
                                    </div>
                                )}
                            </Card>
                        )}

                        <Card
                            className={clsx(
                                "p-4 rounded-xl gap-2 col-span-4",
                                hasCertCheckExpire ? "lg:col-span-2" : "lg:col-span-3"
                            )}
                        >
                            <div className="text-white font-semibold">{t("monitors.view.live_status")}</div>
                            <BarHistory data={heartbeatData} />
                            <div className="text-sm text-gray-400">
                                {t("monitors.view.check_every")} {monitor?.interval} {t("monitors.view.seconds")}
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
                        <AlertDialogTitle>{t("monitors.view.dialogs.delete.title")}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t("monitors.view.dialogs.delete.description")}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel onClick={() => setShowConfirmDelete(false)}>
                            {t("common.cancel")}
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
                            {t("monitors.view.buttons.delete")}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>

            <AlertDialog open={showConfirmPause}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t("monitors.view.dialogs.pause.title")}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t("monitors.view.dialogs.pause.description")}
                        </AlertDialogDescription>
                    </AlertDialogHeader>

                    <AlertDialogFooter>
                        <AlertDialogCancel onClick={() => setShowConfirmPause(false)}>
                            {t("common.cancel")}
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
                            {t("monitors.view.buttons.pause")}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>

            <AlertDialog open={showConfirmReset}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t("monitors.view.dialogs.reset.title")}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t("monitors.view.dialogs.reset.description")}
                        </AlertDialogDescription>
                    </AlertDialogHeader>

                    <AlertDialogFooter>
                        <AlertDialogCancel onClick={() => setShowConfirmReset(false)}>
                            {t("common.cancel")}
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
                            {t("monitors.view.buttons.reset_data")}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </Layout>
    );
};

export default MonitorPage;
