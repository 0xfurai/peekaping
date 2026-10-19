# Peekaping server

## Docs
http://localhost:8034/docs

The API contract comes from typed Huma registrations. From the repository root, run `bun run api:generate` to export OpenAPI and regenerate the frontend client, `bun run api:validate` to validate it, or `bun run api:check` to check committed artifacts. Export needs no database or Redis.

See [API contract and route inventory](docs/README.md) for compatibility details and exclusions.
