// The signed-in app lives under /w/<workspaceId>/ (docs/adr/0005, section 7).
// The address is the one place that says which workspace a page and its
// requests belong to, so nothing here keeps a copy of it.

const WORKSPACE_ID = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";
const WORKSPACE_ADDRESS = new RegExp(`^/w/(${WORKSPACE_ID})(?:/|$)`, "i");
const ANY_WORKSPACE_PREFIX = /^\/w\/[^/]+(?=\/|$)/;

/** The workspace an address names, or null when it names none. */
export const workspaceIdFromPathname = (pathname: string): string | null =>
  WORKSPACE_ADDRESS.exec(pathname)?.[1] ?? null;

/** The workspace the browser is on right now, for code that runs outside a component. */
export const currentWorkspaceId = (): string | null =>
  workspaceIdFromPathname(window.location.pathname);

/** A signed-in page's path (`/monitors`, `/settings#error-reports`) in a workspace. */
export const inWorkspace = (workspaceId: string, path: string): string => {
  const rest = path === "" || path === "/" ? "" : path.startsWith("/") ? path : `/${path}`;
  return `/w/${workspaceId}${rest}`;
};

/** An address without its `/w/<segment>` prefix: the same page, workspace not yet chosen. */
export const withoutWorkspace = (pathname: string): string => {
  const rest = pathname.replace(ANY_WORKSPACE_PREFIX, "");
  return rest === "" ? "/" : rest;
};

/** `inWorkspace` for the workspace in the address, for code outside components. */
export const workspacePath = (path: string): string => {
  const id = currentWorkspaceId();
  return id ? inWorkspace(id, path) : path;
};
