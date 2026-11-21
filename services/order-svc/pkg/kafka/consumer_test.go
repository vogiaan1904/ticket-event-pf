package kafka

import (
	"testing"

	"github.com/IBM/sarama"
)

// A partition added to a live topic is written to before the group claims it.
func TestAnUnclaimedPartitionIsReadFromItsOldestMessage(t *testing.T) {
	c := newConsumerConfig()
	if c.Consumer.Offsets.Initial != sarama.OffsetOldest {
		t.Fatalf("Offsets.Initial = %d, want sarama.OffsetOldest (%d)", c.Consumer.Offsets.Initial, sarama.OffsetOldest)
	}
	if !c.Consumer.Group.ResetInvalidOffsets {
		t.Fatal("ResetInvalidOffsets is off: a recreated topic would leave the group past its end")
	}
}
