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

func commands() []*discordgo.ApplicationCommand {
	guildOnly := &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	anywhere := &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild, discordgo.InteractionContextBotDM}
	install := &[]discordgo.ApplicationIntegrationType{discordgo.ApplicationIntegrationGuildInstall}
	public := &discordgo.ApplicationCommandOption{
		Type: discordgo.ApplicationCommandOptionBoolean, Name: "public",
		Description: "Post it for everyone in the channel instead of just you",
	}
	cmd := func(name, desc string, ctxs *[]discordgo.InteractionContextType, opts ...*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommand {
		return &discordgo.ApplicationCommand{Name: name, Description: desc, Contexts: ctxs, IntegrationTypes: install, Options: opts}
	}
	return []*discordgo.ApplicationCommand{
		cmd("claim-admin", "Become this server's game admin (first come, first served)", guildOnly),
		cmd("start", "Admin: start the game and post the Join button in this channel", guildOnly,
			&discordgo.ApplicationCommandOption{
				Type: discordgo.ApplicationCommandOptionChannel, Name: "announcements",
				Description:  "Channel for public events (defaults to this one)",
				ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText},
			},
			&discordgo.ApplicationCommandOption{
				Type: discordgo.ApplicationCommandOptionString, Name: "senate",
				Description: "Who sits in the Senate (defaults to the standard Senate)",
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "The standard Senate (balanced, the same every game)", Value: string(game.SenateStandard)},
					{Name: "A random Senate (8 senators drawn from every figure)", Value: string(game.SenateRandom)},
				},
			}),
		cmd("join", "Create your character and join the court", guildOnly),
		cmd("court", "Open your actions menu (works in DMs too)", anywhere),
		cmd("senate", "See today's matter and vote (works in DMs too)", anywhere),
		cmd("character", "Your full character sheet", anywhere),
		cmd("estate", "Your properties and possessions", anywhere),
		cmd("shop", "Buy houses, art, artifacts, weapons and armor", anywhere),
		cmd("realm", "The state of the realm and recent Senate decisions", anywhere, public),
		cmd("players", "Everyone at court, by influence", anywhere, public),
		cmd("profile", "A player's public profile", anywhere,
			&discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionUser, Name: "player",
				Description: "Whose profile (defaults to yours)"},
			public),
		cmd("help", "How to play", anywhere),
	}
}

func userOf(i *discordgo.InteractionCreate) *discordgo.User {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	return i.User
}

// option returns a slash command option's value, or nil.
func option(i *discordgo.InteractionCreate, name string) any {
	for _, o := range i.ApplicationCommandData().Options {
		if o.Name == name {
			return o.Value
		}
	}
	return nil
}

func optBool(i *discordgo.InteractionCreate, name string) bool {
	v, _ := option(i, name).(bool)
	return v
}

func optString(i *discordgo.InteractionCreate, name string) string {
	v, _ := option(i, name).(string)
	return v
}

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if userOf(i) == nil {
		return
	}
	defer recoverPanic("interaction", "type", i.Type.String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		name := i.ApplicationCommandData().Name
		switch name {
		case "claim-admin":
			b.cmdClaimAdmin(ctx, i)
		case "start":
			b.cmdStart(ctx, i)
		case "join":
			b.onJoin(ctx, i)
		case "realm", "players", "profile", "help":
			b.onInfoCommand(ctx, i, name)
		default: // commands that need your character
			c, ok := b.myCharacter(ctx, i)
			if !ok {
				return
			}
			switch name {
			case "court":
				b.respond(i, b.homePanel(ctx, c))
			case "senate":
				b.respond(i, b.senatePanel(ctx, c))
			case "character":
				b.respond(i, ephemeral(b.characterPage(ctx, c, false)))
			case "estate":
				b.respond(i, ephemeral(b.estatePage(c, false)))
			case "shop":
				b.respond(i, ephemeral(b.shopPanel(ctx, c, shopProps, "")))
			}
		}
	case discordgo.InteractionMessageComponent:
		b.dispatch(ctx, i, componentRoutes, i.MessageComponentData().CustomID)
	case discordgo.InteractionModalSubmit:
		b.dispatch(ctx, i, modalRoutes, i.ModalSubmitData().CustomID)
	}
}

func (b *Bot) cmdClaimAdmin(ctx context.Context, i *discordgo.InteractionCreate) {
	err := b.store.ClaimAdmin(ctx, i.GuildID, userOf(i).ID)
	switch {
	case errors.Is(err, store.ErrExists):
		c, _ := b.store.GetCourt(ctx, i.GuildID)
		b.reply(i, fmt.Sprintf("<@%s> already holds the court.", c.AdminUserID))
	case err != nil:
		b.fail(i, err)
	default:
		b.replyPublic(i, fmt.Sprintf("<@%s> is now the keeper of this court. Use `/start` when ready.", userOf(i).ID))
	}
}

func (b *Bot) cmdStart(ctx context.Context, i *discordgo.InteractionCreate) {
	c, err := b.store.GetCourt(ctx, i.GuildID)
	if errors.Is(err, store.ErrNotFound) {
		b.reply(i, "No admin yet. Someone needs to `/claim-admin` first.")
		return
	} else if err != nil {
		b.fail(i, err)
		return
	}
	if c.AdminUserID != userOf(i).ID {
		b.reply(i, "Only the admin can start the game.")
		return
	}
	if c.Status == "running" {
		b.reply(i, fmt.Sprintf("The game is already running. Announcements go to <#%s>.", c.AnnouncementsChannelID))
		return
	}
	if c.Status == "fallen" {
		b.reply(i, "The realm has fallen and this game is over. Starting a new one isn't built yet.")
		return
	}
	announce := i.ChannelID
	if id := optString(i, "announcements"); id != "" {
		announce = id
	}
	if err := b.store.StartCourt(ctx, i.GuildID, announce); err != nil {
		b.fail(i, err)
		return
	}
	kind := game.SenateStandard
	if optString(i, "senate") == string(game.SenateRandom) {
		kind = game.SenateRandom
	}
	st, err := b.ensureRealmWith(ctx, i.GuildID, time.Now(), kind)
	if err != nil {
		b.fail(i, err)
		return
	}
	b.respond(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "👑 The court is assembling",
			Description: "The Republic's great families gather once more. Make your name among them.\n\n" +
				"Press **Join the court** to create your character: your name and family, your crest, your strengths, " +
				"your story, and your place at court.",
			Color: colorPanel,
			Fields: []*discordgo.MessageEmbedField{
				fact("📣 Announcements", fmt.Sprintf("<#%s>", announce)),
				fact(emojiSenate+" The Senate first meets", relTime(st.NextMatterAt)),
				fact(emojiSenate+" The Senate", senateKindName(kind)),
			},
			Footer: footer("You can also use /join. New here? /help explains everything."),
		}},
		Components: []discordgo.MessageComponent{row(withEmoji(button("Join the court", idCreatorStart, discordgo.SuccessButton, false), "👑"))},
	})
}

// myCharacter finds the caller's character. In a server it's that server's
// court; in DMs it's their only court.
func (b *Bot) myCharacter(ctx context.Context, i *discordgo.InteractionCreate) (store.Character, bool) {
	uid := userOf(i).ID
	if i.GuildID != "" {
		c, err := b.store.GetCharacter(ctx, i.GuildID, uid)
		if errors.Is(err, store.ErrNotFound) {
			b.reply(i, "You have no character yet. Pick a role from the game's start message.")
			return c, false
		} else if err != nil {
			b.fail(i, err)
			return c, false
		}
		return c, true
	}
	chars, err := b.store.CharactersForUser(ctx, uid)
	switch {
	case err != nil:
		b.fail(i, err)
	case len(chars) == 0:
		b.reply(i, "You have no character yet. Join a court in a server first.")
	case len(chars) > 1:
		b.reply(i, "You're in more than one court. Use this command in that court's server.")
	default:
		return chars[0], true
	}
	return store.Character{}, false
}

func modalValues(d discordgo.ModalSubmitInteractionData) map[string]string {
	out := map[string]string{}
	for _, row := range d.Components {
		r, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, comp := range r.Components {
			if ti, ok := comp.(*discordgo.TextInput); ok {
				out[ti.CustomID] = ti.Value
			}
		}
	}
	return out
}

// ---- response helpers ----

func ephemeral(d *discordgo.InteractionResponseData) *discordgo.InteractionResponseData {
	d.Flags = discordgo.MessageFlagsEphemeral
	return d
}

// deferUpdate acknowledges a component click at once, for handlers about
// to do slow work (DMs, announcements) before they can answer: Discord
// wants an answer within 3 seconds. The handler's next update or respond
// then edits the message or posts a follow-up instead.
func (b *Bot) deferUpdate(i *discordgo.InteractionCreate) {
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	}); err != nil {
		slog.Error("defer response", "err", err)
		return
	}
	b.deferred.Store(i.ID, true)
}

func (b *Bot) send(i *discordgo.InteractionCreate, t discordgo.InteractionResponseType, d *discordgo.InteractionResponseData) {
	// Send empty lists rather than null so Discord clears them.
	if d.Embeds == nil {
		d.Embeds = []*discordgo.MessageEmbed{}
	}
	if d.Components == nil {
		d.Components = []discordgo.MessageComponent{}
	}
	// An updated message keeps its old pictures unless told otherwise: keep
	// none, so only this view's files (if any) are on it.
	if t == discordgo.InteractionResponseUpdateMessage && d.Attachments == nil {
		d.Attachments = &[]*discordgo.MessageAttachment{}
	}
	if _, ok := b.deferred.LoadAndDelete(i.ID); ok {
		b.sendDeferred(i, t, d)
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: t, Data: d}); err != nil {
		slog.Error("respond", "err", err)
	}
}

// sendDeferred answers an interaction already acknowledged by deferUpdate.
func (b *Bot) sendDeferred(i *discordgo.InteractionCreate, t discordgo.InteractionResponseType, d *discordgo.InteractionResponseData) {
	var err error
	if t == discordgo.InteractionResponseUpdateMessage {
		_, err = b.s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &d.Content, Embeds: &d.Embeds, Components: &d.Components, Files: d.Files, Attachments: d.Attachments,
		})
	} else {
		_, err = b.s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: d.Content, Embeds: d.Embeds, Components: d.Components, Flags: d.Flags, Files: d.Files,
		})
	}
	if err != nil {
		slog.Error("respond after defer", "err", err)
	}
}

// respond posts a new message.
func (b *Bot) respond(i *discordgo.InteractionCreate, d *discordgo.InteractionResponseData) {
	b.send(i, discordgo.InteractionResponseChannelMessageWithSource, d)
}

// update replaces the message the clicked component is on.
func (b *Bot) update(i *discordgo.InteractionCreate, d *discordgo.InteractionResponseData) {
	b.send(i, discordgo.InteractionResponseUpdateMessage, d)
}

// reply answers privately with a short note.
func (b *Bot) reply(i *discordgo.InteractionCreate, msg string) {
	b.respond(i, ephemeral(noteView(msg, colorMuted)))
}

// replyDone answers privately that something worked.
func (b *Bot) replyDone(i *discordgo.InteractionCreate, msg string) {
	b.respond(i, ephemeral(noteView("✅ "+msg, colorGood)))
}

func (b *Bot) replyPublic(i *discordgo.InteractionCreate, msg string) {
	b.respond(i, noteView(msg, colorPanel))
}

func (b *Bot) fail(i *discordgo.InteractionCreate, err error) {
	slog.Error("interaction failed", "err", err)
	b.reply(i, emojiWarn+" Something went wrong. Try again in a moment.")
}

func senateKindName(k game.SenateKind) string {
	if k == game.SenateRandom {
		return "Drawn at random"
	}
	return "The standard Senate"
}
