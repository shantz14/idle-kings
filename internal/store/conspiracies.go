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

// ---- conspiracies ----
//
// A plot is an activity of kind game.KindConspiracy (free: it doesn't take
// the leader's action) with a row in conspiracies for its state, and a row
// in conspiracy_members for everyone asked to join. The leader's strike is
// an activity of kind game.KindStrike whose target_id is the plot's id.

// Member kinds and statuses.
const (
	MemberPlayer  = "player"
	MemberSenator = "senator"

	MemberInvited  = "invited"
	MemberJoined   = "joined"
	MemberDeclined = "declined"
	MemberLeft     = "left"
)

var (
	ErrInvited  = errors.New("they have already been asked")
	ErrPlotting = errors.New("you are already leading a conspiracy")
)

// Conspiracy is a plot and everyone asked into it.
type Conspiracy struct {
	Activity
	TargetKind game.TargetKind
	TargetItem int64 // estate_items.id to steal; 0 = their coffers
	Purse      string
	Progress   float64
	Warned     game.Discovery
	NextStepAt time.Time
	Members    []ConspiracyMember
}

// ConspiracyMember is one player or senator asked into a plot.
type ConspiracyMember struct {
	Kind   string // MemberPlayer or MemberSenator
	ID     int64
	Status string
}

// Joined lists the ids of members of one kind who joined.
func (c Conspiracy) Joined(kind string) []int64 {
	var out []int64
	for _, m := range c.Members {
		if m.Kind == kind && m.Status == MemberJoined {
			out = append(out, m.ID)
		}
	}
	return out
}

// Member finds someone asked into the plot.
func (c Conspiracy) Member(kind string, id int64) (ConspiracyMember, bool) {
	for _, m := range c.Members {
		if m.Kind == kind && m.ID == id {
			return m, true
		}
	}
	return ConspiracyMember{}, false
}

// StartConspiracy pays the purse and starts a plot. A character leads one
// plot at a time.
func (s *Store) StartConspiracy(ctx context.Context, a Activity, cost int, k Conspiracy) (Conspiracy, error) {
	a.Kind = game.KindConspiracy
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		if a, err = startActivity(ctx, tx, a, cost, nil); errors.Is(err, ErrBuilding) {
			return ErrPlotting
		} else if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO conspiracies (activity_id, target_kind, target_item, purse, progress, warned, next_step_at) VALUES (?, ?, ?, ?, 0, 0, ?)`,
			a.ID, string(k.TargetKind), k.TargetItem, k.Purse, k.NextStepAt.Unix())
		return err
	})
	k.Activity = a
	return k, err
}

const conspiracyCols = `k.target_kind, k.target_item, k.purse, k.progress, k.warned, k.next_step_at`

func (s *Store) conspiracies(ctx context.Context, where string, args ...any) ([]Conspiracy, error) {
	cols := `a.id, a.guild_id, a.host_id, a.kind, a.goal, a.spend, a.target_id, a.public, a.announce_channel_id, a.announce_message_id, a.started_at, a.ends_at, a.resolved_at, ` + conspiracyCols
	out, err := queryAll(ctx, s.db,
		`SELECT `+cols+` FROM conspiracies k JOIN activities a ON a.id = k.activity_id WHERE `+where+` ORDER BY a.id`,
		func(r scanner) (Conspiracy, error) {
			var k Conspiracy
			var tk string
			var warned int
			var next int64
			act, err := scanActivity(rowWith{r, []any{&tk, &k.TargetItem, &k.Purse, &k.Progress, &warned, &next}})
			k.Activity, k.TargetKind, k.Warned, k.NextStepAt = act, game.TargetKind(tk), game.Discovery(warned), time.Unix(next, 0)
			return k, err
		}, args...)
	if err != nil || len(out) == 0 {
		return out, err
	}
	ids := make([]int64, len(out))
	for i, k := range out {
		ids[i] = k.ID
	}
	in, inArgs := inList(ids)
	type row struct {
		plot int64
		m    ConspiracyMember
	}
	members, err := queryAll(ctx, s.db,
		`SELECT activity_id, member_kind, member_id, status FROM conspiracy_members WHERE activity_id IN `+in+` ORDER BY invited_at, member_id`,
		func(r scanner) (row, error) {
			var x row
			return x, r.Scan(&x.plot, &x.m.Kind, &x.m.ID, &x.m.Status)
		}, inArgs...)
	if err != nil {
		return nil, err
	}
	pos := map[int64]int{}
	for i, k := range out {
		pos[k.ID] = i
	}
	for _, x := range members {
		out[pos[x.plot]].Members = append(out[pos[x.plot]].Members, x.m)
	}
	return out, nil
}

// rowWith scans an activity's columns, then extra ones.
type rowWith struct {
	r     scanner
	extra []any
}

func (w rowWith) Scan(dest ...any) error { return w.r.Scan(append(dest, w.extra...)...) }

// GetConspiracy loads a plot, resolved or not.
func (s *Store) GetConspiracy(ctx context.Context, id int64) (Conspiracy, error) {
	ks, err := s.conspiracies(ctx, `a.id = ?`, id)
	if err != nil {
		return Conspiracy{}, err
	}
	if len(ks) == 0 {
		return Conspiracy{}, ErrNotFound
	}
	return ks[0], nil
}

// LedConspiracy is the plot a character is leading, if any.
func (s *Store) LedConspiracy(ctx context.Context, leaderID int64) (Conspiracy, error) {
	ks, err := s.conspiracies(ctx, `a.host_id = ? AND a.kind = ? AND a.resolved_at IS NULL`, leaderID, game.KindConspiracy)
	if err != nil {
		return Conspiracy{}, err
	}
	if len(ks) == 0 {
		return Conspiracy{}, ErrNotFound
	}
	return ks[0], nil
}

// ConspiraciesOf lists the open plots a player has been asked into or
// joined (not ones they lead).
func (s *Store) ConspiraciesOf(ctx context.Context, charID int64) ([]Conspiracy, error) {
	return s.conspiracies(ctx,
		`a.resolved_at IS NULL AND a.id IN (SELECT activity_id FROM conspiracy_members
		   WHERE member_kind = ? AND member_id = ? AND status IN (?, ?))`,
		MemberPlayer, charID, MemberInvited, MemberJoined)
}

// DueConspiracies lists open plots whose next day has come.
func (s *Store) DueConspiracies(ctx context.Context, now time.Time) ([]Conspiracy, error) {
	return s.conspiracies(ctx, `k.next_step_at <= ? AND a.resolved_at IS NULL`, now.Unix())
}

// openPlot fails with ErrEnded unless the plot is still open.
func openPlot(ctx context.Context, tx *sql.Tx, id int64) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM activities WHERE id = ? AND resolved_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrEnded
	}
	return nil
}

// InviteMember asks someone into an open plot. ErrInvited if they were
// already asked.
// A gift (cost > 0) is paid by the plot's leader.
func (s *Store) InviteMember(ctx context.Context, id int64, kind string, memberID int64, status string, cost int, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := openPlot(ctx, tx, id); err != nil {
			return err
		}
		if cost > 0 {
			if err := spendOr(ctx, tx, leaderOf(ctx, tx, id), cost, fmt.Sprintf("you need %d gold for the gift", cost)); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO conspiracy_members (activity_id, member_kind, member_id, status, invited_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT DO NOTHING`, id, kind, memberID, status, now.Unix())
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrInvited
		}
		return nil
	})
}

// SetMemberStatus changes a member's answer, if the plot is still open and
// they are in the from state. ErrHandled if they weren't.
func (s *Store) SetMemberStatus(ctx context.Context, id int64, kind string, memberID int64, from, to string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := openPlot(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE conspiracy_members SET status = ? WHERE activity_id = ? AND member_kind = ? AND member_id = ? AND status = ?`,
			to, id, kind, memberID, from)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrHandled
		}
		return nil
	})
}

// StepConspiracy records a plot's day: its progress, what the target knows,
// and when the next day is. broken ends the plot. out is applied with it.
func (s *Store) StepConspiracy(ctx context.Context, k Conspiracy, progress float64, warned game.Discovery, next time.Time, broken bool, out Outcome, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE conspiracies SET progress = ?, warned = ?, next_step_at = ? WHERE activity_id = ? AND next_step_at = ?`,
			progress, int(warned), next.Unix(), k.ID, k.NextStepAt.Unix())
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrEnded // another tick got there first
		}
		if err := openPlot(ctx, tx, k.ID); err != nil {
			return err
		}
		if broken {
			if err := resolveOnce(ctx, tx, k.ID, now); err != nil {
				return err
			}
		}
		return applyOutcome(ctx, tx, k.GuildID, out, now)
	})
}

// EndConspiracy closes a plot (a strike, betrayal or abandonment) and
// applies out with it. ErrEnded if it was already over.
func (s *Store) EndConspiracy(ctx context.Context, id int64, out Outcome, now time.Time) error {
	return s.ApplyResult(ctx, id, out, now)
}

// StartStrike starts the leader's strike (it takes their action) and keeps
// the plot from falling apart while it runs.
func (s *Store) StartStrike(ctx context.Context, k Conspiracy, ends time.Time, now time.Time) (Activity, error) {
	var a Activity
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := openPlot(ctx, tx, k.ID); err != nil {
			return err
		}
		var striking int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM activities WHERE kind = ? AND target_id = ? AND resolved_at IS NULL`, game.KindStrike, k.ID).Scan(&striking); err != nil {
			return err
		}
		if striking > 0 {
			return ErrHandled
		}
		var err error
		a, err = startActivity(ctx, tx, Activity{
			GuildID: k.GuildID, HostID: k.HostID, Kind: game.KindStrike, Goal: k.Goal, TargetID: k.ID, StartedAt: now, EndsAt: ends,
		}, 0, nil)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE activities SET ends_at = MAX(ends_at, ?) WHERE id = ?`, ends.Add(time.Hour).Unix(), k.ID)
		return err
	})
	return a, err
}

// leaderOf is a plot's leader (0 if it can't be read; spending then fails).
func leaderOf(ctx context.Context, tx *sql.Tx, id int64) int64 {
	var leader int64
	_ = tx.QueryRowContext(ctx, `SELECT host_id FROM activities WHERE id = ?`, id).Scan(&leader)
	return leader
}

// WarnPlot raises what the target knows of an open plot (never lowers it),
// and applies out with it.
func (s *Store) WarnPlot(ctx context.Context, id int64, warned game.Discovery, out Outcome, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := openPlot(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conspiracies SET warned = MAX(warned, ?) WHERE activity_id = ?`, int(warned), id); err != nil {
			return err
		}
		var guildID string
		if err := tx.QueryRowContext(ctx, `SELECT guild_id FROM activities WHERE id = ?`, id).Scan(&guildID); err != nil {
			return err
		}
		return applyOutcome(ctx, tx, guildID, out, now)
	})
}

// ResolveStrike ends a strike and its plot together, and applies what
// happened. ErrEnded if the plot was already over (betrayed, found out).
func (s *Store) ResolveStrike(ctx context.Context, strikeID, plotID int64, out Outcome, now time.Time) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := resolveOnce(ctx, tx, strikeID, now); err != nil {
			return err
		}
		if err := resolveOnce(ctx, tx, plotID, now); err != nil {
			return err
		}
		var guildID string
		if err := tx.QueryRowContext(ctx, `SELECT guild_id FROM activities WHERE id = ?`, plotID).Scan(&guildID); err != nil {
			return err
		}
		return applyOutcome(ctx, tx, guildID, out, now)
	})
}

// ConspiraciesAgainst lists open plots aimed at a player.
func (s *Store) ConspiraciesAgainst(ctx context.Context, charID int64) ([]Conspiracy, error) {
	return s.conspiracies(ctx, `a.kind = ? AND a.target_id = ? AND k.target_kind = ? AND a.resolved_at IS NULL`,
		game.KindConspiracy, charID, string(game.TargetPlayer))
}

// ---- what a plot does ----

// ItemTransfer moves an estate item from one character to another.
type ItemTransfer struct {
	HoldingID int64
	From, To  int64
}

// Death is a character murdered; their heir takes their seat.
type Death struct {
	CharacterID int64
	Heir        game.Heir
}

// transferItems moves possessions (never properties, so nobody is left
// homeless) and cancels deals involving them.
func transferItems(ctx context.Context, tx *sql.Tx, ts []ItemTransfer) error {
	for _, t := range ts {
		var key string
		err := tx.QueryRowContext(ctx, `SELECT item_key FROM estate_items WHERE id = ? AND character_id = ?`, t.HoldingID, t.From).Scan(&key)
		if errors.Is(err, sql.ErrNoRows) {
			continue // gone already: sold, traded or stolen
		} else if err != nil {
			return err
		}
		if it, ok := game.ItemByKey(key); !ok || it.IsProperty() {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE estate_items SET character_id = ? WHERE id = ?`, t.To, t.HoldingID); err != nil {
			return err
		}
		if err := cancelOffersWith(ctx, tx, t.From, []int64{t.HoldingID}, 0); err != nil {
			return err
		}
	}
	return nil
}

// applyDeaths puts each dead character's heir in their seat. The house keeps
// its estate, monuments, ventures and dossier; the dead's own business ends.
func applyDeaths(ctx context.Context, tx *sql.Tx, deaths []Death, now time.Time) error {
	for _, d := range deaths {
		h := d.Heir
		stats, _ := json.Marshal(h.Stats)
		steps := []struct {
			q    string
			args []any
		}{
			{`UPDATE characters SET name = ?, stats = ?, renown = ?, influence = ?, wealth = ?, heir_unnamed = 1 WHERE id = ?`,
				[]any{h.Name, string(stats), h.Renown, h.Influence, h.Wealth, d.CharacterID}},
			{`UPDATE senator_opinions SET value = value * ? WHERE character_id = ?`, []any{game.HeirKeepsOpinion, d.CharacterID}},
			{`UPDATE spy_networks SET value = value * ? WHERE owner_id = ?`, []any{game.HeirKeepsSpies, d.CharacterID}},
			{`DELETE FROM conditions WHERE character_id = ?`, []any{d.CharacterID}},
			{`UPDATE activity_events SET resolved_at = ? WHERE resolved_at IS NULL AND activity_id IN
			    (SELECT id FROM activities WHERE host_id = ? AND resolved_at IS NULL)`, []any{now.Unix(), d.CharacterID}},
			{`UPDATE activities SET resolved_at = ? WHERE host_id = ? AND resolved_at IS NULL`, []any{now.Unix(), d.CharacterID}},
			{`DELETE FROM participants WHERE character_id = ? AND activity_id IN (SELECT id FROM activities WHERE resolved_at IS NULL)`,
				[]any{d.CharacterID}},
			{`UPDATE conspiracy_members SET status = ? WHERE member_kind = ? AND member_id = ? AND status IN (?, ?)`,
				[]any{MemberLeft, MemberPlayer, d.CharacterID, MemberInvited, MemberJoined}},
			{`UPDATE estate_offers SET status = 'cancelled' WHERE status = 'pending' AND (from_id = ? OR to_id = ?)`,
				[]any{d.CharacterID, d.CharacterID}},
		}
		for _, st := range steps {
			if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
				return err
			}
		}
	}
	return nil
}

// replaceSenators seats new senators in the given seats (by ID). Nobody has
// an opinion of a newcomer yet, and the old senator's bribes, visits,
// answers and plots die with them.
func replaceSenators(ctx context.Context, tx *sql.Tx, seats []game.Senator, now time.Time) error {
	for _, n := range seats {
		traits, _ := json.Marshal(n.Traits)
		steps := []struct {
			q    string
			args []any
		}{
			{`UPDATE senators SET name = ?, traits = ?, influence = ?, base_price = ? WHERE id = ?`,
				[]any{n.Name, string(traits), n.Influence, n.BasePrice, n.ID}},
			{`DELETE FROM senator_opinions WHERE senator_id = ?`, []any{n.ID}},
			{`DELETE FROM senator_visits WHERE senator_id = ?`, []any{n.ID}},
			{`DELETE FROM vote_asks WHERE senator_id = ?`, []any{n.ID}},
			{`DELETE FROM bribes WHERE senator_id = ? AND matter_id IN (SELECT id FROM matters WHERE resolved_at IS NULL)`, []any{n.ID}},
			{`UPDATE conspiracy_members SET status = ? WHERE member_kind = ? AND member_id = ? AND status IN (?, ?)`,
				[]any{MemberLeft, MemberSenator, n.ID, MemberInvited, MemberJoined}},
			{`UPDATE activities SET resolved_at = ? WHERE kind = ? AND target_id = ? AND resolved_at IS NULL
			    AND id IN (SELECT activity_id FROM conspiracies WHERE target_kind = ?)`,
				[]any{now.Unix(), game.KindConspiracy, n.ID, string(game.TargetSenator)}},
		}
		for _, st := range steps {
			if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
				return err
			}
		}
	}
	return nil
}

// fileDenunciations puts reports in their owners' dossiers.
func fileDenunciations(ctx context.Context, tx *sql.Tx, ds []Denunciation, now time.Time) error {
	for _, d := range ds {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO denunciations (guild_id, owner_id, actor_id, crime, detail, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, 0)`,
			d.GuildID, d.OwnerID, d.ActorID, d.Crime, d.Detail, now.Unix()); err != nil {
			return err
		}
	}
	return nil
}

// HeirUnnamed reports whether the character's heir is still waiting for a name.
func (s *Store) HeirUnnamed(ctx context.Context, charID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT heir_unnamed FROM characters WHERE id = ?`, charID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return n == 1, err
}

// NameHeir gives a new heir their name, once. ErrHandled if they have one.
func (s *Store) NameHeir(ctx context.Context, charID int64, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE characters SET name = ?, heir_unnamed = 0 WHERE id = ? AND heir_unnamed = 1`, name, charID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrHandled
	}
	return nil
}

// InvitePlayer asks a player into an open plot on terms. ErrInvited if they
// were already asked.
func (s *Store) InvitePlayer(ctx context.Context, id, memberID int64, terms game.PlotTerms, now time.Time) error {
	if err := s.InviteMember(ctx, id, MemberPlayer, memberID, MemberInvited, 0, now); err != nil {
		return err
	}
	if terms == (game.PlotTerms{}) {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conspiracy_terms (activity_id, member_id, gold, share) VALUES (?, ?, ?, ?)
		 ON CONFLICT (activity_id, member_id) DO UPDATE SET gold = excluded.gold, share = excluded.share`,
		id, memberID, terms.Gold, terms.Share)
	return err
}

// ErrLeaderShort is returned when a plot's leader can't pay what they
// promised a joining member.
var ErrLeaderShort = errors.New("the leader can't pay what they promised")

// JoinPlot has an invited player join, and pays them the leader's gold up
// front, together. ErrHandled if they'd already answered; ErrLeaderShort if
// the leader can't pay (the invitation stays open).
func (s *Store) JoinPlot(ctx context.Context, id, memberID int64) (game.PlotTerms, error) {
	var terms game.PlotTerms
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := openPlot(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE conspiracy_members SET status = ? WHERE activity_id = ? AND member_kind = ? AND member_id = ? AND status = ?`,
			MemberJoined, id, MemberPlayer, memberID, MemberInvited)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrHandled
		}
		err = tx.QueryRowContext(ctx, `SELECT gold, share FROM conspiracy_terms WHERE activity_id = ? AND member_id = ?`, id, memberID).
			Scan(&terms.Gold, &terms.Share)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if terms.Gold > 0 {
			if err := spend(ctx, tx, leaderOf(ctx, tx, id), terms.Gold); err != nil {
				return ErrLeaderShort
			}
			return credit(ctx, tx, memberID, terms.Gold)
		}
		return nil
	})
	return terms, err
}

// PlotTerms is what a plot's leader offered each player, by character.
func (s *Store) PlotTerms(ctx context.Context, id int64) (map[int64]game.PlotTerms, error) {
	type row struct {
		id int64
		t  game.PlotTerms
	}
	rows, err := queryAll(ctx, s.db, `SELECT member_id, gold, share FROM conspiracy_terms WHERE activity_id = ?`,
		func(r scanner) (row, error) {
			var x row
			err := r.Scan(&x.id, &x.t.Gold, &x.t.Share)
			return x, err
		}, id)
	out := make(map[int64]game.PlotTerms, len(rows))
	for _, x := range rows {
		out[x.id] = x.t
	}
	return out, err
}
