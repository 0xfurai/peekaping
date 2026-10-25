---
sidebar_position: 0
---

# Deployment modes

Peekaping runs in one of two modes. Both run the same code and have the same features. They differ in how the server's four roles (the API, the producer, the worker and the ingester) talk to each other, and so in how far an install can scale.

- **[Single mode](#single-mode)** is the default for new installs. One process runs every role. It runs from the SQLite or PostgreSQL bundle image.
- **[Cluster mode](#cluster-mode)** is for installs that need to scale out: Kubernetes with the Helm chart, or the cluster Docker Compose stack.

There is no setting that switches between them. The mode is the image you run. The `MODE` setting (`dev`, `prod` or `test`) has nothing to do with it.

## Single mode

In single mode, Peekaping is one process, `peekaping`, with every role inside it. The roles pass work and events to each other in memory, so there is no Redis. The same process serves the web app, the API and the websocket on port 8383.

You run it from a bundle image, which is one container with everything in it:

- `0xfurai/peekaping-bundle-sqlite` keeps its data in a SQLite file.
- `0xfurai/peekaping-bundle-postgres` runs PostgreSQL 17 in the same container, beside `peekaping`.

Single mode supports SQLite and PostgreSQL only. It doesn't scale out: one process runs every check.

## Cluster mode

In cluster mode, each role is its own process, and the roles talk through Redis. You can run several of each, for scale and high availability. Use it when one process isn't enough.

You run it in one of two ways:

- On Kubernetes, with the Helm chart. See [Kubernetes (Helm)](./kubernetes-helm.md).
- With Docker Compose, one container per role. See the Cluster mode section of [Docker + PostgreSQL](./docker-with-postgres.md#cluster-mode) or [Docker + SQLite](./docker-with-sqlite.md#cluster-mode).

The cluster images run as a non-root user. See [Cluster user and permissions](./cluster-user.md).

The MongoDB bundle image also runs cluster mode, with all the processes in one container. MongoDB storage is deprecated, so new installs shouldn't use it. See [Docker + MongoDB](./docker-with-mongo.md).

## What each mode runs

| | Single mode | Cluster mode |
| --- | --- | --- |
| Images | `0xfurai/peekaping-bundle-sqlite` or `0xfurai/peekaping-bundle-postgres` | `0xfurai/peekaping-api`, `-producer`, `-worker`, `-ingester`, `-web` and `-migrate` |
| Processes | `peekaping`, and PostgreSQL in the PostgreSQL bundle | One for each role, and a web server. `migrate` runs once to set up the database. |
| Web app | Served by `peekaping` on port 8383 | The `web` image. Docker Compose puts an nginx gateway on port 8383 in front of it, and Helm uses an ingress or Istio. |
| Redis | None | Required |
| Storage backends | SQLite or PostgreSQL | PostgreSQL with Helm. PostgreSQL, SQLite or MongoDB (deprecated) with Docker Compose. |
| Scaling | One process | Several of each role |
| Set up with | [Docker + SQLite](./docker-with-sqlite.md), [Docker + PostgreSQL](./docker-with-postgres.md) | [Kubernetes (Helm)](./kubernetes-helm.md), the Cluster mode sections of [Docker + PostgreSQL](./docker-with-postgres.md#cluster-mode) and [Docker + SQLite](./docker-with-sqlite.md#cluster-mode) |

## Which mode to use

Start with single mode. In single mode you choose the storage backend by choosing the image. [Supported databases](../architecture/overview.md#supported-databases) compares SQLite and PostgreSQL.

Use cluster mode when you need to scale out, for example to run several workers, or when you already run on Kubernetes. It needs a Redis, and more containers or pods to run and update.

## Upgrading to single mode

The bundle images from 0.0.44 to 0.0.47 ran Redis, Caddy and supervisord beside Peekaping's processes. To move one to the single-mode bundle, follow [Upgrading to single mode](./upgrading-to-single-mode.md).

Helm and the Docker Compose stacks stay in cluster mode.

## Remote probes

Both modes can check monitors from other machines, with remote probes (not with MongoDB storage). A probe connects out to the API, so it works with either mode and needs no extra service on the server. See [Remote probes](./remote-probes.md).
