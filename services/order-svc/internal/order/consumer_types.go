package order

type HandlePaymentCompletedInput struct {
	OrderCode string
}

type HandlePaymentFailedInput struct {
	OrderCode string
	Reason    string
}
