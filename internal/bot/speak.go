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

func init() {
	componentRoutes["speak"] = (*Bot).onSpeak
}

// Speak to a Senator custom IDs:
//   speak:who                          (select) senator chosen → their page
//   speak:open:<senator>               open a senator's page (counts as a visit)
//   speak:back:<senator>               back to their page (not a new visit)
//   speak:court:<senator>              the courting approaches for them
//   speak:ask:<matter>:<senator>       ask how they'll vote
//   speak:bribe:<matter>:<senator>     their bribe prices (onBribeWho)
// Selling them your vote uses senate:sellai:<matter>:<senator>.

func (b *Bot) onSpeak(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "who":
		if vals := i.MessageComponentData().Values; len(vals) > 0 {
			b.update(i, b.speakPanel(ctx, c, atoi(vals[0]), true, ""))
		}
	case parts[0] == "open" && len(parts) > 1:
		b.update(i, b.speakPanel(ctx, c, atoi(parts[1]), true, ""))
	case parts[0] == "back" && len(parts) > 1:
		b.update(i, b.speakPanel(ctx, c, atoi(parts[1]), false, ""))
	case parts[0] == "court" && len(parts) > 1:
		b.update(i, b.courtPanel(ctx, c, atoi(parts[1]), ""))
	case parts[0] == "ask" && len(parts) > 2:
		b.onSpeakAsk(ctx, i, c, atoi(parts[1]), atoi(parts[2]))
	case parts[0] == "bribe" && len(parts) > 2:
		b.onBribeWho(ctx, i, atoi(parts[1]), atoi(parts[2]))
	}
}

// speakMatter is an open matter as the Speak screen shows it.
type speakMatter struct {
	Matter store.Matter
	Event  game.MatterEvent
	Ask    *store.VoteAsk // what they told you, nil if you haven't asked
	Offer  int            // what they'd pay for your vote, 0 if nothing
	Sold   bool           // your vote on it is already sold
}

// speakPanel loads what speakView needs. senatorID 0 = none chosen yet.
// visit counts this as a call on them (opening their page, not pressing
// their buttons).
func (b *Bot) speakPanel(ctx context.Context, c store.Character, senatorID int64, visit bool, note string) *discordgo.InteractionResponseData {
	sen, err1 := b.store.Senators(ctx, c.GuildID)
	ops, err2 := b.store.Opinions(ctx, c.ID)
	if err := errors.Join(err1, err2); err != nil {
		slog.Error("speak panel", "err", err)
		return noteView(emojiWarn+" Something went wrong.", colorMuted)
	}
	busy := ""
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		busy = b.activityLine(ctx, a)
	} else if c.RealmFallen {
		busy = "The realm has fallen. No one is receiving visitors."
	}
	s, ok := findSenator(sen, senatorID)
	if !ok {
		return speakView(c, sen, ops, nil, "", nil, busy, note)
	}

	var visits int
	if visit {
		visits, err1 = b.store.RecordVisit(ctx, s.ID, c.ID, time.Now())
	} else if visits, err1 = b.store.Visits(ctx, s.ID, c.ID); visits > 0 {
		visits-- // the visit in progress, so the greeting stays the same
	}
	matters, err2 := b.speakMatters(ctx, c, s, ops[s.ID])
	if err := errors.Join(err1, err2); err != nil {
		slog.Error("speak panel", "err", err)
	}
	return speakView(c, sen, ops, &s, game.Greeting(s, ops[s.ID], visits), matters, busy, note)
}

// speakMatters is every open matter with what the senator has said and
// would pay.
func (b *Bot) speakMatters(ctx context.Context, c store.Character, s game.Senator, op float64) ([]speakMatter, error) {
	open, err := b.store.OpenMatters(ctx, c.GuildID)
	if err != nil || len(open) == 0 {
		return nil, err
	}
	st, err := b.store.GetRealm(ctx, c.GuildID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(open))
	for k, m := range open {
		ids[k] = m.ID
	}
	asks, err := b.store.VoteAsks(ctx, s.ID, c.ID, ids)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []speakMatter
	for _, m := range open {
		ev, ok := game.MatterByKey(m.EventKey)
		if !ok || ev.Auto || m.Resolved || !now.Before(m.ClosesAt) {
			continue
		}
		sm := speakMatter{Matter: m, Event: ev}
		if a, ok := asks[m.ID]; ok {
			sm.Ask = &a
		}
		_, _, sm.Sold = b.store.SoldVote(ctx, m.ID, c.ID)
		if _, gold, ok := s.VoteOfferFor(ev, c.VoteWeight(), st.Trackers, m.ID, op); ok && !st.Laws.Rules().SecretBallot {
			sm.Offer = gold
		}
		out = append(out, sm)
	}
	return out, nil
}

func speakView(c store.Character, sen []game.Senator, ops game.Opinions, chosen *game.Senator, greeting string, matters []speakMatter, busy, note string) *discordgo.InteractionResponseData {
	var opts []discordgo.SelectMenuOption
	for _, s := range sen {
		opts = append(opts, discordgo.SelectMenuOption{
			Label: s.Name, Value: fmt.Sprint(s.ID), Default: chosen != nil && s.ID == chosen.ID,
			Description: truncate(fmt.Sprintf("%s · %s · opinion %s", game.OpinionWord(ops[s.ID]), s.TraitNames(), game.OpinionNum(ops[s.ID])), 100),
		})
	}
	comps := []discordgo.MessageComponent{}
	if len(opts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "speak:who",
			Placeholder: "Choose a senator to call on…", Options: opts}))
	}
	back := backButton(game.KindCourt)

	if chosen == nil {
		desc := "Call on a senator. Ask how they mean to vote, bribe them, sell them your vote, or spend time courting them.\n\n" +
			"• 🗳️ **Ask how they'll vote**\n• 💰 **Bribe** them\n• 🪙 **Sell** them your vote\n• 💐 **Court** them, to win them over"
		e := actionEmbed(game.KindCourt, withNote(busyNote(busy, note), desc))
		e.Color = colorSenate
		e.Footer = footer("What they tell you depends on what they think of you, your Diplomacy, and who they are. They may lie.")
		return &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{e},
			Components: append(comps, row(back)),
		}
	}

	s := *chosen
	var desc strings.Builder
	if greeting != "" {
		fmt.Fprintf(&desc, "💬 *%s*\n\n", greeting)
	}
	if p, ok := game.PersonaByName(s.Name); ok {
		fmt.Fprintf(&desc, "%s\n", p.Bio)
	}
	fmt.Fprintf(&desc, "*%s* · influence %s\n**Their opinion of you**\n%s", s.TraitNames(), game.Num(s.Influence), opinionBlock(ops[s.ID]))
	e := &discordgo.MessageEmbed{Title: "🎩 Senator " + s.Name, Description: withNote(busyNote(busy, note), strings.TrimSpace(desc.String())), Color: colorSenate}

	var heard strings.Builder
	for _, sm := range matters {
		if sm.Ask != nil {
			fmt.Fprintf(&heard, "• On *%s*: %s\n", sm.Event.Title, sm.Ask.Text)
		}
	}
	if heard.Len() > 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "🗳️ What they've told you", Value: truncate(strings.TrimSpace(heard.String()), 1024)})
	}
	if len(matters) == 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: emojiSenate + " The Senate", Value: "Nothing is before the Senate right now."})
	}
	e.Fields = append(e.Fields, fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)))

	comps = append(comps, row(
		withEmoji(button("Court them", fmt.Sprintf("speak:court:%d", s.ID), discordgo.SuccessButton, busy != ""), "💐"),
		back,
	))
	for _, sm := range matters {
		suffix := ""
		if len(matters) > 1 {
			suffix = ": " + sm.Event.Title
		}
		m := sm.Matter.ID
		sell := "Sell your vote"
		if sm.Offer > 0 {
			sell = fmt.Sprintf("Sell your vote (%d gold)", sm.Offer)
		}
		comps = append(comps, row(
			withEmoji(button(truncate("Ask how they'll vote"+suffix, 80), fmt.Sprintf("speak:ask:%d:%d", m, s.ID), discordgo.PrimaryButton, sm.Ask != nil), emojiVote),
			withEmoji(button("Bribe", fmt.Sprintf("speak:bribe:%d:%d", m, s.ID), discordgo.SecondaryButton, false), "💰"),
			withEmoji(button(sell, fmt.Sprintf("senate:sellai:%d:%d", m, s.ID), discordgo.SecondaryButton, sm.Offer == 0 || sm.Sold), "🪙"),
		))
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
}

// ---- asking ----

func (b *Bot) onSpeakAsk(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, matterID, senatorID int64) {
	m, ev, _, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.update(i, b.speakPanel(ctx, c, senatorID, false, msg))
		return
	}
	if ev.Auto {
		return
	}
	asked, err := b.store.VoteAsks(ctx, senatorID, c.ID, []int64{m.ID})
	if err != nil {
		b.fail(i, err)
		return
	}
	if a, ok := asked[m.ID]; ok { // once per matter: they say the same again
		b.update(i, b.speakPanel(ctx, c, senatorID, false, "You've asked already. "+a.Text))
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	st, err2 := b.store.GetRealm(ctx, m.GuildID)
	bribes, err3 := b.store.Bribes(ctx, m.ID)
	votes, err4 := b.store.Votes(ctx, m.ID)
	chars, err5 := b.store.CharactersInCourt(ctx, m.GuildID)
	ops, err6 := b.store.CourtOpinions(ctx, m.GuildID)
	if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
		b.fail(i, err)
		return
	}
	s, ok := findSenator(sen, senatorID)
	if !ok {
		b.update(i, b.speakPanel(ctx, c, 0, false, "That senator is gone."))
		return
	}
	var players []game.Player
	for _, p := range chars {
		players = append(players, game.Player{Person: p.Person(), Role: p.Role, Vote: votes[p.ID], Weight: p.VoteWeight(), Items: p.Items})
	}
	br, bribed := bribes[s.ID]
	truth := game.SenatorIntent(ev, st.Trackers, m.ID, s, players, ops, br.Option)
	res := game.ResolveAsk(b.rng, game.AskInput{
		Player: c.Person(), Senator: s, Opinion: ops.Of(s.ID, c.ID), Event: ev, Realm: st.Trackers,
		MatterID: m.ID, Truth: truth, BribedByYou: bribed && br.CharacterID == c.ID,
	})
	now := time.Now()
	a, fresh, err := b.store.SaveVoteAsk(ctx, c.ID, store.VoteAsk{
		MatterID: m.ID, SenatorID: s.ID, Reply: string(res.Kind), Option: res.Option, Text: res.Text,
	}, now)
	if err != nil {
		b.fail(i, err)
		return
	}
	if fresh && len(res.Practice.Stats) > 0 {
		if err := b.store.ApplyOutcome(ctx, c.GuildID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: res.Practice}}, now); err != nil {
			slog.Error("ask practice", "err", err)
		}
	}
	b.update(i, b.speakPanel(ctx, c, s.ID, false, fmt.Sprintf("🗳️ You ask how they'll vote on *%s*.\n%s", ev.Title, a.Text)))
	if fresh {
		b.leak(ctx, c, game.SecretAskVote(c.FullName(), s.Name, ev.Title))
	}
}
