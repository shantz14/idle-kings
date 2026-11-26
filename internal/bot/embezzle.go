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
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Embezzling",
			Description: fmt.Sprintf("You start cooking the treasury's books. I'll DM you <t:%d:R>.", ends.Unix()), Color: colorGreen}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:home", discordgo.SecondaryButton, false))},
	})
}

func (b *Bot) embezzlePanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	return embezzleView(b.t.Embezzle, b.spyBusy(ctx, c), note)
}

func embezzleView(d time.Duration, busy, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Move some of the Republic's silver into your own purse. It takes %s. Stewardship and Intrigue keep the ledgers straight.\n\n", humanDuration(d))
	sb.WriteString("The more you take, the likelier you are to be caught. If you are, the silver goes back, the whole court hears of it, and the Senate turns against you.\n\n")
	sb.WriteString("Every coin you take is a coin the realm doesn't have. **If the treasury empties, everyone loses, you too.**")
	if busy != "" {
		sb.WriteString("\n\n" + busy)
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	var btns []discordgo.MessageComponent
	for _, s := range game.EmbezzleSizes {
		btns = append(btns, button(fmt.Sprintf("%s (~%d gold)", s.Name, s.Gold()), "embezzle:go:"+s.Key, discordgo.DangerButton, busy != ""))
	}
	btns = append(btns, button("Back", "menu:cat:"+string(game.CatIntrigue), discordgo.SecondaryButton, false))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Embezzle from the Treasury", Description: sb.String(), Color: colorGold}},
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
	res := game.ResolveEmbezzle(b.rng, c.Person(), size, before.Trackers[game.AttrTreasury], sen)
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
	color := colorGold
	switch {
	case res.Success:
		color = colorGreen
	case res.Caught:
		color = colorRed
	}
	desc := res.Text
	if d := res.Delta.Describe(); d != "" {
		desc += fmt.Sprintf(" (%s)", d)
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: "The treasury's books", Description: desc, Color: color,
	}}})
	if res.Caught {
		if court, err := b.store.GetCourt(ctx, c.GuildID); err == nil && court.AnnouncementsChannelID != "" {
			_, _ = b.s.ChannelMessageSendEmbed(court.AnnouncementsChannelID, &discordgo.MessageEmbed{
				Title: "A thief in the treasury", Description: res.Public, Color: colorRed,
			})
		}
	}
	if res.Success {
		b.leak(ctx, c, game.SecretEmbezzle(c.FullName(), res.Delta.Wealth))
		b.afterRealmChange(ctx, a.GuildID, before)
	}
}
