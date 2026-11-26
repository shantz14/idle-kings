package bot

import (
	"context"
	"log/slog"
	"slices"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// leak gives every player with spies on actor a chance to hear about a
// private action. Call it after the action has succeeded. It is best effort:
// errors are logged and never stop the action.
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
		d, ok := game.RollLeak(b.rng, n.Value)
		if !ok {
			continue
		}
		o, err := b.store.GetCharacterByID(ctx, n.OwnerID)
		if err != nil {
			continue
		}
		_, _ = b.dm(o.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
			Title:       "🕵️ Word from your spies in " + actor.House(),
			Description: s.Text(d),
			Color:       colorGold,
		}}})
	}
}
