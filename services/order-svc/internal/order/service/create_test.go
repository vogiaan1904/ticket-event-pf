package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"github.com/vogiaan1904/ticketbottle-order/internal/testutil/dynamotest"
	"github.com/vogiaan1904/ticketbottle-order/internal/workflows"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/event"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	pkgJwt "github.com/vogiaan1904/ticketbottle-order/pkg/jwt"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
	temporalCli "go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

const (
	testUserID  = "u1"
	testEventID = "e1"

	// The slot key a create must claim when the event has no waiting room. It
	// is written out rather than built with order.PurchaseSlotKey so that the
	// test states the format independently of the code under test.
	testUserEventSlotKey = "user#" + testUserID + ":event#" + testEventID
)

type stubEventClient struct {
	event.EventServiceClient
	allowWaitRoom bool
}

func (c stubEventClient) FindOne(ctx context.Context, in *event.FindOneEventRequest, opts ...grpc.CallOption) (*event.FindOneEventResponse, error) {
	return &event.FindOneEventResponse{Event: &event.Event{
		Id:     in.Id,
		Name:   "Test Event",
		Status: event.EventStatus_EVENT_STATUS_PUBLISHED,
	}}, nil
}

func (c stubEventClient) GetConfig(ctx context.Context, in *event.GetEventConfigRequest, opts ...grpc.CallOption) (*event.GetEventConfigResponse, error) {
	return &event.GetEventConfigResponse{EventConfig: &event.EventConfig{
		Id:            in.EventId,
		AllowWaitRoom: c.allowWaitRoom,
	}}, nil
}

type stubInventoryClient struct {
	inventory.InventoryServiceClient
	err error
}

func (c stubInventoryClient) FindManyTicketClass(ctx context.Context, in *inventory.FindManyTicketClassRequest, opts ...grpc.CallOption) (*inventory.FindManyTicketClassResponse, error) {
	if c.err != nil {
		return nil, c.err
	}

	tcs := make([]*inventory.TicketClass, len(in.Ids))
	for i, id := range in.Ids {
		tcs[i] = &inventory.TicketClass{Id: id, EventId: in.EventId, Name: "General", PriceCents: 1000}
	}

	return &inventory.FindManyTicketClassResponse{TicketClasses: tcs}, nil
}

type stubJwtManager struct {
	sessionID string
}

func (m stubJwtManager) Verify(ctx context.Context, token string) (pkgJwt.Payload, error) {
	return pkgJwt.Payload{CheckoutTokenClaim: models.CheckoutTokenClaim{
		SessionID: m.sessionID,
		UserID:    testUserID,
		EventID:   testEventID,
	}}, nil
}

// fakeWorkflowRun stands in for a started CreateOrder execution. Nothing here
// runs the saga: these tests are about what the service does around it.
type fakeWorkflowRun struct {
	id string

	// outliveCaller makes Get block until the caller's own deadline expires,
	// which is how a create whose workflow is still running server-side reaches
	// the cancellation path.
	outliveCaller bool
	getErr        error
}

func (r *fakeWorkflowRun) GetID() string    { return r.id }
func (r *fakeWorkflowRun) GetRunID() string { return r.id + ":run" }

func (r *fakeWorkflowRun) Get(ctx context.Context, valuePtr any) error {
	if r.outliveCaller {
		<-ctx.Done()
		return ctx.Err()
	}
	if r.getErr != nil {
		return r.getErr
	}

	if res, ok := valuePtr.(*workflows.CreateOrderWorkflowResult); ok {
		*res = workflows.CreateOrderWorkflowResult{
			PaymentUrl: "https://pay.test/checkout",
			Order:      &models.Order{Code: r.id},
		}
	}

	return nil
}

func (r *fakeWorkflowRun) GetWithOptions(ctx context.Context, valuePtr any, options temporalCli.WorkflowRunGetOptions) error {
	return r.Get(ctx, valuePtr)
}

type fakeTemporalClient struct {
	temporalCli.Client

	run       *fakeWorkflowRun
	startErr  error
	cancelErr error

	startedInput *workflows.CreateOrderWorkflowInput
	cancelCalls  int
}

func (c *fakeTemporalClient) ExecuteWorkflow(ctx context.Context, options temporalCli.StartWorkflowOptions, workflow any, args ...any) (temporalCli.WorkflowRun, error) {
	if len(args) > 0 {
		if in, ok := args[0].(*workflows.CreateOrderWorkflowInput); ok {
			c.startedInput = in
			c.run.id = in.OrderCode
		}
	}
	if c.startErr != nil {
		return nil, c.startErr
	}

	return c.run, nil
}

func (c *fakeTemporalClient) CancelWorkflow(ctx context.Context, workflowID, runID string) error {
	c.cancelCalls++
	return c.cancelErr
}

func newCreateService(t *testing.T, tprCli *fakeTemporalClient, allowWaitRoom bool, sessionID string) (*implService, repo.Repository) {
	t.Helper()

	l := logger.InitializeTestZapLogger()
	r := repo.New(l, dynamotest.NewClient(t), dynamotest.TableName)

	return &implService{
		l:             l,
		repo:          r,
		jwt:           stubJwtManager{sessionID: sessionID},
		invSvc:        stubInventoryClient{},
		evSvc:         stubEventClient{allowWaitRoom: allowWaitRoom},
		pmtSvc:        stubPaymentClient{url: "https://pay.test/checkout"},
		temporal:      tprCli,
		createTimeout: testCreateTimeout,
		clock:         time.Now,
	}, r
}

func createInput() order.CreateOrderInput {
	return order.CreateOrderInput{
		CheckoutToken: "checkout-token",
		UserID:        testUserID,
		EventID:       testEventID,
		Email:         "buyer@test.local",
		PaymentMethod: testPaymentMethod,
		Items:         []order.OrderItemInput{{TicketClassID: "tc1", Quantity: 2}},
	}
}

// slotHolder returns the order code currently holding dedupeKey, or "" when
// the slot is free. It takes the slot to find out, so callers that go on to
// assert about the slot have to account for the probe holding it.
func slotHolder(t *testing.T, r repo.Repository, dedupeKey string) string {
	t.Helper()

	held, _, err := r.ClaimPurchaseSlot(context.Background(), dedupeKey, "TB-PROBE-0001")
	if err == nil {
		return ""
	}
	if !errors.Is(err, order.ErrPurchaseSlotTaken) {
		t.Fatalf("read slot %s: %v", dedupeKey, err)
	}

	return held
}

// Without a waiting room there is no session to key the slot on, so it is keyed
// on the buyer and the event. Dropping the event from that key would give a
// buyer one in-flight purchase across the whole platform: a checkout open for
// one show would refuse them every other show on sale.
func TestCreate_WithoutAWaitingRoomTheSlotIsTheBuyerAndTheEvent(t *testing.T) {
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}}
	s, r := newCreateService(t, tprCli, false, "")

	if _, err := s.Create(context.Background(), createInput()); err != nil {
		t.Fatalf("create: %v", err)
	}

	if held := slotHolder(t, r, testUserEventSlotKey); held != tprCli.startedInput.OrderCode {
		t.Fatalf("slot %s is held by %q, want the order this create started, %q",
			testUserEventSlotKey, held, tprCli.startedInput.OrderCode)
	}
}

// With a waiting room the slot is the session, because admission is what handed
// it out: one admitted session buys once.
func TestCreate_WithAWaitingRoomTheSlotIsTheSession(t *testing.T) {
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}}
	s, r := newCreateService(t, tprCli, true, "sess-1")

	if _, err := s.Create(context.Background(), createInput()); err != nil {
		t.Fatalf("create: %v", err)
	}

	if held := slotHolder(t, r, "sess-1"); held != tprCli.startedInput.OrderCode {
		t.Fatalf("slot sess-1 is held by %q, want the order this create started, %q",
			held, tprCli.startedInput.OrderCode)
	}
}

// The caller's deadline expiring does not stop the workflow, and a cancellation
// that failed did not stop it either: it may still go on to reserve inventory
// and write a live order. Releasing the slot here would let the buyer's retry
// mint a second order and a second hold behind the first one's back. A slot
// held too long is the recoverable error; two orders on one slot is not.
func TestCreate_AFailedCancellationKeepsTheSlot(t *testing.T) {
	tprCli := &fakeTemporalClient{
		run:       &fakeWorkflowRun{outliveCaller: true},
		cancelErr: errors.New("temporal unreachable"),
	}
	s, r := newCreateService(t, tprCli, false, "")
	s.createTimeout = 100 * time.Millisecond

	_, err := s.Create(context.Background(), createInput())
	if !errors.Is(err, order.ErrRequestTimeout) {
		t.Fatalf("create returned %v, want ErrRequestTimeout", err)
	}
	if tprCli.cancelCalls != 1 {
		t.Fatalf("cancel was called %d times, want once", tprCli.cancelCalls)
	}

	if held := slotHolder(t, r, testUserEventSlotKey); held != tprCli.startedInput.OrderCode {
		t.Fatalf("slot %s is held by %q after a failed cancellation, want the workflow that may still be running, %q",
			testUserEventSlotKey, held, tprCli.startedInput.OrderCode)
	}
}

// The cancellation landed, and the saga reserves inventory before it writes the
// order row, so this workflow will never write one. Its claim guards nothing
// and would refuse every retry on behalf of an order that cannot exist.
func TestCreate_ACancelledWorkflowGivesTheSlotBack(t *testing.T) {
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{outliveCaller: true}}
	s, r := newCreateService(t, tprCli, false, "")
	s.createTimeout = 100 * time.Millisecond

	_, err := s.Create(context.Background(), createInput())
	if !errors.Is(err, order.ErrRequestTimeout) {
		t.Fatalf("create returned %v, want ErrRequestTimeout", err)
	}

	if held := slotHolder(t, r, testUserEventSlotKey); held != "" {
		t.Fatalf("slot %s is still held by %q after the workflow was cancelled", testUserEventSlotKey, held)
	}
}

// A workflow that ran to completion and failed as a business outcome -- sold
// out -- holds no inventory and wrote no order. Keeping its claim would lock
// the buyer out of an event they never bought a ticket for.
func TestCreate_ASoldOutWorkflowGivesTheSlotBack(t *testing.T) {
	tprCli := &fakeTemporalClient{
		run: &fakeWorkflowRun{getErr: workflows.NewInsufficientInventoryError(workflows.ErrInsufficientInventory)},
	}
	s, r := newCreateService(t, tprCli, false, "")

	_, err := s.Create(context.Background(), createInput())
	if !errors.Is(err, order.ErrNotEnoughTickets) {
		t.Fatalf("create returned %v, want ErrNotEnoughTickets", err)
	}
	if tprCli.cancelCalls != 0 {
		t.Fatal("a workflow that finished on its own was cancelled")
	}

	if held := slotHolder(t, r, testUserEventSlotKey); held != "" {
		t.Fatalf("slot %s is still held by %q after the buyer lost the race for stock", testUserEventSlotKey, held)
	}
}

// The workflow was never started, so nothing behind this claim will ever write
// an order or hold stock.
func TestCreate_AWorkflowThatNeverStartedGivesTheSlotBack(t *testing.T) {
	tprCli := &fakeTemporalClient{
		run:      &fakeWorkflowRun{},
		startErr: errors.New("temporal unreachable"),
	}
	s, r := newCreateService(t, tprCli, false, "")

	if _, err := s.Create(context.Background(), createInput()); err == nil {
		t.Fatal("create succeeded although the workflow could not be started")
	}

	if held := slotHolder(t, r, testUserEventSlotKey); held != "" {
		t.Fatalf("slot %s is still held by %q after the workflow failed to start", testUserEventSlotKey, held)
	}
}
