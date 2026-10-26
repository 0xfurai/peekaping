import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import {
  getProbesByIdAvailabilityOptions,
  getProbesByIdOptions,
} from "@/api/@tanstack/react-query.gen";
import { BackButton } from "@/components/back-button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useTimezone } from "@/context/timezone-context";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import Layout from "@/layout";
import { AvailabilityBar, InstanceBar } from "../components/availability-bar";
import { formatUptime, instanceName } from "../components/availability-layout";
import ProbeStatusBadge from "../components/probe-status-badge";

// How often the probe and its availability refresh, so that a probe that went offline shows up
const REFRESH_INTERVAL_MS = 30_000;

// The instances' connections always cover the last 24 hours, whatever the range
const CONNECTIONS_WINDOW_MS = 24 * 60 * 60 * 1000;

const RANGES = ["24h", "7d", "30d"] as const;
type Range = (typeof RANGES)[number];

const isNotFound = (error: unknown) =>
  (error as { response?: { status?: number } } | null)?.response?.status ===
  404;

const ProbePage = () => {
  const { id } = useParams<{ id: string }>();
  const { t } = useLocalizedTranslation();
  const { timezone } = useTimezone();
  const [range, setRange] = useState<Range>("24h");

  const probeQuery = useQuery({
    ...getProbesByIdOptions({ path: { id: id! } }),
    enabled: !!id,
    refetchInterval: REFRESH_INTERVAL_MS,
  });
  const availabilityQuery = useQuery({
    ...getProbesByIdAvailabilityOptions({
      path: { id: id! },
      query: { range },
    }),
    enabled: !!id,
    refetchInterval: REFRESH_INTERVAL_MS,
    // Changing the range keeps the old bar in place until the new one arrives
    placeholderData: keepPreviousData,
  });

  const probe = probeQuery.data?.data;
  const availability = availabilityQuery.data?.data;
  // The previous range's data, shown while the new range loads, is dimmed so it isn't read as the new one
  const stale = availabilityQuery.isPlaceholderData
    ? "opacity-50 transition-opacity"
    : "transition-opacity";

  if (probeQuery.isLoading) {
    return (
      <Layout pageName={t("probes.page_name")}>{t("common.loading")}</Layout>
    );
  }

  if (probeQuery.isError || !probe) {
    return (
      <Layout pageName={t("probes.page_name")}>
        <BackButton to="/probes" />
        <p className="text-sm text-muted-foreground" data-testid="probe-error">
          {isNotFound(probeQuery.error)
            ? t("probes.detail.not_found")
            : t("probes.messages.failed_to_load")}
        </p>
      </Layout>
    );
  }

  const instancesFrom = availability
    ? new Date(
        Date.parse(availability.to) - CONNECTIONS_WINDOW_MS
      ).toISOString()
    : "";

  const rangeLabels: Record<Range, string> = {
    "24h": t("timezone.last_24_hours"),
    "7d": t("timezone.last_7_days"),
    "30d": t("monitors.view.stats.last_30_days"),
  };

  return (
    <Layout pageName={probe.name}>
      <div className="flex flex-col gap-4">
        <div>
          <BackButton to="/probes" className="mb-2" />
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4">
            <h2 className="text-xl font-semibold break-words">{probe.name}</h2>
            <ProbeStatusBadge status={probe.status} />
            {probe.location && (
              <span className="text-sm text-muted-foreground break-words">
                {probe.location}
              </span>
            )}
          </div>
        </div>

        {availabilityQuery.isError && !availability && (
          <p className="text-sm text-destructive">
            {t("probes.messages.failed_to_load")}
          </p>
        )}

        {availability?.in_process && (
          <Card>
            <CardContent>
              <p
                className="text-sm text-muted-foreground"
                data-testid="probe-in-process-note"
              >
                {t("probes.availability.in_process_note")}
              </p>
            </CardContent>
          </Card>
        )}

        {availability && !availability.in_process && (
          <>
            <Card>
              <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
                <CardTitle>{t("probes.availability.title")}</CardTitle>
                <Select
                  value={range}
                  onValueChange={(value: Range) => setRange(value)}
                >
                  <SelectTrigger
                    className="w-[160px] rounded-lg"
                    aria-label={t("probes.availability.range_label")}
                    data-testid="probe-range"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="rounded-xl">
                    {RANGES.map((value) => (
                      <SelectItem
                        key={value}
                        value={value}
                        className="rounded-lg"
                      >
                        {rangeLabels[value]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </CardHeader>
              <CardContent className={`flex flex-col gap-3 ${stale}`}>
                <div className="flex items-baseline gap-2">
                  <span
                    className="text-2xl font-semibold tabular-nums"
                    data-testid="probe-uptime"
                  >
                    {formatUptime(availability.uptime_percent)}
                  </span>
                  <span className="text-sm text-muted-foreground">
                    {t("timezone.uptime")}
                  </span>
                </div>
                <AvailabilityBar availability={availability} />
                <div className="flex justify-between gap-2 text-xs text-muted-foreground">
                  <span>
                    {formatDateToTimezone(availability.from, timezone, {
                      year: undefined,
                      second: undefined,
                    })}
                  </span>
                  <span>{t("probes.availability.now")}</span>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>
                  {t("probes.availability.instances_title")}
                </CardTitle>
              </CardHeader>
              <CardContent className={`flex flex-col gap-4 ${stale}`}>
                {(availability.instances ?? []).length === 0 && (
                  <p className="text-sm text-muted-foreground">
                    {t("probes.table.no_instances")}
                  </p>
                )}
                {(availability.instances ?? []).map((instance) => (
                  <div
                    key={instance.id}
                    data-testid="instance-row"
                    className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-4"
                  >
                    <div className="min-w-0 sm:w-56 sm:shrink-0">
                      <div className="truncate font-mono text-sm">
                        {instanceName(instance)}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {instance.version}
                      </div>
                    </div>
                    <div className="min-w-0 flex-1">
                      <InstanceBar
                        instance={instance}
                        from={instancesFrom}
                        to={availability.to}
                      />
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>
          </>
        )}
      </div>
    </Layout>
  );
};

export default ProbePage;
