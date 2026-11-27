package bot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	componentRoutes["deal"] = (*Bot).onDeal
	modalRoutes["deal"] = (*Bot).onDealModal
}

// Estate deal custom IDs:
//   deal:start              pick a player to deal with
//   deal:who                (user select) player chosen → the deal composer
//   deal:to:<character>     open the composer for a player (from a profile)
//   deal:give               (multi select) your items on the table
//   deal:want               (multi select) their items you ask for
//   deal:gold:<ask|pay>     open the gold popup
//   deal:goldm:<ask|pay>    (modal) gold amount
//   deal:send               send the offer
//   deal:yes:<offer>        recipient accepts (in DMs)
//   deal:no:<offer>         recipient declines (in DMs)

// EstateOfferTTL is how long an estate offer stays open.
const EstateOfferTTL = 24 * time.Hour

// dealDraft is an offer being put together. Drafts live in memory: a restart
// just means starting the offer again.
type dealDraft struct {
	ToID int64
	Give []int64
	Want []int64
	Gold int // > 0: they pay you; < 0: you pay them
}

type dealDrafts struct {
	mu sync.Mutex
	m  map[int64]*dealDraft // by the drafting character
}

func (d *dealDrafts) get(charID int64) (dealDraft, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dr, ok := d.m[charID]; ok {
		return *dr, true
	}
	return dealDraft{}, false
}

func (d *dealDrafts) set(charID int64, dr dealDraft) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = map[int64]*dealDraft{}
	}
	d.m[charID] = &dr
}

func (d *dealDrafts) drop(charID int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.m, charID)
}

func (b *Bot) onDeal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	// Answers arrive in DMs and are checked against the offer itself.
	if (parts[0] == "yes" || parts[0] == "no") && len(parts) > 1 {
		b.onDealAnswer(ctx, i, atoi(parts[1]), parts[0] == "yes")
		return
	}
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
	case parts[0] == "start": // a deal is private, even from a public estate page
		b.open(i, dealStart(""), true)
	case parts[0] == "who" && len(vals()) > 0:
		other, err := b.store.GetCharacter(ctx, c.GuildID, vals()[0])
		if err != nil || other.ID == c.ID {
			b.update(i, dealStart("Pick another player at this court."))
			return
		}
		b.deals.set(c.ID, dealDraft{ToID: other.ID})
		b.update(i, b.dealPanel(ctx, c, ""))
	case parts[0] == "to" && len(parts) > 1:
		other, err := b.store.GetCharacterByID(ctx, atoi(parts[1]))
		if err != nil || other.GuildID != c.GuildID || other.ID == c.ID {
			b.reply(i, "You can't make a deal with them.")
			return
		}
		b.deals.set(c.ID, dealDraft{ToID: other.ID})
		b.respond(i, ephemeral(b.dealPanel(ctx, c, "")))
	case parts[0] == "give" || parts[0] == "want":
		dr, ok := b.deals.get(c.ID)
		if !ok {
			b.update(i, dealStart("That deal was lost. Start again."))
			return
		}
		var ids []int64
		for _, v := range vals() {
			ids = append(ids, atoi(v))
		}
		if parts[0] == "give" {
			dr.Give = ids
		} else {
			dr.Want = ids
		}
		b.deals.set(c.ID, dr)
		b.update(i, b.dealPanel(ctx, c, ""))
	case parts[0] == "gold" && len(parts) > 1:
		title := "How much gold do you ask for?"
		if parts[1] == "pay" {
			title = "How much gold will you pay?"
		}
		if err := b.s.InteractionRespond(i.Interaction, amountModal("deal:goldm:"+parts[1], title, 0, 100000)); err != nil {
			b.fail(i, err)
		}
	case parts[0] == "send":
		b.onDealSend(ctx, i, c)
	}
}

func (b *Bot) onDealModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) < 2 || parts[0] != "goldm" {
		return
	}
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	dr, ok := b.deals.get(c.ID)
	if !ok {
		b.update(i, dealStart("That deal was lost. Start again."))
		return
	}
	gold, ok := modalAmount(i)
	if !ok || gold < 0 {
		b.update(i, b.dealPanel(ctx, c, "That wasn't a whole number of gold."))
		return
	}
	dr.Gold = gold
	if parts[1] == "pay" {
		dr.Gold = -gold
	}
	b.deals.set(c.ID, dr)
	b.update(i, b.dealPanel(ctx, c, ""))
}

func dealStart(note string) *discordgo.InteractionResponseData {
	desc := withNote(note, "Sell, buy or trade properties and possessions with another player, with gold either way. "+
		"They get your offer by DM and can accept or decline.")
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: emojiDeal + " Offer a deal", Description: desc, Color: colorPanel,
			Footer: footer("⏳ Offers stay open for a day.")}},
		Components: []discordgo.MessageComponent{
			row(discordgo.SelectMenu{MenuType: discordgo.UserSelectMenu, CustomID: "deal:who", Placeholder: "Who do you want to deal with?"}),
			row(backTo("estate:view")),
		},
	}
}

// dealSummary describes a deal as what the sender gives for what they get,
// e.g. "Riverside Villa for 500 gold".
func dealSummary(give, want []string, gold int) string {
	side := func(items []string, gold int) string {
		if gold > 0 {
			items = append(items, fmt.Sprintf("%d gold", gold))
		}
		if len(items) == 0 {
			return "nothing"
		}
		return strings.Join(items, ", ")
	}
	return side(give, -gold) + " for " + side(want, gold)
}

// names lists the item names for estate_items ids owned by c.
func names(c store.Character, ids []int64) []string {
	var out []string
	for _, id := range ids {
		if h, ok := c.Holding(id); ok {
			if it, ok := h.Item(); ok {
				out = append(out, it.Name)
			}
		}
	}
	return out
}

func holdingOptions(c store.Character, chosen []int64) []discordgo.SelectMenuOption {
	var opts []discordgo.SelectMenuOption
	for _, h := range c.Holdings {
		it, ok := h.Item()
		if !ok || len(opts) == 25 {
			continue
		}
		desc := fmt.Sprintf("%s · value %s", kindOrTier(it), game.Num(it.Value))
		if h.ID == c.ResidenceID {
			desc += " · their home"
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate(it.Name, 100), Value: fmt.Sprint(h.ID),
			Description: truncate(desc, 100), Default: slices.Contains(chosen, h.ID),
			Emoji: &discordgo.ComponentEmoji{Name: game.KindByKey(it.Kind).Emoji}})
	}
	return opts
}

func (b *Bot) dealPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	dr, ok := b.deals.get(c.ID)
	if !ok {
		return dealStart("That deal was lost. Start again.")
	}
	other, err := b.store.GetCharacterByID(ctx, dr.ToID)
	if err != nil {
		return dealStart("That player is gone.")
	}
	return dealView(c, other, dr, note)
}

func dealView(c, other store.Character, dr dealDraft, note string) *discordgo.InteractionResponseData {
	none := func(s []string) string {
		if len(s) == 0 {
			return "nothing"
		}
		return strings.Join(s, ", ")
	}
	gold := "None"
	switch {
	case dr.Gold > 0:
		gold = fmt.Sprintf("They pay you **%d**", dr.Gold)
	case dr.Gold < 0:
		gold = fmt.Sprintf("You pay them **%d**", -dr.Gold)
	}
	fields := []*discordgo.MessageEmbedField{
		{Name: "📤 You give", Value: truncate(none(names(c, dr.Give)), 1024), Inline: true},
		{Name: "📥 You ask for", Value: truncate(none(names(other, dr.Want)), 1024), Inline: true},
		fact("💰 Gold", gold),
		fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)),
	}
	desc := withNote(note, "Pick what changes hands, then send it.")
	comps := []discordgo.MessageComponent{}
	if opts := holdingOptions(c, dr.Give); len(opts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "deal:give",
			Placeholder: "What will you give?", Options: opts, MinValues: new(int), MaxValues: len(opts)}))
	}
	if opts := holdingOptions(other, dr.Want); len(opts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "deal:want",
			Placeholder: "What do you want from " + truncate(other.FullName(), 60) + "?", Options: opts, MinValues: new(int), MaxValues: len(opts)}))
	}
	empty := len(dr.Give) == 0 && len(dr.Want) == 0
	comps = append(comps, row(
		withEmoji(button("Ask for gold", "deal:gold:ask", discordgo.SecondaryButton, false), "📥"),
		withEmoji(button("Offer gold", "deal:gold:pay", discordgo.SecondaryButton, false), "📤"),
		withEmoji(button("Send offer", "deal:send", discordgo.SuccessButton, empty), "✉️"),
		button("Cancel", "estate:view", discordgo.SecondaryButton, false),
	))
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: emojiDeal + " A deal with " + other.FullName(), Description: desc, Color: colorPanel, Fields: fields,
			Footer: footer("🏡 Everyone keeps at least one property. Trade away the house you live in, and you move into your best remaining one.")}},
		Components: comps,
	}
}

func (b *Bot) onDealSend(ctx context.Context, i *discordgo.InteractionCreate, c store.Character) {
	dr, ok := b.deals.get(c.ID)
	if !ok {
		b.update(i, dealStart("That deal was lost. Start again."))
		return
	}
	other, err := b.store.GetCharacterByID(ctx, dr.ToID)
	if err != nil {
		b.update(i, dealStart("That player is gone."))
		return
	}
	if len(dr.Give) == 0 && len(dr.Want) == 0 {
		b.update(i, dealView(c, other, dr, "Put something on the table first."))
		return
	}
	if dr.Gold < 0 && c.Wealth < -dr.Gold {
		b.update(i, dealView(c, other, dr, fmt.Sprintf("You only have %d gold.", c.Wealth)))
		return
	}
	now := time.Now()
	o, err := b.store.CreateEstateOffer(ctx, store.EstateOffer{GuildID: c.GuildID, FromID: c.ID, ToID: other.ID,
		Give: dr.Give, Want: dr.Want, Gold: dr.Gold, CreatedAt: now, ExpiresAt: now.Add(EstateOfferTTL)})
	if err != nil {
		b.update(i, dealView(c, other, dr, estateErr(err)))
		return
	}
	give, want := names(c, dr.Give), names(other, dr.Want)
	b.deferUpdate(i) // the DM comes before the answer
	if _, err := b.dm(other.UserID, offerMessage(o, c, other, give, want)); err != nil {
		_ = b.store.DeclineEstateOffer(ctx, o.ID)
		b.update(i, dealView(c, other, dr, other.FullName()+" isn't accepting DMs, so I couldn't deliver the offer."))
		return
	}
	b.deals.drop(c.ID)
	summary := dealSummary(give, want, dr.Gold)
	b.leak(ctx, c, game.SecretEstateOffer(c.FullName(), other.ID, other.FullName(), summary))
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "✉️ Offer sent", Color: colorGood,
			Description: fmt.Sprintf("Your offer to **%s**: %s. I'll DM you their answer.", other.FullName(), summary),
			Fields:      []*discordgo.MessageEmbedField{fact("Open until", relTime(o.ExpiresAt))}}},
		Components: []discordgo.MessageComponent{row(withEmoji(button("My Estate", "estate:view", discordgo.SecondaryButton, false), emojiEstate))},
	})
}

// offerMessage is the DM the recipient gets.
func offerMessage(o store.EstateOffer, from, to store.Character, give, want []string) *discordgo.MessageSend {
	side := func(items []string, gold int) string {
		if gold > 0 {
			items = append(items, fmt.Sprintf("**%d** gold", gold))
		}
		if len(items) == 0 {
			return "Nothing"
		}
		return joinLines(items, 1000)
	}
	return &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{Title: fmt.Sprintf("%s %s offers you a deal", emojiDeal, from.FullName()),
			Description: "Accept and everything changes hands at once.", Color: colorNotice,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "📥 You get", Value: side(give, -o.Gold), Inline: true},
				{Name: "📤 You give", Value: side(want, o.Gold), Inline: true},
				fact("Your purse", fmt.Sprintf("%d gold", to.Wealth)),
				fact("⏳ Closes", relTime(o.ExpiresAt)),
			}}},
		Components: []discordgo.MessageComponent{row(
			withEmoji(button("Accept", fmt.Sprintf("deal:yes:%d", o.ID), discordgo.SuccessButton, false), "✅"),
			withEmoji(button("Decline", fmt.Sprintf("deal:no:%d", o.ID), discordgo.DangerButton, false), "✖️"),
		)},
	}
}

func (b *Bot) onDealAnswer(ctx context.Context, i *discordgo.InteractionCreate, offerID int64, accept bool) {
	o, err := b.store.GetEstateOffer(ctx, offerID)
	if err != nil {
		b.reply(i, "That offer no longer exists.")
		return
	}
	me, err1 := b.store.GetCharacterByID(ctx, o.ToID)
	them, err2 := b.store.GetCharacterByID(ctx, o.FromID)
	if err := errors.Join(err1, err2); err != nil {
		b.fail(i, err)
		return
	}
	if me.UserID != userOf(i).ID {
		b.reply(i, "That offer isn't for you.")
		return
	}
	// Names before the swap, while each side still owns their items.
	summary := dealSummary(names(them, o.Give), names(me, o.Want), o.Gold)
	done := func(text string, color int) {
		b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
			Title: fmt.Sprintf("%s %s's offer", emojiDeal, them.FullName()), Description: text, Color: color}},
			Components: []discordgo.MessageComponent{}})
	}
	if !accept {
		if err := b.store.DeclineEstateOffer(ctx, o.ID); err != nil {
			done(estateErr(err), colorMuted)
			return
		}
		done("You declined.", colorMuted)
		b.dmNote(them.UserID, emojiDeal+" Deal declined", fmt.Sprintf("**%s** declined your deal: %s.", me.FullName(), summary), colorBad)
		return
	}
	if _, err := b.store.AcceptEstateOffer(ctx, o.ID, time.Now()); err != nil {
		done("The deal fell through: "+estateErr(err), colorBad)
		if !errors.Is(err, store.ErrHandled) {
			b.dmNote(them.UserID, emojiDeal+" Deal fell through", fmt.Sprintf(
				"**%s** tried to accept your deal (%s), but it fell through: %s", me.FullName(), summary, estateErr(err)), colorBad)
		}
		return
	}
	done("✅ The deal is struck: "+summary+".", colorGood)
	b.dmNote(them.UserID, emojiDeal+" Deal struck", fmt.Sprintf("**%s** accepted your deal: %s.", me.FullName(), summary), colorGood)
	b.leak(ctx, me, game.SecretEstateDeal(me.FullName(), them.ID, them.FullName(), summary))
}
