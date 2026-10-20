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
The init container that holds a pod until the migrate Job has applied every
migration. The api, producer, worker and ingester Deployments include it, so a
fresh install or an upgrade never starts them on a schema the migrations haven't
reached yet. The web Deployment doesn't use the database and has none.

It runs the migrate image, so the migrations it checks are the ones the Job
applies, and it reads the same ConfigMap and Secret as the app containers. It
asks `./bun db status`, which every migrate image has, every 2 seconds, and
ends when the unapplied migrations are empty. While PostgreSQL is down or the
migration tables don't exist yet the command fails, and that counts as not ready.
Each try prints a line, so `kubectl logs <pod> -c wait-for-migrations` shows why
the pod waits.

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
        sleep 2
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
