import { describe, expect, it } from "vitest";
import { deletionNotice, formatDeletionDate } from "./workspace-deletion";

const AT = "2026-11-05T10:30:00Z";

describe("deletionNotice", () => {
  it("tells every member the date, and lets only an owner cancel", () => {
    expect(deletionNotice({ role: "owner", deletionScheduledAt: AT })).toEqual({ scheduledAt: AT, canCancel: true });
    expect(deletionNotice({ role: "admin", deletionScheduledAt: AT })).toEqual({ scheduledAt: AT, canCancel: false });
    expect(deletionNotice({ role: "member", deletionScheduledAt: AT })).toEqual({ scheduledAt: AT, canCancel: false });
  });

  it("is nothing for a workspace with no deletion scheduled, or none at all", () => {
    expect(deletionNotice({ role: "owner", deletionScheduledAt: null })).toBeNull();
    expect(deletionNotice({ role: "owner" })).toBeNull();
    expect(deletionNotice(undefined)).toBeNull();
  });
});

describe("formatDeletionDate", () => {
  it("words the date and the time in the language and the time zone given", () => {
    expect(formatDeletionDate(AT, "en-US", "UTC")).toBe("November 5, 2026 at 10:30 AM");
    expect(formatDeletionDate(AT, "de-DE", "Europe/Berlin")).toBe("5. November 2026 um 11:30");
  });

  it("falls back to English for a language the browser does not know", () => {
    expect(formatDeletionDate(AT, "not a locale", "UTC")).toBe("November 5, 2026 at 10:30 AM");
  });
});
