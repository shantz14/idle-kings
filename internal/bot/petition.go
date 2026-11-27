package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	registerActivity(game.KindPetition, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.petitionPanel(ctx, c, "", "")
		},
		resolve: (*Bot).resolvePetition,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("You are lobbying the Senate's clerks. You'll know <t:%d:R>.", a.EndsAt.Unix())
		},
	})
	componentRoutes["petition"] = (*Bot).onPetition
}

// Petition the Senate custom IDs:
//   petition:pick        (select) a petition chosen
//   petition:go:<key>    start lobbying for it (key like "pass:grain_dole")

func (b *Bot) onPetition(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "pick":
		if vals := i.MessageComponentData().Values; len(vals) > 0 {
			b.update(i, b.petitionPanel(ctx, c, vals[0], ""))
		}
	case parts[0] == "go" && len(parts) > 2:
		b.onStartPetition(ctx, i, c, parts[1]+":"+parts[2])
	}
}

// onAgenda lists the matters already open or waiting, so nobody petitions
// for one twice.
func (b *Bot) onAgenda(ctx context.Context, guildID string) []string {
	keys, _ := b.store.QueuedEvents(ctx, guildID)
	if m, err := b.store.OpenMatter(ctx, guildID); err == nil {
		keys = append(keys, m.EventKey)
	}
	return keys
}

func (b *Bot) petitionPanel(ctx context.Context, c store.Character, key, note string) *discordgo.InteractionResponseData {
	st, _ := b.store.GetRealm(ctx, c.GuildID)
	return petitionView(c, st.Laws, b.onAgenda(ctx, c.GuildID), b.t.Petition,
		b.busyLine(ctx, c, "The realm has fallen. There is no Senate left to petition."), key, note)
}

func petitionView(c store.Character, laws game.Laws, agenda []string, dur time.Duration, busy, key, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	sb.WriteString("Clerks, dinners and copies of your motion put a law, or its repeal, before the Senate. ")
	sb.WriteString("Your petition always reaches the floor, as a vote of its own alongside the day's business, with your name on it. ")
	sb.WriteString("You gain standing if it passes, and look foolish if it fails.")
	if laws.Full() {
		fmt.Fprintf(&sb, "\n\n⚠️ All %d laws stand: the next one to pass pushes out **%s**.", game.MaxLaws, lawName(laws[0].Key))
	}
	if busy != "" {
		note = joinNotes("⏳ "+busy, note)
	}
	e := actionEmbed(game.KindPetition, withNote(note, sb.String()))
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Cost", fmt.Sprintf("%d gold", game.PetitionCost)),
		fact("Lobbying takes", humanDuration(dur)),
		fact("Law slots", fmt.Sprintf("%d of %d used", len(laws), game.MaxLaws)),
		lawsField(laws),
	}
	e.Footer = footer(fmt.Sprintf("At most %d laws stand at once; when another passes, the oldest lapses.", game.MaxLaws))
	embeds := []*discordgo.MessageEmbed{e}

	var opts []discordgo.SelectMenuOption
	for _, p := range game.Petitions(laws) {
		desc := p.Desc
		if len(p.Lapses) > 0 {
			desc = "Pushes out " + lawNames(p.Lapses) + ". " + desc
		}
		if slices.Contains(agenda, p.Event) {
			desc = "Already before the Senate. " + desc
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate(p.Name, 100), Value: p.Key, Description: truncate(desc, 100), Default: p.Key == key})
	}
	back := backButton(game.KindPetition)
	comps := []discordgo.MessageComponent{row(discordgo.SelectMenu{
		MenuType: discordgo.StringSelectMenu, CustomID: "petition:pick", Placeholder: "Choose what to petition for…", Options: opts,
	})}
	p, ok := game.PetitionByKey(key, laws)
	if !ok {
		return &discordgo.InteractionResponseData{Embeds: embeds, Components: append(comps, row(back))}
	}
	queued := slices.Contains(agenda, p.Event)
	desc := p.Desc
	if len(p.Lapses) > 0 {
		desc += "\n\n⚠️ If the Senate passes this, " + lapseList(laws, p.Lapses) + " lapses to make room."
	}
	if queued {
		desc = quote("🔒 The Senate already has this before it.") + "\n\n" + desc
	}
	embeds = append(embeds, &discordgo.MessageEmbed{Title: "📜 " + p.Name, Description: desc, Color: colorSenate})
	comps = append(comps, row(
		withEmoji(button(fmt.Sprintf("Petition (%d gold)", game.PetitionCost), "petition:go:"+p.Key, discordgo.SuccessButton,
			busy != "" || queued || c.Wealth < game.PetitionCost), "📜"),
		back,
	))
	return &discordgo.InteractionResponseData{Embeds: embeds, Components: comps}
}

func (b *Bot) onStartPetition(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, key string) {
	if busy := b.busyLine(ctx, c, "fallen"); busy != "" {
		b.update(i, b.petitionPanel(ctx, c, key, ""))
		return
	}
	st, err := b.store.GetRealm(ctx, c.GuildID)
	if err != nil {
		b.fail(i, err)
		return
	}
	p, ok := game.PetitionByKey(key, st.Laws)
	if !ok {
		b.update(i, b.petitionPanel(ctx, c, "", "That petition no longer makes sense: the laws have changed."))
		return
	}
	if slices.Contains(b.onAgenda(ctx, c.GuildID), p.Event) {
		b.update(i, b.petitionPanel(ctx, c, key, "The Senate already has this before it."))
		return
	}
	now := time.Now()
	a, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindPetition, Goal: p.Key, Public: true, StartedAt: now, EndsAt: now.Add(b.t.Petition),
	}, game.PetitionCost, nil)
	if err != nil {
		b.update(i, b.petitionPanel(ctx, c, key, err.Error()))
		return
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "📜 Lobbying the clerks",
			Description: fmt.Sprintf("You set about lobbying to **%s**. You'll know %s, and I'll DM you.",
				lowerName(p.Name), relTime(a.EndsAt)),
			Color: colorGood}},
		Components: []discordgo.MessageComponent{homeRow()},
	})
}

func (b *Bot) resolvePetition(ctx context.Context, a store.Activity, now time.Time) {
	c, err := b.store.GetCharacterByID(ctx, a.HostID)
	if err != nil {
		slog.Error("petition host", "err", err)
		return
	}
	st, err := b.store.GetRealm(ctx, a.GuildID)
	if err != nil {
		slog.Error("petition realm", "err", err)
		return
	}
	p, ok := game.PetitionByKey(a.Goal, st.Laws)
	if !ok || slices.Contains(b.onAgenda(ctx, a.GuildID), p.Event) || !st.Alive() {
		// The laws changed, or someone beat them to it: the gold is spent, nothing else.
		if err := b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now); err == nil {
			_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
				Title: "📜 Your petition", Description: "By the time your petition reached the clerks, it was no longer needed.", Color: colorMuted}}})
		}
		return
	}
	res := game.ResolvePetition(b.rng, c.Person(), p)
	if err := b.store.ApplyResult(ctx, a.ID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: res.Delta}}, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply petition", "err", err)
		}
		return
	}
	// A petition always reaches the floor; the tick opens it alongside the day's matter.
	if err := b.store.QueuePetition(ctx, a.GuildID, p.Event, c.ID, now); err != nil {
		slog.Error("queue petition", "err", err)
	}
	b.postCourt(ctx, a.GuildID, &discordgo.MessageEmbed{
		Title:       "📜 A petition to the Senate",
		Description: fmt.Sprintf("**%s** (%s) has petitioned the Senate to **%s**. The Senate takes it up at once, alongside the day's business.", c.FullName(), c.House(), lowerName(p.Name)),
		Color:       colorSenate,
	})
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: "📜 Your petition reaches the floor", Description: withDelta(res.Text, res.Delta), Color: colorGood,
	}}})
}

// withDelta puts what a delta did on its own line under the text, if it
// does anything.
func withDelta(text string, d game.Delta) string {
	if s := d.Describe(); s != "" {
		return text + "\n" + effects(s)
	}
	return text
}

// lawNames is "🪙 Debased Coinage and 🪖 Martial Law".
func lawNames(ks []game.LawKey) string {
	var names []string
	for _, k := range ks {
		names = append(names, lawName(k))
	}
	return strings.Join(names, " and ")
}

// lapseList names the laws that would lapse, in bold, with when each passed.
func lapseList(laws game.Laws, ks []game.LawKey) string {
	var names []string
	for _, k := range ks {
		n := "**" + lawName(k) + "**"
		for _, l := range laws {
			if l.Key == k && !l.Since.IsZero() {
				n += fmt.Sprintf(" (law since <t:%d:D>)", l.Since.Unix())
			}
		}
		names = append(names, n)
	}
	return strings.Join(names, " and ")
}
