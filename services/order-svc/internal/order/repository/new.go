package repository

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/vogiaan1904/ticketbottle-order/pkg/logger"
)

type implRepository struct {
	l         logger.Logger
	db        *dynamodb.Client
	tableName string
	clock     func() time.Time
}

var _ Repository = &implRepository{}

func New(l logger.Logger, db *dynamodb.Client, tableName string) Repository {
	return NewWithClock(l, db, tableName, time.Now)
}

// NewWithClock is New with the clock that timestamps claims -- claimed_at,
// expires_at -- injectable, so a test can pin how old a claim is without
// sleeping for it.
func NewWithClock(l logger.Logger, db *dynamodb.Client, tableName string, clock func() time.Time) Repository {
	return &implRepository{
		l:         l,
		db:        db,
		tableName: tableName,
		clock:     clock,
	}
}
