---
sidebar_position: 3
---

# Kubernetes (Helm)

The Helm chart in the `charts/` folder of the [Peekaping repository](https://github.com/0xfurai/peekaping) runs Peekaping in [cluster mode](./deployment-modes.md#cluster-mode). The API, producer, worker and ingester run as separate deployments and talk through Redis. It needs a PostgreSQL database and a Redis.

The chart installs:

- Deployments for the API (`<release>-api`), the producer, the worker, the ingester and the web app (`<release>-web`). Each runs 3 replicas by default.
- A Job, `<release>-migrate`, that runs the database migrations.
- Services for the API (port 8034) and the web app (port 80).
- A ConfigMap, `<release>-config`, built from `config.env`.
- Optionally, an Ingress, or an Istio Gateway and VirtualService.

`<release>` is the name you give `helm install`.

:::note PostgreSQL only
The chart supports PostgreSQL only. SQLite doesn't work with it: the chart has no volume for the database file, and several pods would have to share it. MongoDB storage is [deprecated](./docker-with-mongo.md) and the chart doesn't support it. For SQLite, use the [single-mode bundle](./docker-with-sqlite.md).
:::

## Before you start

You need:

- A Kubernetes cluster and `kubectl` access to it.
- Helm 3.13 or later.
- A PostgreSQL database, with a user that can create tables in it. The migrate Job creates the tables, not the database.
- A Redis.
- Servers that accept connections without TLS. Peekaping connects to PostgreSQL with `sslmode=disable` and to Redis in plain text, so a server that requires TLS doesn't work.

## Install

The chart isn't published to a Helm repository, so install it from a checkout of the repository.

### 1. Get the chart and its dependencies

```bash
git clone https://github.com/0xfurai/peekaping.git
cd peekaping
helm dependency build charts
```

Run `helm dependency build` first. `Chart.yaml` lists a PostgreSQL chart and a Redis chart as optional dependencies, and Helm refuses to install or render the chart until both are in `charts/charts/`, even if you don't enable them.

### 2. Create the namespace and the config secret

The pods read their passwords from a Secret that you create. Its default name is `peekaping-config`, and it must be in the namespace you install into:

```bash
kubectl create namespace peekaping
kubectl create secret generic peekaping-config \
  --namespace peekaping \
  --from-literal=DB_PASS='your-database-password' \
  --from-literal=REDIS_PASSWORD='your-redis-password'
```

Leave out `REDIS_PASSWORD` if your Redis has no password. [Configuration](#configuration) explains how the secret and `config.env` work together.

### 3. Write a values file

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

[Ingress or Istio](#ingress-or-istio) covers the routing, and [Pin the image tags](#pin-the-image-tags) the versions.

### 4. Install

```bash
helm install peekaping charts \
  --namespace peekaping \
  --values peekaping-values.yaml
```

Check that the migrate Job finished and the pods are running:

```bash
kubectl get job,pods --namespace peekaping
kubectl logs --namespace peekaping job/peekaping-migrate
```

The Job is an ordinary resource of the release, not a Helm hook, so Helm doesn't wait for it before it creates the Deployments. Open `https://status.example.com` and create your admin account.

## Configuration

Every pod, including the migrate Job, gets its environment variables from two places:

- **`config.env`** is rendered into the ConfigMap `<release>-config`. Each key becomes an environment variable. The ConfigMap is plain text, so put no passwords in it.
- **`config.existingSecret`** names a Secret that you create in the release's namespace. Each of its keys becomes an environment variable too, so name the keys after the variables, such as `DB_PASS` and `REDIS_PASSWORD`. If a key is in both places, the secret's value wins. The chart never creates this Secret. If it doesn't exist, the pods can't start. Set `config.existingSecret` to `""` if you don't use one.

The database settings have no defaults. For PostgreSQL, set `DB_TYPE` (the chart's default is `postgres`), `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER` and `DB_PASS`. The other settings have defaults you may want to change:

| Variable | Default | Notes |
| --- | --- | --- |
| `CLIENT_URL` | `http://localhost:3000` | The address people open, such as `https://status.example.com`. Notifications link to it. |
| `MODE` | `dev` | Set `prod`. With `dev`, the API logs every request. |
| `TZ` | `UTC` | The server's time zone. |
| `REDIS_HOST` | `redis` | Your Redis host. |
| `REDIS_PORT` | `6379` | |
| `REDIS_USERNAME` | empty | A Redis ACL user. Leave it empty for password-only authentication. |
| `REDIS_PASSWORD` | empty | Put it in the secret. |
| `METRICS_ENABLED` | `false` | See [Metrics](#metrics). |

Leave `API_URL` unset. The web app then calls the API on its own address, which is where the Ingress sends `/api`. [API Server](../architecture/api-server.md#environment-variables) lists every variable.

Changing `config.env` and running `helm upgrade` restarts the pods. Changing the secret doesn't, because Kubernetes passes its values to a pod only when the pod starts. Restart the deployments after you change it:

```bash
kubectl rollout restart deployment --namespace peekaping \
  peekaping-api peekaping-producer peekaping-worker peekaping-ingester peekaping-web
```

## External PostgreSQL and Redis

Point the chart at a PostgreSQL and a Redis that run outside the release, such as managed services or your own. Put the hosts, ports, database name and user in `config.env`, and the passwords in the secret, as in the values file above.

The pods open their connections from inside the cluster, so make sure the cluster's network can reach both servers. Both must accept connections without TLS, so passwords and data cross that network unencrypted: keep both servers on a network you trust, such as inside the cluster or a private network, or reach them through an encrypted tunnel.

The chart also lists Bitnami's PostgreSQL and Redis charts as dependencies. They are off by default. `postgresql.enabled` and `redis.enabled` turn them on, and the other values under `postgresql` and `redis` are the Bitnami charts' own. If you enable them, point `config.env` at the services they create. For a release named `peekaping` in the namespace `peekaping`, those are `peekaping-postgresql.peekaping.svc.cluster.local` and `peekaping-redis-master.peekaping.svc.cluster.local`.

## Ingress or Istio

People open the web app and Peekaping's API at one address. Two paths, `/api` and `/socket.io`, go to the API Service. Every other path goes to the web Service. The chart sets this up for you with an Ingress or with Istio. The API's websocket uses `/socket.io`, so your ingress controller must pass WebSocket connections.

### Ingress

Set `ingress.enabled` to `true` and `ingress.host` to your host name. Also set `ingress.ingressClassName` to your ingress class. The Ingress then routes:

| Path | Goes to |
| --- | --- |
| `/api` | `<release>-api`, port 8034 |
| `/socket.io` | `<release>-api`, port 8034 |
| `/` | `<release>-web`, port 80 |

Other values:

- `ingress.annotations` sets annotations on the Ingress. `values.yaml` has examples for an AWS ALB.
- `ingress.tlsSecretName` is the Secret with your certificate. The Ingress has a `tls` section for `ingress.host` whether you set it or not.
- `ingress.namespace` is the namespace of the Ingress. It defaults to the release's.

### Istio

Set `istio.gateway.enabled` and `istio.virtualservice.enabled` to `true`. The chart then creates:

- A Gateway, `<release>-gateway`, for `ingress.host`. It listens on port 80 for HTTP (`istio.gateway.port` and `istio.gateway.protocol`), and selects the Istio ingress gateway pods by the label `istio: <istio.gateway.selector>`. The selector is `ingressgateway` by default.
- A VirtualService, `<release>-virtualservice`, that sends `/api/` and `/socket.io/` to the `<release>-api` Service and everything else to the `<release>-web` Service.

The Gateway and VirtualService match `ingress.host`, so set it even if you don't enable the Ingress. If your Istio ingress gateway is already reachable from outside, you are done.

If you also set `ingress.enabled`, the Ingress sends all paths to the Istio ingress gateway Service instead of to the API and web Services. Use that to put a load balancer in front of Istio. The Service is `istio.ingress.name` (`istio-ingressgateway` by default) on port `istio.ingress.port` (80). Set `ingress.namespace`, for example to `istio-system`, so the Ingress is in the namespace of that Service.

## Pin the image tags

Every image defaults to the tag `latest`, with `imagePullPolicy: IfNotPresent`. A node that has already pulled `latest` keeps using it after a new release, so pods on different nodes can run different versions. Pin every component to the same release in your values file:

```yaml
api:
  image:
    tag: "0.0.47"
producer:
  image:
    tag: "0.0.47"
worker:
  image:
    tag: "0.0.47"
ingester:
  image:
    tag: "0.0.47"
web:
  image:
    tag: "0.0.47"
migrate:
  image:
    tag: "0.0.47"
```

There is one set of values for each of the six components, `<component>.image.repository`, `<component>.image.tag` and `<component>.image.pullPolicy`. The repositories are `0xfurai/peekaping-api`, `-producer`, `-worker`, `-ingester`, `-web` and `-migrate`. There is no single value for all of them, so set all six tags. The web pods serve the web app of the image you set in `web.image`.

## Ping monitors

Ping monitors send ICMP echo requests from the worker. The worker pods run as a non-root user, which Linux allows to send ICMP echo only if its group is in the `net.ipv4.ping_group_range` sysctl. The chart sets it on the worker pods through `worker.sysctls`:

```yaml
worker:
  sysctls:
    - name: net.ipv4.ping_group_range
      value: "0 2147483647"
```

Kubernetes allows this sysctl as a safe one, so it needs no `NET_RAW` capability and no change to the cluster. Set `worker.sysctls` to `[]` to leave the node's default. See [Ping Monitors and ICMP Permissions](../architecture/worker.md#ping-monitors-and-icmp-permissions).

## Metrics

Peekaping can serve Prometheus metrics at `/metrics` on the API's port. Set `METRICS_ENABLED` in `config.env`:

```yaml
config:
  env:
    METRICS_ENABLED: "true"
```

The endpoint needs no credentials and lists your monitors' names and types. The Ingress and the Istio VirtualService don't route it: `/metrics` goes to the web app, like every path that isn't `/api` or `/socket.io`. Scrape the API Service from inside the cluster, for example `peekaping-api.peekaping.svc.cluster.local:8034/metrics`. See [Prometheus Metrics](../architecture/api-server.md#prometheus-metrics).

## Scale and resources

Each component has a `replicaCount`, which is 3 by default, and `resources` with requests and limits. Change them under the component's name, for example `worker.replicaCount` or `api.resources`.

## Upgrading

1. Back up your PostgreSQL database. If you upgrade from 0.0.46 or earlier, also read [Upgrading stats storage](./upgrading-stats.md).
2. Update your checkout of the repository, and set the new release in every `image.tag`.
3. Delete the migrate Job of the last install or upgrade, if it is still there. Kubernetes can't change a Job's pods once it exists, so `helm upgrade` fails on it for an hour after it finished:

   ```bash
   kubectl delete job --namespace peekaping peekaping-migrate --ignore-not-found
   ```

4. Upgrade the release:

   ```bash
   helm upgrade peekaping charts \
     --namespace peekaping \
     --values peekaping-values.yaml
   ```

The migrate Job runs the migrations of the new release. Check that it finished with `kubectl logs --namespace peekaping job/peekaping-migrate`. The Job is removed an hour after it finishes (`migrate.ttlSecondsAfterFinished`).

`helm rollback` returns the Kubernetes objects to an earlier revision, but it doesn't undo database migrations. If a migration has changed your database, restore the backup too.
