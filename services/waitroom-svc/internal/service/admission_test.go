package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-waitroom/internal/models"
	repo "github.com/vogiaan1904/ticketbottle-waitroom/internal/repository/redis"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/logger"
	"github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

// --- rig ---------------------------------------------------------------------

// admissionRig runs the real session and queue services against a test Redis,
// so a test can place a processor tick exactly where a race needs it.
type admissionRig struct {
	svc  WaitroomService
	proc *queueProcessor
	cli  *redis.Client
	eID  string
}

// interleavingProducer runs one processor tick inside PublishQueueJoined:
// after JoinQueue has enqueued the session, before JoinQueue returns.
type interleavingProducer struct {
	fakeProducer
	proc       *queueProcessor
	tickOnJoin bool
	ticked     bool
}

func (p *interleavingProducer) PublishQueueJoined(ctx context.Context, e kafka.QueueJoinedEvent) error {
	if !p.tickOnJoin || p.ticked {
		return nil
	}
	p.ticked = true
	sleepPastSecond()
	return p.proc.ProcessEventQueue(ctx, e.EventID)
}

// sleepPastSecond waits into the next whole second, when a queue score
// drawn now has come due.
func sleepPastSecond() {
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second + 20*time.Millisecond)))
}

func newAdmissionRig(t *testing.T, saleStart time.Time, slots int, prod *interleavingProducer) *admissionRig {
	t.Helper()

	addr := os.Getenv("WAITROOM_TEST_REDIS_ADDR")
	if addr == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI requires WAITROOM_TEST_REDIS_ADDR to point at a test redis")
		}
		t.Skip("WAITROOM_TEST_REDIS_ADDR not set; skipping Redis integration test")
	}

	cli := redis.NewClient(config.RedisConfig{Addr: addr, PoolSize: 5, MinIdleConns: 1})
	l := logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})

	ssSvc := NewSessionService(repo.NewRedisSessionRepository(cli, l), config.JWTConfig{Secret: "test", Expiry: 15 * time.Minute}, l)
	qSvc := NewQueueService(repo.NewRedisQueueRepository(cli, l), l)
	proc := &queueProcessor{
		qSvc:  qSvc,
		ssSvc: ssSvc,
		prod:  prod,
		l:     l,
		cfg: ProcessorConfig{
			MaxConcurrentPerEvent: slots,
			BatchSize:             10,
			RetryAttempts:         2,
			RetryDelay:            time.Millisecond,
			CheckoutTTL:           15 * time.Minute,
		},
		stopCh: make(chan struct{}),
	}
	prod.proc = proc
	ev := &fakeEventClient{allowWaitRoom: true, saleStart: saleStart.UTC().Format(time.RFC3339)}

	eID := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	t.Cleanup(func() {
		ctx := context.Background()
		cli.Del(ctx, "waitroom:"+eID+":queue")
		cli.Del(ctx, "waitroom:"+eID+":checkouts")
		cli.SRem(ctx, "waitroom:active_events", eID)
	})

	return &admissionRig{
		svc:  NewWaitroomService(qSvc, ssSvc, ev, prod, l, proc, time.Minute),
		proc: proc,
		cli:  cli,
		eID:  eID,
	}
}

func (r *admissionRig) join(t *testing.T, userID string) string {
	t.Helper()
	out, err := r.svc.JoinQueue(context.Background(), &JoinQueueInput{UserID: userID, EventID: r.eID})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	return out.SessionID
}

func (r *admissionRig) tick(t *testing.T) {
	t.Helper()
	if err := r.proc.ProcessEventQueue(context.Background(), r.eID); err != nil {
		t.Fatalf("tick: %v", err)
	}
}

func (r *admissionRig) status(t *testing.T, ssID string) *QueueStatusOutput {
	t.Helper()
	st, err := r.svc.GetQueueStatus(context.Background(), ssID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// --- tests -------------------------------------------------------------------

func TestAPostOpenJoinerIsAdmittedWithinASecond(t *testing.T) {
	r := newAdmissionRig(t, time.Now().Add(-time.Hour), 10, &interleavingProducer{})

	ssID := r.join(t, "u-1")
	sleepPastSecond()
	r.tick(t)

	if st := r.status(t, ssID); st.Status != models.SessionStatusAdmitted || st.CheckoutToken == "" {
		t.Fatalf("want admitted with a token, got status=%s token=%q", st.Status, st.CheckoutToken)
	}
}

// Invariant: a join never undoes an admission that lands while it is in flight.
func TestAnAdmissionDuringTheJoinIsNotUndone(t *testing.T) {
	prod := &interleavingProducer{tickOnJoin: true}
	r := newAdmissionRig(t, time.Now().Add(-time.Hour), 10, prod)

	ssID := r.join(t, "u-1")
	if !prod.ticked {
		t.Fatal("no tick ran inside the join, so nothing was asserted")
	}

	if st := r.status(t, ssID); st.Status != models.SessionStatusAdmitted || st.CheckoutToken == "" {
		t.Fatalf("admitted during the join, then reverted: status=%s token=%q", st.Status, st.CheckoutToken)
	}
}

func TestNobodyIsAdmittedBeforeTheSaleOpens(t *testing.T) {
	r := newAdmissionRig(t, time.Now().Add(time.Hour), 10, &interleavingProducer{})

	ssID := r.join(t, "u-1")
	sleepPastSecond()
	r.tick(t)

	if st := r.status(t, ssID); st.Status != models.SessionStatusQueued {
		t.Fatalf("admitted an hour before the sale opens: status=%s", st.Status)
	}
}

func TestPreOpenJoinersAreAdmittedOnceTheSaleOpens(t *testing.T) {
	saleStart := time.Now().Truncate(time.Second).Add(2 * time.Second)
	r := newAdmissionRig(t, saleStart, 10, &interleavingProducer{})
	ids := []string{r.join(t, "u-1"), r.join(t, "u-2"), r.join(t, "u-3")}

	r.tick(t)
	for _, id := range ids {
		if st := r.status(t, id); st.Status != models.SessionStatusQueued {
			t.Fatalf("admitted before the sale opened: status=%s", st.Status)
		}
	}

	time.Sleep(time.Until(saleStart.Add(20 * time.Millisecond)))
	r.tick(t)
	for _, id := range ids {
		if st := r.status(t, id); st.Status != models.SessionStatusAdmitted {
			t.Fatalf("still waiting after the sale opened: status=%s", st.Status)
		}
	}
}

// The draw must survive a real sorted set: every pre-open score inside
// [saleStart-1, saleStart), and the resulting order not arrival's.
func TestThePreOpenQueueIsOrderedByLotInRedis(t *testing.T) {
	saleStart := time.Now().Add(time.Hour).Truncate(time.Second)
	r := newAdmissionRig(t, saleStart, 0, &interleavingProducer{})

	const n = 40
	arrival := make([]string, n)
	for i := range n {
		arrival[i] = r.join(t, fmt.Sprintf("u-%d", i))
	}

	zs, err := r.cli.GetClient().ZRangeWithScores(context.Background(), "waitroom:"+r.eID+":queue", 0, -1).Result()
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	if len(zs) != n {
		t.Fatalf("queue holds %d, want %d", len(zs), n)
	}

	lo, hi := float64(saleStart.Unix()-1), float64(saleStart.Unix())
	inPlace := 0
	for i, z := range zs {
		if z.Score < lo || z.Score >= hi {
			t.Fatalf("score %v outside the pre-open band [%v, %v)", z.Score, lo, hi)
		}
		if z.Member == arrival[i] {
			inPlace++
		}
	}
	// A shuffle of 40 leaves about one in place; arrival order leaves all 40.
	if inPlace == n {
		t.Fatal("the queue is in arrival order; the draw did not reach Redis")
	}
}
