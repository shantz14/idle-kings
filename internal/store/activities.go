package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

var (
	ErrBusy     = errors.New("you are already hosting something")
	ErrEnded    = errors.New("that has already ended")
	ErrOwnEvent = errors.New("you can't attend your own event")
)

type Activity struct {
	ID                int64
	GuildID           string
	HostID            int64
	Kind              string
	Goal              string
	Spend             int // gold the host paid up front
	Public            bool
	AnnounceChannelID string
	AnnounceMessageID string
	StartedAt         time.Time
	EndsAt            time.Time
	Resolved          bool
}

type NewEvent struct {
	Key      string
	FiresAt  time.Time
	Deadline time.Time
}

type Participant struct {
	Character Character
	Goal      string
	TargetID  int64 // 0 = none
}

type Event struct {
	ID          int64
	ActivityID  int64
	CharacterID int64
	UserID      string // owner of CharacterID
	Key         string
	FiresAt     time.Time
	Deadline    time.Time
	DMChannelID string
	DMMessageID string
	Result      *game.EventOutcome
}

// StartActivity pays the cost (recorded as the activity's Spend) and creates the activity with its scheduled
// events. A character can host one activity at a time.
func (s *Store) StartActivity(ctx context.Context, a Activity, cost int, events []NewEvent) (Activity, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()

	var busy int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM activities WHERE host_id = ? AND resolved_at IS NULL`, a.HostID).Scan(&busy); err != nil {
		return a, err
	}
	if busy > 0 {
		return a, ErrBusy
	}
	if cost > 0 {
		res, err := tx.ExecContext(ctx, `UPDATE characters SET wealth = wealth - ? WHERE id = ? AND wealth >= ?`, cost, a.HostID, cost)
		if err != nil {
			return a, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return a, fmt.Errorf("you need %d gold for that", cost)
		}
	}
	a.Spend = cost
	res, err := tx.ExecContext(ctx,
		`INSERT INTO activities (guild_id, host_id, kind, goal, spend, public, started_at, ends_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.GuildID, a.HostID, a.Kind, a.Goal, a.Spend, a.Public, a.StartedAt.Unix(), a.EndsAt.Unix())
	if err != nil {
		return a, err
	}
	a.ID, _ = res.LastInsertId()
	for _, e := range events {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO activity_events (activity_id, character_id, event_key, fires_at, deadline_at) VALUES (?, ?, ?, ?, ?)`,
			a.ID, a.HostID, e.Key, e.FiresAt.Unix(), e.Deadline.Unix()); err != nil {
			return a, err
		}
	}
	return a, tx.Commit()
}

func (s *Store) SetAnnouncement(ctx context.Context, activityID int64, channelID, messageID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE activities SET announce_channel_id = ?, announce_message_id = ? WHERE id = ?`, channelID, messageID, activityID)
	return err
}

const activityCols = `id, guild_id, host_id, kind, goal, spend, public, announce_channel_id, announce_message_id, started_at, ends_at, resolved_at`

func scanActivity(row scanner) (Activity, error) {
	var a Activity
	var ch, msg sql.NullString
	var started, ends int64
	var resolved sql.NullInt64
	err := row.Scan(&a.ID, &a.GuildID, &a.HostID, &a.Kind, &a.Goal, &a.Spend, &a.Public, &ch, &msg, &started, &ends, &resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	a.AnnounceChannelID, a.AnnounceMessageID = ch.String, msg.String
	a.StartedAt, a.EndsAt, a.Resolved = time.Unix(started, 0), time.Unix(ends, 0), resolved.Valid
	return a, err
}

func (s *Store) GetActivity(ctx context.Context, id int64) (Activity, error) {
	return scanActivity(s.db.QueryRowContext(ctx, `SELECT `+activityCols+` FROM activities WHERE id = ?`, id))
}

// HostedActivity returns the character's unresolved hosted activity.
func (s *Store) HostedActivity(ctx context.Context, hostID int64) (Activity, error) {
	return scanActivity(s.db.QueryRowContext(ctx,
		`SELECT `+activityCols+` FROM activities WHERE host_id = ? AND resolved_at IS NULL`, hostID))
}

// EndedActivities lists unresolved activities whose time is up.
func (s *Store) EndedActivities(ctx context.Context, now time.Time) ([]Activity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+activityCols+` FROM activities WHERE resolved_at IS NULL AND ends_at <= ?`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AddParticipant lets a character join a public activity.
func (s *Store) AddParticipant(ctx context.Context, activityID, charID int64, goal string, targetID int64, now time.Time) error {
	a, err := s.GetActivity(ctx, activityID)
	if err != nil {
		return err
	}
	if a.Resolved || !now.Before(a.EndsAt) {
		return ErrEnded
	}
	if a.HostID == charID {
		return ErrOwnEvent
	}
	var target any
	if targetID != 0 {
		target = targetID
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO participants (activity_id, character_id, goal, target_id, joined_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT DO NOTHING`, activityID, charID, goal, target, now.Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrExists
	}
	return nil
}

func (s *Store) Participants(ctx context.Context, activityID int64) ([]Participant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+charCols+`, p.goal, COALESCE(p.target_id, 0)
		 FROM participants p JOIN characters c ON c.id = p.character_id
		 WHERE p.activity_id = ? ORDER BY p.joined_at`, activityID)
	if err != nil {
		return nil, err
	}
	var out []Participant
	for rows.Next() {
		var p Participant
		c, err := scanCharacter(rows, &p.Goal, &p.TargetID)
		if err != nil {
			rows.Close()
			return nil, err
		}
		p.Character = c
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	ptrs := make([]*Character, len(out))
	for i := range out {
		ptrs[i] = &out[i].Character
	}
	return out, s.fillItems(ctx, ptrs)
}

// ---- events ----

const eventCols = `e.id, e.activity_id, e.character_id, c.user_id, e.event_key, e.fires_at, e.deadline_at,
	e.dm_channel_id, e.dm_message_id, e.result`

func (s *Store) queryEvents(ctx context.Context, where string, args ...any) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+eventCols+` FROM activity_events e JOIN characters c ON c.id = e.character_id WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var fires, deadline int64
		var ch, msg, result sql.NullString
		if err := rows.Scan(&e.ID, &e.ActivityID, &e.CharacterID, &e.UserID, &e.Key, &fires, &deadline, &ch, &msg, &result); err != nil {
			return nil, err
		}
		e.FiresAt, e.Deadline = time.Unix(fires, 0), time.Unix(deadline, 0)
		e.DMChannelID, e.DMMessageID = ch.String, msg.String
		if result.Valid {
			e.Result = &game.EventOutcome{}
			if err := json.Unmarshal([]byte(result.String), e.Result); err != nil {
				return nil, err
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEvent(ctx context.Context, id int64) (Event, error) {
	evs, err := s.queryEvents(ctx, `e.id = ?`, id)
	if err != nil {
		return Event{}, err
	}
	if len(evs) == 0 {
		return Event{}, ErrNotFound
	}
	return evs[0], nil
}

// DueEvents lists events whose time has come but haven't been sent.
func (s *Store) DueEvents(ctx context.Context, now time.Time) ([]Event, error) {
	return s.queryEvents(ctx, `e.sent_at IS NULL AND e.resolved_at IS NULL AND e.fires_at <= ?`, now.Unix())
}

// ExpiredEvents lists unanswered events past their deadline.
func (s *Store) ExpiredEvents(ctx context.Context, now time.Time) ([]Event, error) {
	return s.queryEvents(ctx, `e.resolved_at IS NULL AND e.deadline_at <= ?`, now.Unix())
}

// OpenEvents lists an activity's unanswered events.
func (s *Store) OpenEvents(ctx context.Context, activityID int64) ([]Event, error) {
	return s.queryEvents(ctx, `e.activity_id = ? AND e.resolved_at IS NULL`, activityID)
}

// EventResults lists an activity's answered events.
func (s *Store) EventResults(ctx context.Context, activityID int64) ([]Event, error) {
	return s.queryEvents(ctx, `e.activity_id = ? AND e.resolved_at IS NOT NULL`, activityID)
}

func (s *Store) MarkEventSent(ctx context.Context, id int64, channelID, messageID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE activity_events SET sent_at = ?, dm_channel_id = ?, dm_message_id = ? WHERE id = ?`,
		now.Unix(), channelID, messageID, id)
	return err
}

// ResolveEvent records the choice. Returns ErrEnded if it was already
// resolved (e.g. a click racing the timeout).
func (s *Store) ResolveEvent(ctx context.Context, id int64, choice string, out game.EventOutcome, now time.Time) error {
	b, _ := json.Marshal(out)
	res, err := s.db.ExecContext(ctx,
		`UPDATE activity_events SET choice = ?, result = ?, resolved_at = ? WHERE id = ? AND resolved_at IS NULL`,
		choice, string(b), now.Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrEnded
	}
	return nil
}

// ApplyResult applies every delta and marks the activity resolved, once.
func (s *Store) ApplyResult(ctx context.Context, activityID int64, deltas map[int64]game.Delta, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE activities SET resolved_at = ? WHERE id = ? AND resolved_at IS NULL`, now.Unix(), activityID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrEnded
	}
	for id, d := range deltas {
		if err := applyDelta(ctx, tx, id, d); err != nil {
			return err
		}
	}
	return tx.Commit()
}
