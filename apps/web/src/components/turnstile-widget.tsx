import { useEffect, useRef } from "react";
import { loadTurnstile, type TurnstileApi } from "@/lib/turnstile";

// Cloudflare's Turnstile check. `onToken` gets the token once the visitor passes, and null when it
// expires or fails. Changing `resetKey` asks for a new check, which a token that was refused needs.
export function TurnstileWidget({
  siteKey,
  onToken,
  resetKey,
}: {
  siteKey: string;
  onToken: (token: string | null) => void;
  resetKey: number;
}) {
  const container = useRef<HTMLDivElement>(null);
  const api = useRef<TurnstileApi | null>(null);
  const widgetId = useRef<string | null>(null);
  // The latest callback, without rendering the widget again when the parent's changes
  const callback = useRef(onToken);
  useEffect(() => {
    callback.current = onToken;
  }, [onToken]);

  useEffect(() => {
    let cancelled = false;
    loadTurnstile()
      .then((turnstile) => {
        if (cancelled || !container.current) return;
        api.current = turnstile;
        widgetId.current = turnstile.render(container.current, {
          sitekey: siteKey,
          callback: (token) => callback.current(token),
          "expired-callback": () => callback.current(null),
          "error-callback": () => callback.current(null),
        });
      })
      .catch((error) => console.error("Failed to load the Turnstile check", error));
    return () => {
      cancelled = true;
      if (api.current && widgetId.current !== null) api.current.remove(widgetId.current);
      api.current = null;
      widgetId.current = null;
    };
  }, [siteKey]);

  const firstReset = useRef(resetKey);
  useEffect(() => {
    if (resetKey === firstReset.current) return;
    if (api.current && widgetId.current !== null) api.current.reset(widgetId.current);
  }, [resetKey]);

  return <div ref={container} data-turnstile className="flex justify-center" />;
}
