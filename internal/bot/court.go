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

// Court a Senator custom IDs:
//   court:who                       (select) senator chosen → approaches
//   court:go:<senator>:<approach>   start courting

func (b *Bot) onCourt(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "who":
		if vals := i.MessageComponentData().Values; len(vals) > 0 {
			b.update(i, b.courtPanel(ctx, c, atoi(vals[0]), ""))
		}
	case parts[0] == "go" && len(parts) > 2:
		b.onCourtGo(ctx, i, c, atoi(parts[1]), parts[2])
	}
}

// ---- showing opinion ----

// opinionTag is a short coloured opinion for lists: "🟢 +35".
func opinionTag(v float64) string {
	n := game.OpinionNum(v)
	switch {
	case n == "0":
		return "⚪ 0"
	case v > 0:
		return "🟢 " + n
	default:
		return "🔴 " + n
	}
}

// opinionBlock is a red/green number in an ansi code block, for detail pages.
func opinionBlock(v float64) string {
	color := "37" // white
	switch n := game.OpinionNum(v); {
	case n == "0":
	case v > 0:
		color = "32"
	default:
		color = "31"
	}
	return fmt.Sprintf("```ansi\n\u001b[1;%sm%s\u001b[0m %s\n```", color, game.OpinionNum(v), game.OpinionWord(v))
}

// ---- the panel ----

// courtPanel loads what courtView needs. senatorID 0 = none chosen yet.
func (b *Bot) courtPanel(ctx context.Context, c store.Character, senatorID int64, note string) *discordgo.InteractionResponseData {
	sen, err1 := b.store.Senators(ctx, c.GuildID)
	ops, err2 := b.store.Opinions(ctx, c.ID)
	if err := errors.Join(err1, err2); err != nil {
		slog.Error("court panel", "err", err)
		return &discordgo.InteractionResponseData{Content: "Something went wrong."}
	}
	busy := ""
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		busy = b.activityLine(ctx, a)
	} else if st, err := b.store.GetRealm(ctx, c.GuildID); err == nil && !st.Alive() {
		busy = "The realm has fallen. No one is receiving visitors."
	}
	return courtView(c, sen, ops, senatorID, b.courtFor, busy, note)
}

func courtView(c store.Character, sen []game.Senator, ops game.Opinions, senatorID int64, dur time.Duration, busy, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Spend %s in a senator's company and try to win them over. ", humanDuration(dur))
	sb.WriteString("How well it goes depends on your skills, how you approach them, their traits, and what they already think of you. ")
	sb.WriteString("Senators who like you charge less for bribes, pay more for your vote, and may follow your lead in the Senate.\n")

	embeds := []*discordgo.MessageEmbed{}
	var chosen *game.Senator
	var opts []discordgo.SelectMenuOption
	for _, s := range sen {
		opts = append(opts, discordgo.SelectMenuOption{
			Label: s.Name, Value: fmt.Sprint(s.ID), Default: s.ID == senatorID,
			Description: truncate(fmt.Sprintf("%s · %s · opinion %s", game.OpinionWord(ops[s.ID]), s.TraitNames(), game.OpinionNum(ops[s.ID])), 100),
		})
		if s.ID == senatorID {
			s := s
			chosen = &s
		}
	}
	if busy != "" {
		sb.WriteString("\n" + busy)
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	embeds = append(embeds, &discordgo.MessageEmbed{Title: "Court a Senator", Description: sb.String(), Color: colorGold})

	comps := []discordgo.MessageComponent{}
	if len(opts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "court:who",
			Placeholder: "Choose a senator to call on…", Options: opts}))
	}
	back := button("Back", "menu:cat:"+string(game.CatDiplomacy), discordgo.SecondaryButton, false)
	if chosen == nil {
		return &discordgo.InteractionResponseData{Embeds: embeds, Components: append(comps, row(back))}
	}

	s := *chosen
	op := ops[s.ID]
	var desc strings.Builder
	if p, ok := game.PersonaByName(s.Name); ok {
		fmt.Fprintf(&desc, "*%s*\n", p.Bio)
	}
	fmt.Fprintf(&desc, "%s · influence %s\n**Their opinion of you**\n%s\n**How will you approach them?**\n", s.TraitNames(), game.Num(s.Influence), opinionBlock(op))
	var btns []discordgo.MessageComponent
	for _, a := range game.Approaches {
		label := a.Name
		cost := 0
		if a.Gift {
			cost = game.GiftCost(s)
			label = fmt.Sprintf("%s (%d gold)", a.Name, cost)
		}
		fmt.Fprintf(&desc, "**%s**: %s\n", label, a.Description)
		btns = append(btns, button(label, fmt.Sprintf("court:go:%d:%s", s.ID, a.Key), discordgo.SuccessButton, busy != "" || cost > c.Wealth))
	}
	fmt.Fprintf(&desc, "\nYou have **%d gold**.", c.Wealth)
	embeds = append(embeds, &discordgo.MessageEmbed{Title: "Senator " + s.Name, Description: desc.String(), Color: colorGold})
	return &discordgo.InteractionResponseData{Embeds: embeds, Components: append(comps, row(append(btns, back)...))}
}

// ---- starting ----

func (b *Bot) onCourtGo(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, senatorID int64, approach string) {
	a, ok := game.ApproachByKey(approach)
	if !ok {
		return
	}
	sen, err := b.store.Senators(ctx, c.GuildID)
	if err != nil {
		b.fail(i, err)
		return
	}
	s, ok := findSenator(sen, senatorID)
	if !ok {
		b.update(i, b.courtPanel(ctx, c, 0, "That senator is gone."))
		return
	}
	if st, err := b.store.GetRealm(ctx, c.GuildID); err == nil && !st.Alive() {
		b.update(i, b.courtPanel(ctx, c, s.ID, ""))
		return
	}
	cost := 0
	if a.Gift {
		cost = game.GiftCost(s)
	}
	now := time.Now()
	ends := now.Add(b.courtFor)
	_, err = b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindCourt, Goal: a.Key, TargetID: s.ID,
		StartedAt: now, EndsAt: ends,
	}, cost, nil)
	if err != nil {
		b.update(i, b.courtPanel(ctx, c, s.ID, err.Error()))
		return
	}
	text := fmt.Sprintf("You call on **Senator %s** (%s). You'll be back <t:%d:R>, and I'll DM you how it went.", s.Name, strings.ToLower(a.Name), ends.Unix())
	if cost > 0 {
		text += fmt.Sprintf(" The gift cost **%d gold**.", cost)
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Courting " + s.Name, Description: text, Color: colorGreen}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:home", discordgo.SecondaryButton, false))},
	})
	b.leak(ctx, c, game.SecretCourt(c.FullName(), s.Name, strings.ToLower(a.Name)))
}

// ---- resolving ----

func (b *Bot) resolveCourt(ctx context.Context, a store.Activity, now time.Time) {
	c, err := b.store.GetCharacterByID(ctx, a.HostID)
	if err != nil {
		slog.Error("court host", "err", err)
		return
	}
	sen, err1 := b.store.Senators(ctx, c.GuildID)
	ops, err2 := b.store.Opinions(ctx, c.ID)
	if err := errors.Join(err1, err2); err != nil {
		slog.Error("court load", "err", err)
		return
	}
	s, ok1 := findSenator(sen, a.TargetID)
	ap, ok2 := game.ApproachByKey(a.Goal)
	if !ok1 || !ok2 { // senator or approach no longer exists: close it quietly
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now)
		return
	}
	before := ops[s.ID]
	res := game.ResolveCourt(b.rng, game.CourtInput{Player: c.Person(), Senator: s, Approach: ap, Opinion: before})
	var changes []game.OpinionChange
	if res.Delta != 0 {
		changes = []game.OpinionChange{{SenatorID: s.ID, CharacterID: c.ID, Delta: res.Delta}}
	}
	out := store.Outcome{Opinions: changes}
	if len(res.Practice.Stats) > 0 {
		out.Deltas = map[int64]game.Delta{c.ID: res.Practice}
	}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply court", "err", err)
		}
		return
	}
	after := game.ClampOpinion(before + res.Delta)
	desc := res.Text + "\n\n**Their opinion of you**\n" + opinionBlock(after)
	if res.Delta != 0 {
		desc += fmt.Sprintf("(%s)", game.OpinionNum(res.Delta))
	}
	color := colorGold
	switch {
	case res.Delta > 0:
		color = colorGreen
	case res.Delta < 0:
		color = colorRed
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: "You called on Senator " + s.Name, Description: desc, Color: color,
	}}})
	b.leak(ctx, c, game.SecretCourtResult(c.FullName(), s.Name, res.Delta > 0, res.Delta < 0))
}

// activityLine describes what a character is busy doing.
func (b *Bot) activityLine(ctx context.Context, a store.Activity) string {
	switch a.Kind {
	case game.KindParty:
		return fmt.Sprintf("You are hosting a party. It ends <t:%d:R>.", a.EndsAt.Unix())
	case game.KindCourt:
		name := "a senator"
		if sen, err := b.store.Senators(ctx, a.GuildID); err == nil {
			if s, ok := findSenator(sen, a.TargetID); ok {
				name = "Senator " + s.Name
			}
		}
		return fmt.Sprintf("You are calling on %s. You'll be back <t:%d:R>.", name, a.EndsAt.Unix())
	case game.KindDuel:
		return b.duelLine(ctx, a)
	case game.KindHunt:
		return fmt.Sprintf("You are leading a hunt. It ends <t:%d:R>.", a.EndsAt.Unix())
	case game.KindInvest:
		name := "a venture"
		if d, ok := game.SectorByKey(game.SectorKey(a.Goal)); ok {
			name = d.Name
		}
		return fmt.Sprintf("You are negotiating a stake in %s. The deal is struck <t:%d:R>.", name, a.EndsAt.Unix())
	case game.KindPlantSpies:
		name := "another house"
		if t, err := b.store.GetCharacterByID(ctx, a.TargetID); err == nil {
			name = t.FullName() + "'s house"
		}
		return fmt.Sprintf("Your agent is working their way into %s. You'll hear <t:%d:R>.", name, a.EndsAt.Unix())
	case game.KindRootOut:
		return fmt.Sprintf("You are searching your household for spies. You'll be done <t:%d:R>.", a.EndsAt.Unix())
	case game.KindCanvass:
		return fmt.Sprintf("You are out canvassing the districts. You'll be back <t:%d:R>.", a.EndsAt.Unix())
	case game.KindSpeech:
		return fmt.Sprintf("You are speaking in the Forum. You'll finish <t:%d:R>.", a.EndsAt.Unix())
	case game.KindCircus:
		return fmt.Sprintf("The city is feasting at your expense. It ends <t:%d:R>.", a.EndsAt.Unix())
	case game.KindWork:
		name := "a public work"
		if w, ok := game.WorkByKey(a.Goal); ok {
			name = "your " + strings.ToLower(w.Name)
		}
		return fmt.Sprintf("Builders are at work on %s. It opens <t:%d:R>.", name, a.EndsAt.Unix())
	}
	return fmt.Sprintf("You are busy until <t:%d:R>.", a.EndsAt.Unix())
}
