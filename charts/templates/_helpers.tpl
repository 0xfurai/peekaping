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

A MongoDB install has no SQL migrations. The migrate Job's script
(apps/server/scripts/run-migrations.sh) skips them for DB_TYPE `mongo` and
`mongodb`, and `./bun db status` has no MongoDB support, so it would fail
forever. For those two values the script prints a line and ends at once. DB_TYPE
comes from the ConfigMap or the Secret, so the script reads it at run time. Without
this, the pods of an existing MongoDB install would never leave Init after an
upgrade (ADR 0001 allows a fix for an app that doesn't start).

The shell is PID 1 of the container, and the kernel gives PID 1 no default
action for a signal, so without a trap it ignores SIGTERM and a pod stuck in Init
takes the whole grace period to die. The trap ends the loop at once, and the
sleep runs in the background and is waited for, because a shell runs a trap only
after its foreground command ends.

Put it under initContainers with: include "peekaping.waitForMigrations" . | nindent 8
*/}}
{{- define "peekaping.waitForMigrations" -}}
- name: wait-for-migrations
  image: {{ .Values.migrate.image.repository }}:{{ .Values.migrate.image.tag }}
  imagePullPolicy: {{ .Values.migrate.image.pullPolicy }}
  command:
    - sh
    - -c
    - |
      case "$DB_TYPE" in
        mongo|mongodb)
          echo "MongoDB has no SQL migrations to wait for."
          exit 0
          ;;
      esac
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
