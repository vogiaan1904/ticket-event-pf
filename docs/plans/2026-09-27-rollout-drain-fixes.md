# Rollout drain fixes — plan

**Status: NOT STARTED.** Written 2026-09-27. Implements
[0015](../decisions/0015-app-pods-sleep-before-sigterm.md) and
[0016](../decisions/0016-the-gateway-drains-for-up-to-65s.md), from the findings of
[the rollout drain measurement](2026-09-24-rollout-drain-measurement.md#results).

**Goal:** A rollout on k3s refuses no request and cuts no request: pods with a Service
keep serving until kube-proxy has dropped them, and a stopping gateway sheds its
keep-alive clients instead of waiting for SIGKILL.

**Architecture:** The chart gives every app with a Service a native
`lifecycle.preStop.sleep` of 5s, and gives the gateway a grace period of
sleep + drain + 5, all from one `shutdown` block in `values.yaml`. The gateway gains a
small drain: from SIGTERM on, every response carries `Connection: close`, idle sockets
close at once, and whatever is still open at the deadline is cut. A trace on the
cluster confirms why one refused reconnect cost ~40s; the crash case stays open.

**Tech stack:** Helm v4.2.3 (`azure/setup-helm` in CI); Kubernetes `preStop.sleep`
(k3s v1.36, EKS at its default version); NestJS 11.1.6 on Express; Node 20 in the
image and in CI; Jest 29 with ts-jest; grpc-js 1.13.4, traced only; k6 0.54 through
`deploy/scripts/rollout-drain.sh`.

**Spec:** [the measurement's Results](2026-09-24-rollout-drain-measurement.md#results),
[0015](../decisions/0015-app-pods-sleep-before-sigterm.md),
[0016](../decisions/0016-the-gateway-drains-for-up-to-65s.md).

## Already verified

Every code block below was run before this plan was written:

- **The chart edits** render with helm v4.2.3. The new assertion passes on them and fails
  on today's chart and on four mutants: no sleep on `payment-webhook`, a hard-coded
  grace period, a sleep on every app, and a hard-coded drain value.
- **The drain tests** pass under the gateway's own Jest and ts-jest, 3 of 3. Their
  JavaScript prototype fails on the mutant each test exists to catch, the same way
  on Node 20.17 (the image's major version) and Node 24:
  - no `Connection: close` — test 1 hangs;
  - every socket cut at once — test 2 fails;
  - no deadline — test 3 hangs.
- **The helper and its `main.ts` wiring** type-check under `--strict`.
- **grpc-js on its own** recovers the moment the port listens again (48 failures over a
  1.0s refusal), and 1.13.4 has no connect timeout.
- **A local boot cannot show Finding 2.** `GET /api` answers in ~1ms, so every socket is
  idle at SIGTERM and today's build exits 0 within 52ms. The unit tests and the k3s run
  are the evidence; the local boot in Task 2 is a smoke test only.

## Global constraints

- Values, from the decisions: `shutdown.preStopSleepSeconds: 5`,
  `shutdown.gatewayDrainSeconds: 65`, and the gateway's `terminationGracePeriodSeconds`
  = 5 + 65 + 5 = 75.
- The sleep goes on exactly `app-gateway`, `user-service`, `event-service`,
  `order-service`, `payment-service`, `waitroom-service`, `inventory-service` and
  `payment-webhook` — never `order-consumer` or `outbox-relay`.
- The chart is authored once and targets are overlays: parameterise, never fork a
  manifest per target.
- A change to the rendered chart commits its regenerated golden
  (`deploy/helm/ticketbottle/tests/render-golden.sh`) in the same commit, and
  `tests/assert-render.sh` passes.
- Comment budget (root `CLAUDE.md`): inline ≤3 lines; a doc comment ≤5 lines, the
  first line one sentence; no history, no restating the code.
- New TS files follow `docs/design/ts-layout.md`: a shared helper lives in
  `src/shared/utils/`, named `*.util.ts`.
- Before any TS build or test in a service: `rm -rf node_modules && npm ci`. The
  on-disk `.bin` links can be stale.
- Deploy with `make -C deploy k3s-deploy`, which deploys the immutable `sha-` build.
  Never set `:dev` again by hand.
- Stop the k3s box at the end of the session; EKS is not touched.

## Review focus

1. **The API server accepts the Deployment but drops `preStop.sleep`.** An older or
   gated API server discards a field it does not know, and every render test stays
   green while nothing sleeps. → Task 3, step 5, reads the live objects.
2. **Someone raises `order.createTimeout` past the gateway's drain.** From then on every
   gateway stop cuts slow checkouts, silently. → Task 1's assertion compares the two
   rendered values.
3. **Trace settings outlive the trace.** An env var added with `kubectl set env` is not
   in the chart, and a `helm upgrade` does not remove a field the chart never set. →
   Task 3, step 3, removes it, and step 5 checks that it is gone.
4. **A response already streaming when the drain begins** cannot be given the header.
   Its socket closes at the client's next request, or at Node's 5s keep-alive
   timeout. Accepted in 0016; it is bounded well inside 65s. No test.
5. **EKS behind the ALB.** The ALB deregisters targets on its own, slower clock, so 5s
   may not be enough there. Out of scope by decision; it stays open in the measurement
   plan.

---

### Task 1: Sleep before SIGTERM, and size the gateway's grace period

→ [0015](../decisions/0015-app-pods-sleep-before-sigterm.md), [0016](../decisions/0016-the-gateway-drains-for-up-to-65s.md)

**Files:**
- Modify: `deploy/helm/ticketbottle/tests/assert-render.sh` (insert before the goldens-secret block, which begins `# The goldens are committed`)
- Modify: `deploy/helm/ticketbottle/values.yaml:155` (after `replicas: {}`)
- Modify: `deploy/helm/ticketbottle/templates/apps/_appservice.tpl`
- Modify: `deploy/helm/ticketbottle/templates/apps/gateway.yaml`
- Modify: `deploy/helm/ticketbottle/templates/apps/payment-events.yaml`
- Modify: `deploy/helm/ticketbottle/templates/apps/config.yaml:143` (gateway-config)
- Regenerate: `deploy/helm/ticketbottle/tests/golden/values-k3s.yaml`
- Commit alongside: `docs/decisions/0015-*.md`, `docs/decisions/0016-*.md`, `docs/decisions/README.md`

**Interfaces:**
- Produces: the value `shutdown.gatewayDrainSeconds`, rendered into `gateway-config` as
  `SHUTDOWN_DRAIN_SECONDS` (a string of whole seconds). Task 2 reads it.
- Produces: the template helper `tb.preStopSleep`, taking the root context.

- [ ] **Step 1: Write the failing assertions.** Insert into `tests/assert-render.sh`,
  before `# The goldens are committed`:

```bash
# Apps a Service routes to sleep before SIGTERM, so kube-proxy drops them before
# they close their port; the gateway's grace period covers its drain deadline.
# Distinct values, so a template that hard-codes 5, 65 or 75 cannot pass.
helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" \
  --set shutdown.preStopSleepSeconds=7 --set shutdown.gatewayDrainSeconds=11 > "$actual"
deployment() { awk -v n="$1" 'BEGIN { RS = "\n---\n" } /kind: Deployment/ && $0 ~ ("\n  name: " n "\n")' "$actual"; }
for d in app-gateway user-service event-service order-service payment-service \
         waitroom-service inventory-service payment-webhook; do
  grep -q "sleep: { seconds: 7 }" <<<"$(deployment "$d")" || fail "$d does not sleep before SIGTERM"
done
for d in order-consumer outbox-relay; do
  grep -q "preStop" <<<"$(deployment "$d")" && fail "$d sleeps, though no Service routes requests to it"
done
grep -q "terminationGracePeriodSeconds: 23$" <<<"$(deployment app-gateway)" \
  || fail "app-gateway's grace period is not sleep + drain + 5"
grep -q "terminationGracePeriodSeconds" <<<"$(deployment event-service)" \
  && fail "event-service has its own grace period, though only the gateway drains long requests"
grep -q 'SHUTDOWN_DRAIN_SECONDS: "11"' "$actual" || fail "gateway-config does not carry the drain deadline"
echo "OK  apps with a Service sleep before SIGTERM; the gateway's grace covers its drain"

# A gateway stop must outlast the longest checkout, or every rollout cuts some.
helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" > "$actual"
create=$(sed -n 's/^  ORDER_CREATE_TIMEOUT: "\([0-9]*\)\([sm]\)"$/\1 \2/p' "$actual")
drain=$(sed -n 's/^  SHUTDOWN_DRAIN_SECONDS: "\([0-9]*\)"$/\1/p' "$actual")
[ -n "$create" ] && [ -n "$drain" ] || fail "cannot read ORDER_CREATE_TIMEOUT (Ns or Nm) or SHUTDOWN_DRAIN_SECONDS"
read -r n unit <<<"$create"; [ "$unit" = m ] && n=$((n * 60))
[ "$drain" -gt "$n" ] || fail "the gateway drains for ${drain}s, but a checkout may run ${n}s"
echo "OK  the gateway's drain outlasts a checkout (${drain}s > ${n}s)"
```

- [ ] **Step 2: Run it and watch it fail.**

Run: `deploy/helm/ticketbottle/tests/assert-render.sh`
Expected: `FAIL: app-gateway does not sleep before SIGTERM`.

- [ ] **Step 3: Add the values.** In `values.yaml`, after `replicas: {}` and its blank line:

```yaml
# How an app pod stops. See docs/decisions/0015 and 0016.
shutdown:
  # Seconds an app with a Service keeps serving after it leaves its endpoints.
  preStopSleepSeconds: 5
  # The gateway's drain deadline; its grace period is sleep + drain + 5.
  gatewayDrainSeconds: 65
```

- [ ] **Step 4: Add the sleep and the grace period to the shared template.** In
  `_appservice.tpl`, put the helper above `{{- define "tb.appService" -}}`:

```yaml
{{- /* Keeps serving while kube-proxy drops the pod from its Service.
       See docs/decisions/0015. */}}
{{- define "tb.preStopSleep" -}}
lifecycle:
  preStop:
    sleep: { seconds: {{ .Values.shutdown.preStopSleepSeconds }} }
{{- end -}}

```

Then, directly after the `serviceAccountName` block (`{{- end }}` of `if .serviceAccount`):

```yaml
      {{- with .drainSeconds }}
      terminationGracePeriodSeconds: {{ add $.Values.shutdown.preStopSleepSeconds . 5 }}
      {{- end }}
```

And directly after the `ports:` block (`{{- end }}` of `if gt (int .port) 0`):

```yaml
          {{- if .svcName }}
          {{- include "tb.preStopSleep" $ | nindent 10 }}
          {{- end }}
```

- [ ] **Step 5: Wire the gateway and `payment-webhook`.** In `gateway.yaml`, append
  `"drainSeconds" .Values.shutdown.gatewayDrainSeconds` to the `dict`, after
  `"metricsPort" 2112`. In `payment-events.yaml`, directly after
  `          ports: [{ containerPort: 8080 }]`:

```yaml
          {{- include "tb.preStopSleep" . | nindent 10 }}
```

In `config.yaml`, in `gateway-config`, after `SALT_ROUND: "10"`:

```yaml
  SHUTDOWN_DRAIN_SECONDS: {{ .Values.shutdown.gatewayDrainSeconds | quote }}
```

- [ ] **Step 6: Regenerate the golden and run the assertions.**

Run: `deploy/helm/ticketbottle/tests/render-golden.sh && deploy/helm/ticketbottle/tests/assert-render.sh`
Expected: both new `OK` lines, then `all render assertions passed`. Check the golden
diff: `lifecycle` on eight Deployments, `terminationGracePeriodSeconds: 75` on
`app-gateway` only, and `SHUTDOWN_DRAIN_SECONDS: "65"` in `gateway-config`.

- [ ] **Step 7: Prove each assertion can go red.** Make one change at a time, run
  `assert-render.sh`, see the named failure, then revert with `git checkout -- <file>`:

| Change | Expected failure |
|---|---|
| Delete the `tb.preStopSleep` line in `payment-events.yaml` | `payment-webhook does not sleep before SIGTERM` |
| Replace `{{ add $.Values.shutdown.preStopSleepSeconds . 5 }}` with `75` | `app-gateway's grace period is not sleep + drain + 5` |
| Set `order.createTimeout: 70s` in `values.yaml` | `the gateway drains for 65s, but a checkout may run 70s` |

- [ ] **Step 8: Commit.**

```bash
python3 docs/decisions/index.py && python3 docs/decisions/index.py --check
git add deploy/helm/ticketbottle docs/decisions/0015-app-pods-sleep-before-sigterm.md \
  docs/decisions/0016-the-gateway-drains-for-up-to-65s.md docs/decisions/README.md
git commit -m "feat(deploy): sleep before SIGTERM on apps with a Service, and give the gateway a 75s grace period"
```

---

### Task 2: A stopping gateway sheds its keep-alive clients

→ [0016](../decisions/0016-the-gateway-drains-for-up-to-65s.md)

**Files:**
- Create: `services/api-gateway/src/shared/utils/http-drain.util.ts`
- Test: `services/api-gateway/src/shared/utils/http-drain.util.spec.ts`
- Modify: `services/api-gateway/src/main.ts`

**Interfaces:**
- Consumes: `SHUTDOWN_DRAIN_SECONDS` from the environment (Task 1), whole seconds.
- Produces: `createHttpDrain(): HttpDrain`, where
  `HttpDrain.middleware(req: IncomingMessage, res: ServerResponse, next: () => void): void`
  and `HttpDrain.begin(server: http.Server, deadlineMs: number): void`.

- [ ] **Step 1: Reinstall.** `cd services/api-gateway && rm -rf node_modules && npm ci`

- [ ] **Step 2: Write the failing test.** Create `src/shared/utils/http-drain.util.spec.ts`:

```ts
import * as http from 'http';
import { AddressInfo } from 'net';
import { createHttpDrain } from './http-drain.util';

type Handler = (req: http.IncomingMessage, res: http.ServerResponse) => void;

// serve runs handler behind a drain, with a keep-alive client of up to four sockets.
async function serve(handler: Handler) {
  const drain = createHttpDrain();
  const server = http.createServer((req, res) => drain.middleware(req, res, () => handler(req, res)));
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  // Read once: server.address() is null after close().
  const { port } = server.address() as AddressInfo;
  const agent = new http.Agent({ keepAlive: true, maxSockets: 4 });
  const get = () =>
    new Promise<http.IncomingMessage>((resolve, reject) => {
      http
        .get({ host: '127.0.0.1', port, agent }, (res) => {
          res.resume();
          res.on('end', () => resolve(res));
        })
        .on('error', reject);
    });
  const stop = (deadlineMs: number) => {
    const started = Date.now();
    drain.begin(server, deadlineMs);
    return new Promise<number>((resolve) => server.close(() => resolve(Date.now() - started)));
  };
  return { get, stop, agent };
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe('createHttpDrain', () => {
  it('tells keep-alive clients with a request in flight to reconnect, then closes', async () => {
    let holding = true;
    const held: Array<() => void> = [];
    const { get, stop, agent } = await serve((_req, res) => {
      if (holding) held.push(() => res.end('ok'));
      else res.end('ok');
    });
    let toldToLeave = 0;
    const client = async () => {
      for (;;) {
        const res = await get();
        if (res.headers.connection === 'close') {
          toldToLeave++;
          return;
        }
      }
    };
    const clients = [client(), client(), client(), client()];
    while (held.length < 4) await sleep(5);

    const stopped = stop(10_000);
    holding = false;
    held.forEach((respond) => respond());
    await Promise.all(clients);

    expect(toldToLeave).toBe(4);
    expect(await stopped).toBeLessThan(1_000);
    agent.destroy();
  });

  it('lets a request in flight finish', async () => {
    const { get, stop, agent } = await serve((_req, res) => setTimeout(() => res.end('ok'), 300));
    const pending = get();
    await sleep(50);

    const stopped = stop(10_000);
    const res = await pending;
    agent.destroy();

    expect(res.statusCode).toBe(200);
    await stopped;
  });

  it('cuts a request still running at the deadline', async () => {
    const { get, stop, agent } = await serve(() => {});
    const pending = get().then(
      () => 'answered',
      (err: NodeJS.ErrnoException) => err.code,
    );
    await sleep(50);

    const ms = await stop(200);

    expect(await pending).toBe('ECONNRESET');
    expect(ms).toBeLessThan(1_000);
    agent.destroy();
  });
});
```

Test 1 is Finding 2 itself. All four sockets are busy when the drain begins, so none
of them is idle to close, and only the header ends the clients' loop.

- [ ] **Step 3: Run it and watch it fail.**

Run: `npx jest src/shared/utils/http-drain.util.spec.ts`
Expected: FAIL, `Cannot find module './http-drain.util'`.

- [ ] **Step 4: Write the helper.** Create `src/shared/utils/http-drain.util.ts`:

```ts
import type { IncomingMessage, Server, ServerResponse } from 'http';

// HttpDrain lets a stopping HTTP server shed keep-alive clients instead of waiting on them.
// See docs/decisions/0016-the-gateway-drains-for-up-to-65s.md.
export interface HttpDrain {
  middleware(req: IncomingMessage, res: ServerResponse, next: () => void): void;
  begin(server: Server, deadlineMs: number): void;
}

// createHttpDrain returns a drain whose middleware must run on every request.
export function createHttpDrain(): HttpDrain {
  let draining = false;
  return {
    middleware(_req, res, next) {
      // Why: server.close() waits on every open socket, and a busy keep-alive
      // client never lets its socket go idle. The header makes it reconnect.
      if (draining) res.setHeader('Connection', 'close');
      next();
    },
    begin(server, deadlineMs) {
      draining = true;
      server.closeIdleConnections();
      setTimeout(() => server.closeAllConnections(), deadlineMs).unref();
    },
  };
}
```

- [ ] **Step 5: Run it and watch it pass.**

Run: `npx jest src/shared/utils/http-drain.util.spec.ts`
Expected: 3 passed, in about 1s of test time.

- [ ] **Step 6: Prove each test can go red.** Make one change at a time, run the spec,
  then revert:

| Change in `http-drain.util.ts` | Expected |
|---|---|
| Delete the `if (draining) res.setHeader(...)` line | Test 1 exceeds Jest's 5s timeout |
| Add `server.closeAllConnections();` as the first line of `begin` | Test 2 fails with `socket hang up` |
| Delete the `setTimeout(...)` line | Test 3 exceeds the timeout |

- [ ] **Step 7: Wire it into `main.ts`.** Add the import beside the metrics import:

```ts
import { createHttpDrain } from './shared/utils/http-drain.util';
```

Directly after `const app = await NestFactory.create<NestExpressApplication>(AppModule);`:

```ts
  const drain = createHttpDrain();
  app.use(drain.middleware);
```

Beside `const metricsPort = ...`:

```ts
  const drainMs = Number(process.env.SHUTDOWN_DRAIN_SECONDS || 25) * 1000;
```

And in the signal handler, before `await app.close();`:

```ts
      drain.begin(app.getHttpServer(), drainMs);
```

`drain.begin` must come first: `app.close()` resolves only when `server.close()` does,
and that is what the drain lets finish.

- [ ] **Step 8: Build, test, and boot.**

Run: `npm run build && npm test`
Expected: the build succeeds, and every suite passes, including the three new tests.

Then boot the build as a smoke test for the wiring (it cannot show Finding 2 locally;
see *Already verified*). Run from `services/api-gateway`, where its `.env` sets port 3000:

```bash
(SERVER_METRICS_PORT=2999 node dist/main.js > /tmp/gw.log 2>&1; echo "EXIT=$?" >> /tmp/gw.log) &
until curl -sf -o /dev/null localhost:3000/api; do sleep 0.5; done
kill -TERM "$(pgrep -f '^node dist/main.js')"; sleep 2; tail -1 /tmp/gw.log
```

Expected: `EXIT=0`. Redis connection errors in the log are expected without Redis.

- [ ] **Step 9: Commit.**

```bash
git add services/api-gateway/src/shared/utils/http-drain.util.ts \
  services/api-gateway/src/shared/utils/http-drain.util.spec.ts services/api-gateway/src/main.ts
git commit -m "fix(gateway): tell keep-alive clients to reconnect when draining, and cut what is left at a deadline"
```

---

### Task 3: On k3s — trace, deploy, re-run the matrix, record

→ [0015](../decisions/0015-app-pods-sleep-before-sigterm.md), [0016](../decisions/0016-the-gateway-drains-for-up-to-65s.md)

**Files:**
- Modify: this plan (*Results*, and the status line)
- Modify: `docs/plans/2026-09-24-rollout-drain-measurement.md` (status line)
- Modify: `docs/decisions/0015-*.md`, `docs/decisions/0016-*.md` (accept, add *Outcome*), `docs/decisions/README.md`
- Modify: `CLAUDE.md` (the decision register)

**Interfaces:**
- Consumes: Tasks 1 and 2 committed on `dev`; `deploy/scripts/rollout-drain.sh` and
  `deploy/loadtest/rollout-probe*.{js,yaml}`, from the measurement. These were
  uncommitted on 2026-09-27; commit them with the measurement plan before step 1, or
  the run is not reproducible from the repository.

- [ ] **Step 1: Build the images.** Pushing `dev` publishes the branch; confirm with the
  architect first. Then `git push origin dev`, and wait for `build-push-ecr` on the new
  commit:

```bash
gh run list --branch dev --workflow build-push-ecr --limit 1
gh run watch <run-id> --exit-status
```

- [ ] **Step 2: Start the box.** `make -C deploy start-ec2-k3s`, open the tunnel it
  prints, then `make -C deploy k3s-kubeconfig && export KUBECONFIG=/tmp/k3s.yaml`. Every
  pod in `ticketbottle` must be Running before you go on.

- [ ] **Step 3: Trace one event-service rollout on the build still running** (no sleep
  yet — the trace is of the failure):

```bash
kubectl -n ticketbottle set env deploy/app-gateway GRPC_TRACE=subchannel GRPC_VERBOSITY=DEBUG
kubectl -n ticketbottle rollout status deploy/app-gateway
OUT=$(mktemp -d) deploy/scripts/rollout-drain.sh rollouts event-service 1
kubectl -n ticketbottle logs deploy/app-gateway --since=10m | grep ' subchannel | ' | grep ':50053 .* -> '
kubectl -n ticketbottle set env deploy/app-gateway GRPC_TRACE- GRPC_VERBOSITY-
```

If the runner reports no failures, run the middle two commands again: on 2026-09-25,
one rollout in five missed the race. Each trace line reads
`... | subchannel | (N) <ip>:50053 <STATE> -> <STATE>`. Record the transitions and
their times under *Results*, and compare them with the 503 window:

| Transitions inside the 503 window | Reading |
|---|---|
| One `CONNECTING -> TRANSIENT_FAILURE` (ECONNREFUSED), then one `CONNECTING` lasting ~40s, ending `-> READY` | A connection attempt stuck on the vanishing pod IP. It confirms 0015's reading, and a crash gets the same ~40s. |
| Many `CONNECTING -> TRANSIENT_FAILURE` at growing intervals | Backoff, not a stuck connect. Record it; the crash-case row then names backoff as its lever. |

- [ ] **Step 4: Deploy the fixes.** `make -C deploy k3s-deploy`

- [ ] **Step 5: Check the live objects, not the render.**

```bash
kubectl -n ticketbottle get deploy -o jsonpath='{range .items[*]}{.metadata.name}{" sleep="}{.spec.template.spec.containers[0].lifecycle.preStop.sleep.seconds}{" grace="}{.spec.template.spec.terminationGracePeriodSeconds}{" env="}{.spec.template.spec.containers[0].env}{"\n"}{end}'
kubectl -n ticketbottle get cm gateway-config -o jsonpath='{.data.SHUTDOWN_DRAIN_SECONDS}{"\n"}'
```

Expected:
- `sleep=5` on the eight apps and empty on `order-consumer`, `outbox-relay` and the two
  Temporal Deployments;
- `grace=75` on `app-gateway`, and 30 everywhere else;
- `env=` empty on `app-gateway`, so no trace setting survived;
- `65` from the ConfigMap;
- every app image is the new `sha-`.

If `sleep` is empty on the eight apps, the API server dropped the field. Stop here and
bring it to the architect.

- [ ] **Step 6: Positive controls.** A zero is meaningless from a probe that cannot fail.

```bash
OUT=$(mktemp -d) deploy/scripts/rollout-drain.sh control app-gateway
OUT=$(mktemp -d) deploy/scripts/rollout-drain.sh control event-service
```

Expected: failures on both. With one replica the sleep only delays the outage, since
the new pod is not Ready until ~10s after the delete. If a control reports zero
failures, stop: the matrix below cannot be trusted.

- [ ] **Step 7: The matrix.** Each run takes ~10 min; run them one at a time.

```bash
OUT=$(mktemp -d) deploy/scripts/rollout-drain.sh rollouts app-gateway 5
OUT=$(mktemp -d) deploy/scripts/rollout-drain.sh rollouts event-service 5
```

| Result | Then |
|---|---|
| Zero failures on both, and the runner's `gone, Ns after the stop began` for the gateway down from 40–42s to about 20s or less (~10s to Ready + 5s sleep + up to 5s drain) | Done: go to step 8 |
| Gateway failures, with old pods gone in ~5–7s | The drain works, but 5s is short for kube-proxy here. Record it and bring 0015's length to the architect. |
| The gateway's `gone` still at 40s or more | The drain is not wired. Check that `drain.begin` runs before `app.close()`, and that `SHUTDOWN_DRAIN_SECONDS` reached the pod. |
| event-service failures | Repeat step 3 on this build for one rollout, and bring the trace to the architect. Do not tune the sleep by hand. |

- [ ] **Step 8: Stop the box.** `make -C deploy stop-ec2-k3s`, and confirm that
  `aws eks list-clusters --region us-east-1` returns an empty list.

- [ ] **Step 9: Record.**
  - Fill in *Results* below with the step 3 trace, the step 6 control counts, and the
    step 7 counts and pod exit times.
  - Change this plan's status line to `COMPLETE <date>`, and the measurement plan's to
    point here.
  - Accept 0015 and 0016: change `proposed` to `accepted`, and add `## Outcome` citing
    the commits, the three tests and their mutants, and the step 7 counts.
  - Run `python3 docs/decisions/index.py`.
  - In `CLAUDE.md`, change the *Decided* row owned by 0015/0016 from *unbuilt* to
    *built; measured on k3s <date>*. If step 3 read backoff, update the crash-case row
    to say so.

Commit. Stage `CLAUDE.md` only if `git diff CLAUDE.md` shows the register rows and
nothing else; otherwise ask the architect first.

```bash
git add docs/plans/2026-09-27-rollout-drain-fixes.md docs/plans/2026-09-24-rollout-drain-measurement.md \
  docs/decisions/0015-app-pods-sleep-before-sigterm.md docs/decisions/0016-the-gateway-drains-for-up-to-65s.md \
  docs/decisions/README.md CLAUDE.md
git commit -m "docs: accept the rollout drain records with their k3s run"
```

## Results

Not run.
