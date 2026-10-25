# 0005: Workspaces and data isolation

Status: Accepted
Date: 2026-10-25
Ticket: PP-142

## Context

Peekaping Cloud is one shared install that many customers use at once (project Peekaping Cloud, milestone 2). Today an install has one set of monitors, and every signed-in user sees all of it. Only the first user can sign up (`apps/server/internal/modules/auth/auth.service.go`), so in practice an install has one user, and no record says whom it belongs to.

To sell one install to many customers, each customer's data has to be walled off. Self-hosters need the same thing when several groups of people share an install. And nothing may change for a self-hoster who doesn't care: no new limits, the same web app, the same API for scripts and the community Terraform provider.

What the code has today, as far as it matters here:

- **Auth.** A request carries a JWT or an API key (`X-API-Key`), and `apps/server/internal/modules/middleware/auth_chain.go` picks one. A JWT puts the user ID into the request context. An API key has no user at all.
- **Repositories** take a context and the record's ID, for example `FindByID(ctx, id)` in `apps/server/internal/modules/monitor/monitor.sql.repository.go`. There is no shared repository code (PP-91 proposes some).
- **Settings** are one key/value table. The settings API reaches one key, the data retention period (`userEditableKeys` in `apps/server/internal/modules/setting/setting.service.go`). The others are internal.
- **Probes** belong to the install. Any signed-in user can create, change or delete one (`apps/server/internal/modules/probe/probe.route.go`). A probe's offline notification goes to notification channels linked to it.
- **Uniqueness.** Status page slugs and probe tokens are unique in the database. Custom domains are unique across the install only in the service (`validateDomains` in `apps/server/internal/modules/status_page/status_page.service.go`); the database only stops one page listing a domain twice. Push tokens have no index, and a monitor without one stores an empty string. Tag names are unique across the install, in the database and in `apps/server/internal/modules/tag/tag.service.go`.
- **Websocket.** Any signed-in user can join any room, and every heartbeat also goes to the `monitor:all` room (`apps/server/internal/modules/websocket/websocket.go`).

The Cloud planning document ("Cloud planning: decisions and research (Oct 2026)" in Linear) holds the first round of answers, decided on 2026-10-18. This ADR records them and settles the rest. The ticket asked nine questions; sections 1 to 9 answer them in order, and sections 10 to 12 cover what the code turned up.

## Decision

The ticket called the thing an **organization**. We call it a **workspace**, as Linear does. It reads as naturally for one person as for a company, and it leaves "team" free for a level inside a workspace, which is how Linear and Better Stack use that word. Peekaping has one level: no teams inside a workspace.

### 1. Model

A **workspace** owns monitors and everything around them. A user belongs to a workspace as a **member**, with one **role**. A user can be a member of several workspaces, with a role in each. The user's own account (email, password, two-factor) belongs to the user, not to a workspace.

| What | Member | Admin | Owner |
| --- | --- | --- | --- |
| See everything in the workspace | yes | yes | yes |
| Monitors, notification channels (and their test), status pages and their custom domains, maintenance windows, tags, proxies | yes | yes | yes |
| Invite members, change a role, remove a member (owners excepted) | | yes | yes |
| API keys | | yes | yes |
| Workspace settings (section 5) and the workspace's name | | yes | yes |
| See the plan and usage | | yes | yes |
| Change the plan and billing, delete the workspace, make or remove owners | | | yes |

- Every workspace has at least one owner. The last owner can't leave, be removed or be made something else.
- The plainest role shares its name with the people who hold roles: a member of a workspace has the role member, admin or owner.
- A **viewer** role (sees everything, changes nothing) comes later, not in v1.
- An **API key** belongs to one workspace and acts as an admin of it, never as an owner. Scripts and the Terraform provider keep what they reach in a workspace today. They lose two things: billing and deleting the workspace, which are new, and managing probes, which a key can do today but which move to the install (section 10).
- The **plan** belongs to the workspace. A user who owns two workspaces pays for each. A workspace without a plan has no limits, which is every workspace on a self-hosted install.

### 2. Where the workspace ID lives

New tables: the workspaces, their members (workspace, user, role, when they joined), and the workspace settings (section 5). Names are for PP-146.

These tables get a `workspace_id` column: `NOT NULL`, indexed, and a foreign key to the workspace with `ON DELETE CASCADE`:

- `monitors`
- `notification_channels`
- `proxies`
- `tags`
- `maintenances`
- `status_pages`
- `api_keys`

Every other record gets its workspace through its parent:

- through the monitor: `heartbeats`, `stat_minutely`, `stat_hourly`, `stat_daily`, `monitor_tls_info`, `notification_sent_history`, `monitor_probes`;
- through both ends, which must be in the same workspace: `monitor_notifications`, `monitor_tags`, `monitor_maintenances`, `monitor_status_pages`, and a monitor's `proxy_id`;
- through the status page: `domain_status_page`.

These belong to the install and get no workspace: `users`, `login_state`, `settings`, `probes`, `probe_instances`. `probe_notification_channels` links an install's probe to a workspace's channel (section 10).

Deleting a workspace deletes its records by cascade, and theirs in turn, so no row is left behind (PP-170). Most child tables already cascade from their parent, but `monitor_tls_info` and `notification_sent_history` have no foreign key to the monitor. Their `monitor_id` is a `VARCHAR`, and deleting a monitor leaves their rows behind. PP-146 deletes those orphans, gives the column the monitor ID's type, and adds the key.

We don't copy the workspace ID onto child records. A copy is a second truth that can drift from the parent. Today cleanup deletes heartbeats by time alone; per workspace it deletes by monitor and time. If PP-161 finds that slow on a large `heartbeats` table, it adds an index, not a column. A join table's two ends being in the same workspace is checked by the service that writes the link (PP-148, PP-149), not by a composite foreign key, which would need the copy.

### 3. How queries are scoped

The workspace ID is an explicit argument of every repository method on a workspace's table, right after the context: `FindByID(ctx, workspaceID, id)`. Leaving it out doesn't compile.

- It has its own Go type, so it can't be swapped with a record's ID by accident. Package and name are for PP-146.
- Every read filters on it. Every update and delete filters on it too and checks how many rows it changed, so a write to another workspace's record matches no row and is reported as not found. Today `Delete` and `UpdatePartial` don't check.
- A create takes the workspace from the argument, never from the request body.
- A route for a child record (a monitor's heartbeats, stats or TLS info, a status page's domains) first loads the parent in the request's workspace, and answers 404 when that fails. The child's own queries then run by the parent's ID.
- Services get the workspace from the request context (section 7) and pass it down. Repositories never read it from the context.

Some paths have to read across workspaces. They use separate methods whose names end in `AnyWorkspace`, so a reviewer can find them with one search. Only these paths call them:

- the push endpoint, finding a monitor by its push token;
- public status pages and badges, by slug, custom domain, or a monitor on a published page;
- the probe protocol: dealing monitors to probe instances and storing their results;
- the watchdog, the push timeout checks and the probe offline sweep;
- the cleanup job and the certificate expiry checks;
- notification sending, from a monitor to its own channels;
- the usage ping's counts;
- `/metrics`, which lists every monitor of the install with no auth, for the people who run it. The cloud must keep it off the public internet;
- startup jobs such as status repair.

A new caller needs a reason in review. PP-153 proves that no handler serving a workspace's request reaches another workspace.

We rejected a query hook that adds the filter by itself. It hides the filter, so a missing one fails silently. Background work would also need a way to switch the hook off, and that switch becomes the hole. PP-91 isn't a prerequisite: if it lands first, its helpers take the workspace ID as an argument too.

### 4. Background work

The api's ingest, the watchdog, the cleanup job, the certificate expiry checks and the notification listener run across all workspaces. They take the workspace from the monitor's `workspace_id` and read that workspace's settings and limits with it.

- The cleanup job goes workspace by workspace and deletes each one's heartbeats older than its own data retention period (PP-161). Stats keep their fixed retention for the whole install.
- A notification goes only to the channels linked to the monitor, which are in its workspace by section 2.
- Limits come from the plan, through the interface PP-143 defines, keyed by the workspace ID (PP-160). The public build has no plans, so it has no limits.
- A short in-process cache of workspace settings is allowed. With several api replicas, a changed setting then takes effect within the cache's lifetime.

### 5. Install and workspace settings

| Setting | Belongs to | Where it's stored |
| --- | --- | --- |
| JWT signing secrets (`auth.access_token_secret`, `auth.refresh_token_secret`) | install | `settings` |
| Token lifetimes (`ACCESS_TOKEN_EXPIRED_IN`, `REFRESH_TOKEN_EXPIRED_IN`; no API reaches them today) | install | `settings` |
| Usage ping (`TELEMETRY_ENABLED`, `TELEMETRY_INSTANCE_ID`, `TELEMETRY_LAST_SENT`) | install | `settings` |
| Error reports (`ERROR_REPORTING_ENABLED`) and the one-time notice (`TELEMETRY_NOTICE_SEEN`) | install | `settings` |
| Notification history and TLS info retention (`NOTIFICATION_HISTORY_RETENTION_DAYS`, `TLS_INFO_RETENTION_DAYS`) | install | environment |
| Signup mode (`SIGNUP_MODE`, section 11) and every other environment variable | install | environment |
| Data retention period (`KEEP_DATA_PERIOD_DAYS`) | workspace | workspace settings |
| Certificate expiry notification days (`cert_expiry_notify_days`) | workspace | workspace settings |
| Plan | workspace | its own record (PP-160, PP-162) |

- `/settings/key/KEEP_DATA_PERIOD_DAYS` keeps its path and works on the request's workspace. Members read it, admins change it. A plan can cap it (PP-161).
- Only an instance admin (section 10) can change install settings: today that's the usage ping and error reports.
- A new workspace starts with today's defaults for a new install: a data retention period of 180 days and certificate expiry notifications at 7, 14 and 21 days.
- On upgrade, the install's current values move to the upgrade's workspace (section 12). An install that never stored a data retention period keeps 365 days, the value it uses today when the setting is unset.

### 6. Unique across the install

These stay unique across the whole install, and the database enforces each one:

- **Status page slugs.** Already a unique column.
- **Custom domains.** A new unique index on the domain, stored lower-case and trimmed. The service already refuses a domain another page uses, so the migration should find no duplicates once it has normalized them. If it does, it fails and names them. On the cloud, the first workspace to add a domain holds it, and PP-165's DNS check stops anyone holding a domain they don't own.
- **Push tokens.** A new unique index. The migration first turns empty tokens into `NULL`, and the code then stores `NULL` for a monitor without a token. Duplicate tokens fail the migration, which names the monitors.
- **Probe tokens** and **user emails.** Already unique columns.
- **API keys.** A key carries its record's ID and is checked against the stored hash, so it finds its record, and through it its workspace, as it does today.

A slug, custom domain or push token another workspace already holds is refused as taken. That tells the caller the value exists somewhere. We accept it: slugs and domains are public anyway, and a push token is a secret its owner chose, which is worth nothing to someone who only learns that it's taken.

**Tag names** stop being unique across the install. They become unique within a workspace: the index moves to `(workspace_id, name)` and the service's check does the same. Two customers may each have a tag called "production".

### 7. Choosing the workspace in a request

A request names its workspace with an `X-Workspace-ID` header.

- **No header:** the request runs in the user's **default workspace**, the one they joined first. So today's web app, scripts and the Terraform provider keep working unchanged.
- **A JWT request** may name any workspace the user is a member of. A workspace they aren't a member of answers 404, as if it didn't exist.
- **An API key** always runs in its own workspace. Naming another one answers 404.
- **A user in no workspace** (possible after being removed) can still sign in and manage their account. Every workspace route answers 404 until they join one.
- **Middleware** runs after authentication. It resolves the workspace, checks the membership, and puts the workspace ID and role into the request context (PP-147).
- **CORS** allows the `X-Workspace-ID` header, next to `Authorization` (`apps/server/internal/httpx/middleware.go`).
- **Routes that need no workspace** are unchanged: sign-in and account routes, public status pages, badges, the push endpoint and the probe protocol.
- **The web app** puts the workspace in its addresses, `/w/<workspaceId>/…`, and sends the header from there. A link to a monitor then opens in the right workspace for a teammate, and two tabs can show two workspaces. An old address redirects to the default workspace. The ID is the workspace's UUID; a readable slug can come later.
- **The websocket** gets one room per workspace in place of `monitor:all`. `join_room` lets a user into a workspace's room, or a monitor's room, only while they're a member of that workspace, and a removed member loses those rooms (PP-151).

We rejected a claim in the token: switching workspaces would mean a new token, and two tabs share one stored token. We rejected a workspace in the API path (`/api/v1/w/<id>/…`): every route would exist twice, and the unscoped one still needs the default.

### 8. Not found and forbidden

- **Another workspace's record** answers 404, for reads and writes alike, so its existence doesn't leak. A request body that names another workspace's record (a tag, a notification channel, a proxy, a monitor on a status page) gets the same answer as an ID that doesn't exist at all.
- **A workspace the user isn't a member of** answers 404 (section 7).
- **An action the member's role doesn't allow,** in their own workspace, answers 403.
- **An install route without the instance admin flag** answers 403.

### 9. Databases

Workspaces come to SQLite and PostgreSQL only. MongoDB storage is deprecated (ADR 0001), and PP-277 removes it before milestone 2 starts. Milestone 2 tickets write no Mongo code, and they wait for PP-277 if it hasn't merged by then.

### 10. Instance admin and probes

Some things belong to the install, not to a workspace: probes, token lifetimes, the usage ping and error reports. An **instance admin** manages them. It's a flag on the user, not a role, and it gives no access to any workspace's data. On the cloud, the instance admins are the people who run it.

- The first user to sign up gets the flag. On upgrade, every existing user gets it. Today that's the one user, who also owns the upgrade's workspace, so a self-hoster sees no change.
- v1 has no screen to give the flag to anyone else.
- ADR 0003 says an admin opts out of the usage ping and error reports. That admin is the instance admin.
- **Probes** stay the install's. Only an instance admin creates, changes, deletes or rotates the token of one. An API key can't, since it belongs to a workspace; today it can.
- Every member can see a probe's name, location, status and the monitor types it runs, so they can choose probes for their monitors. A probe instance's hostname, OS and version are shown to instance admins only.
- Any workspace's monitor can run on any probe that isn't disabled. A plan may limit how many probes a monitor uses (PP-160, PP-167).
- A probe's offline notification goes to the channels an instance admin links to it. They pick from the channels of a workspace they're a member of; the flag itself shows them no workspace's channels. Deleting that workspace removes the links by cascade.

Probes a workspace runs for itself are a later ticket, PP-286. They would give `probes` a nullable `workspace_id`.

### 11. Signup

Signup modes are two, set with `SIGNUP_MODE` (PP-155):

- **`invite`**, the default. The first user signs up as today and gets the first workspace, as its owner, with the instance admin flag. After that, people join only through an invite to a workspace.
- **`open`**, for the cloud. Anyone may sign up, and each new user gets a workspace of their own, as its owner. Their monitors run only once their email is verified (PP-152).

In both modes an invite adds the invited user to an existing workspace with the role the invite names (PP-154). Someone who signs up through an invite joins that workspace and gets no workspace of their own, in `open` mode too. Invites are their own records, added by PP-154. Without an email sender, the inviter copies the invite link.

Any signed-in user can create another workspace and becomes its owner (PP-154). Like open signup, that waits for the isolation suite (section 12).

### 12. Upgrade and shipping

On the first start after the upgrade (PP-146), one migration:

- creates one workspace, the upgrade's workspace;
- makes every existing user its owner and an instance admin;
- puts every record in it;
- moves the data retention period and the certificate expiry notification days into its settings, as section 5 says.

Running it again changes nothing. A self-hosted install then works as before. The addresses gain `/w/<id>/`, and the web app shows no workspace switcher while a user is in only one workspace (PP-154).

Each milestone 2 ticket merges to main and ships in normal releases. With one workspace, the change is invisible. Anything that lets a second workspace exist (open signup, creating another workspace) waits for the isolation suite (PP-153). Inviting members into the one existing workspace doesn't, so it can ship early.

## Consequences

- Every repository method on a workspace's table changes its signature, and every caller with it. That's most of PP-148 to PP-150. The compiler finds the callers.
- The `AnyWorkspace` methods are where a leak can still happen. They're few, easy to search for, and covered by PP-153.
- Once the upgrade has run, a self-hoster sees one change, `/w/<id>/` in the address bar, until they invite someone. The owner of the upgrade's workspace is also the instance admin, so every screen they use today stays theirs.
- The upgrade can fail on duplicate push tokens or custom domains that slipped past the service. It fails before changing anything and names the records, and the release notes say how to fix them.
- Tag names are now per workspace, so an import into a workspace (PP-170) checks names there, not across the install.
- Probes are shared by every workspace, so every member sees the install's probe names and locations. That's the point on the cloud, where the probes are its regions. A self-hoster who wants probes that only one workspace sees waits for workspace probes.
- Background work reads a workspace's settings per monitor. That's one more lookup per check where a setting matters, and the cache in section 4 is the remedy if it shows.
- Instance admin is a second axis next to roles. It stays small: probes and the install settings in section 5.

Waiting tickets:

- **PP-146** (data model and upgrade): the tables in section 2, the instance admin flag, the indexes in section 6, the migration in section 12. SQL only.
- **PP-147** (workspace in every request): section 7, the role helper, API keys as admins of their workspace, the 404 and 403 rules in section 8.
- **PP-148, PP-149, PP-150** (scoping): section 3 per module, the same-workspace check for links, the settings split in section 5, the probe rules in section 10.
- **PP-151** (websocket): a room per workspace, checked joins.
- **PP-152** (account emails): unchanged. An instance-wide sender.
- **PP-153** (isolation suite): SQL only. It also searches the code for `AnyWorkspace` callers outside the list in section 3.
- **PP-154** (members and roles): the roles in section 1, no viewer in v1, `/w/<id>/` and the switcher. Creating another workspace waits for PP-153.
- **PP-155** (signup modes): the two modes in section 11.
- **PP-160, PP-161** (plan limits, per-workspace retention): section 4.
- **PP-91** (shared repository code): optional. Its helpers take the workspace ID if it lands first.
- **PP-286** (probes a workspace runs for itself): section 10.
- **PP-287**: keep `/metrics` off the public internet on the cloud (section 3).
