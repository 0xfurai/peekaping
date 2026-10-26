---
sidebar_position: 0
---

# Deployment modes

Peekaping runs in one of two modes. Both run the same code and have the same features. They differ in how the server's two parts, the API and the probe that runs the checks, are split into processes, and so in how far an install can scale.

- **[Single mode](#single-mode)** is the default for new installs. One process runs both. It runs from the SQLite or PostgreSQL bundle image.
- **[Cluster mode](#cluster-mode)** is for installs that need to scale out: Kubernetes with the Helm chart, or the cluster Docker Compose stack.

There is no setting that switches between them. The mode is the image you run. The `MODE` setting (`dev`, `prod` or `test`) has nothing to do with it.

## Single mode

In single mode, Peekaping is one process, `peekaping`, that is both the API and the [local probe](../architecture/probe.md#who-runs-the-local-probes-checks). It passes work and events between its parts in memory, so there is no Redis. The same process serves the web app, the API and the websocket on port 8383.

You run it from a bundle image, which is one container with everything in it:

- `0xfurai/peekaping-bundle-sqlite` keeps its data in a SQLite file.
- `0xfurai/peekaping-bundle-postgres` runs PostgreSQL 17 in the same container, beside `peekaping`.

Single mode supports SQLite and PostgreSQL only. It doesn't scale out: one process runs every check.

Because the `peekaping` process checks the local probe's monitors, a `peekaping-probe` connects to single mode only as a [remote probe](./remote-probes.md), with that probe's own token. A probe instance that uses the local probe's token would check the same monitors a second time, so `peekaping` refuses it with a message that says why, and the probe logs that message. `peekaping` also ignores `PROBE_BOOTSTRAP_TOKEN`: it doesn't store it, and logs a warning at startup when it is set.

## Cluster mode

In cluster mode, the API and the probe are separate processes:

- The **API replicas** share the database and talk to each other through Redis. The API stores the results, and one replica, the leader, runs the stale-monitor watchdog, the probe offline sweep and the push monitors' checks.
- The **probe instances** (`peekaping-probe`) check the local probe's monitors. They connect to the API, get their monitors, run the checks and send the results back, as a [remote probe](./remote-probes.md) does. They need no database and no Redis.

You can run several of each, for scale and high availability. To check more monitors, add probe replicas. Use cluster mode when one process isn't enough.

The probe instances find the API with `PEEKAPING_URL` and prove they are the local probe with a token. The API stores that token at startup from `PROBE_BOOTSTRAP_TOKEN`, and the probes send the same value as `PROBE_TOKEN`. It is at least 32 characters, such as the output of `openssl rand -hex 32`. The Compose files and the Helm chart set both for you: the Compose files stop with a message until you put `PROBE_BOOTSTRAP_TOKEN` in `.env`, and the chart makes a Secret with a random token.

You run cluster mode in one of two ways:

- On Kubernetes, with the Helm chart. See [Kubernetes (Helm)](./kubernetes-helm.md).
- With Docker Compose, one container per part. See the Cluster mode section of [Docker + PostgreSQL](./docker-with-postgres.md#cluster-mode) or [Docker + SQLite](./docker-with-sqlite.md#cluster-mode).

The cluster images run as a non-root user. See [Cluster user and permissions](./cluster-user.md).

In cluster mode the local probe is online while one of its instances is connected, like a remote probe. If you stop the probe deployment, Peekaping tells the local probe's own notification channels, or, when it has none (the default), the channels of its monitors, that the local probe is offline. In single mode the local probe is never offline.

Coming from the layout with a producer, a worker and an ingester? Their images are no longer published. See [Upgrading to probes](./upgrading-to-probes.md).

## What each mode runs

| | Single mode | Cluster mode |
| --- | --- | --- |
| Images | `0xfurai/peekaping-bundle-sqlite` or `0xfurai/peekaping-bundle-postgres` | `0xfurai/peekaping-api`, `-probe`, `-web` and `-migrate` |
| Processes | `peekaping`, and PostgreSQL in the PostgreSQL bundle | The API, the probe and a web server. `migrate` runs once to set up the database. |
| Who checks the local probe's monitors | The `peekaping` process | The probe instances |
| Web app | Served by `peekaping` on port 8383 | The `web` image. Docker Compose puts an nginx gateway on port 8383 in front of it, and Helm uses an ingress or Istio. |
| Redis | None | Required, between the API replicas |
| Storage backends | SQLite or PostgreSQL | PostgreSQL with Helm. PostgreSQL or SQLite with Docker Compose. |
| Scaling | One process | Several API replicas and several probe instances |
| Set up with | [Docker + SQLite](./docker-with-sqlite.md), [Docker + PostgreSQL](./docker-with-postgres.md) | [Kubernetes (Helm)](./kubernetes-helm.md), the Cluster mode sections of [Docker + PostgreSQL](./docker-with-postgres.md#cluster-mode) and [Docker + SQLite](./docker-with-sqlite.md#cluster-mode) |

## Which mode to use

Start with single mode. In single mode you choose the storage backend by choosing the image. [Supported databases](../architecture/overview.md#supported-databases) compares SQLite and PostgreSQL.

Use cluster mode when you need to scale out, for example to run several API replicas and probe instances, or when you already run on Kubernetes. It needs a Redis, and more containers or pods to run and update.

## Upgrading to single mode

The bundle images from 0.0.44 to 0.0.47 ran Redis, Caddy and supervisord beside Peekaping's processes. To move one to the single-mode bundle, follow [Upgrading to single mode](./upgrading-to-single-mode.md).

Helm and the Docker Compose stacks stay in cluster mode.

## Remote probes

Both modes can check monitors from other machines, with remote probes. A probe connects out to the API, so it works with either mode and needs no extra service on the server. See [Remote probes](./remote-probes.md).
