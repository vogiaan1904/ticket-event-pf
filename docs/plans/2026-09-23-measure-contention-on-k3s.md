# Re-measure inventory contention on the k3s box

**Status: NOT STARTED.**

**Goal:** Establish whether the reserve ceiling in
`services/inventory-svc/CLAUDE.md` (~1300 reserves/sec, peaking at four
concurrent reservers) is a property of the code or of a laptop, and whether the
sharding result from `2026-09-22-inventory-contention-benchmark.md` — 8 rows
bought only ~2x — survives real storage.

Nothing about `SKIP LOCKED` gets built until this runs. The current numbers come
from Docker Desktop on Mac, which showed 0.5-3.4ms per statement. That is one to
two orders of magnitude worse than local Postgres should be, and high enough
that per-statement latency, not the row lock, may be what the curve is shaped
by.

## What is being measured, and where

The three tests in `internal/services/reservation_contention_test.go`, against
the in-cluster `postgres` StatefulSet on the k3s box. That Postgres writes to a
PVC on `local-path`, so its fsync lands on the instance's EBS root volume —
which is the storage the claim is actually about.

## Decisions

**Cross-compile the test binary; do not install Go on the box.** `go test -c`
with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` produces a statically linked ELF
(verified: 20MB, no dynamic deps). `inventory-svc` vendors its modules and the
Postgres driver is pure Go (`gorm.io/driver/postgres` over pgx v5), so the build
needs no network and the box needs no toolchain.
*Not taken:* installing Go and rsyncing the source. Costs a toolchain install
plus a build on 2 shared vCPU, and buys nothing.

**Connect by pod IP from the host network namespace. Never `kubectl
port-forward`.** port-forward is a userspace TCP relay through the API server —
the same class of contamination as running the test over the SSH tunnel, which
is the entire reason this is being re-measured. Flannel routes the pod CIDR on
the host, so a direct dial is kernel routing only.

**Scale the app tier to zero first.** The box is a t3.large: 2 vCPU shared by
Postgres, Temporal, Redpanda, Redis and ten app pods. The claim under test is
about a *row lock*; a number that cannot be attributed to the lock does not
support it.
*Costs:* the measurement no longer reflects the system under realistic load. If
the question later becomes "what does the whole box sustain", that is a second
run with the apps up, and a different number.

**Use a throwaway database.** `newTestDB`'s `t.Cleanup` runs
`TRUNCATE reservation, ticket_class RESTART IDENTITY CASCADE`. Pointing
`TEST_POSTGRES_URL` at `ticketbottle_inventory` destroys live inventory.

## Steps

Build locally:

```bash
cd services/inventory-svc
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go test -mod=vendor -c -o /tmp/inventory-bench ./internal/services/
```

Open the tunnel and get kubeconfig (`make -C deploy k3s-kubeconfig`), then copy
the binary to the box and work from there — every remaining command runs on the
instance, not the laptop.

On the box:

1. Record the baseline:
   `kubectl get deploy,sts -n ticketbottle -o wide > /tmp/before.txt`
2. Scale everything except Postgres to zero. The apps and both Temporal
   workloads are Deployments; Redpanda and Redis are StatefulSets, so they need
   the second command or they keep burning vCPU:

   ```bash
   kubectl scale -n ticketbottle --replicas=0 $(kubectl get deploy -n ticketbottle -o name | tr '\n' ' ')
   kubectl scale -n ticketbottle --replicas=0 statefulset/redpanda statefulset/redis
   ```

   Do not touch `statefulset/postgres`.
3. Create the test database:
   `kubectl exec -n ticketbottle postgres-0 -- psql -U root -c 'CREATE DATABASE ticketbottle_inventory_test'`
4. Take the pod IP: `kubectl get pod -n ticketbottle postgres-0 -o jsonpath='{.status.podIP}'`
5. Run each of the three tests three times, recording the median:

```bash
TEST_POSTGRES_URL="postgresql://root:<pw>@<podIP>:5432/ticketbottle_inventory_test?sslmode=disable" \
INVENTORY_BENCH=1 \
./inventory-bench -test.run 'TestReserve(Throughput|UnderContention)' -test.v -test.count=1
```

The password is `secrets.postgresPassword` from the gitignored secrets file.

6. Restore from `/tmp/before.txt` — scale the Deployments and the two
   StatefulSets back to their recorded replica counts — then
   `DROP DATABASE ticketbottle_inventory_test`.

## What to record

Into this file, then into `services/inventory-svc/CLAUDE.md` if the numbers
move:

- reserves/sec at 1, 4 and 16 workers, one hot class
- the spread-across-classes curve at 1, 2, 4 and 8 classes
- per-statement latency, for comparison against the laptop's 0.5-3.4ms

## The decision this feeds

If 8 classes still buys only ~2x on real storage, sharding a hot class is not
worth building and the ceiling is somewhere other than the row lock — find it
before writing code. If it scales closer to linear, the laptop was the
bottleneck and `SKIP LOCKED` sharding becomes worth designing.

Either way the answer is evidence, not the commit message of `35e864f`.

## Known limits of this run

- One Postgres pod on one burstable instance. t3 CPU credits mean a long run can
  be throttled mid-measurement; check `CPUCreditBalance` if numbers drift
  downward across repeats.
- `confirmReservationTx` takes the same hot row and is still unmeasured. Every
  successful purchase pays it, so the real per-class ceiling is below whatever
  reserve alone reports.
