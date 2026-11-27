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
	registerActivity(game.KindCourt, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.courtPanel(ctx, c, 0, "")
		},
		resolve: (*Bot).resolveCourt,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			name := "a senator"
			if sen, err := b.store.Senators(ctx, a.GuildID); err == nil {
				if s, ok := findSenator(sen, a.TargetID); ok {
					name = "Senator " + s.Name
				}
			}
			return fmt.Sprintf("You are calling on %s. You'll be back <t:%d:R>.", name, a.EndsAt.Unix())
		},
	})
	componentRoutes["court"] = (*Bot).onCourt
}

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
	} else if c.RealmFallen {
		busy = "The realm has fallen. No one is receiving visitors."
	}
	return courtView(c, sen, ops, senatorID, b.courtFor, busy, note)
}

func courtView(c store.Character, sen []game.Senator, ops game.Opinions, senatorID int64, dur time.Duration, busy, note string) *discordgo.InteractionResponseData {
	intro := "Spend time in a senator's company and try to win them over. It depends on your skills, your approach, " +
		"their traits, and what they already think of you."

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
	e := actionEmbed(game.KindCourt, withNote(busyNote(busy, note), intro))
	e.Fields = []*discordgo.MessageEmbedField{fact("Takes", humanDuration(dur)), fact("Your purse", fmt.Sprintf("%d gold", c.Wealth))}
	e.Footer = footer("💬 Senators who like you take smaller bribes, pay more for your vote, and may follow your lead.")
	embeds = append(embeds, e)

	comps := []discordgo.MessageComponent{}
	if len(opts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "court:who",
			Placeholder: "Choose a senator to call on…", Options: opts}))
	}
	back := backButton(game.KindCourt)
	if chosen == nil {
		return &discordgo.InteractionResponseData{Embeds: embeds, Components: append(comps, row(back))}
	}

	s := *chosen
	op := ops[s.ID]
	var desc strings.Builder
	if p, ok := game.PersonaByName(s.Name); ok {
		fmt.Fprintf(&desc, "*%s*\n", p.Bio)
	}
	fmt.Fprintf(&desc, "*%s* · influence %s\n**Their opinion of you**\n%s\n**How will you approach them?**\n", s.TraitNames(), game.Num(s.Influence), opinionBlock(op))
	var btns []discordgo.MessageComponent
	for _, a := range game.Approaches {
		label := a.Name
		cost := 0
		if a.Gift {
			cost = game.GiftCost(s)
			label = fmt.Sprintf("%s (%d gold)", a.Name, cost)
		}
		fmt.Fprintf(&desc, "• **%s**: %s\n", label, a.Description)
		btns = append(btns, button(label, fmt.Sprintf("court:go:%d:%s", s.ID, a.Key), discordgo.PrimaryButton, busy != "" || cost > c.Wealth))
	}
	embeds = append(embeds, &discordgo.MessageEmbed{Title: "🎩 Senator " + s.Name, Description: strings.TrimSpace(desc.String()), Color: colorSenate})
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
	if c.RealmFallen {
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
	text := fmt.Sprintf("You call on **Senator %s**. You'll be back %s, and I'll DM you how it went.", s.Name, relTime(ends))
	fields := []*discordgo.MessageEmbedField{fact("Approach", a.Name)}
	if cost > 0 {
		fields = append(fields, fact("Gift", fmt.Sprintf("%d gold", cost)))
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "🎩 Courting Senator " + s.Name, Description: text, Color: colorGood, Fields: fields}},
		Components: []discordgo.MessageComponent{homeRow()},
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
		desc += effects("Opinion " + game.OpinionNum(res.Delta))
	}
	color := colorPanel
	switch {
	case res.Delta > 0:
		color = colorGood
	case res.Delta < 0:
		color = colorBad
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: "🎩 You called on Senator " + s.Name, Description: desc, Color: color,
	}}})
	b.leak(ctx, c, game.SecretCourtResult(c.FullName(), s.Name, res.Delta > 0, res.Delta < 0))
}

// activityLine describes what a character is busy doing.
func (b *Bot) activityLine(ctx context.Context, a store.Activity) string {
	if d, ok := activityDefs[a.Kind]; ok && d.busy != nil {
		return d.busy(b, ctx, a)
	}
	return fmt.Sprintf("You are busy until <t:%d:R>.", a.EndsAt.Unix())
}
