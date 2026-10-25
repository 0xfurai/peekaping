---
sidebar_position: 4
---

# Bundle user and permissions

The bundle images (`peekaping-bundle-sqlite` and `peekaping-bundle-postgres`) run Peekaping in single mode, as a non-root user, `peekaping`, with UID and GID 1000. The one process, `peekaping`, runs as that user. A startup script runs as root, sets up the user, runs the migrations and starts `peekaping`. In the PostgreSQL bundle, PostgreSQL runs as its own `postgres` user, with UID 102 and GID 105 as in earlier bundles.

The binaries belong to root, so Peekaping can't change them.

## Choosing the user and group IDs

Set `PUID` and `PGID` to run Peekaping with other IDs, for example to match the owner of your data folder on the host:

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

At each start, the container makes that user the owner of `/app/data`, where the SQLite bundle keeps its database. Some volumes, such as NFS with `root_squash`, don't let root change owners. The container then logs a warning and starts anyway, so set `PUID` and `PGID` to the folder's owner on the host.

An ID that isn't a number, or that a user or group in the image already has (such as 0 for root), stops the container with an error.

In the PostgreSQL bundle, the database files belong to PostgreSQL's own user, so `PUID` and `PGID` don't change them.

## Starting the container with `--user`

The SQLite and PostgreSQL bundles also start with `docker run --user <uid>:<gid>`, or `user:` in Docker Compose. Everything then runs as that user, including PostgreSQL in the PostgreSQL bundle. The container skips the steps that need root, and logs that it did:

- `PUID` and `PGID` don't apply.
- The user doesn't get the Docker socket's group. Add it with `--group-add <the socket's GID>`.
- The container doesn't change the owner of the data folder, and doesn't check `net.ipv4.ping_group_range`.

The user must own the data folder: `/app/data` for SQLite, `/var/lib/postgresql/data` for PostgreSQL. If it can't write there, the container stops with an error. A data folder from an earlier start without `--user` belongs to `PUID`:`PGID` (1000:1000 by default) in the SQLite bundle, and to PostgreSQL's user, 102:105, in the PostgreSQL bundle. Start with those IDs, or give the folder to your user first.

## Docker monitors

Docker monitors that connect through the Docker socket need it mounted at the default path:

```bash
-v /var/run/docker.sock:/var/run/docker.sock
```

At start, the container adds the `peekaping` user to the socket's group. If that group can't read and write the socket, the container logs a warning. A socket mounted at another path isn't detected, and Docker monitors using it are Down. `--group-add` doesn't help, because Peekaping starts with only the groups its user has in the image. It does help under [`--user`](#starting-the-container-with---user).

Anything that can use the Docker socket controls the host. To give Peekaping read-only access instead, run [docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy) with `CONTAINERS=1`, since a Docker monitor inspects its container. Then set the Docker monitor's **Connection type** field to TCP, with a host such as `http://dockerproxy:2375`.

## Ping monitors

Ping monitors send ICMP echo requests. Peekaping doesn't run as root, so it opens an unprivileged ICMP socket. Linux allows that socket only for a process whose group, or one of its other groups, is in the `net.ipv4.ping_group_range` sysctl.

- **`net.ipv4.ping_group_range`** is what makes ping work. It lets those groups send and receive ICMP echo (ping) messages, and nothing else that needs root. Docker sets it to every group in each container with its own network, so nothing needs to change there.
- **`NET_RAW`** usually doesn't help. That capability lets a process open a raw ICMP socket, but Peekaping runs as `peekaping`, and most runtimes don't make a container's added capabilities effective for a non-root process. So `--cap-add NET_RAW` or `cap_add: [NET_RAW]` changes nothing for ping monitors there. If your runtime does make it effective for a non-root process, the raw socket works as a fallback when `ping_group_range` is unavailable.
- **The `ping` command**: when Peekaping can't open an ICMP socket, it runs the system `ping`. The SQLite bundle has BusyBox `ping`, which needs the same permission, so it doesn't get around a missing `ping_group_range`. The PostgreSQL bundle has no `ping`.

On a runtime that doesn't allow the group, the container logs a warning at start, and ping monitors are Down with `cannot open an ICMP socket`. This happens with rootless Docker, `userns-remap` and Podman, for example. Allow the range with:

```bash
--sysctl net.ipv4.ping_group_range="0 2147483647"
```

Or in Docker Compose:

```yaml
sysctls:
  net.ipv4.ping_group_range: "0 2147483647"
```

Rootless Podman rejects a range with groups outside its user namespace. Allow only Peekaping's group there: `--sysctl net.ipv4.ping_group_range="1000 1000"`, or your `PGID`.

With `--network host`, Docker rejects this option and the host's value applies. Set it on the host instead: `sysctl -w net.ipv4.ping_group_range="0 2147483647"`.
