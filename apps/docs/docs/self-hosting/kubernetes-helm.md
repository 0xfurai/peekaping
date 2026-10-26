---
sidebar_position: 3
---

# Kubernetes (Helm)

The Helm chart runs Peekaping in [cluster mode](./deployment-modes.md#cluster-mode). The API and the probe run as separate deployments. The API replicas share the database and talk to each other through Redis, and the probe pods connect to the API and run the checks. It needs a PostgreSQL database and a Redis.

Every [release](https://github.com/0xfurai/peekaping/releases) publishes the chart at `oci://ghcr.io/0xfurai/charts/peekaping`. The chart version is the release, and every image defaults to that release, so a chart version installs one consistent version of Peekaping.

The chart installs:

- Deployments for the API (`<release>-api`), the probe (`<release>-probe`) and the web app (`<release>-web`). Each runs 3 replicas by default. The API pods start with an init container, `wait-for-migrations`, that holds them until the migrations are applied. The probe and web pods don't use the database and start right away.
- A Job, `<release>-migrate-<revision>`, that runs the database migrations. A release name longer than 44 characters is shortened in the Job's name, so that the name stays within Kubernetes' limit of 63 characters.
- Services for the API (port 8034) and the web app (port 80).
- A ConfigMap, `<release>-config`, built from `config.env`.
- A Secret, `<release>-probe-token`, with the local probe's token, unless you name a Secret of your own. See [The local probe's token](#the-local-probes-token).
- Optionally, an Ingress, or an Istio Gateway and VirtualService.

`<release>` is the name you give `helm install`. `<revision>` is the release revision the Job belongs to: 1 for the install, 2 for the first upgrade, and so on. `helm history` lists the revisions. A rollback applies the manifest of the revision it goes back to, so the Job it creates has that revision's number, not a new one.

:::note PostgreSQL only
The chart supports PostgreSQL only. SQLite doesn't work with it: the chart has no volume for the database file, and several pods would have to share it. So the chart refuses it: with `config.env.DB_TYPE` set to `sqlite` (in any case), `helm install`, `helm upgrade` and `helm template` fail with an error that points to the bundle. Helm can't read a `DB_TYPE` that you put in the config secret, so the chart doesn't refuse that one, but the `api` pods log a warning about SQLite in cluster mode when they start. Keep `DB_TYPE` out of the secret. MongoDB storage was removed, so the chart refuses it too: with `config.env.DB_TYPE` set to `mongo` or `mongodb` (in any case), `helm install`, `helm upgrade` and `helm template` fail with an error that says so and points to [the guide for moving to PostgreSQL](./docker-with-mongo.md#move-to-sqlite-or-postgresql). For SQLite, use the [single-mode bundle](./docker-with-sqlite.md).
:::

## Before you start

You need:

- A Kubernetes cluster and `kubectl` access to it.
- Helm 3.13 or later.
- A PostgreSQL database, with a user that can create tables in it. The migrate Job creates the tables, not the database.
- A Redis.
- Servers that accept connections without TLS. Peekaping connects to PostgreSQL with `sslmode=disable` and to Redis in plain text, so a server that requires TLS doesn't work.

## Install

You install the published chart with `--version`, the Peekaping release you want. The chart is published from release 0.0.48 on, so the examples use `0.0.48`. Use the latest release instead. You need no checkout of the repository and no `helm dependency build`: the chart carries its optional PostgreSQL and Redis charts. Until release 0.0.48 is out, [install from a checkout](#install-from-a-checkout).

### 1. Create the namespace and the config secret

The pods read their passwords from a Secret that you create. Its default name is `peekaping-config`, and it must be in the namespace you install into:

```bash
kubectl create namespace peekaping
kubectl create secret generic peekaping-config \
  --namespace peekaping \
  --from-literal=DB_PASS='your-database-password' \
  --from-literal=REDIS_PASSWORD='your-redis-password'
```

Leave out `REDIS_PASSWORD` if your Redis has no password. [Configuration](#configuration) explains how the secret and `config.env` work together.

### 2. Write a values file

Create `peekaping-values.yaml` with the connection to your PostgreSQL and Redis, and the address people will open:

```yaml
config:
  existingSecret: peekaping-config
  env:
    DB_TYPE: postgres
    DB_HOST: postgres.example.com
    DB_PORT: "5432"
    DB_NAME: peekaping
    DB_USER: peekaping
    REDIS_HOST: redis.example.com
    REDIS_PORT: "6379"
    CLIENT_URL: https://status.example.com
    MODE: prod
    TZ: UTC

ingress:
  enabled: true
  ingressClassName: nginx
  host: status.example.com
  # A TLS secret for status.example.com in the ingress's namespace, for
  # example one cert-manager issues. Without it, the ingress controller
  # serves its default certificate.
  tlsSecretName: status-example-com-tls
```

[Ingress or Istio](#ingress-or-istio) covers the routing, and [Image versions](#image-versions) the images. You set no image tags: the chart version picks them.

### 3. Install

```bash
helm install peekaping oci://ghcr.io/0xfurai/charts/peekaping \
  --version 0.0.48 \
  --namespace peekaping \
  --values peekaping-values.yaml
```

Check that the migrate Job finished and the pods are running:

```bash
kubectl get job,pods --namespace peekaping
kubectl logs --namespace peekaping --selector app.kubernetes.io/instance=peekaping,app.kubernetes.io/component=migrate --prefix --tail=-1
```

The selector finds the migrate Job's pod whatever the revision, `--prefix` names the pod on each line, and `--tail=-1` prints the whole log.

The Job is an ordinary resource of the release, not a Helm hook, so Helm creates it and the Deployments at the same time. The API pods wait in `Init` until the Job has applied every migration, and then start. The web pods and the probe pods don't use the database and start right away. A probe pod retries until the API answers, and is ready once it has its first assignment.

The `wait-for-migrations` init container asks the migrate image every 2 seconds whether any migrations are left to apply. Its log shows what the pod is waiting for:

```bash
kubectl logs --namespace peekaping deploy/peekaping-api --container wait-for-migrations
```

If the Job fails, the pods stay in `Init`. Read the Job's log to find out why.

Open `https://status.example.com` and create your admin account.

### Install from a checkout

To try changes that aren't released yet, install from a checkout of the repository instead. Its images are `latest`, not a release (see [Image versions](#image-versions)).

```bash
git clone https://github.com/0xfurai/peekaping.git
cd peekaping
helm dependency build charts
helm install peekaping charts \
  --namespace peekaping \
  --values peekaping-values.yaml
```

Run `helm dependency build` first. `Chart.yaml` lists a PostgreSQL chart and a Redis chart as optional dependencies, and Helm refuses to install or render the chart from a folder until both are in `charts/charts/`, even if you don't enable them.

## Configuration

Every pod, including the migrate Job, gets its environment variables from two places:

- **`config.env`** is rendered into the ConfigMap `<release>-config`. Each key becomes an environment variable. The ConfigMap is plain text, so put no passwords in it.
- **`config.existingSecret`** names a Secret that you create in the release's namespace. Each of its keys becomes an environment variable too, so name the keys after the variables, such as `DB_PASS` and `REDIS_PASSWORD`. If a key is in both places, the secret's value wins. The chart checks `config.env.DB_TYPE` only, so keep `DB_TYPE` out of the secret: a `DB_TYPE` key there replaces the ConfigMap's value and passes the check for `sqlite`, `mongo` and `mongodb`. The chart never creates this Secret. If it doesn't exist, the pods can't start. Set `config.existingSecret` to `""` if you don't use one.

The database settings have no defaults. For PostgreSQL, set `DB_TYPE` (the chart's default is `postgres`), `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER` and `DB_PASS`. The other settings have defaults you may want to change:

| Variable | Default | Notes |
| --- | --- | --- |
| `CLIENT_URL` | `http://localhost:3000` | The address people open, such as `https://status.example.com`. Notifications link to it. |
| `CORS_ORIGINS` | the origin of `CLIENT_URL` | Origins whose pages may read the API's responses, comma-separated. The web app calls the API on its own address, which needs no entry. Set it only for a page on another origin. `*` allows any origin. |
| `MODE` | `dev` | Set `prod`. With `dev`, the API logs every request. |
| `TZ` | `UTC` | The server's time zone. |
| `REDIS_HOST` | `redis` | Your Redis host. |
| `REDIS_PORT` | `6379` | |
| `REDIS_USERNAME` | empty | A Redis ACL user. Leave it empty for password-only authentication. |
| `REDIS_PASSWORD` | empty | Put it in the secret. |
| `METRICS_ENABLED` | `false` | See [Metrics](#metrics). |
| `PROBE_OFFLINE_AFTER` | `3m` | How long the local probe's pods, or a remote probe's instances, may go unheard before the probe counts as offline. From `2m` to `24h`. See [When a probe is offline](./remote-probes.md#when-a-probe-is-offline). |
| `NOTIFICATION_HISTORY_RETENTION_DAYS` | `90` | How many days the hourly cleanup keeps notification history. At least `30`. See [Cleanup Configuration](../architecture/api-server.md#cleanup-configuration). |
| `TLS_INFO_RETENTION_DAYS` | `30` | How many days the hourly cleanup keeps monitors' TLS information. At least `1`. |
| `TRUSTED_PROXIES` | loopback and the private ranges | The addresses the API believes `X-Forwarded-For` from. See [Client addresses](#client-addresses). |
| `TELEMETRY` | empty | Set `off` to switch off the anonymous [usage statistics](./telemetry.md) whatever an admin chose. |
| `ERROR_REPORTING` | empty | Set `off` to switch off the [error reports](./telemetry.md#error-reporting) whatever an admin chose. |

Leave `API_URL` unset. The web app then calls the API on its own address, which is where the Ingress sends `/api`. [API Server](../architecture/api-server.md#environment-variables) lists every variable.

Changing `config.env` and running `helm upgrade` restarts the pods. Changing the secret doesn't, because Kubernetes passes its values to a pod only when the pod starts. Restart the deployments after you change it:

```bash
kubectl rollout restart deployment --namespace peekaping \
  peekaping-api peekaping-probe peekaping-web
```

The probe pods read the ConfigMap only, for `LOG_LEVEL` and `TZ`. They get no database or Redis settings.

### The local probe's token

The probe pods check the local probe's monitors, and they prove they are the local probe with a token. The API stores the token at startup (`PROBE_BOOTSTRAP_TOKEN`), and the probe pods send the same value (`PROBE_TOKEN`). It gives access to the config of the monitors on the local probe, such as the credentials in them, so keep it secret.

The probe pods call the API over plain HTTP inside the cluster, at `http://<release>-api.<namespace>.svc:8034`, and the chart sets `PROBE_ALLOW_INSECURE=true` for that. So the token crosses the cluster network unencrypted. Use the chart only on a cluster network you trust and keep isolated: limit who can reach the API pods, for example with a NetworkPolicy. If you can't trust the network between pods, encrypt it, for example with a service mesh that uses mutual TLS. The chart has no setting for an `https://` address between the probe pods and the API.

By default the chart makes a Secret named `<release>-probe-token`, with a random token of 48 characters, and reads it back on every upgrade so the token stays the same. Reading it back needs a render that can look at the cluster, which `helm install` and `helm upgrade` do. `helm template`, `helm install --dry-run` and tools that render without the cluster, such as Argo CD, make a new token each time. For those, make the Secret yourself, with a token of at least 32 characters, and name it in your values file:

```bash
kubectl create secret generic peekaping-probe-token \
  --namespace peekaping \
  --from-literal=PROBE_BOOTSTRAP_TOKEN="$(openssl rand -hex 32)"
```

```yaml
probe:
  token:
    existingSecret: peekaping-probe-token
    # The key in the Secret. This is the default.
    existingSecretKey: PROBE_BOOTSTRAP_TOKEN
```

The API and the probe pods read the token from the Secret when they start. Changing the Secret doesn't change the value a running pod holds, and the chart doesn't restart the pods for you. The API stores the token when it starts, and a probe is accepted only if it sends the stored one. So the old token stays active until the API restarts: the running probe pods keep working, and a probe pod that restarts first is refused.

To change the token, update the Secret, then restart the API, wait until every API pod runs with the new token, and restart the probe pods:

```bash
kubectl rollout restart deployment/peekaping-api --namespace peekaping
kubectl rollout status deployment/peekaping-api --namespace peekaping
kubectl rollout restart deployment/peekaping-probe --namespace peekaping
kubectl rollout status deployment/peekaping-probe --namespace peekaping
```

Between the two restarts, the old probe pods send a token the API no longer holds, so they are refused and the local probe stops being checked. The new probe pods are accepted as soon as they connect, and become ready with their first assignment. If the gap lasts longer than `PROBE_OFFLINE_AFTER` (3 minutes by default), the leader reports the local probe offline, and it comes back online when a new pod connects. See [When a probe is offline](./remote-probes.md#when-a-probe-is-offline).

### Client addresses

The login lockout counts failed logins per client address, and a client can send any `X-Forwarded-For` header it likes. So the API believes that header (and `X-Real-IP`) only from the addresses in `TRUSTED_PROXIES`, and takes the last address in it that isn't one of them. The chart sets `TRUSTED_PROXIES` to loopback and the private ranges (`127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7`), which is where an Ingress or gateway inside the cluster reaches the API from. The API Service has no address outside the cluster.

Your Ingress or gateway must add the client's address to the end of `X-Forwarded-For`, or replace the header with it. Most do; check yours. If it passes the client's `X-Forwarded-For` through unchanged, set `TRUSTED_PROXIES` to `""` in `config.env`, which believes no header, because otherwise a client could send its own. If its pods are outside the private ranges (some clusters use 100.64.0.0/10 for pods), add their addresses to the list instead. With `""`, every login counts as coming from the Ingress, so one person's failed logins can lock an account for everyone. The default ranges are wide: narrowing `TRUSTED_PROXIES` to your ingress controller's pod CIDR is safer, because a client with a private address behind an Ingress that appends to `X-Forwarded-For` would be skipped as a trusted proxy and could pick its own address from the entries to its left. The account counter still bounds guessing either way. Whatever you set, the API also locks an account after `BRUTEFORCE_ACCOUNT_MAX_ATTEMPTS` failed logins (100 by default) from any addresses within `BRUTEFORCE_ACCOUNT_WINDOW` (10 minutes). A lock lasts `BRUTEFORCE_ACCOUNT_LOCKOUT` (1 minute), but the count of failures is kept until the window, measured from the first failure, has passed, so until then each further failure after a lock ends locks the account again. After an attack stops, the owner is back in within the window at most. An attacker who keeps going can keep the account locked; the counter bounds how many passwords they can try.

## External PostgreSQL and Redis

Point the chart at a PostgreSQL and a Redis that run outside the release, such as managed services or your own. Put the hosts, ports, database name and user in `config.env`, and the passwords in the secret, as in the values file above.

The pods open their connections from inside the cluster, so make sure the cluster's network can reach both servers. Both must accept connections without TLS, so passwords and data cross that network unencrypted: keep both servers on a network you trust, such as inside the cluster or a private network, or reach them through an encrypted tunnel.

The chart also lists Bitnami's PostgreSQL and Redis charts as dependencies. They are off by default. `postgresql.enabled` and `redis.enabled` turn them on, and the other values under `postgresql` and `redis` are the Bitnami charts' own. If you enable them, point `config.env` at the services they create. For a release named `peekaping` in the namespace `peekaping`, those are `peekaping-postgresql.peekaping.svc.cluster.local` and `peekaping-redis-master.peekaping.svc.cluster.local`. [Bundled PostgreSQL and Redis images](#bundled-postgresql-and-redis-images) says which images they run.

## Bundled PostgreSQL and Redis images

The bundled PostgreSQL and Redis run Bitnami's free `latest` images, `bitnami/postgresql` and `bitnami/redis` on Docker Hub. Bitnami's free repository has no version tags: the tagged images moved to `bitnamilegacy/`, which gets no updates. `latest` floats. When it turns into a new major version, a pod that Kubernetes reschedules would start it on the data of the old one, and PostgreSQL refuses to.

So the chart pins each image by digest, in `postgresql.image.digest` and `redis.image.digest` in `values.yaml`. They are PostgreSQL 18 and Redis 8, and each digest covers amd64 and arm64. A digest wins over the tag.

The digests and the subchart versions in `Chart.yaml` are updated by hand, around each release. With the published chart, a newer chart version carries newer ones, so upgrade to it.

To run an image of your own instead, set `postgresql.image.digest` and `redis.image.digest` in your values file. A digest from another repository also needs `image.registry` and `image.repository`. Without them the pod pulls the digest from Bitnami's repository, `registry-1.docker.io/bitnami/postgresql`, and the pull fails. Bitnami's charts also refuse an image they don't recognize, so set `global.security.allowInsecureImages` to `true`:

```yaml
global:
  security:
    allowInsecureImages: true
postgresql:
  image:
    registry: registry.example.com
    repository: mirror/postgresql
    digest: sha256:<the digest of your image>
```

`redis.image` takes the same three values.

In a checkout of the repository, you can also move to newer digests and subchart versions yourself. Run this, then `helm dependency build charts` and `helm upgrade`:

```bash
scripts/update-chart-deps.sh           # lists what is behind
scripts/update-chart-deps.sh --write   # updates Chart.yaml and values.yaml
```

The script needs `helm`, `curl` and `jq`. It is not part of the published chart.

A new digest or subchart version restarts the bundled PostgreSQL. The migrate Job retries until the database is back, up to `migrate.backoffLimit` tries (6 by default). If it still fails, run `helm upgrade` again. Your data is intact.

`update-chart-deps.sh` refuses a new major version of a subchart or an image, because a major version can change the values and the data layout. Move to one by hand. For PostgreSQL that needs a dump and restore: PostgreSQL doesn't start on the data of an older major version. Dump the database with `pg_dump`, delete the PersistentVolumeClaim `data-<release>-postgresql-0` (`helm uninstall` keeps it), install the new version, and restore the dump.

## Ingress or Istio

People open the web app and Peekaping's API at one address. Three paths, `/api`, `/probe` and `/socket.io`, go to the API Service. Every other path goes to the web Service. The chart sets this up for you with an Ingress or with Istio. The API's websocket uses `/socket.io`, so your ingress controller must pass WebSocket connections.

### Ingress

Set `ingress.enabled` to `true` and `ingress.host` to your host name. Also set `ingress.ingressClassName` to your ingress class. The Ingress then routes:

| Path | Goes to |
| --- | --- |
| `/api` | `<release>-api`, port 8034 |
| `/probe` | `<release>-api`, port 8034 |
| `/socket.io` | `<release>-api`, port 8034 |
| `/` | `<release>-web`, port 80 |

Other values:

- `ingress.annotations` sets annotations on the Ingress. `values.yaml` has examples for an AWS ALB. With ingress-nginx, set `nginx.ingress.kubernetes.io/proxy-body-size: "2m"` here if you use [remote probes](./remote-probes.md): ingress-nginx refuses request bodies over 1 MiB by default, and a probe's batch of results can be a little over that.
- `ingress.tlsSecretName` is the Secret with your certificate. The Ingress has a `tls` section for `ingress.host` whether you set it or not.
- `ingress.namespace` is the namespace of the Ingress. It defaults to the release's.

### Istio

Set `istio.gateway.enabled` and `istio.virtualservice.enabled` to `true`. The chart then creates:

- A Gateway, `<release>-gateway`, for `ingress.host`. It listens on port 80 for HTTP (`istio.gateway.port` and `istio.gateway.protocol`), and selects the Istio ingress gateway pods by the label `istio: <istio.gateway.selector>`. The selector is `ingressgateway` by default.
- A VirtualService, `<release>-virtualservice`, that sends `/api/`, `/probe/` and `/socket.io/` to the `<release>-api` Service and everything else to the `<release>-web` Service.

The Gateway and VirtualService match `ingress.host`, so set it even if you don't enable the Ingress. If your Istio ingress gateway is already reachable from outside, you are done.

If you also set `ingress.enabled`, the Ingress sends all paths to the Istio ingress gateway Service instead of to the API and web Services. Use that to put a load balancer in front of Istio. The Service is `istio.ingress.name` (`istio-ingressgateway` by default) on port `istio.ingress.port` (80). Set `ingress.namespace`, for example to `istio-system`, so the Ingress is in the namespace of that Service.

## Image versions

The chart version pins every image. Chart `0.0.48` runs the `0.0.48` api, probe, web and migrate images, and the init containers that follow them. The `wait-for-migrations` init containers run the migrate image, and `seed-web-root` runs the web image. So every pod runs one release, on any node, even with `imagePullPolicy: IfNotPresent`.

There is one set of values for each of the four components, `<component>.image.repository`, `<component>.image.tag` and `<component>.image.pullPolicy`. The repositories are `0xfurai/peekaping-api`, `-probe`, `-web` and `-migrate`. An empty `tag`, the default, means the chart's `appVersion`: the release in a published chart, `latest` in a checkout.

Set a component's `image.tag` only to run another build of that component, such as a release candidate:

```yaml
api:
  image:
    tag: "0.0.49-rc.1"
```

A component's `image.tag` changes that component's container only. `web.image.tag` also sets `seed-web-root`, the init container of the web pods. `migrate.image.tag` sets the migrate Job and the `wait-for-migrations` init containers of the API pods, because they run the migrate image. Everything else stays on the chart's `appVersion`.

A checkout of the repository has the chart version `0.0.0` and the app version `latest`, so its images are `latest`. A node that has already pulled `latest` keeps using it after a new release, so pods on different nodes can run different versions. When you install from a checkout, set `image.tag` on all four components to the same release.

## Ping monitors

Ping monitors send ICMP echo requests from the probe. The probe pods run as a non-root user, which Linux allows to send ICMP echo only if its group is in the `net.ipv4.ping_group_range` sysctl. The chart sets it on the probe pods through `probe.sysctls`:

```yaml
probe:
  sysctls:
    - name: net.ipv4.ping_group_range
      value: "0 2147483647"
```

Kubernetes allows this sysctl as a safe one, so it needs no `NET_RAW` capability and no change to the cluster. Set `probe.sysctls` to `[]` to leave the node's default. See [Ping Monitors and ICMP Permissions](../architecture/probe.md#ping-monitors-and-icmp-permissions).

## Web pods and port 80

The web image runs nginx as a non-root user, which Linux lets bind port 80 only if the pod's `net.ipv4.ip_unprivileged_port_start` sysctl is 0. Some container runtimes set it by default and some don't, so the chart sets it on the web pods through `web.sysctls`:

```yaml
web:
  sysctls:
    - name: net.ipv4.ip_unprivileged_port_start
      value: "0"
```

Kubernetes allows this sysctl as a safe one, so it needs no change to the cluster. Set `web.sysctls` to `[]` to leave the node's default, for example if your policy forbids pod sysctls and your nodes already allow port 80. With a read-only root filesystem, the web pods mount an `emptyDir` at `/tmp`, where nginx keeps its process ID file and temporary files, and one at `/usr/share/nginx/html`, where the container writes `env.js` at start. See [Cluster user and permissions](./cluster-user.md).

## Metrics

Peekaping can serve Prometheus metrics at `/metrics` on the API's port. Set `METRICS_ENABLED` in `config.env`:

```yaml
config:
  env:
    METRICS_ENABLED: "true"
```

The endpoint needs no credentials and lists your monitors' names and types. The Ingress and the Istio VirtualService don't route it: `/metrics` goes to the web app, like every path that isn't `/api` or `/socket.io`. Scrape the API Service from inside the cluster, for example `peekaping-api.peekaping.svc.cluster.local:8034/metrics`. See [Prometheus Metrics](../architecture/api-server.md#prometheus-metrics).

## Scale and resources

Each component has a `replicaCount`, which is 3 by default, and `resources` with requests and limits. Change them under the component's name, for example `probe.replicaCount` or `api.resources`. The API replicas share one database and one leader, and the probe pods share the local probe's monitors, so add probe replicas to check more monitors. A pod that stops hands its monitors to the others.

## Upgrading

1. Back up your PostgreSQL database. If you upgrade from 0.0.46 or earlier, also read [Upgrading stats storage](./upgrading-stats.md). If the release you run still has `producer`, `worker` and `ingester` values, also read [Upgrading to probes](./upgrading-to-probes.md): your values file needs a few changes.
2. Remove any `image.tag` from your values file that pins an old release. A tag you set overrides the chart version, so that image stays on the old release after the upgrade.
3. Upgrade the release to the new version:

   ```bash
   helm upgrade peekaping oci://ghcr.io/0xfurai/charts/peekaping \
     --version 0.0.49 \
     --namespace peekaping \
     --values peekaping-values.yaml
   ```

   A release you installed from a checkout upgrades to the published chart the same way. To keep upgrading from a checkout instead, update it, run `helm dependency build charts`, and use `charts` in place of the chart address and `--version`.

The upgrade creates a new migrate Job, `peekaping-migrate-<revision>`, that runs the migrations of the new release, and Helm removes the previous one. Every install, upgrade and rollback creates a Job, so there is nothing to delete by hand. The new API pods wait in `Init` until the Job has applied the migrations, and the old pods keep serving until then. Check that it finished with `kubectl logs --namespace peekaping --selector app.kubernetes.io/instance=peekaping,app.kubernetes.io/component=migrate --prefix --tail=-1`. The Job is removed `migrate.ttlSecondsAfterFinished` seconds after it finishes, an hour by default.

`helm rollback` returns the Kubernetes objects to an earlier revision, but it doesn't undo database migrations. It applies the manifest of the revision you roll back to, so it creates that revision's migrate Job again: rolling back to revision 1 creates `peekaping-migrate-1`, not a Job with a new number. If a migration has changed your database, restore the backup too.
