import { describe, expect, it } from "vitest";
import type { DomainStatusPageStatus } from "@/api/types.gen";
import {
  domainStateLabel,
  findDomainStatus,
  needsRecords,
  PENDING_REFETCH_MS,
  refetchIntervalFor,
  withCheckedDomain,
} from "./domain-state";

const status = (
  domain: string,
  over: Partial<DomainStatusPageStatus> = {}
): DomainStatusPageStatus => ({
  domain,
  state: "pending",
  verifiedAt: null,
  lastCheckedAt: null,
  failingSince: null,
  removeAt: null,
  records: {
    txt: { name: `_peekaping-verify.${domain}`, value: "peekaping-verify=abc" },
    cname: null,
  },
  ...over,
});

describe("domainStateLabel", () => {
  it("says pending for a pending domain", () => {
    expect(domainStateLabel(status("a.example.com"))).toBe("pending");
  });

  it("says verified for a verified domain", () => {
    expect(domainStateLabel(status("a.example.com", { state: "verified" }))).toBe("verified");
  });

  it("says records missing for a verified domain that is failing", () => {
    expect(
      domainStateLabel(
        status("a.example.com", { state: "verified", failingSince: "2026-10-28T10:00:00Z" })
      )
    ).toBe("records_missing");
  });

  it("says pending for a pending domain even when failingSince is set", () => {
    expect(
      domainStateLabel(status("a.example.com", { failingSince: "2026-10-28T10:00:00Z" }))
    ).toBe("pending");
  });
});

describe("needsRecords", () => {
  it("is true for pending and records-missing domains only", () => {
    expect(needsRecords(status("a.example.com"))).toBe(true);
    expect(needsRecords(status("a.example.com", { state: "verified" }))).toBe(false);
    expect(
      needsRecords(status("a.example.com", { state: "verified", failingSince: "2026-10-28T10:00:00Z" }))
    ).toBe(true);
  });
});

describe("findDomainStatus", () => {
  it("finds a domain by name and misses an unsaved one", () => {
    const list = [status("a.example.com"), status("b.example.com")];
    expect(findDomainStatus(list, "b.example.com")?.domain).toBe("b.example.com");
    expect(findDomainStatus(list, "c.example.com")).toBeUndefined();
    expect(findDomainStatus(undefined, "a.example.com")).toBeUndefined();
  });
});

describe("withCheckedDomain", () => {
  it("replaces only the checked domain", () => {
    const list = { verificationRequired: true, domains: [status("a.example.com"), status("b.example.com")] };
    const checked = status("b.example.com", { state: "verified" });
    const next = withCheckedDomain(list, checked);
    expect(next.domains[0].state).toBe("pending");
    expect(next.domains[1].state).toBe("verified");
    expect(next.verificationRequired).toBe(true);
  });
});

describe("refetchIntervalFor", () => {
  it("polls every 60 seconds while a domain is pending", () => {
    expect(PENDING_REFETCH_MS).toBe(60_000);
    const domains = [status("a.example.com", { state: "verified" }), status("b.example.com")];
    expect(refetchIntervalFor(domains)).toBe(60_000);
  });

  it("stops when every domain is verified, or there are none", () => {
    expect(refetchIntervalFor([status("a.example.com", { state: "verified" })])).toBe(false);
    expect(refetchIntervalFor([])).toBe(false);
  });

  it("does not poll before the list has arrived", () => {
    expect(refetchIntervalFor(undefined)).toBe(false);
  });
});
