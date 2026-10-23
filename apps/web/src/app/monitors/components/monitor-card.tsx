import type { MonitorMonitorResponseDto } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { useNavigate } from "react-router-dom";
import { Pause } from "lucide-react";
import BarHistory from "@/components/bars";
import { useQuery } from "@tanstack/react-query";
import { getMonitorsByIdHeartbeatsOptions } from "@/api/@tanstack/react-query.gen";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { cn } from "@/lib/utils";
import StaleMarker from "./stale-marker";
import { getMonitorStatusView } from "./monitor-status";

const MonitorCard = ({ monitor }: { monitor: MonitorMonitorResponseDto }) => {
  const navigate = useNavigate();
  const { t } = useLocalizedTranslation();

  const { data: heartbeats } = useQuery({
    ...getMonitorsByIdHeartbeatsOptions({
      path: {
        id: monitor.id!,
      },
      query: {
        limit: 50,
        reverse: true,
      },
    }),
    enabled: !!monitor.id,
  });

  return (
    <Card
      key={monitor.id}
      className="mb-2 p-2 hover:cursor-pointer light:hover:bg-gray-100 dark:hover:bg-zinc-800"
      onClick={() => navigate(`/monitors/${monitor.id}`)}
      data-testid="monitor-card"
    >
      <CardContent className="px-2">
        <div className="flex justify-between flex-col md:flex-row">
          <div className="flex items-center mb-4 md:mb-0">
            <div className="text-sm text-gray-500 mr-4 min-w-[60px]">
              {(() => {
                if (monitor.active === false) {
                  return (
                    <span className="flex items-center text-yellow-500 font-semibold">
                      <Pause className="w-5 h-5 mr-1" />{" "}
                      {t("monitors.view.status.paused")}
                    </span>
                  );
                }
                const lastHeartbeat =
                  heartbeats?.data?.[heartbeats.data.length - 1];
                const statusView = getMonitorStatusView(lastHeartbeat?.status);
                if (!statusView) return null;
                const StatusIcon = statusView.icon;
                return (
                  <span
                    className={cn(
                      "flex items-center font-semibold",
                      statusView.cardColor
                    )}
                  >
                    <StatusIcon className="w-5 h-5 mr-1" />{" "}
                    {statusView.name(t)}
                  </span>
                );
              })()}
            </div>

            <div className="flex flex-col min-w-[100px]">
              <h3 className="font-bold mb-1">{monitor.name}</h3>
              <Badge variant={"outline"}>{monitor.type}</Badge>
            </div>
          </div>

          <div className="flex flex-col w-full md:w-[400px] justify-center mb-2 md:mb-0">
            <div className="text-sm text-gray-500 mb-1">
              {t("monitors.card.check_interval")} {monitor.interval}s
            </div>
            {monitor.active !== false && (
              <StaleMarker
                className="mb-1"
                staleSince={monitor.stale_since}
                lastHeartbeatTime={
                  heartbeats?.data?.[heartbeats.data.length - 1]?.time
                }
              />
            )}

            <BarHistory
              segmentWidth={6}
              gap={2}
              barHeight={16}
              borderRadius={2}
              data={heartbeats?.data || []}
              tooltip={false}
            />
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default MonitorCard;
