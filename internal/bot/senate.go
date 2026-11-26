package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Senate custom IDs:
//   senate:vote:<matter>:<option>
//   senate:bribe:<matter>                     pick a senator to bribe
//   senate:bribewho:<matter>                  (select) senator chosen
//   senate:pay:<matter>:<senator>:<option>    pay the bribe
//   senate:roster                             who sits in the Senate

func (b *Bot) onSenate(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	switch {
	case parts[0] == "roster":
		b.onRoster(ctx, i)
	case parts[0] == "vote" && len(parts) > 2:
		b.onVote(ctx, i, atoi(parts[1]), parts[2])
	case parts[0] == "bribe" && len(parts) > 1:
		b.onBribe(ctx, i, atoi(parts[1]))
	case parts[0] == "bribewho" && len(parts) > 1:
		if vals := i.MessageComponentData().Values; len(vals) > 0 {
			b.onBribeWho(ctx, i, atoi(parts[1]), atoi(vals[0]))
		}
	case parts[0] == "pay" && len(parts) > 3:
		b.onPay(ctx, i, atoi(parts[1]), atoi(parts[2]), parts[3])
	default:
		b.onVoteTrade(ctx, i, parts)
	}
}

// ---- the daily cycle ----

// ensureRealm gives a running court its realm and senate.
func (b *Bot) ensureRealm(ctx context.Context, guildID string, now time.Time) (store.RealmState, error) {
	st, err := b.store.GetRealm(ctx, guildID)
	if !errors.Is(err, store.ErrNotFound) {
		return st, err
	}
	senate := game.GenerateSenate(b.rng, game.SenateSize)
	if err := b.store.CreateRealm(ctx, guildID, game.StartingRealm(), senate, now.Add(b.senate.FirstDelay)); err != nil {
		return st, err
	}
	return b.store.GetRealm(ctx, guildID)
}

func (b *Bot) senateTick(ctx context.Context, now time.Time) {
	courts, err := b.store.RunningCourts(ctx)
	if err != nil {
		slog.Error("running courts", "err", err)
		return
	}
	for _, court := range courts {
		st, err := b.ensureRealm(ctx, court.GuildID, now)
		if err != nil {
			slog.Error("ensure realm", "guild", court.GuildID, "err", err)
			continue
		}
		if !st.Alive() || now.Before(st.NextMatterAt) {
			continue
		}
		if _, err := b.store.OpenMatter(ctx, court.GuildID); err == nil {
			continue // still voting on the last one
		}
		b.openMatter(ctx, court, st, now)
	}

	closed, err := b.store.ClosedMatters(ctx, now)
	if err != nil {
		slog.Error("closed matters", "err", err)
		return
	}
	for _, m := range closed {
		b.resolveMatter(ctx, m, now)
	}
}

func (b *Bot) openMatter(ctx context.Context, court store.Court, st store.RealmState, now time.Time) {
	var ev game.MatterEvent
	qid, key, err := b.store.NextQueued(ctx, court.GuildID, now)
	if err == nil {
		ev, _ = game.MatterByKey(key)
	} else {
		qid = 0
	}
	if ev.Key == "" {
		ev = game.PickMatter(b.rng, st.Trackers, st.Recent)
	}
	recent := append(st.Recent, ev.Key)
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	m, err := b.store.CreateMatter(ctx, court.GuildID, ev.Key, qid, now, now.Add(b.senate.VoteWindow), now.Add(b.senate.Interval), recent)
	if err != nil {
		slog.Error("create matter", "err", err)
		return
	}
	if court.AnnouncementsChannelID == "" {
		return
	}
	msg, err := b.s.ChannelMessageSendComplex(court.AnnouncementsChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{matterEmbed(m, ev, st.Trackers)},
		Components: matterButtons(m, ev, false),
	})
	if err != nil {
		slog.Warn("announce matter", "err", err)
		return
	}
	_ = b.store.SetMatterMessage(ctx, m.ID, msg.ChannelID, msg.ID)
}

func (b *Bot) resolveMatter(ctx context.Context, m store.Matter, now time.Time) {
	ev, ok := game.MatterByKey(m.EventKey)
	st, err := b.store.GetRealm(ctx, m.GuildID)
	if err != nil {
		slog.Error("realm", "err", err)
		return
	}
	if !ok { // event removed from the catalogue; close it with no effect
		_ = b.store.ResolveMatter(ctx, m, store.MatterResolution{Realm: st.Trackers}, now)
		return
	}
	senators, err1 := b.store.Senators(ctx, m.GuildID)
	bribes, err2 := b.store.Bribes(ctx, m.ID)
	votes, err3 := b.store.Votes(ctx, m.ID)
	chars, err4 := b.store.CharactersInCourt(ctx, m.GuildID)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		slog.Error("load matter", "err", err)
		return
	}

	in := game.MatterInput{MatterID: m.ID, Event: ev, Realm: st.Trackers, Senators: senators, Bribes: map[int64]string{}}
	for sid, br := range bribes {
		in.Bribes[sid] = br.Option
	}
	users := map[int64]string{}
	for _, c := range chars {
		users[c.ID] = c.UserID
		in.Players = append(in.Players, game.Player{Person: c.Person(), Role: c.Role, Vote: votes[c.ID], Weight: c.VoteWeight()})
	}
	res := game.ResolveMatter(b.rng, in)

	err = b.store.ResolveMatter(ctx, m, store.MatterResolution{
		Summary: map[string]any{"winner": res.Winner.Key, "succeeded": res.Succeeded, "tally": res.Tally},
		Realm:   res.Realm, Fallen: res.Fallen, FallenReason: res.FallenReason,
		Deltas: res.Deltas, FollowUps: res.FollowUps, DayLength: b.senate.Interval,
	}, now)
	if err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("resolve matter", "err", err)
		}
		return
	}

	for id, lines := range res.Notices {
		if uid, ok := users[id]; ok {
			_, _ = b.dm(uid, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
				Title: "The Senate: " + ev.Title, Description: strings.Join(lines, "\n"), Color: colorGold,
			}}})
		}
	}

	channel := m.ChannelID
	if channel == "" {
		if court, err := b.store.GetCourt(ctx, m.GuildID); err == nil {
			channel = court.AnnouncementsChannelID
		}
	}
	if channel == "" {
		return
	}
	result := decisionEmbed(ev, res)
	_, _ = b.s.ChannelMessageSendEmbed(channel, result)
	if m.MessageID != "" {
		closed := matterEmbed(m, ev, st.Trackers)
		closed.Fields = []*discordgo.MessageEmbedField{{Name: "Decided", Value: res.Winner.Label}}
		embeds := []*discordgo.MessageEmbed{closed}
		empty := []discordgo.MessageComponent{}
		_, _ = b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: m.ChannelID, ID: m.MessageID, Embeds: &embeds, Components: &empty})
	}
	if res.Fallen {
		fall := &discordgo.MessageEmbed{
			Title:       "The realm has fallen",
			Description: res.FallenReason + "\n\n**All players have lost.**",
			Color:       colorRed,
		}
		_, _ = b.s.ChannelMessageSendEmbed(channel, fall)
		for _, uid := range users {
			_, _ = b.dm(uid, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{fall}})
		}
	}
}

// ---- panels ----

func trackersLine(r game.Realm) string {
	var parts []string
	for _, t := range game.Trackers {
		parts = append(parts, fmt.Sprintf("%s **%s**", t.Name, game.Num(r[t.Key])))
	}
	return strings.Join(parts, " · ")
}

func matterEmbed(m store.Matter, ev game.MatterEvent, r game.Realm) *discordgo.MessageEmbed {
	var sb strings.Builder
	sb.WriteString(ev.Prompt + "\n")
	for _, o := range ev.Options {
		fmt.Fprintf(&sb, "\n**%s**: %s", o.Label, o.Hint)
	}
	return &discordgo.MessageEmbed{
		Title:       "The Senate convenes: " + ev.Title,
		Description: sb.String(),
		Color:       colorGold,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "The realm", Value: trackersLine(r)},
			{Name: "Voting closes", Value: fmt.Sprintf("<t:%d:R>. Your vote counts in proportion to your influence. Ties go to *%s*.",
				m.ClosesAt.Unix(), optionLabel(ev, ev.Default))},
		},
	}
}

func optionLabel(ev game.MatterEvent, key string) string {
	if o, ok := ev.Option(key); ok {
		return o.Label
	}
	return key
}

func matterButtons(m store.Matter, ev game.MatterEvent, back bool) []discordgo.MessageComponent {
	var votes []discordgo.MessageComponent
	for _, o := range ev.Options {
		votes = append(votes, button(o.Label, fmt.Sprintf("senate:vote:%d:%s", m.ID, o.Key), discordgo.PrimaryButton, false))
	}
	extra := []discordgo.MessageComponent{
		button("Bribe a senator", fmt.Sprintf("senate:bribe:%d", m.ID), discordgo.SecondaryButton, false),
		button("Sell your vote", fmt.Sprintf("senate:sell:%d", m.ID), discordgo.SecondaryButton, false),
		button("Buy a vote", fmt.Sprintf("senate:buy:%d", m.ID), discordgo.SecondaryButton, false),
		button("The Senate", "senate:roster", discordgo.SecondaryButton, false),
	}
	if back {
		extra = append(extra, button("Back", "menu:home", discordgo.SecondaryButton, false))
	}
	return []discordgo.MessageComponent{row(votes...), row(extra...)}
}

func decisionEmbed(ev game.MatterEvent, res game.MatterResult) *discordgo.MessageEmbed {
	desc := fmt.Sprintf("**%s**\n%s", res.Winner.Label, res.Outcome.Text)
	if s := res.Outcome.Realm.Describe(); s != "" {
		desc += "\n\n" + s
	}
	var tally strings.Builder
	for _, o := range ev.Options {
		var names []string
		for _, v := range res.Votes {
			if v.Option == o.Key {
				names = append(names, v.Name)
			}
		}
		if len(names) == 0 {
			names = []string{"nobody"}
		}
		fmt.Fprintf(&tally, "**%s** (%s): %s\n", o.Label, game.Num(res.Tally[o.Key]), strings.Join(names, ", "))
	}
	color := colorGreen
	if !res.Succeeded {
		color = colorRed
	}
	return &discordgo.MessageEmbed{
		Title:       "The Senate has decided: " + ev.Title,
		Description: desc,
		Color:       color,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Votes (by influence)", Value: tally.String()},
			{Name: "The realm", Value: trackersLine(res.Realm)},
		},
	}
}

// senatePanel is the /senate view: the realm, and today's matter if any.
func (b *Bot) senatePanel(ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
	st, err := b.ensureRealm(ctx, c.GuildID, time.Now())
	if err != nil {
		return ephemeral(&discordgo.InteractionResponseData{Content: "Something went wrong."})
	}
	realm := &discordgo.MessageEmbed{Title: "The realm", Description: trackersLine(st.Trackers), Color: colorGold}
	if !st.Alive() {
		realm.Description += "\n\n**The realm has fallen.** " + st.FallenReason
		realm.Color = colorRed
	}
	embeds := []*discordgo.MessageEmbed{realm}
	var comps []discordgo.MessageComponent
	if m, err := b.store.OpenMatter(ctx, c.GuildID); err == nil {
		if ev, ok := game.MatterByKey(m.EventKey); ok {
			embeds = append(embeds, matterEmbed(m, ev, st.Trackers))
			comps = matterButtons(m, ev, true)
		}
	} else if st.Alive() {
		realm.Description += fmt.Sprintf("\n\nThe Senate next convenes <t:%d:R>.", st.NextMatterAt.Unix())
	}
	if comps == nil {
		comps = []discordgo.MessageComponent{row(
			button("The Senate", "senate:roster", discordgo.SecondaryButton, false),
			button("Back", "menu:home", discordgo.SecondaryButton, false),
		)}
	}
	return ephemeral(&discordgo.InteractionResponseData{Embeds: embeds, Components: comps})
}

// share is a vote weight's portion of the whole Senate (senators plus
// players), in percent.
func (b *Bot) share(ctx context.Context, guildID string, influence float64) float64 {
	total := 0.0
	if sen, err := b.store.Senators(ctx, guildID); err == nil {
		for _, s := range sen {
			total += s.Influence
		}
	}
	if chars, err := b.store.CharactersInCourt(ctx, guildID); err == nil {
		for _, c := range chars {
			total += c.VoteWeight()
		}
	}
	if total == 0 {
		return 0
	}
	return 100 * influence / total
}

func (b *Bot) onRoster(ctx context.Context, i *discordgo.InteractionCreate) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	sen, err := b.store.Senators(ctx, c.GuildID)
	if err != nil {
		b.fail(i, err)
		return
	}
	chars, _ := b.store.CharactersInCourt(ctx, c.GuildID)
	sort.Slice(chars, func(a, b int) bool { return chars[a].VoteWeight() > chars[b].VoteWeight() })

	var sb strings.Builder
	sb.WriteString("**Senators**\n")
	for _, s := range sen {
		fmt.Fprintf(&sb, "**%s**: %s · influence %s (%s%%)\n", s.Name, s.TraitNames(), game.Num(s.Influence),
			game.Num(b.share(ctx, c.GuildID, s.Influence)))
	}
	sb.WriteString("\n**Players**\n")
	for _, p := range chars {
		role, _ := game.RoleByKey(p.Role)
		fmt.Fprintf(&sb, "%s **%s** (%s): influence %s (%s%%)\n", p.Crest.Emoji(), p.FullName(), role.Name, game.Num(p.Influence),
			game.Num(b.share(ctx, c.GuildID, p.VoteWeight())))
	}
	var traits strings.Builder
	seen := map[game.Trait]bool{}
	for _, s := range sen {
		for _, t := range s.Traits {
			if !seen[t] {
				seen[t] = true
				ti, _ := game.TraitByKey(t)
				fmt.Fprintf(&traits, "**%s**: %s\n", ti.Name, ti.Desc)
			}
		}
	}
	var opts []discordgo.SelectMenuOption
	for _, s := range sen {
		opts = append(opts, discordgo.SelectMenuOption{Label: s.Name, Value: fmt.Sprint(s.ID),
			Description: truncate(s.TraitNames()+" · influence "+game.Num(s.Influence), 100)})
	}
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "The Senate", Description: sb.String(), Color: colorGold,
			Fields: []*discordgo.MessageEmbedField{{Name: "Traits", Value: traits.String()}},
		}},
		Components: []discordgo.MessageComponent{row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu,
			CustomID: "info:senator", Placeholder: "Learn more about a senator…", Options: opts})},
	}))
}

// ---- voting ----

// matterFor loads an open matter and the caller's character in its court.
func (b *Bot) matterFor(ctx context.Context, i *discordgo.InteractionCreate, matterID int64) (store.Matter, game.MatterEvent, store.Character, string) {
	m, err := b.store.GetMatter(ctx, matterID)
	if err != nil {
		return m, game.MatterEvent{}, store.Character{}, "That matter doesn't exist."
	}
	ev, ok := game.MatterByKey(m.EventKey)
	if !ok {
		return m, ev, store.Character{}, "That matter no longer exists."
	}
	c, err := b.store.GetCharacter(ctx, m.GuildID, userOf(i).ID)
	if err != nil {
		return m, ev, c, "You have no place at this court yet."
	}
	if m.Resolved || !time.Now().Before(m.ClosesAt) {
		return m, ev, c, "Voting on this matter has closed."
	}
	return m, ev, c, ""
}

func (b *Bot) onVote(ctx context.Context, i *discordgo.InteractionCreate, matterID int64, opt string) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	o, ok := ev.Option(opt)
	if !ok {
		b.reply(i, "Unknown option.")
		return
	}
	if err := b.store.CastVote(ctx, m.ID, c.ID, opt, time.Now()); errors.Is(err, store.ErrEnded) {
		b.reply(i, "Voting on this matter has closed.")
		return
	} else if errors.Is(err, store.ErrVoteSold) {
		sold, _, _ := b.store.SoldVote(ctx, m.ID, c.ID)
		b.reply(i, fmt.Sprintf("You sold your vote on this matter. You will vote to **%s**.", strings.ToLower(optionLabel(ev, sold))))
		return
	} else if err != nil {
		b.fail(i, err)
		return
	}
	b.reply(i, fmt.Sprintf("You voted to **%s**. Your voice carries **%s%%** of the Senate. You can change your vote until <t:%d:R>.",
		strings.ToLower(o.Label), game.Num(b.share(ctx, m.GuildID, c.VoteWeight())), m.ClosesAt.Unix()))
}

// ---- bribes ----

func (b *Bot) senatorSelect(ctx context.Context, m store.Matter, senators []game.Senator, bribes map[int64]store.Bribe) discordgo.ActionsRow {
	var opts []discordgo.SelectMenuOption
	for _, s := range senators {
		desc := fmt.Sprintf("%s · influence %s", s.TraitNames(), game.Num(s.Influence))
		if _, paid := bribes[s.ID]; paid {
			desc += " · already paid"
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: s.Name, Value: fmt.Sprint(s.ID), Description: desc})
	}
	return row(discordgo.SelectMenu{
		MenuType: discordgo.StringSelectMenu, CustomID: fmt.Sprintf("senate:bribewho:%d", m.ID),
		Placeholder: "Choose a senator…", Options: opts,
	})
}

func (b *Bot) onBribe(ctx context.Context, i *discordgo.InteractionCreate, matterID int64) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	bribes, err2 := b.store.Bribes(ctx, m.ID)
	if err := errors.Join(err1, err2); err != nil {
		b.fail(i, err)
		return
	}
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "Bribe a senator: " + ev.Title,
			Description: fmt.Sprintf("Pay a senator to vote your way. Each senator can be bought once per matter. "+
				"The less they like your option, the more it costs.\n\nYou have **%d gold**.", c.Wealth),
			Color: colorGold,
		}},
		Components: []discordgo.MessageComponent{b.senatorSelect(ctx, m, sen, bribes)},
	}))
}

func findSenator(sen []game.Senator, id int64) (game.Senator, bool) {
	for _, s := range sen {
		if s.ID == id {
			return s, true
		}
	}
	return game.Senator{}, false
}

func (b *Bot) onBribeWho(ctx context.Context, i *discordgo.InteractionCreate, matterID, senatorID int64) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.update(i, &discordgo.InteractionResponseData{Content: msg, Embeds: []*discordgo.MessageEmbed{}})
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	bribes, err2 := b.store.Bribes(ctx, m.ID)
	st, err3 := b.store.GetRealm(ctx, m.GuildID)
	if err := errors.Join(err1, err2, err3); err != nil {
		b.fail(i, err)
		return
	}
	s, ok := findSenator(sen, senatorID)
	if !ok {
		return
	}

	desc := fmt.Sprintf("%s · influence %s\n\n", s.TraitNames(), game.Num(s.Influence))
	var btns []discordgo.MessageComponent
	if br, paid := bribes[s.ID]; paid {
		if br.CharacterID == c.ID {
			desc += fmt.Sprintf("You've already paid them to vote to **%s**.", strings.ToLower(optionLabel(ev, br.Option)))
		} else {
			desc += "Someone has already paid them for this matter."
		}
	} else if _, can := s.BribePrice(ev, ev.Default, st.Trackers, m.ID); !can {
		desc += "They cannot be bought."
	} else {
		desc += fmt.Sprintf("You have **%d gold**. Their price:", c.Wealth)
		for _, o := range ev.Options {
			price, _ := s.BribePrice(ev, o.Key, st.Trackers, m.ID)
			btns = append(btns, button(fmt.Sprintf("%s: %d gold", o.Label, price),
				fmt.Sprintf("senate:pay:%d:%d:%s", m.ID, s.ID, o.Key), discordgo.SuccessButton, price > c.Wealth))
		}
	}
	comps := []discordgo.MessageComponent{b.senatorSelect(ctx, m, sen, bribes)}
	if len(btns) > 0 {
		comps = append(comps, row(btns...))
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: s.Name, Description: desc, Color: colorGold}},
		Components: comps,
	})
}

func (b *Bot) onPay(ctx context.Context, i *discordgo.InteractionCreate, matterID, senatorID int64, opt string) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.update(i, &discordgo.InteractionResponseData{Content: msg, Embeds: []*discordgo.MessageEmbed{}})
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	st, err2 := b.store.GetRealm(ctx, m.GuildID)
	if err := errors.Join(err1, err2); err != nil {
		b.fail(i, err)
		return
	}
	s, ok := findSenator(sen, senatorID)
	if !ok {
		return
	}
	if _, ok := ev.Option(opt); !ok {
		return
	}
	price, can := s.BribePrice(ev, opt, st.Trackers, m.ID)
	if !can {
		b.update(i, &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{{Title: s.Name, Description: s.Name + " cannot be bought.", Color: colorGold}},
		})
		return
	}
	var text string
	switch err := b.store.PayBribe(ctx, m.ID, s.ID, c.ID, opt, price, time.Now()); {
	case errors.Is(err, store.ErrBought):
		text = "Someone got to " + s.Name + " first."
	case errors.Is(err, store.ErrEnded):
		text = "Voting on this matter has closed."
	case err != nil:
		text = err.Error()
	default:
		text = fmt.Sprintf("Gold changes hands. **%s** will vote to **%s**. (-%d gold)", s.Name, strings.ToLower(optionLabel(ev, opt)), price)
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: s.Name, Description: text, Color: colorGold}},
	})
}
