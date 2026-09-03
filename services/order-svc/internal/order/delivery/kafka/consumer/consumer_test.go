package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/IBM/sarama"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

// stubService answers the one call a payment-completed message makes.
type stubService struct {
	order.Service
	err error
}

func (s stubService) HandlePaymentCompleted(ctx context.Context, in order.HandlePaymentCompletedInput) error {
	return s.err
}

// fakeSession records what the handler marked. Everything else on the
// interface is unused by ConsumeClaim and is present only to satisfy it.
type fakeSession struct {
	ctx    context.Context
	marked []int64
}

func (s *fakeSession) Claims() map[string][]int32 { return nil }
func (s *fakeSession) MemberID() string           { return "test-member" }
func (s *fakeSession) GenerationID() int32        { return 1 }
func (s *fakeSession) MarkOffset(topic string, partition int32, offset int64, metadata string) {
}
func (s *fakeSession) Commit() {}
func (s *fakeSession) ResetOffset(topic string, partition int32, offset int64, metadata string) {
}
func (s *fakeSession) MarkMessage(msg *sarama.ConsumerMessage, metadata string) {
	s.marked = append(s.marked, msg.Offset)
}
func (s *fakeSession) Context() context.Context { return s.ctx }

type fakeClaim struct {
	msgs chan *sarama.ConsumerMessage
}

func (c fakeClaim) Topic() string                            { return kafka.TopicPaymentCompleted }
func (c fakeClaim) Partition() int32                         { return 0 }
func (c fakeClaim) InitialOffset() int64                     { return 0 }
func (c fakeClaim) HighWaterMarkOffset() int64               { return 0 }
func (c fakeClaim) Messages() <-chan *sarama.ConsumerMessage { return c.msgs }

// newClaim delivers one payment-completed message and then closes the channel,
// so a handler that consumes it and carries on reaches the end of the claim
// instead of blocking.
func newClaim(t *testing.T, offset int64) fakeClaim {
	t.Helper()

	body, err := json.Marshal(kafka.PaymentCompletedEvent{OrderCode: "TB-TEST-0001"})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	msgs := make(chan *sarama.ConsumerMessage, 1)
	msgs <- &sarama.ConsumerMessage{
		Topic:  kafka.TopicPaymentCompleted,
		Offset: offset,
		Value:  body,
	}
	close(msgs)

	return fakeClaim{msgs: msgs}
}

func newConsumer(svcErr error) (*Consumer, *fakeSession) {
	return &Consumer{
			svc: stubService{err: svcErr},
			l:   logger.InitializeTestZapLogger(),
		}, &fakeSession{
			ctx: context.Background(),
		}
}

// The payment is already captured. Sarama commits the highest marked offset, so
// moving on lets a later success commit past this one and drop it: money taken,
// order PENDING forever. The failure must leave the offset and end the session.
func TestConsumeClaim_AFailedMessageIsNotMarkedAndIsRedelivered(t *testing.T) {
	c, ss := newConsumer(errors.New("inventory-svc unreachable"))

	err := c.ConsumeClaim(ss, newClaim(t, 42))
	if err == nil {
		t.Fatal("a failed message was swallowed; the session stayed alive and the offset moved on")
	}
	if len(ss.marked) != 0 {
		t.Fatalf("a failed message was marked at offsets %v, so the next commit would skip past it", ss.marked)
	}
}

// An event whose order already accounts for it is answered -- completed, or the
// refund recorded. Redelivery reaches the same answer, so holding the partition
// would stall every later buyer behind a finished event.
func TestConsumeClaim_ASettledFailureIsMarkedAndDoesNotHoldThePartition(t *testing.T) {
	for _, settled := range []error{order.ErrOrderAlreadyProcessed, order.ErrOrderNotFound} {
		t.Run(settled.Error(), func(t *testing.T) {
			c, ss := newConsumer(settled)

			if err := c.ConsumeClaim(ss, newClaim(t, 42)); err != nil {
				t.Fatalf("a settled outcome ended the session with %v; it will be redelivered forever", err)
			}
			if len(ss.marked) != 1 || ss.marked[0] != 42 {
				t.Fatalf("marked offsets %v, want the settled message at 42 marked so the partition moves on", ss.marked)
			}
		})
	}
}

func TestConsumeClaim_AHandledMessageIsMarked(t *testing.T) {
	c, ss := newConsumer(nil)

	if err := c.ConsumeClaim(ss, newClaim(t, 7)); err != nil {
		t.Fatalf("a handled message returned %v", err)
	}
	if len(ss.marked) != 1 || ss.marked[0] != 7 {
		t.Fatalf("marked offsets %v, want 7", ss.marked)
	}
}
