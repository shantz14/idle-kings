package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Menu custom IDs:
//   menu:home            main panel
//   menu:cat:<category>  actions in a category
//   menu:act:<action>    one action's panel
//   menu:char            character sheet
//   menu:estate          estate page
//   menu:senate          the Senate panel

func button(label, id string, style discordgo.ButtonStyle, disabled bool) discordgo.Button {
	return discordgo.Button{Label: label, CustomID: id, Style: style, Disabled: disabled}
}

func row(btns ...discordgo.MessageComponent) discordgo.ActionsRow {
	return discordgo.ActionsRow{Components: btns}
}

func (b *Bot) onMenu(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "home":
		b.update(i, b.homePanel(ctx, c))
	case parts[0] == "senate":
		b.update(i, b.senatePanel(ctx, c))
	case parts[0] == "char":
		b.update(i, b.characterPage(ctx, c, true))
	case parts[0] == "estate":
		b.update(i, b.estatePage(c, true))
	case parts[0] == "cat" && len(parts) > 1:
		b.update(i, categoryPanel(game.Category(parts[1]), c.Role))
	case parts[0] == "act" && len(parts) > 1:
		switch parts[1] {
		case game.KindParty:
			b.update(i, b.partyPanel(ctx, c, ""))
		case game.KindCourt:
			b.update(i, b.courtPanel(ctx, c, 0, ""))
		case game.KindDuel:
			b.update(i, b.duelPanel(ctx, c, ""))
		case game.KindHunt:
			b.update(i, b.huntPanel(ctx, c, ""))
		case game.KindJourney:
			b.update(i, b.journeyPanel(ctx, c, ""))
		case game.KindInvest:
			b.update(i, b.investPanel(ctx, c, ""))
		case game.KindPlantSpies:
			b.update(i, b.plantPanel(ctx, c, ""))
		case game.KindRootOut:
			b.update(i, b.rootOutPanel(ctx, c, ""))
		case game.KindEmbezzle:
			b.update(i, b.embezzlePanel(ctx, c, ""))
		case game.KindDossier:
			b.update(i, b.dossierPanel(ctx, c))
		case game.KindAid:
			b.update(i, b.aidPanel(ctx, c, ""))
		case game.KindCanvass:
			b.update(i, b.canvassPanel(ctx, c, ""))
		case game.KindSpeech:
			b.update(i, b.speechPanel(ctx, c, ""))
		case game.KindCircus:
			b.update(i, b.circusPanel(ctx, c, ""))
		case game.KindWork:
			b.update(i, b.worksPanel(ctx, c, ""))
		case game.KindPetition:
			b.update(i, b.petitionPanel(ctx, c, "", ""))
		}
	}
}

func (b *Bot) homePanel(ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
	desc := standingLine(c)
	if c.RealmFallen {
		desc = "**The realm has fallen.** " + c.FallenReason + "\n\n" + desc
	}
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		desc += "\n\n" + b.activityLine(ctx, a)
	}
	if a, err := b.store.WorkInProgress(ctx, c.ID); err == nil {
		desc += "\n\n" + b.activityLine(ctx, a)
	}
	var cats []discordgo.MessageComponent
	for _, cat := range game.Categories {
		cats = append(cats, button(cat.Name, "menu:cat:"+string(cat.Key), discordgo.PrimaryButton, false))
	}
	return ephemeral(&discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: c.Crest.Emoji() + " " + c.FullName(), Description: desc, Color: colorGold}},
		Components: []discordgo.MessageComponent{
			row(cats...),
			row(
				button("The Senate", "menu:senate", discordgo.SuccessButton, false),
				button("My Character", "menu:char", discordgo.SecondaryButton, false),
				button("Estate", "menu:estate", discordgo.SecondaryButton, false),
			),
		},
	})
}

func categoryPanel(cat game.Category, role string) *discordgo.InteractionResponseData {
	name := string(cat)
	for _, ci := range game.Categories {
		if ci.Key == cat {
			name = ci.Name
		}
	}
	actions := game.ActionsFor(cat, role)
	var sb strings.Builder
	var btns []discordgo.MessageComponent
	for _, a := range actions {
		fmt.Fprintf(&sb, "**%s**%s\n%s\n\n", a.Name, publicTag(a.Public), a.Description)
		btns = append(btns, button(a.Name, "menu:act:"+a.Key, discordgo.PrimaryButton, false))
	}
	if len(actions) == 0 {
		sb.WriteString("Nothing here yet. Coming soon.")
	}
	btns = append(btns, button("Back", "menu:home", discordgo.SecondaryButton, false))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: name, Description: sb.String(), Color: colorGold}},
		Components: rows(btns),
	}
}

// rows lays buttons out five to a row, as Discord allows.
func rows(btns []discordgo.MessageComponent) []discordgo.MessageComponent {
	var out []discordgo.MessageComponent
	for len(btns) > 0 {
		n := min(5, len(btns))
		out = append(out, row(btns[:n]...))
		btns = btns[n:]
	}
	return out
}

func publicTag(public bool) string {
	if public {
		return " · *public*"
	}
	return ""
}

// partyPanel shows Throw a Party. note is an optional status line.
func (b *Bot) partyPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Open your estate to the court for %s. Costs **%d gold** or more: you choose how much to spend after picking a goal.\n",
		humanDuration(b.timing.Duration), game.PartyMinSpend)
	sb.WriteString("Its success depends most on your influence, then your estate, your diplomacy, and your renown. ")
	sb.WriteString("Something will probably go wrong during the evening; I'll DM you when it does.\n")
	sb.WriteString("The court is told, and others may attend.\n\n**Choose your goal:**\n")
	for _, g := range game.HostGoals {
		fmt.Fprintf(&sb, "**%s** — %s\n", g.Name, g.Description)
	}

	busy := false
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		busy = true
		sb.WriteString("\n" + b.activityLine(ctx, a))
	} else if c.Wealth < game.PartyMinSpend {
		busy = true
		fmt.Fprintf(&sb, "\nYou only have %d gold.", c.Wealth)
	}
	note = joinNotes(permitNote(c.Laws), note)
	if note != "" {
		sb.WriteString("\n\n" + note)
	}

	var btns []discordgo.MessageComponent
	for _, g := range game.HostGoals {
		btns = append(btns, button(g.Name, "party:host:"+string(g.Key), discordgo.SuccessButton, busy))
	}
	btns = append(btns, button("Back", "menu:cat:"+string(game.CatDiplomacy), discordgo.SecondaryButton, false))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Throw a Party", Description: sb.String(), Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}
