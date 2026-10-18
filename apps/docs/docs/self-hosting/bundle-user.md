---
sidebar_position: 4
---

# Bundle user and permissions

The bundle images (`peekaping-bundle-sqlite`, `peekaping-bundle-mongo` and `peekaping-bundle-postgres`) run Peekaping's services (api, producer, worker and ingester) as a non-root user, `peekaping`, with UID and GID 1000. Caddy, Redis, the database and supervisord run as before. The binaries belong to root, so the services can't change them.

## Choosing the user and group IDs

Set `PUID` and `PGID` to run the services with other IDs, for example to match the owner of your data folder on the host:

```bash
docker run -d --restart=always \
  -p 8383:8383 \
  -e DB_NAME=/app/data/peekaping.db \
  -e PUID=1001 \
  -e PGID=1001 \
  -v $(pwd)/.data/sqlite:/app/data \
  --name peekaping \
  0xfurai/peekaping-bundle-sqlite:latest
```

At each start, the container makes that user the owner of `/app/data`, where the SQLite bundle keeps its database. An ID that isn't a number, or that a user or group in the image already has (such as 0 for root), stops the container with an error.

In the MongoDB and PostgreSQL bundles, the database files belong to the database's own user, so `PUID` and `PGID` don't change them.

## Docker monitors

Docker monitors that connect through the Docker socket need it mounted at the default path:

```bash
-v /var/run/docker.sock:/var/run/docker.sock
```

At start, the container adds the `peekaping` user to the socket's group. A socket mounted at another path isn't detected, and Docker monitors using it go down. `--group-add` doesn't help, because supervisord sets the services' groups itself.

Anything that can use the Docker socket controls the host. To give Peekaping read-only access instead, run [docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy) and pick the TCP connection type in the monitor, with a host such as `http://dockerproxy:2375`.

## Ping monitors

Ping monitors open unprivileged ICMP sockets, which Linux allows only for groups in the `net.ipv4.ping_group_range` sysctl. Docker allows every group by default, so nothing needs to change there. On a runtime that doesn't, the container logs a warning at start and ping monitors fail. Allow the range with:

```bash
--sysctl net.ipv4.ping_group_range="0 2147483647"
```

Or in Docker Compose:

```yaml
sysctls:
  net.ipv4.ping_group_range: "0 2147483647"
```

Adding the `NET_RAW` capability doesn't help, because the services don't run as root.
