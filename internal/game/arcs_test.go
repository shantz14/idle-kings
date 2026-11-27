package game

import (
	"math/rand/v2"
	"testing"
)

func outcomes(o MatterOption) []MatterOutcome {
	if o.Check != nil {
		return []MatterOutcome{o.Success, o.Failure}
	}
	return []MatterOutcome{o.Success}
}

func TestArcsAreWellFormed(t *testing.T) {
	byArc := map[string][]MatterEvent{}
	for _, e := range MatterEvents {
		if e.Arc == "" {
			continue
		}
		a, ok := ArcByKey(e.Arc)
		if !ok {
			t.Fatalf("%s: unknown arc %s", e.Key, e.Arc)
		}
		if e.Chapter < 1 || e.Chapter > a.Chapters {
			t.Errorf("%s: chapter %d of %d", e.Key, e.Chapter, a.Chapters)
		}
		if (e.Chapter == 1) == e.FollowUpOnly {
			t.Errorf("%s: only chapter 1 should open on its own", e.Key)
		}
		byArc[e.Arc] = append(byArc[e.Arc], e)
	}
	for _, a := range Arcs {
		evs := byArc[a.Key]
		chapters := map[int]bool{}
		openers := 0
		for _, e := range evs {
			chapters[e.Chapter] = true
			if e.Chapter == 1 {
				openers++
			}
		}
		if openers != 1 {
			t.Errorf("%s: %d openers", a.Key, openers)
		}
		for c := 1; c <= a.Chapters; c++ {
			if !chapters[c] {
				t.Errorf("%s: no chapter %d", a.Key, c)
			}
		}
		// Every chapter can be reached from the opener.
		reached := map[string]bool{}
		var queue []string
		for _, e := range evs {
			if e.Chapter == 1 {
				queue = append(queue, e.Key)
			}
		}
		for len(queue) > 0 {
			k := queue[0]
			queue = queue[1:]
			if reached[k] {
				continue
			}
			reached[k] = true
			e, _ := MatterByKey(k)
			for _, o := range e.Options {
				for _, out := range outcomes(o) {
					for _, f := range out.FollowUps {
						queue = append(queue, f.Event)
					}
				}
			}
		}
		for _, e := range evs {
			if !reached[e.Key] {
				t.Errorf("%s: %s can't be reached from the opener", a.Key, e.Key)
			}
		}
		if !a.Strict {
			continue
		}
		// Strict arcs always move forward, and the last chapter ends it.
		for _, e := range evs {
			for _, o := range e.Options {
				for _, out := range outcomes(o) {
					var onward bool
					for _, f := range out.FollowUps {
						n, _ := MatterByKey(f.Event)
						if n.Arc == a.Key {
							if f.Chance < 1 || n.Chapter <= e.Chapter {
								t.Errorf("%s/%s: weak or backward follow-up %+v", e.Key, o.Key, f)
							}
							onward = true
						}
					}
					if e.Chapter < a.Chapters && !onward {
						t.Errorf("%s/%s: the story stops before the end", e.Key, o.Key)
					}
					if e.Chapter == a.Chapters && onward {
						t.Errorf("%s/%s: the last chapter continues", e.Key, o.Key)
					}
				}
			}
		}
	}
}

func TestEventsUseKnownSectors(t *testing.T) {
	for _, e := range MatterEvents {
		for _, o := range e.Options {
			for _, out := range outcomes(o) {
				for k := range out.Sectors {
					if _, ok := SectorByKey(k); !ok {
						t.Errorf("%s/%s: unknown sector %s", e.Key, o.Key, k)
					}
				}
			}
		}
	}
}

func TestPickMatterAvoidsRunningArcs(t *testing.T) {
	r := rand.New(rand.NewPCG(4, 4))
	active := ActiveArcs([]string{"witch_bargain", "barrow_herald", "star_cult", "elixir_sickness"})
	if !active["ashwood_witch"] || !active["barrow_king"] || len(active) != 4 {
		t.Fatalf("active arcs: %v", active)
	}
	for n := 0; n < 3000; n++ {
		e := PickMatterAvoiding(r, StartingRealm(), nil, active, nil)
		if active[e.Arc] {
			t.Fatalf("opened %s while %s was running", e.Key, e.Arc)
		}
	}
}

func TestChronicle(t *testing.T) {
	decisions := []Decided{ // newest first
		{EventKey: "barrow_herald", Winner: "pay"},
		{EventKey: "witch_settles", Winner: "seer"},
		{EventKey: "barrow_dead", Winner: "salt"},
		{EventKey: "plague", Winner: "quarantine"},
		{EventKey: "raise_taxes", Winner: "x"},
	}
	got := Chronicle(decisions, []string{"barrow_tithe"})
	if len(got) != 3 {
		t.Fatalf("chronicle: %+v", got)
	}
	if got[0].Arc.Key != "barrow_king" || got[0].Chapter != 3 || got[0].Status != ArcContinues || got[0].Choice == "" {
		t.Fatalf("barrow: %+v", got[0])
	}
	if got[1].Status != ArcOver || got[2].Status != ArcResting {
		t.Fatalf("statuses: %+v %+v", got[1], got[2])
	}
	for _, c := range got {
		if c.Line() == "" {
			t.Fatal("empty line")
		}
	}
	if l := (MatterEvent{Arc: "barrow_king", Chapter: 3}).ArcLabel(); l != "💀 Acererak, the Barrow King · Chapter 3" {
		t.Fatalf("label %q", l)
	}
}
