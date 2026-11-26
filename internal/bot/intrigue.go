package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Intrigue (spy network) custom IDs:
//   spy:who                    (user select) target chosen → purse panel
//   spy:go:<target>:<purse>    start planting spies
//   spy:root                   start rooting out spies

func (b *Bot) onSpy(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "who":
		vals := i.MessageComponentData().Values
		if len(vals) == 0 {
			return
		}
		t, err := b.store.GetCharacter(ctx, c.GuildID, vals[0])
		if err != nil || t.ID == c.ID {
			b.update(i, b.plantPanel(ctx, c, "Pick another player who has a place at this court."))
			return
		}
		b.update(i, b.pursePanel(ctx, c, t, ""))
	case parts[0] == "go" && len(parts) > 2:
		b.onPlantGo(ctx, i, c, atoi(parts[1]), parts[2])
	case parts[0] == "root":
		b.onRootOutGo(ctx, i, c)
	}
}

// spyBusy says why the character can't start an intrigue action, or "".
func (b *Bot) spyBusy(ctx context.Context, c store.Character) string {
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		return b.activityLine(ctx, a)
	}
	if st, err := b.store.GetRealm(ctx, c.GuildID); err == nil && !st.Alive() {
		return "The realm has fallen. There are no secrets left worth keeping."
	}
	return ""
}

// spyLine is one of your networks, for the panel and the character sheet.
type spyLine struct {
	Name   string
	Tier   string
	Flavor string // the tier's description, with the target's name filled in
}

// spyLines loads a character's spy networks above None.
func (b *Bot) spyLines(ctx context.Context, c store.Character) []spyLine {
	nets, err := b.store.SpyNetworksBy(ctx, c.ID)
	if err != nil {
		slog.Error("spy networks", "err", err)
	}
	var lines []spyLine
	for _, n := range nets {
		if game.TierIndex(n.Value) == 0 {
			continue
		}
		if t, err := b.store.GetCharacterByID(ctx, n.TargetID); err == nil {
			tier := game.SpyTierOf(n.Value)
			lines = append(lines, spyLine{Name: t.FullName(), Tier: tier.Name, Flavor: fmt.Sprintf(tier.Flavor, t.FullName())})
		}
	}
	return lines
}

// spiesText lists networks within max characters.
func spiesText(lines []spyLine, max int) string {
	var out []string
	for _, l := range lines {
		s := fmt.Sprintf("**%s** — **%s**", l.Name, l.Tier)
		if l.Flavor != "" {
			s += ": *" + l.Flavor + "*"
		}
		out = append(out, s)
	}
	return joinLines(out, max)
}

// spiesField is the character sheet's view of your networks.
func spiesField(lines []spyLine) *discordgo.MessageEmbedField {
	v := "No spies placed yet. Intrigue → Plant Spies."
	if len(lines) > 0 {
		v = spiesText(lines, 1000)
	}
	return &discordgo.MessageEmbedField{Name: "Your spies", Value: v}
}

// nextTier is the tier above the given level, or "" at the top.
func nextTier(v float64) string {
	if i := game.TierIndex(v); i+1 < len(game.SpyTiers) {
		return game.SpyTiers[i+1].Name
	}
	return ""
}

// ---- Plant Spies ----

func (b *Bot) plantPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	return plantView(b.spyLines(ctx, c), b.t.Spy, b.spyBusy(ctx, c), note)
}

func plantView(lines []spyLine, d time.Duration, busy, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Slip an agent into another player's household. It takes %s. ", humanDuration(d))
	sb.WriteString("Your Intrigue is set against theirs, and gold opens doors. The deeper your network already runs, the harder it is to go deeper.\n\n")
	sb.WriteString("Spies report what their master does in private: bribes, deals, secret votes, schemes. The deeper the network, the more often they report and the more they learn.\n\n")
	sb.WriteString("**Your network stays in place and builds up.** Each successful attempt takes it deeper: Low, Moderate, High, Very High, Extremely High. ")
	sb.WriteString("It fades a little each day, and they can root it out, but you never start again from nothing.\n\n")
	sb.WriteString("A clumsy attempt may be noticed. A very clumsy one gets your agent caught, and they will give up your name.\n\n")
	sb.WriteString("**Your spies**\n")
	if len(lines) == 0 {
		sb.WriteString("You have no spies in anyone's court.\n")
	} else {
		sb.WriteString(spiesText(lines, 2000) + "\n")
	}
	if busy != "" {
		sb.WriteString("\n" + busy)
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	comps := []discordgo.MessageComponent{}
	if busy == "" {
		comps = append(comps, userSelect("spy:who", "Whose court will you infiltrate?"))
	}
	comps = append(comps, row(button("Back", "menu:cat:"+string(game.CatIntrigue), discordgo.SecondaryButton, false)))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Plant Spies", Description: sb.String(), Color: colorGold}},
		Components: comps,
	}
}

func (b *Bot) pursePanel(ctx context.Context, c, t store.Character, note string) *discordgo.InteractionResponseData {
	lvl, err := b.store.SpyLevel(ctx, c.ID, t.ID)
	if err != nil {
		slog.Error("spy level", "err", err)
	}
	return purseView(c, t, game.SpyTierOf(lvl), nextTier(lvl), note)
}

func purseView(c, t store.Character, tier game.SpyTier, next, note string) *discordgo.InteractionResponseData {
	climb := "Your network there is as deep as it can go; another attempt keeps it from fading."
	if next != "" {
		climb = fmt.Sprintf("Next: **%s**. A successful attempt builds on what you already have.", next)
	}
	desc := fmt.Sprintf("**%s** (%s)\nYour spies there: **%s**. %s\n%s\n\nHow much gold will you spend to open doors?\n\nYou have **%d gold**.",
		t.FullName(), t.House(), tier.Name, fmt.Sprintf(tier.Flavor, t.FullName()), climb, c.Wealth)
	if note != "" {
		desc += "\n\n" + note
	}
	var btns []discordgo.MessageComponent
	for _, p := range game.SpyPurses {
		label := p.Name
		if p.Gold > 0 {
			label = fmt.Sprintf("%s (%d gold)", p.Name, p.Gold)
		}
		btns = append(btns, button(label, fmt.Sprintf("spy:go:%d:%s", t.ID, p.Key), discordgo.SuccessButton, p.Gold > c.Wealth))
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Plant Spies on " + t.FullName(), Description: desc, Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...),
			row(button("Back", "menu:act:"+game.KindPlantSpies, discordgo.SecondaryButton, false))},
	}
}

func (b *Bot) onPlantGo(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, targetID int64, purseKey string) {
	t, err := b.store.GetCharacterByID(ctx, targetID)
	if err != nil || t.GuildID != c.GuildID || t.ID == c.ID {
		b.update(i, b.plantPanel(ctx, c, "Pick another player who has a place at this court."))
		return
	}
	p, ok := game.PurseByKey(purseKey)
	if !ok {
		return
	}
	if busy := b.spyBusy(ctx, c); busy != "" {
		b.update(i, b.plantPanel(ctx, c, ""))
		return
	}
	now := time.Now()
	ends := now.Add(b.t.Spy)
	if _, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindPlantSpies, Goal: p.Key, TargetID: t.ID,
		StartedAt: now, EndsAt: ends,
	}, p.Gold, nil); err != nil {
		b.update(i, b.pursePanel(ctx, c, t, err.Error()))
		return
	}
	text := fmt.Sprintf("Your agent sets out for **%s**'s house. I'll DM you <t:%d:R> with how it went.", t.FullName(), ends.Unix())
	if p.Gold > 0 {
		text += fmt.Sprintf(" You spent **%d gold**.", p.Gold)
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Planting Spies", Description: text, Color: colorGreen}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:home", discordgo.SecondaryButton, false))},
	})
	b.leak(ctx, c, game.SecretPlant(c.FullName(), t.ID, t.FullName()))
}

func (b *Bot) resolvePlant(ctx context.Context, a store.Activity, now time.Time) {
	c, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	t, err2 := b.store.GetCharacterByID(ctx, a.TargetID)
	p, ok := game.PurseByKey(a.Goal)
	if err1 != nil || err2 != nil || !ok {
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now)
		return
	}
	before, err := b.store.SpyLevel(ctx, c.ID, t.ID)
	if err != nil {
		slog.Error("spy level", "err", err)
		return
	}
	res := game.ResolvePlant(b.rng, game.PlantInput{Spy: c.Person(), Target: t.Person(), Level: before, Purse: p})
	out := store.Outcome{
		Deltas: map[int64]game.Delta{c.ID: res.Practice},
		Spies:  []game.SpyChange{{Owner: c.ID, Target: t.ID, Delta: res.Delta}},
	}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply plant", "err", err)
		}
		return
	}
	after := game.SpyTierOf(game.ClampSpy(before + res.Delta))
	color := colorGold
	switch {
	case res.Success:
		color = colorGreen
	case res.Exposure != game.Unseen:
		color = colorRed
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title:       "Your spies in " + t.House(),
		Description: res.Text + fmt.Sprintf("\n\nYour spies there: **%s**. %s", after.Name, fmt.Sprintf(after.Flavor, t.FullName())),
		Color:       color,
	}}})
	switch res.Exposure {
	case game.Detected:
		_, _ = b.dm(t.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
			Title:       "A stranger at your door",
			Description: "You caught someone trying to plant spies in your court. They slipped away before you learned who sent them.",
			Color:       colorRed,
		}}})
	case game.Caught:
		desc := fmt.Sprintf("You caught **%s**'s spy in your court.", c.FullName())
		if res.Close {
			desc += " It was someone very close to you."
		}
		_, _ = b.dm(t.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
			Title: "A spy unmasked", Description: desc, Color: colorRed,
		}}})
	}
}

// ---- Root Out Spies ----

func (b *Bot) rootOutPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	return rootOutView(b.t.RootOut, b.spyBusy(ctx, c), note)
}

func rootOutView(d time.Duration, busy, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Question your servants, check the locks and read your own mail twice. It takes %s.\n\n", humanDuration(d))
	sb.WriteString("Your Intrigue is set against every player who may have spies in your house. Deep networks are easier to find. ")
	sb.WriteString("Those you find are dismissed, and if you do especially well you may learn who sent them.")
	if busy != "" {
		sb.WriteString("\n\n" + busy)
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Root Out Spies", Description: sb.String(), Color: colorGold}},
		Components: []discordgo.MessageComponent{row(
			button("Root them out", "spy:root", discordgo.SuccessButton, busy != ""),
			button("Back", "menu:cat:"+string(game.CatIntrigue), discordgo.SecondaryButton, false),
		)},
	}
}

func (b *Bot) onRootOutGo(ctx context.Context, i *discordgo.InteractionCreate, c store.Character) {
	if busy := b.spyBusy(ctx, c); busy != "" {
		b.update(i, b.rootOutPanel(ctx, c, ""))
		return
	}
	now := time.Now()
	ends := now.Add(b.t.RootOut)
	if _, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindRootOut, StartedAt: now, EndsAt: ends,
	}, 0, nil); err != nil {
		b.update(i, b.rootOutPanel(ctx, c, err.Error()))
		return
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Rooting Out Spies",
			Description: fmt.Sprintf("You turn your household upside down. I'll DM you <t:%d:R> with what you found.", ends.Unix()), Color: colorGreen}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:home", discordgo.SecondaryButton, false))},
	})
	b.leak(ctx, c, game.SecretRootOut(c.FullName()))
}

func (b *Bot) resolveRootOut(ctx context.Context, a store.Activity, now time.Time) {
	c, err := b.store.GetCharacterByID(ctx, a.HostID)
	if err != nil {
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now)
		return
	}
	nets, err := b.store.SpyNetworksOn(ctx, c.ID)
	if err != nil {
		slog.Error("spy networks", "err", err)
		return
	}
	owners := map[int64]store.Character{}
	in := game.RootInput{Player: c.Person(), Laws: c.Laws}
	for _, n := range nets {
		o, err := b.store.GetCharacterByID(ctx, n.OwnerID)
		if err != nil {
			continue
		}
		owners[o.ID] = o
		in.Networks = append(in.Networks, game.Network{Spy: o.Person(), Level: n.Value})
	}
	res := game.ResolveRootOut(b.rng, in)
	out := store.Outcome{Deltas: map[int64]game.Delta{c.ID: res.Practice}}
	for _, h := range res.Hits {
		out.Spies = append(out.Spies, game.SpyChange{Owner: h.Spy.ID, Target: c.ID, Delta: h.Delta})
	}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply root out", "err", err)
		}
		return
	}

	var sb strings.Builder
	var named []string
	for _, h := range res.Hits {
		if h.Named {
			named = append(named, "**"+owners[h.Spy.ID].FullName()+"**")
		}
	}
	switch n := len(res.Hits); {
	case n == 0:
		sb.WriteString("You found nothing. Either your house is loyal, or your enemies are careful.")
	case n == 1:
		sb.WriteString("You dismissed a servant of doubtful loyalty.")
	default:
		fmt.Fprintf(&sb, "You dismissed %d servants of doubtful loyalty, each in the pay of a different house.", n)
	}
	if len(named) > 0 {
		fmt.Fprintf(&sb, "\n\nUnder questioning they named their masters: %s.", strings.Join(named, ", "))
	}
	color := colorGold
	if len(res.Hits) > 0 {
		color = colorGreen
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: "You searched your household", Description: sb.String(), Color: color,
	}}})
	for _, h := range res.Hits {
		o := owners[h.Spy.ID]
		_, _ = b.dm(o.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
			Title:       "Silence from " + c.House(),
			Description: fmt.Sprintf("Your agents in %s's court have gone quiet.", c.FullName()),
			Color:       colorRed,
		}}})
	}
}
