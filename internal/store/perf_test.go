package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestBatchLookups(t *testing.T) {
	s, ctx := estateStore(t)
	a, b := newChar(t, s, "a", "merchant"), newChar(t, s, "b", "merchant")

	byID, err := s.CharactersByIDs(ctx, []int64{a.ID, b.ID, 999})
	if err != nil || len(byID) != 2 || byID[b.ID].Name != "b" || len(byID[a.ID].Items) == 0 {
		t.Fatalf("CharactersByIDs = %v, %v", byID, err)
	}
	names, err := s.CharacterNames(ctx, []int64{a.ID})
	if err != nil || names[a.ID].FullName != "a a" || names[a.ID].UserID != "a" {
		t.Fatalf("CharacterNames = %v, %v", names, err)
	}
	if n, err := s.CountCharacters(ctx, "g"); err != nil || n != 2 {
		t.Fatalf("CountCharacters = %d, %v", n, err)
	}
	if got, err := s.CharactersByIDs(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("empty CharactersByIDs = %v, %v", got, err)
	}
}

func TestWithTxRollsBack(t *testing.T) {
	s, ctx := estateStore(t)
	c := newChar(t, s, "a", "merchant")
	boom := errors.New("boom")
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := credit(ctx, tx, c.ID, 500); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || reload(t, s, c).Wealth != c.Wealth {
		t.Fatalf("withTx err %v, wealth %d (was %d)", err, reload(t, s, c).Wealth, c.Wealth)
	}
	if err := s.withTx(ctx, func(tx *sql.Tx) error { return spendOr(ctx, tx, c.ID, c.Wealth+1, "too poor") }); err == nil || err.Error() != "too poor" {
		t.Fatalf("spendOr = %v", err)
	}
}

// TestHotQueriesUseIndexes guards the queries run every tick or on most
// interactions against full table scans.
func TestHotQueriesUseIndexes(t *testing.T) {
	s, ctx := estateStore(t)
	for name, q := range map[string]string{
		"pay income":     `SELECT id FROM characters c WHERE c.last_income_at <= 5`,
		"open events":    `SELECT id FROM activity_events e WHERE e.activity_id = 1 AND e.resolved_at IS NULL`,
		"expired events": `SELECT id FROM activity_events e WHERE e.resolved_at IS NULL AND e.deadline_at <= 5`,
		"opinions":       `SELECT senator_id FROM senator_opinions WHERE character_id = 1`,
		"hosted":         `SELECT id FROM activities WHERE host_id = 1 AND resolved_at IS NULL`,
		"queue":          `SELECT id FROM realm_queue WHERE guild_id = 'g' AND due_at <= 5`,
		"senators":       `SELECT id FROM senators WHERE guild_id = 'g'`,
		"by user":        `SELECT id FROM characters WHERE user_id = 'a'`,
		"matters held":   `SELECT COUNT(*) FROM matters WHERE guild_id = 'g'`,
		"plot days":      `SELECT activity_id FROM conspiracies k WHERE k.next_step_at <= 5`,
		"plots of":       `SELECT activity_id FROM conspiracy_members WHERE member_kind = 'player' AND member_id = 1`,
		"happenings due": `SELECT id FROM happenings WHERE ended_at IS NULL AND ends_at <= 5`,
		"happenings":     `SELECT id FROM happenings WHERE guild_id = 'g' AND ended_at IS NULL`,
	} {
		rows, err := s.db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+q)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		if p := strings.Join(plan, "; "); !strings.Contains(p, "INDEX") {
			t.Errorf("%s: no index used: %s", name, p)
		}
	}
}
