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
	componentRoutes["law"] = (*Bot).onLaw
}

// Law custom IDs:
//   law:denounce:<denunciation>   expose a misdeed from your dossier
//   law:whisper:<denunciation>    spread it quietly instead; only the culprit hears
//   law:keep:<denunciation>       keep a fresh report for later (it's already filed)

func (b *Bot) onLaw(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	switch {
	case parts[0] == "denounce" && len(parts) > 1:
		b.onDenounce(ctx, i, atoi(parts[1]))
	case parts[0] == "whisper" && len(parts) > 1:
		b.onWhisper(ctx, i, atoi(parts[1]))
	case parts[0] == "keep" && len(parts) > 1:
		b.onKeepReport(ctx, i, atoi(parts[1]))
	}
}

// onKeepReport takes the buttons off a spy report; it stays in the dossier.
func (b *Bot) onKeepReport(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	d, err := b.store.GetDenunciation(ctx, id)
	me, err2 := b.store.GetCharacterByID(ctx, d.OwnerID)
	if err != nil || err2 != nil || me.UserID != userOf(i).ID {
		b.reply(i, "That report isn't yours.")
		return
	}
	embeds := i.Message.Embeds
	if len(embeds) > 0 {
		embeds[0].Footer = footer("📁 Filed in your dossier (Intrigue → Your Dossier).")
	}
	b.update(i, &discordgo.InteractionResponseData{Embeds: embeds, Components: []discordgo.MessageComponent{}})
}

// loadReport loads a dossier report the clicking player owns, with its
// culprit and the court's senators. ok is false once it has replied.
func (b *Bot) loadReport(ctx context.Context, i *discordgo.InteractionCreate, id int64) (d store.Denunciation, me, accused store.Character, sen []game.Senator, ok bool) {
	d, err := b.store.GetDenunciation(ctx, id)
	if err != nil {
		b.reply(i, "That report has gone missing.")
		return
	}
	me, err = b.store.GetCharacterByID(ctx, d.OwnerID)
	if err != nil || me.UserID != userOf(i).ID {
		b.reply(i, "That report isn't yours.")
		return
	}
	accused, err = b.store.GetCharacterByID(ctx, d.ActorID)
	if err != nil {
		b.reply(i, "They are gone from court.")
		return
	}
	sen, err = b.store.Senators(ctx, d.GuildID)
	if err != nil {
		b.fail(i, err)
		return
	}
	return d, me, accused, sen, true
}

// reportUsed answers a click on a report that's already been used.
func (b *Bot) reportUsed(i *discordgo.InteractionCreate) {
	b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
		Title: "📁 Already used", Description: "You've already used that report.", Color: colorMuted}},
		Components: []discordgo.MessageComponent{}})
}

// onWhisper spreads a report quietly. Nothing is posted; the culprit is
// told someone is talking, and anyone with spies on the whisperer may learn
// who, the culprit included.
func (b *Bot) onWhisper(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	d, me, accused, sen, ok := b.loadReport(ctx, i, id)
	if !ok {
		return
	}
	w := game.Whisper(sen, d.Crime, accused.ID)
	out := store.Outcome{Deltas: map[int64]game.Delta{accused.ID: w.Accused}, Opinions: w.Opinions}
	if err := b.store.Denounce(ctx, d.ID, out, time.Now()); errors.Is(err, store.ErrDenounced) {
		b.reportUsed(i)
		return
	} else if err != nil {
		b.fail(i, err)
		return
	}
	word := game.MisdeedWord(d.Crime)
	b.update(i, whisperedView(d, accused, w))
	_, _ = b.dm(accused.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title:       "🗣️ Someone is talking",
		Description: fmt.Sprintf("Someone is spreading the rumor that you've been %s, and the Senate thinks less of you.\n%s", word, effects(w.Accused.Describe())),
		Color:       colorBad,
	}}})
	b.leak(ctx, me, game.SecretWhisper(me.FullName(), accused.FullName(), word))
}

func whisperedView(d store.Denunciation, accused store.Character, w game.Denunciation) *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
		Title: "🗣️ You whispered it about",
		Description: fmt.Sprintf("%s\n\nYou let it slip in the right ears, and the story is spreading. %s only knows that someone is talking, unless their own spies are watching you.\n%s",
			quote(d.Detail), accused.FullName(), effects(accused.FullName()+": "+w.Accused.Describe())),
		Color: colorSecret,
	}}, Components: []discordgo.MessageComponent{}}
}

func (b *Bot) onDenounce(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	d, me, accused, sen, ok := b.loadReport(ctx, i, id)
	if !ok {
		return
	}
	now := time.Now()
	den := game.Denounce(sen, d.Crime, accused.ID, me.ID, me.Influence, accused.Laws)
	before, hadRealm := b.realmBefore(ctx, d.GuildID)
	out := store.Outcome{
		Deltas:   map[int64]game.Delta{accused.ID: den.Accused, me.ID: den.Informer},
		Opinions: den.Opinions, Realm: den.Realm,
	}
	if err := b.store.Denounce(ctx, d.ID, out, now); errors.Is(err, store.ErrDenounced) {
		b.reportUsed(i)
		return
	} else if err != nil {
		b.fail(i, err)
		return
	}
	if hadRealm {
		b.afterRealmChange(ctx, d.GuildID, before)
	}

	b.update(i, denouncedView(d, accused, den))
	word := game.MisdeedWord(d.Crime)
	_, _ = b.dm(accused.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: "📢 You have been denounced",
		Description: fmt.Sprintf("Someone has exposed you before the whole court. The charge: **%s**. The Senate thinks less of you.\n%s",
			word, effects(den.Accused.Describe())),
		Color: colorBad,
	}}})
	public := fmt.Sprintf("Word is going round that **%s** (%s) has been %s. Someone has the proof, and the court is talking.",
		accused.FullName(), accused.House(), word)
	if den.Crime && den.Paid {
		public = fmt.Sprintf("An informer has denounced **%s** (%s) to the magistrates for %s. They have the evidence, and the court is talking.",
			accused.FullName(), accused.House(), word)
	}
	b.postCourt(ctx, d.GuildID, &discordgo.MessageEmbed{Title: "🕵️ Denounced", Description: public, Color: colorBad})
}

func denouncedView(d store.Denunciation, accused store.Character, den game.Denunciation) *discordgo.InteractionResponseData {
	text := fmt.Sprintf("%s\n\nThe court knows now.\n%s", quote(d.Detail), effects(accused.FullName()+": "+den.Accused.Describe()))
	if den.Paid {
		text = fmt.Sprintf("%s\n\nThe Informers' Law pays you for your trouble.\n%s", quote(d.Detail), effects(den.Informer.Describe()))
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
		Title: "📢 You denounced " + accused.FullName(), Description: text, Color: colorGood,
	}}, Components: []discordgo.MessageComponent{}}
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
		return fmt.Sprintf("🪖 Under martial law, every public gathering needs a permit: **%d gold** more.", fee)
	}
	return ""
}

// permitShort says why a character can't afford Martial Law's permit on top
// of spend, or "".
func permitShort(c store.Character, spend int) string {
	if fee := game.PermitFee(c.Laws); fee > 0 && c.Wealth < spend+fee {
		return fmt.Sprintf("🪖 Under martial law you also need a **%d gold** permit, and you only have %d gold.", fee, c.Wealth)
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
	return fmt.Sprintf("\n\n🪖 The watch charged you **%d gold** for a permit.", fee)
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
