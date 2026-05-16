package services

import (
	"bottrade/database"
	"bottrade/models"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// SeasonService owns the tournament-account lifecycle: enrollment creates an
// isolated $100k account; trades against that account go through the
// season-aware trading engine path; close finalizes by marking positions to
// market and ranking on (cash + position market value).
type SeasonService struct {
	portfolio *PortfolioService
}

func NewSeasonService() *SeasonService {
	return &SeasonService{portfolio: NewPortfolioService()}
}

var ErrSeasonNotFound = errors.New("season not found")
var ErrSeasonClosed = errors.New("season is closed")
var ErrSeasonAlreadyActive = errors.New("season has already started; enrollment is closed")
var ErrAlreadyEnrolled = errors.New("bot is already enrolled in this season")

func (s *SeasonService) ListSeasons(status string) ([]models.Season, error) {
	q := `
		SELECT s.id, s.name, s.slug, s.starts_at, s.ends_at,
		       s.starting_balance, s.status, s.auto_enroll, s.created_at,
		       COALESCE(c.cnt, 0)
		FROM seasons s
		LEFT JOIN (
			SELECT season_id, COUNT(*) AS cnt
			FROM season_enrollments
			GROUP BY season_id
		) c ON c.season_id = s.id
	`
	args := []any{}
	if status != "" {
		q += " WHERE s.status = ?"
		args = append(args, status)
	}
	q += " ORDER BY s.starts_at DESC"

	rows, err := database.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var seasons []models.Season
	for rows.Next() {
		var sn models.Season
		var idStr, startsAt, endsAt, createdAt string
		var autoEnroll int
		if err := rows.Scan(&idStr, &sn.Name, &sn.Slug, &startsAt, &endsAt,
			&sn.StartingBalance, &sn.Status, &autoEnroll, &createdAt, &sn.EnrollmentCount); err != nil {
			continue
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		sn.ID = id
		sn.StartsAt = parseTime(startsAt)
		sn.EndsAt = parseTime(endsAt)
		sn.CreatedAt = parseTime(createdAt)
		sn.AutoEnroll = autoEnroll != 0
		seasons = append(seasons, sn)
	}
	return seasons, nil
}

func (s *SeasonService) GetSeason(idOrSlug string) (*models.Season, error) {
	q := `
		SELECT s.id, s.name, s.slug, s.starts_at, s.ends_at,
		       s.starting_balance, s.status, s.auto_enroll, s.created_at,
		       COALESCE(c.cnt, 0)
		FROM seasons s
		LEFT JOIN (
			SELECT season_id, COUNT(*) AS cnt
			FROM season_enrollments
			GROUP BY season_id
		) c ON c.season_id = s.id
		WHERE s.id = ?1 OR s.slug = ?1
		LIMIT 1
	`
	row := database.DB.QueryRow(q, idOrSlug)
	var sn models.Season
	var idStr, startsAt, endsAt, createdAt string
	var autoEnroll int
	err := row.Scan(&idStr, &sn.Name, &sn.Slug, &startsAt, &endsAt,
		&sn.StartingBalance, &sn.Status, &autoEnroll, &createdAt, &sn.EnrollmentCount)
	if err == sql.ErrNoRows {
		return nil, ErrSeasonNotFound
	}
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	sn.ID = id
	sn.StartsAt = parseTime(startsAt)
	sn.EndsAt = parseTime(endsAt)
	sn.CreatedAt = parseTime(createdAt)
	sn.AutoEnroll = autoEnroll != 0
	return &sn, nil
}

// CreateSeason inserts a pending season. AutoEnroll defaults to false at the
// handler level — opt-in tournaments are the intended UX.
func (s *SeasonService) CreateSeason(name, slug string, startsAt, endsAt time.Time, startingBalance float64, autoEnroll bool) (*models.Season, error) {
	if startingBalance <= 0 {
		startingBalance = 100000.0
	}
	if !endsAt.After(startsAt) {
		return nil, fmt.Errorf("ends_at must be after starts_at")
	}
	id := uuid.New()
	ae := 0
	if autoEnroll {
		ae = 1
	}
	_, err := database.DB.Exec(
		`INSERT INTO seasons (id, name, slug, starts_at, ends_at, starting_balance, status, auto_enroll)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id.String(), name, slug,
		startsAt.UTC().Format(time.RFC3339),
		endsAt.UTC().Format(time.RFC3339),
		startingBalance, models.SeasonStatusPending, ae,
	)
	if err != nil {
		return nil, err
	}
	return s.GetSeason(id.String())
}

// EnrollBot creates an isolated $100k tournament account for the bot in this
// season. Only pending seasons accept enrollments — once a season is live,
// the field is locked. (auto_enroll seasons may still snapshot bots in at
// start, but those go through StartSeason, not this path.)
func (s *SeasonService) EnrollBot(seasonID, botID uuid.UUID) (*models.SeasonEnrollment, error) {
	sn, err := s.GetSeason(seasonID.String())
	if err != nil {
		return nil, err
	}
	switch sn.Status {
	case models.SeasonStatusClosed:
		return nil, ErrSeasonClosed
	case models.SeasonStatusActive:
		return nil, ErrSeasonAlreadyActive
	}

	var existing int
	err = database.DB.QueryRow(
		`SELECT COUNT(*) FROM season_enrollments WHERE season_id = ? AND bot_id = ?`,
		seasonID.String(), botID.String(),
	).Scan(&existing)
	if err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrAlreadyEnrolled
	}

	enrollment := &models.SeasonEnrollment{
		ID:          uuid.New(),
		SeasonID:    seasonID,
		BotID:       botID,
		CashBalance: sn.StartingBalance,
		EnrolledAt:  time.Now().UTC(),
	}
	// start_value is a legacy column from migration 007 — set it equal to
	// starting_balance so return_pct calculations remain consistent if any
	// path still reads it.
	_, err = database.DB.Exec(
		`INSERT INTO season_enrollments (id, season_id, bot_id, start_value, cash_balance, enrolled_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		enrollment.ID.String(), seasonID.String(), botID.String(),
		sn.StartingBalance, enrollment.CashBalance, enrollment.EnrolledAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	return enrollment, nil
}

// Leaderboard returns the standings for a season. For active seasons it
// computes live values (cash + mark-to-market position value) per enrollment.
// For closed seasons it returns the immutable end_value / final_rank.
func (s *SeasonService) Leaderboard(seasonID uuid.UUID) (*models.Season, []models.SeasonLeaderboardEntry, error) {
	sn, err := s.GetSeason(seasonID.String())
	if err != nil {
		return nil, nil, err
	}

	rows, err := database.DB.Query(`
		SELECT e.bot_id, b.name, COALESCE(b.creator_email, ''),
		       e.cash_balance, e.end_value, e.return_pct, e.final_rank
		FROM season_enrollments e
		JOIN bots b ON b.id = e.bot_id
		WHERE e.season_id = ?
	`, seasonID.String())
	if err != nil {
		return sn, nil, err
	}

	type seed struct {
		botIDStr, botName, creatorEmail string
		cashBalance                     float64
		endValue, returnPct             sql.NullFloat64
		finalRank                       sql.NullInt64
	}
	var seeds []seed
	for rows.Next() {
		var sd seed
		if err := rows.Scan(&sd.botIDStr, &sd.botName, &sd.creatorEmail,
			&sd.cashBalance, &sd.endValue, &sd.returnPct, &sd.finalRank); err != nil {
			continue
		}
		seeds = append(seeds, sd)
	}
	rows.Close()

	final := sn.Status == models.SeasonStatusClosed
	var entries []models.SeasonLeaderboardEntry

	for _, sd := range seeds {
		entry := models.SeasonLeaderboardEntry{
			BotID:        sd.botIDStr,
			BotName:      sd.botName,
			CreatorEmail: maskEmailPublic(sd.creatorEmail),
			Final:        final,
		}
		if final && sd.endValue.Valid {
			entry.TotalValue = sd.endValue.Float64
			entry.CashBalance = sd.cashBalance
			entry.PositionsValue = sd.endValue.Float64 - sd.cashBalance
			if sd.returnPct.Valid {
				entry.ReturnPct = sd.returnPct.Float64
			}
		} else {
			botID, err := uuid.Parse(sd.botIDStr)
			if err != nil {
				continue
			}
			p, err := s.portfolio.GetSeasonPortfolio(botID, seasonID)
			if err != nil {
				continue
			}
			entry.CashBalance = p.CashBalance
			entry.PositionsValue = p.TotalValue - p.CashBalance
			entry.TotalValue = p.TotalValue
			if sn.StartingBalance > 0 {
				entry.ReturnPct = (p.TotalValue - sn.StartingBalance) / sn.StartingBalance
			}
		}

		// Per-season trade count — cheap, gives the leaderboard a sense of
		// who is actually playing vs. who's idling at $100k.
		botID, err := uuid.Parse(sd.botIDStr)
		if err == nil {
			database.DB.QueryRow(
				`SELECT COUNT(*) FROM trades WHERE bot_id = ? AND season_id = ?`,
				botID.String(), seasonID.String(),
			).Scan(&entry.TradeCount)
		}
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].TotalValue > entries[j].TotalValue
	})
	for i := range entries {
		entries[i].Rank = i + 1
	}
	return sn, entries, nil
}

// StartSeason flips a pending season to active. If auto_enroll is set, every
// currently-active bot that isn't already enrolled gets a $100k account.
func (s *SeasonService) StartSeason(seasonID uuid.UUID) error {
	sn, err := s.GetSeason(seasonID.String())
	if err != nil {
		return err
	}
	if sn.Status != models.SeasonStatusPending {
		return nil
	}

	if sn.AutoEnroll {
		botIDs, err := s.listActiveBotIDs()
		if err != nil {
			return err
		}
		for _, botID := range botIDs {
			// Reuse EnrollBot rather than duplicating insert logic — but
			// EnrollBot refuses active seasons. Temporarily insert directly
			// here since we're about to flip status.
			_, err := s.insertEnrollment(seasonID, botID, sn.StartingBalance)
			if err != nil && !errors.Is(err, ErrAlreadyEnrolled) {
				continue
			}
		}
	}

	_, err = database.DB.Exec(
		`UPDATE seasons SET status = ? WHERE id = ?`,
		models.SeasonStatusActive, seasonID.String(),
	)
	return err
}

// insertEnrollment is the bare insert used by StartSeason for auto_enroll
// (which would otherwise hit EnrollBot's "season already active" guard
// during the brief window between auto-enroll and the status flip).
func (s *SeasonService) insertEnrollment(seasonID, botID uuid.UUID, startingBalance float64) (*models.SeasonEnrollment, error) {
	var existing int
	err := database.DB.QueryRow(
		`SELECT COUNT(*) FROM season_enrollments WHERE season_id = ? AND bot_id = ?`,
		seasonID.String(), botID.String(),
	).Scan(&existing)
	if err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrAlreadyEnrolled
	}
	enrollment := &models.SeasonEnrollment{
		ID: uuid.New(), SeasonID: seasonID, BotID: botID,
		CashBalance: startingBalance, EnrolledAt: time.Now().UTC(),
	}
	_, err = database.DB.Exec(
		`INSERT INTO season_enrollments (id, season_id, bot_id, start_value, cash_balance, enrolled_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		enrollment.ID.String(), seasonID.String(), botID.String(),
		startingBalance, startingBalance, enrollment.EnrolledAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	return enrollment, nil
}

// CloseSeason finalizes a season: marks every enrollment's positions to
// market, sums (cash + positions_value) as end_value, sorts, assigns
// final_rank, and flips status to closed. Idempotent.
func (s *SeasonService) CloseSeason(seasonID uuid.UUID) error {
	sn, err := s.GetSeason(seasonID.String())
	if err != nil {
		return err
	}
	if sn.Status == models.SeasonStatusClosed {
		return nil
	}

	rows, err := database.DB.Query(
		`SELECT id, bot_id FROM season_enrollments WHERE season_id = ?`,
		seasonID.String(),
	)
	if err != nil {
		return err
	}
	type pending struct {
		ID    string
		BotID uuid.UUID
	}
	var ids []pending
	for rows.Next() {
		var idStr, botIDStr string
		if err := rows.Scan(&idStr, &botIDStr); err != nil {
			continue
		}
		botID, err := uuid.Parse(botIDStr)
		if err != nil {
			continue
		}
		ids = append(ids, pending{ID: idStr, BotID: botID})
	}
	rows.Close()

	type closeRow struct {
		ID       string
		EndValue float64
		Return   float64
	}
	closes := make([]closeRow, 0, len(ids))
	for _, p := range ids {
		port, err := s.portfolio.GetSeasonPortfolio(p.BotID, seasonID)
		if err != nil {
			continue
		}
		ret := 0.0
		if sn.StartingBalance > 0 {
			ret = (port.TotalValue - sn.StartingBalance) / sn.StartingBalance
		}
		closes = append(closes, closeRow{ID: p.ID, EndValue: port.TotalValue, Return: ret})
	}

	sort.SliceStable(closes, func(i, j int) bool { return closes[i].EndValue > closes[j].EndValue })

	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, r := range closes {
		_, err := tx.Exec(
			`UPDATE season_enrollments
			 SET end_value = ?, return_pct = ?, final_rank = ?
			 WHERE id = ?`,
			r.EndValue, r.Return, i+1, r.ID,
		)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(
		`UPDATE seasons SET status = ? WHERE id = ?`,
		models.SeasonStatusClosed, seasonID.String(),
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SeasonService) FindStartableSeasons(now time.Time) ([]uuid.UUID, error) {
	return s.findByStatusAndTime(models.SeasonStatusPending, "starts_at", now)
}

func (s *SeasonService) FindClosableSeasons(now time.Time) ([]uuid.UUID, error) {
	return s.findByStatusAndTime(models.SeasonStatusActive, "ends_at", now)
}

func (s *SeasonService) findByStatusAndTime(status, column string, now time.Time) ([]uuid.UUID, error) {
	q := fmt.Sprintf(`SELECT id FROM seasons WHERE status = ? AND %s <= ?`, column)
	rows, err := database.DB.Query(q, status, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var idStr string
		if err := rows.Scan(&idStr); err != nil {
			continue
		}
		if id, err := uuid.Parse(idStr); err == nil {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *SeasonService) listActiveBotIDs() ([]uuid.UUID, error) {
	rows, err := database.DB.Query(`SELECT id FROM bots WHERE is_active = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var idStr string
		if err := rows.Scan(&idStr); err != nil {
			continue
		}
		if id, err := uuid.Parse(idStr); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func maskEmailPublic(email string) string {
	if email == "" {
		return ""
	}
	at := -1
	for i, c := range email {
		if c == '@' {
			at = i
			break
		}
	}
	if at < 1 {
		return email
	}
	local := email[:at]
	domain := email[at:]
	if len(local) <= 2 {
		return local[:1] + "***" + domain
	}
	return string(local[0]) + "***" + string(local[len(local)-1]) + domain
}
