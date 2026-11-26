import React, { useEffect, useRef, useState } from "react";
import {
  Tooltip,
  TooltipTrigger,
  TooltipProvider,
  TooltipContent,
} from "./ui/tooltip";
import { useTimezone } from "../context/timezone-context";
import { formatDateToTimezone } from "../lib/formatDateToTimezone";
import { cn } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { instanceLabel } from "@/app/monitors/locations";
import HeartbeatTooltipBody, { type HistoryHeartbeat } from "./heartbeat-tooltip-body";

type BarHistoryProps = {
  data: HistoryHeartbeat[];
  segmentWidth?: number;
  gap?: number;
  barHeight?: number;
  borderRadius?: number;
  tooltip?: boolean;
  // The hostname of every probe instance on record, by id. A heartbeat's tooltip names its instance
  // by the hostname here, else by the first 8 characters of the id.
  instanceHostnames?: ReadonlyMap<string, string>;
};

const BarHistory: React.FC<BarHistoryProps> = ({
  data,
  segmentWidth = 8,
  gap = 3,
  barHeight = 24,
  borderRadius = 3,
  tooltip = true,
  instanceHostnames,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [visibleCount, setVisibleCount] = useState(0);
  const { timezone } = useTimezone();
  const { t } = useLocalizedTranslation();

  useEffect(() => {
    const updateCount = () => {
      if (containerRef.current) {
        const containerWidth = containerRef.current.offsetWidth;
        // Calculate how many segments can fit in the container
        // Each segment takes segmentWidth + gap space, except the last one which doesn't need gap
        const totalSegmentSpace = segmentWidth + gap;
        const count = Math.max(
          0,
          Math.floor((containerWidth + gap) / totalSegmentSpace)
        );
        setVisibleCount(count);
      }
    };

    updateCount();
    // Watch the container, not the window: opening or closing the sidebar changes
    // the container's width without a window resize.
    const observer = new ResizeObserver(updateCount);
    if (containerRef.current) observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, [segmentWidth, gap]);

  const trimmedData = data.slice(-visibleCount);
  const paddedData = Array.from({
    length: Math.max(0, visibleCount - trimmedData.length),
  })
    .fill(null)
    .concat(trimmedData) as (HistoryHeartbeat | null)[];

  return (
    <div
      ref={containerRef}
      className="w-full relative"
      style={{ height: `${barHeight}px` }}
    >
      <div
        className="absolute inset-0 flex items-center"
        style={{ height: `${barHeight}px`, gap: `${gap}px` }}
      >
        <TooltipProvider>
          {paddedData.map((value, idx) => {
            const prev = idx > 0 ? paddedData[idx - 1] : null;
            return (
              <Tooltip key={idx}>
                <TooltipTrigger asChild>
                  <div
                    data-testid="heartbeat-bar-segment"
                    className={cn("flex-shrink-0 h-full bg-gray-300", {
                      "bg-green-500": value?.status === 1,
                      "bg-red-500": value?.status === 0 || value?.status === 2,
                      "bg-blue-500": value?.status === 3,
                    })}
                    style={{
                      width: `${segmentWidth}px`,
                      // marginRight: `${idx < paddedData.length - 1 ? gap : 0}px`,
                      borderRadius,
                    }}
                  />
                </TooltipTrigger>

                {value && value.time && tooltip && (
                  <TooltipContent className="max-w-xs">
                    <HeartbeatTooltipBody
                      value={value}
                      prev={prev}
                      timeText={formatDateToTimezone(value.time, timezone)}
                      instance={instanceLabel(value.instance_id, instanceHostnames)}
                      t={(key) => t(key)}
                    />
                  </TooltipContent>
                )}
              </Tooltip>
            );
          })}
        </TooltipProvider>
      </div>
    </div>
  );
};

export default BarHistory;

{
  /* <TooltipProvider>
<Tooltip>
  <TooltipTrigger asChild>
    <Button variant="outline">Hover</Button>
  </TooltipTrigger>
  <TooltipContent>
    <p>Add to library</p>
  </TooltipContent>
</Tooltip>
</TooltipProvider> */
}
