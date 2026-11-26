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
// errors are logged and never stop the action. A misdeed the spies learn
// enough about is filed in the spymaster's dossier, to denounce now or later.
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
		if game.CanExpose(s.Kind, d, actor.Laws) {
			id, err := b.store.CreateDenunciation(ctx, store.Denunciation{
				GuildID: actor.GuildID, OwnerID: o.ID, ActorID: actor.ID, Crime: s.Kind, Detail: s.Text(d),
			}, time.Now())
			if err != nil {
				slog.Error("denunciation", "err", err)
			} else {
				msg.Embeds[0].Description += "\n\n" + exposeNote(actor.Laws)
				msg.Components = denounceButtons(id, actor.FullName())
			}
		}
		_, _ = b.dm(o.UserID, msg)
	}
}

// exposeNote is what a report in the dossier could do, under the laws now.
func exposeNote(ls game.Laws) string {
	if ls.Has(game.LawInformers) {
		return "That is worth exposing, and the Informers' Law pays for reports like this. It's filed in your dossier: denounce them now, or keep it for when it hurts most."
	}
	return "That is worth exposing. It's filed in your dossier: make it public now, or keep it for when it hurts most. (Under the Informers' Law it would hit harder, and pay.)"
}

func denounceButtons(id int64, name string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{row(
		button(truncate("Denounce "+name, 80), fmt.Sprintf("law:denounce:%d", id), discordgo.DangerButton, false),
		button("Keep it for later", fmt.Sprintf("law:keep:%d", id), discordgo.SecondaryButton, false),
	)}
}
