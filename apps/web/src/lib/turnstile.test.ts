import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TurnstileApi } from "./turnstile";

// A page with only what loadTurnstile touches: a head that takes script tags, and window.turnstile
type FakeScript = { src: string; async: boolean; onload?: () => void; onerror?: () => void; remove: () => void };

describe("loadTurnstile", () => {
  let scripts: FakeScript[];
  const api = { render: vi.fn(), reset: vi.fn(), remove: vi.fn() } as TurnstileApi;

  // The module remembers a load in progress, so each test gets a fresh copy
  let loadTurnstile: () => Promise<TurnstileApi>;

  beforeEach(async () => {
    vi.resetModules();
    ({ loadTurnstile } = await import("./turnstile"));
    scripts = [];
    const fakeWindow: { turnstile?: TurnstileApi } = {};
    vi.stubGlobal("window", fakeWindow);
    vi.stubGlobal("document", {
      createElement: () => {
        const script: FakeScript = {
          src: "",
          async: false,
          remove: () => {
            scripts = scripts.filter((s) => s !== script);
          },
        };
        return script;
      },
      head: { appendChild: (script: FakeScript) => scripts.push(script) },
      querySelector: () => scripts[0] ?? null,
    });
  });

  afterEach(() => vi.unstubAllGlobals());

  it("resolves with Cloudflare's API once the script has loaded", async () => {
    const loading = loadTurnstile();
    (window as { turnstile?: TurnstileApi }).turnstile = api;
    scripts[0].onload?.();

    await expect(loading).resolves.toBe(api);
    // and later calls use it without another script
    await expect(loadTurnstile()).resolves.toBe(api);
    expect(scripts).toHaveLength(1);
  });

  it("shares one script between calls made while it loads", async () => {
    const first = loadTurnstile();
    const second = loadTurnstile();
    expect(scripts).toHaveLength(1);
    (window as { turnstile?: TurnstileApi }).turnstile = api;
    scripts[0].onload?.();

    await expect(Promise.all([first, second])).resolves.toEqual([api, api]);
  });

  it("rejects when the script fails, and a retry loads it again", async () => {
    const failed = loadTurnstile();
    scripts[0].onerror?.();
    await expect(failed).rejects.toThrow("Turnstile did not load");
    expect(scripts, "the failed tag is removed, so a retry doesn't leave two").toHaveLength(0);

    const retry = loadTurnstile();
    expect(scripts, "a retry adds a new script").toHaveLength(1);
    (window as { turnstile?: TurnstileApi }).turnstile = api;
    scripts[0].onload?.();
    await expect(retry).resolves.toBe(api);
  });

  it("rejects when the script loads without defining window.turnstile", async () => {
    const loading = loadTurnstile();
    scripts[0].onload?.();
    await expect(loading).rejects.toThrow("Turnstile did not load");
  });
});
