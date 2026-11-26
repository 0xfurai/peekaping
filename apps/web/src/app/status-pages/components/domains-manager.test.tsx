import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { getStatusPagesDomainsByIdOptions } from "@/api/@tanstack/react-query.gen";
import { renderAs, text } from "@/test-support/render-with-me";
import DomainsManager from "./domains-manager";

const { asked, clicks } = vi.hoisted(() => ({
  asked: vi.fn(),
  clicks: new Map<string, () => void>(),
}));

// The request for the domain statuses, answered by a spy so that a retry can be seen
vi.mock("@/api/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/sdk.gen")>()),
  getStatusPagesDomainsById: asked,
}));

// Static markup has no clicks: the buttons hand their handlers to the test by their text
vi.mock("@/components/ui/button", () => ({
  Button: ({ children, onClick }: { children?: unknown; onClick?: () => void }) => {
    if (typeof children === "string" && onClick) clicks.set(children, onClick);
    return <button type="button">{children as never}</button>;
  },
}));

const page = (verifyCustomDomains: boolean, pageId?: string, seed?: (client: QueryClient) => void) =>
  renderAs(
    { instanceAdmin: false, role: "admin", verifyCustomDomains },
    <DomainsManager value={["status.example.com"]} onChange={() => {}} pageId={pageId} />,
    undefined,
    seed
  );

// The statuses of "page-1" asked for and refused, as the query leaves them after a failed request.
// A mounting query would ask again at once, so it is told to leave the failure as it is.
const failedRequest = (client: QueryClient) => {
  const { queryKey } = getStatusPagesDomainsByIdOptions({ path: { id: "page-1" } });
  client.setQueryDefaults(queryKey, { retryOnMount: false });
  client
    .getQueryCache()
    .build(client, { queryKey })
    .setState({ status: "error", error: new Error("boom"), fetchStatus: "idle" });
};

const SAVE_NOTE = text("status_pages.domain_verification.save_to_see_records");
const LOAD_FAILED = text("status_pages.domain_verification.records_load_failed");

describe("DomainsManager", () => {
  beforeAll(() => {
    vi.stubGlobal("window", { location: { hostname: "peekaping.example.com" } });
  });

  beforeEach(() => {
    asked.mockReset();
    asked.mockReturnValue(new Promise(() => {}));
    clicks.clear();
  });

  it("shows no verification when the install does not verify domains", () => {
    expect(page(false)).not.toContain(SAVE_NOTE);
    expect(page(false, "page-1")).not.toContain(SAVE_NOTE);
    expect(page(false, "page-1", failedRequest)).not.toContain(LOAD_FAILED);
  });

  it("asks to save the page of a domain that is not saved yet when verification is on", () => {
    expect(page(true)).toContain(SAVE_NOTE);
  });

  it("shows a loading line for a saved page while its domains load", () => {
    const html = page(true, "page-1");
    expect(html).toContain(text("common.loading"));
    expect(html).not.toContain(SAVE_NOTE);
    expect(html).not.toContain(LOAD_FAILED);
  });

  it("says the DNS records could not be loaded, with a Retry that asks again", () => {
    const html = page(true, "page-1", failedRequest);
    expect(html).toContain(LOAD_FAILED);
    expect(html).not.toContain(text("common.loading"));
    expect(html).not.toContain(SAVE_NOTE);

    const retry = clicks.get(text("common.retry"));
    expect(retry).toBeDefined();
    expect(asked).not.toHaveBeenCalled();
    retry?.();
    expect(asked).toHaveBeenCalledTimes(1);
  });
});
