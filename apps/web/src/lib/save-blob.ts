// Saving a file the API sends (PP-170). The export routes need the Authorization and X-Workspace-ID
// headers the app's client adds, so a plain link can't fetch them: the client gets the file as a
// blob, and a temporary link saves it.

const DISPOSITION_FILENAME_STAR = /filename\*\s*=\s*[^']*'[^']*'([^;]+)/i;
const DISPOSITION_FILENAME = /filename\s*=\s*(?:"([^"]*)"|([^;]+))/i;

const decode = (value: string): string => {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
};

// A name from a header must not choose a folder: only its last part is kept
const baseName = (name: string): string => name.split(/[\\/]/).pop()?.trim() ?? "";

/**
 * The file name in a Content-Disposition header (`filename*` before `filename`), or the fallback
 * when the header is missing, has no name, or the browser hid it (the API's CORS answer doesn't
 * expose it to a page on another origin).
 */
export const filenameFromDisposition = (header: unknown, fallback: string): string => {
  if (typeof header !== "string") return fallback;
  const encoded = DISPOSITION_FILENAME_STAR.exec(header)?.[1];
  const plain = DISPOSITION_FILENAME.exec(header);
  const raw = encoded ? decode(encoded.trim()) : (plain?.[1] ?? plain?.[2] ?? "");
  const name = baseName(raw);
  return name && name !== ".." && name !== "." ? name : fallback;
};

/** `<prefix>-<yyyy-mm-dd>.<extension>`, the day in UTC as the server names its files. */
export const downloadName = (prefix: string, extension: string, now: Date = new Date()): string =>
  `${prefix}-${now.toISOString().slice(0, 10)}.${extension}`;

const REVOKE_AFTER_MS = 1000;

type Page = {
  document: Pick<Document, "createElement" | "body">;
  urls: Pick<typeof URL, "createObjectURL" | "revokeObjectURL">;
};

const thisPage = (): Page => ({ document, urls: URL });

/** Saves the blob as a file of this name, through a temporary link. */
export const saveBlob = (blob: Blob, filename: string, page: Page = thisPage()): void => {
  const url = page.urls.createObjectURL(blob);
  const link = page.document.createElement("a");
  link.href = url;
  link.download = filename;
  page.document.body.append(link);
  link.click();
  link.remove();
  // The browser reads the blob after the click returns: a revoke straight away can cancel the save
  setTimeout(() => page.urls.revokeObjectURL(url), REVOKE_AFTER_MS);
};

/**
 * An API download as saveDownload takes it. The generated client asks for these routes as blobs,
 * but types the answer by the route's JSON schema, so the body is checked rather than trusted.
 */
export const asDownload = (response: {
  data: unknown;
  headers: Record<string, unknown>;
}): { data: Blob; headers: Record<string, unknown> } => {
  if (!(response.data instanceof Blob)) throw new Error("the download did not come back as a file");
  return { data: response.data, headers: response.headers };
};

/** Saves the blob an API call answered with, named by its Content-Disposition or else the fallback. */
export const saveDownload = (
  response: { data: Blob; headers: Record<string, unknown> },
  fallbackName: string,
  page?: Page
): void => {
  saveBlob(response.data, filenameFromDisposition(response.headers["content-disposition"], fallbackName), page);
};
