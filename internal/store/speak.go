package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RecordVisit counts a call on a senator and returns how many there were
// before it.
func (s *Store) RecordVisit(ctx context.Context, senatorID, charID int64, now time.Time) (int, error) {
	var before int
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT visits FROM senator_visits WHERE senator_id = ? AND character_id = ?`,
			senatorID, charID).Scan(&before)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO senator_visits (senator_id, character_id, visits, last_at) VALUES (?, ?, 1, ?)
			ON CONFLICT (senator_id, character_id) DO UPDATE SET visits = visits + 1, last_at = excluded.last_at`,
			senatorID, charID, now.Unix())
		return err
	})
	return before, err
}

// Visits is how many times a character has called on a senator.
func (s *Store) Visits(ctx context.Context, senatorID, charID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT visits FROM senator_visits WHERE senator_id = ? AND character_id = ?`,
		senatorID, charID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// VoteAsk is what a senator told a character about their vote on a matter.
type VoteAsk struct {
	MatterID  int64
	SenatorID int64
	Reply     string // game.AskReply
	Option    string
	Text      string
}

// VoteAsks is everything a character has been told by one senator about
// these matters, by matter ID.
func (s *Store) VoteAsks(ctx context.Context, senatorID, charID int64, matterIDs []int64) (map[int64]VoteAsk, error) {
	out := map[int64]VoteAsk{}
	if len(matterIDs) == 0 {
		return out, nil
	}
	in, args := inList(matterIDs)
	asks, err := queryAll(ctx, s.db,
		`SELECT matter_id, senator_id, reply, option, text FROM vote_asks
		 WHERE senator_id = ? AND character_id = ? AND matter_id IN `+in,
		func(r scanner) (VoteAsk, error) {
			var a VoteAsk
			return a, r.Scan(&a.MatterID, &a.SenatorID, &a.Reply, &a.Option, &a.Text)
		}, append([]any{senatorID, charID}, args...)...)
	for _, a := range asks {
		out[a.MatterID] = a
	}
	return out, err
}

// SaveVoteAsk records an answer, unless one is already recorded, and
// returns the answer that stands: asking twice at once can't reroll it.
func (s *Store) SaveVoteAsk(ctx context.Context, charID int64, a VoteAsk, now time.Time) (VoteAsk, bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO vote_asks (matter_id, senator_id, character_id, reply, option, text, asked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		a.MatterID, a.SenatorID, charID, a.Reply, a.Option, a.Text, now.Unix())
	if err != nil {
		return a, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return a, true, nil
	}
	got, err := s.VoteAsks(ctx, a.SenatorID, charID, []int64{a.MatterID})
	return got[a.MatterID], false, err
}
