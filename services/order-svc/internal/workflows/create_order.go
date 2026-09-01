package workflows

import (
	"fmt"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/pkg/util"
	"go.temporal.io/sdk/workflow"
)

type CreateOrderWorkflowInput struct {
	OrderCode       string
	SessionID       string
	UserID          string
	Email           string
	Phone           string
	UserFullName    string
	EventID         string
	EventName       string
	Currency        string
	TotalAmount     int64
	Items           []CreateOrderItemInput
	PaymentProvider string
	RedirectUrl     string
	IdempotencyKey  string
}

type CreateOrderItemInput struct {
	OrderID         string
	TicketClassID   string
	TicketClassName string
	PriceAtPurchase int64
	Quantity        int32
	TotalAmount     int64
}

type CreateOrderWorkflowResult struct {
	PaymentUrl string
	Order      *models.Order
	OrderItems []models.OrderItem
}

func GetCreateOrderWorkflowID(oCode string) string {
	return fmt.Sprintf("CreateOrder:%s", oCode)
}

// CreateOrder runs the purchase saga:
// 1. Reserve tickets   - a timed hold, the only step that can be contended
// 2. Create order      - the order record (saga begins)
// 3. Create order items
// 4. Create payment intent - returns the URL the buyer is sent to
// Compensation on failure runs in reverse: delete items -> delete order ->
// release the hold.
//
// The hold is taken first on purpose. It is the only step whose outcome is
// decided by other buyers, so taking it first means a buyer who loses the race
// costs two DynamoDB writes and two deletes less than one who is told after
// the fact. Availability is not pre-checked: Reserve decides it under a row
// lock, and an unlocked read beforehand is wrong precisely when it matters.
func CreateOrder(ctx workflow.Context, in *CreateOrderWorkflowInput) (*CreateOrderWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting create order workflow", "orderCode", in.OrderCode)

	var compensations Compensations
	var err error

	defer func() {
		if err != nil {
			logger.Error("Workflow failed, running compensations", "error", err)
			disconnectedCtx, _ := workflow.NewDisconnectedContext(ctx)
			compensations.Compensate(disconnectedCtx, false)
		}
	}()

	ctx = workflow.WithActivityOptions(ctx, getCreateOrderActivityOptions())

	// 1. Reserve inventory. in.OrderCode is assigned before the workflow starts,
	//    so the hold can be keyed on it without an order row existing yet.
	expAt := util.TimeToISO8601Str(reservationExpiry(workflow.Now(ctx)))
	err = reserveInventory(ctx, in.OrderCode, expAt, in.Items)
	if err != nil {
		return nil, err
	}
	compensations.AddCompensation(iActs.ReleaseInventory, in.OrderCode)

	// 2. Create order
	o, err := createOrder(ctx, in)
	if err != nil {
		return nil, err
	}
	code := o.Code
	compensations.AddCompensation(oActs.DeleteOrder, code)

	// 3. Create order items
	itms, err := createOrderItems(ctx, code, in.Items)
	if err != nil {
		return nil, err
	}
	compensations.AddCompensation(oActs.DeleteOrderItems, code)

	// 4. Create payment intent
	pmtResp, err := processPayment(ctx, in)
	if err != nil {
		return nil, err
	}

	return &CreateOrderWorkflowResult{
		PaymentUrl: pmtResp.PaymentUrl,
		Order:      o,
		OrderItems: itms,
	}, nil
}
