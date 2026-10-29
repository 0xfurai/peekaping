import { describe, expect, it } from "vitest";
import { inviteNextPath, safeNextPath } from "./next-path";

describe("safeNextPath", () => {
  it.each([
    ["/invite/tok_abc-123", "/invite/tok_abc-123"],
    ["/invite/a.b~c", "/invite/a.b~c"],
    // Anything that isn't an invite page of this app goes to the app's start
    ["https://evil.example/invite/x", undefined],
    ["//evil.example/invite/x", undefined],
    ["/invite/x/../../y", undefined],
    ["/invite/", undefined],
    ["/invite/x?y=1", undefined],
    ["/monitors", undefined],
    ["javascript:alert(1)", undefined],
    ["", undefined],
    [null, undefined],
  ])("%j gives %j", (next, expected) => {
    expect(safeNextPath(next)).toBe(expected);
  });
});

describe("inviteNextPath", () => {
  it("is the login address that returns to the invite", () => {
    expect(inviteNextPath("tok_abc")).toBe("/login?next=%2Finvite%2Ftok_abc");
  });
});
