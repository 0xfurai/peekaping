import { CopyIcon } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import type { DomainStatusPageStatus } from "@/api/types.gen";

const CopyableValue = ({ label, value }: { label: string; value: string }) => {
  const { t } = useLocalizedTranslation();

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(t("status_pages.domain_verification.copy_success"));
    } catch {
      // navigator.clipboard is missing on a page served over plain http.
      toast.error(t("status_pages.domain_verification.copy_failed"));
    }
  };

  return (
    <div className="flex items-center justify-between gap-2">
      <div className="min-w-0">
        <span className="text-xs text-muted-foreground">{label}</span>
        <code className="block break-all text-xs">{value}</code>
      </div>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-6 w-6 shrink-0 p-0"
        onClick={copy}
        aria-label={t("status_pages.domain_verification.copy", { label })}
      >
        <CopyIcon className="h-3 w-3" />
      </Button>
    </div>
  );
};

const Record = ({ title, children }: { title: string; children: React.ReactNode }) => (
  <div className="space-y-1 rounded-md border bg-background p-2">
    <p className="text-xs font-medium">{title}</p>
    {children}
  </div>
);

// The DNS records that verify a domain: a TXT record or, when the install has a host to point
// at, a CNAME. Either one is enough.
const DomainRecords = ({ records }: { records: DomainStatusPageStatus["records"] }) => {
  const { t } = useLocalizedTranslation();
  const label = (key: string) => t(`status_pages.domain_verification.${key}`);

  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">{label("add_one_record")}</p>
      <Record title={label("txt_record")}>
        <CopyableValue label={label("name")} value={records.txt.name} />
        <CopyableValue label={label("value")} value={records.txt.value} />
      </Record>
      {records.cname && (
        <Record title={label("cname_record")}>
          <CopyableValue label={label("name")} value={records.cname.name} />
          <CopyableValue label={label("target")} value={records.cname.target} />
        </Record>
      )}
    </div>
  );
};

export default DomainRecords;
