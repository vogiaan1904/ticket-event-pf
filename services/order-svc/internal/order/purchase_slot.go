package order

import "fmt"

// PurchaseSlotKey names the slot that admits one in-flight purchase per buyer.
//
//	waiting room on  -> the session, which is what admission handed out
//	waiting room off -> user + event, so it is still suppressed
//
// Derived here because the claim's writer and its releaser must agree on it byte
// for byte. See docs/PURCHASE_SLOT.md#the-key.
func PurchaseSlotKey(sessionID, userID, eventID string) string {
	if sessionID != "" {
		return sessionID
	}

	return fmt.Sprintf("user#%s:event#%s", userID, eventID)
}
