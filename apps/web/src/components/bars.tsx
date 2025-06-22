import type { HeartbeatModel } from "@/api";
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

type BarHistoryProps = {
  data: HeartbeatModel[];
  segmentWidth?: number;
  gap?: number;
  barHeight?: number;
  borderRadius?: number;
  tooltip?: boolean;
};

const BarHistory: React.FC<BarHistoryProps> = ({
  data,
  segmentWidth = 8,
  gap = 3,
  barHeight = 24,
  borderRadius = 3,
  tooltip = true,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [visibleCount, setVisibleCount] = useState(0);
  const { timezone } = useTimezone();

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
    window.addEventListener("resize", updateCount);
    return () => window.removeEventListener("resize", updateCount);
  }, [segmentWidth, gap]);

  const trimmedData = data.slice(-visibleCount);
  const paddedData = Array.from({
    length: Math.max(0, visibleCount - trimmedData.length),
  })
    .fill(null)
    .concat(trimmedData) as (HeartbeatModel | null)[];

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
                  <TooltipContent>
                    <p>ID: {value.id}</p>
                    <p>{formatDateToTimezone(value.time, timezone)}</p>
                    <p>Status: {value.status}</p>
                    <p>Ping: {value.ping} ms</p>
                    <p>Important: {value.important?.toString()}</p>
                    <p>Message: {value.msg}</p>
                    <p>Retries: {value.retries}</p>
                    <p>Down count: {value.down_count}</p>
                    <p>Notified: {value.notified?.toString()}</p>
                    {prev && prev?.time && (
                      <p>
                        Interval:{" "}
                        {new Date(value.time!).getTime() -
                          new Date(prev.time!).getTime()}{" "}
                        ms
                      </p>
                    )}
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
