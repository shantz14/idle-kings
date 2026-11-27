package bot

import (
	"context"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// realmBefore snapshots the realm so afterRealmChange can compare. ok is
// false if there's no realm yet.
func (b *Bot) realmBefore(ctx context.Context, guildID string) (store.RealmState, bool) {
	st, err := b.store.GetRealm(ctx, guildID)
	return st, err == nil
}

// afterRealmChange posts warnings for trackers that crossed a line since
// before, and announces the fall if the change ended the realm. Call it
// after anything outside a Senate matter changes the trackers.
func (b *Bot) afterRealmChange(ctx context.Context, guildID string, before store.RealmState) {
	after, err := b.store.GetRealm(ctx, guildID)
	if err != nil {
		slog.Error("realm after change", "err", err)
		return
	}
	channel := ""
	if court, err := b.store.GetCourt(ctx, guildID); err == nil {
		channel = court.AnnouncementsChannelID
	}
	if before.Alive() && !after.Alive() {
		b.announceFall(ctx, guildID, channel, after.FallenReason)
		return
	}
	b.postWarnings(channel, game.RealmWarnings(before.Trackers, after.Trackers))
}

func (b *Bot) postWarnings(channel string, warnings []string) {
	if channel == "" || len(warnings) == 0 {
		return
	}
	_, _ = b.s.ChannelMessageSendEmbed(channel, &discordgo.MessageEmbed{
		Title: "⚠️ The state of the realm", Description: strings.Join(warnings, "\n"), Color: colorBad,
		Footer: footer("💀 If any tracker reaches 0, the realm falls and everyone loses."),
	})
}

// announceFall tells the court, and every player by DM, that all is lost.
func (b *Bot) announceFall(ctx context.Context, guildID, channel, reason string) {
	fall := &discordgo.MessageEmbed{
		Title:       "🔥 The realm has fallen",
		Description: reason + "\n\n💀 **All players have lost.**",
		Color:       colorBad,
	}
	if channel != "" {
		_, _ = b.s.ChannelMessageSendEmbed(channel, fall)
	}
	chars, err := b.store.CharactersInCourt(ctx, guildID)
	if err != nil {
		slog.Error("fall dms", "err", err)
		return
	}
	for _, c := range chars {
		_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{fall}})
	}
}
