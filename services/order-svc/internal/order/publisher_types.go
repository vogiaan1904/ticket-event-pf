package order

type PubCheckoutCompletedEventInput struct {
	SessionID string
	UserID    string
	EventID   string
}

type PubCheckoutFailedEventInput struct {
	SessionID string
	UserID    string
	EventID   string
}
