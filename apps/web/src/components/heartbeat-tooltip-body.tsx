import type { HeartbeatModel } from "@/api";

export type HistoryHeartbeat = Pick<HeartbeatModel, "id" | "time" | "end_time" | "ping" | "status"> &
  Partial<HeartbeatModel>;

type HeartbeatTooltipBodyProps = {
  value: HistoryHeartbeat;
  // The heartbeat before this one in the bar, for the interval between them
  prev: HistoryHeartbeat | null;
  // The heartbeat's time, already in the user's timezone
  timeText: string;
  // What names the probe instance that ran the check; null for a heartbeat without one
  instance: string | null;
  t: (key: string) => string;
};

// The grid a heartbeat's bar segment shows on hover. It takes its text and time already resolved,
// so it renders without the i18n and timezone providers.
const HeartbeatTooltipBody = ({ value, prev, timeText, instance, t }: HeartbeatTooltipBodyProps) => (
  <div className="grid grid-cols-3 gap-x-3 gap-y-1">
    <div className="font-semibold">ID:</div>
    <div className="col-span-2">{value.id}</div>

    <div className="font-semibold">{t("bars.time")}:</div>
    <div className="col-span-2">{timeText}</div>

    {instance && (
      <>
        <div className="font-semibold">{t("bars.instance")}:</div>
        <div className="col-span-2 break-words" data-testid="heartbeat-tooltip-instance">
          {instance}
        </div>
      </>
    )}

    <div className="font-semibold">{t("bars.status")}:</div>
    <div className="col-span-2">{value.status}</div>

    <div className="font-semibold">{t("bars.ping")}:</div>
    <div className="col-span-2">{value.ping} ms</div>

    <div className="font-semibold">{t("bars.important")}:</div>
    <div className="col-span-2">{value.important?.toString()}</div>

    <div className="font-semibold">{t("bars.message")}:</div>
    <div className="col-span-2 break-words">{value.msg}</div>

    <div className="font-semibold">{t("bars.retries")}:</div>
    <div className="col-span-2">{value.retries}</div>

    <div className="font-semibold">{t("bars.down_count")}:</div>
    <div className="col-span-2">{value.down_count}</div>

    <div className="font-semibold">{t("bars.notified")}:</div>
    <div className="col-span-2">{value.notified?.toString()}</div>

    {prev && prev.time && (
      <>
        <div className="font-semibold">{t("bars.interval")}:</div>
        <div className="col-span-2">
          {new Date(value.time).getTime() - new Date(prev.time).getTime()} {t("bars.ms")}
        </div>
      </>
    )}
  </div>
);

export default HeartbeatTooltipBody;
