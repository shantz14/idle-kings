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
//   estate:repair             (select) pay to repair a sabotaged property
//   estate:auction            (select) put a unique possession up for auction → auc:setup

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
	case parts[0] == "auction" && len(vals()) > 0:
		h, ok := c.Holding(atoi(vals()[0]))
		if it, iok := h.Item(); !ok || !iok || !game.Auctionable(it) {
			b.update(i, b.estatePageNote(c, "You can't auction that."))
			return
		}
		b.update(i, auctionSetupView(c, h, 1, 1, ""))
	case parts[0] == "repair" && len(vals()) > 0:
		h, ok := c.Holding(atoi(vals()[0]))
		it, _ := h.Item()
		cost, err := b.store.RepairItem(ctx, c.ID, h.ID)
		if !ok || err != nil {
			note := "There's nothing to repair."
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				note = err.Error()
			}
			b.update(i, b.estatePageNote(c, note))
			return
		}
		if fresh, err := b.store.GetCharacterByID(ctx, c.ID); err == nil {
			c = fresh
		}
		b.update(i, b.estatePageNote(c, fmt.Sprintf("🔨 Builders put **%s** to rights. (-%d gold)", it.Name, cost)))
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
		v := noteView("You don't own that any more.", colorMuted)
		v.Components = []discordgo.MessageComponent{row(backTo("estate:view"))}
		return v
	}
	desc := fmt.Sprintf("The shop will buy **%s** from you.", it.Name)
	if it.Magic && c.Laws.Has(game.LawWitchcraftAct) {
		desc += "\n🧙 Under the Witchcraft Act, the shop pays more for enchanted things: people are eager to be rid of them."
	}
	if it.Unique {
		desc += "\nIt goes back on the market, where anyone can buy it."
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "💰 Sell " + it.Name + "?", Description: desc, Color: colorBad,
			Fields: []*discordgo.MessageEmbedField{
				fact("You get", fmt.Sprintf("%d gold", it.SellBackUnder(c.Laws))),
				fact("It cost", fmt.Sprintf("%d gold", it.Price)),
			},
			Footer: footer(emojiDeal + " Or sell it to another player for any price you agree on, with Offer a deal.")}},
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
	if need := game.RenownNeeded(it); need > 0 {
		s += fmt.Sprintf(" · 👑 %s renown", game.Num(need))
	}
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
		Color: colorPanel,
	}
	desc := "A finer estate makes your parties more successful."
	if total := c.Effects().Describe(); total != "" {
		desc += "\n**All together:** " + total
	}
	e.Description = withNote(note, desc)
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Estate value", game.Num(c.EstateValue())),
		fact("Gold", fmt.Sprint(c.Wealth)),
		fact("Income", fmt.Sprintf("%d/day", c.TotalIncome())),
	}
	e.Footer = footer("🏡 Your home and possessions count fully; other properties count half, and only their income and stats apply.")

	home, _ := c.Residence()
	byKind := map[game.ItemKind][]string{}
	var moveOpts, sellOpts, repairOpts []discordgo.SelectMenuOption
	for _, h := range c.Holdings {
		it, ok := h.Item()
		if !ok {
			continue
		}
		line := itemLine(it)
		if h.ID == home.ID {
			line = "🏡" + strings.TrimPrefix(line, game.KindByKey(it.Kind).Emoji) + " · *your home*"
		}
		if h.Repair > 0 {
			line += fmt.Sprintf("\n> 🔥 *Sabotaged: it gives you nothing until it's repaired (%d gold).*", h.Repair)
			if len(repairOpts) < 25 {
				repairOpts = append(repairOpts, discordgo.SelectMenuOption{Label: truncate("Repair "+it.Name, 100), Value: fmt.Sprint(h.ID),
					Emoji: &discordgo.ComponentEmoji{Name: "🔨"}, Description: fmt.Sprintf("%d gold", h.Repair)})
			}
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
	if len(repairOpts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "estate:repair",
			Placeholder: "🔨 Repair what saboteurs damaged…", Options: repairOpts}))
	}
	if opts := auctionablePieces(c); len(opts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "estate:auction",
			Placeholder: "🔨 Put a unique piece up for auction…", Options: opts}))
	}
	btns := []discordgo.MessageComponent{
		withEmoji(button("Shop", "shop:open:"+shopProps, discordgo.PrimaryButton, false), emojiShop),
		withEmoji(button("Offer a deal", "deal:start", discordgo.PrimaryButton, false), emojiDeal),
		withEmoji(button("My Character", "menu:char", discordgo.SecondaryButton, false), emojiSheet),
	}
	if back {
		btns = append(btns, backTo("menu:home"))
	}
	comps = append(comps, row(btns...))
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
}
