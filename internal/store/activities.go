package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

var (
	ErrBusy     = errors.New("you are already busy with something")
	ErrEnded    = errors.New("that has already ended")
	ErrOwnEvent = errors.New("you can't attend your own event")
)

type Activity struct {
	ID                int64
	GuildID           string
	HostID            int64
	Kind              string
	Goal              string
	Spend             int   // gold the host paid up front
	TargetID          int64 // kind-specific, e.g. the senator being courted
	Public            bool
	AnnounceChannelID string
	AnnounceMessageID string
	StartedAt         time.Time
	EndsAt            time.Time
	Resolved          bool
}

type NewEvent struct {
	CharacterID int64 // who gets it; 0 = the host
	Key         string
	FiresAt     time.Time
	Deadline    time.Time
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
	Choice      string // the chosen option, once answered
	Result      *game.EventOutcome
}

// Outcome is everything an activity changes when it resolves.
type Outcome struct {
	Deltas     map[int64]game.Delta
	Opinions   []game.OpinionChange
	Conditions []NewCondition
	Spies      []game.SpyChange
	Realm      game.Realm    // change to the court's trackers
	Monuments  []NewMonument // finished public works
}

// NewMonument records a finished public work.
type NewMonument struct {
	CharacterID int64
	Key         string
}

// ErrBuilt means the character already has that public work.
var (
	ErrBuilt    = errors.New("you have already built one of those")
	ErrBuilding = errors.New("you are already building something")
)

// slotFilter is the SQL condition for activities that take up a
// character's action (all but game.FreeKinds), with its args.
func slotFilter() (string, []any) {
	q, args := "1 = 1", []any{}
	for _, k := range game.FreeKinds {
		q += " AND kind != ?"
		args = append(args, k)
	}
	return q, args
}

// StartActivity pays the cost (recorded as the activity's Spend) and creates the activity with its scheduled
// events. A character can host one activity at a time.
func (s *Store) StartActivity(ctx context.Context, a Activity, cost int, events []NewEvent) (Activity, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()

	// Slot kinds compete for the one action; a free kind only with itself.
	var busy int
	where, args := slotFilter()
	if !game.UsesSlot(a.Kind) {
		where, args = "kind = ?", []any{a.Kind}
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM activities WHERE host_id = ? AND resolved_at IS NULL AND `+where,
		append([]any{a.HostID}, args...)...).Scan(&busy); err != nil {
		return a, err
	}
	if busy > 0 {
		if !game.UsesSlot(a.Kind) {
			return a, ErrBuilding
		}
		return a, ErrBusy
	}
	if a.Kind == game.KindWork {
		var built int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM monuments WHERE character_id = ? AND work_key = ?`,
			a.HostID, a.Goal).Scan(&built); err != nil {
			return a, err
		}
		if built > 0 {
			return a, ErrBuilt
		}
	}
	if cost > 0 {
		if err := spendOr(ctx, tx, a.HostID, cost, fmt.Sprintf("you need %d gold for that", cost)); err != nil {
			return a, err
		}
	}
	a.Spend = cost
	res, err := tx.ExecContext(ctx,
		`INSERT INTO activities (guild_id, host_id, kind, goal, spend, target_id, public, started_at, ends_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.GuildID, a.HostID, a.Kind, a.Goal, a.Spend, a.TargetID, a.Public, a.StartedAt.Unix(), a.EndsAt.Unix())
	if err != nil {
		return a, err
	}
	a.ID, _ = res.LastInsertId()
	if err := insertEvents(ctx, tx, a, events); err != nil {
		return a, err
	}
	return a, tx.Commit()
}

func (s *Store) SetAnnouncement(ctx context.Context, activityID int64, channelID, messageID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE activities SET announce_channel_id = ?, announce_message_id = ? WHERE id = ?`, channelID, messageID, activityID)
	return err
}

const activityCols = `id, guild_id, host_id, kind, goal, spend, target_id, public, announce_channel_id, announce_message_id, started_at, ends_at, resolved_at`

func scanActivity(row scanner) (Activity, error) {
	var a Activity
	var ch, msg sql.NullString
	var started, ends int64
	var resolved sql.NullInt64
	err := row.Scan(&a.ID, &a.GuildID, &a.HostID, &a.Kind, &a.Goal, &a.Spend, &a.TargetID, &a.Public, &ch, &msg, &started, &ends, &resolved)
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

// HostedActivity returns the character's unresolved activity that takes up
// their action. Public works being built don't count; see WorkInProgress.
func (s *Store) HostedActivity(ctx context.Context, hostID int64) (Activity, error) {
	where, args := slotFilter()
	return scanActivity(s.db.QueryRowContext(ctx,
		`SELECT `+activityCols+` FROM activities WHERE host_id = ? AND resolved_at IS NULL AND `+where+` LIMIT 1`,
		append([]any{hostID}, args...)...))
}

// WorkInProgress returns the public work the character is building.
func (s *Store) WorkInProgress(ctx context.Context, hostID int64) (Activity, error) {
	return scanActivity(s.db.QueryRowContext(ctx,
		`SELECT `+activityCols+` FROM activities WHERE host_id = ? AND kind = ? AND resolved_at IS NULL LIMIT 1`,
		hostID, game.KindWork))
}

// EndedActivities lists unresolved activities whose time is up.
func (s *Store) EndedActivities(ctx context.Context, now time.Time) ([]Activity, error) {
	return queryAll(ctx, s.db, `SELECT `+activityCols+` FROM activities WHERE resolved_at IS NULL AND ends_at <= ?`,
		func(r scanner) (Activity, error) { return scanActivity(r) }, now.Unix())
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
	// The insert checks the activity is still open, in case it ended since
	// it was read above.
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO participants (activity_id, character_id, goal, target_id, joined_at)
		 SELECT ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM activities WHERE id = ? AND resolved_at IS NULL AND ends_at > ?)
		 ON CONFLICT DO NOTHING`, activityID, charID, goal, target, now.Unix(), activityID, now.Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.whyNotJoined(ctx, s.db, activityID, charID)
	}
	return nil
}

// whyNotJoined explains a guarded participant insert that did nothing:
// already joined, or the activity ended.
func (s *Store) whyNotJoined(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, activityID, charID int64) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM participants WHERE activity_id = ? AND character_id = ?`,
		activityID, charID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrExists
	}
	return ErrEnded
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
	return out, s.fillAll(ctx, ptrs)
}

// ---- events ----

const eventCols = `e.id, e.activity_id, e.character_id, c.user_id, e.event_key, e.fires_at, e.deadline_at,
	e.dm_channel_id, e.dm_message_id, COALESCE(e.choice, ''), e.result`

func insertEvents(ctx context.Context, tx *sql.Tx, a Activity, events []NewEvent) error {
	for _, e := range events {
		who := e.CharacterID
		if who == 0 {
			who = a.HostID
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO activity_events (activity_id, character_id, event_key, fires_at, deadline_at) VALUES (?, ?, ?, ?, ?)`,
			a.ID, who, e.Key, e.FiresAt.Unix(), e.Deadline.Unix()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) queryEvents(ctx context.Context, where string, args ...any) ([]Event, error) {
	return queryAll(ctx, s.db,
		`SELECT `+eventCols+` FROM activity_events e JOIN characters c ON c.id = e.character_id WHERE `+where,
		func(r scanner) (Event, error) {
			var e Event
			var fires, deadline int64
			var ch, msg, result sql.NullString
			if err := r.Scan(&e.ID, &e.ActivityID, &e.CharacterID, &e.UserID, &e.Key, &fires, &deadline, &ch, &msg, &e.Choice, &result); err != nil {
				return e, err
			}
			e.FiresAt, e.Deadline = time.Unix(fires, 0), time.Unix(deadline, 0)
			e.DMChannelID, e.DMMessageID = ch.String, msg.String
			if result.Valid {
				e.Result = &game.EventOutcome{}
				if err := json.Unmarshal([]byte(result.String), e.Result); err != nil {
					return e, err
				}
			}
			return e, nil
		}, args...)
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

// ApplyResult applies an activity's outcome and marks it resolved, once.
func (s *Store) ApplyResult(ctx context.Context, activityID int64, out Outcome, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := resolveOnce(ctx, tx, activityID, now); err != nil {
		return err
	}
	var guildID string
	if err := tx.QueryRowContext(ctx, `SELECT guild_id FROM activities WHERE id = ?`, activityID).Scan(&guildID); err != nil {
		return err
	}
	if err := applyOutcome(ctx, tx, guildID, out, now); err != nil {
		return err
	}
	return tx.Commit()
}

func applyOutcome(ctx context.Context, tx *sql.Tx, guildID string, out Outcome, now time.Time) error {
	if len(out.Realm) > 0 {
		if err := applyRealm(ctx, tx, guildID, out.Realm); err != nil {
			return err
		}
	}
	for _, m := range out.Monuments {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO monuments (character_id, work_key, built_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
			m.CharacterID, m.Key, now.Unix()); err != nil {
			return err
		}
	}
	for id, d := range out.Deltas {
		if err := applyDelta(ctx, tx, id, d); err != nil {
			return err
		}
	}
	if err := changeOpinions(ctx, tx, out.Opinions); err != nil {
		return err
	}
	if err := changeSpies(ctx, tx, out.Spies); err != nil {
		return err
	}
	return addConditions(ctx, tx, out.Conditions)
}

// applyRealm adds a change to a court's trackers, clamped to their range.
// If that ends the realm, the realm and court are marked fallen. A fallen
// realm doesn't change.
func applyRealm(ctx context.Context, tx *sql.Tx, guildID string, d game.Realm) error {
	var raw, status string
	err := tx.QueryRowContext(ctx, `SELECT trackers, status FROM realms WHERE guild_id = ?`, guildID).Scan(&raw, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // no realm yet: nothing to change
	} else if err != nil {
		return err
	}
	if status != realmAlive {
		return nil
	}
	r := game.Realm{}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return err
	}
	r.Apply(d)
	b, _ := json.Marshal(r)
	if _, err = tx.ExecContext(ctx, `UPDATE realms SET trackers = ? WHERE guild_id = ?`, string(b), guildID); err != nil {
		return err
	}
	if fell, why := r.Fallen(); fell {
		if _, err := tx.ExecContext(ctx, `UPDATE realms SET status = ?, fallen_reason = ? WHERE guild_id = ?`, realmFallen, why, guildID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE courts SET status = ? WHERE guild_id = ?`, courtFallen, guildID); err != nil {
			return err
		}
	}
	return nil
}

// ApplyAid takes a gift's gold from the character and applies its effects
// at once; there's no activity. It fails if they can't afford it or the
// realm has fallen.
func (s *Store) ApplyAid(ctx context.Context, charID int64, guildID string, gold int, realm game.Realm, d game.Delta, ops []game.OpinionChange) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM realms WHERE guild_id = ?`, guildID).Scan(&status); err != nil {
		return err
	}
	if status != realmAlive {
		return ErrEnded
	}
	if err := spendOr(ctx, tx, charID, gold, fmt.Sprintf("you need %d gold for that", gold)); err != nil {
		return err
	}
	if err := applyRealm(ctx, tx, guildID, realm); err != nil {
		return err
	}
	if err := applyDelta(ctx, tx, charID, d); err != nil {
		return err
	}
	if err := changeOpinions(ctx, tx, ops); err != nil {
		return err
	}
	return tx.Commit()
}

// fillMonuments loads each character's finished public works.
func (s *Store) fillMonuments(ctx context.Context, chars []*Character) error {
	if len(chars) == 0 {
		return nil
	}
	byID := map[int64]*Character{}
	args := make([]any, 0, len(chars))
	for _, c := range chars {
		byID[c.ID] = c
		c.Monuments = nil
		args = append(args, c.ID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT character_id, work_key FROM monuments WHERE character_id IN (?`+
		strings.Repeat(",?", len(args)-1)+`) ORDER BY built_at, work_key`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			return err
		}
		byID[id].Monuments = append(byID[id].Monuments, key)
	}
	return rows.Err()
}

// AbandonActivity marks an activity and its open events resolved without
// applying anything, for rows that can never be resolved properly.
func (s *Store) AbandonActivity(ctx context.Context, activityID int64, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE activity_events SET resolved_at = ? WHERE activity_id = ? AND resolved_at IS NULL`,
			now.Unix(), activityID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE activities SET resolved_at = ? WHERE id = ? AND resolved_at IS NULL`, now.Unix(), activityID)
		return err
	})
}
