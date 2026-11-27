package game

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func TestHappeningsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, h := range HappeningDefs {
		if seen[h.Key] {
			t.Errorf("%s: duplicate key", h.Key)
		}
		seen[h.Key] = true
		if h.Weight <= 0 || h.MinDays < 1 || h.MaxDays < h.MinDays || h.Name == "" || h.Emoji == "" ||
			h.Announce == "" || h.Fade == "" || h.Hint == "" || (len(h.Events) == 0 && h.Key != KindAuctionHappening) {
			t.Errorf("%s: incomplete", h.Key)
		}
		for k := range h.Sectors {
			if _, ok := SectorByKey(k); !ok {
				t.Errorf("%s: no sector %s", h.Key, k)
			}
		}
		if h.Trophy != "" {
			if it, ok := ItemByKey(h.Trophy); !ok || it.Shelf != ShelfNone {
				t.Errorf("%s: trophy %s must exist and not be sold", h.Key, h.Trophy)
			}
		}
		ends := false
		for where, ev := range h.Events {
			if !contains([]string{MeetHunt, MeetJourney, MeetJourneyAbroad, MeetParty}, where) {
				t.Errorf("%s: met at unknown %q", h.Key, where)
			}
			if got, ok := EventByKey(ev.Key); !ok || got.Title != ev.Title {
				t.Errorf("%s/%s: not found by key %q", h.Key, where, ev.Key)
			}
			if hh, ok := HappeningOfEvent(ev.Key); !ok || hh.Key != h.Key {
				t.Errorf("%s/%s: HappeningOfEvent gives %q", h.Key, where, hh.Key)
			}
			if _, ok := ev.Choice(ev.Default); !ok {
				t.Errorf("%s/%s: default %q isn't a choice", h.Key, where, ev.Default)
			}
			for _, c := range ev.Choices {
				ends = ends || c.Success.Ends || c.Failure.Ends
			}
		}
		if ends && !strings.Contains(h.Ended, "%s") {
			t.Errorf("%s: can be ended, so Ended must name who did it", h.Key)
		}
	}
	// Only happening events end anything.
	for _, list := range [][]PartyEvent{PartyEvents, HuntEvents, DuelEvents, JourneyEvents, JourneyAbroadEvents} {
		for _, ev := range list {
			for _, c := range ev.Choices {
				if c.Success.Ends || c.Failure.Ends {
					t.Errorf("%s: only happening events can end a happening", ev.Key)
				}
			}
		}
	}
}

func TestPickHappening(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	active := []string{"manticore", "troll"}
	for i := 0; i < 500; i++ {
		h, ok := PickHappening(r, active, "kraken")
		if !ok || contains(active, h.Key) || h.Key == "kraken" {
			t.Fatalf("picked %q", h.Key)
		}
		if d := HappeningDays(r, h); d < h.MinDays || d > h.MaxDays {
			t.Fatalf("%s lasts %d days", h.Key, d)
		}
	}
	var all []string
	for _, h := range HappeningDefs {
		all = append(all, h.Key)
	}
	if _, ok := PickHappening(r, all, ""); ok {
		t.Fatal("nothing left to pick")
	}
	if evs := HappeningEvents(MeetHunt, []string{"manticore", "grendel"}); len(evs) != 1 || !strings.HasPrefix(evs[0].Key, "hap_manticore_") {
		t.Fatalf("hunt events: %+v", evs)
	}
}
