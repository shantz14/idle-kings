package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	componentRoutes["info"] = (*Bot).onInfo
}

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
		viewer := c.ID
		if public { // a public post names nobody in a secret industry
			viewer = 0
		}
		show(b.realmPage(ctx, c.GuildID, viewer))
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
		b.respond(i, ephemeral(&discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{b.senatorEmbed(ctx, c.GuildID, s, ops[s.ID])},
			Components: []discordgo.MessageComponent{row(button("Speak to them", fmt.Sprintf("speak:open:%d", s.ID), discordgo.PrimaryButton, false))}}))
	}
}

// ---- the realm ----

// realmPage is /realm. viewer is who sees it (0 for a public post): only
// they are named in a secret industry's ownership.
func (b *Bot) realmPage(ctx context.Context, guildID string, viewer int64) *discordgo.InteractionResponseData {
	st, err := b.store.GetRealm(ctx, guildID)
	if err != nil {
		return noteView("This court has no realm yet.", colorMuted)
	}
	// Bars first, in code, so they line up whatever the emoji widths.
	var trackers strings.Builder
	for _, t := range game.Trackers {
		fmt.Fprintf(&trackers, "`%s %3s` %s %s\n", bar(st.Trackers[t.Key]), game.Num(st.Trackers[t.Key]), t.Emoji, t.Name)
	}

	status := "The Republic stands."
	color := colorPanel
	if !st.Alive() {
		status, color = "🔥 **The realm has fallen.** "+st.FallenReason, colorBad
	} else if open, _ := b.store.OpenMatters(ctx, guildID); len(open) > 0 {
		for _, m := range open {
			if ev, ok := game.MatterByKey(m.EventKey); ok {
				what := "On the floor"
				if m.PetitionerID != 0 {
					what = "By petition"
				}
				status += fmt.Sprintf("\n%s %s: **%s**, voting closes %s.", emojiVote, what, ev.Title, relTime(m.ClosesAt))
			}
		}
	} else {
		status += fmt.Sprintf("\n%s The Senate next meets %s.", emojiSenate, relTime(st.NextMatterAt))
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
			fmt.Fprintf(&history, "%s **%s**: %s · %s\n", mark, ev.Title, optionLabel(ev, d.Winner), relTime(d.ResolvedAt))
		}
	}
	if history.Len() == 0 {
		history.WriteString("The Senate hasn't decided anything yet.")
	}
	players, _ := b.store.CountCharacters(ctx, guildID)
	sen, _ := b.store.Senators(ctx, guildID)
	chronicle := b.chronicle(ctx, guildID)

	var fall []string
	for _, t := range game.Trackers {
		if t.Fall != "" {
			fall = append(fall, t.Emoji+" "+t.Name)
		}
	}
	e := &discordgo.MessageEmbed{
		Title:       titled(emojiRealm, "The Realm"),
		Description: status + "\n\n" + trackers.String(),
		Color:       color,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "🗳️ Recent decisions", Value: history.String()},
			{Name: "📜 Chronicle", Value: chronicle},
			lawsField(st.Laws),
			{Name: "🏭 Industries", Value: industriesBlock(st)},
		},
	}
	data := &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}}
	if cards, err := b.ownership(ctx, guildID, viewer); err != nil {
		slog.Error("ownership", "err", err)
	} else {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "🥧 Who owns the industries", Value: legendLines(cards)})
		attachChart(data, e, "industries.png", pieGrid(cards))
	}
	e.Fields = append(e.Fields,
		fact("👥 The court", fmt.Sprintf("%d players · %d senators", players, len(sen))),
		fact("💀 At 0, everyone loses", strings.Join(fall, ", ")),
	)
	return data
}

// ---- players ----

func (b *Bot) playersPage(ctx context.Context, guildID string) *discordgo.InteractionResponseData {
	chars, err := b.store.CharactersInCourt(ctx, guildID)
	if err != nil || len(chars) == 0 {
		return noteView("Nobody has joined the court yet.", colorMuted)
	}
	sort.Slice(chars, func(a, c int) bool { return chars[a].VoteWeight() > chars[c].VoteWeight() })
	sen, _ := b.store.Senators(ctx, guildID)
	total := senateTotalOf(sen, chars)
	var sb strings.Builder
	var opts []discordgo.SelectMenuOption
	for n, c := range chars {
		role, _ := game.RoleByKey(c.Role)
		bg, _ := game.BackgroundByKey(c.Background)
		fmt.Fprintf(&sb, "**%d.** %s **%s** · %s\n*Influence %s (%s%% of the Senate) · Renown %s · Estate %s*\n\n",
			n+1, c.Crest.Emoji(), c.FullName(), role.Name, game.Num(c.Influence),
			game.Num(percentOf(c.VoteWeight(), total)), game.Num(c.Renown), game.Num(c.EstateValue()))
		if len(opts) < 25 {
			opts = append(opts, discordgo.SelectMenuOption{Label: truncate(c.FullName(), 100), Value: fmt.Sprint(c.ID),
				Description: truncate(role.Name+" · "+bg.Name, 100)})
		}
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: titled(emojiCourt, "The Court"), Description: truncate(sb.String(), 4000), Color: colorPanel,
			Footer: footer("Ranked by weight in the Senate.")}},
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
	desc += fmt.Sprintf("**%s** · %s · <@%s>\n*%s*", role.Name, bg.Name, c.UserID, bg.Story)

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
		Color:       colorPanel,
		Fields: []*discordgo.MessageEmbedField{
			fact("Influence", fmt.Sprintf("%s (%s%% of the Senate)", game.Num(c.Influence),
				game.Num(b.share(ctx, c.GuildID, c.VoteWeight())))),
			fact("Renown", game.Num(c.Renown)),
			fact("Estate value", game.Num(c.EstateValue())),
			{Name: emojiEstate + " Estate", Value: joinLines(estate, 1000)},
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
			&discordgo.MessageEmbedField{Name: "📊 Stats", Value: effectiveStatsBlock(c)},
			fact("Gold", fmt.Sprint(c.Wealth)),
			fact("Income", fmt.Sprintf("%d/day", c.TotalIncome())),
		)
		e.Footer = footer("🔒 Only you can see your stats and gold.")
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
	return &discordgo.MessageEmbedField{Name: "🩹 Condition", Value: strings.Join(lines, "\n")}
}

// ---- your own pages ----

func (b *Bot) characterPage(ctx context.Context, c store.Character, back bool) *discordgo.InteractionResponseData {
	role, _ := game.RoleByKey(c.Role)
	bg, _ := game.BackgroundByKey(c.Background)
	eff := c.Effects()
	weight := game.Num(c.Influence)
	if eff.VoteWeight != 0 {
		weight += fmt.Sprintf("\n*+%s vote weight from estate*", game.Num(eff.VoteWeight))
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
		income += "\n*" + strings.Join(parts, " + ") + "*"
	}
	e := &discordgo.MessageEmbed{
		Title:       c.Crest.Emoji() + " " + c.FullName(),
		Description: fmt.Sprintf("%s · %s · %s", c.House(), role.Name, bg.Name),
		Color:       colorPanel,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "📊 Stats", Value: effectiveStatsBlock(c)},
			fact("Influence", weight),
			fact("Share of the Senate", game.Num(b.share(ctx, c.GuildID, c.VoteWeight()))+"%"),
			fact("Renown", game.Num(c.Renown)),
			fact("Gold", fmt.Sprint(c.Wealth)),
			fact("Income", income),
			fact("Estate value", game.Num(c.EstateValue())),
		},
		Footer: footer("🔒 Only you can see this sheet."),
	}
	e.Fields = append(e.Fields, ventureSheetField(c), spiesField(b.spyLines(ctx, c)))
	if dos, err := b.store.Dossier(ctx, c.ID); err == nil && len(dos) > 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "📁 Dossier",
			Value: fmt.Sprintf("%d report%s on your rivals, ready to use. *Intrigue → Your Dossier*", len(dos), plural(len(dos)))})
	}
	if f := conditionsField(c); f != nil {
		e.Fields = append(e.Fields, f)
	}
	if f := monumentsField(c); f != nil {
		e.Fields = append(e.Fields, f)
	}
	btns := []discordgo.MessageComponent{withEmoji(button("Estate", "menu:estate", discordgo.SecondaryButton, false), emojiEstate)}
	if back {
		btns = append(btns, backTo("menu:home"))
	}
	data := &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{row(btns...)}}
	if len(c.Ventures) > 0 { // the industries you're in, and who you share them with
		if cards, err := b.ownership(ctx, c.GuildID, c.ID); err != nil {
			slog.Error("ownership", "err", err)
		} else {
			attachChart(data, e, "ventures.png", pieGrid(ownedCards(cards)))
		}
	}
	return data
}

// ---- senators ----

// op is the senator's opinion of the viewer.
func (b *Bot) senatorEmbed(ctx context.Context, guildID string, s game.Senator, op float64) *discordgo.MessageEmbed {
	var traits []string
	for _, t := range s.Traits {
		if ti, ok := game.TraitByKey(t); ok {
			traits = append(traits, fmt.Sprintf("• **%s**: %s", ti.Name, ti.Desc))
		}
	}
	desc := strings.Join(traits, "\n")
	if p, ok := game.PersonaByName(s.Name); ok {
		desc = "*" + p.Bio + "*\n\n" + desc
	}
	return &discordgo.MessageEmbed{
		Title:       titled(emojiSenate, "Senator "+s.Name),
		Description: desc,
		Color:       colorSenate,
		Fields: []*discordgo.MessageEmbedField{
			fact("Influence", game.Num(s.Influence)),
			fact("Share of the Senate", game.Num(b.share(ctx, guildID, s.Influence))+"%"),
			{Name: "Their opinion of you", Value: opinionBlock(op)},
		},
	}
}

// ---- help ----

var helpPages = []struct{ title, body string }{
	{"Welcome to Idle Kings", "You belong to one of the great families at the court of the Republic. " +
		"Grow your **influence**, your family's **renown** and your **gold** through parties, deals and votes in the Senate.\n\n" +
		"⏳ Most things take real time: a party lasts two hours, and the Senate meets once a day. Start something, then come back later.\n\n" +
		"⚠️ The realm is shared. If it goes bankrupt, loses its army, falls into lawlessness or rises against the Senate, **everyone loses**."},
	{"Your character", "**Stats** decide your odds whenever you try something. Rolls happen in the background and are never shown.\n" +
		"• **Diplomacy**: parties and persuasion\n• **Martial**: soldiers and guards\n• **Stewardship**: money\n" +
		"• **Intrigue**: rumors and schemes\n• **Prowess**: your own sword arm\n\n" +
		"**Influence** is your weight in the Senate. Newcomers barely count; with enough, your vote decides matters.\n" +
		"**Renown** is your family's fame. **Gold** comes in as **income** every real day.\n" +
		"**Estate** is your home and possessions. Its value helps your parties, and many pieces have small bonuses of their own."},
	{"Actions and parties", "Open your actions with `/court`, in the server or in a DM with me.\n\n" +
		"🎭 **Throw a Party** (Diplomacy): pick a goal, Make Friends or Talk Business. Two hours, announced to the court, and others can attend. " +
		"Something usually goes wrong during the evening; I'll DM you to decide what to do.\n\n" +
		"🥂 **Attending** a party, you can make friends, talk business, or **spread rumors** about another player. Targets and hosts may notice, and learn who started them.\n\n" +
		"🎩 **Speak to a Senator** (Diplomacy): call on a senator. **Ask how they'll vote** on what's before the Senate (once per matter): depending on what they think of you, your Diplomacy and their character, they may tell you, refuse, or lie. " +
		"You can bribe them or sell them your vote from there too, or **court** them: an hour of flattery, gifts, secrets or strategy. Pick the approach that suits their traits; their opinion of you can rise, or fall.\n\n" +
		"📈 **Invest in a Venture** (Monetary): buy a share of one of the realm's industries, from farms to the Black Market, and earn from it every day. " +
		"What the Senate decides moves these industries, and your income with them.\n\n" +
		"🛒 **Estate and shop** (`/shop`): houses up to castles, art, relics, weapons and armor. Own several properties but live in one; that's where you host. " +
		"Named pieces are one of a kind, and the market changes daily. Sell back for 60% of the price, or trade with players (*Offer a deal*, or *Make an offer* on a profile)."},
	{"Winning influence", "Besides parties and backing winning votes:\n\n" +
		"🗺️ **Canvass the Districts** (Diplomacy): three hours of handshakes. Free, and always worth a little.\n\n" +
		"📢 **Give a Speech** (Diplomacy): an hour in the Forum for a cause. Others come to **cheer** (helping you) or **heckle** " +
		"(a battle of wits: the winner takes the crowd). Senators who share your cause warm to a good speaker.\n\n" +
		"🎪 **Bread and Circuses** (Monetary): feed and entertain the city. Lifts the people's mood and your influence; each coin helps a little less than the last.\n\n" +
		"🏛️ **Endow a Public Work** (Monetary): a statue, road, temple or aqueduct with your name on it. It takes days to build, " +
		"doesn't stop you doing other things, and it gives you lasting influence.\n\n" +
		"*The more influence you have, the less canvassing, speeches and circuses add. Public works don't shrink.*"},
	{"Duels and hunts", "⚔️ **Challenge to a Duel** (Martial): call out a player, with or without a gold wager they must match. " +
		"They answer by DM; refusing costs a little renown. If they accept, you fight 30 minutes later and the court can **bet**: " +
		"backers of the winner split the pot.\n" +
		"Midway you each pick a stance or yield: **attack** beats a trick, a **trick** beats a guard, a **guard** beats an attack. " +
		"Prowess decides most fights. The winner takes renown from the loser, and a bad loss leaves you **wounded** for a few days.\n\n" +
		"🦌 **Hunt in the Royal Forest** (Martial): two hours chasing glory (renown) or talking in the saddle (influence). " +
		"Others may join to hunt, make friends, or **arrange an accident** for another rider. Victims and hosts may notice, and learn who did it.\n\n" +
		"Only hosting a hunt or issuing a challenge uses your one action. Answering a challenge, riding along and betting don't."},
	{"Spies", "🕵️ **Plant Spies** (Intrigue): slip an agent into another player's household. Your Intrigue is set against theirs, and gold helps. " +
		"Each success takes your network deeper, from **Low** to **Extremely High**. A clumsy attempt may be noticed; a very clumsy agent is caught and gives up your name.\n\n" +
		"Your spies send word when their target does something in private: bribes, vote deals, secret votes, courting, rumors, schemes. " +
		"The deeper the network, the more often they write and the more they know.\n\n" +
		"📁 **Your Dossier**: shameful secrets your spies catch are filed here. **Denounce** them publicly, or **whisper** them quietly, whenever you like.\n\n" +
		"🔍 **Root Out Spies** (Intrigue): search your own household. Agents you find are dismissed, and you may learn who sent them.\n\n" +
		"*Networks fade a little each day. Every action also gives a little practice in the skill it uses.*"},
	{"The Senate", "🏛️ Every day the Senate votes on a matter. Vote with the buttons on the announcement, or with `/senate`.\n\n" +
		"Your vote counts in proportion to your influence, against 8 senators whose traits decide how they vote. " +
		"You can **bribe** a senator, **sell** your vote to a senator or a player, or **buy** another player's vote. Player deals are answered in DMs.\n\n" +
		"Decisions move the realm's trackers and can help or hurt players by how they voted and their role. Some lead to new matters later, " +
		"and some are chapters in a longer story; `/realm` keeps a **Chronicle** of them.\n\n" +
		fmt.Sprintf("⚖️ **Laws**: some decisions pass a lasting law that changes the rules until it's repealed. At most %d stand at once: when another passes, the oldest lapses, and you're warned which before you vote. Some laws quietly cost the realm every day. ", game.MaxLaws) +
		"**Petition the Senate** to put a law, or its repeal, to a vote yourself. It always reaches the floor, alongside the day's matter.\n\n" +
		"💬 **Opinion**: every senator thinks of you somewhere from -100 to +100. Voting with them, paying them, good parties and courting raise it; " +
		"voting against them, dull parties and rumors lower it, and it fades a little each day. Senators who like you take smaller bribes, pay more for your vote " +
		"and may follow your lead on a close vote. Those who hate you won't deal with you, and may vote against you out of spite."},
	{"Keeping the realm alive", "💀 If the treasury, the army, order or the people's mood reaches **0**, the realm falls and **everyone loses**.\n\n" +
		"Senators won't vote for anything that could end the realm outright, but they only judge what a vote does today. " +
		"Cheap-now options, like loans and promises, bring **bills that come due** days later, and the hints say so. **Reckless** senators will gamble the realm on a good chance, and can be bribed into it.\n\n" +
		"Some options pay those who vote for them at the realm's expense. Under each option you'll see how it moves the industries (▲ up, ▼ down, *lasting* for a permanent change).\n\n" +
		"🪙 **Embezzle from the Treasury** (Intrigue) fills your purse from the realm's. 🤲 **Aid the Realm** (Monetary) does the opposite, and the court rewards it most when the realm is in trouble.\n\n" +
		"*The announcements channel warns when the realm is in danger.*"},
	{"Commands", "`/court` · your actions menu\n`/senate` · today's matter and voting\n`/character` · your full sheet\n`/estate` · your properties and possessions\n`/shop` · buy houses and treasures\n" +
		"`/realm` · trackers, industries and recent decisions\n`/players` · everyone at court\n`/profile` · a player's profile\n`/join` · create your character\n`/help` · this guide\n\n" +
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
		Embeds: []*discordgo.MessageEmbed{{Title: titled(emojiHelp, p.title), Description: p.body, Color: colorPanel,
			Footer: footer(fmt.Sprintf("Page %d of %d · %s", n+1, len(helpPages), pageDots(n, len(helpPages))))}},
		Components: []discordgo.MessageComponent{row(
			button("◀ Previous", fmt.Sprintf("info:help:%d", n-1), discordgo.SecondaryButton, n == 0),
			button("Next ▶", fmt.Sprintf("info:help:%d", n+1), discordgo.SecondaryButton, n == len(helpPages)-1),
		)},
	}
}

// pageDots is "● ○ ○" with the current page filled.
func pageDots(n, total int) string {
	dots := make([]string, total)
	for i := range dots {
		dots[i] = "○"
	}
	dots[n] = "●"
	return strings.Join(dots, " ")
}

// industriesBlock lists every sector's earnings and condition.
func industriesBlock(st store.RealmState) string {
	var lines []string
	for _, d := range game.SectorDefs {
		lines = append(lines, fmt.Sprintf("%s %s · %s gold/day · %s", d.Emoji, d.Name,
			game.Num(st.Sectors.Revenue(d, st.Trackers, st.Laws)), st.Sectors.Condition(d, st.Trackers, st.Laws)))
	}
	return strings.Join(lines, "\n")
}

// ventureSheetField is the character sheet's view of your investments:
// always shown, with total income, or a pointer to where to start.
func ventureSheetField(c store.Character) *discordgo.MessageEmbedField {
	f := venturesField(c, true)
	if f == nil {
		return &discordgo.MessageEmbedField{Name: "📈 Ventures", Value: "No investments yet. *Monetary → Invest in a Venture*"}
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
	return &discordgo.MessageEmbedField{Name: "📈 Ventures", Value: strings.Join(lines, "\n")}
}

// profileButtons lets you make an estate offer from someone else's profile.
func profileButtons(t store.Character, self bool) []discordgo.MessageComponent {
	if self {
		return nil
	}
	return []discordgo.MessageComponent{row(withEmoji(button("Make an offer", fmt.Sprintf("deal:to:%d", t.ID), discordgo.PrimaryButton, false), emojiDeal))}
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
