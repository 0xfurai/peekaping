import type { ProbeProbeResponse } from "@/api/types.gen";
import { Badge } from "@/components/ui/badge";
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

// What a cell shows when it has no value, such as the token prefix of the local probe.
const NO_VALUE = "—";

interface ProbesTableProps {
  probes: ProbeProbeResponse[];
}

const ProbesTable = ({ probes }: ProbesTableProps) => {
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
        </TableRow>
      </TableHeader>
      <TableBody>
        {probes.map((probe) => (
          <TableRow key={probe.id} data-testid={`probe-row-${probe.id}`}>
            <TableCell className="font-medium">{probe.name}</TableCell>
            <TableCell className="text-muted-foreground">
              {probe.location || NO_VALUE}
            </TableCell>
            <TableCell>
              {probe.status === "online" ? (
                <Badge className="border-transparent bg-green-600 text-white">
                  {t("probes.table.status.online")}
                </Badge>
              ) : (
                <Badge variant="secondary">
                  {t("probes.table.status.offline")}
                </Badge>
              )}
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
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
};

export default ProbesTable;
