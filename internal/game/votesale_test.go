package game

import "testing"

func saleInput(t *testing.T) VoteSaleInput {
	t.Helper()
	ev, ok := MatterByKey("border_barbarians")
	if !ok {
		t.Fatal("no border_barbarians")
	}
	return VoteSaleInput{
		Seller:  Person{ID: 7, Name: "Lucius Test", Sheet: Sheet{AttrIntrigue: 2}},
		Senator: Senator{ID: 1, Name: "Crassus", Traits: []Trait{TraitGreedy}},
		Matter:  ev, Option: ev.Options[0].Key, Realm: StartingRealm(), MatterID: 1,
		Gold: 40,
	}
}

func TestVoteScandalChance(t *testing.T) {
	in := saleInput(t)
	base := VoteScandalChance(in)
	bump := func(name string, f func(*VoteSaleInput), wantUp bool) {
		c := in
		f(&c)
		got := VoteScandalChance(c)
		if wantUp && got <= base || !wantUp && got >= base {
			t.Errorf("%s: chance %d vs base %d", name, got, base)
		}
	}
	bump("more gold", func(c *VoteSaleInput) { c.Gold = 400 }, true)
	bump("a habit", func(c *VoteSaleInput) { c.Recent = 2 }, true)
	bump("spies", func(c *VoteSaleInput) { c.Spies = 60 }, true)
	bump("intrigue", func(c *VoteSaleInput) { c.Seller.Sheet = Sheet{AttrIntrigue: 6} }, false)

	worst, best := in, in
	worst.Gold, worst.Recent, worst.Spies, worst.Seller.Sheet = 100000, 20, 100, Sheet{}
	best.Gold, best.Seller.Sheet = 0, Sheet{AttrIntrigue: 50}
	if c := VoteScandalChance(worst); c != scandalMaxChance {
		t.Errorf("worst case %d, want the cap %d", c, scandalMaxChance)
	}
	if c := VoteScandalChance(best); c != scandalMinChance {
		t.Errorf("best case %d, want the floor %d", c, scandalMinChance)
	}
}

func TestResolveVoteSale(t *testing.T) {
	in := saleInput(t)
	senators := []Senator{
		in.Senator,
		{ID: 2, Name: "Agrees"},
		{ID: 3, Name: "Plain"},
		{ID: 4, Name: "Upright", Traits: []Trait{TraitIncorruptible}},
		{ID: 5, Name: "Stern", Traits: []Trait{TraitStern}},
		{ID: 6, Name: "Hawk", Traits: []Trait{TraitHawk}},
		{ID: 7, Name: "Dove", Traits: []Trait{TraitDove}},
	}

	if res := ResolveVoteSale(top{}, in, senators); res.Caught || len(res.Opinions) > 0 || res.Delta.Renown != 0 || res.Delta.Influence != 0 {
		t.Fatalf("a clean sale should cost nothing: %+v", res)
	}

	res := ResolveVoteSale(zero{}, in, senators)
	if !res.Caught || res.Delta.Renown >= 0 || res.Delta.Influence >= 0 || res.Public == "" {
		t.Fatalf("caught sale: %+v", res)
	}
	got := map[int64]float64{}
	for _, o := range res.Opinions {
		if o.CharacterID != in.Seller.ID {
			t.Errorf("opinion change for %d, want the seller", o.CharacterID)
		}
		got[o.SenatorID] = o.Delta
	}
	if _, ok := got[in.Senator.ID]; ok {
		t.Error("the buyer shouldn't mind")
	}
	for _, s := range senators[1:] {
		d, hit := got[s.ID]
		wanted := s.Preferred(in.Matter, in.Realm, in.MatterID)
		switch {
		case wanted == in.Option && hit:
			t.Errorf("%s wanted the same option but lost opinion", s.Name)
		case wanted != in.Option && !hit:
			t.Errorf("%s wanted %s but didn't mind", s.Name, wanted)
		case hit && (s.Has(TraitIncorruptible) || s.Has(TraitStern)) && d != scandalOutraged:
			t.Errorf("%s should be outraged, got %v", s.Name, d)
		case hit && !s.Has(TraitIncorruptible) && !s.Has(TraitStern) && d != scandalOpinion:
			t.Errorf("%s got %v, want %v", s.Name, d, scandalOpinion)
		}
	}
	if len(got) == 0 {
		t.Error("nobody minded; the test senators should disagree with the sale")
	}
}

func TestSoldVoteIsACrimeUnderBribery(t *testing.T) {
	if IsCrime("sold_vote", nil) {
		t.Error("selling a vote shouldn't be a crime without the law")
	}
	if !IsCrime("sold_vote", Laws{{Key: LawBribery}}) {
		t.Error("selling a vote should be a crime under the Law Against Bribery")
	}
}
