package bot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/store"
)

// post sends an embed to the court's announcements channel, if it has one.
func (b *Bot) post(ctx context.Context, guildID string, e *discordgo.MessageEmbed) {
	court, err := b.store.GetCourt(ctx, guildID)
	if err != nil || court.AnnouncementsChannelID == "" {
		return
	}
	if _, err := b.s.ChannelMessageSendEmbed(court.AnnouncementsChannelID, e); err != nil {
		slog.Warn("post announcement", "guild", guildID, "err", err)
	}
}

// announce posts a public activity in the court's announcements channel and
// records it on the activity. It returns " The court has been told in #x."
// or "".
func (b *Bot) announce(ctx context.Context, a store.Activity, e *discordgo.MessageEmbed, comps []discordgo.MessageComponent) string {
	court, err := b.store.GetCourt(ctx, a.GuildID)
	if err != nil || court.AnnouncementsChannelID == "" {
		return ""
	}
	msg, err := b.s.ChannelMessageSendComplex(court.AnnouncementsChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{e}, Components: comps,
	})
	if err != nil {
		slog.Warn("announce", "kind", a.Kind, "err", err)
		return ""
	}
	_ = b.store.SetAnnouncement(ctx, a.ID, msg.ChannelID, msg.ID)
	return fmt.Sprintf(" The court has been told in <#%s>.", msg.ChannelID)
}

// announceEnd posts a public activity's result and strips the buttons from
// its announcement.
func (b *Bot) announceEnd(a store.Activity, e *discordgo.MessageEmbed) {
	if a.AnnounceChannelID == "" {
		return
	}
	_, _ = b.s.ChannelMessageSendEmbed(a.AnnounceChannelID, e)
	if a.AnnounceMessageID != "" {
		ended := []*discordgo.MessageEmbed{e}
		empty := []discordgo.MessageComponent{}
		_, _ = b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: a.AnnounceChannelID, ID: a.AnnounceMessageID, Embeds: &ended, Components: &empty})
	}
}
