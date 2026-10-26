{{/*
The image of a container: <repository>:<tag>. An empty tag means the chart's
appVersion, which is the release the chart was published with (`latest` in a
checkout), so one chart version runs one release. Every container and init
container takes its image from here, so a component's init container can't run
another tag than the component.

Use it with: include "peekaping.image" (dict "image" .Values.api.image "root" .)
*/}}
{{- define "peekaping.image" -}}
{{ .image.repository }}:{{ .image.tag | default .root.Chart.AppVersion }}
{{- end }}

{{/*
The name of the Secret that holds the local probe's token: the one in
probe.token.existingSecret, or the one this chart makes. The api reads the token
from it as PROBE_BOOTSTRAP_TOKEN and the probe pods as PROBE_TOKEN.
*/}}
{{- define "peekaping.probeTokenSecretName" -}}
{{ .Values.probe.token.existingSecret | default (printf "%s-probe-token" .Release.Name) }}
{{- end }}

{{/*
The key of the token in that Secret. The Secret this chart makes uses
PROBE_BOOTSTRAP_TOKEN, and so does probe.token.existingSecretKey by default.
*/}}
{{- define "peekaping.probeTokenSecretKey" -}}
{{ if .Values.probe.token.existingSecret }}{{ .Values.probe.token.existingSecretKey }}{{ else }}PROBE_BOOTSTRAP_TOKEN{{ end }}
{{- end }}

{{/*
The local probe's token, base64 encoded for the Secret this chart makes: the one
the Secret already holds in the cluster, so that an upgrade keeps it, otherwise 48
random characters. `helm template` and `helm install --dry-run` can't look at the
cluster, so they render a new token every time; only an install or an upgrade
that talks to the cluster keeps it. Set probe.token.existingSecret where the
chart is rendered without the cluster (Argo CD, for one).
*/}}
{{- define "peekaping.probeToken" -}}
{{- $token := "" -}}
{{- $existing := lookup "v1" "Secret" .Release.Namespace (printf "%s-probe-token" .Release.Name) -}}
{{- if $existing -}}
{{- if $existing.data -}}
{{- $token = index $existing.data "PROBE_BOOTSTRAP_TOKEN" | default "" -}}
{{- end -}}
{{- end -}}
{{- if not $token -}}
{{- $token = randAlphaNum 48 | b64enc -}}
{{- end -}}
{{- $token -}}
{{- end }}

{{/*
The init container that holds a pod until the migrate Job has applied every
migration. The api Deployment includes it, so a fresh install or an upgrade never
starts it on a schema the migrations haven't reached yet. The probe and web
Deployments don't use the database and have none.

It runs the migrate image, so the migrations it checks are the ones the Job
applies, and it reads the same ConfigMap and Secret as the app containers. It
asks `./bun db status`, which every migrate image has, every 2 seconds, and
ends when the unapplied migrations are empty. While PostgreSQL is down or the
migration tables don't exist yet the command fails, and that counts as not ready.
Each try prints a line, so `kubectl logs <pod> -c wait-for-migrations` shows why
the pod waits.

A DB_TYPE of `mongo` or `mongodb` in the Secret passes the render check below.
`./bun db status` refuses it with the error that says MongoDB storage was removed
and where to move the data, so the pod waits in Init and prints that error on each
try, and the migrate Job fails with the same one.

The shell is PID 1 of the container, and the kernel gives PID 1 no default
action for a signal, so without a trap it ignores SIGTERM and a pod stuck in Init
takes the whole grace period to die. The trap ends the loop at once, and the
sleep runs in the background and is waited for, because a shell runs a trap only
after its foreground command ends.

Put it under initContainers with: include "peekaping.waitForMigrations" . | nindent 8
*/}}
{{- define "peekaping.waitForMigrations" -}}
- name: wait-for-migrations
  image: {{ include "peekaping.image" (dict "image" .Values.migrate.image "root" .) }}
  imagePullPolicy: {{ .Values.migrate.image.pullPolicy }}
  command:
    - sh
    - -c
    - |
      trap 'exit 143' INT TERM
      while true; do
        if out=$(./bun db status 2>&1); then
          unapplied=$(echo "$out" | grep '^unapplied migrations:' || true)
          if [ "$unapplied" = "unapplied migrations: empty" ]; then
            echo "All migrations are applied."
            exit 0
          fi
          echo "Waiting for the migrate Job to apply the migrations. $unapplied"
        else
          echo "Waiting for the database and its migration tables: $(echo "$out" | tail -n 1)"
        fi
        sleep 2 &
        wait $!
      done
  envFrom:
    - configMapRef:
        name: {{ .Release.Name }}-config
    {{- if .Values.config.existingSecret }}
    - secretRef:
        name: {{ .Values.config.existingSecret }}
    {{- end }}
  resources:
    requests:
      cpu: {{ .Values.migrate.resources.requests.cpu }}
      memory: {{ .Values.migrate.resources.requests.memory }}
    limits:
      cpu: {{ .Values.migrate.resources.limits.cpu }}
      memory: {{ .Values.migrate.resources.limits.memory }}
  securityContext:
    allowPrivilegeEscalation: false
    readOnlyRootFilesystem: true
    runAsNonRoot: true
    privileged: false
    capabilities:
      drop: ["ALL"]
{{- end }}

{{/*
Fails the render when config.env.DB_TYPE is sqlite, mongo or mongodb, in any
case. The chart runs Peekaping in cluster mode, and it has no volume for a SQLite
file: each pod and the migrate Job would get an empty file of its own, so the
migrations and each replica's writes would land in separate databases. The
single-mode bundle image runs Peekaping on SQLite in one process, so the message
sends the reader there. MongoDB storage was removed (ADR 0001): every binary
stops on it with config.ErrMongoStorageRemoved, so the message says the same and
sends the reader to the guide for moving to PostgreSQL.

It reads config.env only. Helm doesn't read Secrets at render time, so a DB_TYPE
in config.existingSecret isn't seen here. The api warns about SQLite in cluster
mode when it starts, which covers that case for SQLite.

Include it from a template that always renders: include "peekaping.requirePostgres" .
*/}}
{{- define "peekaping.requirePostgres" -}}
{{- $dbType := toString (get (.Values.config.env | default (dict)) "DB_TYPE") -}}
{{- if eq (lower $dbType) "sqlite" -}}
{{- fail (printf "config.env.DB_TYPE is %q, but the chart supports PostgreSQL only: it runs Peekaping in cluster mode and has no volume for a SQLite file, so each pod and the migrate Job would get a separate database. For SQLite, run the single-mode bundle image (0xfurai/peekaping-bundle-sqlite) or the SQLite Docker Compose stack instead: https://docs.peekaping.com/self-hosting/docker-with-sqlite" $dbType) -}}
{{- end -}}
{{- if or (eq (lower $dbType) "mongo") (eq (lower $dbType) "mongodb") -}}
{{- fail (printf "config.env.DB_TYPE is %q, but MongoDB storage was removed. Move this install's data to PostgreSQL with \"bun db import-mongo\", then set config.env.DB_TYPE to postgres. See https://docs.peekaping.com/self-hosting/docker-with-mongo#move-to-sqlite-or-postgresql" $dbType) -}}
{{- end -}}
{{- end }}
