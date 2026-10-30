import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useTimezone } from "@/context/timezone-context";
import { formatDateToTimezone } from "@/lib/formatDateToTimezone";
import type { DomainStatusPageStatus } from "@/api/types.gen";
import DomainRecords from "./domain-records";
import { domainStateLabel, needsRecords, type DomainStateLabel } from "./domain-state";
import { useCheckDomain } from "./use-domain-statuses";

const BADGE_VARIANT: Record<DomainStateLabel, "default" | "secondary" | "destructive"> = {
  verified: "default",
  pending: "secondary",
  records_missing: "destructive",
};

// The visitor's own time zone, as everywhere else in the app
const useFormatTime = () => {
  const { timezone } = useTimezone();
  return (value: string) => formatDateToTimezone(value, timezone);
};

const Notes = ({ status }: { status: DomainStatusPageStatus }) => {
  const { t } = useLocalizedTranslation();
  const format = useFormatTime();
  const text = (key: string, values?: Record<string, string>) =>
    t(`status_pages.domain_verification.${key}`, values);

  return (
    <>
      {status.state === "pending" && <p className="text-xs">{text("pending_note")}</p>}
      {status.state === "pending" && status.removeAt && (
        <p className="text-xs font-medium">
          {text("removed_on", { date: format(status.removeAt) })}
        </p>
      )}
      {status.state === "verified" && status.failingSince && (
        <p className="text-xs">{text("records_missing_note", { time: format(status.failingSince) })}</p>
      )}
    </>
  );
};

// One saved domain's verification: its state, when it was last checked, the records to add
// while it needs them, and a button that checks its DNS now
const DomainStatus = ({ pageId, status }: { pageId: string; status: DomainStatusPageStatus }) => {
  const { t } = useLocalizedTranslation();
  const format = useFormatTime();
  const label = domainStateLabel(status);
  const check = useCheckDomain(pageId, () =>
    toast.error(t("status_pages.domain_verification.check_failed", { domain: status.domain }))
  );

  return (
    <div className="space-y-2 rounded-md border p-2">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={BADGE_VARIANT[label]}>
          {t(`status_pages.domain_verification.state.${label}`)}
        </Badge>
        <span className="text-xs text-muted-foreground">
          {status.lastCheckedAt
            ? t("status_pages.domain_verification.last_checked", { time: format(status.lastCheckedAt) })
            : t("status_pages.domain_verification.not_checked")}
        </span>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="ml-auto h-7"
          disabled={check.isPending}
          onClick={() => check.mutate({ path: { id: pageId, domain: status.domain } })}
        >
          {check.isPending
            ? t("status_pages.domain_verification.checking")
            : t("status_pages.domain_verification.check_now")}
        </Button>
      </div>
      <Notes status={status} />
      {needsRecords(status) && <DomainRecords records={status.records} />}
    </div>
  );
};

export default DomainStatus;
