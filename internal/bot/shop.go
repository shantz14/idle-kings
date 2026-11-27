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
	componentRoutes["shop"] = (*Bot).onShop
}

// Shop custom IDs:
//   shop:open:<tab>     the shop, on a tab (a game.Section: props | goods | armory)
//   shop:pick:<tab>     (select) an item → its detail view
//   shop:buy:<key>      buy it

const (
	shopProps  = string(game.SectionProperties)
	shopGoods  = string(game.SectionGoods)
	shopArmory = string(game.SectionArmory)
)

func (b *Bot) onShop(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || len(parts) < 2 {
		return
	}
	switch parts[0] {
	case "open":
		b.update(i, b.shopPanel(ctx, c, parts[1], ""))
	case "pick":
		if vals := i.MessageComponentData().Values; len(vals) > 0 {
			b.update(i, b.itemPanel(ctx, c, vals[0], ""))
		}
	case "buy":
		b.onBuy(ctx, i, c, parts[1])
	}
}

// shopState is what the shop panels need: today's stock and who owns what.
type shopState struct {
	period int64
	ends   time.Time
	stock  []game.Item // fixed shelf, then today's market
	market map[string]bool
	owners map[string]int64 // unique item key → owner
	houses map[int64]string // character → "House X"
}

func (b *Bot) loadShop(ctx context.Context, guildID string) (shopState, error) {
	st := shopState{market: map[string]bool{}, houses: map[int64]string{}}
	st.period, st.ends = game.MarketPeriod(time.Now(), b.t.Shop)
	st.stock = game.FixedShelf()
	for _, it := range game.Market(guildID, st.period) {
		st.stock = append(st.stock, it)
		st.market[it.Key] = true
	}
	var err error
	if st.owners, err = b.store.ItemOwners(ctx, guildID); err != nil {
		return st, err
	}
	ids := make([]int64, 0, len(st.owners))
	for _, id := range st.owners {
		ids = append(ids, id)
	}
	names, err := b.store.CharacterNames(ctx, ids)
	if err != nil {
		return st, err
	}
	for id, n := range names {
		st.houses[id] = "House " + n.Family
	}
	return st, nil
}

// ownerNote says who has a unique piece, if anyone.
func (st shopState) ownerNote(it game.Item, me int64) string {
	owner, ok := st.owners[it.Key]
	switch {
	case !it.Unique || !ok:
		return ""
	case owner == me:
		return "yours"
	}
	return "owned by " + st.houses[owner]
}

func (b *Bot) shopPanel(ctx context.Context, c store.Character, tab, note string) *discordgo.InteractionResponseData {
	st, err := b.loadShop(ctx, c.GuildID)
	if err != nil {
		slog.Error("shop", "err", err)
		return noteView(emojiShop+" The shop is closed. Something went wrong.", colorMuted)
	}
	return shopView(c, st, tab, note)
}

func shopView(c store.Character, st shopState, tab, note string) *discordgo.InteractionResponseData {
	sec, ok := game.SectionByKey(game.Section(tab))
	if !ok {
		sec = game.Sections[0]
	}
	var fixed, market []string
	var opts []discordgo.SelectMenuOption
	for _, it := range st.stock {
		if it.Section() != sec.Key {
			continue
		}
		o := st.ownerNote(it, c.ID)
		line := shopLine(it, o, c.Laws)
		if o == "" && len(opts) < 25 {
			opts = append(opts, discordgo.SelectMenuOption{
				Label: truncate(it.Name, 100), Value: it.Key, Emoji: &discordgo.ComponentEmoji{Name: game.KindByKey(it.Kind).Emoji},
				Description: truncate(fmt.Sprintf("%d gold · value %s · %s", it.PriceUnder(c.Laws), game.Num(it.Value), kindOrTier(it)), 100),
			})
		}
		if st.market[it.Key] {
			market = append(market, line)
		} else {
			fixed = append(fixed, line)
		}
	}
	title := fmt.Sprintf("%s The Shop · %s %s", emojiShop, sec.Emoji, sec.Name)
	e := &discordgo.MessageEmbed{Title: title, Description: withNote(note, sec.Intro), Color: colorPanel}
	e.Fields = []*discordgo.MessageEmbedField{fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)), fact("Market changes", relTime(st.ends))}
	if len(fixed) > 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "📦 Always in stock", Value: joinSep(fixed, "\n\n", 1000)})
	}
	if len(market) > 0 {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "🌅 Today's market", Value: joinSep(market, "\n\n", 1000)})
	}
	e.Footer = footer("✨ Named pieces are one of a kind: once someone owns one, nobody else can buy it until they sell it.")
	var comps []discordgo.MessageComponent
	if len(opts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "shop:pick:" + tab,
			Placeholder: "Take a closer look…", Options: opts}))
	}
	var nav []discordgo.MessageComponent
	for _, s := range game.Sections {
		if s.Key != sec.Key {
			nav = append(nav, button(s.Emoji+" "+s.Name, "shop:open:"+string(s.Key), discordgo.PrimaryButton, false))
		}
	}
	nav = append(nav,
		withEmoji(button("My Estate", "estate:view", discordgo.SecondaryButton, false), emojiEstate),
		backTo("menu:home"),
	)
	comps = append(comps, row(nav...))
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
}

func kindOrTier(it game.Item) string {
	if it.IsProperty() && it.Tier != 0 {
		return it.Tier.String() + " property"
	}
	return game.KindByKey(it.Kind).Name
}

func (b *Bot) itemPanel(ctx context.Context, c store.Character, key, note string) *discordgo.InteractionResponseData {
	st, err := b.loadShop(ctx, c.GuildID)
	if err != nil {
		slog.Error("shop", "err", err)
		return noteView(emojiShop+" The shop is closed. Something went wrong.", colorMuted)
	}
	return itemView(c, st, key, note)
}

func itemView(c store.Character, st shopState, key, note string) *discordgo.InteractionResponseData {
	it, ok := game.ItemByKey(key)
	tab := string(it.Section())
	back := withEmoji(button("Back to the shop", "shop:open:"+tab, discordgo.SecondaryButton, false), "↩️")
	if !ok {
		v := noteView("That isn't for sale.", colorMuted)
		v.Components = []discordgo.MessageComponent{row(back)}
		return v
	}
	inStock := false
	for _, s := range st.stock {
		inStock = inStock || s.Key == key
	}
	tags := []string{kindOrTier(it)}
	if it.Unique {
		tags = append(tags, "✨ one of a kind")
	}
	if it.Magic {
		tags = append(tags, "🔮 enchanted")
	}
	desc := fmt.Sprintf("*%s*\n\n%s", strings.Join(tags, " · "), it.Description)
	var warn []string
	if c.Laws.Has(game.LawDebasement) {
		warn = append(warn, "🪙 Prices are up: the coinage is debased.")
	}
	if it.Magic && c.Laws.Has(game.LawWitchcraftAct) {
		warn = append(warn, "🧙 Under the Witchcraft Act, owning it may cost you fines.")
	}
	owner := st.ownerNote(it, c.ID)
	if owner != "" {
		warn = append(warn, fmt.Sprintf("🔒 This piece is %s.", owner))
	} else if !inStock {
		warn = append(warn, "It isn't in the market today.")
	}
	if len(warn) > 0 {
		desc += "\n\n" + quote(strings.Join(warn, "\n"))
	}
	price := it.PriceUnder(c.Laws)
	fields := []*discordgo.MessageEmbedField{
		fact("Price", fmt.Sprintf("%d gold", price)),
		fact("Value", game.Num(it.Value)),
		fact("Sells back for", fmt.Sprintf("%d gold", it.SellBackUnder(c.Laws))),
	}
	if fx := it.Effects.Describe(); fx != "" {
		v := fx
		if it.IsProperty() {
			v += "\n*Party bonus, rumor-noticing and vote weight only apply while you live there.*"
		}
		fields = append(fields, &discordgo.MessageEmbedField{Name: "✨ Effects", Value: v})
	}
	fields = append(fields, fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)))
	buyable := owner == "" && inStock && c.Wealth >= price
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: game.KindByKey(it.Kind).Emoji + " " + it.Name, Description: withNote(note, desc), Color: colorPanel, Fields: fields}},
		Components: []discordgo.MessageComponent{row(
			button(fmt.Sprintf("Buy for %d gold", price), "shop:buy:"+it.Key, discordgo.SuccessButton, !buyable),
			back,
		)},
	}
}

func (b *Bot) onBuy(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, key string) {
	it, ok := game.ItemByKey(key)
	period, _ := game.MarketPeriod(time.Now(), b.t.Shop)
	if !ok || !game.InStock(key, c.GuildID, period) {
		b.update(i, b.itemPanel(ctx, c, key, "That's no longer for sale."))
		return
	}
	price := it.PriceUnder(c.Laws)
	hid, err := b.store.BuyItem(ctx, c.ID, c.GuildID, key, price, time.Now())
	if err != nil {
		note := estateErr(err)
		if !errors.Is(err, store.ErrOwned) {
			note = fmt.Sprintf("You need %d gold for that.", price)
		}
		b.update(i, b.itemPanel(ctx, c, key, note))
		return
	}
	if fresh, err := b.store.GetCharacterByID(ctx, c.ID); err == nil {
		c = fresh
	}
	if it.Magic && game.IsMisdeed(game.SecretKindMagic, c.Laws) { // a misdeed now, and spies may hear of it
		defer b.leak(ctx, c, game.SecretMagic(c.FullName(), it.Name))
	}
	text := fmt.Sprintf("**%s** is yours.", it.Name)
	tab := string(it.Section())
	btns := []discordgo.MessageComponent{
		withEmoji(button("Keep shopping", "shop:open:"+tab, discordgo.PrimaryButton, false), emojiShop),
		withEmoji(button("My Estate", "estate:view", discordgo.SecondaryButton, false), emojiEstate),
	}
	if it.IsProperty() {
		text += " You can move in now, or keep living where you are and collect its income."
		btns = append([]discordgo.MessageComponent{withEmoji(button("Move in", fmt.Sprintf("estate:movein:%d", hid), discordgo.SuccessButton, false), "🏡")}, btns...)
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "🛍️ Bought: " + it.Name, Description: text, Color: colorGood,
			Fields: []*discordgo.MessageEmbedField{fact("Paid", fmt.Sprintf("%d gold", price)), fact("Left in your purse", fmt.Sprintf("%d gold", c.Wealth))}}},
		Components: []discordgo.MessageComponent{row(btns...)},
	})

	// Famous pieces change hands in public.
	if it.Unique {
		b.post(ctx, c.GuildID, &discordgo.MessageEmbed{
			Description: fmt.Sprintf("%s **%s** has bought **%s**.", game.KindByKey(it.Kind).Emoji, c.FullName(), it.Name),
			Color:       colorPanel,
		})
	}
}
