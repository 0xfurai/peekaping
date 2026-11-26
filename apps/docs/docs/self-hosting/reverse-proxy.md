---
sidebar_position: 2
---

# Reverse proxy

The `0xfurai/peekaping` image runs Peekaping in [single mode](./deployment-modes.md): one process serves the web UI, the HTTP API and live Socket.IO updates as plain HTTP on port **8383**. Traefik or Caddy in front of that port terminates HTTPS and forwards every request for your hostname to it.

This page has ready-to-use recipes for both. The recipes use SQLite, with the database in `/app/data`, mounted from `./.data/sqlite` on the host, as in [Docker + SQLite](./docker-with-sqlite.md). For PostgreSQL, add the `DB_*` settings from [Docker + PostgreSQL](./docker-with-postgres.md#single-mode); the proxy part is the same.

`peekaping` runs as a non-root user and owns `/app/data`. [Single-mode user and permissions](./single-user.md) covers `PUID` and `PGID` when that directory needs a matching owner.

## What the proxy forwards

Peekaping serves everything on one port, so the proxy needs no path rules:

- `/api/*` is the API.
- `/socket.io/*` is the websocket for live updates. The UI uses the websocket transport only.
- Every other path is the single-page app. Reloading a nested route such as `/monitors` returns the app (`index.html`).

Forward the whole hostname, including the WebSocket upgrade. Traefik and Caddy pass `Upgrade` and `Connection` through on their own. One router, or one Caddy site block, is the whole configuration.

Two settings matter behind a proxy:

- **`CLIENT_URL`** is the public address, such as `https://peekaping.example.com`. Links in notifications point to it.
- **`TRUSTED_PROXIES`** is the address the proxy connects from. Peekaping reads the client's address from `X-Forwarded-For` only on requests from an address in this list. When it is empty, every login counts as coming from the proxy, and one person's failed logins can lock an account for everyone. Each recipe below says what to set. [Reverse proxy and HTTPS](./docker-with-sqlite.md#reverse-proxy-and-https) has the full rules, including a second proxy such as Cloudflare in front of yours.

The recipes are for single mode. In [cluster mode](./docker-with-sqlite.md#cluster-mode), the Compose file has its own nginx gateway, published on port 8383, and an outer proxy forwards the hostname to that port.

## Traefik

Use this with a Traefik installation that is already running. The Compose file only adds Peekaping. Traefik reaches it on the shared Docker network at port 8383, so Peekaping publishes no host port.

### Prerequisites

Your existing Traefik provides:

- An HTTPS entrypoint named `websecure` (the usual listener on port 443). The label key below is `entrypoints`.
- A certificate resolver in Traefik's static configuration.
- A Docker network the Traefik container is attached to.
- Public DNS for the hostname pointing at the Traefik host, plus the ports that resolver's challenge already uses (often 80 and 443).

### Hostname and network

`PEEKAPING_HOSTNAME` is the name in the Host rule, in DNS, and in the browser. `TRAEFIK_NETWORK` is the Docker network Traefik already uses; the commands below use the name `traefik`. Create that network only when it is missing, then connect the running Traefik container:

```bash
docker network create traefik
docker network connect traefik traefik
```

The last argument of `docker network connect` is the Traefik container name from `docker ps`. Skip both commands when Traefik is already on the network.

Read the network's address range. Traefik connects to Peekaping from an address in it, so it is the value of `TRUSTED_PROXIES`:

```bash
docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' traefik
```

Put the values in a `.env` file in the same directory as the Compose file:

```env
PEEKAPING_HOSTNAME=peekaping.example.com
TRAEFIK_NETWORK=traefik
TRAEFIK_CERT_RESOLVER=letsencrypt
# The range the command above printed
TRUSTED_PROXIES=172.18.0.0/16
```

### Compose file

```yml
# PEEKAPING_HOSTNAME: public DNS name served by this router
# TRAEFIK_NETWORK: Docker network the existing Traefik container already uses
# TRAEFIK_CERT_RESOLVER: certificate resolver name from Traefik's static configuration
# TRUSTED_PROXIES: address range of TRAEFIK_NETWORK

services:
  peekaping:
    image: 0xfurai/peekaping:latest
    restart: unless-stopped
    # peekaping takes up to 30 seconds to shut down cleanly
    stop_grace_period: 35s
    environment:
      - CLIENT_URL=https://${PEEKAPING_HOSTNAME}
      - TRUSTED_PROXIES=${TRUSTED_PROXIES-}
    volumes:
      - ./.data/sqlite:/app/data
    networks:
      - traefik
    labels:
      - "traefik.enable=true"
      - "traefik.docker.network=${TRAEFIK_NETWORK}"
      - "traefik.http.routers.peekaping.rule=Host(`${PEEKAPING_HOSTNAME}`)"
      - "traefik.http.routers.peekaping.entrypoints=websecure"
      - "traefik.http.routers.peekaping.tls=true"
      # A router-level TLS section does not inherit the entrypoint's
      # certResolver, so name it here. Use the resolver name from Traefik's
      # static configuration.
      - "traefik.http.routers.peekaping.tls.certresolver=${TRAEFIK_CERT_RESOLVER}"
      - "traefik.http.routers.peekaping.service=peekaping"
      - "traefik.http.services.peekaping.loadbalancer.server.port=8383"

networks:
  traefik:
    external: true
    name: ${TRAEFIK_NETWORK}
```

There is one router. Its name, `peekaping`, is the same on the rule, the entrypoint, TLS, and the service label. `tls.certresolver` must name a resolver defined in Traefik's static configuration; without it the router can serve Traefik's default self-signed certificate. The service `peekaping` uses port 8383. `traefik.docker.network` and the external network's `name` are both `TRAEFIK_NETWORK`, so Traefik selects that shared network.

`./.data/sqlite` on the host is the persistent copy of `/app/data` (`/app/data/peekaping.db` inside the container).

From the directory that contains the file:

```bash
docker compose up -d
```

Open `https://peekaping.example.com`, using the hostname you set.

## Caddy

Caddy turns a public site address into HTTPS when DNS for that name points at this machine and ports **80** and **443** reach Caddy.

The site address below is `PEEKAPING_HOSTNAME`, or `peekaping.example.com` when that variable is unset. Use the same hostname in `CLIENT_URL`.

### Caddy on the host

Publish Peekaping on loopback and forward the site to `127.0.0.1:8383`.

Add this site block to the Caddyfile on the host:

```caddyfile
{$PEEKAPING_HOSTNAME:peekaping.example.com} {
    reverse_proxy 127.0.0.1:8383
}
```

`reverse_proxy` forwards every path on that host and upgrades WebSocket connections to `/socket.io/*`.

Compose file for Peekaping:

```yml
# PEEKAPING_HOSTNAME: public DNS name of the Caddy site block
# TRUSTED_PROXIES: the gateway of this container's Docker network (see below)

services:
  peekaping:
    image: 0xfurai/peekaping:latest
    restart: unless-stopped
    # peekaping takes up to 30 seconds to shut down cleanly
    stop_grace_period: 35s
    ports:
      - "127.0.0.1:8383:8383"
    environment:
      - CLIENT_URL=https://${PEEKAPING_HOSTNAME:-peekaping.example.com}
      - TRUSTED_PROXIES=${TRUSTED_PROXIES-}
    volumes:
      - ./.data/sqlite:/app/data
```

The host side of the port mapping is `127.0.0.1`, so port 8383 is only on the loopback interface. Caddy on that same machine is the process listening on ports 80 and 443.

Start Peekaping with `docker compose up -d`, then load the Caddyfile with the host's Caddy (`caddy reload`, or `caddy run --config /path/to/Caddyfile`).

`PEEKAPING_HOSTNAME` must be supplied to the host Caddy process itself, for example by exporting it in the shell that starts Caddy or by setting it in the Caddy service environment (such as a systemd `Environment=` line). Compose reads `.env` for the Compose file only, so that file does not set the site address. The value Caddy receives must match the hostname used in `CLIENT_URL`.

Caddy on the host connects through the gateway of the container's Docker network, so that gateway is the value of `TRUSTED_PROXIES`. Read it from the running container (the name is from `docker ps`), put it in `.env`, and run `docker compose up -d` again:

```bash
docker inspect -f '{{range $name, $net := .NetworkSettings.Networks}}{{$name}} {{$net.Gateway}}{{println}}{{end}}' <container name>
```

```env
TRUSTED_PROXIES=172.19.0.1
```

Trust the gateway only while the port is published on loopback, as above. [Reverse proxy and HTTPS](./docker-with-sqlite.md#reverse-proxy-and-https) says why.

### Caddy in Docker

A Caddy container has its own loopback interface, so `127.0.0.1:8383` inside that container is Caddy itself. Put Caddy and Peekaping on one Docker network and proxy to Peekaping's Compose service name, `peekaping`, on port 8383:

```caddyfile
{$PEEKAPING_HOSTNAME:peekaping.example.com} {
    reverse_proxy peekaping:8383
}
```

Save that file as `Caddyfile` next to this Compose file:

```yml
# PEEKAPING_HOSTNAME: public DNS name of the Caddy site block
# PEEKAPING_NETWORK: explicit name of the Docker network both services share
# TRUSTED_PROXIES: address range of that network (see below)

services:
  caddy:
    image: caddy:2
    restart: unless-stopped
    environment:
      - PEEKAPING_HOSTNAME=${PEEKAPING_HOSTNAME:-peekaping.example.com}
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
    networks:
      - peekaping

  peekaping:
    image: 0xfurai/peekaping:latest
    restart: unless-stopped
    # peekaping takes up to 30 seconds to shut down cleanly
    stop_grace_period: 35s
    environment:
      - CLIENT_URL=https://${PEEKAPING_HOSTNAME:-peekaping.example.com}
      - TRUSTED_PROXIES=${TRUSTED_PROXIES-}
    volumes:
      - ./.data/sqlite:/app/data
    expose:
      - "8383"
    networks:
      - peekaping

networks:
  peekaping:
    name: ${PEEKAPING_NETWORK:-peekaping}

volumes:
  caddy_data:
  caddy_config:
```

`PEEKAPING_NETWORK` is the Docker name of that shared network (default `peekaping`). Both services join it. Caddy publishes ports 80 and 443 for automatic HTTPS and stores certificates in the `caddy_data` volume. Peekaping publishes no host port.

```bash
export PEEKAPING_HOSTNAME=peekaping.example.com
export PEEKAPING_NETWORK=peekaping
docker compose up -d
```

Caddy connects to Peekaping from its address on the shared network. Read the network's range, set it as `TRUSTED_PROXIES` in `.env`, and run `docker compose up -d` again:

```bash
docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' peekaping
```

To serve status pages on domains of their own through this Caddy, see [Custom domains and HTTPS](./docker-with-sqlite.md#custom-domains-and-https).

## Troubleshooting

### Unreachable upstream

Traefik's gateway timeout means it cannot dial Peekaping. Keep `traefik.http.services.peekaping.loadbalancer.server.port` at `8383` and `traefik.docker.network` equal to `TRAEFIK_NETWORK`. Peekaping speaks plain HTTP on 8383 in the container, whatever `SERVER_PORT` says.

Host Caddy logging `connection refused` for `127.0.0.1:8383` means the container is stopped, or the published address is different from `127.0.0.1:8383:8383`.

A Caddy container whose site block still says `127.0.0.1:8383` dials itself. The container site block is `reverse_proxy peekaping:8383`. A lookup failure for the name `peekaping` means the Caddy service and Peekaping are on different networks, or the Peekaping service has another name.

### Network mismatch

The Traefik network block is `external: true` with `name: ${TRAEFIK_NETWORK}`. That same value is `traefik.docker.network`. The running Traefik container is attached with `docker network connect`.

Compose reports a missing external network when `TRAEFIK_NETWORK` names a network that was never created and that Traefik is not already using. Dropping `external: true` or `name` makes Compose create a project-prefixed network instead. Traefik stays on the original network, and the upstream is unreachable.

For Caddy in Docker, both services join the network whose Docker name is `PEEKAPING_NETWORK`.

### TLS, the entrypoint, and the hostname

`traefik.http.routers.peekaping.entrypoints=websecure` has to name an entrypoint that exists on the running Traefik. The label key is `entrypoints`. When your HTTPS entrypoint uses another name, put that name in this label.

Set `traefik.http.routers.peekaping.tls.certresolver` only to a name present in Traefik's static configuration. A resolver name that Traefik does not already define will not issue a certificate.

`PEEKAPING_HOSTNAME` in the Host rule is the name in DNS and in the browser. A different Host value leaves the `peekaping` router unmatched, and Traefik returns its own 404.

Caddy's site address is that same public name. Automatic HTTPS needs DNS pointed at this host and ports 80 and 443 delivered to the Caddy that loaded the site block (the host process, or the published ports on the Caddy container).

### Everyone is locked out after one person's failed logins

`TRUSTED_PROXIES` is empty or does not hold the address the proxy connects from, so every login counts as coming from the proxy. Set it as the recipe says and restart the container.

### What a working proxy serves

These all go through the one Traefik router or the one Caddy site block:

- `/` loads the UI.
- `/api/v1/health` answers from the API on that same hostname.
- A signed-in session keeps a Socket.IO connection on `/socket.io/`. Live monitor updates use that connection.
- Reloading `/monitors` loads the UI again, which exercises the single-page fallback.
