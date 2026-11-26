package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
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
		id := i.MessageComponentData().CustomID
		parts := strings.Split(id, ":")
		switch parts[0] {
		case "cc":
			b.onCreator(ctx, i, parts[1:])
		case "info":
			b.onInfo(ctx, i, parts[1:])
		case "menu":
			b.onMenu(ctx, i, parts[1:])
		case "party":
			b.onParty(ctx, i, parts[1:])
		case "court":
			b.onCourt(ctx, i, parts[1:])
		case "duel":
			b.onDuel(ctx, i, parts[1:])
		case "hunt":
			b.onHunt(ctx, i, parts[1:])
		case "journey":
			b.onJourney(ctx, i, parts[1:])
		case "invest":
			b.onInvest(ctx, i, parts[1:])
		case "canvass":
			b.onCanvass(ctx, i, parts[1:])
		case "speech":
			b.onSpeech(ctx, i, parts[1:])
		case "circus":
			b.onCircus(ctx, i, parts[1:])
		case "work":
			b.onWork(ctx, i, parts[1:])
		case "spy":
			b.onSpy(ctx, i, parts[1:])
		case "embezzle":
			b.onEmbezzle(ctx, i, parts[1:])
		case "aid":
			b.onAid(ctx, i, parts[1:])
		case "estate":
			b.onEstate(ctx, i, parts[1:])
		case "shop":
			b.onShop(ctx, i, parts[1:])
		case "deal":
			b.onDeal(ctx, i, parts[1:])
		case "senate":
			b.onSenate(ctx, i, parts[1:])
		case "petition":
			b.onPetition(ctx, i, parts[1:])
		case "law":
			b.onLaw(ctx, i, parts[1:])
		case "dossier":
			b.onDossier(ctx, i, parts[1:])
		}
	case discordgo.InteractionModalSubmit:
		parts := strings.Split(i.ModalSubmitData().CustomID, ":")
		switch parts[0] {
		case "cc":
			b.onCreatorModal(ctx, i, parts[1:])
		case "senate":
			b.onTradeModal(ctx, i, parts[1:])
		case "party":
			b.onPartyModal(ctx, i, parts[1:])
		case "duel":
			b.onDuelModal(ctx, i, parts[1:])
		case "invest":
			b.onInvestModal(ctx, i, parts[1:])
		case "circus":
			b.onCircusModal(ctx, i, parts[1:])
		case "deal":
			b.onDealModal(ctx, i, parts[1:])
		case "aid":
			b.onAidModal(ctx, i, parts[1:])
		}
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
	st, err := b.ensureRealm(ctx, i.GuildID, time.Now())
	if err != nil {
		b.fail(i, err)
		return
	}
	b.respond(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "The court is assembling",
			Description: "The Republic's great families gather once more. Make your name among them.\n\n" +
				"Press **Join the court** to create your character: your name and family, your crest, your strengths, " +
				"your story, and your place at court. (You can also use `/join`.)",
			Color: colorGold,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Announcements", Value: fmt.Sprintf("<#%s>", announce), Inline: true},
				{Name: "The Senate first meets", Value: fmt.Sprintf("<t:%d:R>", st.NextMatterAt.Unix()), Inline: true},
			},
		}},
		Components: []discordgo.MessageComponent{row(button("Join the court", idCreatorStart, discordgo.SuccessButton, false))},
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

func (b *Bot) send(i *discordgo.InteractionCreate, t discordgo.InteractionResponseType, d *discordgo.InteractionResponseData) {
	// Send empty lists rather than null so Discord clears them.
	if d.Embeds == nil {
		d.Embeds = []*discordgo.MessageEmbed{}
	}
	if d.Components == nil {
		d.Components = []discordgo.MessageComponent{}
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: t, Data: d}); err != nil {
		slog.Error("respond", "err", err)
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

func (b *Bot) reply(i *discordgo.InteractionCreate, msg string) {
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{Content: msg}))
}

func (b *Bot) replyPublic(i *discordgo.InteractionCreate, msg string) {
	b.respond(i, &discordgo.InteractionResponseData{Content: msg})
}

func (b *Bot) fail(i *discordgo.InteractionCreate, err error) {
	slog.Error("interaction failed", "err", err)
	b.reply(i, "Something went wrong.")
}
