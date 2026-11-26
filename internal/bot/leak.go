package bot

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// leak gives every player with spies on actor a chance to hear about a
// private action. Call it after the action has succeeded. It is best effort:
// errors are logged and never stop the action. Under the Informers' Law, a
// crime the spies learn enough about comes with a Denounce button.
func (b *Bot) leak(ctx context.Context, actor store.Character, s game.Secret) {
	nets, err := b.store.SpyNetworksOn(ctx, actor.ID)
	if err != nil {
		slog.Error("leak networks", "err", err)
		return
	}
	for _, n := range nets {
		if n.OwnerID == actor.ID || slices.Contains(s.Skip, n.OwnerID) {
			continue
		}
		d, ok := game.RollLeak(b.rng, n.Value, actor.Laws)
		if !ok {
			continue
		}
		o, err := b.store.GetCharacterByID(ctx, n.OwnerID)
		if err != nil {
			continue
		}
		msg := &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
			Title:       "🕵️ Word from your spies in " + actor.House(),
			Description: s.Text(d),
			Color:       colorGold,
		}}}
		if game.CanDenounce(s.Kind, d, actor.Laws) {
			now := time.Now()
			id, err := b.store.CreateDenunciation(ctx, store.Denunciation{
				GuildID: actor.GuildID, OwnerID: o.ID, ActorID: actor.ID, Crime: s.Kind, Detail: s.Text(d),
				ExpiresAt: now.Add(time.Duration(game.LawDenounceDays) * b.t.Day),
			}, now)
			if err != nil {
				slog.Error("denunciation", "err", err)
			} else {
				msg.Embeds[0].Description += "\n\nThat is a crime, and the Informers' Law pays those who report one."
				msg.Components = []discordgo.MessageComponent{row(
					button("Denounce "+actor.FullName(), fmt.Sprintf("law:denounce:%d", id), discordgo.DangerButton, false),
				)}
			}
		}
		_, _ = b.dm(o.UserID, msg)
	}
}
