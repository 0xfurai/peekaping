# API contract

`httpx.Register` registrations and typed Go inputs/outputs define the REST contract; [oasome](https://github.com/0xfurai/oasome) builds the OpenAPI document from them. The server and `go run ./cmd/openapi` use the same registrations. Export does not connect to databases or Redis. `openapi.json` and `openapi.yaml` are committed artifacts; the frontend client reads `openapi.json`.

From the repository root:

```sh
bun run api:generate
bun run api:validate
bun run api:check
```

Generation uses pinned oasome, Hey API, and Prettier versions. CI regenerates the spec and client, validates OpenAPI 3.1, and rejects changes to generated artifacts. The exporter uses the version declared in `internal/version`; release builds may override the runtime version.

## Route inventory

All 92 REST operations are represented, including non-JSON responses:

| Area | Operations |
| --- | ---: |
| Monitors | 12 |
| Status pages | 9 |
| Maintenance windows | 8 |
| Authentication | 7 |
| Notification channels | 7 |
| SVG badges | 7 |
| API keys | 6 |
| Probes | 8 |
| Proxies | 6 |
| Tags | 6 |
| Settings | 3 |
| Telemetry | 3 |
| Error reporting | 2 |
| Probe protocol (`/probe/v1`) | 4 |
| Push URL | 1 |
| API health/version | 2 |
| Root `/health` alias | 1 |

The probe protocol is served under `/probe/v1`, beside `/api/v1`, and listed under the `ProbeAuth` bearer scheme: a probe sends its token as `Authorization: Bearer <token>` and its instance id as `X-Probe-Instance`, and the admin's JWT and API keys open none of these routes. Its request and reply bodies are the messages of `internal/probeproto`, not the `{message,data}` envelope; its errors keep the envelope. A body may be gzipped, up to 1 MB once decompressed, and replies are gzipped when `Accept-Encoding` allows it. Operation IDs start with `probe` (`probePostHello`).

Socket.IO GET/POST polling and WebSocket upgrades are transport endpoints, excluded from the REST specification. Documentation/spec/schema endpoints are also excluded. Unknown-method responses and CORS preflight behavior remain router/middleware concerns. Tests compare the actual REST route manifest with the specification and verify security against the authentication allowlists.

## URLs

- `/docs`: interactive reference.
- `/openapi.json`, `/openapi.yaml`: OpenAPI 3.1.
- `/swagger/index.html`: temporary redirect (307) to `/docs`.
- `/swagger/doc.json`: alias serving the OpenAPI 3.1 document, replacing Swagger 2.0.

Old Swagger UI assets return 404. Integrations requiring Swagger 2.0 must migrate. The spec uses a relative `/` server URL and full paths, including `/api/v1`; it does not hard-code a deployment host. Frontend client configuration supplies the API origin without appending `/api/v1` again.

## Compatibility

`httpx.Input[Body, Parameters]` declares each handler's body and parameter types. Its `Decode` method deliberately retains the existing one-value JSON decoder and endpoint validation order. Parameter models supply OpenAPI metadata; controllers retain their existing parsing, defaults, and error messages. Unknown JSON fields, empty query defaults, loose boolean parsing, and PATCH omission/null/empty distinctions remain supported. No automatic binding or validation runs ahead of these handlers. The router's `httpx.LimitBody` does: it answers 413 to a request that declares a body over 1 MiB before authentication or the handler run, and makes reading any other body fail at 1 MiB; the one exception is the workspace import, whose body may be 16 MiB.

Typed `httpx.Output[T]` responses are written by `httpx` with the existing compact JSON encoding, HTML escaping, content types, and status codes. SVG badges retain their cache headers. Authentication runs before handler decoding. Router compatibility includes wrong-method/HEAD rejection; HEAD never writes a push heartbeat. Socket.IO keeps standard HTTP handlers and upgrade-capable writers.

The default JSON error is `{message,data:null}`. Domain conflicts, login lockout (including `Retry-After`), and bare 2FA responses have their own typed schemas. There are no Problem Details, automatic 422 responses, `$schema` fields, or `Link` headers. A handler error that is not an `httpx.ResponseError` answers 500 with `{message,data:null}` and does not show its cause. PP-80's documentation drift is addressed here; PP-87 retains error-model unification and internal error handling changes.
