import { describe, expect, it } from "vitest";
import {
  inWorkspace,
  isAdminPath,
  workspaceIdFromPathname,
  withoutWorkspace,
} from "./workspace-path";

const ID = "0b9c2f5e-6c1a-4d5e-9a57-3f1d2c4b8a10";
const OTHER = "7f3e1a90-2b44-4c6d-8e15-a9d0c5b7e321";

describe("workspaceIdFromPathname", () => {
  it("reads the workspace from /w/<uuid>/...", () => {
    expect(workspaceIdFromPathname(`/w/${ID}/monitors`)).toBe(ID);
    expect(workspaceIdFromPathname(`/w/${ID}/monitors/${OTHER}/edit`)).toBe(ID);
  });

  it("reads the workspace from an address with nothing after it", () => {
    expect(workspaceIdFromPathname(`/w/${ID}`)).toBe(ID);
    expect(workspaceIdFromPathname(`/w/${ID}/`)).toBe(ID);
  });

  it("reads an upper-case id the way it was typed", () => {
    expect(workspaceIdFromPathname(`/w/${ID.toUpperCase()}/tags`)).toBe(ID.toUpperCase());
  });

  it("returns null for an address without a workspace", () => {
    expect(workspaceIdFromPathname("/monitors")).toBeNull();
    expect(workspaceIdFromPathname("/")).toBeNull();
    expect(workspaceIdFromPathname("/login")).toBeNull();
    expect(workspaceIdFromPathname(`/status/w/${ID}`)).toBeNull();
    expect(workspaceIdFromPathname(`/monitors/${ID}`)).toBeNull();
  });

  it("returns null when the segment is not a uuid", () => {
    expect(workspaceIdFromPathname("/w/not-a-uuid/monitors")).toBeNull();
    expect(workspaceIdFromPathname(`/w/${ID}x/monitors`)).toBeNull();
    expect(workspaceIdFromPathname("/w/")).toBeNull();
  });
});

describe("inWorkspace", () => {
  it("prefixes a signed-in path with the workspace", () => {
    expect(inWorkspace(ID, "/monitors")).toBe(`/w/${ID}/monitors`);
    expect(inWorkspace(ID, `/monitors/${OTHER}/edit`)).toBe(`/w/${ID}/monitors/${OTHER}/edit`);
  });

  it("keeps a search and a hash", () => {
    expect(inWorkspace(ID, "/settings#error-reports")).toBe(`/w/${ID}/settings#error-reports`);
    expect(inWorkspace(ID, "/monitors?status=down")).toBe(`/w/${ID}/monitors?status=down`);
  });

  it("turns the root into the workspace's own address", () => {
    expect(inWorkspace(ID, "/")).toBe(`/w/${ID}`);
    expect(inWorkspace(ID, "")).toBe(`/w/${ID}`);
  });

  it("accepts a path without a leading slash", () => {
    expect(inWorkspace(ID, "monitors")).toBe(`/w/${ID}/monitors`);
  });
});

describe("withoutWorkspace", () => {
  it("cuts the workspace prefix off an address", () => {
    expect(withoutWorkspace(`/w/${ID}/monitors/${OTHER}`)).toBe(`/monitors/${OTHER}`);
  });

  it("turns a bare workspace address into the root", () => {
    expect(withoutWorkspace(`/w/${ID}`)).toBe("/");
    expect(withoutWorkspace(`/w/${ID}/`)).toBe("/");
  });

  it("cuts any /w/<segment> prefix, so an unknown workspace can be redirected", () => {
    expect(withoutWorkspace("/w/not-a-uuid/settings")).toBe("/settings");
    expect(withoutWorkspace(`/w/${OTHER}/tags/new`)).toBe("/tags/new");
  });

  it("leaves an address without the prefix alone", () => {
    expect(withoutWorkspace("/monitors")).toBe("/monitors");
    expect(withoutWorkspace("/")).toBe("/");
  });
});

describe("isAdminPath", () => {
  it("is true for the admin area and what is in it", () => {
    expect(isAdminPath("/admin")).toBe(true);
    expect(isAdminPath("/admin/")).toBe(true);
    expect(isAdminPath("/admin/probes/p1")).toBe(true);
  });

  it("is false for anything else, a workspace page named admin included", () => {
    expect(isAdminPath("/administrator")).toBe(false);
    expect(isAdminPath(`/w/${ID}/admin`)).toBe(false);
    expect(isAdminPath("/")).toBe(false);
  });
});
