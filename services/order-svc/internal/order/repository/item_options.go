package repository

type CreateOrderItemOption struct {
	OrderCode       string
	TicketClassID   string
	TicketClassName string
	PriceAtPurchase int64
	Quantity        int32
	TotalAmount     int64
}
