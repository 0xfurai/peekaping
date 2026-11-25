# Peekaping server

## Docs
http://localhost:8034/docs

The API contract comes from typed `httpx.Register` registrations, which [oasome](https://github.com/0xfurai/oasome) turns into OpenAPI. From the repository root, run `bun run api:generate` to export OpenAPI and regenerate the frontend client, `bun run api:validate` to validate it, or `bun run api:check` to check committed artifacts. Export needs no database or Redis.

See [API contract and route inventory](docs/README.md) for compatibility details and exclusions.
