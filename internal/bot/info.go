package bot

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Info page custom IDs:
//   info:help:<page>     help pages
//   info:profile         (select) open a player's profile
//   info:senator         (select) open a senator's details

func (b *Bot) onInfoCommand(ctx context.Context, i *discordgo.InteractionCreate, name string) {
	if name == "help" {
		b.respond(i, ephemeral(helpPage(0)))
		return
	}
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	public := optBool(i, "public")
	show := func(d *discordgo.InteractionResponseData) {
		if !public {
			ephemeral(d)
		}
		b.respond(i, d)
	}
	switch name {
	case "realm":
		show(b.realmPage(ctx, c.GuildID))
	case "players":
		show(b.playersPage(ctx, c.GuildID))
	case "profile":
		target := c
		if uid := optString(i, "player"); uid != "" && uid != c.UserID {
			t, err := b.store.GetCharacter(ctx, c.GuildID, uid)
			if err != nil {
				b.reply(i, "They have no place at this court.")
				return
			}
			target = t
		}
		// Stats and gold only on your own profile, and only privately.
		show(&discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{
			b.profileEmbed(ctx, target, target.ID == c.ID && !public)}})
	}
}

func (b *Bot) onInfo(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	switch parts[0] {
	case "help":
		n := 0
		if len(parts) > 1 {
			n = int(atoi(parts[1]))
		}
		b.update(i, helpPage(n))
	case "profile":
		vals := i.MessageComponentData().Values
		if len(vals) == 0 {
			return
		}
		t, err := b.store.GetCharacterByID(ctx, atoi(vals[0]))
		if err != nil {
			b.reply(i, "That player is gone.")
			return
		}
		self := t.UserID == userOf(i).ID
		b.respond(i, ephemeral(&discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{b.profileEmbed(ctx, t, self)}}))
	case "senator":
		vals := i.MessageComponentData().Values
		c, ok := b.myCharacter(ctx, i)
		if !ok || len(vals) == 0 {
			return
		}
		sen, err := b.store.Senators(ctx, c.GuildID)
		if err != nil {
			b.fail(i, err)
			return
		}
		s, ok := findSenator(sen, atoi(vals[0]))
		if !ok {
			return
		}
		b.respond(i, ephemeral(&discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{b.senatorEmbed(ctx, c.GuildID, s)}}))
	}
}

// ---- the realm ----

func bar(v float64) string {
	n := int(math.Round(v / 10))
	if n < 0 {
		n = 0
	}
	if n > 10 {
		n = 10
	}
	return strings.Repeat("█", n) + strings.Repeat("░", 10-n)
}

func (b *Bot) realmPage(ctx context.Context, guildID string) *discordgo.InteractionResponseData {
	st, err := b.store.GetRealm(ctx, guildID)
	if err != nil {
		return &discordgo.InteractionResponseData{Content: "This court has no realm yet."}
	}
	var trackers strings.Builder
	trackers.WriteString("```\n")
	for _, t := range game.Trackers {
		fmt.Fprintf(&trackers, "%-16s %s %3s\n", t.Name, bar(st.Trackers[t.Key]), game.Num(st.Trackers[t.Key]))
	}
	trackers.WriteString("```")

	status := "The Republic stands."
	color := colorGold
	if !st.Alive() {
		status, color = "**The realm has fallen.** "+st.FallenReason, colorRed
	} else if m, err := b.store.OpenMatter(ctx, guildID); err == nil {
		if ev, ok := game.MatterByKey(m.EventKey); ok {
			status += fmt.Sprintf(" The Senate is voting on **%s**; voting closes <t:%d:R>.", ev.Title, m.ClosesAt.Unix())
		}
	} else {
		status += fmt.Sprintf(" The Senate next meets <t:%d:R>.", st.NextMatterAt.Unix())
	}

	var history strings.Builder
	if ds, err := b.store.RecentDecisions(ctx, guildID, 5); err == nil {
		for _, d := range ds {
			ev, ok := game.MatterByKey(d.EventKey)
			if !ok {
				continue
			}
			mark := "✅"
			if !d.Succeeded {
				mark = "⚠️"
			}
			fmt.Fprintf(&history, "%s **%s**: %s · <t:%d:R>\n", mark, ev.Title, optionLabel(ev, d.Winner), d.ResolvedAt.Unix())
		}
	}
	if history.Len() == 0 {
		history.WriteString("The Senate hasn't decided anything yet.")
	}
	chars, _ := b.store.CharactersInCourt(ctx, guildID)
	sen, _ := b.store.Senators(ctx, guildID)

	var fall []string
	for _, t := range game.Trackers {
		if t.Fall != "" {
			fall = append(fall, t.Name)
		}
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
		Title:       "The Realm",
		Description: status + "\n" + trackers.String(),
		Color:       color,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Recent decisions", Value: history.String()},
			{Name: "The court", Value: fmt.Sprintf("%d players · %d senators", len(chars), len(sen)), Inline: true},
			{Name: "If any of these reach 0, everyone loses", Value: strings.Join(fall, ", "), Inline: true},
		},
	}}}
}

// ---- players ----

func (b *Bot) playersPage(ctx context.Context, guildID string) *discordgo.InteractionResponseData {
	chars, err := b.store.CharactersInCourt(ctx, guildID)
	if err != nil || len(chars) == 0 {
		return &discordgo.InteractionResponseData{Content: "Nobody has joined the court yet."}
	}
	sort.Slice(chars, func(a, c int) bool { return chars[a].VoteWeight() > chars[c].VoteWeight() })
	var sb strings.Builder
	var opts []discordgo.SelectMenuOption
	for n, c := range chars {
		role, _ := game.RoleByKey(c.Role)
		bg, _ := game.BackgroundByKey(c.Background)
		fmt.Fprintf(&sb, "**%d.** %s **%s** · %s\nInfluence %s (%s%% of the Senate) · Renown %s · Estate %s\n\n",
			n+1, c.Crest.Emoji(), c.FullName(), role.Name, game.Num(c.Influence),
			game.Num(b.share(ctx, guildID, c.VoteWeight())), game.Num(c.Renown), game.Num(c.EstateValue()))
		if len(opts) < 25 {
			opts = append(opts, discordgo.SelectMenuOption{Label: truncate(c.FullName(), 100), Value: fmt.Sprint(c.ID),
				Description: truncate(role.Name+" · "+bg.Name, 100)})
		}
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "The Court", Description: truncate(sb.String(), 4000), Color: colorGold}},
		Components: []discordgo.MessageComponent{row(discordgo.SelectMenu{
			MenuType: discordgo.StringSelectMenu, CustomID: "info:profile", Placeholder: "Open a profile…", Options: opts})},
	}
}

// profileEmbed is a player's public profile; private adds stats and gold.
func (b *Bot) profileEmbed(ctx context.Context, c store.Character, private bool) *discordgo.MessageEmbed {
	role, _ := game.RoleByKey(c.Role)
	bg, _ := game.BackgroundByKey(c.Background)
	desc := ""
	if c.Crest.Motto != "" {
		desc = fmt.Sprintf("*\"%s\"*\n\n", c.Crest.Motto)
	}
	desc += fmt.Sprintf("**%s** · %s\n%s\n<@%s>", role.Name, bg.Name, bg.Story, c.UserID)

	var estate []string
	for _, k := range c.Items {
		if it, ok := game.ItemByKey(k); ok {
			icon := "📦"
			if it.Kind == game.KindHome {
				icon = "🏠"
			}
			estate = append(estate, icon+" "+it.Name)
		}
	}
	if len(estate) == 0 {
		estate = []string{"Nothing to speak of."}
	}
	e := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s %s of House %s", c.Crest.Emoji(), c.Name, c.FamilyName),
		Description: desc,
		Color:       colorGold,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Influence", Value: fmt.Sprintf("%s (%s%% of the Senate)", game.Num(c.Influence),
				game.Num(b.share(ctx, c.GuildID, c.VoteWeight()))), Inline: true},
			{Name: "Renown", Value: game.Num(c.Renown), Inline: true},
			{Name: "Estate value", Value: game.Num(c.EstateValue()), Inline: true},
			{Name: "Estate", Value: strings.Join(estate, "\n")},
		},
	}
	if private {
		e.Fields = append(e.Fields,
			&discordgo.MessageEmbedField{Name: "Stats", Value: effectiveStatsBlock(c)},
			&discordgo.MessageEmbedField{Name: "Gold", Value: fmt.Sprint(c.Wealth), Inline: true},
			&discordgo.MessageEmbedField{Name: "Income", Value: fmt.Sprintf("%d/day", c.TotalIncome()), Inline: true},
		)
		e.Footer = &discordgo.MessageEmbedFooter{Text: "Only you can see your stats and gold."}
	}
	return e
}

func effectiveStatsBlock(c store.Character) string {
	bonus := c.Effects().Stats
	notes := map[game.Stat]string{}
	for s, v := range bonus {
		if v != 0 {
			notes[s] = fmt.Sprintf("(%+g from estate)", v)
		}
	}
	return statsBlock(c.Effective(), notes)
}

// ---- your own pages ----

func (b *Bot) characterPage(ctx context.Context, c store.Character, back bool) *discordgo.InteractionResponseData {
	role, _ := game.RoleByKey(c.Role)
	bg, _ := game.BackgroundByKey(c.Background)
	eff := c.Effects()
	weight := game.Num(c.Influence)
	if eff.VoteWeight != 0 {
		weight += fmt.Sprintf(" (+%s vote weight from estate)", game.Num(eff.VoteWeight))
	}
	income := fmt.Sprintf("%d gold/day", c.TotalIncome())
	if eff.DailyIncome != 0 {
		income += fmt.Sprintf(" (%d base + %d from estate)", c.DailyIncome, eff.DailyIncome)
	}
	e := &discordgo.MessageEmbed{
		Title:       c.FullName(),
		Description: fmt.Sprintf("%s · %s · %s", c.House(), role.Name, bg.Name),
		Color:       colorGold,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Stats", Value: effectiveStatsBlock(c)},
			{Name: "Influence", Value: weight, Inline: true},
			{Name: "Share of the Senate", Value: game.Num(b.share(ctx, c.GuildID, c.VoteWeight())) + "%", Inline: true},
			{Name: "Renown", Value: game.Num(c.Renown), Inline: true},
			{Name: "Gold", Value: fmt.Sprint(c.Wealth), Inline: true},
			{Name: "Income", Value: income, Inline: true},
			{Name: "Estate value", Value: game.Num(c.EstateValue()), Inline: true},
		},
	}
	btns := []discordgo.MessageComponent{button("Estate", "menu:estate", discordgo.SecondaryButton, false)}
	if back {
		btns = append(btns, button("Back", "menu:home", discordgo.SecondaryButton, false))
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{row(btns...)}}
}

func (b *Bot) estatePage(c store.Character, back bool) *discordgo.InteractionResponseData {
	e := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%s · the estate of %s", c.House(), c.FullName()),
		Color: colorGold,
	}
	desc := fmt.Sprintf("Estate value **%s**. A finer estate makes your parties more successful.", game.Num(c.EstateValue()))
	if total := c.Effects().Describe(); total != "" {
		desc += "\nAll together: " + total
	}
	e.Description = desc
	for _, k := range c.Items {
		it, ok := game.ItemByKey(k)
		if !ok {
			continue
		}
		icon := "📦"
		if it.Kind == game.KindHome {
			icon = "🏠"
		}
		val := it.Description + fmt.Sprintf("\nValue %s", game.Num(it.Value))
		if fx := it.Effects.Describe(); fx != "" {
			val += " · " + fx
		}
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: icon + " " + it.Name, Value: val})
	}
	btns := []discordgo.MessageComponent{button("My Character", "menu:char", discordgo.SecondaryButton, false)}
	if back {
		btns = append(btns, button("Back", "menu:home", discordgo.SecondaryButton, false))
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{row(btns...)}}
}

// ---- senators ----

func (b *Bot) senatorEmbed(ctx context.Context, guildID string, s game.Senator) *discordgo.MessageEmbed {
	var traits []string
	for _, t := range s.Traits {
		if ti, ok := game.TraitByKey(t); ok {
			traits = append(traits, fmt.Sprintf("**%s**: %s", ti.Name, ti.Desc))
		}
	}
	return &discordgo.MessageEmbed{
		Title:       "Senator " + s.Name,
		Description: strings.Join(traits, "\n"),
		Color:       colorGold,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Influence", Value: game.Num(s.Influence), Inline: true},
			{Name: "Share of the Senate", Value: game.Num(b.share(ctx, guildID, s.Influence)) + "%", Inline: true},
		},
	}
}

// ---- help ----

var helpPages = []struct{ title, body string }{
	{"Welcome to Idle Kings", "You are a member of one of the great families at the court of the Republic. " +
		"Grow your **influence**, your family's **renown** and your **wealth** through parties, deals and votes in the Senate.\n\n" +
		"Most things take real time: a party lasts two hours, and the Senate meets once a day. Start something, then come back later.\n\n" +
		"The realm is shared. If it goes bankrupt, loses its army, collapses into lawlessness or rises up against the Senate, **everyone loses**."},
	{"Your character", "**Stats**: Diplomacy (parties, persuasion), Martial (soldiers and guards), Stewardship (money), Intrigue (rumors and schemes) and Prowess (your own sword arm). They decide your odds whenever you try something. Rolls happen in the background and are never shown.\n\n" +
		"**Influence** is your weight in the Senate. New players barely count; with enough influence, your vote can decide matters.\n\n" +
		"**Renown** is your family's fame. **Wealth** is your gold, and you earn **income** every real day.\n\n" +
		"**Estate**: your home and possessions. They add to your estate value, which helps your parties, and many have small bonuses of their own."},
	{"Actions and parties", "Use `/court` (in the server or in a DM with me) to open your actions menu.\n\n" +
		"**Throw a Party** (Diplomacy): pick a goal, Make Friends or Talk Business. It lasts two hours, everyone is told, and others can attend. " +
		"Something will probably go wrong during the evening; I'll DM you to decide what to do.\n\n" +
		"**Attending** someone's party, you can make friends, talk business, or **spread rumors** about another player. " +
		"Targets and hosts may notice the rumors, and might even learn who started them."},
	{"The Senate", "Every day the Senate votes on a matter in the realm. Vote with the buttons on the announcement, or with `/senate`.\n\n" +
		"Your vote counts in proportion to your influence, against 8 AI senators whose traits decide how they vote. " +
		"You can **bribe** a senator, **sell** your vote to a senator or a player, or offer to **buy** another player's vote. Player deals are accepted in DMs.\n\n" +
		"Decisions change the realm's trackers and can help or hurt players depending on how they voted and their role. Some decisions lead to new matters later."},
	{"Commands", "`/court`: your actions menu\n`/senate`: today's matter and voting\n`/character`: your full sheet\n`/estate`: your home and possessions\n" +
		"`/realm`: the realm's trackers and recent decisions\n`/players`: everyone at court\n`/profile`: a player's profile\n`/join`: create your character\n`/help`: this guide\n\n" +
		"Add `public: True` to `/realm`, `/players` or `/profile` to post it for everyone."},
}

func helpPage(n int) *discordgo.InteractionResponseData {
	if n < 0 {
		n = 0
	}
	if n >= len(helpPages) {
		n = len(helpPages) - 1
	}
	p := helpPages[n]
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: p.title, Description: p.body, Color: colorGold,
			Footer: &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Page %d of %d", n+1, len(helpPages))}}},
		Components: []discordgo.MessageComponent{row(
			button("◀ Previous", fmt.Sprintf("info:help:%d", n-1), discordgo.SecondaryButton, n == 0),
			button("Next ▶", fmt.Sprintf("info:help:%d", n+1), discordgo.SecondaryButton, n == len(helpPages)-1),
		)},
	}
}
