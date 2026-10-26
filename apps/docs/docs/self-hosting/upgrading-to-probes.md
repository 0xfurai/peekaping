---
sidebar_position: 5
---

# Upgrading to probes

This page takes a cluster on SQLite or PostgreSQL from the layout with a producer, a worker and an ingester to the layout with the API and probes. In the new layout, `peekaping-probe` instances check the local probe's monitors, and the API replicas store the results. See [Deployment modes](./deployment-modes.md#cluster-mode) for the cluster, and [Probe](../architecture/probe.md) for what the probe does.

The producer, worker and ingester are gone. The `peekaping-producer`, `peekaping-worker` and `peekaping-ingester` images are no longer published: 0.0.47 is the last release that has them, and the tags already published stay where they are.

## Who this is for

You need this page if you run [cluster mode](./deployment-modes.md#cluster-mode) on SQLite or PostgreSQL and upgrade to the release that retires the producer, worker and ingester:

- With Docker Compose: the `docker-compose.prod.sqlite.yml` or `docker-compose.prod.postgres.yml` stack, or your own compose file with `producer`, `worker` and `ingester` services.
- With Helm: a values file that sets `producer`, `worker` or `ingester`.

You don't need it for these:

- **Single mode.** The SQLite and PostgreSQL bundle images don't change. Their one process is the local probe.
- **MongoDB.** See [MongoDB storage was removed](#mongodb-storage-was-removed).
- **A new install.** The setup pages already describe the new layout.

No image of this release runs a producer, worker or ingester. A stack that pulls `latest` for them keeps the 0.0.47 images, which this release's API doesn't use, so follow this page to remove them.

## What changes

| | Before | After |
| --- | --- | --- |
| Processes | api, producer, worker, ingester, web | api, probe, web |
| Who checks the local probe's monitors | The producer schedules them and the worker runs them | The probe instances, which hold the local probe's token |
| Who stores the results | The ingester | The API replica that took the request |
| Watchdog, probe sweep, push monitors' checks | The leader producer | The API replica that holds the leader lock |
| Redis | Between all the processes | Between the API replicas only |
| New settings | None | `PROBE_BOOTSTRAP_TOKEN` on the API, and `PROBE_TOKEN` and `PEEKAPING_URL` on the probe |
| To check more monitors | Add workers | Add probe replicas |

The upgrade adds no database migration. Your monitors, heartbeats and settings stay as they are.

These things behave differently afterwards:

- **The local probe goes online and offline.** In cluster mode it is online while one of its instances is connected, as a remote probe is. If you stop the probe deployment, Peekaping tells the local probe's own notification channels, or, when it has none (the default), the channels of its monitors, that the local probe is offline. See [When a probe is offline](./remote-probes.md#when-a-probe-is-offline).
- **A stale monitor is notified, not checked again.** The watchdog can't make a probe check right away, so it notifies the monitor's channels once the wait has passed.
- **`PROBE_OFFLINE_AFTER` is read by the API.** The producer read it before. Set it on the API replicas now.
- **Docker monitors and ping monitors need the probe's permissions.** The Docker socket mount and `group_add` move from the `worker` service to the `probe` service, and so does a `sysctls` entry for ping. See [Cluster user and permissions](./cluster-user.md).
- **`QUEUE_CONCURRENCY` and `PRODUCER_CONCURRENCY` no longer size anything for the checks.** `PROBE_CONCURRENCY` (128 by default) sets how many checks one probe instance runs at the same time. `PRODUCER_CONCURRENCY` is no longer read, and `QUEUE_CONCURRENCY` only sizes the API's `notification` queue.
- **`ERROR_REPORTING=off` is needed on the API only.** The probe sends no error reports.

## Before you start

- Back up your database. For SQLite, see [Upgrading stats storage](./upgrading-stats.md#before-upgrading). For PostgreSQL, use your usual tested backup.
- Make a token. It is the local probe's token: the API stores it, and the probe sends it. It is a random string of at least 32 characters, and it gives access to the config of the monitors on the local probe, so keep it secret:

  ```bash
  openssl rand -hex 32
  ```

- Check that the probes can reach the API. In Docker Compose and Helm they reach it inside the stack, on port 8034, and need no inbound port and no Redis.

## The order

The steps are the same for every stack, and go in this order:

1. **Set the token.** Put it where the API and the probe both read it. The compose files and the chart refuse to start without it.
2. **Start the probe deployment.** The probe instances connect to the API with the token. Until the API holds the token, they are refused.
3. **Upgrade the API.** The new API stores the token, deals the local probe's monitors to the probe instances, stores their results and runs the leader jobs.
4. **Remove the producer, worker and ingester.** Once the probes check the monitors, nothing needs them.

The compose files and the chart of this release carry out steps 2 to 4 with one command, so the stack is without a checker for as long as it takes the probes to connect, which is seconds. A monitor goes stale only after three missed intervals, so a gap that short raises nothing.

If you do the steps one at a time, note two things. A probe connects only to an API that has the probe protocol, which comes with [remote probes](./remote-probes.md): from an older API, upgrade it first (step 3) and start the probes right after. And between steps 2 and 4 a monitor of the local probe can be checked by both the worker and a probe, so it can get a second heartbeat for one interval. Nothing is lost, and step 4 ends it.

## Docker Compose, SQLite or PostgreSQL

1. Back up the database, as above.
2. Set the token. Add it to the `.env` next to your compose file:

   ```bash
   echo "PROBE_BOOTSTRAP_TOKEN=$(openssl rand -hex 32)" >> .env
   ```

3. Take the new compose file from [Docker + SQLite](./docker-with-sqlite.md#docker-composeyml-file) or [Docker + PostgreSQL](./docker-with-postgres.md#docker-composeyml-file), and keep your own changes in it. If you edit your own file instead:
   - Delete the `producer`, `worker` and `ingester` services.
   - Add `PROBE_BOOTSTRAP_TOKEN` to the `api` service, under `environment`. Don't count on an `env_file`: your file may not have one, and the API starts without the token and stores none. The value must be the one the probe gets as `PROBE_TOKEN`:

     ```yaml
       api:
         environment:
           PROBE_BOOTSTRAP_TOKEN: ${PROBE_BOOTSTRAP_TOKEN}
     ```

   - Add a `probe` service:

     ```yaml
       probe:
         image: 0xfurai/peekaping-probe:latest
         restart: unless-stopped
         stop_grace_period: 30s
         environment:
           PEEKAPING_URL: http://api:8034
           PROBE_ALLOW_INSECURE: "true"
           PROBE_TOKEN: ${PROBE_BOOTSTRAP_TOKEN}
         depends_on:
           api:
             condition: service_healthy
         networks:
           - appnet
     ```

     The probe reaches the API over plain HTTP inside `appnet`, which is why it sets `PROBE_ALLOW_INSECURE`. Its token crosses `appnet` unencrypted, so `appnet` must stay trusted and isolated: put only the services of this stack on it, and don't publish the `api` port. If the probe connects through a network you don't trust, use an `https://` address in `PEEKAPING_URL` and remove `PROBE_ALLOW_INSECURE`.

   - Check that the two values match. This prints both, so don't paste the output anywhere:

     ```bash
     docker compose config | grep -E 'PROBE_(BOOTSTRAP_)?TOKEN'
     ```

     `PROBE_BOOTSTRAP_TOKEN` under `api` and `PROBE_TOKEN` under `probe` must show the same value. A probe whose token differs from the one the API stored is refused, and the local probe is reported offline.

   - Move what the `worker` had to the `probe`: the Docker socket mount and `group_add` for Docker monitors, and `sysctls` for ping if you set one.
   - Keep `REDIS_*` and the Redis service for the `api`.

4. Pull the new images and start the stack. `--remove-orphans` removes the containers of the services that are gone:

   ```bash
   docker compose pull
   docker compose up -d --remove-orphans
   ```

5. [Check that it worked](#check-that-it-worked).

To check more monitors at the same time, run more probes: `docker compose up -d --scale probe=3`. The monitors are dealt among them.

## Helm

The chart replaces the `producer`, `worker` and `ingester` values with `probe`.

1. Back up the PostgreSQL database.
2. Edit your values file:
   - Delete the `producer`, `worker` and `ingester` blocks. Move what you set there to `probe`: `probe.replicaCount`, `probe.resources`, and `probe.sysctls` in place of `worker.sysctls`. A leftover `producer`, `worker` or `ingester` block is ignored, and does nothing.
   - Delete the `image.tag` of any component that pins an old release, so that the chart version picks it.
   - Set nothing for the token, unless you render the chart without the cluster (`helm template`, Argo CD). The chart makes a Secret, `<release>-probe-token`, with a random token, and keeps it across upgrades. To use a Secret of your own, see [The local probe's token](./kubernetes-helm.md#the-local-probes-token).

3. Upgrade the release:

   ```bash
   helm upgrade peekaping oci://ghcr.io/0xfurai/charts/peekaping \
     --version <the new release> \
     --namespace peekaping \
     --values peekaping-values.yaml
   ```

   One revision creates the Secret and the `<release>-probe` Deployment, rolls the API pods once the migrate Job is done, and removes the producer, worker and ingester Deployments.

4. [Check that it worked](#check-that-it-worked).

## Check that it worked

- Only the new services run. In Docker Compose, `docker compose ps` lists `api`, `probe`, `web`, `gateway`, `redis` and your database. In Kubernetes, `kubectl get pods --namespace peekaping` lists the API, probe and web pods.
- Open **Probes** in Peekaping. The **Local** probe is **Online**, and lists its instances.
- `curl -s http://localhost:8383/api/v1/health` answers `{"status":"success","stale_monitors":0,"watchdog_ran_at":"..."}` within a minute or two. `watchdog_ran_at` is set once the leader has run the watchdog.
- Your monitors get new heartbeats at their usual intervals.

## If the probes are missing

If the upgraded cluster runs with no probe instance, no monitor of the local probe is checked, and Peekaping says so:

- The API logs this when it starts, and the leader logs it again when it reports the probe:

  ```text
  the local probe has 12 active monitors and no instance: deploy peekaping-probe with PROBE_TOKEN set to PROBE_BOOTSTRAP_TOKEN
  ```

- After `PROBE_OFFLINE_AFTER` (3 minutes by default), the leader reports the local probe offline once. The local probe's own notification channels get "Probe `Local` has been offline since `<time>`". Unless you set channels on the local probe, these are the notification channels of its monitors. The watchdog skips those monitors, so you get that one notification and not one for each monitor. Push monitors still work, because pushes arrive at the API.
- When an instance connects, the probe comes online and "Probe `Local` is online" goes out by the same rule, to the channels the probe and its monitors have at that moment.

To fix it, start probe instances with `PEEKAPING_URL` set to the API's address and `PROBE_TOKEN` set to the same value as the API's `PROBE_BOOTSTRAP_TOKEN`. If they are running and the probe is still offline, check these:

- The API has `PROBE_BOOTSTRAP_TOKEN`. Without it, it stores no token and the probes are refused.
- The probes' `PROBE_TOKEN` equals it, character for character. In Kubernetes, a changed Secret reaches the pods only when they restart: restart the API first, then the probes. See [The local probe's token](./kubernetes-helm.md#the-local-probes-token).
- `PEEKAPING_URL` reaches the API from the probe's network. See [What the probe needs to reach](./remote-probes.md#what-the-probe-needs-to-reach).

## Rolling back

Go back to the previous images with the previous compose file or chart revision. The upgrade changes nothing in the database, so you restore nothing.

- **Docker Compose.** Put back your earlier compose file and set each image to the tag you ran before, such as `0xfurai/peekaping-api:<previous>`. Run `docker compose up -d --remove-orphans`, which removes the `probe` container and starts the producer, worker and ingester. Their images are no longer published, but the tags of earlier releases stay on the registries.
- **Helm.** Run `helm rollback <release> <revision>`, with the revision from `helm history`. It brings back the earlier Deployments and removes the probe Deployment and the token Secret. It doesn't undo database migrations, and this upgrade has none.

The local probe's token stays in the database. It is harmless: it opens only the probe protocol, which no probe uses once the probe deployment is gone. You can leave `PROBE_BOOTSTRAP_TOKEN` in `.env`, because an older API either ignores it or stores the same token again.

If you came from a release that has no remote probes, the upgrade also ran the migrations that add them, and those do change the database. To go back to that release, restore your backup. See the [release notes](https://github.com/0xfurai/peekaping/blob/main/CHANGELOG.md).

## MongoDB storage was removed

MongoDB has no probes, and MongoDB storage was removed, so a MongoDB install has no producer, worker and ingester to retire and nothing to upgrade on this page. [Move to SQLite or PostgreSQL](./docker-with-mongo.md#move-to-sqlite-or-postgresql) first, and then set up the new layout from the start.
