import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { downloadName, filenameFromDisposition, saveBlob, saveDownload } from "./save-blob";

describe("filenameFromDisposition", () => {
  it("reads a quoted filename", () => {
    expect(filenameFromDisposition('attachment; filename="peekaping-workspace-2026-10-29.json"', "x.json")).toBe(
      "peekaping-workspace-2026-10-29.json"
    );
  });

  it("reads an unquoted filename", () => {
    expect(filenameFromDisposition("attachment; filename=export.ndjson", "x.json")).toBe("export.ndjson");
  });

  it("prefers the encoded filename* and decodes it", () => {
    expect(
      filenameFromDisposition("attachment; filename=\"plain.json\"; filename*=UTF-8''na%C3%AFve.json", "x.json")
    ).toBe("naïve.json");
  });

  it("falls back when the header is missing, has no filename, or is not text", () => {
    expect(filenameFromDisposition(undefined, "fallback.json")).toBe("fallback.json");
    expect(filenameFromDisposition("attachment", "fallback.json")).toBe("fallback.json");
    expect(filenameFromDisposition(42, "fallback.json")).toBe("fallback.json");
  });

  it("never lets the header name a path", () => {
    expect(filenameFromDisposition('attachment; filename="../../etc/passwd"', "fallback.json")).toBe("passwd");
    expect(filenameFromDisposition('attachment; filename="..\\evil.json"', "fallback.json")).toBe("evil.json");
    expect(filenameFromDisposition('attachment; filename=".."', "fallback.json")).toBe("fallback.json");
  });
});

describe("downloadName", () => {
  it("is the file's name with the day, in UTC like the server's", () => {
    const at = new Date("2026-10-29T23:30:00Z");
    expect(downloadName("peekaping-workspace", "json", at)).toBe("peekaping-workspace-2026-10-29.json");
    expect(downloadName("peekaping-heartbeats", "ndjson", at)).toBe("peekaping-heartbeats-2026-10-29.ndjson");
  });
});

// The parts of the page saveBlob touches
const fakePage = () => {
  const link = { href: "", download: "", click: vi.fn(), remove: vi.fn() };
  const body = { append: vi.fn() };
  return {
    link,
    body,
    document: { createElement: vi.fn(() => link), body } as unknown as Document,
    urls: { createObjectURL: vi.fn(() => "blob:abc"), revokeObjectURL: vi.fn() },
  };
};

describe("saveBlob", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("saves the blob through a temporary link named for the file, then cleans up", () => {
    const page = fakePage();
    const blob = new Blob(["{}"]);
    saveBlob(blob, "a.json", page);

    expect(page.urls.createObjectURL).toHaveBeenCalledWith(blob);
    expect(page.link.href).toBe("blob:abc");
    expect(page.link.download).toBe("a.json");
    expect(page.body.append).toHaveBeenCalledWith(page.link);
    expect(page.link.click).toHaveBeenCalledOnce();
    expect(page.link.remove).toHaveBeenCalledOnce();
    // The object URL lives a moment longer, so that the browser can read the blob
    expect(page.urls.revokeObjectURL).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1000);
    expect(page.urls.revokeObjectURL).toHaveBeenCalledWith("blob:abc");
  });
});

describe("saveDownload", () => {
  it("names the file from the response's Content-Disposition", () => {
    const page = fakePage();
    saveDownload(
      { data: new Blob(["x"]), headers: { "content-disposition": 'attachment; filename="server-name.json"' } },
      "fallback.json",
      page
    );
    expect(page.link.download).toBe("server-name.json");
  });

  it("uses the fallback name when the browser hides the header", () => {
    const page = fakePage();
    saveDownload({ data: new Blob(["x"]), headers: {} }, "fallback.json", page);
    expect(page.link.download).toBe("fallback.json");
  });
});
