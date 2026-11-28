package game

import "testing"

func TestRebuildingWorks(t *testing.T) {
	w, _ := WorkByKey("aqueduct")
	if n, ok := NextWork("aqueduct", nil); !ok || n.Key != "aqueduct" {
		t.Fatalf("first the work itself: %+v", n)
	}
	n, ok := NextWork("aqueduct", []string{"aqueduct"})
	if !ok || n.Tier != 1 || n.Cost != 3*w.Cost || n.VoteWeight != w.VoteWeight || WorkTitle("Vane", n) != "The Grand Vane Aqueduct" {
		t.Fatalf("grand: %+v %q", n, WorkTitle("Vane", n))
	}
	if g, ok := NextWork("aqueduct", []string{"aqueduct", n.Key}); !ok || g.Tier != 2 || g.Cost != 9*w.Cost {
		t.Fatalf("great: %+v", g)
	}
	if _, ok := NextWork("aqueduct", []string{"aqueduct", "aqueduct@grand", "aqueduct@great"}); ok {
		t.Fatal("as great as it gets")
	}
	if MonumentEffects([]string{"aqueduct", "aqueduct@grand"}).VoteWeight != 2*w.VoteWeight {
		t.Fatal("a rebuild adds its weight again")
	}
	if PatronEarned([]string{"aqueduct@grand"}) {
		t.Fatal("rebuilds aren't the works")
	}
}
