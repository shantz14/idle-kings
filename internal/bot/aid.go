package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Aid the Realm custom IDs:
//   aid:t:<target>               amounts for one tracker
//   aid:give:<target>:<gold>     give a preset amount
//   aid:custom:<target>          open the custom amount popup
//   aid:amount:<target>          (modal) custom amount submitted

func (b *Bot) onAid(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || len(parts) < 2 {
		return
	}
	switch parts[0] {
	case "t":
		b.update(i, b.aidAmountPanel(ctx, c, parts[1], ""))
	case "give":
		if len(parts) > 2 {
			b.onAidGive(ctx, i, c, parts[1], int(atoi(parts[2])))
		}
	case "custom":
		if err := b.s.InteractionRespond(i.Interaction, amountModal("aid:amount:"+parts[1], "How much will you give?", game.AidMin, game.AidMax)); err != nil {
			slog.Error("aid modal", "err", err)
		}
	}
}

func (b *Bot) onAidModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || len(parts) < 2 || parts[0] != "amount" {
		return
	}
	gold, ok := modalAmount(i)
	if !ok || gold < game.AidMin || gold > game.AidMax {
		b.update(i, b.aidAmountPanel(ctx, c, parts[1], fmt.Sprintf("Give a whole number of gold, from %d to %d.", game.AidMin, game.AidMax)))
		return
	}
	b.onAidGive(ctx, i, c, parts[1], gold)
}

func (b *Bot) aidPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	st, err := b.store.GetRealm(ctx, c.GuildID)
	if err != nil {
		return &discordgo.InteractionResponseData{Content: "The Senate hasn't met yet."}
	}
	return aidView(st, note)
}

func aidView(st store.RealmState, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	sb.WriteString("Pay from your own purse to shore up the Republic. It happens at once, and doesn't keep you from other business.\n\n")
	sb.WriteString("The court honors generosity, all the more when the realm is in trouble. Large gifts are announced.\n\n")
	sb.WriteString("**The realm**\n" + trackersLine(st.Trackers))
	if !st.Alive() {
		sb.WriteString("\n\nThe realm has fallen. There is nothing left to save.")
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	var btns []discordgo.MessageComponent
	for _, t := range game.AidTargets {
		btns = append(btns, button(t.Name, "aid:t:"+t.Key, discordgo.SuccessButton, !st.Alive()))
	}
	btns = append(btns, button("Back", "menu:cat:"+string(game.CatMonetary), discordgo.SecondaryButton, false))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Aid the Realm", Description: sb.String(), Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func (b *Bot) aidAmountPanel(ctx context.Context, c store.Character, key, note string) *discordgo.InteractionResponseData {
	t, ok := game.AidTargetByKey(key)
	st, err := b.store.GetRealm(ctx, c.GuildID)
	if !ok || err != nil {
		return b.aidPanel(ctx, c, "")
	}
	return aidAmountView(c, t, st.Trackers[t.Tracker], note)
}

func aidAmountView(c store.Character, t game.AidTarget, current float64, note string) *discordgo.InteractionResponseData {
	desc := fmt.Sprintf("The realm's %s stands at **%s**.", strings.ToLower(trackerLabel(t.Tracker)), game.Num(current))
	if current < game.AidCrisis {
		desc += " It's in trouble: a gift now will be remembered."
	}
	desc += fmt.Sprintf("\n\nHow much will you give? You have **%d gold**.", c.Wealth)
	if note != "" {
		desc += "\n\n" + note
	}
	var btns []discordgo.MessageComponent
	for _, g := range game.AidAmounts {
		btns = append(btns, button(fmt.Sprintf("%d gold", g), fmt.Sprintf("aid:give:%s:%d", t.Key, g), discordgo.SuccessButton, g > c.Wealth))
	}
	btns = append(btns, button("Other amount", "aid:custom:"+t.Key, discordgo.PrimaryButton, c.Wealth < game.AidMin))
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: t.Name, Description: desc, Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...),
			row(button("Back", "menu:act:"+game.KindAid, discordgo.SecondaryButton, false))},
	}
}

func trackerLabel(a game.Attr) string {
	for _, t := range game.Trackers {
		if t.Key == a {
			return t.Name
		}
	}
	return string(a)
}

func (b *Bot) onAidGive(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, key string, gold int) {
	t, ok := game.AidTargetByKey(key)
	if !ok || gold < game.AidMin || gold > game.AidMax {
		return
	}
	before, ok := b.realmBefore(ctx, c.GuildID)
	sen, err := b.store.Senators(ctx, c.GuildID)
	if !ok || err != nil {
		b.update(i, b.aidPanel(ctx, c, "The Senate hasn't met yet."))
		return
	}
	res := game.ResolveAid(c.Person(), t, gold, before.Trackers[t.Tracker], sen)
	err = b.store.ApplyAid(ctx, c.ID, c.GuildID, gold, game.Realm{t.Tracker: res.Points}, res.Delta, res.Opinions)
	switch {
	case errors.Is(err, store.ErrEnded):
		b.update(i, b.aidPanel(ctx, c, ""))
		return
	case err != nil:
		b.update(i, b.aidAmountPanel(ctx, c, key, err.Error()))
		return
	}
	desc := res.Text
	if d := res.Delta.Describe(); d != "" {
		desc += fmt.Sprintf(" (%s)", d)
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: t.Name, Description: desc, Color: colorGreen}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:act:"+game.KindAid, discordgo.SecondaryButton, false))},
	})
	if res.Public != "" {
		if court, err := b.store.GetCourt(ctx, c.GuildID); err == nil && court.AnnouncementsChannelID != "" {
			_, _ = b.s.ChannelMessageSendEmbed(court.AnnouncementsChannelID, &discordgo.MessageEmbed{
				Title: "A gift to the Republic", Description: res.Public, Color: colorGreen,
			})
		}
	}
	b.afterRealmChange(ctx, c.GuildID, before)
}
