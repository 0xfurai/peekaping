# 0001: Deprecate MongoDB storage

Status: Accepted, amended 2026-10-25
Date: 2026-10-19
Ticket: PP-23

## Context

Peekaping has three **storage backends**: SQLite, PostgreSQL and MongoDB. The storage backend is the database an install keeps its data in, set with `DB_TYPE`. It is not the **MongoDB monitor type**, which checks someone else's MongoDB server. That monitor type stays. (`DB_TYPE=mysql` is also accepted, but its migrations are Postgres-only; PP-76 decides what to do about it.)

Each module reads and writes its data through a repository (Go code for one storage backend). SQLite and PostgreSQL share one set of SQL repositories. MongoDB has its own. So every feature that stores data is built twice.

- The Mongo repositories are about 4.8k lines. The SQL repositories are about 3.5k lines. Both cover 19 modules.
- MongoDB is the least used bundle image (the all-in-one Docker image for one storage backend): about 15k pulls, against about 73k for SQLite and 27k for PostgreSQL.

We looked at three options:

1. **Keep full support.** Every feature and fix lands on both. The double work stays.
2. **Freeze.** Mongo stays, but gets no new work. Users have no way out.
3. **Deprecate.** Freeze Mongo, tell users, and ship a way to move to SQL, then remove it.

## Decision

We deprecate MongoDB as a storage backend. We chose this on 2026-10-19 because it ends the double work and gives Mongo users a way out.

Existing Mongo installs keep working. The API, producer and ingester log a warning at startup. The MongoDB self-hosting page opens with a deprecation notice.

While Mongo is deprecated, it is frozen:

- **New features** are SQL-only. How a Mongo install behaves without a feature's repository is designed with the first such feature, not now. Today a module with no Mongo repository stops a Mongo install from starting, because `utils.RegisterRepositoryByDBType` (`apps/server/internal/utils/dig.go`) registers nothing for it.
- **Interface changes** touch Mongo repositories only enough to compile and keep today's behaviour.
- **Mongo-only bugs** are fixed only when they lose data, stop the app starting, or leak security. Others are closed with a pointer to the migration. So there is no contract test suite for the two backends (PP-81 shrinks to nothing), and PP-70, PP-184 and PP-183 are judged case by case against this rule.
- **Existing Mongo tests, the e2e matrix and the Mongo images**: the existing Mongo tests stay in the repository. The e2e matrix and the Mongo images keep running in CI until removal. The e2e matrix is the CI run of the Playwright browser tests once per storage backend. CI doesn't set `PEE_TEST_MONGO_URI`, so the Mongo repository tests run only when someone sets it (see `.agents/skills/ticket/TESTING.md`).
- **Removal** happens no earlier than the second release after the one that ships the migration command (`mongo → sqlite/postgres`, not built yet). It deletes the Mongo repositories, `infra/mongo.go`, the `mongo` and `mongodb` values of `DB_TYPE`, the bundle image, compose files, the CI matrix entry, the Makefile targets and the docs page. It keeps the MongoDB monitor type and the Mongo driver, which that monitor type uses.

Single-container mode (PP-24) covers SQLite and PostgreSQL only. The Mongo bundle keeps today's multi-process layout until removal.

New installs are pointed at SQLite or PostgreSQL. The README, docs site, landing page, example `.env` files and Makefile defaults no longer present Mongo as an equal choice.

## Consequences

- Features ship once, on the SQL repositories. Reviews flag Mongo changes beyond what this ADR allows. The ticket skills (plan, review, testing) say so.
- Mongo users get no new features. Some may want one and have to move. That is the point of the migration command.
- A SQL-only feature must not stop a Mongo install from starting. Today it would, because a module with no Mongo repository leaves the app unable to build its services. So the first SQL-only feature has to give Mongo a way to start without its repository, and say how in its own ticket.
- The Mongo bundle, compose files, images and tests cost upkeep until removal. We accept that for the notice period.

Waiting tickets:

- **PP-81** (contract test suite for both backends): not built.
- **PP-91**: shrinks. Its Mongo half goes at removal.
- **PP-86**: the Mongo path keeps its current behaviour. No transaction work there.
- **PP-207**: move the SQL code. Touch Mongo only to compile.
- **PP-148, PP-149, PP-150** (organization scoping): written for the SQL repositories. Mongo behaviour is decided in PP-142.
- **PP-24** (single-container mode): SQLite and PostgreSQL only.
- **PP-70, PP-184, PP-183, PP-72**: judged case by case against the Mongo-only bug rule above.
- **PP-67**: `DB_TYPE=mongodb` passes config validation, but `dig.go` only matches `mongo`. So `mongodb` registers no repositories and the app won't start. A bug that stops the app starting is one the Mongo-only bug rule allows us to fix. Removal deletes the `mongodb` value anyway.

## Update

Date: 2026-10-21
Ticket: #227

The migration command now exists as `bun db import-mongo`. It copies a MongoDB install into SQLite or PostgreSQL, with its history, and the [MongoDB page](../../apps/docs/docs/self-hosting/docker-with-mongo.md#move-to-sqlite-or-postgresql) gives the steps. So:

- The "not built yet" in the Removal bullet no longer holds.
- The removal clock starts with the release that ships the command: removal happens no earlier than the second release after that one.
- The command reads the MongoDB collections itself and writes the SQL tables. The Mongo repositories are unchanged.

## Update

Date: 2026-10-25
Ticket: PP-277

MongoDB storage is removed, as the Removal bullet above says. It ships in the same release as `bun db import-mongo`, the first after 0.0.47, which is the last release that runs on MongoDB storage. The freeze above is over, since nothing is left to freeze.

The maintainer dropped the notice period on 2026-10-25, so the second-release notice period (the Removal bullet, and the #227 update above) no longer holds. The older text stays as history. A Mongo install moves with that release's own `bun db import-mongo` (in the migrate image and in the SQLite and PostgreSQL bundle images) before it starts on SQL, and stays on 0.0.47 until then.

What went:

- The Mongo repositories of every module, `infra/mongo.go`, and the helpers that only they used.
- The `mongo` and `mongodb` values of `DB_TYPE`. A Mongo `DB_TYPE` stops every binary, and `bun db` and the migrate image with them, with an error that says MongoDB storage was removed and links to the move guide. The Helm chart refuses the two values at render time, with a message that points PostgreSQL installs to the same guide.
- The `peekaping-bundle-mongo` image, which gets no new tags, the Mongo compose files, the `mongo` profile of `docker-compose.deps.yml`, the Makefile targets, the CI jobs that built and published the image, and the Mongo leg of the e2e matrix. The e2e specs no longer skip on MongoDB.
- The self-hosting page for MongoDB, down to the move guide. It keeps its URL and its `#move-to-sqlite-or-postgresql` anchor, so the links to it, and the one in the error, still work.

What stays:

- The MongoDB monitor type and the Mongo driver it uses.
- `bun db import-mongo`, in the SQLite and PostgreSQL bundle images and in the migrate image, with its tests and its `import-mongo` CI job. The tests seed a real MongoDB with raw documents, from copies of the models that the repositories wrote, which `mongoimporttest` keeps. The format of an old Mongo install can't change any more, so the copies are frozen.
- `PEE_TEST_MONGO_URI`, which only the import tests read now.
- The Telemetry Worker still accepts `mongo`, because older installs still send it.

The Consequences about upkeep and about SQL-only features no longer apply, and neither do the waiting tickets that were only about the Mongo repositories.
