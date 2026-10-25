---
sidebar_position: 4
---

# Cluster user and permissions

The images of [cluster mode](./deployment-modes.md#cluster-mode) run as a non-root user:

- **`peekaping-api`, `-probe` and `-migrate`** run as user and group 65532 (`65532:65532`). The binaries belong to root, so Peekaping can't change them.
- **`peekaping-web`** runs nginx as the user `nginx`, UID 101, on top of [`nginxinc/nginx-unprivileged`](https://hub.docker.com/r/nginxinc/nginx-unprivileged). It listens on port 80 as before.

The bundle images run as another user. See [Bundle user and permissions](./bundle-user.md). The Helm chart sets its own user, 10001, on every pod.

## The SQLite data folder

With SQLite, the migrate and API containers share one data folder, `./.data/sqlite` on the host. The probe has no database and needs no volume. SQLite creates the database and its `-wal` and `-shm` files in that folder, so the user 65532 must be able to write to it. Docker creates a missing `./.data/sqlite` as root, and a database that an earlier version created belongs to root too.

The [SQLite compose file](./docker-with-sqlite.md#docker-composeyml-file) has a one-shot service, `data-permissions`, that gives the folder to 65532 each time you start the stack. `migrate` waits for it, and the other services wait for `migrate`. Without it, `migrate` stops with `unable to open database file`.

```yaml
  data-permissions:
    image: alpine
    restart: "no"
    command: ["chown", "-R", "65532:65532", "/app/data"]
    volumes:
      - ./.data/sqlite:/app/data

  migrate:
    # ...
    depends_on:
      data-permissions:
        condition: service_completed_successfully
```

If you run SQLite from your own compose file, do one of these when you upgrade from a release whose images ran as root:

- Add the service and the `depends_on` above. If you keep the data in a named volume, mount that volume in `data-permissions`.
- Or give the folder to 65532 once, on the host: `sudo chown -R 65532:65532 .data/sqlite`.

PostgreSQL and Redis keep their data in their own containers, so they need nothing.

## Docker monitors

In cluster mode, the probe checks Docker monitors. The probe needs the Docker socket and a group that can use it. Mount the socket, and add the socket's group to the probe with `group_add`:

```yaml
  probe:
    # ...
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    group_add:
      - "999" # the group ID of /var/run/docker.sock on the Docker host
```

On a Linux host, the group ID is the one of the socket, usually the `docker` group:

```bash
stat -c %g /var/run/docker.sock
```

On Docker Desktop for Mac, the socket inside a container belongs to `root:root` with mode 660, so the group to add is `"0"`.

In the monitor, leave **Connection type** at socket, with the path `/var/run/docker.sock`. Without the mount, the monitor is Down with `Cannot connect to the Docker daemon`. With the mount and no group, it is Down with `permission denied while trying to connect to the Docker daemon socket`.

Anything that can use the Docker socket controls the host. To give the probe read-only access instead, run [docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy) with `CONTAINERS=1`, since a Docker monitor inspects its container. Then set the monitor's **Connection type** to TCP, with a host such as `http://dockerproxy:2375`.

## Ping monitors

Ping monitors send ICMP echo requests from the probe. The probe runs as group 65532, which Linux allows to send ICMP echo only if the group is in the `net.ipv4.ping_group_range` sysctl. Rootful Docker sets it for every group in each container with its own network namespace, so ping monitors work in the compose files without changes there. See [Ping Monitors and ICMP Permissions](../architecture/probe.md#ping-monitors-and-icmp-permissions) for rootless Docker, Podman and Kubernetes.

## Port 80 of the web image

The web image binds port 80 without root. Docker allows that: it sets `net.ipv4.ip_unprivileged_port_start` to 0 in each container. If the `web` container stops with `bind() to 0.0.0.0:80 failed (13: Permission denied)`, your container runtime doesn't. Allow the port for that service:

```yaml
  web:
    # ...
    sysctls:
      net.ipv4.ip_unprivileged_port_start: "0"
```

The Helm chart sets this sysctl on the web pods (`web.sysctls`). See [Kubernetes (Helm)](./kubernetes-helm.md#web-pods-and-port-80).
