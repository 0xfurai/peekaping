# 0006: Serving status pages from the server

Status: Proposed
Date: 2026-10-26
Ticket: PP-333

## Context

A status page is a route of the dashboard SPA. What happens when someone opens one:

1. Any path that isn't a file gets the build's `index.html`: from the Go gateway in single mode (`apps/server/internal/web/gateway.go`, ADR 0002 section 7), and from the `peekaping-web` nginx container in cluster mode. Neither looks at the path or the `Host` header. The HTML is titled "Peekaping" and holds an empty `<div id="root">`.
2. The browser loads the whole dashboard in one chunk, about 1.6 MB or 440 KB gzipped. Routes aren't split.
3. On start, every load of the app, dashboard included, asks `GET /api/v1/status-pages/domain/{hostname}` and holds all routing until it answers (`apps/web/src/components/app-router.tsx`). A listed custom domain, for a visitor who isn't signed in, gets the status page at `/`. A signed-in user gets the dashboard on any host. The app also asks `GET /api/v1/version` and can show the version mismatch banner to a visitor.
4. The page then asks for the page, then for its monitors, one after the other, and asks again for both every `auto_refresh_interval` seconds, in every visitor's tab.
5. For the monitors, the API reads the page, its monitor links, and for each monitor the monitor, its last 100 heartbeats and its 24 hour uptime, and the same again for each location when the page shows them (`publicMonitors` in `apps/server/internal/modules/status_page/status_page.controller.go`). Nothing is cached.

What follows from it:

- **Only the browser knows which page a host shows.** Any host pointed at Peekaping serves the dashboard and its login. A custom domain can't be told from any other host by a proxy, a CDN or a TLS terminator. The Helm ingress lists one host, so a custom domain doesn't reach a Helm install at all unless its owner adds a rule.
- **Visitors pay for the dashboard.** 440 KB of code they never run, then two requests in a row before the first status shows. With JavaScript off, the page is blank. Link previews and search engines see "Peekaping".
- **Nothing can be cached.** The HTML is the same for every page, the data comes per request, and every visitor costs the database the whole read. A status page gets its most visitors during an outage, which is when the install has the least to spare.
- **The page is tied to the dashboard's start.** Its providers, its version check and the custom-domain lookup that held the routes and reset open forms (#161, #228).

On Peekaping Cloud (ADR 0005) one install serves the custom domains of many workspaces, so which page a host shows is a question of whose data is shown. That belongs on the server.

## Decision

### 1. Matching a request to a status page

The server matches, in both modes, before the web app is served:

- **A listed custom domain** shows its own page and nothing else. `/` renders the page. The page's own assets and the API's public status page routes work as anywhere. Any other path is a 404, including `/status/<slug>` of another page and the dashboard's paths. Listed means in `domain_status_page`. A domain of an unpublished page gets the same 404 page as an unknown path, as the API does today.
- **`/status/<slug>` on any other host** renders the published page with that slug, or the 404 page.
- **Every other request** gets the web app, as today.

The match keys on the custom domain list, not on `CLIENT_URL`. Many installs leave `CLIENT_URL` at its default, `http://localhost:3000`, and open the dashboard by IP address or another name. A rule like "every host but the app's own is a status page" would take their dashboard away.

The host is compared in lower case, without its port and without a trailing dot. The service starts saving custom domains in that form too, and refuses the host of `CLIENT_URL` as a custom domain. Today it saves them as typed.

The gateway holds the list in memory: domain to page ID. It loads it at startup and again on a new event, sent when a status page's domains change. In cluster mode the event reaches the other API instances over the event bus, and a reload every 60 seconds covers a lost one. A lookup per request would put a database read in front of every asset of the dashboard.

A signed-in admin who opens a custom domain sees the status page, as a visitor does. The dashboard is on the app's own host.

### 2. Rendering

The API process renders the page with `html/template`. The templates, the stylesheet and a small script live in a Go package (working name `internal/statuspage`) and are embedded with `go:embed`. ADR 0002 rejected `go:embed` for the web build, because `go build` would need a bun build first. These files are the Go package's own, so that reason doesn't apply.

- **The HTML is the page.** Title, description, icon, overall status, every monitor with its status, uptime and history, and the footer are in the response. The `<title>`, the description meta tag, the favicon and the Open Graph tags come from the page.
- **The script only refreshes.** Under 10 KB, no framework. Every `auto_refresh_interval` seconds it fetches the snapshot (section 3) and updates the page in place. It also shows "last updated" in the visitor's time zone and keeps the light/dark toggle. Without JavaScript, the page is complete and a reload refreshes it.
- **The page's `theme` setting** picks light, dark or the visitor's system preference. It's stored today and ignored.
- **Translations.** The page's strings (`status.*` in the web app's locale files) are copied into the Go package by a script, and a CI check fails when the copies drift, as the API client check does. The language comes from `Accept-Language`, falling back to `en-US`.
- **Headers.** A `Content-Security-Policy` that allows only the page's own script and stylesheet, and images from anywhere, since the icon is any URL. No cookies are read or set.

We rejected a second web entry (a small React bundle for status pages). The HTML would still be empty, the page would still wait for its data, and it would be a second front end to keep in step with the first. We rejected server-side rendering of React: it needs a Node process next to Go, which ADR 0002 took out. We rejected patching the SPA (split routes, skip the version check): it leaves matching in the browser and the HTML empty.

### 3. One snapshot per page, cached

The page reads one value: the **snapshot** of a published page. It holds the page's public fields and its monitors in the page's order, each with the fields the page shows today: name, status, 24 hour uptime, history and, when the page shows them, locations. It names no internal ID: a monitor is keyed by its position on the page. The renderer and a new public route, `GET /api/v1/status-pages/slug/{slug}/snapshot`, both serve it.

- **In the process:** cached per page for 15 seconds. Concurrent misses for one page build it once (`singleflight`). Editing the page in this process drops its entry.
- **Over HTTP:** the HTML and the snapshot both carry `Cache-Control: public, max-age=15, stale-while-revalidate=60` and an `ETag`, so a browser, a proxy or a CDN can answer for the install. The 404 page carries `max-age=15` too, so a page that's unpublished disappears within that time plus what a cache keeps stale. That's the price of caching, and it's short.
- **What the snapshot contains is out of scope here.** It carries what the page shows today. A history by day from `stat_daily`, groups, or maintenance on the page are content changes, and the snapshot is where they land.

The public routes the SPA uses today (`/slug/{slug}`, `/domain/{domain}`, `/slug/{slug}/monitors`, `/slug/{slug}/monitors/homepage`) keep working and are marked deprecated in the OpenAPI document. Removing them is a later decision, since scripts may call them.

### 4. Cluster mode

In cluster mode the Go gateway serves the web build too, as in single mode. This changes ADR 0002, which left cluster mode's web serving as it was (sections 7 and Consequences there). The API image carries the web build and sets `WEB_DIR`. The nginx gateway in the compose files sends every request to the API, and the Helm ingress sends `/` to the API service. The `peekaping-web` image and the chart's web deployment are retired.

So matching lives in one place for both modes. The alternative, teaching nginx and the ingress which hosts are custom domains, needs either the domain list in their config, edited and reloaded with every change, or a host rule we can't write (section 1).

What a cluster install has to change: the compose files and the chart. The release that ships this keeps publishing `peekaping-web`, and keeps the SPA's status page route, for one release. A compose file that still sends `/` to `peekaping-web` keeps working until then. The release notes say what to change. For a custom domain to reach a Helm install, the chart gains a list of extra ingress hosts. Their certificates are PP-332 and PP-165.

### 5. The web app

Once section 4's grace release is out, the SPA's status page goes:

- the `/status/:slug` route, `createCustomDomainRoute` and `useCheckCustomDomain`;
- the custom-domain lookup in `AppRouter`, so the dashboard no longer waits on a request before routing.

The admin's "open" button already opens `/status/<slug>` as a new page load, so it reaches the server's page.

### 6. Workspaces

The lookups in sections 1 and 3 are public reads across workspaces, as ADR 0005 section 3 lists: a status page by slug or custom domain, and the monitors of a published page. They use the `AnyWorkspace` methods, and the snapshot reads only the monitors linked to the page it's for. A custom domain shows its own page and nothing else, so it can't be used to show another workspace's page.

### 7. Custom domains and TLS

The domain list in section 1 is the same list that decides which hosts get a certificate. The TLS ask endpoint of PP-332 answers from it, and PP-165 limits it to verified domains on the cloud. If `peekaping` ever terminates TLS itself in single mode, its certificate decision reads the same list.

## Consequences

- A status page is one request, server-rendered and cacheable. It loads without the dashboard, shows without JavaScript, and a CDN in front can carry an outage's traffic.
- The server knows which host is a status page. That is what PP-332, PP-165 and any CDN setup need.
- A custom domain no longer serves the dashboard or its login.
- The page is drawn twice for one release: by the server, and by the SPA for compose files not yet updated. Changes to the page in that release go to the server's version only.
- The page's strings live in two places, kept equal by a CI check. New strings start in the web app's locale files, as now.
- The look of the page moves from the dashboard's components to its own template and stylesheet. It has to be kept close to the dashboard's look by hand.
- The cluster API image grows by the web build, and cluster installs change their compose file or chart once.
- A change to a status page reaches visitors within 15 seconds plus what a cache keeps stale, not at once.

Waiting tickets:

- **PP-334** (snapshot and cache): section 3. The snapshot route, the in-process cache, the HTTP cache headers, and the deprecation marks on the old routes.
- **PP-335** (server-rendered page and matching): sections 1 and 2. The domain list in the gateway, the templates and script, the copied translations and their CI check, the 404 page, and refusing `CLIENT_URL`'s host as a custom domain. Builds on PP-334.
- **PP-336** (cluster serving): section 4. The web build in the API image, the compose files' gateway, the chart's ingress and extra hosts, retiring `peekaping-web` after the grace release.
- **PP-337** (SPA clean-up): section 5, one release after PP-335 and PP-336 ship.
- **PP-332** (TLS ask endpoint) and **PP-165** (domain verification): section 7. They read the same domain list.
