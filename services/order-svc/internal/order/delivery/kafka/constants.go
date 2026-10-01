package kafka

const (
	TopicPaymentCompleted = "payment.completed"
	TopicPaymentFailed    = "payment.failed"

	TopicCheckoutCompleted = "checkout.completed"
	TopicCheckoutFailed    = "checkout.failed"
	TopicCheckoutExpired   = "checkout.expired"

	TopicRefundRequired = "order.refund_required"
)
