package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestSeasonsDeedsAndTitles(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.Now()
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), nil, now); err != nil {
		t.Fatal(err)
	}
	a := newChar(t, s, "a", "merchant")
	b := newChar(t, s, "b", "officer")

	if se, err := s.Season(ctx, "g"); err != nil || se.Number != 1 || se.SenateDays != 0 || se.DaysLeft() != game.SeasonDays {
		t.Fatalf("fresh season %+v %v", se, err)
	}

	// A Senate day counts toward the season and fades influence; a petition doesn't.
	st, _ := s.GetRealm(ctx, "g")
	resolve := func(senateDay bool) {
		t.Helper()
		m, err := s.CreateMatter(ctx, "g", "grand_games", 0, now, now, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ResolveMatter(ctx, m, MatterResolution{Realm: st.Trackers, SenateDay: senateDay,
			Deltas: map[int64]game.Delta{a.ID: {Influence: 1}}}, now); err != nil {
			t.Fatal(err)
		}
	}
	resolve(true)
	resolve(false)
	se, _ := s.Season(ctx, "g")
	got, _ := s.GetCharacterByID(ctx, a.ID)
	if se.SenateDays != 1 || math.Abs(got.Influence-(a.Influence*game.InfluenceDecay+2)) > 1e-9 {
		t.Fatalf("after a Senate day and a petition: %+v, influence %v (was %v)", se, got.Influence, a.Influence)
	}

	// Deeds add up per season and kind.
	if err := s.AddDeeds(ctx, "g", []Deed{{a.ID, game.DeedDuelWon, 1}, {a.ID, game.DeedDuelWon, 1}, {b.ID, game.DeedDuelWon, 1}, {b.ID, game.DeedCrisisAid, 50}}, now); err != nil {
		t.Fatal(err)
	}
	tot, err := s.SeasonTotals(ctx, "g", 1, game.DeedDuelWon)
	if err != nil || tot[a.ID] != 2 || tot[b.ID] != 1 {
		t.Fatalf("totals %v %v", tot, err)
	}

	// Ending a season gives its titles once and starts the next.
	if err := s.EndSeason(ctx, "g", 1, []TitleAward{{a.ID, game.TitleChampion}}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.EndSeason(ctx, "g", 1, nil, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("second end: %v", err)
	}
	if se, _ := s.Season(ctx, "g"); se.Number != 2 || se.SenateDays != 0 {
		t.Fatalf("next season %+v", se)
	}
	if tot, _ := s.SeasonTotals(ctx, "g", 2, game.DeedDuelWon); len(tot) != 0 {
		t.Fatalf("new season should start clean: %v", tot)
	}

	// Feats are earned once.
	if fresh, err := s.AwardFeat(ctx, "g", b.ID, game.FeatPatron, now); err != nil || !fresh {
		t.Fatalf("feat: %v %v", fresh, err)
	}
	if fresh, _ := s.AwardFeat(ctx, "g", b.ID, game.FeatPatron, now); fresh {
		t.Fatal("feat awarded twice")
	}
	ts, err := s.TitlesOf(ctx, []int64{a.ID, b.ID})
	if err != nil || len(ts[a.ID]) != 1 || ts[a.ID][0] != (game.Title{Key: game.TitleChampion, Season: 1}) ||
		len(ts[b.ID]) != 1 || ts[b.ID][0].Season != 0 {
		t.Fatalf("titles %v %v", ts, err)
	}
}

func TestRecentEmbezzlements(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	now := time.Now()
	a := newChar(t, s, "a", "merchant")
	act, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: a.ID, Kind: game.KindEmbezzle, Goal: "skim", StartedAt: now, EndsAt: now}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := s.RecentEmbezzlements(ctx, "g", now.Add(-time.Hour)); n != 0 {
		t.Fatalf("unfinished counted: %d", n)
	}
	if err := s.ApplyResult(ctx, act.ID, Outcome{}, now); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.RecentEmbezzlements(ctx, "g", now.Add(-time.Hour)); n != 1 {
		t.Fatalf("recent = %d", n)
	}
	if n, _ := s.RecentEmbezzlements(ctx, "g", now.Add(time.Hour)); n != 0 {
		t.Fatalf("old counted: %d", n)
	}
}
