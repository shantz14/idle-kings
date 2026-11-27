package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	registerActivity(game.KindParty, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.partyPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveParty,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("You are hosting a party. It ends <t:%d:R>.", a.EndsAt.Unix())
		},
		eventWhere: "At your party",
	})
	componentRoutes["menu"] = (*Bot).onMenu
}

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
		b.open(i, b.characterPage(ctx, c, true), true)
	case parts[0] == "estate":
		b.update(i, b.estatePage(c, true))
	case parts[0] == "cat" && len(parts) > 1:
		b.update(i, categoryPanel(game.Category(parts[1]), c.Role))
	case parts[0] == "act" && len(parts) > 1:
		if d, ok := activityDefs[parts[1]]; ok && d.panel != nil {
			a, _ := game.ActionByKey(parts[1])
			b.open(i, d.panel(b, ctx, c), a.Secret)
		}

	}
}

func (b *Bot) homePanel(ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
	var lines []string
	if c.RealmFallen {
		lines = append(lines, quote("🔥 **The realm has fallen.** "+c.FallenReason))
	}
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		lines = append(lines, "⏳ "+b.activityLine(ctx, a))
	} else {
		lines = append(lines, "You are free to act. Pick a category below.")
	}
	if a, err := b.store.WorkInProgress(ctx, c.ID); err == nil {
		lines = append(lines, "🏗️ "+b.activityLine(ctx, a))
	}
	if k, err := b.store.LedConspiracy(ctx, c.ID); err == nil {
		lines = append(lines, "🕸️ "+b.activityLine(ctx, k.Activity))
	}
	var cats []discordgo.MessageComponent
	for _, cat := range game.Categories {
		cats = append(cats, withEmoji(button(cat.Name, "menu:cat:"+string(cat.Key), discordgo.PrimaryButton, false), cat.Emoji))
	}
	e := panelEmbed(c.Crest.Emoji()+" "+c.FullName(), strings.Join(lines, "\n\n"))
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Influence", game.Num(c.Influence)),
		fact("Renown", game.Num(c.Renown)),
		fact("Gold", fmt.Sprintf("%d (+%d/day)", c.Wealth, c.TotalIncome())),
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{
			row(cats...),
			row(
				withEmoji(button("The Senate", "menu:senate", discordgo.SuccessButton, false), emojiSenate),
				withEmoji(button("My Character", "menu:char", discordgo.SecondaryButton, false), emojiSheet),
				withEmoji(button("Estate", "menu:estate", discordgo.SecondaryButton, false), emojiEstate),
			),
		},
	}
}

func categoryPanel(cat game.Category, role string) *discordgo.InteractionResponseData {
	info, ok := game.CategoryByKey(cat)
	if !ok {
		info = game.CategoryInfo{Key: cat, Name: string(cat)}
	}
	actions := game.ActionsFor(cat, role)
	var sb strings.Builder
	var btns []discordgo.MessageComponent
	for _, a := range actions {
		fmt.Fprintf(&sb, "%s **%s**%s\n%s\n\n", a.Emoji, a.Name, publicTag(a.Public), a.Description)
		btns = append(btns, withEmoji(button(a.Name, "menu:act:"+a.Key, discordgo.PrimaryButton, false), a.Emoji))
	}
	if len(actions) == 0 {
		sb.WriteString("Nothing here yet. Coming soon.")
	}
	btns = append(btns, backTo("menu:home"))
	e := panelEmbed(titled(info.Emoji, info.Name), strings.TrimSpace(sb.String()))
	if len(actions) > 0 {
		e.Footer = footer("📣 Public actions are announced to the court, and others can join in.")
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: rows(btns)}
}

// backButton returns to the menu category an action is listed under.
func backButton(kind string) discordgo.Button {
	a, _ := game.ActionByKey(kind)
	return backTo("menu:cat:" + string(a.Category))
}

// homeRow is a row with one Back button to the home panel.
func homeRow() discordgo.ActionsRow {
	return row(backTo("menu:home"))
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
		return " · 📣"
	}
	return ""
}

// goalList is a bulleted list of goals to pick from.
func goalList(goals []game.GoalInfo) string {
	var sb strings.Builder
	for _, g := range goals {
		fmt.Fprintf(&sb, "• **%s**: %s\n", g.Name, g.Description)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// partyPanel shows Throw a Party. note is an optional status line.
func (b *Bot) partyPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	sb.WriteString("Open your estate to the court for an evening. It goes best with high influence, then a fine estate, diplomacy and renown.\n\n")
	sb.WriteString("**Choose your goal**\n" + goalList(game.HostGoals))

	busy := false
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		busy = true
		note = joinNotes("⏳ "+b.activityLine(ctx, a), note)
	} else if c.Wealth < game.PartyMinSpend {
		busy = true
		note = joinNotes(fmt.Sprintf("You only have %s.", goldText(c.Wealth)), note)
	}
	note = joinNotes(b.happeningNote(ctx, c.GuildID, game.MeetParty), permitNote(c.Laws), note)

	var btns []discordgo.MessageComponent
	for _, g := range game.HostGoals {
		btns = append(btns, button(g.Name, "party:host:"+string(g.Key), discordgo.PrimaryButton, busy))
	}
	btns = append(btns, backButton(game.KindParty))
	e := actionEmbed(game.KindParty, withNote(note, sb.String()))
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Cost", fmt.Sprintf("%d+ gold", game.PartyMinSpend)),
		fact("Lasts", humanDuration(b.timing.Duration)),
		fact("Guests", "📣 Anyone may attend"),
	}
	e.Footer = footer("🔔 Something usually goes wrong during the evening. I'll DM you to decide what to do.")
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}
