package order

import "fmt"

// PurchaseSlotKey names the slot that admits one in-flight purchase per buyer.
// With a waiting room the slot is the session, because that is what admission
// handed out; without one it is the buyer and the event, so an event that skips
// the queue still gets suppression rather than none at all.
//
// It is derived here rather than at each call site because two of them have to
// agree on it exactly: the service writes the claim before an order exists, and
// the workflow releases it from the order record once the purchase is over. A
// format string copied into the second caller could drift from the first, and
// the release would then miss every claim it was meant to free.
func PurchaseSlotKey(sessionID, userID, eventID string) string {
	if sessionID != "" {
		return sessionID
	}

	return fmt.Sprintf("user#%s:event#%s", userID, eventID)
}
