---
sidebar_position: 2
---

# Without Docker

Peekaping's [single mode](./deployment-modes.md#single-mode) is one process, `peekaping`, so it runs on a host without Docker too: on a router, in a small LXC container, or on any Linux machine where you don't want Docker and its iptables rules. You build two binaries and the web app, then run them under systemd. There is no Redis, no Caddy and no Node.js at runtime.

This is what the `0xfurai/peekaping` image does inside its container (`Dockerfile.single` and `startup.single.sh` in the repository), set up by hand.

## What you need

To build, on any machine (the binaries don't have to be built on the server):

- Go 1.27 or later
- [Bun](https://bun.sh) 1.4.4, the version in the root `package.json`, to build the web app
- `git`

On the server:

- Linux with systemd
- Nothing else for SQLite. For PostgreSQL, a PostgreSQL server that the server can reach.

## 1. Build

Clone the repository and check out the release you want to run:

```bash
git clone https://github.com/0xfurai/peekaping.git
cd peekaping
git checkout <release tag>
```

Build `peekaping` and `bun`, the migration tool. Both are static binaries with no C dependencies. Set `GOOS` and `GOARCH` to build for another machine, for example `GOARCH=arm64` for an ARM router:

```bash
cd apps/server
CGO_ENABLED=0 GOFLAGS="-trimpath" go build -o peekaping -ldflags="-s -w" ./cmd/peekaping
CGO_ENABLED=0 GOFLAGS="-trimpath" go build -o bun -ldflags="-s -w" ./cmd/bun
cd ../..
```

The migration tool's `bun` has nothing to do with the Bun JavaScript runtime below.

Build the web app. Its build is a folder of static files, in `apps/web/dist`:

```bash
bun install --frozen-lockfile --filter web
cd apps/web
bun run build
cd ../..
```

## 2. Install

Create a system user for Peekaping, and put the binaries, the web app and a data folder in `/opt/peekaping`:

```bash
sudo useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin peekaping

sudo mkdir -p /opt/peekaping/data
sudo install -m 755 apps/server/peekaping apps/server/bun /opt/peekaping/
sudo cp -r apps/web/dist /opt/peekaping/web
sudo chown peekaping:peekaping /opt/peekaping/data
```

The binaries and the web app belong to root, so Peekaping can't change them. Only the data folder belongs to `peekaping`.

## 3. Configure

Peekaping reads its settings from environment variables. Put them in `/etc/peekaping/peekaping.env`:

```env
# SQLite: the database file, in the data folder
DB_TYPE=sqlite
DB_NAME=/opt/peekaping/data/peekaping.db

# peekaping serves the web app, the API and the websocket on this port
SERVER_PORT=8383
WEB_DIR=/opt/peekaping/web

# The address people open, so links in notifications point there
CLIENT_URL=http://localhost:8383

MODE=prod
TZ=UTC

# Behind a reverse proxy on the same host (see Reverse proxy and HTTPS)
# TRUSTED_PROXIES=127.0.0.1,::1
```

Create the folder first with `sudo mkdir -p /etc/peekaping`. The file can hold passwords, such as the PostgreSQL one or `ACCOUNT_SMTP_PASSWORD`, so keep it readable by root only. systemd reads it as root before it starts Peekaping:

```bash
sudo chmod 600 /etc/peekaping/peekaping.env
```

`DB_TYPE` and `DB_NAME` have no defaults, and both binaries need them. `WEB_DIR` makes `peekaping` serve the web app on `SERVER_PORT` beside the API; without it, it serves the API alone. The image's other settings work the same way here: see [Settings](./docker-with-sqlite.md#settings) and [API Server](../architecture/api-server.md). Unlike in the image, `SERVER_PORT` and `DB_NAME` are used as you set them.

Both binaries also read a `.env` file two folders above the folder they run in. With the unit below they run in `/opt/peekaping`, so that is `/.env`. Don't put one there.

### PostgreSQL

To use PostgreSQL instead of SQLite, replace the two database lines with:

```env
DB_TYPE=postgres
DB_HOST=127.0.0.1
DB_PORT=5432
DB_USER=peekaping
DB_PASS=your-secure-password-here
DB_NAME=peekaping
# DB_SSLMODE=require
```

Create the user and the database in PostgreSQL first. The data folder then stays empty.

## 4. Run with systemd

Create `/etc/systemd/system/peekaping.service`. It runs the migrations before each start, as the image's startup script does, so an upgrade migrates the database too:

```ini
[Unit]
Description=Peekaping
After=network-online.target
Wants=network-online.target
# With PostgreSQL on the same host, also:
# After=postgresql.service

[Service]
Type=simple
User=peekaping
Group=peekaping
WorkingDirectory=/opt/peekaping
EnvironmentFile=/etc/peekaping/peekaping.env
# Create the migration tables on the first start, then apply the migrations
ExecStartPre=/bin/sh -c './bun db status >/dev/null 2>&1 || ./bun db init'
ExecStartPre=/opt/peekaping/bun db migrate
ExecStart=/opt/peekaping/peekaping
Restart=on-failure
RestartSec=5
# peekaping takes up to 30 seconds to shut down cleanly
TimeoutStopSec=35

[Install]
WantedBy=multi-user.target
```

Start it, and start it at boot:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now peekaping
```

Then open `http://<server>:8383` and create your admin account. `journalctl -u peekaping -f` shows the logs, and `http://<server>:8383/api/v1/health` answers once Peekaping is up.

## Ping monitors

Peekaping doesn't run as root, so ping monitors open an unprivileged ICMP socket. Linux allows that only for a process whose group is in the `net.ipv4.ping_group_range` sysctl. Docker sets it in each container, but on the host it is your distribution's choice. Check it:

```bash
sysctl net.ipv4.ping_group_range
```

Many distributions set `0 2147483647`, which allows every group. If yours shows `1 0`, which allows none, ping monitors are Down with `cannot open an ICMP socket`. Allow Peekaping's group, or every group, and keep the setting across reboots:

```bash
echo 'net.ipv4.ping_group_range = 0 2147483647' | sudo tee /etc/sysctl.d/60-peekaping-ping.conf
sudo sysctl --system
```

See [Ping monitors](./single-user.md#ping-monitors) for the details.

## Reverse proxy and HTTPS

`peekaping` serves plain HTTP on `SERVER_PORT`, on every interface. For HTTPS or your own domain, put a reverse proxy in front of it, such as Caddy or nginx on the same host, pointed at `localhost:8383`. Set `TRUSTED_PROXIES=127.0.0.1,::1` so Peekaping reads the client's address from the proxy's `X-Forwarded-For`, and block port 8383 from other hosts in your firewall, so clients can't reach Peekaping past the proxy and send their own header. [Reverse proxy and HTTPS](./docker-with-sqlite.md#reverse-proxy-and-https) and [Custom domains and HTTPS](./docker-with-sqlite.md#custom-domains-and-https) cover the rest; with the proxy on the host, use `localhost:8383` wherever they write `peekaping:8383`.

## Upgrading

Build the new release as in [Build](#1-build), then:

```bash
sudo systemctl stop peekaping
# With SQLite, back up the database while Peekaping is stopped,
# in a new folder named after the date and time, so earlier backups stay as they are
sudo cp -a /opt/peekaping/data "/opt/peekaping/data.backup-$(date +%Y%m%d-%H%M%S)"

sudo install -m 755 apps/server/peekaping apps/server/bun /opt/peekaping/
sudo rm -rf /opt/peekaping/web
sudo cp -r apps/web/dist /opt/peekaping/web

sudo systemctl start peekaping
```

The unit runs the new migrations before `peekaping` starts. Read the [changelog](https://github.com/0xfurai/peekaping/blob/main/CHANGELOG.md) for upgrade notes first, and see [Upgrading stats storage](./upgrading-stats.md) for backups. If the SQLite startup check finds corruption, see [Recovering a corrupt SQLite database](./sqlite-recovery.md).
