import { Pencil, RefreshCw, Trash2 } from "lucide-react";
import { Link } from "react-router-dom";
import type {
  ProbeProbeAvailabilityResponse,
  ProbeProbeResponse,
} from "@/api/types.gen";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useTimezone } from "@/context/timezone-context";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import { AvailabilityBar } from "./availability-bar";
import { formatUptime } from "./availability-layout";
import { LOCAL_PROBE_ID } from "./local-probe";
import ProbeStatusBadge from "./probe-status-badge";

// What a cell shows when it has no value, such as the token prefix of the local probe.
const NO_VALUE = "—";

// The probe's status, and under it the last 24 hours as a bar with the uptime. In single mode
// Peekaping checks the local probe's monitors itself, so that probe has no history to draw.
const ProbeStatusCell = ({
  probe,
  availability,
}: {
  probe: ProbeProbeResponse;
  availability: ProbeProbeAvailabilityResponse | undefined;
}) => (
  <div className="flex flex-col items-start gap-1.5">
    <div className="flex items-center gap-2">
      <ProbeStatusBadge status={probe.status} />
      {availability && !availability.in_process && (
        <span
          className="text-xs tabular-nums text-muted-foreground"
          data-testid="probe-uptime"
        >
          {formatUptime(availability.uptime_percent)}
        </span>
      )}
    </div>
    {availability && !availability.in_process && (
      <AvailabilityBar availability={availability} size="compact" />
    )}
  </div>
);

interface ProbesTableProps {
  probes: ProbeProbeResponse[];
  // The last 24 hours of every probe, by probe id. A probe it doesn't have shows no bar.
  availability: Map<string, ProbeProbeAvailabilityResponse>;
  onEdit: (probe: ProbeProbeResponse) => void;
  onRotate: (probe: ProbeProbeResponse) => void;
  onDelete: (probe: ProbeProbeResponse) => void;
}

const ProbesTable = ({
  probes,
  availability,
  onEdit,
  onRotate,
  onDelete,
}: ProbesTableProps) => {
  const { t } = useLocalizedTranslation();
  const { timezone } = useTimezone();

  const formatLastSeen = (lastSeenAt: string) =>
    formatDateToTimezone(lastSeenAt, timezone, {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("probes.table.headers.name")}</TableHead>
          <TableHead>{t("probes.table.headers.location")}</TableHead>
          <TableHead>{t("probes.table.headers.status")}</TableHead>
          <TableHead>{t("probes.table.headers.disabled")}</TableHead>
          <TableHead>{t("probes.table.headers.token")}</TableHead>
          <TableHead>{t("probes.table.headers.instances")}</TableHead>
          <TableHead>{t("probes.table.headers.actions")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {probes.map((probe) => (
          <TableRow key={probe.id} data-testid={`probe-row-${probe.id}`}>
            <TableCell className="font-medium">
              <Link
                to={`/probes/${probe.id}`}
                className="hover:underline"
                data-testid="probe-link"
              >
                {probe.name}
              </Link>
            </TableCell>
            <TableCell className="text-muted-foreground">
              {probe.location || NO_VALUE}
            </TableCell>
            <TableCell>
              <ProbeStatusCell
                probe={probe}
                availability={availability.get(probe.id)}
              />
            </TableCell>
            <TableCell>
              {probe.disabled ? (
                <Badge variant="destructive">
                  {t("probes.table.disabled")}
                </Badge>
              ) : (
                <span className="text-muted-foreground">{NO_VALUE}</span>
              )}
            </TableCell>
            <TableCell>
              {probe.token_prefix ? (
                <code className="bg-muted px-2 py-1 rounded text-sm font-mono">
                  {probe.token_prefix}
                </code>
              ) : (
                <span className="text-muted-foreground">{NO_VALUE}</span>
              )}
            </TableCell>
            <TableCell>
              {probe.instances.length === 0 ? (
                <span className="text-sm text-muted-foreground">
                  {t("probes.table.no_instances")}
                </span>
              ) : (
                <div className="flex flex-col gap-1">
                  <span className="font-medium">{probe.instances.length}</span>
                  <ul className="flex flex-col gap-1 text-sm">
                    {probe.instances.map((instance) => (
                      <li
                        key={instance.id}
                        className="flex flex-wrap items-baseline gap-x-2"
                      >
                        <span className="font-mono">{instance.hostname}</span>
                        <span className="text-muted-foreground">
                          {instance.version}
                        </span>
                        <span className="text-muted-foreground">
                          {t("probes.table.last_seen", {
                            date: formatLastSeen(instance.last_seen_at),
                          })}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </TableCell>
            <TableCell>
              <div className="flex gap-1">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => onEdit(probe)}
                  aria-label={t("probes.table.actions.edit", {
                    name: probe.name,
                  })}
                  title={t("probes.table.actions.edit", { name: probe.name })}
                >
                  <Pencil className="h-4 w-4" />
                </Button>
                {probe.id !== LOCAL_PROBE_ID && (
                  <>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => onRotate(probe)}
                      aria-label={t("probes.table.actions.rotate", {
                        name: probe.name,
                      })}
                      title={t("probes.table.actions.rotate", {
                        name: probe.name,
                      })}
                    >
                      <RefreshCw className="h-4 w-4" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => onDelete(probe)}
                      aria-label={t("probes.table.actions.delete", {
                        name: probe.name,
                      })}
                      title={t("probes.table.actions.delete", {
                        name: probe.name,
                      })}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </>
                )}
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
};

export default ProbesTable;
