package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	pkgDynamo "github.com/vogiaan1904/ticketbottle-order/pkg/dynamodb"
)

// A retry that reuses a code must be refused, not served. The old PutItem had
// no condition, so a replay would overwrite a paid order with a pending one.
func TestCreate_SecondWriteOfTheSameCodeIsRefused(t *testing.T) {
	repo := newTestRepo(t)

	opt := CreateOrderOption{
		Code: "TB-DUP-0001", UserID: "u1", EventID: "e1",
		Currency: "VND", TotalAmount: 1000,
	}

	if _, err := repo.Create(context.Background(), opt); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := repo.Create(context.Background(), opt)
	if !errors.Is(err, order.ErrOrderAlreadyExists) {
		t.Fatalf("second create returned %v, want ErrOrderAlreadyExists", err)
	}
}

// The loser of a claim has to be handed the order that already owns the slot,
// not just a refusal: the caller answers a duplicate create by returning that
// order, so without the winner's code it has nothing to return.
func TestClaimPurchaseSlot_SecondCallerLosesAndLearnsTheWinner(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	existing, err := repo.ClaimPurchaseSlot(ctx, "sess-42", "TB-WIN-0001")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if existing != "" {
		t.Fatalf("first claim reported an existing order %q", existing)
	}

	existing, err = repo.ClaimPurchaseSlot(ctx, "sess-42", "TB-LOSE-0002")
	if !errors.Is(err, order.ErrPurchaseSlotTaken) {
		t.Fatalf("second claim returned %v, want ErrPurchaseSlotTaken", err)
	}
	if existing != "TB-WIN-0001" {
		t.Fatalf("second claim reported winner %q, want TB-WIN-0001", existing)
	}
}

// Requests arriving together are the case the claim exists for. A read followed
// by a create lets every racer through, because they all read nothing; only the
// database can settle it, so exactly one writer may come back a winner and every
// loser must be told the same winning code.
func TestClaimPurchaseSlot_ConcurrentClaimsLeaveExactlyOneWinner(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	const racers = 8

	type outcome struct {
		code     string
		existing string
		err      error
	}

	start := make(chan struct{})
	outcomes := make(chan outcome, racers)

	var wg sync.WaitGroup
	for i := range racers {
		code := fmt.Sprintf("TB-RACE-%04d", i)
		wg.Go(func() {
			<-start
			existing, err := repo.ClaimPurchaseSlot(ctx, "sess-race", code)
			outcomes <- outcome{code: code, existing: existing, err: err}
		})
	}

	close(start)
	wg.Wait()
	close(outcomes)

	var winner string
	var losers []outcome
	for o := range outcomes {
		switch {
		case o.err == nil:
			if winner != "" {
				t.Fatalf("two claims won one slot: %s and %s", winner, o.code)
			}
			if o.existing != "" {
				t.Fatalf("winner %s was told an order %q already held the slot", o.code, o.existing)
			}
			winner = o.code
		case errors.Is(o.err, order.ErrPurchaseSlotTaken):
			losers = append(losers, o)
		default:
			t.Fatalf("claim %s: unexpected error %v", o.code, o.err)
		}
	}

	if winner == "" {
		t.Fatal("no claim won the slot")
	}
	if len(losers) != racers-1 {
		t.Fatalf("got %d losers, want %d", len(losers), racers-1)
	}
	for _, l := range losers {
		if l.existing != winner {
			t.Fatalf("loser %s was told the winner is %q, want %q", l.code, l.existing, winner)
		}
	}
}

// Releasing a slot frees it for the next buyer. Without this the claim is a
// one-way door: a create that fails before writing an order would lock the
// buyer out of the event it failed on.
func TestReleasePurchaseSlot_FreesTheSlotForTheNextClaim(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, err := repo.ClaimPurchaseSlot(ctx, "sess-rel", "TB-FIRST-0001"); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	if err := repo.ReleasePurchaseSlot(ctx, "sess-rel", "TB-FIRST-0001"); err != nil {
		t.Fatalf("release: %v", err)
	}

	existing, err := repo.ClaimPurchaseSlot(ctx, "sess-rel", "TB-SECOND-0002")
	if err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	if existing != "" {
		t.Fatalf("claim after release reported an existing order %q", existing)
	}
}

// A release is scoped to the order that took the slot. A late release from an
// abandoned create must not delete the claim a fresh request has since taken,
// or two creates run at once -- exactly what the claim exists to prevent.
func TestReleasePurchaseSlot_LeavesAClaimTakenBySomeoneElseStanding(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, err := repo.ClaimPurchaseSlot(ctx, "sess-late", "TB-CURRENT-0002"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	if err := repo.ReleasePurchaseSlot(ctx, "sess-late", "TB-ABANDONED-0001"); err != nil {
		t.Fatalf("stale release: %v", err)
	}

	existing, err := repo.ClaimPurchaseSlot(ctx, "sess-late", "TB-INTRUDER-0003")
	if !errors.Is(err, order.ErrPurchaseSlotTaken) {
		t.Fatalf("claim after stale release returned %v, want ErrPurchaseSlotTaken", err)
	}
	if existing != "TB-CURRENT-0002" {
		t.Fatalf("slot is held by %q, want TB-CURRENT-0002", existing)
	}
}

// The claim carries a TTL so an item nobody ever comes back for is collected
// instead of holding a buyer out of an event forever.
func TestClaimPurchaseSlot_WritesATTLBeyondTheCheckoutWindow(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, err := repo.ClaimPurchaseSlot(ctx, "sess-ttl", "TB-TTL-0001"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	k := pkgDynamo.BuildPurchaseSlotKey("sess-ttl")
	res, err := repo.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(repo.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: k},
			"SK": &types.AttributeValueMemberS{Value: k},
		},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		t.Fatalf("read back the claim: %v", err)
	}

	attr, ok := res.Item[pkgDynamo.TTLAttribute].(*types.AttributeValueMemberN)
	if !ok {
		t.Fatalf("claim has no numeric %s attribute: %#v", pkgDynamo.TTLAttribute, res.Item)
	}

	expiresAt, err := strconv.ParseInt(attr.Value, 10, 64)
	if err != nil {
		t.Fatalf("parse %s: %v", pkgDynamo.TTLAttribute, err)
	}

	// A claim on a live order has to outlive the buyer's whole checkout, so the
	// floor is well past the payment window plus its hold grace (9 minutes in
	// workflows), not merely "now". The workflows package imports this one, so
	// the constants cannot be named here without a cycle.
	floor := time.Now().Add(24 * time.Hour)
	if !time.Unix(expiresAt, 0).After(floor) {
		t.Fatalf("claim expires at %v, which is not beyond the checkout window ending %v", time.Unix(expiresAt, 0), floor)
	}
}
