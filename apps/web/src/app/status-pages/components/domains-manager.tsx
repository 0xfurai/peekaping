import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { PlusIcon, XIcon, AlertTriangleIcon } from "lucide-react";
import { useVerifyCustomDomains } from "@/hooks/useVerifyCustomDomains";
import type { DomainStatusPageStatus } from "@/api/types.gen";
import DomainStatus from "./domain-status";
import { findDomainStatus } from "./domain-state";
import { useDomainStatuses } from "./use-domain-statuses";

// What stands in the place of the records while the saved domains load or can't be read
const RecordsLoadState = ({
  isError,
  onRetry,
}: {
  isError: boolean;
  onRetry: () => void;
}) => {
  const { t } = useLocalizedTranslation();
  if (!isError) {
    return <p className="text-xs text-muted-foreground">{t("common.loading")}</p>;
  }
  return (
    <div className="flex items-center gap-2 text-xs text-destructive">
      <span>{t("status_pages.domain_verification.records_load_failed")}</span>
      <Button type="button" variant="outline" size="sm" onClick={onRetry}>
        {t("common.retry")}
      </Button>
    </div>
  );
};

type StatusesLoad = {
  isLoading: boolean;
  isError: boolean;
  refetch: () => unknown;
};

// The verification of one domain of the list: its state when the page is saved with it, and
// the way to get there when it isn't. While the saved domains load, or when they can't be
// read, a line says so (with a Retry for the failure).
const DomainVerification = ({
  pageId,
  status,
  known,
  load,
}: {
  pageId?: string;
  status?: DomainStatusPageStatus;
  known: boolean;
  load: StatusesLoad;
}) => {
  const { t } = useLocalizedTranslation();
  if (pageId && status) return <DomainStatus pageId={pageId} status={status} />;
  if (pageId && !known) {
    if (!load.isError && !load.isLoading) return null;
    return <RecordsLoadState isError={load.isError} onRetry={() => void load.refetch()} />;
  }
  return (
    <p className="text-xs text-muted-foreground">
      {t("status_pages.domain_verification.save_to_see_records")}
    </p>
  );
};

const DomainsManager = ({
  value = [],
  onChange,
  error,
  pageId,
}: {
  // The saved status page whose domains these are; left out when it is being created
  pageId?: string;
  value?: string[];
  onChange: (domains: string[]) => void;
  error?: {
    message?: string;
    domain?: string;
  };
}) => {
  const { t } = useLocalizedTranslation();
  const [newDomain, setNewDomain] = useState("");
  const currentHost = window.location.hostname;
  const verify = useVerifyCustomDomains();
  const { domains: saved, ...load } = useDomainStatuses(pageId, verify);

  const addDomain = () => {
    if (newDomain.trim() && !value.includes(newDomain.trim())) {
      onChange([...value, newDomain.trim()]);
      setNewDomain("");
    }
  };

  const removeDomain = (index: number) => {
    const updated = value.filter((_, i) => i !== index);
    onChange(updated);
  };

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault();
      addDomain();
    }
  };

  return (
    <div className="space-y-3">
      {value.length > 0 && (
        <div className="space-y-2">
          <p className="text-sm font-medium">{t("forms.placeholders.domains")}</p>
          <div className="space-y-2">
            {value.map((domain, index) => {
              const isCurrentHost = domain === currentHost;
              const isHighlighted = error?.domain === domain;
              return (
                <div key={index} className="space-y-1">
                  <div
                    className={
                      `flex items-center justify-between p-2 rounded-md ` +
                      (isHighlighted
                        ? "bg-destructive/10 border border-destructive"
                        : "bg-muted")
                    }
                  >
                    <span className={`text-sm ${isHighlighted ? "text-destructive" : ""}`}>{domain}</span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => removeDomain(index)}
                      className="h-6 w-6 p-0"
                    >
                      <XIcon className="h-3 w-3" />
                    </Button>
                  </div>
                  {verify && (
                    <DomainVerification
                      pageId={pageId}
                      status={findDomainStatus(saved, domain)}
                      known={saved !== undefined}
                      load={load}
                    />
                  )}
                  {isHighlighted && (
                    <Alert variant="destructive" className="mt-1">
                      <AlertTriangleIcon className="h-4 w-4" />
                      <AlertDescription>
                        {error?.message}
                      </AlertDescription>
                    </Alert>
                  )}
                  {isCurrentHost && (
                    <Alert variant="destructive" className="mt-1">
                      <AlertTriangleIcon className="h-4 w-4" />
                      <AlertDescription>
                        {t("status_pages.domain_host_warning")}
                      </AlertDescription>
                    </Alert>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}

      <div className="flex gap-2">
        <Input
          placeholder={t("forms.placeholders.domains")}
          value={newDomain}
          onChange={(e) => setNewDomain(e.target.value)}
          onKeyDown={handleKeyPress}
          className="flex-1"
        />
        <Button
          type="button"
          onClick={addDomain}
          disabled={!newDomain.trim() || value.includes(newDomain.trim())}
        >
          <PlusIcon className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
};

export default DomainsManager;