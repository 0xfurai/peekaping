import { useState, useEffect } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  getStatusPagesSlugBySlugOptions,
  getStatusPagesSlugBySlugMonitorsOptions,
} from "@/api/@tanstack/react-query.gen";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  AlertCircle,
  CheckCircle,
  XCircle,
  Clock,
  Activity,
  TrendingUp,
  RefreshCw,
} from "lucide-react";
import type { StatusPageMonitorWithHeartbeatsAndUptimeDto } from "@/api/types.gen";
import BarHistory from "@/components/bars";
import { last } from "@/lib/utils";
import { ThemeToggle } from "../../../components/theme-toggle";

const PublicStatusPage = ({ incomingSlug = "" }: { incomingSlug?: string }) => {
  const params = useParams<{ slug: string }>();
  const slug = incomingSlug ?? params.slug;

  const [refreshInterval, setRefreshInterval] = useState(30);
  const [countdown, setCountdown] = useState(refreshInterval);
  const [lastUpdated, setLastUpdated] = useState<Date>(new Date());

  const {
    data: statusPageData,
    isLoading: statusPageLoading,
    error: statusPageError,
    refetch: refetchStatusPage,
  } = useQuery({
    ...getStatusPagesSlugBySlugOptions({
      path: {
        slug: slug!,
      },
    }),
    enabled: !!slug,
  });

  const statusPage = statusPageData?.data;

  // Update refresh interval when status page data is loaded
  useEffect(() => {
    if (
      statusPage?.auto_refresh_interval &&
      statusPage.auto_refresh_interval > 0
    ) {
      setRefreshInterval(statusPage.auto_refresh_interval);
      setCountdown(statusPage.auto_refresh_interval);
    }
  }, [statusPage?.auto_refresh_interval]);

  // Fetch monitors data with heartbeats and uptime for the status page
  const {
    data: monitorsData,
    isLoading: monitorsLoading,
    refetch: refetchMonitors,
  } = useQuery({
    ...getStatusPagesSlugBySlugMonitorsOptions({
      path: {
        slug: slug!,
      },
    }),
    enabled: !!slug && !!statusPage,
  });

  const monitors = monitorsData?.data || [];

  // Auto-refresh logic
  useEffect(() => {
    if (!slug || !statusPage) return;

    const interval = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          // Time to refresh
          refetchStatusPage();
          refetchMonitors();
          setLastUpdated(new Date());
          return refreshInterval;
        }
        return prev - 1;
      });
    }, 1000);

    return () => clearInterval(interval);
  }, [slug, statusPage, refreshInterval, refetchStatusPage, refetchMonitors]);

  // Format countdown as MM:SS
  const formatCountdown = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins.toString().padStart(2, "0")}:${secs
      .toString()
      .padStart(2, "0")}`;
  };

  // Format last updated timestamp
  const formatLastUpdated = (date: Date) => {
    return date.toLocaleString("en-US", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    });
  };

  // Handle loading state
  if (statusPageLoading) {
    return (
      <div className="min-h-screen bg-background p-4">
        <div className="max-w-4xl mx-auto">
          <div className="space-y-6">
            <Skeleton className="h-12 w-1/3" />
            <Skeleton className="h-6 w-1/2" />
            <div className="space-y-4">
              {Array.from({ length: 5 }, (_, i) => (
                <Skeleton key={i} className="h-20 w-full" />
              ))}
            </div>
          </div>
        </div>
      </div>
    );
  }

  // Handle error state
  if (statusPageError) {
    return (
      <div className="min-h-screen bg-background p-4">
        <div className="max-w-4xl mx-auto">
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>
              Status page not found or is not published.
            </AlertDescription>
          </Alert>
        </div>
      </div>
    );
  }

  // Check if status page exists and is published
  if (!statusPage || !statusPage.published) {
    return (
      <div className="min-h-screen bg-background p-4">
        <div className="max-w-4xl mx-auto">
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>
              Status page not found or is not published.
            </AlertDescription>
          </Alert>
        </div>
      </div>
    );
  }

  const getStatusIcon = (status: number) => {
    switch (status) {
      case 1: // Up
        return <CheckCircle className="h-5 w-5 text-green-500" />;
      case 0: // Down
      case 2: // Down
        return <XCircle className="h-5 w-5 text-red-500" />;
      case 3: // Maintenance
        return <Clock className="h-5 w-5 text-blue-500" />;
      default:
        return <Activity className="h-5 w-5 text-gray-500" />;
    }
  };

  const getStatusText = (status: number) => {
    switch (status) {
      case 1:
        return "Operational";
      case 0:
      case 2:
        return "Down";
      case 3:
        return "Maintenance";
      default:
        return "Unknown";
    }
  };

  const getOverallStatus = () => {
    if (monitors.length === 0)
      return { status: 1, text: "All Systems Operational" };

    const hasDown = monitors.some(
      (m: StatusPageMonitorWithHeartbeatsAndUptimeDto) => {
        const lastHeartbeat = last(m.heartbeats || []);
        return lastHeartbeat?.status === 0 || lastHeartbeat?.status === 2;
      }
    );
    const hasMaintenance = monitors.some(
      (m: StatusPageMonitorWithHeartbeatsAndUptimeDto) => {
        const lastHeartbeat = last(m.heartbeats || []);
        return lastHeartbeat?.status === 3;
      }
    );

    if (hasDown) return { status: 0, text: "Partial System Outage" };
    if (hasMaintenance) return { status: 3, text: "Under Maintenance" };
    return { status: 1, text: "All Systems Operational" };
  };

  const formatUptime = (uptime: number) => {
    return `24h ${uptime.toFixed(2)}%`;
  };

  const overallStatus = getOverallStatus();

  return (
    <div className="min-h-screen bg-background">
      <div className="max-w-4xl mx-auto p-4">
        <div className="text-center mb-8">
          {statusPage.icon && (
            <div className="mb-4">
              <img
                src={statusPage.icon}
                alt="Status Page Icon"
                className="h-16 w-16 mx-auto"
                onError={(e) => {
                  e.currentTarget.style.display = "none";
                }}
              />
            </div>
          )}

          <h1 className="text-3xl font-bold mb-2">{statusPage.title}</h1>

          {statusPage.description && (
            <p className="text-muted-foreground mb-4">
              {statusPage.description}
            </p>
          )}

          {/* Theme Toggle */}
          <div className="flex justify-center mb-4">
            <ThemeToggle />
          </div>

          {/* Overall Status */}
          <div className="flex items-center justify-center gap-2 mb-6">
            {getStatusIcon(overallStatus.status)}
            <span className="text-lg font-semibold">{overallStatus.text}</span>
          </div>

          {/* Monitors */}
          <div className="space-y-4">
            {monitorsLoading && (
              <div className="space-y-4">
                {Array.from({ length: 3 }, (_, i) => (
                  <Skeleton key={i} className="h-20 w-full" />
                ))}
              </div>
            )}

            {!monitorsLoading && monitors.length === 0 && (
              <Card>
                <CardContent className="p-6 text-center">
                  <p className="text-muted-foreground">
                    No monitors configured for this status page.
                  </p>
                </CardContent>
              </Card>
            )}

            {!monitorsLoading &&
              monitors.length > 0 &&
              monitors.map(
                (monitor: StatusPageMonitorWithHeartbeatsAndUptimeDto) => {
                  const lastHeartbeat = last(monitor.heartbeats || []);
                  const lastHeartbeatStatus = lastHeartbeat?.status;

                  return (
                    <Card key={monitor.id}>
                      <CardContent className="flex flex-col items-center justify-between gap-2 md:flex-row">
                        <div className="flex justify-between items-center gap-2 w-full">
                          <div className="flex items-center gap-2">
                            {getStatusIcon(lastHeartbeatStatus || 0)}
                            <div>
                              <h3 className="font-semibold">{monitor.name}</h3>
                            </div>
                          </div>

                          <div className="flex items-center gap-2">
                            {monitor.uptime_24h !== undefined && (
                              <Badge
                                variant="outline"
                                className="flex items-center gap-1"
                              >
                                <TrendingUp className="h-3 w-3" />
                                {formatUptime(monitor.uptime_24h)}
                              </Badge>
                            )}

                            <Badge
                              variant={
                                lastHeartbeatStatus === 1
                                  ? "default"
                                  : lastHeartbeatStatus === 0 ||
                                    lastHeartbeatStatus === 2
                                  ? "destructive"
                                  : lastHeartbeatStatus === 3
                                  ? "secondary"
                                  : "outline"
                              }
                            >
                              {getStatusText(lastHeartbeatStatus || 0)}
                            </Badge>
                          </div>
                        </div>

                        {/* Heartbeats Section - Only show if there are heartbeats */}
                        {monitor.heartbeats &&
                          monitor.heartbeats.length > 0 && (
                            <div className="w-full">
                              <BarHistory
                                data={monitor.heartbeats}
                                segmentWidth={6}
                                gap={2}
                                barHeight={16}
                                borderRadius={2}
                                tooltip={false}
                              />
                            </div>
                          )}
                      </CardContent>
                    </Card>
                  );
                }
              )}
          </div>

          {statusPage.footer_text && (
            <div className="mt-8 pt-8 border-t text-center">
              <p className="text-sm text-muted-foreground">
                {statusPage.footer_text}
              </p>
            </div>
          )}

          {/* Auto-refresh info */}
          <div className="flex items-center justify-center gap-4 text-sm text-muted-foreground mb-4 mt-4">
            <div className="flex items-center gap-1">
              <RefreshCw className="h-4 w-4" />
              <span>Last Updated: {formatLastUpdated(lastUpdated)}</span>
            </div>
            <div className="flex items-center gap-1">
              <Clock className="h-4 w-4" />
              <span className="w-[150px]">
                Refresh in: {formatCountdown(countdown)}
              </span>
            </div>
            <button
              onClick={() => {
                refetchStatusPage();
                refetchMonitors();
                setLastUpdated(new Date());
                setCountdown(refreshInterval);
              }}
              className="flex items-center gap-1 text-primary hover:text-primary/80 transition-colors"
            >
              <RefreshCw className="h-4 w-4" />
              <span>Refresh Now</span>
            </button>
          </div>
        </div>

        <div className="text-center">
          <p className="text-xs text-muted-foreground">
            Powered by{" "}
            <a
              href="https://github.com/0xfurai/peekaping"
              className="underline hover:text-foreground"
            >
              Peekaping
            </a>
          </p>
        </div>
      </div>
    </div>
  );
};

export default PublicStatusPage;
