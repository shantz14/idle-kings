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

// Law custom IDs:
//   law:denounce:<denunciation>   report a crime your spies uncovered (Informers' Law)

func (b *Bot) onLaw(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if parts[0] == "denounce" && len(parts) > 1 {
		b.onDenounce(ctx, i, atoi(parts[1]))
	}
}

// crimeWord is how a crime is named in a public denunciation.
func crimeWord(kind string) string {
	switch kind {
	case "embezzle":
		return "stealing from the treasury"
	case "bribe":
		return "bribing a senator"
	case "magic":
		return "dealing in enchanted goods"
	}
	return "a crime against the state"
}

func (b *Bot) onDenounce(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	d, err := b.store.GetDenunciation(ctx, id)
	if err != nil {
		b.reply(i, "That report has gone missing.")
		return
	}
	me, err := b.store.GetCharacterByID(ctx, d.OwnerID)
	if err != nil || me.UserID != userOf(i).ID {
		b.reply(i, "That report isn't yours.")
		return
	}
	accused, err := b.store.GetCharacterByID(ctx, d.ActorID)
	if err != nil {
		b.reply(i, "They are gone from court.")
		return
	}
	if !accused.Laws.Has(game.LawInformers) {
		b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
			Title: "Too late", Description: "The Informers' Law has been repealed. Nobody pays for reports any more.", Color: colorGold}},
			Components: []discordgo.MessageComponent{}})
		return
	}
	sen, err := b.store.Senators(ctx, d.GuildID)
	if err != nil {
		b.fail(i, err)
		return
	}
	now := time.Now()
	den := game.Denounce(sen, accused.ID, me.ID, me.Influence)
	before, hadRealm := b.realmBefore(ctx, d.GuildID)
	out := store.Outcome{
		Deltas:   map[int64]game.Delta{accused.ID: den.Accused, me.ID: den.Informer},
		Opinions: den.Opinions, Realm: den.Realm,
	}
	if err := b.store.Denounce(ctx, d.ID, out, now); errors.Is(err, store.ErrDenounced) {
		b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
			Title: "Old news", Description: "That report is too old to bring before the magistrates.", Color: colorGold}},
			Components: []discordgo.MessageComponent{}})
		return
	} else if err != nil {
		b.fail(i, err)
		return
	}
	if hadRealm {
		b.afterRealmChange(ctx, d.GuildID, before)
	}

	b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
		Title:       "You denounced " + accused.FullName(),
		Description: fmt.Sprintf("%s\n\nThe magistrates thank you, and pay you for your trouble. (%s)", d.Detail, den.Informer.Describe()),
		Color:       colorGreen,
	}}, Components: []discordgo.MessageComponent{}})
	_, _ = b.dm(accused.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: "You have been denounced",
		Description: fmt.Sprintf("Someone reported you to the magistrates for %s, and the whole court has heard. (%s, and the Senate thinks less of you)",
			crimeWord(d.Crime), den.Accused.Describe()),
		Color: colorRed,
	}}})
	b.postCourt(ctx, d.GuildID, &discordgo.MessageEmbed{
		Title: "🕵️ Denounced",
		Description: fmt.Sprintf("An informer has denounced **%s** (%s) for %s. The magistrates have the evidence, and the court is talking.",
			accused.FullName(), accused.House(), crimeWord(d.Crime)),
		Color: colorRed,
	})
}

// postCourt posts an embed in the court's announcements channel, if it has one.
func (b *Bot) postCourt(ctx context.Context, guildID string, e *discordgo.MessageEmbed) {
	court, err := b.store.GetCourt(ctx, guildID)
	if err != nil || court.AnnouncementsChannelID == "" {
		return
	}
	if _, err := b.s.ChannelMessageSendEmbed(court.AnnouncementsChannelID, e); err != nil {
		slog.Warn("post to court", "err", err)
	}
}

// lawsField lists the laws in force for an info page.
func lawsField(ls game.Laws) *discordgo.MessageEmbedField {
	return &discordgo.MessageEmbedField{Name: fmt.Sprintf("📜 Laws of the realm (%d of %d)", len(ls), game.MaxLaws), Value: game.DescribeLaws(ls)}
}

// permitNote explains Martial Law's fee on a public gathering's panel, or "".
func permitNote(ls game.Laws) string {
	if fee := game.PermitFee(ls); fee > 0 {
		return fmt.Sprintf("Under martial law, every public gathering needs a permit: **%d gold** more.", fee)
	}
	return ""
}

// permitShort says why a character can't afford Martial Law's permit on top
// of spend, or "".
func permitShort(c store.Character, spend int) string {
	if fee := game.PermitFee(c.Laws); fee > 0 && c.Wealth < spend+fee {
		return fmt.Sprintf("Under martial law you also need a **%d gold** permit, and you only have %d gold.", fee, c.Wealth)
	}
	return ""
}

// payPermit charges Martial Law's permit fee for a gathering that has just
// started, and returns a line saying so ("" if there is no fee).
func (b *Bot) payPermit(ctx context.Context, c store.Character) string {
	fee := game.PermitFee(c.Laws)
	if fee == 0 {
		return ""
	}
	if err := b.store.ApplyOutcome(ctx, c.GuildID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: {Wealth: -fee}}}, time.Now()); err != nil {
		slog.Error("permit fee", "err", err)
		return ""
	}
	return fmt.Sprintf("\n\nThe watch charged you **%d gold** for a permit.", fee)
}

// joinNotes joins the non-empty lines with a blank line between them.
func joinNotes(notes ...string) string {
	var out []string
	for _, n := range notes {
		if n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, "\n\n")
}
