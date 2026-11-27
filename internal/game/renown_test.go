package game

import (
	"math"
	"testing"
)

func TestRenownWeight(t *testing.T) {
	for renown, want := range map[float64]float64{0: 0, 100: 1, 300: 2, -10: 0} {
		if got := RenownWeight(renown); math.Abs(got-want) > 1e-9 {
			t.Errorf("RenownWeight(%v) = %v, want %v", renown, got, want)
		}
	}
}

func TestRenownNeeded(t *testing.T) {
	prev := 0.0
	for _, tier := range []Tier{TierHumble, TierModest, TierFine, TierGrand, TierPalace, TierCastle} {
		it := Item{Kind: KindProperty, Tier: tier}
		need := RenownNeeded(it)
		if need < prev {
			t.Fatalf("%v needs less renown than the tier below", tier)
		}
		prev = need
	}
	if RenownNeeded(Item{Kind: KindProperty, Tier: TierFine}) != 0 || RenownNeeded(Item{Kind: KindProperty, Tier: TierCastle}) == 0 {
		t.Fatal("only grand houses are gated")
	}
}

func TestRenownIsWhole(t *testing.T) {
	for in, want := range map[float64]float64{0.1: 1, -0.3: -1, 2.4: 2, 2.5: 3, -7.6: -8, 0: 0} {
		if got := RoundRenown(in); got != want {
			t.Errorf("RoundRenown(%v) = %v, want %v", in, got, want)
		}
	}
	for _, b := range Backgrounds {
		if b.Renown < 1 || b.Renown != float64(int(b.Renown)) {
			t.Errorf("%s starts with %v renown; want a whole number, at least 1", b.Key, b.Renown)
		}
	}
	if s := NewSheet(nil, 0, 30, 0, 0); s[AttrRenown] != 3 {
		t.Errorf("checks see renown ÷ RenownScale: %v", s[AttrRenown])
	}
}
