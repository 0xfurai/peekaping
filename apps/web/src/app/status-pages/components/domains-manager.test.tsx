import { beforeAll, describe, expect, it, vi } from "vitest";
import { renderAs, text } from "@/test-support/render-with-me";
import DomainsManager from "./domains-manager";

const page = (verifyCustomDomains: boolean, pageId?: string) =>
  renderAs(
    { instanceAdmin: false, role: "admin", verifyCustomDomains },
    <DomainsManager value={["status.example.com"]} onChange={() => {}} pageId={pageId} />
  );

const SAVE_NOTE = text("status_pages.domain_verification.save_to_see_records");

describe("DomainsManager", () => {
  beforeAll(() => {
    vi.stubGlobal("window", { location: { hostname: "peekaping.example.com" } });
  });

  it("shows no verification when the install does not verify domains", () => {
    expect(page(false)).not.toContain(SAVE_NOTE);
    expect(page(false, "page-1")).not.toContain(SAVE_NOTE);
  });

  it("asks to save the page of a domain that is not saved yet when verification is on", () => {
    expect(page(true)).toContain(SAVE_NOTE);
  });

  it("shows nothing for a saved page while its domains load", () => {
    expect(page(true, "page-1")).not.toContain(SAVE_NOTE);
  });
});
