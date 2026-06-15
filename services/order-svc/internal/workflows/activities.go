package workflows

import "github.com/vogiaan1904/ticketbottle-order/internal/activities"

var (
	iActs  *activities.InventoryActivities
	oActs  *activities.OrderActivities
	pActs  *activities.PaymentActivities
	epActs *activities.EventPublishingActivities
)
