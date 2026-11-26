import type {
  DomainStatusPageStatus,
  StatusPageDomainStatusesDto,
} from "@/api/types.gen";

export type DomainStateLabel = "verified" | "pending" | "records_missing";

// What the badge of a domain says. A verified domain whose records are missing is still served,
// until it goes back to pending 24 hours after the records went missing.
export const domainStateLabel = (
  status: Pick<DomainStatusPageStatus, "state" | "failingSince">
): DomainStateLabel => {
  if (status.state === "pending") return "pending";
  return status.failingSince ? "records_missing" : "verified";
};

// A pending domain's records are what its owner still has to add. A verified domain whose
// records are missing needs them again.
export const needsRecords = (status: DomainStatusPageStatus): boolean =>
  domainStateLabel(status) !== "verified";

export const findDomainStatus = (
  domains: DomainStatusPageStatus[] | undefined,
  domain: string
): DomainStatusPageStatus | undefined =>
  domains?.find((status) => status.domain === domain);

// The list of a page's domains with one domain's state replaced by the answer of a check
export const withCheckedDomain = (
  list: StatusPageDomainStatusesDto,
  checked: DomainStatusPageStatus
): StatusPageDomainStatusesDto => ({
  ...list,
  domains: list.domains.map((status) =>
    status.domain === checked.domain ? checked : status
  ),
});

// How often the list is asked for again while a domain is pending, so that its state changes
// after the background check without a reload
export const PENDING_REFETCH_MS = 60_000;

export const refetchIntervalFor = (
  domains: DomainStatusPageStatus[] | undefined
): number | false =>
  domains?.some((status) => status.state === "pending") ? PENDING_REFETCH_MS : false;
