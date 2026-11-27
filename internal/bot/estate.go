package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	componentRoutes["estate"] = (*Bot).onEstate
}

// Estate custom IDs:
//   estate:view               the estate page
//   estate:move               (select) move into a property
//   estate:movein:<holding>   move into a property just bought
//   estate:sell               (select) pick something to sell to the shop
//   estate:sellok:<holding>   sale confirmed

func (b *Bot) onEstate(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	vals := func() []string {
		if i.Type == discordgo.InteractionMessageComponent {
			return i.MessageComponentData().Values
		}
		return nil
	}
	switch {
	case parts[0] == "view":
		b.update(i, b.estatePage(c, true))
	case parts[0] == "move" && len(vals()) > 0:
		b.moveIn(ctx, i, c, atoi(vals()[0]))
	case parts[0] == "movein" && len(parts) > 1:
		b.moveIn(ctx, i, c, atoi(parts[1]))
	case parts[0] == "sell" && len(vals()) > 0:
		b.update(i, sellConfirm(c, atoi(vals()[0])))
	case parts[0] == "sellok" && len(parts) > 1:
		b.sellToShop(ctx, i, c, atoi(parts[1]))
	}
}

func (b *Bot) moveIn(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, holdingID int64) {
	h, ok := c.Holding(holdingID)
	if !ok {
		b.update(i, b.estatePageNote(c, "You don't own that any more."))
		return
	}
	it, _ := h.Item()
	if err := b.store.MoveResidence(ctx, c.ID, holdingID); err != nil {
		b.update(i, b.estatePageNote(c, estateErr(err)))
		return
	}
	if fresh, err := b.store.GetCharacterByID(ctx, c.ID); err == nil {
		c = fresh
	}
	b.update(i, b.estatePageNote(c, fmt.Sprintf("You move your household into **%s**.", it.Name)))
}

func (b *Bot) sellToShop(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, holdingID int64) {
	h, ok := c.Holding(holdingID)
	it, known := h.Item()
	if !ok || !known {
		b.update(i, b.estatePageNote(c, "You don't own that any more."))
		return
	}
	if err := b.store.SellToShop(ctx, c.ID, holdingID, it.SellBackUnder(c.Laws)); err != nil {
		b.update(i, b.estatePageNote(c, estateErr(err)))
		return
	}
	if fresh, err := b.store.GetCharacterByID(ctx, c.ID); err == nil {
		c = fresh
	}
	b.update(i, b.estatePageNote(c, fmt.Sprintf("You sold **%s** to the shop for **%d gold**.", it.Name, it.SellBackUnder(c.Laws))))
}

// estateErr turns store errors into something to tell the player.
func estateErr(err error) string {
	for _, known := range []error{store.ErrOwned, store.ErrNotOwned, store.ErrResidence, store.ErrLastProperty,
		store.ErrNotProperty, store.ErrPartyHome, store.ErrStale, store.ErrHandled} {
		if errors.Is(err, known) {
			s := known.Error()
			return strings.ToUpper(s[:1]) + s[1:] + "."
		}
	}
	if errors.Is(err, store.ErrEnded) {
		return "That offer has expired."
	}
	return err.Error()
}

func sellConfirm(c store.Character, holdingID int64) *discordgo.InteractionResponseData {
	h, ok := c.Holding(holdingID)
	it, known := h.Item()
	if !ok || !known {
		return &discordgo.InteractionResponseData{Content: "You don't own that any more.",
			Components: []discordgo.MessageComponent{row(button("Back", "estate:view", discordgo.SecondaryButton, false))}}
	}
	desc := fmt.Sprintf("The shop will give you **%d gold** for **%s** (of its %d gold price).",
		it.SellBackUnder(c.Laws), it.Name, it.Price)
	if it.Magic && c.Laws.Has(game.LawWitchcraftAct) {
		desc += "\nUnder the Witchcraft Act, the shop pays more for enchanted things: people are eager to be rid of them."
	}
	if it.Unique {
		desc += "\nIt goes back on the market, where anyone can buy it."
	}
	desc += "\n\nOr sell it to another player for any price you agree on, with **Offer a deal**."
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Sell " + it.Name + "?", Description: desc, Color: colorGold}},
		Components: []discordgo.MessageComponent{row(
			button(fmt.Sprintf("Sell for %d gold", it.SellBackUnder(c.Laws)), fmt.Sprintf("estate:sellok:%d", h.ID), discordgo.DangerButton, false),
			button("Cancel", "estate:view", discordgo.SecondaryButton, false),
		)},
	}
}

// itemLine is one item on the estate page or in the shop.
func itemLine(it game.Item) string {
	s := fmt.Sprintf("%s **%s**", game.KindByKey(it.Kind).Emoji, it.Name)
	if it.IsProperty() && it.Tier != 0 {
		s += fmt.Sprintf(" (%s)", it.Tier)
	}
	s += " · value " + game.Num(it.Value)
	if fx := it.Effects.Describe(); fx != "" {
		s += " · " + fx
	}
	return s
}

// joinLines joins lines up to max characters, noting how many were left out.
func joinLines(lines []string, max int) string { return joinSep(lines, "\n", max) }

// joinSep joins lines with sep, stopping with "…and N more" before max bytes.
func joinSep(lines []string, sep string, max int) string {
	var sb strings.Builder
	for n, l := range lines {
		more := fmt.Sprintf("%s…and %d more", sep, len(lines)-n)
		if sb.Len()+len(l)+len(sep)+len(more) > max {
			sb.WriteString(more)
			break
		}
		if n > 0 {
			sb.WriteString(sep)
		}
		sb.WriteString(l)
	}
	return sb.String()
}

// shopLine is an item as the shop lists it: name and price, then what it
// does on a second line.
func shopLine(it game.Item, note string, laws game.Laws) string {
	s := fmt.Sprintf("%s **%s**", game.KindByKey(it.Kind).Emoji, it.Name)
	if it.IsProperty() && it.Tier != 0 {
		s += fmt.Sprintf(" (%s)", it.Tier)
	}
	s += fmt.Sprintf(" — **%d gold**", it.PriceUnder(laws))
	if note != "" {
		s += " · *" + note + "*"
	}
	s += "\n> value " + game.Num(it.Value)
	if fx := it.Effects.Describe(); fx != "" {
		s += " · " + fx
	}
	return s
}

func (b *Bot) estatePage(c store.Character, back bool) *discordgo.InteractionResponseData {
	return estateView(c, back, "")
}

func (b *Bot) estatePageNote(c store.Character, note string) *discordgo.InteractionResponseData {
	return estateView(c, true, note)
}

// estateView lists everything a character owns, grouped by kind, with ways
// to move house, sell, shop and trade.
func estateView(c store.Character, back bool, note string) *discordgo.InteractionResponseData {
	e := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%s · the estate of %s", c.House(), c.FullName()),
		Color: colorGold,
	}
	desc := fmt.Sprintf("Estate value **%s**. A finer estate makes your parties more successful. "+
		"Your home and possessions count fully; other properties count half, and only their income and stats apply.",
		game.Num(c.EstateValue()))
	if total := c.Effects().Describe(); total != "" {
		desc += "\nAll together: " + total
	}
	desc += fmt.Sprintf("\nYou have **%d gold**.", c.Wealth)
	if note != "" {
		desc += "\n\n" + note
	}
	e.Description = desc

	home, _ := c.Residence()
	byKind := map[game.ItemKind][]string{}
	var moveOpts, sellOpts []discordgo.SelectMenuOption
	for _, h := range c.Holdings {
		it, ok := h.Item()
		if !ok {
			continue
		}
		line := itemLine(it)
		if h.ID == home.ID {
			line = "🏡 " + line + " · *your home*"
		}
		byKind[it.Kind] = append(byKind[it.Kind], line)
		if h.ID == home.ID {
			continue
		}
		opt := discordgo.SelectMenuOption{Label: truncate(it.Name, 100), Value: fmt.Sprint(h.ID),
			Description: truncate(fmt.Sprintf("%s · value %s · sells for %d", game.KindByKey(it.Kind).Name, game.Num(it.Value), it.SellBackUnder(c.Laws)), 100)}
		if it.IsProperty() && len(moveOpts) < 25 {
			moveOpts = append(moveOpts, opt)
		}
		if len(sellOpts) < 25 {
			sellOpts = append(sellOpts, opt)
		}
	}
	// Properties and estate goods by kind; the armory's weapons, armor and
	// mounts together in one field.
	var armory []string
	for _, k := range game.Kinds {
		lines := byKind[k.Kind]
		switch {
		case len(lines) == 0:
		case k.Section == game.SectionArmory:
			armory = append(armory, lines...)
		default:
			e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: k.Emoji + " " + k.Name, Value: joinLines(lines, 1000)})
		}
	}
	if len(armory) > 0 {
		sec, _ := game.SectionByKey(game.SectionArmory)
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: sec.Emoji + " " + sec.Name, Value: joinLines(armory, 1000)})
	}
	if f := venturesField(c, true); f != nil {
		e.Fields = append(e.Fields, f)
	}

	var comps []discordgo.MessageComponent
	if len(moveOpts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "estate:move",
			Placeholder: "Move into another property…", Options: moveOpts}))
	}
	if len(sellOpts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "estate:sell",
			Placeholder: "Sell something to the shop…", Options: sellOpts}))
	}
	btns := []discordgo.MessageComponent{
		button("Shop", "shop:open:"+shopProps, discordgo.SuccessButton, false),
		button("Offer a deal", "deal:start", discordgo.PrimaryButton, false),
		button("My Character", "menu:char", discordgo.SecondaryButton, false),
	}
	if back {
		btns = append(btns, button("Back", "menu:home", discordgo.SecondaryButton, false))
	}
	comps = append(comps, row(btns...))
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
}
