package game

import (
	"math"
	"testing"
)

func TestOwnership(t *testing.T) {
	raw := map[int64]Stakes{1: {SectorMining: 3000}, 2: {SectorMining: 1000}, 3: {SectorTrade: 500}}
	for _, laws := range []Laws{nil, {{Key: LawMonopolies}}} {
		eff, totals := EffectiveStakes(laws, raw)
		for _, d := range SectorDefs {
			players, houses := Ownership(d, eff, totals)
			sum := houses
			for i, p := range players {
				sum += p.Share
				if want := Share(d, eff[p.CharacterID][d.Key], totals[d.Key]); math.Abs(p.Share-want) > 1e-12 {
					t.Errorf("%s: player %d share %v, want %v", d.Key, p.CharacterID, p.Share, want)
				}
				if i > 0 && players[i-1].Share < p.Share {
					t.Errorf("%s: not biggest first", d.Key)
				}
			}
			if math.Abs(sum-1) > 1e-12 {
				t.Errorf("%s (laws %v): shares sum to %v", d.Key, laws, sum)
			}
			if d.Key == SectorAgriculture && (len(players) != 0 || houses != 1) {
				t.Errorf("an industry nobody invested in should be all houses: %v %v", players, houses)
			}
		}
	}
}
