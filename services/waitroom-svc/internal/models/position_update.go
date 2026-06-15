package models

import "time"

type UpdateType string

const (
	UpdateTypeUserJoined    UpdateType = "user_joined"
	UpdateTypeUserLeft      UpdateType = "user_left"
	UpdateTypeUserAdmitted  UpdateType = "user_admitted"
	UpdateTypeRecalculation UpdateType = "position_recalc"
)

// This is published to Redis Pub/Sub when the queue state changes
type PositionUpdateEvent struct {
	EventID            string     `json:"event_id"`
	UpdateType         UpdateType `json:"update_type"`
	AffectedSessionIDs []string   `json:"affected_session_ids,omitempty"`
	Timestamp          time.Time  `json:"timestamp"`
}
