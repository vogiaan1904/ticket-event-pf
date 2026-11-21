package service

import (
	"context"
	"testing"
	"time"
)

// Every field a create or an update is given reads back as given.
func TestTicketClassReadsBackEveryField(t *testing.T) {
	svc := NewTicketClassService(newTestLogger(), newTestDB(t))
	ctx := context.Background()
	at := func(d int) *time.Time { t := time.Date(2027, 1, d, 19, 30, 0, 0, time.UTC); return &t }

	created, err := svc.Create(ctx, CreateTicketClassInput{
		EventID: "evt-readback", Name: "VIP", PriceCents: 12345, Currency: "VND",
		Total: 50, SaleStartAt: at(1), SaleEndAt: at(9),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.EventID != "evt-readback" || got.Name != "VIP" || got.PriceCents != 12345 || got.Currency != "VND" ||
		got.Total != 50 || !got.SaleStartAt.Equal(*at(1)) || !got.SaleEndAt.Equal(*at(9)) || got.Status != "ACTIVE" {
		t.Fatalf("after create: %+v", got)
	}

	name, price, cur, total, status := "VIP Two", int64(23456), "USD", 60, "INACTIVE"
	if _, err := svc.Update(ctx, created.ID, UpdateTicketClassInput{
		Name: &name, PriceCents: &price, Currency: &cur, Total: &total,
		SaleStartAt: at(2), SaleEndAt: at(10), Status: &status,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Name != name || got.PriceCents != price || got.Currency != cur || got.Total != total ||
		!got.SaleStartAt.Equal(*at(2)) || !got.SaleEndAt.Equal(*at(10)) || string(got.Status) != status {
		t.Fatalf("after update: %+v", got)
	}
}
