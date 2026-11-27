package game

import "testing"

func TestTitlesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range TitleDefs {
		if d.Key == "" || d.Name == "" || d.Emoji == "" || d.Description == "" {
			t.Errorf("incomplete title %+v", d)
		}
		if seen[d.Key] {
			t.Errorf("duplicate title %s", d.Key)
		}
		seen[d.Key] = true
	}
	// Every happening someone can end has a feat for it.
	for _, h := range HappeningDefs {
		ends := false
		for _, ev := range h.Events {
			for _, c := range ev.Choices {
				ends = ends || c.Success.Ends || c.Failure.Ends
			}
		}
		if ends != (h.Feat != "") {
			t.Errorf("%s: ends %v but feat %q", h.Key, ends, h.Feat)
		}
		if _, ok := TitleByKey(HappeningFeat(h.Key)); h.Feat != "" && !ok {
			t.Errorf("%s: feat not indexed", h.Key)
		}
	}
	for _, d := range SeasonTitles() {
		if d.Key != TitleGreatHouse && d.Key != TitleMasterOfCoin && d.Deed == "" {
			t.Errorf("season title %s has no deed", d.Key)
		}
	}
}

func TestPrestigeAndWealth(t *testing.T) {
	titles := []Title{{Key: TitleGreatHouse, Season: 1}, {Key: FeatPatron}}
	if got := Prestige(120, 8, 2.5, titles); got != 120+10*10.5+100+25 {
		t.Fatalf("prestige %v", got)
	}
	if got := Prestige(-5, -1, 0, nil); got != 0 {
		t.Fatalf("negative prestige %v", got)
	}
	if got := Wealth(300, 450.4, 1200); got != 1950 {
		t.Fatalf("wealth %v", got)
	}
	if (Title{Key: TitleGreatHouse, Season: 2}).Label() != "👑 Great House (Season 2)" {
		t.Fatal("label")
	}
}

func TestWinners(t *testing.T) {
	if w := Winners([]Score{{1, 3}, {2, 5}, {3, 5}}); len(w) != 2 || w[0] != 2 || w[1] != 3 {
		t.Fatalf("tie: %v", w)
	}
	if w := Winners([]Score{{1, 0}, {2, 0}}); w != nil {
		t.Fatalf("nobody should win with nothing: %v", w)
	}
	if r := Ranked([]Score{{1, 3}, {2, 5}}); r[0].CharacterID != 2 {
		t.Fatalf("ranked %v", r)
	}
}

func TestFeatChecks(t *testing.T) {
	var all []string
	for _, w := range WorkDefs {
		all = append(all, w.Key)
	}
	if !PatronEarned(all) || PatronEarned(all[1:]) {
		t.Fatal("patron")
	}
	if !WhispersEarned([]float64{90, 95, 100, 10}) || WhispersEarned([]float64{90, 95, 60}) {
		t.Fatal("whispers")
	}
}
