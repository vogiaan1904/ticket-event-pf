package kafka

const (
	TopicQueueReady  = "queue.ready"
	TopicQueueJoined = "queue.joined"
	TopicQueueLeft   = "queue.left"

	TopicCheckoutCompleted = "checkout.completed"
	TopicCheckoutFailed    = "checkout.failed"
	TopicCheckoutExpired   = "checkout.expired"
)

// DLQTopicSuffix names the dead-letter topic for a consumed topic, e.g.
// "checkout.completed" -> "checkout.completed.dlq".
const DLQTopicSuffix = ".dlq"

func DLQTopic(topic string) string {
	return topic + DLQTopicSuffix
}
