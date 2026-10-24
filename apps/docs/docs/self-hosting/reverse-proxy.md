---
sidebar_position: 5
---

# Reverse proxy

The bundle image serves the web UI, the HTTP API, and live Socket.IO updates on port **8383**. Traefik or Caddy in front of that port terminates HTTPS and forwards every request for your hostname to it.

Install the bundle with [Docker + SQLite](./docker-with-sqlite.md) or [Docker + PostgreSQL](./docker-with-postgres.md). The recipes below use `0xfurai/peekaping-bundle-sqlite:latest` and store the database at `/app/data`, mounted from `./.data/sqlite` on the host, as in the SQLite guide. The PostgreSQL bundle uses the same gateway port and the data directory from its own guide. An existing [MongoDB](./docker-with-mongo.md) bundle does too.

The bundle runs its app processes as a non-root user and owns `/app/data`. [Bundle user and permissions](./bundle-user.md) covers `PUID` and `PGID` when that directory needs a matching owner.

## Gateway routes inside the bundle

The image ships a Caddy gateway on port 8383. The bundle writes `API_URL` as an empty string, so the browser calls the API and Socket.IO on the same host as the page. That gateway already sorts the traffic:

- `/api/*` goes to the API.
- `/socket.io/*` goes to the API as a WebSocket. The UI uses the websocket transport.
- Every other path is the single-page app. Reloading a nested route such as `/monitors` returns the app (`index.html`).

An outer proxy forwards the whole hostname, including the WebSocket upgrade. Traefik and Caddy pass `Upgrade` and `Connection` through on their own. One router, or one Caddy site block, is the whole configuration.

Microservice mode, in the [SQLite guide](./docker-with-sqlite.md#microservice-mode), splits `/api/*` and `/socket.io/*` from the web app. The API service in that mode is named `api`. The bundle recipes on this page keep a single upstream: the bundle on port 8383.

## Traefik

Use this with a Traefik installation that is already running. The Compose file only adds the bundle. Traefik reaches it on the shared Docker network at port 8383, so the bundle publishes no host port.

### Prerequisites

Your existing Traefik provides:

- An HTTPS entrypoint named `websecure` (the usual listener on port 443). The label key below is `entrypoints`.
- A certificate resolver in Traefik's static configuration. `tls=true` uses the default resolver for that entrypoint. This file does not choose a resolver name. When the entrypoint has no default resolver, uncomment the `tls.certresolver` label and set the name from that static configuration.
- A Docker network the Traefik container is attached to.
- Public DNS for the hostname pointing at the Traefik host, plus the ports that resolver's challenge already uses (often 80 and 443).

### Hostname and network

`PEEKAPING_HOSTNAME` is the name in the Host rule, in DNS, and in the browser. `TRAEFIK_NETWORK` is the Docker network Traefik already uses. Create that network only when it is missing, then connect the running Traefik container:

```bash
export PEEKAPING_HOSTNAME=peekaping.example.com
export TRAEFIK_NETWORK=traefik

docker network create "$TRAEFIK_NETWORK"
docker network connect "$TRAEFIK_NETWORK" traefik
```

The last argument of `docker network connect` is the Traefik container name from `docker ps`. Skip `docker network create` when the network already exists. Compose also reads these variables from a `.env` file in the same directory as the Compose file:

```env
PEEKAPING_HOSTNAME=peekaping.example.com
TRAEFIK_NETWORK=traefik
```

### Compose file

```yml
# PEEKAPING_HOSTNAME — public DNS name served by this router
# TRAEFIK_NETWORK — Docker network the existing Traefik container already uses

services:
  peekaping:
    image: 0xfurai/peekaping-bundle-sqlite:latest
    restart: unless-stopped
    environment:
      - DB_TYPE=sqlite
      - DB_NAME=/app/data/peekaping.db
      - CLIENT_URL=https://${PEEKAPING_HOSTNAME}
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
      # Uncomment when the websecure entrypoint has no default certificate
      # resolver, and set the name from Traefik's static configuration.
      # - "traefik.http.routers.peekaping.tls.certresolver=RESOLVER_NAME"
      - "traefik.http.routers.peekaping.service=peekaping"
      - "traefik.http.services.peekaping.loadbalancer.server.port=8383"

networks:
  traefik:
    external: true
    name: ${TRAEFIK_NETWORK}
```

There is one router. Its name, `peekaping`, is the same on the rule, the entrypoint, TLS, and the service label. The service `peekaping` uses port 8383. `traefik.docker.network` and the external network's `name` are both `TRAEFIK_NETWORK`, so Traefik selects that shared network.

`CLIENT_URL` is the public site URL. Notification messages link to it. `./.data/sqlite` on the host is the persistent copy of `/app/data` (`/app/data/peekaping.db` inside the container).

From the directory that contains the file:

```bash
docker compose up -d
```

Open `https://peekaping.example.com`, using the hostname you set.

## Caddy

Caddy turns a public site address into HTTPS when DNS for that name points at this machine and ports **80** and **443** reach Caddy.

The site address below is `PEEKAPING_HOSTNAME`, or `peekaping.example.com` when that variable is unset. Use the same hostname in `CLIENT_URL`.

### Caddy on the host

Publish the bundle on loopback and forward the site to `127.0.0.1:8383`.

Add this site block to the Caddyfile on the host:

```caddyfile
{$PEEKAPING_HOSTNAME:peekaping.example.com} {
    reverse_proxy 127.0.0.1:8383
}
```

`reverse_proxy` forwards every path on that host and upgrades WebSocket connections to `/socket.io/*`.

Compose file for the bundle:

```yml
# PEEKAPING_HOSTNAME — public DNS name of the Caddy site block

services:
  peekaping:
    image: 0xfurai/peekaping-bundle-sqlite:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:8383:8383"
    environment:
      - DB_TYPE=sqlite
      - DB_NAME=/app/data/peekaping.db
      - CLIENT_URL=https://${PEEKAPING_HOSTNAME:-peekaping.example.com}
    volumes:
      - ./.data/sqlite:/app/data
```

The host side of the port mapping is `127.0.0.1`, so port 8383 is only on the loopback interface. Caddy on that same machine is the process listening on ports 80 and 443.

Start the bundle with `docker compose up -d`, then load the Caddyfile with the host's Caddy (`caddy reload`, or `caddy run --config /path/to/Caddyfile`).

`PEEKAPING_HOSTNAME` must be supplied to the host Caddy process itself, for example by exporting it in the shell that starts Caddy or by setting it in the Caddy service environment (such as a systemd `Environment=` line). Compose reads `.env` for `CLIENT_URL` only, so that file does not set the site address. The value Caddy receives must match the hostname used in `CLIENT_URL`.

### Caddy in Docker

A Caddy container has its own loopback interface, so `127.0.0.1:8383` inside that container is Caddy itself. Put Caddy and the bundle on one Docker network and proxy to the bundle's Compose service name, `peekaping`, on port 8383:

```caddyfile
{$PEEKAPING_HOSTNAME:peekaping.example.com} {
    reverse_proxy peekaping:8383
}
```

Save that file as `Caddyfile` next to this Compose file. The bundle keeps the gateway file baked into the image. This Caddyfile is mounted only on the Caddy service.

```yml
# PEEKAPING_HOSTNAME — public DNS name of the Caddy site block
# PEEKAPING_NETWORK — explicit name of the Docker network both services share

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
    image: 0xfurai/peekaping-bundle-sqlite:latest
    restart: unless-stopped
    environment:
      - DB_TYPE=sqlite
      - DB_NAME=/app/data/peekaping.db
      - CLIENT_URL=https://${PEEKAPING_HOSTNAME:-peekaping.example.com}
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

`PEEKAPING_NETWORK` is the Docker name of that shared network (default `peekaping`). Both services join it. Caddy publishes ports 80 and 443 for automatic HTTPS and stores certificates in the `caddy_data` volume. The bundle publishes no host port.

```bash
export PEEKAPING_HOSTNAME=peekaping.example.com
export PEEKAPING_NETWORK=peekaping
docker compose up -d
```

## Troubleshooting

### Unreachable upstream

Traefik's gateway timeout means it cannot dial the bundle. Keep `traefik.http.services.peekaping.loadbalancer.server.port` at `8383` and `traefik.docker.network` equal to `TRAEFIK_NETWORK`. The bundle gateway speaks plain HTTP on 8383. That port is the upstream. Port 8034 is the API process inside the image and does not serve the UI.

Host Caddy logging `connection refused` for `127.0.0.1:8383` means the bundle is stopped, or the published address is different from `127.0.0.1:8383:8383`.

A Caddy container whose site block still says `127.0.0.1:8383` dials itself. The container site block is `reverse_proxy peekaping:8383`. A lookup failure for the name `peekaping` means the Caddy service and the bundle are on different networks, or the bundle service has another name.

### Network mismatch

The Traefik network block is `external: true` with `name: ${TRAEFIK_NETWORK}`. That same value is `traefik.docker.network`. The running Traefik container is attached with `docker network connect`.

Compose reports a missing external network when `TRAEFIK_NETWORK` names a network that was never created and that Traefik is not already using. Dropping `external: true` or `name` makes Compose create a project-prefixed network instead. Traefik stays on the original network, and the upstream is unreachable.

For Caddy in Docker, both services join the network whose Docker name is `PEEKAPING_NETWORK`.

### TLS, the entrypoint, and the hostname

`traefik.http.routers.peekaping.entrypoints=websecure` has to name an entrypoint that exists on the running Traefik. The label key is `entrypoints`. When your HTTPS entrypoint uses another name, put that name in this label.

`tls=true` selects the certificate resolver already configured for that entrypoint. Set `traefik.http.routers.peekaping.tls.certresolver` only to a name present in Traefik's static configuration. A resolver name that Traefik does not already define will not issue a certificate.

`PEEKAPING_HOSTNAME` in the Host rule is the name in DNS and in the browser. A different Host value leaves the `peekaping` router unmatched, and Traefik returns its own 404.

Caddy's site address is that same public name. Automatic HTTPS needs DNS pointed at this host and ports 80 and 443 delivered to the Caddy that loaded the site block (the host process, or the published ports on the Caddy container).

### What a working proxy serves

These all go through the one Traefik router or the one Caddy site block, then the bundle gateway:

- `/` loads the UI.
- `/api/v1/health` answers from the API on that same hostname.
- A signed-in session keeps a Socket.IO connection on `/socket.io/`. Live monitor updates use that connection.
- Reloading `/monitors` loads the UI again, which exercises the bundle's single-page fallback.
