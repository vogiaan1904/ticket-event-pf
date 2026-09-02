package producer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/IBM/sarama"
	kafka "github.com/vogiaan1904/ticketbottle-order/internal/order/delivery/kafka"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

// captureProducer records the messages that would go to the broker.
type captureProducer struct {
	sent []*sarama.ProducerMessage
}

func (c *captureProducer) SendMessage(msg *sarama.ProducerMessage) (int32, int64, error) {
	c.sent = append(c.sent, msg)
	return 0, 0, nil
}

func (c *captureProducer) SendMessages(msgs []*sarama.ProducerMessage) error {
	c.sent = append(c.sent, msgs...)
	return nil
}

func (c *captureProducer) Close() error                            { return nil }
func (c *captureProducer) TxnStatus() sarama.ProducerTxnStatusFlag { return 0 }
func (c *captureProducer) IsTransactional() bool                   { return false }
func (c *captureProducer) BeginTxn() error                         { return nil }
func (c *captureProducer) CommitTxn() error                        { return nil }
func (c *captureProducer) AbortTxn() error                         { return nil }
func (c *captureProducer) AddOffsetsToTxn(map[string][]*sarama.PartitionOffsetMetadata, string) error {
	return nil
}
func (c *captureProducer) AddMessageToTxn(*sarama.ConsumerMessage, string, *string) error {
	return nil
}

func (c *captureProducer) lastBody(t *testing.T) map[string]any {
	t.Helper()

	if len(c.sent) != 1 {
		t.Fatalf("expected exactly 1 message, got %d", len(c.sent))
	}

	raw, err := c.sent[0].Value.Encode()
	if err != nil {
		t.Fatalf("encode message value: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("message body is not valid JSON: %v", err)
	}

	return body
}

// The waitroom decodes these timestamps as RFC3339. Anything else fails the
// whole message there and strands the user's checkout slot.
func TestPublishedCheckoutTimestampsAreRFC3339(t *testing.T) {
	l := logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"})

	tests := []struct {
		name    string
		publish func(Producer, context.Context) error
	}{
		{
			name: "checkout completed",
			publish: func(p Producer, ctx context.Context) error {
				return p.PublishCheckoutCompleted(ctx, kafka.CheckoutCompletedEvent{
					SessionID: "ss-1", UserID: "u-1", EventID: "e-1",
				})
			},
		},
		{
			name: "checkout failed",
			publish: func(p Producer, ctx context.Context) error {
				return p.PublishCheckoutFailed(ctx, kafka.CheckoutFailedEvent{
					SessionID: "ss-1", UserID: "u-1", EventID: "e-1",
				})
			},
		},
		{
			name: "refund required",
			publish: func(p Producer, ctx context.Context) error {
				return p.PublishRefundRequired(ctx, kafka.RefundRequiredEvent{
					OrderCode: "TB-1", UserID: "u-1", EventID: "e-1", Reason: "stock is gone",
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cap := &captureProducer{}
			before := time.Now().Add(-time.Second)

			if err := tt.publish(NewProducer(cap, l), context.Background()); err != nil {
				t.Fatalf("publish: %v", err)
			}

			body := cap.lastBody(t)

			ts, ok := body["timestamp"].(string)
			if !ok || ts == "" {
				t.Fatalf("timestamp missing or not a string: %#v", body["timestamp"])
			}

			parsed, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				t.Fatalf("timestamp %q is not RFC3339, waitroom cannot decode it: %v", ts, err)
			}

			// A literal-Z format stamps the local wall clock and labels it UTC,
			// which lands in the future by the host's offset.
			if parsed.Before(before) || parsed.After(time.Now().Add(time.Second)) {
				t.Errorf("timestamp %q is not the current instant (now=%s)", ts, time.Now().UTC().Format(time.RFC3339))
			}
		})
	}
}
