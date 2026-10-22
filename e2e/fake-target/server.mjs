// The local service e2e monitors check and notify, so no test needs a public host.
// docker-compose.e2e.yml runs it as `e2e-target`. It has no dependencies, so it
// runs straight from the node image.
//
//   GET  /health         200, for the start-up wait
//   PUT  /targets/<id>   {"status": 500, "rejectContentType": true} sets the code
//                        /targets/<id> answers with and optionally rejects a
//                        bodyless request that carries Content-Type
//   *    /targets/<id>   answers with that code (200 until set)
//   POST /webhooks/<id>  records the request: headers, parsed body and raw body
//   GET  /webhooks/<id>  the recorded requests, oldest first
//
// Tests pick a random id each, so they don't see each other's state.
import { createServer } from "node:http";

const port = Number(process.env.PORT ?? 3033);
const targetConfig = new Map();
const webhookRequests = new Map();

const readBody = async (req) => {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  return Buffer.concat(chunks).toString("utf8");
};

const parseJSON = (text) => {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
};

const send = (res, status, body) => {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
};

const handle = async (req, res) => {
  const [, resource, id, ...rest] = new URL(req.url, "http://localhost").pathname.split("/");
  const body = await readBody(req);

  if (resource === "health" && !id) return send(res, 200, { status: "ok" });
  if (!id || rest.length > 0) return send(res, 404, { error: "not found" });

  if (resource === "targets") {
    if (req.method === "PUT") {
      const { status, rejectContentType = false } = parseJSON(body) ?? {};
      if (!Number.isInteger(status) || status < 100 || status > 599) {
        return send(res, 400, { error: "status must be an HTTP status code" });
      }
      if (typeof rejectContentType !== "boolean") {
        return send(res, 400, { error: "rejectContentType must be a boolean" });
      }
      targetConfig.set(id, { status, rejectContentType });
      return send(res, 200, { status, rejectContentType });
    }
    const { status, rejectContentType } = targetConfig.get(id) ?? {
      status: 200,
      rejectContentType: false,
    };
    if (rejectContentType && !body && req.headers["content-type"]) {
      return send(res, 400, { error: "bodyless request must not send Content-Type" });
    }
    return send(res, status, { status });
  }

  if (resource === "webhooks") {
    const received = webhookRequests.get(id) ?? [];
    if (req.method === "POST") {
      received.push({
        receivedAt: new Date().toISOString(),
        headers: req.headers,
        body: parseJSON(body),
        rawBody: body,
      });
      webhookRequests.set(id, received);
      return send(res, 200, { ok: true });
    }
    if (req.method === "GET") return send(res, 200, received);
  }

  return send(res, 404, { error: "not found" });
};

// A bad request must not crash the server: a restart would lose every test's state.
const server = createServer((req, res) =>
  handle(req, res).catch((err) => {
    console.error(err);
    if (!res.headersSent) send(res, 500, { error: String(err) });
  }),
);

server.listen(port, () => console.log(`e2e target listening on :${port}`));

// PID 1 in a container gets no default SIGTERM handler; exit so `compose down` doesn't wait.
for (const signal of ["SIGTERM", "SIGINT"]) process.on(signal, () => process.exit(0));
