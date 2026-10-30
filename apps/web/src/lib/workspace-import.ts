import { isAxiosError } from "axios";
import type { WorkspaceportChange, WorkspaceportCreated, WorkspaceportDocument, WorkspaceportProblem } from "@/api/types.gen";

// What the workspace import screen shows (PP-170): the file read in the browser, the answer's
// counts and changes in plain language, and the problems of a file the server refused.

type Translate = (key: string, vars?: Record<string, unknown>) => string;

/** The most the server takes in an import body (16 MiB) */
export const MAX_IMPORT_BYTES = 16 * 1024 * 1024;

export type ReadImportFile =
  | { ok: true; document: WorkspaceportDocument }
  | { ok: false; reason: "too_large" | "not_json" };

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const parseObject = (text: string): Record<string, unknown> | undefined => {
  try {
    const parsed: unknown = JSON.parse(text);
    return isObject(parsed) ? parsed : undefined;
  } catch {
    return undefined;
  }
};

/**
 * Reads the file the owner chose: a JSON object, small enough for the server. Whether it is a
 * workspace export is the server's to say, with every problem together.
 */
export const readImportFile = async (file: Pick<File, "size" | "text">): Promise<ReadImportFile> => {
  if (file.size > MAX_IMPORT_BYTES) return { ok: false, reason: "too_large" };
  const document = parseObject(await file.text());
  return document ? { ok: true, document } : { ok: false, reason: "not_json" };
};

const PREFIX = "settings.workspace_import.changes";

// The sentence of a change: by its kind and reason together where the reason changes the
// sentence, else by its kind
const CHANGE_KEYS: Record<string, string> = {
  tag_reused: `${PREFIX}.tag_reused`,
  proxy_skipped_target_not_allowed: `${PREFIX}.proxy_skipped_target_not_allowed`,
  monitor_proxy_dropped: `${PREFIX}.monitor_proxy_dropped`,
  channel_skipped: `${PREFIX}.channel_skipped`,
  "channel_skipped:target_not_allowed": `${PREFIX}.channel_skipped_target_not_allowed`,
  "channel_skipped:no_install_sender": `${PREFIX}.channel_skipped_no_install_sender`,
  "channel_skipped:recipient_not_member": `${PREFIX}.channel_skipped_recipient_not_member`,
  monitor_skipped_type_disabled: `${PREFIX}.monitor_skipped_type_disabled`,
  monitor_skipped_target_not_allowed: `${PREFIX}.monitor_skipped_target_not_allowed`,
  monitor_interval_raised: `${PREFIX}.monitor_interval_raised`,
  "monitor_interval_raised:retryInterval": `${PREFIX}.monitor_retry_interval_raised`,
  push_token_replaced: `${PREFIX}.push_token_replaced`,
  monitor_paused_over_limit: `${PREFIX}.monitor_paused_over_limit`,
  status_page_slug_changed: `${PREFIX}.status_page_slug_changed`,
  status_page_skipped_over_limit: `${PREFIX}.status_page_skipped_over_limit`,
  custom_domain_dropped: `${PREFIX}.custom_domain_dropped`,
  custom_domain_dropped_over_limit: `${PREFIX}.custom_domain_dropped_over_limit`,
  retention_capped: `${PREFIX}.retention_capped`,
};

/** One line in the user's language for a change the import made; a kind we don't know gets a generic line. */
export const changeMessage = (change: WorkspaceportChange, t: Translate): string => {
  const key = CHANGE_KEYS[`${change.kind}:${change.reason ?? ""}`] ?? CHANGE_KEYS[change.kind];
  if (key) {
    return t(key, { name: change.name, from: change.from, to: change.to, reason: change.reason });
  }
  const subject = [change.record, change.name].filter(Boolean).join(" ");
  return t(`${PREFIX}.unknown`, { subject, kind: change.kind });
};

const CREATED_KEYS = ["tags", "proxies", "notificationChannels", "monitors", "maintenances", "statusPages"] as const;

export type CreatedLine = { key: (typeof CREATED_KEYS)[number]; label: string; count: number };

/** What the import created, as labelled counts; what it didn't create is left out. */
export const createdLines = (created: WorkspaceportCreated, t: Translate): CreatedLine[] =>
  CREATED_KEYS.filter((key) => created[key] > 0).map((key) => ({
    key,
    label: t(`settings.workspace_import.created.${key}`),
    count: created[key],
  }));

type RejectedBody = { error?: { code?: unknown; problems?: unknown } };

const rejectedBody = (error: unknown): RejectedBody["error"] =>
  isAxiosError(error) ? (error.response?.data as RejectedBody | undefined)?.error : undefined;

/** The problems of a file the server refused (400, INVALID_WORKSPACE_FILE), or undefined for any other failure. */
export const importProblems = (error: unknown): WorkspaceportProblem[] | undefined => {
  const body = rejectedBody(error);
  if (body?.code !== "INVALID_WORKSPACE_FILE") return undefined;
  return Array.isArray(body.problems) ? (body.problems as WorkspaceportProblem[]) : [];
};

/** A problem as one line: which record, then what is wrong with it (the server's words). */
export const problemLine = (problem: WorkspaceportProblem): string =>
  `${[problem.record, problem.ref].filter(Boolean).join(" ")}: ${problem.message}`;

// The queries an import makes stale: the records it adds (and what hangs off a monitor), and the
// settings it can cap
const IMPORTED_QUERIES = [
  "getMonitors",
  "getNotificationChannels",
  "getTags",
  "getProxies",
  "getMaintenances",
  "getStatusPages",
  "getSettings",
];

/** Whether the query with this key should be fetched again after an import. */
export const affectedByImport = (queryKey: readonly unknown[]): boolean => {
  const id = (queryKey[0] as { _id?: unknown } | undefined)?._id;
  return typeof id === "string" && IMPORTED_QUERIES.some((prefix) => id.startsWith(prefix));
};
