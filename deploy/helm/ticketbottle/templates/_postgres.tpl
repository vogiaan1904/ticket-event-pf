{{- /*
How a database-backed workload reaches its Postgres. Two paths:
  postgres.passwordArns empty -> the chart's password, in each service's Secret (k3s)
  postgres.passwordArns set   -> each host's password from Secrets Manager, mounted
                                 by the CSI driver under the pod's IRSA role (EKS)
See docs/design/eks-stateful-tier.md#credentials.
*/ -}}

{{- /* tb.pgSecretsManager is "true" on the Secrets Manager path. */ -}}
{{- define "tb.pgSecretsManager" -}}
{{- if .Values.postgres.passwordArns }}true{{ end -}}
{{- end -}}

{{- /* tb.pgRole names the host a service's database lives on. */ -}}
{{- define "tb.pgRole" -}}
{{- get (dict "user" "shared" "event" "shared" "temporal" "shared" "payment" "payment" "inventory" "inventory") . -}}
{{- end -}}

{{- /* tb.pgDsn is a service's DSN with the given password. Call with (dict "ctx" $ "svc" "user" "password" "...") */ -}}
{{- define "tb.pgDsn" -}}
postgresql://{{ .ctx.Values.postgres.user }}:{{ .password }}@{{ index .ctx.Values.postgres.hosts (include "tb.pgRole" .svc) }}:5432/ticketbottle_{{ .svc }}{{ include "tb.pgSsl" . }}
{{- end -}}

{{- /* tb.pgSsl is the DSN's TLS query, in each client's own dialect (0042):
       inventory (pgx)     require | disable
       node-postgres       no-verify, or nothing
       Prisma              nothing: it tries TLS first */ -}}
{{- define "tb.pgSsl" -}}
{{- $tls := .ctx.Values.postgres.tls -}}
{{- if eq .svc "inventory" }}?sslmode={{ ternary "require" "disable" $tls }}
{{- else if and $tls (eq (default "" .client) "node-pg") }}?sslmode=no-verify
{{- end -}}
{{- end -}}

{{- /* tb.saName is a ServiceAccount's name where the chart creates it, else empty. */ -}}
{{- define "tb.saName" -}}
{{- $sa := index .ctx.Values.serviceAccount .key -}}
{{- if $sa.create }}{{ $sa.name }}{{ end -}}
{{- end -}}

{{- /* The pieces below render only on the Secrets Manager path. Each takes (dict "ctx" $ "svc" "<svc>"). */ -}}

{{- define "tb.pgVolume" -}}
- name: pg-password
  csi:
    driver: secrets-store.csi.k8s.io
    readOnly: true
    volumeAttributes: { secretProviderClass: pg-{{ include "tb.pgRole" .svc }} }
{{- end -}}

{{- /* Mounted by the main container: the mount is what makes the driver sync pg-<host>. */ -}}
{{- define "tb.pgMount" -}}
- { name: pg-password, mountPath: /mnt/pg-password, readOnly: true }
{{- end -}}

{{- /* The env a service's Secret carries on k3s, with the password from pg-<host>. */ -}}
{{- define "tb.pgEnv" -}}
- name: DATABASE_PASSWORD
  valueFrom: { secretKeyRef: { name: pg-{{ include "tb.pgRole" .svc }}, key: password } }
{{- $dsn := include "tb.pgDsn" (dict "ctx" .ctx "svc" .svc "client" (default "" .client) "password" "$(DATABASE_PASSWORD)") }}
{{- if eq .svc "inventory" }}
- { name: POSTGRES_URL, value: {{ $dsn | quote }} }
{{- else }}
- { name: DATABASE_USERNAME, value: {{ .ctx.Values.postgres.user | quote }} }
- { name: DATABASE_URL, value: {{ $dsn | quote }} }
{{- end }}
{{- end -}}

{{- /* RDS creates no database of ours; each pod creates its own before it connects.
       Two pods racing on CREATE DATABASE both pass: the loser finds it made. */ -}}
{{- define "tb.pgEnsureDb" -}}
{{- $db := printf "ticketbottle_%s" .svc -}}
{{- $exists := printf "psql -tAc \"SELECT 1 FROM pg_database WHERE datname = '%s'\" | grep -q 1" $db -}}
- name: ensure-db
  image: {{ .ctx.Values.postgres.image }}
  env:
    - { name: PGHOST, value: {{ index .ctx.Values.postgres.hosts (include "tb.pgRole" .svc) | quote }} }
    - { name: PGUSER, value: {{ .ctx.Values.postgres.user | quote }} }
    - { name: PGDATABASE, value: postgres }
    - name: PGPASSWORD
      valueFrom: { secretKeyRef: { name: pg-{{ include "tb.pgRole" .svc }}, key: password } }
  command:
    - sh
    - -c
    - |
      until pg_isready; do echo waiting for postgres; sleep 2; done
      {{ $exists }} || psql -c 'CREATE DATABASE {{ $db }}' || {{ $exists }}
  resources: { requests: { cpu: 10m, memory: 32Mi } }
{{- end -}}
