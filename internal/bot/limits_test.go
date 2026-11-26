package bot

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// checkLimits fails on anything Discord would reject.
func checkLimits(t *testing.T, where string, d *discordgo.InteractionResponseData) {
	t.Helper()
	n := func(s string) int { return utf8.RuneCountInString(s) }
	if len(d.Components) > 5 {
		t.Errorf("%s: %d rows", where, len(d.Components))
	}
	for _, c := range d.Components {
		r, ok := c.(discordgo.ActionsRow)
		if !ok {
			t.Errorf("%s: top-level component is not a row", where)
			continue
		}
		if len(r.Components) > 5 || len(r.Components) == 0 {
			t.Errorf("%s: row with %d components", where, len(r.Components))
		}
		for _, x := range r.Components {
			switch v := x.(type) {
			case discordgo.Button:
				if n(v.Label) > 80 || n(v.CustomID) > 100 || v.Label == "" {
					t.Errorf("%s: button %q / %q", where, v.Label, v.CustomID)
				}
			case discordgo.SelectMenu:
				if len(r.Components) != 1 {
					t.Errorf("%s: select must be alone in its row", where)
				}
				if v.MenuType == discordgo.StringSelectMenu && (len(v.Options) == 0 || len(v.Options) > 25) {
					t.Errorf("%s: select %s has %d options", where, v.CustomID, len(v.Options))
				}
				for _, o := range v.Options {
					if n(o.Label) > 100 || o.Label == "" || n(o.Description) > 100 || o.Description == "" || n(o.Value) > 100 {
						t.Errorf("%s: option %q / %q", where, o.Label, o.Description)
					}
				}
			}
		}
	}
	if len(d.Embeds) > 10 {
		t.Errorf("%s: %d embeds", where, len(d.Embeds))
	}
	for _, e := range d.Embeds {
		total := n(e.Title) + n(e.Description)
		if n(e.Title) > 256 || n(e.Description) > 4096 || len(e.Fields) > 25 {
			t.Errorf("%s: embed too big", where)
		}
		for _, f := range e.Fields {
			total += n(f.Name) + n(f.Value)
			if n(f.Name) > 256 || n(f.Value) > 1024 || f.Value == "" || f.Name == "" {
				t.Errorf("%s: field %q (%d chars)", where, f.Name, n(f.Value))
			}
		}
		if total > 6000 {
			t.Errorf("%s: embed totals %d chars", where, total)
		}
	}
}

func TestCreatorViewsFitDiscord(t *testing.T) {
	b := &Bot{}
	long := "Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww" // 32, the max
	base := draft{Build: game.Build{Name: long, Family: long, Points: game.Stats{},
		Crest: game.Crest{Motto: string(make([]rune, 0)) + "Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww"}}}
	for step := stepCrest; step <= stepRole; step++ {
		d := base
		d.Step = step
		checkLimits(t, fmt.Sprintf("empty step %d", step), b.creatorView(d, "a note"))
	}
	for _, bg := range game.Backgrounds {
		for _, r := range game.Roles {
			d := base
			d.Build.Crest.Color, d.Build.Crest.Symbol = "red", "lion"
			d.Build.Points = game.Stats{game.Diplomacy: 4, game.Prowess: 2}
			d.Build.Background, d.Build.Role = bg.Key, r.Key
			if err := d.Build.Validate(); err != nil {
				t.Fatalf("%s/%s: %v", bg.Key, r.Key, err)
			}
			for step := stepCrest; step <= stepRole; step++ {
				d.Step = step
				checkLimits(t, fmt.Sprintf("%s/%s step %d", bg.Key, r.Key, step), b.creatorView(d, ""))
			}
		}
	}
}

func TestPagesFitDiscord(t *testing.T) {
	for n := range helpPages {
		checkLimits(t, fmt.Sprintf("help %d", n), helpPage(n))
	}
	for _, c := range game.Categories {
		for _, r := range game.Roles {
			checkLimits(t, "category "+string(c.Key), categoryPanel(c.Key, r.Key))
		}
	}
	for _, ev := range game.MatterEvents {
		m := store.Matter{ID: 999999}
		checkLimits(t, "matter "+ev.Key, &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{matterEmbed(m, ev, game.StartingRealm())},
			Components: matterButtons(m, ev, true),
		})
	}
	for _, it := range game.Items {
		c := store.Character{Name: "A", FamilyName: "B", Role: "officer", Background: "foreign_exile",
			Crest: game.Crest{Color: "red", Symbol: "lion"}, Stats: game.Stats{}, Items: []string{it.Key}}
		checkLimits(t, "estate "+it.Key, (&Bot{}).estatePage(c, true))
	}
}

func TestPartyBudgetPanelFitsDiscord(t *testing.T) {
	for _, gold := range []int{0, 100, 10000} {
		c := store.Character{Name: "A", FamilyName: "B", Wealth: gold}
		for _, g := range game.HostGoals {
			checkLimits(t, fmt.Sprintf("budget %s %d", g.Key, gold), (&Bot{}).budgetPanel(c, g.Key, "a note"))
		}
	}
}
