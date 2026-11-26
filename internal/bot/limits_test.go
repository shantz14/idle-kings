package bot

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
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
	ids := map[string]bool{}
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
				if v.CustomID != "" && ids[v.CustomID] {
					t.Errorf("%s: duplicate custom ID %q", where, v.CustomID)
				}
				ids[v.CustomID] = true
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
	total := 0 // Discord's 6000 counts every embed in the message
	for _, e := range d.Embeds {
		total += n(e.Title) + n(e.Description)
		if e.Footer != nil {
			total += n(e.Footer.Text)
		}
		if n(e.Title) > 256 || n(e.Description) > 4096 || len(e.Fields) > 25 {
			t.Errorf("%s: embed too big", where)
		}
		for _, f := range e.Fields {
			total += n(f.Name) + n(f.Value)
			if n(f.Name) > 256 || n(f.Value) > 1024 || f.Value == "" || f.Name == "" {
				t.Errorf("%s: field %q (%d chars)", where, f.Name, n(f.Value))
			}
		}
	}
	if total > 6000 {
		t.Errorf("%s: embeds total %d chars", where, total)
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
			Components: matterButtons(m, ev, true, true),
		})
	}
	// The Senate panel with a petition alongside the day's matter, for the
	// longest day's matter and every petition.
	longest, size := game.MatterEvents[0], 0
	for _, ev := range game.MatterEvents {
		e := matterEmbed(store.Matter{ID: 999999}, ev, game.StartingRealm())
		if l := len(e.Description); l > size {
			longest, size = ev, l
		}
	}
	day := store.Matter{ID: 999998}
	for _, p := range game.Petitions(nil) {
		ev, ok := game.MatterByKey(p.Event)
		if !ok {
			t.Fatalf("petition %s has no matter", p.Key)
		}
		pet := store.Matter{ID: 999999, PetitionerID: 1}
		petEmbed := matterEmbed(pet, ev, game.StartingRealm())
		petEmbed.Footer = &discordgo.MessageEmbedFooter{Text: "Brought before the Senate by the petition of Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww, House Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww"}
		realm := &discordgo.MessageEmbed{Title: "The realm", Description: trackersLine(game.StartingRealm()), Fields: []*discordgo.MessageEmbedField{lawsField(nil)}}
		checkLimits(t, "senate panel with "+p.Key, &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{realm, matterEmbed(day, longest, game.StartingRealm()), petEmbed},
			Components: append(matterButtons(day, longest, false, false), matterButtons(pet, ev, true, true)...),
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

func TestCourtViewFitsDiscord(t *testing.T) {
	sen := game.GenerateSenate(rand.New(rand.NewPCG(1, 2)), game.SenateSize)
	for i := range sen {
		sen[i].ID = int64(i + 1)
	}
	ops := game.Opinions{1: 100, 2: -100, 3: 0.4, 4: -37.6}
	for _, gold := range []int{0, 10000} {
		c := store.Character{Name: "A", FamilyName: "B", Wealth: gold}
		for _, id := range []int64{0, 1, 2, 3, 4} {
			for _, busy := range []string{"", "You are calling on Senator Wwwwwwwwwwwwww. You'll be back <t:1999999999:R>."} {
				checkLimits(t, fmt.Sprintf("court %d %d %q", gold, id, busy), courtView(c, sen, ops, id, time.Hour, busy, "a note"))
			}
		}
	}
	m := store.Matter{ID: 999999}
	checkLimits(t, "bribe select", &discordgo.InteractionResponseData{
		Components: []discordgo.MessageComponent{senatorSelect(m, sen, map[int64]store.Bribe{1: {}}, ops)}})
}

func TestOpinionTags(t *testing.T) {
	for v, want := range map[float64]string{35: "🟢 +35", -20: "🔴 -20", 0.3: "⚪ 0", -0.4: "⚪ 0"} {
		if got := opinionTag(v); got != want {
			t.Errorf("opinionTag(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestInvestViewsFitDiscord(t *testing.T) {
	st := store.RealmState{Trackers: game.StartingRealm(), Sectors: game.StartingSectors(), Status: "alive"}
	c := store.Character{Name: "A", FamilyName: "B", Wealth: 30, Ventures: map[game.SectorKey]store.Venture{}}
	for _, d := range game.SectorDefs {
		c.Ventures[d.Key] = store.Venture{Stake: 1000, Share: 0.4567, Income: 123.4}
	}
	c.VentureIncome = 863.8
	long := "You are negotiating a stake in Timber & Shipbuilding. The deal is struck <t:9999999999:R>."
	checkLimits(t, "invest picker", investView(c, st, "", long, "a note", time.Hour))
	for _, d := range game.SectorDefs {
		checkLimits(t, "invest "+string(d.Key), investView(c, st, d.Key, long, "a note", time.Hour))
	}
	page := &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{Title: "x", Description: "x",
		Fields: []*discordgo.MessageEmbedField{{Name: "Industries", Value: industriesBlock(st)}, venturesField(c, true)}}}}
	checkLimits(t, "industries and ventures", page)
	if f := venturesField(c, false); f == nil || strings.Contains(f.Value, "Black Market") {
		t.Fatal("the Black Market must stay off public profiles")
	}
}

func TestMartialViewsFitDiscord(t *testing.T) {
	long := "Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww"
	c := store.Character{ID: 1, Name: long, FamilyName: long, Wealth: 10000, Crest: game.Crest{Color: "red", Symbol: "lion"}}
	d := store.Character{ID: 2, Name: long, FamilyName: long, Wealth: 0, Crest: game.Crest{Color: "red", Symbol: "lion"}}
	busy := "You are calling on Senator Wwwwwwwwwwwwww. You'll be back <t:1999999999:R>."
	for _, timing := range []game.DuelTiming{game.DefaultDuelTiming, game.FastDuelTiming} {
		checkLimits(t, "duel", duelView(timing, "", "a note"))
		checkLimits(t, "duel busy", duelView(timing, busy, ""))
	}
	checkLimits(t, "wager", wagerView(c, d, "a note"))
	checkLimits(t, "wager broke", wagerView(d, c, ""))
	a := store.Activity{ID: 999999, Spend: 1000, EndsAt: time.Unix(1999999999, 0)}
	checkLimits(t, "bet", betView(a, c, d, "a note"))
	for _, goal := range []string{game.DuelPending, game.DuelAccepted} {
		a.Goal = goal
		checkLimits(t, "announce "+goal, &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{duelAnnouncement(a, c, d, "")}, Components: betButtons(a, c, d)})
	}
	checkLimits(t, "announce result", &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{duelAnnouncement(a, c, d, "**"+long+"** wins the duel decisively.")}})

	checkLimits(t, "hunt", huntView(2*time.Hour, "", "a note"))
	checkLimits(t, "hunt busy", huntView(2*time.Hour, busy, ""))
	checkLimits(t, "hunt join", huntJoinView(a, c))
	checkLimits(t, "accident", accidentPicker(a.ID, ""))

	for _, jt := range []game.PartyTiming{game.DefaultJourneyTiming, game.FastJourneyTiming} {
		checkLimits(t, "journey", journeyView(jt, 10000, "", "a note"))
		checkLimits(t, "journey busy", journeyView(jt, 0, busy, ""))
	}
	checkLimits(t, "hire bandits", hireView(a, c, d))

	// Event DMs: one button per choice, all in one row.
	for _, list := range [][]game.PartyEvent{game.PartyEvents, game.HuntEvents, game.DuelEvents, game.JourneyEvents, game.JourneyAbroadEvents} {
		for _, ev := range list {
			var btns []discordgo.MessageComponent
			for _, ch := range ev.Choices {
				btns = append(btns, button(ch.Label, fmt.Sprintf("party:event:%d:%s", 999999999, ch.Key), discordgo.PrimaryButton, false))
			}
			def, _ := ev.Choice(ev.Default)
			checkLimits(t, "event "+ev.Key, &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{eventResultEmbed("On the dueling ground", ev, def, def.Success, true)},
				Components: []discordgo.MessageComponent{row(btns...)}})
		}
	}
}

func TestIntrigueViewsFitDiscord(t *testing.T) {
	long := "Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww"
	c := store.Character{ID: 1, Name: long, FamilyName: long, Wealth: 1000}
	tgt := store.Character{ID: 999999999, Name: long, FamilyName: long}
	busy := "You are searching your household for spies. You'll be done <t:9999999999:R>."
	var lines []spyLine
	for i := 0; i < 40; i++ {
		top := game.SpyTiers[len(game.SpyTiers)-1]
		lines = append(lines, spyLine{Name: long + " " + long, Tier: top.Name, Flavor: fmt.Sprintf(top.Flavor, long+" "+long)})
	}
	checkLimits(t, "plant", plantView(lines, time.Hour, "", "a note"))
	checkLimits(t, "plant busy", plantView(nil, time.Hour, busy, ""))
	for _, tier := range game.SpyTiers {
		checkLimits(t, "purse "+tier.Name, purseView(c, tgt, tier, "High", "a note"))
	}
	checkLimits(t, "root out", rootOutView(2*time.Hour, "", "a note"))
	checkLimits(t, "root out busy", rootOutView(2*time.Hour, busy, ""))
	checkLimits(t, "intrigue category", categoryPanel(game.CatIntrigue, "scribe"))
}

func TestEstateViewsFitDiscord(t *testing.T) {
	// Someone who owns the whole catalogue.
	rich := store.Character{ID: 1, Name: "A", FamilyName: "B", Wealth: 100000, Crest: game.Crest{Color: "red", Symbol: "lion"}, Stats: game.Stats{}}
	for n, it := range game.Items {
		rich.Holdings = append(rich.Holdings, store.Holding{ID: int64(n + 1), Key: it.Key})
		rich.Items = append(rich.Items, it.Key)
		if it.IsProperty() && rich.ResidenceID == 0 {
			rich.ResidenceID = int64(n + 1)
		}
	}
	poor := store.Character{ID: 2, Name: "C", FamilyName: "D", Crest: game.Crest{Color: "red", Symbol: "lion"}, Stats: game.Stats{},
		Holdings: []store.Holding{{ID: 999, Key: "rented_room"}}, Items: []string{"rented_room"}, ResidenceID: 999}
	checkLimits(t, "estate (everything)", estateView(rich, true, "a note"))
	checkLimits(t, "estate (one room)", estateView(poor, false, ""))
	for _, h := range rich.Holdings {
		checkLimits(t, "sell "+h.Key, sellConfirm(rich, h.ID))
	}

	st := shopState{period: 1, ends: time.Now(), owners: map[string]int64{}, houses: map[int64]string{1: "House B"}, market: map[string]bool{}}
	st.stock = game.FixedShelf()
	for _, it := range game.Items { // the whole catalogue on sale at once
		if it.Shelf == game.ShelfMarket {
			st.stock = append(st.stock, it)
			st.market[it.Key] = true
		}
	}
	for _, c := range []store.Character{rich, poor} {
		for _, tab := range []string{shopProps, shopGoods} {
			checkLimits(t, "shop "+tab, shopView(c, st, tab, "a note"))
		}
		for _, it := range game.Items {
			checkLimits(t, "item "+it.Key, itemView(c, st, it.Key, "a note"))
		}
	}

	dr := dealDraft{ToID: 2, Gold: -5000}
	for _, h := range rich.Holdings {
		dr.Give = append(dr.Give, h.ID)
	}
	checkLimits(t, "deal composer", dealView(rich, poor, dr, "a note"))
	checkLimits(t, "deal composer (reverse)", dealView(poor, rich, dealDraft{ToID: 1, Want: dr.Give, Gold: 300}, ""))
	checkLimits(t, "deal start", dealStart("a note"))
	o := store.EstateOffer{ID: 99999, Gold: 5000, ExpiresAt: time.Now()}
	msg := offerMessage(o, rich, poor, names(rich, dr.Give), []string{"Rented Room in the Lower City"})
	checkLimits(t, "offer DM", &discordgo.InteractionResponseData{Embeds: msg.Embeds, Components: msg.Components})
}

func TestInfluenceViewsFitDiscord(t *testing.T) {
	long := "Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww"
	busy := "You are calling on Senator Wwwwwwwwwwwwww. You'll be back <t:1999999999:R>."
	for _, gold := range []int{0, 100000} {
		c := store.Character{ID: 1, Name: long, FamilyName: long, Wealth: gold, Monuments: []string{"statue"}}
		for _, b := range []string{"", busy} {
			checkLimits(t, "canvass", canvassView(3*time.Hour, b, "a note"))
			checkLimits(t, "speech", speechView(time.Hour, b, "a note"))
			checkLimits(t, "circus", circusView(c, 2*time.Hour, b, "a note"))
			times := map[string]time.Duration{}
			for _, w := range game.WorkDefs {
				times[w.Key] = time.Duration(w.Days * float64(24*time.Hour))
			}
			checkLimits(t, "works", worksView(c, times, b, "a note"))
		}
		for _, cause := range game.Causes {
			a := store.Activity{ID: 999999, Goal: cause.Key}
			checkLimits(t, "speech attend "+cause.Key, speechAttendView(a, c))
		}
		if f := monumentsField(store.Character{FamilyName: long, Monuments: []string{"statue", "road", "temple", "aqueduct"}}); f == nil || len(f.Value) > 1024 {
			t.Fatal("monuments field")
		}
	}
}

func TestSheetAndChronicleFitDiscord(t *testing.T) {
	long := "Wwwwwwwwwwwwwwwwwwwwwwwwwwwwwwww"
	var lines []spyLine
	for i := 0; i < 40; i++ {
		top := game.SpyTiers[len(game.SpyTiers)-1]
		lines = append(lines, spyLine{Name: long, Tier: top.Name, Flavor: fmt.Sprintf(top.Flavor, long)})
	}
	c := store.Character{Name: long, FamilyName: long, Ventures: map[game.SectorKey]store.Venture{}}
	for _, d := range game.SectorDefs {
		c.Ventures[d.Key] = store.Venture{Stake: 1e6, Share: 0.999, Income: 123456}
	}
	for _, f := range []*discordgo.MessageEmbedField{spiesField(lines), spiesField(nil), ventureSheetField(c), ventureSheetField(store.Character{})} {
		checkLimits(t, "sheet "+f.Name, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{Title: "x", Fields: []*discordgo.MessageEmbedField{f}}}})
	}

	// Every arc at once, at its longest.
	var entries []game.ChronicleEntry
	for _, a := range game.Arcs {
		entries = append(entries, game.ChronicleEntry{Arc: a, Chapter: a.Chapters, Title: long + long, Choice: long + long})
	}
	checkLimits(t, "chronicle", &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{Title: "x",
		Fields: []*discordgo.MessageEmbedField{{Name: "Chronicle", Value: chronicleText(entries)}, {Name: "empty", Value: chronicleText(nil)}}}}})
}

func TestPerilViewsFitDiscord(t *testing.T) {
	busy := "You are cooking the treasury's books. You'll be done <t:9999999999:R>."
	checkLimits(t, "embezzle", embezzleView(2*time.Hour, "", "a note"))
	checkLimits(t, "embezzle busy", embezzleView(2*time.Hour, busy, ""))
	st := store.RealmState{Status: "alive", Trackers: game.StartingRealm()}
	checkLimits(t, "aid", aidView(st, "a note"))
	checkLimits(t, "aid fallen", aidView(store.RealmState{Status: "fallen", Trackers: game.StartingRealm()}, ""))
	c := store.Character{Wealth: 100}
	for _, tgt := range game.AidTargets {
		checkLimits(t, "aid "+tgt.Key, aidAmountView(c, tgt, 10, "a note"))
	}
	for _, cat := range game.Categories {
		checkLimits(t, "category "+string(cat.Key), categoryPanel(cat.Key, "scribe"))
	}
	for _, ev := range game.MatterEvents {
		if ev.Auto {
			res := game.ResolveMatter(rand.New(rand.NewPCG(1, 1)), game.MatterInput{Event: ev, Realm: game.StartingRealm()})
			checkLimits(t, "bill "+ev.Key, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{billEmbed(ev, res)}})
		}
	}
}

// longestLaws is the MaxLaws laws with the longest descriptions.
func longestLaws() game.Laws {
	defs := append([]game.LawDef(nil), game.LawDefs...)
	for i := range defs { // simple selection by description length
		for j := i + 1; j < len(defs); j++ {
			if len(defs[j].Desc)+len(defs[j].Name) > len(defs[i].Desc)+len(defs[i].Name) {
				defs[i], defs[j] = defs[j], defs[i]
			}
		}
	}
	var ls game.Laws
	for _, d := range defs[:game.MaxLaws] {
		ls = append(ls, game.Law{Key: d.Key})
	}
	return ls
}

func TestLawViewsFitDiscord(t *testing.T) {
	full, none := longestLaws(), game.Laws{}
	c := store.Character{Name: "A", FamilyName: "B", Wealth: 1000}
	busy := "You are lobbying the Senate's clerks. You'll know <t:9999999999:R>."
	for _, ls := range []game.Laws{full, none} {
		for _, key := range []string{"", game.Petitions(ls)[0].Key} {
			checkLimits(t, "petition "+key, petitionView(c, ls, nil, 2*time.Hour, "", key, "a note"))
			checkLimits(t, "petition busy "+key, petitionView(c, ls, []string{game.Petitions(ls)[0].Event}, 2*time.Hour, busy, key, ""))
		}
		checkLimits(t, "laws field", &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{Title: "x",
			Fields: []*discordgo.MessageEmbedField{lawsField(ls)}}}})
	}
	// Every motion, decided both ways, under the Secret Ballot and not, with
	// the most laws changing at once.
	r := rand.New(rand.NewPCG(1, 1))
	for _, ev := range game.MatterEvents {
		if ev.Motion == "" {
			continue
		}
		for _, secret := range []bool{false, true} {
			ls := full.Clone()
			if secret {
				ls = append(ls[:game.MaxLaws-1], game.Law{Key: game.LawSecretBallot})
			}
			res := game.ResolveMatter(r, game.MatterInput{Event: ev, Realm: game.StartingRealm(), Laws: ls})
			res.LawChanges = game.LawChanges{Enacted: []game.LawKey{full[0].Key}, Repealed: full.Keys()[1:3], Lapsed: full.Keys()[3:]}
			checkLimits(t, "decision "+ev.Key, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{decisionEmbed(ev, res)}})
		}
	}
}
