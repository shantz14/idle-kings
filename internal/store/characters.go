package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

var ErrFamilyTaken = errors.New("that family already sits at this court")

type Character struct {
	ID          int64
	GuildID     string
	UserID      string
	Name        string
	FamilyName  string
	Role        string
	Background  string
	Crest       game.Crest
	Stats       game.Stats // base stats, without item bonuses
	Influence   float64
	Renown      float64
	Wealth      int
	DailyIncome int      // base income; see TotalIncome
	Items       []string // estate item keys, home first
}

func (c Character) FullName() string { return c.Name + " " + c.FamilyName }

// House is how the family is shown: "🟥🦁 House Vane".
func (c Character) House() string { return c.Crest.Emoji() + " House " + c.FamilyName }

func (c Character) Effects() game.Effects { return game.SumEffects(c.Items) }

// Effective is the stats the rules use: base plus estate item bonuses.
func (c Character) Effective() game.Stats {
	s := c.Stats.Clone()
	s.Add(c.Effects().Stats)
	return s
}

func (c Character) EstateValue() float64 { return game.EstateValue(c.Items) }

// VoteWeight is how much this character's vote counts in the Senate.
func (c Character) VoteWeight() float64 { return c.Influence + c.Effects().VoteWeight }

// TotalIncome is gold per day: base income plus estate items.
func (c Character) TotalIncome() int { return c.DailyIncome + c.Effects().DailyIncome }

func (c Character) Sheet() game.Sheet {
	return game.NewSheet(c.Effective(), c.Influence, c.Renown, c.EstateValue(), c.Wealth)
}

func (c Character) Person() game.Person {
	return game.Person{ID: c.ID, UserID: c.UserID, Name: c.FullName(), Sheet: c.Sheet(), Effects: c.Effects()}
}

// FamilyTaken reports whether a family name is already used at a court.
func (s *Store) FamilyTaken(ctx context.Context, guildID, family string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM characters WHERE guild_id = ? AND lower(family_name) = lower(?)`,
		guildID, strings.TrimSpace(family)).Scan(&n)
	return n > 0, err
}

// CreateCharacter turns a finished build into a character with its estate.
func (s *Store) CreateCharacter(ctx context.Context, guildID, userID string, b game.Build) (Character, error) {
	if err := b.Validate(); err != nil {
		return Character{}, err
	}
	start := b.Final()
	c := Character{
		GuildID: guildID, UserID: userID,
		Name: strings.TrimSpace(b.Name), FamilyName: strings.TrimSpace(b.Family),
		Role: b.Role, Background: b.Background, Crest: b.Crest, Stats: start.Stats,
		Influence: start.Influence, Renown: start.Renown, Wealth: start.Wealth, DailyIncome: start.DailyIncome,
		Items: start.Items,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	var exists, taken int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM characters WHERE guild_id = ? AND user_id = ?`,
		guildID, userID).Scan(&exists); err != nil {
		return c, err
	}
	if exists > 0 {
		return c, ErrExists
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM characters WHERE guild_id = ? AND lower(family_name) = lower(?)`,
		guildID, c.FamilyName).Scan(&taken); err != nil {
		return c, err
	}
	if taken > 0 {
		return c, ErrFamilyTaken
	}

	stats, _ := json.Marshal(c.Stats)
	crest, _ := json.Marshal(c.Crest)
	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO characters (guild_id, user_id, name, family_name, role, background, crest, stats,
		                         influence, renown, wealth, daily_income, last_income_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		guildID, userID, c.Name, c.FamilyName, c.Role, c.Background, string(crest), string(stats),
		c.Influence, c.Renown, c.Wealth, c.DailyIncome, now, now)
	if err != nil {
		return c, err
	}
	c.ID, _ = res.LastInsertId()
	for _, k := range c.Items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO estate_items (character_id, item_key, acquired_at) VALUES (?, ?, ?)`,
			c.ID, k, now); err != nil {
			return c, err
		}
	}
	return c, tx.Commit()
}

const charCols = `c.id, c.guild_id, c.user_id, c.name, c.family_name, c.role, c.background, c.crest, c.stats,
	c.influence, c.renown, c.wealth, c.daily_income`

type scanner interface{ Scan(...any) error }

// scanCharacter reads charCols, then any extra columns into extra.
// Items are not loaded; see fillItems.
func scanCharacter(row scanner, extra ...any) (Character, error) {
	var c Character
	var crest, stats string
	dest := append([]any{&c.ID, &c.GuildID, &c.UserID, &c.Name, &c.FamilyName, &c.Role, &c.Background, &crest, &stats,
		&c.Influence, &c.Renown, &c.Wealth, &c.DailyIncome}, extra...)
	if err := row.Scan(dest...); errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	} else if err != nil {
		return c, err
	}
	c.Stats = game.Stats{}
	if err := json.Unmarshal([]byte(stats), &c.Stats); err != nil {
		return c, err
	}
	return c, json.Unmarshal([]byte(crest), &c.Crest)
}

// fillItems loads estate items for the given characters. Call it only after
// any open rows are closed: the pool has a single connection.
func (s *Store) fillItems(ctx context.Context, chars []*Character) error {
	if len(chars) == 0 {
		return nil
	}
	byID := map[int64]*Character{}
	args := make([]any, 0, len(chars))
	for _, c := range chars {
		byID[c.ID] = c
		c.Items = nil
		args = append(args, c.ID)
	}
	q := `SELECT character_id, item_key FROM estate_items WHERE character_id IN (?` +
		strings.Repeat(",?", len(args)-1) + `) ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, args...)
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
		byID[id].Items = append(byID[id].Items, key)
	}
	return rows.Err()
}

func (s *Store) oneCharacter(ctx context.Context, where string, args ...any) (Character, error) {
	c, err := scanCharacter(s.db.QueryRowContext(ctx, `SELECT `+charCols+` FROM characters c WHERE `+where, args...))
	if err != nil {
		return c, err
	}
	return c, s.fillItems(ctx, []*Character{&c})
}

func (s *Store) listCharacters(ctx context.Context, where string, args ...any) ([]Character, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+charCols+` FROM characters c WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	var out []Character
	for rows.Next() {
		c, err := scanCharacter(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	ptrs := make([]*Character, len(out))
	for i := range out {
		ptrs[i] = &out[i]
	}
	return out, s.fillItems(ctx, ptrs)
}

func (s *Store) GetCharacter(ctx context.Context, guildID, userID string) (Character, error) {
	return s.oneCharacter(ctx, `c.guild_id = ? AND c.user_id = ?`, guildID, userID)
}

func (s *Store) GetCharacterByID(ctx context.Context, id int64) (Character, error) {
	return s.oneCharacter(ctx, `c.id = ?`, id)
}

// CharactersForUser lists a user's characters across all courts (for DMs).
func (s *Store) CharactersForUser(ctx context.Context, userID string) ([]Character, error) {
	return s.listCharacters(ctx, `c.user_id = ?`, userID)
}

// CharactersInCourt lists every character in a court.
func (s *Store) CharactersInCourt(ctx context.Context, guildID string) ([]Character, error) {
	return s.listCharacters(ctx, `c.guild_id = ? ORDER BY c.id`, guildID)
}

// PayIncome credits every character one day's income (base plus estate)
// per full real day since they were last paid.
func (s *Store) PayIncome(ctx context.Context, now time.Time) error {
	const day = 86400
	due, err := s.listCharacters(ctx, `? - c.last_income_at >= ?`, now.Unix(), day)
	if err != nil || len(due) == 0 {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range due {
		var last int64
		if err := tx.QueryRowContext(ctx, `SELECT last_income_at FROM characters WHERE id = ?`, c.ID).Scan(&last); err != nil {
			return err
		}
		days := (now.Unix() - last) / day
		if days <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE characters SET wealth = wealth + ?, last_income_at = last_income_at + ? WHERE id = ?`,
			int64(c.TotalIncome())*days, days*day, c.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// applyDelta changes one character inside a transaction.
func applyDelta(ctx context.Context, tx *sql.Tx, charID int64, d game.Delta) error {
	var stats string
	if err := tx.QueryRowContext(ctx, `SELECT stats FROM characters WHERE id = ?`, charID).Scan(&stats); err != nil {
		return err
	}
	st := game.Stats{}
	if err := json.Unmarshal([]byte(stats), &st); err != nil {
		return err
	}
	st.Add(d.Stats)
	b, _ := json.Marshal(st)
	_, err := tx.ExecContext(ctx,
		`UPDATE characters SET stats = ?, influence = MAX(0, influence + ?), renown = MAX(0, renown + ?),
		        wealth = MAX(0, wealth + ?), daily_income = MAX(0, daily_income + ?)
		 WHERE id = ?`,
		string(b), d.Influence, d.Renown, d.Wealth, d.DailyIncome, charID)
	return err
}

// ---- character creator drafts ----

// GetDraft loads a saved draft into v. ok is false if there is none.
func (s *Store) GetDraft(ctx context.Context, guildID, userID string, v any) (ok bool, err error) {
	var data string
	err = s.db.QueryRowContext(ctx, `SELECT data FROM character_drafts WHERE guild_id = ? AND user_id = ?`,
		guildID, userID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(data), v)
}

func (s *Store) SaveDraft(ctx context.Context, guildID, userID string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO character_drafts (guild_id, user_id, data, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (guild_id, user_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		guildID, userID, string(b), time.Now().Unix())
	return err
}

func (s *Store) DeleteDraft(ctx context.Context, guildID, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM character_drafts WHERE guild_id = ? AND user_id = ?`, guildID, userID)
	return err
}
