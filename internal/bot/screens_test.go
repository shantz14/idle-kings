package bot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
)

func click(guild, clicker, owner, id string, flags discordgo.MessageFlags) *discordgo.InteractionCreate {
	msg := &discordgo.Message{Flags: flags}
	if owner != "" {
		msg.InteractionMetadata = &discordgo.MessageInteractionMetadata{User: &discordgo.User{ID: owner}}
	}
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type: discordgo.InteractionMessageComponent, GuildID: guild, Message: msg,
		Member: &discordgo.Member{User: &discordgo.User{ID: clicker}},
		User:   &discordgo.User{ID: clicker},
		Data:   discordgo.MessageComponentInteractionData{CustomID: id},
	}}
}

func TestPublicScreens(t *testing.T) {
	if !publicMessage(click("g", "a", "a", "menu:home", 0)) {
		t.Error("a server message is public")
	}
	if publicMessage(click("g", "a", "a", "menu:home", discordgo.MessageFlagsEphemeral)) || publicMessage(click("", "a", "a", "menu:home", 0)) {
		t.Error("popups and DMs are private")
	}
	if _, locked := lockedOut(click("g", "b", "a", "menu:home", 0)); !locked {
		t.Error("someone else's public screen is locked")
	}
	for _, c := range []*discordgo.InteractionCreate{
		click("g", "a", "a", "menu:home", 0),                               // your own
		click("g", "b", "a", "cc:start", 0),                                // the Join button
		click("g", "b", "a", "deal:to:5", 0),                               // Make an offer
		click("g", "b", "", "party:attend:1", 0),                           // an announcement
		click("g", "b", "a", "menu:home", discordgo.MessageFlagsEphemeral), // a popup
	} {
		if _, locked := lockedOut(c); locked {
			t.Errorf("%s should be open to %s", c.MessageComponentData().CustomID, userOf(c).ID)
		}
	}
	d := screen(click("", "a", "a", "x", 0), &discordgo.InteractionResponseData{})
	if d.Flags&discordgo.MessageFlagsEphemeral == 0 {
		t.Error("screens in DMs stay private")
	}
	d = screen(click("g", "a", "a", "x", 0), ephemeral(&discordgo.InteractionResponseData{}))
	if d.Flags&discordgo.MessageFlagsEphemeral != 0 {
		t.Error("screens in a server are public")
	}
}

func TestSecretActions(t *testing.T) {
	for _, a := range game.Actions {
		if a.Category == game.CatIntrigue && !a.Secret {
			t.Errorf("%s is an intrigue action and should open privately", a.Key)
		}
	}
	for _, k := range []string{game.KindCourt, game.KindInvest} {
		if a, _ := game.ActionByKey(k); !a.Secret {
			t.Errorf("%s should open privately", k)
		}
	}
}
