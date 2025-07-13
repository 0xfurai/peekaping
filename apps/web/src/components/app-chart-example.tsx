"use client";

import * as React from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ReferenceArea,
  XAxis,
  YAxis,
} from "recharts";

import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  type ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useQuery } from "@tanstack/react-query";
import { getMonitorsByIdStatsPointsOptions } from "@/api/@tanstack/react-query.gen";
import type { MonitorStatPoint } from "@/api";
import { useTimezone } from "../context/timezone-context";
import { formatDateToTimezone } from "../lib/formatDateToTimezone";
// import { useMemo } from "react";

function getStatusRanges(data: MonitorStatPoint[]) {
  if (!data.length) return [];
  const ranges = [];
  let startIdx = 0;
  let currentColor = getColor(data[0]);

  function getColor(point: MonitorStatPoint) {
    if (!point) return null;
    if (point.maintenance) return "rgba(0, 0, 255, 0.5)";
    if (point.up !== 0 && point.down !== 0) return "rgba(255, 215, 0, 0.15)";
    if (point.up === 0 && point.down !== 0) return "rgba(255, 0, 0, 0.15)";
    if (point.up !== 0 && point.down === 0) return "none";
    return null;
  }

  for (let i = 1; i < data.length; i++) {
    const color = getColor(data[i]);
    if (color !== currentColor) {
      if (currentColor && data[startIdx].timestamp !== data[i].timestamp) {
        let x1 = data[startIdx].timestamp;
        // If currentColor is red and previous color is not yellow, shift left
        if (
          currentColor === "rgba(255, 0, 0, 0.15)" &&
          getColor(data[startIdx - 1]) !== "rgba(255, 215, 0, 0.15)" &&
          startIdx > 0
        ) {
          x1 = data[startIdx - 1].timestamp;
        }
        ranges.push({
          x1,
          x2: data[i].timestamp,
          color: currentColor,
        });
      }
      startIdx = i;
      currentColor = color;
    }
  }
  // Push the last range, filter zero-width
  if (
    currentColor &&
    data[startIdx].timestamp !== data[data.length - 1].timestamp
  ) {
    let x1 = data[startIdx].timestamp;
    if (
      currentColor === "rgba(255, 0, 0, 0.15)" &&
      getColor(data[startIdx - 1]) !== "rgba(255, 215, 0, 0.15)" &&
      startIdx > 0
    ) {
      x1 = data[startIdx - 1].timestamp;
    }
    ranges.push({
      x1,
      x2: data[data.length - 1].timestamp,
      color: currentColor,
    });
  }
  return ranges;
}

const chartConfig = {
  // visitors: {
  //   label: "Visitors",
  // },
} satisfies ChartConfig;

export function Chart({ id }: { id: string }) {
  const [timeRange, setTimeRange] = React.useState<
    "30m" | "3h" | "6h" | "24h" | "1week"
  >("30m");
  const { timezone } = useTimezone();

  // Helper to map timeRange to custom uptime params
  function getCustomUptimeParams(range: typeof timeRange) {
    const now = new Date();
    // const now = new Date("2025-06-04T20:00:00Z");
    // Truncate seconds and ms to the closest earlier minute
    now.setSeconds(0, 0);
    const since = new Date(now);
    let granularity = "minute";
    switch (range) {
      case "30m":
        since.setMinutes(now.getMinutes() - 30);
        granularity = "minute";
        break;
      case "3h":
        since.setHours(now.getHours() - 3);
        granularity = "minute";
        break;
      case "6h":
        since.setHours(now.getHours() - 6);
        granularity = "minute";
        break;
      case "24h":
        since.setDate(now.getDate() - 1);
        granularity = "hour";
        break;
      case "1week":
        since.setDate(now.getDate() - 7);
        granularity = "hour";
        break;
      default:
        break;
    }
    // Truncate seconds and ms for 'since' as well
    since.setSeconds(0, 0);
    return {
      since: since.toISOString(),
      until: now.toISOString(),
      granularity,
    };
  }

  // Only use custom uptime endpoint
  const { data: statpointsDataRaw } = useQuery({
    ...getMonitorsByIdStatsPointsOptions({
      path: { id: id! },
      query: getCustomUptimeParams(timeRange),
    }),
    refetchInterval: 20 * 1000,
    enabled: !!id,
  });

  // Map MonitorStatPoint to chart fields
  const chartData: unknown[] =
    statpointsDataRaw?.data?.points?.map((el) => {
      if (el.maintenance) {
        return {
          timestamp: el.timestamp,
          up: 0,
          down: 0,
          ping_max: null,
          ping: null,
          ping_min: null,
          maintenance: el.maintenance,
        };
      }

      return {
        timestamp: el.timestamp,
        up: el.up,
        down: el.down,
        ping_max: el.up ? el.ping_max : null,
        ping: el.up ? el.ping : null,
        ping_min: el.up ? el.ping_min : null,
        maintenance: el.maintenance,
      };
    }) || [];

  const statusRanges = getStatusRanges(chartData as MonitorStatPoint[]);

  const statsArray = [
    {
      key: "min",
      label: "Minimum",
      value: statpointsDataRaw?.data?.minPing || 0,
    },
    {
      key: "max",
      label: "Maximum",
      value: statpointsDataRaw?.data?.maxPing || 0,
    },
    {
      key: "avg",
      label: "Average",
      value: statpointsDataRaw?.data?.avgPing || 0,
    },
    {
      key: "uptime",
      label: "Uptime",
      value: statpointsDataRaw?.data?.uptime || 0,
    },
  ];

  return (
    <Card>
      <CardHeader className="flex items-center gap-2 space-y-0 border-b sm:flex-row">
        <div className="grid flex-1 gap-1 text-center sm:text-left">
          <CardTitle>Response time</CardTitle>
        </div>
        <div className="flex gap-2 items-center">
          <Select
            value={timeRange}
            onValueChange={(v: typeof timeRange) => setTimeRange(v)}
          >
            <SelectTrigger
              className="w-[160px] rounded-lg sm:ml-auto"
              aria-label="Select a value"
            >
              <SelectValue placeholder="Last 3 months" />
            </SelectTrigger>
            <SelectContent className="rounded-xl">
              <SelectItem value="30m" className="rounded-lg">
                Last 30 minutes
              </SelectItem>
              <SelectItem value="3h" className="rounded-lg">
                Last 3 hours
              </SelectItem>
              <SelectItem value="6h" className="rounded-lg">
                Last 6 hours
              </SelectItem>
              <SelectItem value="24h" className="rounded-lg">
                Last 24 hours
              </SelectItem>
              <SelectItem value="1week" className="rounded-lg">
                Last 7 days
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
      </CardHeader>

      <CardContent className="px-2 pt-2 sm:px-6 sm:pt-6">
        <ChartContainer
          config={chartConfig}
          className="aspect-auto h-[250px] w-full"
        >
          <LineChart
            data={chartData}
            accessibilityLayer
            margin={{ left: 12, right: 12 }}
          >
            <CartesianGrid vertical={false} />
            <XAxis
              dataKey="timestamp"
              tickLine={false}
              axisLine={false}
              tickMargin={8}
              tickFormatter={(timestamp) => {
                return formatDateToTimezone(timestamp, timezone, {
                  month: "short",
                  day: "numeric",
                  hour: "2-digit",
                  minute: "2-digit",
                });
              }}
            />
            <YAxis
              tickLine={false}
              axisLine={false}
              label={{
                value: "Resp. Time (ms)",
                angle: -90,
                position: "insideLeft",
                offset: 0,
                style: { textAnchor: "middle" },
              }}
            />
            <ChartTooltip
              cursor={false}
              content={
                <ChartTooltipContent
                  labelFormatter={(_, points) => {
                    return formatDateToTimezone(
                      points[0].payload.timestamp,
                      timezone,
                      {
                        month: "short",
                        day: "numeric",
                        hour: "2-digit",
                        minute: "2-digit",
                      }
                    );
                  }}
                  indicator="dot"
                />
              }
            />
            <Line
              dataKey="ping"
              type="monotone"
              stroke="var(--chart-4)"
              // strokeWidth={2}
              dot={true}
              connectNulls={false}
              isAnimationActive={false}
            />
            <Line
              dataKey="ping_min"
              type="monotone"
              stroke="var(--chart-3)"
              // strokeWidth={2}
              dot={true}
              connectNulls={false}
              isAnimationActive={false}
            />
            <Line
              dataKey="ping_max"
              type="monotone"
              stroke="var(--chart-2)"
              // strokeWidth={2}
              dot={true}
              connectNulls={false}
              isAnimationActive={false}
            />
            {statusRanges
              .filter((e: { color: string | null }) => e.color !== "none")
              .map(
                (
                  {
                    x1,
                    x2,
                    color,
                  }: {
                    x1: number | undefined;
                    x2: number | undefined;
                    color: string;
                  },
                  idx: number
                ) => (
                  <ReferenceArea
                    key={idx}
                    x1={x1 ?? 0}
                    x2={x2 ?? 0}
                    strokeOpacity={0}
                    fill={color}
                  />
                )
              )}
          </LineChart>
        </ChartContainer>
      </CardContent>

      <CardFooter className="flex flex-col items-stretch space-y-0 border-t p-0">
        <div className="grid grid-cols-1 md:grid-cols-4">
          {statsArray.map(
            (item: { key: string; label: string; value: number }) => (
              <div
                key={item.key}
                className="flex flex-1 flex-col justify-center gap-1  px-6 py-4 text-left even:border-l sm:border-l sm:border-t-0 sm:px-8 sm:py-6"
              >
                <span className="text-xs text-muted-foreground">
                  {item.label}
                </span>
                <span className="text-lg font-bold leading-none sm:text-3xl">
                  {item.value.toLocaleString()}{" "}
                  <span className="text-sm font-normal text-muted-foreground">
                    {item.key === "uptime" ? "%" : "ms"}
                  </span>
                </span>
              </div>
            )
          )}
        </div>
      </CardFooter>
    </Card>
  );
}
