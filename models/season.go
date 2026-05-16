package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	SeasonStatusPending = "pending"
	SeasonStatusActive  = "active"
	SeasonStatusClosed  = "closed"
)

type Season struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	StartsAt        time.Time `json:"starts_at"`
	EndsAt          time.Time `json:"ends_at"`
	StartingBalance float64   `json:"starting_balance"`
	Status          string    `json:"status"`
	AutoEnroll      bool      `json:"auto_enroll"`
	CreatedAt       time.Time `json:"created_at"`
	EnrollmentCount int       `json:"enrollment_count"`
}

// SeasonEnrollment is a bot's isolated tournament account for one season.
// CashBalance is the live balance the bot trades with (mutates on every
// season-scoped trade). EndValue / FinalRank are populated when the season
// closes; before that they're nil.
type SeasonEnrollment struct {
	ID          uuid.UUID `json:"id"`
	SeasonID    uuid.UUID `json:"season_id"`
	BotID       uuid.UUID `json:"bot_id"`
	CashBalance float64   `json:"cash_balance"`
	EndValue    *float64  `json:"end_value,omitempty"`
	ReturnPct   *float64  `json:"return_pct,omitempty"`
	FinalRank   *int      `json:"final_rank,omitempty"`
	EnrolledAt  time.Time `json:"enrolled_at"`
}

// SeasonLeaderboardEntry is one row of season standings. The values are
// real account balances — cash plus mark-to-market position value — not
// normalized.
type SeasonLeaderboardEntry struct {
	Rank           int     `json:"rank"`
	BotID          string  `json:"bot_id"`
	BotName        string  `json:"bot_name"`
	CreatorEmail   string  `json:"creator_email,omitempty"`
	CashBalance    float64 `json:"cash_balance"`
	PositionsValue float64 `json:"positions_value"`
	TotalValue     float64 `json:"total_value"`
	ReturnPct      float64 `json:"return_pct"`
	TradeCount     int     `json:"trade_count"`
	Final          bool    `json:"final"`
}
