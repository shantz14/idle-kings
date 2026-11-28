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
	registerActivity(game.KindEmbezzle, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.embezzlePanel(ctx, c, "")
		},
		resolve: (*Bot).resolveEmbezzle,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("You are cooking the treasury's books. You'll be done <t:%d:R>.", a.EndsAt.Unix())
		},
	})
	componentRoutes["embezzle"] = (*Bot).onEmbezzle
}

// Embezzle custom IDs:
//   embezzle:go:<size>   start embezzling

func (b *Bot) onEmbezzle(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || len(parts) < 2 || parts[0] != "go" {
		return
	}
	size, ok := game.EmbezzleSizeByKey(parts[1])
	if !ok {
		return
	}
	if busy := b.spyBusy(ctx, c); busy != "" {
		b.update(i, b.embezzlePanel(ctx, c, ""))
		return
	}
	now := time.Now()
	ends := now.Add(b.t.Embezzle)
	if _, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindEmbezzle, Goal: size.Key, StartedAt: now, EndsAt: ends,
	}, 0, nil); err != nil {
		b.update(i, b.embezzlePanel(ctx, c, err.Error()))
		return
	}
	b.update(i, startedView("🪙 Cooking the books", "You start moving the treasury's silver, quietly. I'll DM you how it went.", fact("You'll know", relTime(ends))))
}

func (b *Bot) embezzlePanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	recent, err := b.store.RecentEmbezzlements(ctx, c.GuildID, time.Now().Add(-game.EmbezzleRecentDays*b.t.Day))
	if err != nil {
		slog.Error("recent embezzlements", "err", err)
	}
	return embezzleView(b.t.Embezzle, b.spyBusy(ctx, c), note, recent > 0)
}

// embezzleView is the panel; watchful is true when the treasury has been
// robbed lately and the clerks are on their guard.
func embezzleView(d time.Duration, busy, note string, watchful bool) *discordgo.InteractionResponseData {
	desc := "Move some of the Republic's silver into your own purse. Stewardship and Intrigue keep the ledgers straight. " +
		"The more you take, the likelier you are to be caught: then the silver goes back, the whole court hears of it, and the Senate turns against you."
	if watchful {
		desc += "\n\n" + quote("👀 The treasury has been robbed lately, and the clerks are on their guard. The more it's robbed, the harder it gets.")
	}
	var btns []discordgo.MessageComponent
	for _, s := range game.EmbezzleSizes {
		btns = append(btns, button(fmt.Sprintf("%s (~%d gold)", s.Name, s.Gold()), "embezzle:go:"+s.Key, discordgo.DangerButton, busy != ""))
	}
	btns = append(btns, backButton(game.KindEmbezzle))
	e := actionEmbed(game.KindEmbezzle, withNote(busyNote(busy, note), desc))
	e.Color = colorSecret
	e.Fields = []*discordgo.MessageEmbedField{fact("Takes", humanDuration(d)), fact("Seen by", "🔒 Private, unless you're caught")}
	e.Footer = footer("💀 Every coin you take is one the realm doesn't have. If the treasury empties, everyone loses, you too.")
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func (b *Bot) resolveEmbezzle(ctx context.Context, a store.Activity, now time.Time) {
	c, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	sen, err2 := b.store.Senators(ctx, a.GuildID)
	before, ok := b.realmBefore(ctx, a.GuildID)
	size, okSize := game.EmbezzleSizeByKey(a.Goal)
	if err := errors.Join(err1, err2); err != nil || !ok || !okSize {
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now)
		return
	}
	recent, err := b.store.RecentEmbezzlements(ctx, a.GuildID, now.Add(-game.EmbezzleRecentDays*b.t.Day))
	if err != nil {
		slog.Error("recent embezzlements", "err", err)
	}
	res := game.ResolveEmbezzle(b.rng, c.Person(), size, before.Trackers[game.AttrTreasury], recent, sen)
	out := store.Outcome{Deltas: map[int64]game.Delta{c.ID: res.Delta}, Opinions: res.Opinions}
	if res.Treasury != 0 {
		out.Realm = game.Realm{game.AttrTreasury: res.Treasury}
	}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply embezzle", "err", err)
		}
		return
	}
	color := colorPanel
	switch {
	case res.Success:
		color = colorGood
	case res.Caught:
		color = colorBad
	}
	b.dmNote(c.UserID, "🪙 The treasury's books", withDelta(res.Text, res.Delta), color)
	if res.Caught {
		b.post(ctx, c.GuildID, &discordgo.MessageEmbed{
			Title: "📢 A thief in the treasury", Description: res.Public, Color: colorBad,
		})
	}
	if res.Success {
		b.leak(ctx, c, game.SecretEmbezzle(c.FullName(), res.Delta.Wealth))
		b.afterRealmChange(ctx, a.GuildID, before)
	}
}
