package kafka

import (
	"encoding/json"
	"testing"
	"time"
)

// The waitroom frees a checkout slot only when it can decode the event that
// order-svc publishes. A decode failure strands the slot, so the invariant under
// test is that the routing fields survive *every* timestamp shape we have ever
// put on this topic -- not just the current one.
func TestCheckoutCompletedEventDecodesRegardlessOfTimestampFormat(t *testing.T) {
	broker := time.Date(2026, 7, 21, 6, 29, 4, 0, time.UTC)

	tests := []struct {
		name      string
		timestamp string // raw JSON value for the "timestamp" field
		wantTime  time.Time
	}{
		{
			name:      "current producer format (RFC3339 UTC)",
			timestamp: `"2026-07-21T06:29:04Z"`,
			wantTime:  time.Date(2026, 7, 21, 6, 29, 4, 0, time.UTC),
		},
		{
			name:      "RFC3339 with a non-UTC offset",
			timestamp: `"2026-07-21T13:29:04+07:00"`,
			wantTime:  time.Date(2026, 7, 21, 6, 29, 4, 0, time.UTC),
		},
		{
			// What PublishCheckoutFailed emitted before this fix. It is not
			// parseable as a timestamp and used to fail the whole message.
			name:      "legacy time.Now().String()",
			timestamp: `"2026-07-21 13:29:04.148338 +0700 +07 m=+0.000141042"`,
			wantTime:  broker,
		},
		{
			// What the Temporal activity produced when the producer did not
			// overwrite the field.
			name:      "empty string",
			timestamp: `""`,
			wantTime:  broker,
		},
		{
			name:      "null",
			timestamp: `null`,
			wantTime:  broker,
		},
		{
			name:      "wrong type entirely",
			timestamp: `12345`,
			wantTime:  broker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"session_id":"ss-1","user_id":"u-1","event_id":"e-1","timestamp":` + tt.timestamp + `}`

			var got CheckoutCompletedEvent
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("decode failed, slot would be stranded: %v", err)
			}

			if got.SessionID != "ss-1" || got.UserID != "u-1" || got.EventID != "e-1" {
				t.Errorf("routing fields corrupted: %+v", got)
			}

			if resolved := got.Timestamp.OrElse(broker); !resolved.Equal(tt.wantTime) {
				t.Errorf("timestamp = %v, want %v", resolved.UTC(), tt.wantTime)
			}
		})
	}
}

func TestCheckoutCompletedEventDecodesWithTimestampFieldAbsent(t *testing.T) {
	broker := time.Date(2026, 7, 21, 6, 29, 4, 0, time.UTC)

	var got CheckoutCompletedEvent
	if err := json.Unmarshal([]byte(`{"session_id":"ss-1","event_id":"e-1"}`), &got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !got.Timestamp.OrElse(broker).Equal(broker) {
		t.Errorf("absent timestamp should fall back to the broker timestamp")
	}
}

func TestEventTimeRoundTrips(t *testing.T) {
	want := time.Date(2026, 7, 21, 6, 29, 4, 0, time.UTC)

	encoded, err := json.Marshal(EventTime{Time: want})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got EventTime
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !got.Time.Equal(want) {
		t.Errorf("round trip = %v, want %v", got.Time, want)
	}
}
