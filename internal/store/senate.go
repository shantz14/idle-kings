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
	ErrBought   = errors.New("that senator has already been paid by someone else")
	ErrVoteSold = errors.New("that vote has already been sold")
	ErrHandled  = errors.New("that offer has already been answered")
)

type RealmState struct {
	GuildID      string
	Trackers     game.Realm
	Sectors      game.SectorState
	Laws         game.Laws // in force, oldest first
	Status       string    // alive | fallen
	FallenReason string
	NextMatterAt time.Time
}

func (r RealmState) Alive() bool { return r.Status == realmAlive }

type Matter struct {
	ID        int64
	GuildID   string
	EventKey  string
	OpenedAt  time.Time
	ClosesAt  time.Time
	Resolved  bool
	ChannelID string
	MessageID string
	// PetitionerID is the character who petitioned for the matter, 0 if none.
	PetitionerID int64
}

type Bribe struct {
	SenatorID   int64
	CharacterID int64
	Option      string
	Amount      int
}

// RunningCourts lists courts whose game has started.
func (s *Store) RunningCourts(ctx context.Context) ([]Court, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT guild_id, admin_user_id, COALESCE(announcements_channel_id, ''), status FROM courts WHERE status = ?`, courtRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Court
	for rows.Next() {
		var c Court
		if err := rows.Scan(&c.GuildID, &c.AdminUserID, &c.AnnouncementsChannelID, &c.Status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateRealm sets up a court's realm and senate if it doesn't have one.
func (s *Store) CreateRealm(ctx context.Context, guildID string, realm game.Realm, senators []game.Senator, firstMatter time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tr, _ := json.Marshal(realm)
	sec, _ := json.Marshal(game.StartingSectors())
	res, err := tx.ExecContext(ctx,
		`INSERT INTO realms (guild_id, trackers, sectors, next_matter_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		guildID, string(tr), string(sec), firstMatter.Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // already exists
	}
	for _, sn := range senators {
		traits, _ := json.Marshal(sn.Traits)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO senators (guild_id, name, traits, influence, base_price) VALUES (?, ?, ?, ?, ?)`,
			guildID, sn.Name, string(traits), sn.Influence, sn.BasePrice); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetRealm(ctx context.Context, guildID string) (RealmState, error) {
	var r RealmState
	var tr, sec, laws string
	var reason sql.NullString
	var next int64
	err := s.db.QueryRowContext(ctx,
		`SELECT guild_id, trackers, sectors, laws, status, fallen_reason, next_matter_at FROM realms WHERE guild_id = ?`, guildID).
		Scan(&r.GuildID, &tr, &sec, &laws, &r.Status, &reason, &next)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	} else if err != nil {
		return r, err
	}
	r.FallenReason, r.NextMatterAt = reason.String, time.Unix(next, 0)
	r.Trackers = game.Realm{}
	if err := json.Unmarshal([]byte(tr), &r.Trackers); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(sec), &r.Sectors); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(laws), &r.Laws); err != nil {
		return r, err
	}
	r.Sectors = r.Sectors.Fill()
	return r, nil
}

func (s *Store) Senators(ctx context.Context, guildID string) ([]game.Senator, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, traits, influence, base_price FROM senators WHERE guild_id = ? ORDER BY influence DESC, id`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []game.Senator
	for rows.Next() {
		var sn game.Senator
		var traits string
		if err := rows.Scan(&sn.ID, &sn.Name, &traits, &sn.Influence, &sn.BasePrice); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(traits), &sn.Traits); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// ---- matters ----

const matterCols = `id, guild_id, event_key, opened_at, closes_at, resolved_at, channel_id, message_id, petitioner_id`

func scanMatter(row scanner) (Matter, error) {
	var m Matter
	var opened, closes int64
	var resolved, pet sql.NullInt64
	var ch, msg sql.NullString
	err := row.Scan(&m.ID, &m.GuildID, &m.EventKey, &opened, &closes, &resolved, &ch, &msg, &pet)
	m.PetitionerID = pet.Int64
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	m.OpenedAt, m.ClosesAt, m.Resolved = time.Unix(opened, 0), time.Unix(closes, 0), resolved.Valid
	m.ChannelID, m.MessageID = ch.String, msg.String
	return m, err
}

func (s *Store) GetMatter(ctx context.Context, id int64) (Matter, error) {
	return scanMatter(s.db.QueryRowContext(ctx, `SELECT `+matterCols+` FROM matters WHERE id = ?`, id))
}

// OpenMatter returns the court's matter currently being voted on.
func (s *Store) OpenMatter(ctx context.Context, guildID string) (Matter, error) {
	return scanMatter(s.db.QueryRowContext(ctx,
		`SELECT `+matterCols+` FROM matters WHERE guild_id = ? AND resolved_at IS NULL ORDER BY id DESC LIMIT 1`, guildID))
}

// OpenMatters lists every matter being voted on, oldest first: the day's
// matter and any petition alongside it.
func (s *Store) OpenMatters(ctx context.Context, guildID string) ([]Matter, error) {
	return s.matters(ctx, `SELECT `+matterCols+` FROM matters WHERE guild_id = ? AND resolved_at IS NULL ORDER BY id`, guildID)
}

func (s *Store) matters(ctx context.Context, query string, args ...any) ([]Matter, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Matter
	for rows.Next() {
		m, err := scanMatter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ClosedMatters lists matters whose voting has ended but aren't resolved.
func (s *Store) ClosedMatters(ctx context.Context, now time.Time) ([]Matter, error) {
	return s.matters(ctx, `SELECT `+matterCols+` FROM matters WHERE resolved_at IS NULL AND closes_at <= ?`, now.Unix())
}

// NextQueued returns the earliest follow-up event that is due. Petitions
// aren't follow-ups: they open on their own (NextPetition).
func (s *Store) NextQueued(ctx context.Context, guildID string, now time.Time) (id int64, key string, err error) {
	return s.nextQueued(ctx, guildID, now, `petitioner_id IS NULL`)
}

// NextPetition returns the earliest petitioned matter waiting for the floor.
func (s *Store) NextPetition(ctx context.Context, guildID string, now time.Time) (id int64, key string, err error) {
	return s.nextQueued(ctx, guildID, now, `petitioner_id IS NOT NULL`)
}

func (s *Store) nextQueued(ctx context.Context, guildID string, now time.Time, cond string) (id int64, key string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT id, event_key FROM realm_queue WHERE guild_id = ? AND due_at <= ? AND `+cond+` ORDER BY due_at, id LIMIT 1`,
		guildID, now.Unix()).Scan(&id, &key)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	return id, key, err
}

// RecentEventKeys lists the court's last n matters' event keys, oldest first.
func (s *Store) RecentEventKeys(ctx context.Context, guildID string, n int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_key FROM matters WHERE guild_id = ? ORDER BY opened_at DESC, id DESC LIMIT ?`, guildID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// DeferQueued pushes a queued follow-up back to a later time.
func (s *Store) DeferQueued(ctx context.Context, id int64, due time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE realm_queue SET due_at = ? WHERE id = ?`, due.Unix(), id)
	return err
}

// CreateMatter opens a matter, schedules the next one, and consumes the
// queued follow-up it came from (queueID 0 = none).
func (s *Store) CreateMatter(ctx context.Context, guildID, eventKey string, queueID int64, opened, closes, next time.Time) (Matter, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Matter{}, err
	}
	defer tx.Rollback()
	var pet sql.NullInt64
	if queueID != 0 {
		if err := tx.QueryRowContext(ctx, `SELECT petitioner_id FROM realm_queue WHERE id = ?`, queueID).Scan(&pet); err != nil &&
			!errors.Is(err, sql.ErrNoRows) {
			return Matter{}, err
		}
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO matters (guild_id, event_key, opened_at, closes_at, petitioner_id) VALUES (?, ?, ?, ?, ?)`,
		guildID, eventKey, opened.Unix(), closes.Unix(), pet)
	if err != nil {
		return Matter{}, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `UPDATE realms SET next_matter_at = ? WHERE guild_id = ?`,
		next.Unix(), guildID); err != nil {
		return Matter{}, err
	}
	if queueID != 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM realm_queue WHERE id = ?`, queueID); err != nil {
			return Matter{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Matter{}, err
	}
	return Matter{ID: id, GuildID: guildID, EventKey: eventKey, OpenedAt: opened, ClosesAt: closes, PetitionerID: pet.Int64}, nil
}

// QueuePetition puts a petitioned matter on the agenda: it is due now, and
// opens alongside the day's matter (NextPetition).
func (s *Store) QueuePetition(ctx context.Context, guildID, eventKey string, petitionerID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO realm_queue (guild_id, event_key, due_at, petitioner_id) VALUES (?, ?, ?, ?)`,
		guildID, eventKey, now.Unix(), petitionerID)
	return err
}

func (s *Store) SetMatterMessage(ctx context.Context, id int64, channelID, messageID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE matters SET channel_id = ?, message_id = ? WHERE id = ?`, channelID, messageID, id)
	return err
}

func (s *Store) openMatterTx(ctx context.Context, tx *sql.Tx, matterID int64, now time.Time) error {
	var closes int64
	var resolved sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT closes_at, resolved_at FROM matters WHERE id = ?`, matterID).Scan(&closes, &resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if resolved.Valid || now.Unix() >= closes {
		return ErrEnded
	}
	return nil
}

// CastVote records or changes a player's vote.
func (s *Store) CastVote(ctx context.Context, matterID, charID int64, option string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.openMatterTx(ctx, tx, matterID, now); err != nil {
		return err
	}
	if err := setVote(ctx, tx, matterID, charID, option, "", now); err != nil {
		return err
	}
	return tx.Commit()
}

// setVote records a vote unless it has been sold. soldTo != "" sells it,
// which locks the vote, except under the Secret Ballot, where nobody can
// check it and the seller may still vote as they like.
func setVote(ctx context.Context, tx *sql.Tx, matterID, charID int64, option, soldTo string, now time.Time) error {
	locked, sold := 0, any(nil)
	if soldTo != "" {
		locked, sold = 1, soldTo
		secret, err := secretBallotTx(ctx, tx, matterID)
		if err != nil {
			return err
		}
		if secret {
			locked = 0
		}
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO votes (matter_id, character_id, option, voted_at, locked, sold_to) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (matter_id, character_id) DO UPDATE
		   SET option = excluded.option, voted_at = excluded.voted_at, locked = excluded.locked, sold_to = excluded.sold_to
		   WHERE votes.locked = 0`,
		matterID, charID, option, now.Unix(), locked, sold)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrVoteSold
	}
	return nil
}

// secretBallotTx reports whether the Secret Ballot stands in a matter's court.
func secretBallotTx(ctx context.Context, tx *sql.Tx, matterID int64) (bool, error) {
	var raw string
	err := tx.QueryRowContext(ctx,
		`SELECT r.laws FROM realms r JOIN matters m ON m.guild_id = r.guild_id WHERE m.id = ?`, matterID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var ls game.Laws
	if err := json.Unmarshal([]byte(raw), &ls); err != nil {
		return false, err
	}
	return ls.Has(game.LawSecretBallot), nil
}

// SoldVote reports whether a character's vote on a matter is sold, and to whom.
func (s *Store) SoldVote(ctx context.Context, matterID, charID int64) (option, soldTo string, sold bool) {
	var to sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT option, sold_to FROM votes WHERE matter_id = ? AND character_id = ? AND locked = 1`, matterID, charID).
		Scan(&option, &to)
	return option, to.String, err == nil
}

// RecentSenatorSales counts the votes a character has sold to senators
// since a time, for the habit that makes a scandal likelier.
func (s *Store) RecentSenatorSales(ctx context.Context, charID int64, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM votes WHERE character_id = ? AND sold_to LIKE 'senator:%' AND voted_at >= ?`,
		charID, since.Unix()).Scan(&n)
	return n, err
}

// SellVoteToSenator locks the vote, pays the player, and warms the senator
// to them by opinion.
func (s *Store) SellVoteToSenator(ctx context.Context, matterID, charID int64, option string, senator game.Senator, gold int, opinion float64, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.openMatterTx(ctx, tx, matterID, now); err != nil {
		return err
	}
	if err := setVote(ctx, tx, matterID, charID, option, "senator:"+senator.Name, now); err != nil {
		return err
	}
	if err := credit(ctx, tx, charID, gold); err != nil {
		return err
	}
	if err := changeOpinions(ctx, tx, []game.OpinionChange{{SenatorID: senator.ID, CharacterID: charID, Delta: opinion}}); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- player-to-player vote deals ----

const (
	OfferSell = "sell" // From is selling their vote to To
	OfferBuy  = "buy"  // From wants to buy To's vote for Option
)

type VoteOffer struct {
	ID       int64
	MatterID int64
	Kind     string
	FromID   int64
	ToID     int64
	Option   string
	Price    int
	Status   string
}

// Seller and Buyer are the character IDs on each side of the deal.
func (o VoteOffer) Seller() int64 {
	if o.Kind == OfferSell {
		return o.FromID
	}
	return o.ToID
}

func (o VoteOffer) Buyer() int64 {
	if o.Kind == OfferSell {
		return o.ToID
	}
	return o.FromID
}

func (s *Store) CreateOffer(ctx context.Context, o VoteOffer, now time.Time) (VoteOffer, error) {
	var opt any
	if o.Option != "" {
		opt = o.Option
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO vote_offers (matter_id, kind, from_id, to_id, option, price, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		o.MatterID, o.Kind, o.FromID, o.ToID, opt, o.Price, now.Unix())
	if err != nil {
		return o, err
	}
	o.ID, _ = res.LastInsertId()
	o.Status = "pending"
	return o, nil
}

func (s *Store) GetOffer(ctx context.Context, id int64) (VoteOffer, error) {
	var o VoteOffer
	var opt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, matter_id, kind, from_id, to_id, option, price, status FROM vote_offers WHERE id = ?`, id).
		Scan(&o.ID, &o.MatterID, &o.Kind, &o.FromID, &o.ToID, &opt, &o.Price, &o.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	o.Option = opt.String
	return o, err
}

// AcceptOffer completes a deal: the buyer pays, the seller's vote is locked
// to the option. For a sell offer the buyer chooses the option now.
func (s *Store) AcceptOffer(ctx context.Context, offerID int64, option string, now time.Time) (VoteOffer, error) {
	o, err := s.GetOffer(ctx, offerID)
	if err != nil {
		return o, err
	}
	if o.Kind == OfferBuy {
		option = o.Option
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return o, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE vote_offers SET status = 'accepted', option = ? WHERE id = ? AND status = 'pending'`, option, o.ID)
	if err != nil {
		return o, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return o, ErrHandled
	}
	if err := s.openMatterTx(ctx, tx, o.MatterID, now); err != nil {
		return o, err
	}
	if err := spendOr(ctx, tx, o.Buyer(), o.Price, fmt.Sprintf("the buyer can't afford %d gold any more", o.Price)); err != nil {
		return o, err
	}
	if err := credit(ctx, tx, o.Seller(), o.Price); err != nil {
		return o, err
	}
	if err := setVote(ctx, tx, o.MatterID, o.Seller(), option, fmt.Sprintf("player:%d", o.Buyer()), now); err != nil {
		return o, err
	}
	o.Option, o.Status = option, "accepted"
	return o, tx.Commit()
}

func (s *Store) DeclineOffer(ctx context.Context, offerID int64) (VoteOffer, error) {
	o, err := s.GetOffer(ctx, offerID)
	if err != nil {
		return o, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE vote_offers SET status = 'declined' WHERE id = ? AND status = 'pending'`, offerID)
	if err != nil {
		return o, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return o, ErrHandled
	}
	o.Status = "declined"
	return o, nil
}

// Votes maps character ID to option.
func (s *Store) Votes(ctx context.Context, matterID int64) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT character_id, option FROM votes WHERE matter_id = ?`, matterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var opt string
		if err := rows.Scan(&id, &opt); err != nil {
			return nil, err
		}
		out[id] = opt
	}
	return out, rows.Err()
}

// Bribes maps senator ID to the bribe paid for this matter.
func (s *Store) Bribes(ctx context.Context, matterID int64) (map[int64]Bribe, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT senator_id, character_id, option, amount FROM bribes WHERE matter_id = ?`, matterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Bribe{}
	for rows.Next() {
		var b Bribe
		if err := rows.Scan(&b.SenatorID, &b.CharacterID, &b.Option, &b.Amount); err != nil {
			return nil, err
		}
		out[b.SenatorID] = b
	}
	return out, rows.Err()
}

// PayBribe buys a senator's vote if nobody else has and the briber can pay.
// opinion is how much the senator warms to the briber.
func (s *Store) PayBribe(ctx context.Context, matterID, senatorID, charID int64, option string, price int, opinion float64, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.openMatterTx(ctx, tx, matterID, now); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO bribes (matter_id, senator_id, character_id, option, amount, paid_at) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT DO NOTHING`, matterID, senatorID, charID, option, price, now.Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrBought
	}
	if err := spendOr(ctx, tx, charID, price, fmt.Sprintf("you need %d gold for that", price)); err != nil {
		return err
	}
	if err := changeOpinions(ctx, tx, []game.OpinionChange{{SenatorID: senatorID, CharacterID: charID, Delta: opinion}}); err != nil {
		return err
	}
	return tx.Commit()
}

// MatterResolution is everything that changes when a matter is decided.
type MatterResolution struct {
	Summary any // stored as JSON for the record
	Realm   game.Realm
	// Sectors replaces the realm's sectors (nil = unchanged). It must be
	// built from a fresh GetRealm: investments resolve in the same tick,
	// before matters, so nothing else writes sectors in between.
	Sectors      game.SectorState
	Laws         game.Laws // replaces the laws in force
	Fallen       bool
	FallenReason string
	Deltas       map[int64]game.Delta
	FollowUps    []game.QueuedEvent
	DayLength    time.Duration // how long a follow-up "day" is
	Opinions     []game.OpinionChange
	OpinionDecay float64 // share of every opinion kept (applied before Opinions); 0 = no decay
}

// ResolveMatter applies a decision once: realm, players, follow-ups.
func (s *Store) ResolveMatter(ctx context.Context, m Matter, r MatterResolution, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	summary, _ := json.Marshal(r.Summary)
	res, err := tx.ExecContext(ctx, `UPDATE matters SET resolved_at = ?, result = ? WHERE id = ? AND resolved_at IS NULL`,
		now.Unix(), string(summary), m.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrEnded
	}
	tr, _ := json.Marshal(r.Realm)
	status, reason := realmAlive, any(nil)
	if r.Fallen {
		status, reason = realmFallen, r.FallenReason
	}
	laws, _ := json.Marshal(r.Laws)
	if r.Laws == nil {
		laws = []byte("[]")
	}
	var sectors any // NULL keeps the sectors as they are
	if r.Sectors != nil {
		sec, _ := json.Marshal(r.Sectors)
		sectors = string(sec)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE realms SET trackers = ?, status = ?, fallen_reason = ?, laws = ?, sectors = COALESCE(?, sectors) WHERE guild_id = ?`,
		string(tr), status, reason, string(laws), sectors, m.GuildID); err != nil {
		return err
	}
	if r.Fallen {
		if _, err := tx.ExecContext(ctx, `UPDATE courts SET status = ? WHERE guild_id = ?`, courtFallen, m.GuildID); err != nil {
			return err
		}
	}
	for id, d := range r.Deltas {
		if err := applyDelta(ctx, tx, id, d); err != nil {
			return err
		}
	}
	if r.OpinionDecay > 0 {
		if err := decayOpinions(ctx, tx, m.GuildID, r.OpinionDecay); err != nil {
			return err
		}
		if err := decaySpies(ctx, tx, m.GuildID, game.SpyDecay); err != nil {
			return err
		}
	}
	if err := changeOpinions(ctx, tx, r.Opinions); err != nil {
		return err
	}
	for _, f := range r.FollowUps {
		// Due slightly before that day's matter opens, so it's picked up then.
		due := m.OpenedAt.Add(time.Duration(f.Days)*r.DayLength - time.Minute)
		if _, err := tx.ExecContext(ctx, `INSERT INTO realm_queue (guild_id, event_key, due_at) VALUES (?, ?, ?)`,
			m.GuildID, f.Event, due.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Decision is a resolved matter, for history pages.
type Decision struct {
	EventKey   string
	Winner     string
	Succeeded  bool
	ResolvedAt time.Time
}

// RecentDecisions lists a court's most recently decided matters, newest first.
func (s *Store) RecentDecisions(ctx context.Context, guildID string, n int) ([]Decision, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_key, COALESCE(result, '{}'), resolved_at FROM matters
		 WHERE guild_id = ? AND resolved_at IS NOT NULL ORDER BY resolved_at DESC, id DESC LIMIT ?`, guildID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var d Decision
		var result string
		var at int64
		if err := rows.Scan(&d.EventKey, &result, &at); err != nil {
			return nil, err
		}
		var r struct {
			Winner    string `json:"winner"`
			Succeeded bool   `json:"succeeded"`
		}
		_ = json.Unmarshal([]byte(result), &r)
		d.Winner, d.Succeeded, d.ResolvedAt = r.Winner, r.Succeeded, time.Unix(at, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// QueuedEvents lists the event keys waiting in a court's follow-up queue,
// soonest first.
func (s *Store) QueuedEvents(ctx context.Context, guildID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_key FROM realm_queue WHERE guild_id = ? ORDER BY due_at, id`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
