import { type Ping, validatePing } from "./validate";

export interface Env {
  TELEMETRY: AnalyticsEngineDataset;
}

export const MAX_BODY_BYTES = 4096;
const PING_PATH = "/v1/ping";

// Every response is a status plus, for errors, a short fixed text. Nothing from the request is echoed.
function respond(status: number, text?: string, headers: Record<string, string> = {}): Response {
  return new Response(text === undefined ? null : `${text}\n`, {
    status,
    headers: { "cache-control": "no-store", ...(text === undefined ? {} : { "content-type": "text/plain" }), ...headers },
  });
}

// Reads the body, giving up as soon as it passes the limit, whatever Content-Length claims.
async function readBody(request: Request): Promise<Uint8Array | null> {
  const declared = request.headers.get("content-length");
  if (declared !== null && Number(declared) > MAX_BODY_BYTES) return null;
  if (request.body === null) return new Uint8Array();

  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > MAX_BODY_BYTES) {
      await reader.cancel();
      return null;
    }
    chunks.push(value);
  }

  const body = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return body;
}

// One point for the ping and one for each monitor type and notification channel type. The instance ID is the index,
// so a distinct count of index1 is a count of instances. At most 1 + 50 + 50 points, under the 250 per invocation.
// Only validated payload fields go in: no IP, header, country or request.cf data.
export function dataPoints(ping: Ping): AnalyticsEngineDataPoint[] {
  const indexes = [ping.instance_id];
  const { status_pages, maintenance, metrics } = ping.features;
  return [
    {
      indexes,
      blobs: [
        "ping",
        ping.version,
        ping.mode,
        ping.db,
        ping.os,
        ping.monitors,
        String(status_pages),
        String(maintenance),
        String(metrics),
      ],
    },
    ...ping.monitor_types.map((type) => ({ indexes, blobs: ["monitor_type", type] })),
    ...ping.notification_providers.map((type) => ({ indexes, blobs: ["notification_channel", type] })),
  ];
}

export async function handleRequest(request: Request, env: Env): Promise<Response> {
  if (new URL(request.url).pathname !== PING_PATH) return respond(404, "not found");
  if (request.method !== "POST") return respond(405, "method not allowed", { allow: "POST" });

  const body = await readBody(request);
  if (body === null) return respond(400, "body too large");

  let payload: unknown;
  try {
    payload = JSON.parse(new TextDecoder("utf-8", { fatal: true, ignoreBOM: false }).decode(body));
  } catch {
    return respond(400, "body is not valid JSON");
  }

  const result = validatePing(payload);
  if (!result.ok) return respond(400, result.error);

  for (const point of dataPoints(result.ping)) env.TELEMETRY.writeDataPoint(point);
  return respond(204);
}
