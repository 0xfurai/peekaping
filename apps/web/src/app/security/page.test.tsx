import { describe, expect, it, vi } from "vitest";
import { renderAs, text } from "@/test-support/render-with-me";
import SecurityPage from "./page";

vi.mock("@/context/timezone-context", () => ({
  useTimezone: () => ({ timezone: "UTC", setTimezone: () => {} }),
}));

const security = (role: "member" | "admin" | "owner") =>
  renderAs({ instanceAdmin: false, role }, <SecurityPage />);

describe("SecurityPage", () => {
  it("hides the API keys from a member, whose requests for them the server refuses", () => {
    const html = security("member");
    expect(html).not.toContain(text("security.api_keys.title"));
    expect(html).not.toContain(text("security.api_keys.create_button"));
  });

  it.each(["admin", "owner"] as const)("shows the API keys to an %s", (role) => {
    expect(security(role)).toContain(text("security.api_keys.title"));
  });
});
