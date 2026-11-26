package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestHireBandits(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer s.Close()
	_ = s.ClaimAdmin(ctx, "g", "a")
	traveler := newChar(t, s, "a", "scribe")
	hirer := newChar(t, s, "b", "merchant")
	poor := newChar(t, s, "c", "cupbearer")
	now := time.Now()
	j, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: traveler.ID, Kind: game.KindJourney, Goal: "caravan",
		Public: true, StartedAt: now, EndsAt: now.Add(time.Hour)}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	band, _ := game.BanditBandByKey("cutthroats")
	_, _ = s.db.Exec(`UPDATE characters SET wealth = wealth + ? WHERE id = ?`, band.Price, hirer.ID)
	_, _ = s.db.Exec(`UPDATE characters SET wealth = 0 WHERE id = ?`, poor.ID)
	before, _ := s.GetCharacterByID(ctx, hirer.ID)

	if err := s.HireBandits(ctx, j.ID, hirer.ID, band, now); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetCharacterByID(ctx, hirer.ID)
	if after.Wealth != before.Wealth-band.Price {
		t.Errorf("wealth %d → %d, want -%d", before.Wealth, after.Wealth, band.Price)
	}
	if err := s.HireBandits(ctx, j.ID, hirer.ID, band, now); !errors.Is(err, ErrExists) {
		t.Errorf("second hire: %v", err)
	}
	if err := s.HireBandits(ctx, j.ID, traveler.ID, band, now); !errors.Is(err, ErrOwnEvent) {
		t.Errorf("robbing yourself: %v", err)
	}
	sell, _ := game.BanditBandByKey("sellswords")
	if err := s.HireBandits(ctx, j.ID, poor.ID, sell, now); err == nil {
		t.Error("hired without the gold")
	}
	if ps, _ := s.Participants(ctx, j.ID); len(ps) != 1 || ps[0].Character.ID != hirer.ID {
		t.Errorf("participants: %+v (the poor hire should have rolled back)", ps)
	}
	if err := s.HireBandits(ctx, j.ID, poor.ID, band, now.Add(2*time.Hour)); !errors.Is(err, ErrEnded) {
		t.Errorf("after the journey: %v", err)
	}
}
