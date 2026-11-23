#!/usr/bin/env bash
# Sourced. pg DB [psql args...] runs psql against DB wherever it lives:
#   StatefulSet postgres present (k3s)  -> exec into it
#   absent (EKS, postgres.enabled=false) -> a client pod, against DB's RDS instance
# The client pod reads each password from the pg-<host> Secret the CSI driver syncs.
PG_NS=ticketbottle

# pg_host DB names the instance DB lives on, as the chart's tb.pgRole does.
pg_host() {
  case "$1" in
    ticketbottle_payment) echo payment ;;
    ticketbottle_inventory) echo inventory ;;
    *) echo shared ;;
  esac
}

pg_client() {
  kubectl -n $PG_NS get pod pg-client >/dev/null 2>&1 && return
  local hosts
  hosts=$(helm get values tb -n $PG_NS -o json | jq -r '.postgres.hosts')
  kubectl -n $PG_NS apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata: { name: pg-client, namespace: $PG_NS }
spec:
  containers:
    - name: psql
      image: postgres:15-alpine
      command: [sleep, infinity]
      env:
        - { name: PGUSER, value: root }
        - { name: HOST_shared, value: "$(jq -r .shared <<<"$hosts")" }
        - { name: HOST_payment, value: "$(jq -r .payment <<<"$hosts")" }
        - { name: HOST_inventory, value: "$(jq -r .inventory <<<"$hosts")" }
      volumeMounts:
        - { name: shared, mountPath: /pg/shared, readOnly: true }
        - { name: payment, mountPath: /pg/payment, readOnly: true }
        - { name: inventory, mountPath: /pg/inventory, readOnly: true }
  volumes:
    - { name: shared, secret: { secretName: pg-shared } }
    - { name: payment, secret: { secretName: pg-payment } }
    - { name: inventory, secret: { secretName: pg-inventory } }
EOF
  kubectl -n $PG_NS wait --for=condition=Ready pod/pg-client --timeout=2m >/dev/null
}

pg() {
  local db=$1; shift
  if kubectl -n $PG_NS get statefulset postgres >/dev/null 2>&1; then
    kubectl -n $PG_NS exec statefulset/postgres -- psql -U root -d "$db" "$@"
    return
  fi
  pg_client
  local h; h=$(pg_host "$db")
  kubectl -n $PG_NS exec pg-client -- sh -c \
    'h=$1; shift; PGPASSWORD=$(cat /pg/$h/password) exec psql -h "$(printenv HOST_$h)" "$@"' \
    _ "$h" -d "$db" "$@"
}
