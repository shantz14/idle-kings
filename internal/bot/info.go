package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
		d := &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{
			b.profileEmbed(ctx, target, target.ID == c.ID && !public)}}
		if !public {
			d.Components = profileButtons(target, target.ID == c.ID)
		}
		show(d)
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
		b.respond(i, ephemeral(&discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{b.profileEmbed(ctx, t, self)},
			Components: profileButtons(t, self)}))
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
		ops, _ := b.store.Opinions(ctx, c.ID)
		b.respond(i, ephemeral(&discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{b.senatorEmbed(ctx, c.GuildID, s, ops[s.ID])}}))
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
	// Bars first, in code, so they line up whatever the emoji widths.
	var trackers strings.Builder
	for _, t := range game.Trackers {
		fmt.Fprintf(&trackers, "`%s %3s` %s %s\n", bar(st.Trackers[t.Key]), game.Num(st.Trackers[t.Key]), t.Emoji, t.Name)
	}

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
	chronicle := b.chronicle(ctx, guildID)

	var fall []string
	for _, t := range game.Trackers {
		if t.Fall != "" {
			fall = append(fall, t.Emoji+" "+t.Name)
		}
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
		Title:       "The Realm",
		Description: status + "\n\n" + trackers.String(),
		Color:       color,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Recent decisions", Value: history.String()},
			{Name: "📜 Chronicle", Value: chronicle},
			{Name: "Industries", Value: industriesBlock(st)},
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
	home, _ := c.Residence()
	for _, h := range c.Holdings {
		if it, ok := h.Item(); ok {
			line := game.KindByKey(it.Kind).Emoji + " " + it.Name
			if h.ID == home.ID {
				line = "🏡 " + it.Name + " *(home)*"
			}
			estate = append(estate, line)
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
			{Name: "Estate", Value: joinLines(estate, 1000)},
		},
	}
	if f := venturesField(c, private); f != nil {
		e.Fields = append(e.Fields, f)
	}
	if f := conditionsField(c); f != nil {
		e.Fields = append(e.Fields, f)
	}
	if f := monumentsField(c); f != nil {
		e.Fields = append(e.Fields, f)
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
	estate := game.SumEffects(c.Items).Stats
	conds := game.ConditionEffects(c.Conditions).Stats
	notes := map[game.Stat]string{}
	for _, s := range game.AllStats {
		var parts []string
		if v := estate[s]; v != 0 {
			parts = append(parts, fmt.Sprintf("%+g from estate", v))
		}
		if v := conds[s]; v != 0 {
			parts = append(parts, fmt.Sprintf("%+g from wounds", v))
		}
		if len(parts) > 0 {
			notes[s] = "(" + strings.Join(parts, ", ") + ")"
		}
	}
	return statsBlock(c.Effective(), notes)
}

// conditionsField lists active conditions like wounds, or nil.
func conditionsField(c store.Character) *discordgo.MessageEmbedField {
	var lines []string
	for _, cd := range c.Conditions {
		d, ok := game.ConditionByKey(cd.Key)
		if !ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s **%s** (%s), heals <t:%d:R>", d.Emoji, d.Name, d.Effects.Describe(), cd.EndsAt.Unix()))
	}
	if len(lines) == 0 {
		return nil
	}
	return &discordgo.MessageEmbedField{Name: "Condition", Value: strings.Join(lines, "\n")}
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
	if eff.DailyIncome != 0 || c.VentureIncome > 0 {
		parts := []string{fmt.Sprintf("%d base", c.DailyIncome)}
		if eff.DailyIncome != 0 {
			parts = append(parts, fmt.Sprintf("%d from estate", eff.DailyIncome))
		}
		if c.VentureIncome > 0 {
			parts = append(parts, game.Num(c.VentureIncome)+" from ventures")
		}
		income += " (" + strings.Join(parts, " + ") + ")"
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
	e.Fields = append(e.Fields, ventureSheetField(c), spiesField(b.spyLines(ctx, c)))
	if f := conditionsField(c); f != nil {
		e.Fields = append(e.Fields, f)
	}
	if f := monumentsField(c); f != nil {
		e.Fields = append(e.Fields, f)
	}
	btns := []discordgo.MessageComponent{button("Estate", "menu:estate", discordgo.SecondaryButton, false)}
	if back {
		btns = append(btns, button("Back", "menu:home", discordgo.SecondaryButton, false))
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{row(btns...)}}
}

// ---- senators ----

// op is the senator's opinion of the viewer.
func (b *Bot) senatorEmbed(ctx context.Context, guildID string, s game.Senator, op float64) *discordgo.MessageEmbed {
	var traits []string
	for _, t := range s.Traits {
		if ti, ok := game.TraitByKey(t); ok {
			traits = append(traits, fmt.Sprintf("**%s**: %s", ti.Name, ti.Desc))
		}
	}
	desc := strings.Join(traits, "\n")
	if p, ok := game.PersonaByName(s.Name); ok {
		desc = "*" + p.Bio + "*\n\n" + desc
	}
	return &discordgo.MessageEmbed{
		Title:       "Senator " + s.Name,
		Description: desc,
		Color:       colorGold,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Influence", Value: game.Num(s.Influence), Inline: true},
			{Name: "Share of the Senate", Value: game.Num(b.share(ctx, guildID, s.Influence)) + "%", Inline: true},
			{Name: "Their opinion of you", Value: opinionBlock(op)},
		},
	}
}

// ---- help ----

var helpPages = []struct{ title, body string }{
	{"Welcome to Idle Kings", "You are a member of one of the great families at the court of the Republic. " +
		"Grow your **influence**, your family's **renown** and your **wealth** through parties, deals and votes in the Senate.\n\n" +
		"Most things take real time: a party takes two hours, and the Senate meets once a day. Start something, then come back later.\n\n" +
		"The realm is shared. If it goes bankrupt, loses its army, collapses into lawlessness or rises up against the Senate, **everyone loses**."},
	{"Your character", "**Stats**: Diplomacy (parties, persuasion), Martial (soldiers and guards), Stewardship (money), Intrigue (rumors and schemes) and Prowess (your own sword arm). They decide your odds whenever you try something. Rolls happen in the background and are never shown.\n\n" +
		"**Influence** is your weight in the Senate. New players barely count; with enough influence, your vote can decide matters.\n\n" +
		"**Renown** is your family's fame. **Wealth** is your gold, and you earn **income** every real day.\n\n" +
		"**Estate**: your home and possessions. They add to your estate value, which helps your parties, and many have small bonuses of their own."},
	{"Actions and parties", "Use `/court` (in the server or in a DM with me) to open your actions menu.\n\n" +
		"**Throw a Party** (Diplomacy): pick a goal, Make Friends or Talk Business. It takes two hours, everyone is told, and others can attend. " +
		"Something will probably go wrong during the evening; I'll DM you to decide what to do.\n\n" +
		"**Attending** someone's party, you can make friends, talk business, or **spread rumors** about another player. " +
		"Targets and hosts may notice the rumors, and might even learn who started them.\n\n" +
		"**Court a Senator** (Diplomacy): spend an hour with a senator, flattering them, bringing a gift, sharing secrets or talking strategy. " +
		"Pick the approach that suits their traits. It can raise their opinion of you, or lower it if it goes badly.\n\n" +
		"**Invest in a Venture** (Monetary): put gold into one of the realm's industries, from farms and mines to the Black Market. " +
		"Your gold grows the industry and buys you a share of what it earns every day; how big a share depends on how well you negotiate. " +
		"What the Senate decides moves these industries, and your income with them.\n\n" +
		"**Estate and shop** (`/shop`, or Shop on your estate page): buy houses up to castles, and art, artifacts, war relics, weapons and armor. " +
		"You can own several properties but live in one; that's where you throw parties. Named pieces are one of a kind, and the market changes daily. " +
		"Sell back to the shop for 60% of the price, or deal with other players (Offer a deal, or Make an offer on their profile)."},
	{"Winning influence", "Influence is your weight in the Senate. Besides parties and backing winning votes:\n\n" +
		"**Canvass the Districts** (Diplomacy): three hours of handshakes across the city. Free, and always worth a little influence.\n\n" +
		"**Give a Speech** (Diplomacy): speak for a cause in the Forum for an hour. Others can come to **cheer** (helping you) or **heckle** " +
		"(a battle of wits: win it and the crowd is theirs, lose it and they look the fool). Senators who share your cause warm to a good speaker.\n\n" +
		"**Bread and Circuses** (Monetary): feed and entertain the city. Raises the people's mood and your influence; each coin helps a little less than the last.\n\n" +
		"**Endow a Public Work** (Monetary): pay for a statue, road, temple or aqueduct with your name on it. It takes days to build, " +
		"doesn't stop you doing other things, and once it opens its Senate vote weight is yours for good.\n\n" +
		"The more influence you have, the less canvassing, speeches and circuses add. Public works don't shrink."},
	{"Duels and hunts", "**Challenge to a Duel** (Martial): call out another player, with or without a gold wager they must match. " +
		"They answer by DM; refusing costs them a little renown. If they accept, you fight 30 minutes later and the court can **bet** on it: " +
		"whoever backs the winner splits the whole pot. Midway you each pick a stance (attack beats a trick, a trick beats a guard, a guard beats an attack) or yield. " +
		"Prowess decides most fights. The winner takes renown from the loser, and a bad loss leaves you **wounded**: weaker for a few days.\n\n" +
		"**Hunt in the Royal Forest** (Martial): ride out for two hours to chase glory (renown) or talk in the saddle (influence). " +
		"Others may join to hunt, make friends, or **arrange an accident** for another rider. Victims and hosts may notice, and learn who did it.\n\n" +
		"Only hosting a hunt or issuing a challenge takes up your one action; answering a challenge, riding along and betting don't."},
	{"Spies", "**Plant Spies** (Intrigue): slip an agent into another player's household. Your Intrigue is set against theirs, and gold helps. " +
		"Each success takes your network deeper, from **Low** up to **Extremely High**: someone very close to them. A clumsy attempt may be noticed, " +
		"and a very clumsy one gets your agent caught, and they will give up your name.\n\n" +
		"Your spies may send word when their master does something in private: bribes, vote deals, secret votes, courting, rumors, schemes. " +
		"The deeper the network, the more often they write, and the more they know.\n\n" +
		"**Root Out Spies** (Intrigue): search your own household. Agents you find are dismissed, and you may learn who sent them.\n\n" +
		"Networks fade a little each day. Every action also gives a little practice in the skill it uses."},
	{"The Senate", "Every day the Senate votes on a matter in the realm. Vote with the buttons on the announcement, or with `/senate`.\n\n" +
		"Your vote counts in proportion to your influence, against 8 AI senators whose traits decide how they vote. " +
		"You can **bribe** a senator, **sell** your vote to a senator or a player, or offer to **buy** another player's vote. Player deals are accepted in DMs.\n\n" +
		"Decisions change the realm's trackers and can help or hurt players depending on how they voted and their role. Some decisions lead to new matters later, and some matters are chapters in a longer story: what the Senate decides changes what happens next. `/realm` keeps a **Chronicle** of the stories under way.\n\n" +
		"**Opinion**: every senator has an opinion of you, from -100 to +100. Voting with them, paying them, selling them your vote, good parties and courting raise it; " +
		"voting against them, dull parties and rumors lower it, and it fades a little each day. Senators who like you take smaller bribes, pay more for your vote, " +
		"and may follow your lead on a close vote. Those who hate you won't deal with you at all, and may vote against you out of spite."},
	{"Keeping the realm alive", "If the treasury, the army, order or the people's mood reaches **0**, the realm falls and **everyone loses**.\n\n" +
		"The senators won't vote for anything that could end the realm outright, but they only judge what a vote does today. " +
		"Cheap-now options, like loans and promises, bring **bills that come due** days later, and the hints say so. **Reckless** senators will gamble the realm on a good chance, and can be bribed into it.\n\n" +
		"Some options pay those who vote for them at the realm's expense. Under each option you'll see how it moves the realm's industries (▲ up, ▼ down, *lasting* for a permanent change).\n\n" +
		"**Embezzle from the Treasury** (Intrigue) fills your purse from the realm's. **Aid the Realm** (Monetary) does the opposite, and the court rewards it most when the realm is in trouble. The announcements channel warns when the realm is in danger."},
	{"Commands", "`/court`: your actions menu\n`/senate`: today's matter and voting\n`/character`: your full sheet\n`/estate`: your properties and possessions\n`/shop`: buy houses and treasures\n" +
		"`/realm`: the realm's trackers, industries and recent decisions\n`/players`: everyone at court\n`/profile`: a player's profile\n`/join`: create your character\n`/help`: this guide\n\n" +
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

// industriesBlock lists every sector's earnings and condition.
func industriesBlock(st store.RealmState) string {
	var lines []string
	for _, d := range game.SectorDefs {
		lines = append(lines, fmt.Sprintf("%s %s · %s gold/day · %s", d.Emoji, d.Name,
			game.Num(st.Sectors.Revenue(d, st.Trackers)), st.Sectors.Condition(d, st.Trackers)))
	}
	return strings.Join(lines, "\n")
}

// ventureSheetField is the character sheet's view of your investments:
// always shown, with total income, or a pointer to where to start.
func ventureSheetField(c store.Character) *discordgo.MessageEmbedField {
	f := venturesField(c, true)
	if f == nil {
		return &discordgo.MessageEmbedField{Name: "Ventures", Value: "No investments yet. Monetary → Invest in a Venture."}
	}
	f.Value += fmt.Sprintf("\nAll together: **%s gold/day**", game.Num(c.VentureIncome))
	return f
}

// venturesField lists a character's stakes. Secret sectors and income are
// only shown privately. nil if there's nothing to show.
func venturesField(c store.Character, private bool) *discordgo.MessageEmbedField {
	var lines []string
	for _, d := range game.SectorDefs {
		v, ok := c.Ventures[d.Key]
		if !ok || (d.Secret && !private) {
			continue
		}
		line := fmt.Sprintf("%s %s · owns %s", d.Emoji, d.Name, pct(v.Share))
		if private {
			line += fmt.Sprintf(" · %s gold/day", game.Num(v.Income))
			if d.Secret {
				line += " · *secret*"
			}
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil
	}
	return &discordgo.MessageEmbedField{Name: "Ventures", Value: strings.Join(lines, "\n")}
}

// profileButtons lets you make an estate offer from someone else's profile.
func profileButtons(t store.Character, self bool) []discordgo.MessageComponent {
	if self {
		return nil
	}
	return []discordgo.MessageComponent{row(button("Make an offer", fmt.Sprintf("deal:to:%d", t.ID), discordgo.PrimaryButton, false))}
}

// chronicle is the realm page's account of the stories under way.
func (b *Bot) chronicle(ctx context.Context, guildID string) string {
	ds, err1 := b.store.RecentDecisions(ctx, guildID, 30)
	queued, err2 := b.store.QueuedEvents(ctx, guildID)
	if err := errors.Join(err1, err2); err != nil {
		slog.Error("chronicle", "err", err)
	}
	var decided []game.Decided
	for _, d := range ds {
		decided = append(decided, game.Decided{EventKey: d.EventKey, Winner: d.Winner})
	}
	return chronicleText(game.Chronicle(decided, queued))
}

func chronicleText(entries []game.ChronicleEntry) string {
	if len(entries) == 0 {
		return "No great stories are unfolding in the realm. Yet."
	}
	var lines []string
	for _, e := range entries {
		lines = append(lines, e.Line())
	}
	return joinSep(lines, "\n\n", 1000)
}
