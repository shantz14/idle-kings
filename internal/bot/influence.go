package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	registerActivity(game.KindCanvass, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.canvassPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveCanvass,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("You are out canvassing the districts. You'll be back <t:%d:R>.", a.EndsAt.Unix())
		},
	})
	registerActivity(game.KindCircus, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.circusPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveCircus,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("The city is feasting at your expense. It ends <t:%d:R>.", a.EndsAt.Unix())
		},
	})
	componentRoutes["canvass"] = (*Bot).onCanvass
	componentRoutes["circus"] = (*Bot).onCircus
	modalRoutes["circus"] = (*Bot).onCircusModal
}

// Canvass the Districts custom IDs:
//   canvass:go                 start canvassing
//
// Bread and Circuses custom IDs:
//   circus:size:<gold>         start with a preset spend
//   circus:custom              open the custom amount popup
//   circus:amount              (modal) custom amount submitted

// busyLine says why a character can't start an action that takes their
// slot, or "". fallen is what to say if the realm has fallen.
func (b *Bot) busyLine(ctx context.Context, c store.Character, fallen string) string {
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		return b.activityLine(ctx, a)
	}
	if c.RealmFallen {
		return fallen
	}
	return ""
}

// ---- Canvass the Districts ----

func (b *Bot) onCanvass(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || parts[0] != "go" {
		return
	}
	if busy := b.busyLine(ctx, c, "fallen"); busy != "" {
		b.update(i, b.canvassPanel(ctx, c, ""))
		return
	}
	now := time.Now()
	a, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindCanvass, StartedAt: now, EndsAt: now.Add(b.t.Canvass),
	}, 0, nil)
	if err != nil {
		b.update(i, b.canvassPanel(ctx, c, err.Error()))
		return
	}
	b.leak(ctx, c, game.SecretCanvass(c.FullName()))
	b.update(i, startedView("🗺️ Out in the districts", "You head out to shake hands across the city. I'll DM you how it went.",
		fact("Back", relTime(a.EndsAt))))
}

func (b *Bot) canvassPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	return canvassView(b.t.Canvass, b.busyLine(ctx, c, "The realm has fallen. Nobody is listening any more."), note)
}

func canvassView(dur time.Duration, busy, note string) *discordgo.InteractionResponseData {
	desc := "Shake hands, kiss babies and make promises across the city's districts. " +
		"Even a bad day wins a little influence. Diplomacy helps most, and a famous name opens doors."
	e := actionEmbed(game.KindCanvass, withNote(busyNote(busy, note), desc))
	e.Fields = []*discordgo.MessageEmbedField{fact("Cost", "Free"), fact("Takes", humanDuration(dur)), fact("Seen by", "🔒 Private")}
	e.Footer = footer("Influence comes harder the more of it you already have.")
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{row(
			withEmoji(button("Go canvassing", "canvass:go", discordgo.SuccessButton, busy != ""), "🗺️"),
			backButton(game.KindCanvass),
		)},
	}
}

func (b *Bot) resolveCanvass(ctx context.Context, a store.Activity, now time.Time) {
	c, err := b.store.GetCharacterByID(ctx, a.HostID)
	if err != nil {
		slog.Error("canvass host", "err", err)
		return
	}
	res := game.ResolveCanvass(b.rng, c.Person())
	d := game.Delta{Influence: res.Influence}
	d.Merge(res.Practice)
	if err := b.store.ApplyResult(ctx, a.ID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: d}}, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply canvass", "err", err)
		}
		return
	}
	color := colorPanel
	if res.Success {
		color = colorGood
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: "🗺️ Back from the districts", Description: withDelta(res.Text, d), Color: color,
	}}})
}

// ---- Bread and Circuses ----

func (b *Bot) onCircus(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	switch parts[0] {
	case "size":
		if len(parts) > 1 {
			if c, ok := b.myCharacter(ctx, i); ok {
				b.onStartCircus(ctx, i, c, int(atoi(parts[1])))
			}
		}
	case "custom":
		if err := b.s.InteractionRespond(i.Interaction, amountModal("circus:amount", "How much will you spend?",
			game.CircusMin, game.CircusMax)); err != nil {
			slog.Error("circus modal", "err", err)
		}
	}
}

func (b *Bot) onCircusModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || parts[0] != "amount" {
		return
	}
	gold, ok := modalAmount(i)
	if !ok {
		b.update(i, b.circusPanel(ctx, c, "That wasn't a whole number of gold."))
		return
	}
	b.onStartCircus(ctx, i, c, gold)
}

func (b *Bot) circusPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	return circusView(c, b.t.Circus, b.busyLine(ctx, c, "The realm has fallen. There is no city left to feed."), joinNotes(permitNote(c.Laws), note))
}

func circusView(c store.Character, dur time.Duration, busy, note string) *discordgo.InteractionResponseData {
	desc := "Feed and entertain the city at your own expense. The people's mood rises, and your influence with it. " +
		"A steady hand with money (Stewardship) keeps the crowds from turning ugly. Populists in the Senate will love you for it; misers won't."
	e := actionEmbed(game.KindCircus, withNote(busyNote(busy, note), desc))
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)),
		fact("Spend", fmt.Sprintf("%d to %d gold", game.CircusMin, game.CircusMax)),
		fact("Lasts", humanDuration(dur)),
	}
	e.Footer = footer("Each coin helps a little less than the last, and influence comes harder the more you have.")
	var btns []discordgo.MessageComponent
	for _, s := range game.CircusSizes {
		btns = append(btns, button(fmt.Sprintf("%s (%d gold)", s.Name, s.Gold), fmt.Sprintf("circus:size:%d", s.Gold),
			discordgo.SuccessButton, busy != "" || s.Gold > c.Wealth))
	}
	btns = append(btns, withEmoji(button("Other amount", "circus:custom", discordgo.PrimaryButton, busy != "" || c.Wealth < game.CircusMin), "✏️"))
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{row(btns...),
			row(backButton(game.KindCircus))},
	}
}

func (b *Bot) onStartCircus(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, gold int) {
	if gold < game.CircusMin || gold > game.CircusMax {
		b.update(i, b.circusPanel(ctx, c, fmt.Sprintf("Spend between %d and %d gold.", game.CircusMin, game.CircusMax)))
		return
	}
	if busy := b.busyLine(ctx, c, "fallen"); busy != "" {
		b.update(i, b.circusPanel(ctx, c, ""))
		return
	}
	if short := permitShort(c, gold); short != "" {
		b.update(i, b.circusPanel(ctx, c, short))
		return
	}
	now := time.Now()
	a, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindCircus, Public: true, StartedAt: now, EndsAt: now.Add(b.t.Circus),
		Fee: game.PermitFee(c.Laws),
	}, gold, nil)
	if err != nil {
		b.update(i, b.circusPanel(ctx, c, err.Error()))
		return
	}
	permit := permitLine(a)
	name := game.CircusName(gold)
	where := b.announce(ctx, a, &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🎪 %s pays for a %s", c.FullName(), lowerName(name)),
		Description: fmt.Sprintf("House %s throws open its granaries. Bread for all, and games in the arena.", c.FamilyName),
		Color:       colorPanel,
		Fields:      []*discordgo.MessageEmbedField{fact("Ends", relTime(a.EndsAt))},
	}, nil)
	b.update(i, startedView("🎪 Bread and circuses",
		fmt.Sprintf("You pay for a %s. I'll DM you how the city took it.%s%s", lowerName(name), where, permit),
		fact("Spent", fmt.Sprintf("%d gold", gold)), fact("Ends", relTime(a.EndsAt))))
}

func (b *Bot) resolveCircus(ctx context.Context, a store.Activity, now time.Time) {
	c, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	sen, err2 := b.store.Senators(ctx, a.GuildID)
	if err := errors.Join(err1, err2); err != nil {
		slog.Error("circus load", "err", err)
		return
	}
	res := game.ResolveCircus(b.rng, c.Person(), a.Spend, sen)
	out := store.Outcome{
		Deltas:   map[int64]game.Delta{c.ID: res.Delta},
		Realm:    game.Realm{game.AttrPopularity: res.Popularity},
		Opinions: res.Opinions,
	}
	before, _ := b.realmBefore(ctx, a.GuildID)
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply circus", "err", err)
		}
		return
	}
	b.afterRealmChange(ctx, a.GuildID, before)
	color := colorGood
	if !res.Success {
		color = colorBad
	}
	desc := fmt.Sprintf("%s\n\n👥 The people's mood: **+%s** popularity.", withDelta(res.Text, res.Delta), game.Num(res.Popularity))
	b.dmNote(c.UserID, "🎪 Bread and circuses", desc, color)
	b.announceEnd(a, &discordgo.MessageEmbed{Title: fmt.Sprintf("🎪 %s's %s", c.FullName(), lowerName(game.CircusName(a.Spend))),
		Description: res.Public, Color: color})
}

// ---- shared announcement helpers ----

// lowerName lowercases a preset name for use mid-sentence.
func lowerName(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}
