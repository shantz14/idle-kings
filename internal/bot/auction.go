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

func init() {
	componentRoutes["auc"] = (*Bot).onAuction
	modalRoutes["auc"] = (*Bot).onAuctionModal
	openToAll["auc"] = true // anyone at court may bid on an announced auction
}

// Auction custom IDs (game/auction.go, store/auctions.go):
//   auc:bid:<auction>                     (announcement) open the bid panel
//   auc:pay:<auction>:<gold>              bid that much
//   auc:custom:<auction>                  name your bid → modal auc:amount:<auction>
//   auc:setup:<holding>:<reserve>:<days>  (from /estate) the terms of your own auction
//   auc:start:<holding>:<reserve>:<days>  put it up for auction
// <reserve> is an index into game.AuctionReserves.

// ---- opening ----

// openNPCAuction opens a merchant's auction of a lot nobody at the court
// owns, until ends, and announces it. It returns the lot's name, or "" if
// every lot has been sold.
func (b *Bot) openNPCAuction(ctx context.Context, guildID string, now, ends time.Time) string {
	owners, err := b.store.ItemOwners(ctx, guildID)
	if err != nil {
		slog.Error("item owners", "err", err)
		return ""
	}
	owned := map[string]bool{}
	for k := range owners {
		owned[k] = true
	}
	if open, err := b.store.OpenAuctions(ctx, guildID); err == nil {
		for _, a := range open {
			owned[a.ItemKey] = true
		}
	}
	it, ok := game.PickLot(b.rng, owned)
	if !ok {
		return ""
	}
	a, err := b.store.OpenAuction(ctx, store.Auction{GuildID: guildID, ItemKey: it.Key, Reserve: game.NPCReserve(it), StartedAt: now, EndsAt: ends})
	if err != nil {
		slog.Error("open auction", "err", err)
		return ""
	}
	b.announceAuction(ctx, a)
	return it.Name
}

// announceAuction posts an auction with its Bid button.
func (b *Bot) announceAuction(ctx context.Context, a store.Auction) {
	court, err := b.store.GetCourt(ctx, a.GuildID)
	if err != nil || court.AnnouncementsChannelID == "" {
		return
	}
	e, comps := b.auctionPost(ctx, a)
	msg, err := b.s.ChannelMessageSendComplex(court.AnnouncementsChannelID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{e}, Components: comps})
	if err != nil {
		slog.Warn("announce auction", "err", err)
		return
	}
	_ = b.store.SetAuctionMessage(ctx, a.ID, msg.ChannelID, msg.ID)
}

// auctionPost is an auction's announcement, with the names it needs.
func (b *Bot) auctionPost(ctx context.Context, a store.Auction) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	seller, top := "", ""
	ids := []int64{}
	if a.SellerID != 0 {
		ids = append(ids, a.SellerID)
	}
	if a.TopBidder != 0 {
		ids = append(ids, a.TopBidder)
	}
	if len(ids) > 0 {
		if names, err := b.store.CharacterNames(ctx, ids); err == nil {
			seller, top = names[a.SellerID].Name, names[a.TopBidder].Name
		}
	}
	return auctionEmbed(a, seller, top), auctionButtons(a)
}

func auctionEmbed(a store.Auction, seller, top string) *discordgo.MessageEmbed {
	it, _ := game.ItemByKey(a.ItemKey)
	who := "A Venetian merchant"
	if a.SellerID != 0 {
		who = "**" + seller + "**"
	}
	desc := fmt.Sprintf("%s is selling **%s** to the highest bidder.\n*%s*", who, it.Name, it.Description)
	if fx := it.Effects.Describe(); fx != "" {
		desc += "\n✨ " + fx
	}
	bid := "No bids yet"
	if a.TopBidder != 0 {
		bid = fmt.Sprintf("**%d gold**, from %s", a.TopBid, top)
	}
	fields := []*discordgo.MessageEmbedField{
		fact("Worth", fmt.Sprintf("%d gold, they say", it.Price)),
		fact("Top bid", bid),
		fact("Next bid", fmt.Sprintf("%d gold or more", a.MinBid())),
		fact("Ends", relTime(a.EndsAt)),
	}
	return &discordgo.MessageEmbed{Title: "🔨 An auction: " + it.Name, Description: desc, Color: colorNotice, Fields: fields,
		Footer: footer("Outbid, and your gold comes back. The highest bid when time runs out takes it.")}
}

func auctionButtons(a store.Auction) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{row(withEmoji(button("Bid", fmt.Sprintf("auc:bid:%d", a.ID), discordgo.PrimaryButton, false), "🔨"))}
}

// ---- bidding ----

func (b *Bot) onAuction(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || len(parts) < 2 {
		return
	}
	switch {
	case parts[0] == "bid":
		a, err := b.store.GetAuction(ctx, atoi(parts[1]))
		if err != nil || a.GuildID != c.GuildID {
			return
		}
		b.respond(i, ephemeral(bidView(c, a, "")))
	case parts[0] == "pay" && len(parts) > 2:
		b.placeBid(ctx, i, c, atoi(parts[1]), int(atoi(parts[2])))
	case parts[0] == "custom":
		if err := b.s.InteractionRespond(i.Interaction, amountModal("auc:amount:"+parts[1], "What will you bid?", 1, 1_000_000)); err != nil {
			slog.Error("auction modal", "err", err)
		}
	case (parts[0] == "setup" || parts[0] == "start") && len(parts) > 3:
		b.onAuctionSetup(ctx, i, c, parts[0] == "start", atoi(parts[1]), int(atoi(parts[2])), int(atoi(parts[3])))
	}
}

func (b *Bot) onAuctionModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || len(parts) < 2 || parts[0] != "amount" {
		return
	}
	gold, ok := modalAmount(i)
	if !ok || gold <= 0 {
		if a, err := b.store.GetAuction(ctx, atoi(parts[1])); err == nil {
			b.update(i, bidView(c, a, "Bid a whole number of gold."))
		}
		return
	}
	b.placeBid(ctx, i, c, atoi(parts[1]), gold)
}

// bidView is the bidder's own panel for an auction.
func bidView(c store.Character, a store.Auction, note string) *discordgo.InteractionResponseData {
	it, _ := game.ItemByKey(a.ItemKey)
	desc := fmt.Sprintf("**%s**, worth about %d gold. The next bid must be at least **%d gold**.", it.Name, it.Price, a.MinBid())
	switch {
	case a.Settled || !time.Now().Before(a.EndsAt):
		desc = "The bidding is over."
	case a.TopBidder == c.ID:
		desc = fmt.Sprintf("You hold the top bid on **%s**, at **%d gold**. If someone outbids you, your gold comes back.", it.Name, a.TopBid)
	case a.SellerID == c.ID:
		desc = fmt.Sprintf("It's your own **%s**. Watch the bids come in.", it.Name)
	}
	var btns []discordgo.MessageComponent
	open := !a.Settled && time.Now().Before(a.EndsAt) && a.TopBidder != c.ID && a.SellerID != c.ID
	if open {
		seen := map[int]bool{}
		for _, f := range []float64{1, 1.25, 1.5} {
			g := int(float64(a.MinBid()) * f)
			if seen[g] {
				continue
			}
			seen[g] = true
			btns = append(btns, button(fmt.Sprintf("Bid %d gold", g), fmt.Sprintf("auc:pay:%d:%d", a.ID, g), discordgo.SuccessButton, g > c.Wealth))
		}
		btns = append(btns, button("Name your bid", fmt.Sprintf("auc:custom:%d", a.ID), discordgo.SecondaryButton, false))
	}
	d := &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "🔨 Bidding on " + it.Name, Description: withNote(note, desc), Color: colorPanel,
			Fields: []*discordgo.MessageEmbedField{fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)), fact("Ends", relTime(a.EndsAt))}}},
	}
	if len(btns) > 0 {
		d.Components = []discordgo.MessageComponent{row(btns...)}
	}
	return d
}

func (b *Bot) placeBid(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, id int64, gold int) {
	before, err := b.store.Bid(ctx, id, c.ID, gold, time.Now())
	a, gerr := b.store.GetAuction(ctx, id)
	if gerr != nil || a.GuildID != c.GuildID {
		return
	}
	if fresh, ferr := b.store.GetCharacterByID(ctx, c.ID); ferr == nil {
		c = fresh
	}
	switch {
	case errors.Is(err, store.ErrEnded):
		b.update(i, bidView(c, a, "The bidding is over."))
		return
	case err != nil:
		b.update(i, bidView(c, a, err.Error()))
		return
	}
	it, _ := game.ItemByKey(a.ItemKey)
	b.update(i, bidView(c, a, fmt.Sprintf("✅ You bid **%d gold** for %s.", gold, it.Name)))
	if before.TopBidder != 0 {
		if names, err := b.store.CharacterNames(ctx, []int64{before.TopBidder}); err == nil {
			b.dmNote(names[before.TopBidder].UserID, "🔨 Outbid", fmt.Sprintf("Someone bid **%d gold** for **%s**. Your **%d gold** has come back to you. The bidding ends %s.",
				gold, it.Name, before.TopBid, relTime(a.EndsAt)), colorNotice)
		}
	}
	b.refreshAuction(ctx, a)
}

// refreshAuction updates an auction's announcement.
func (b *Bot) refreshAuction(ctx context.Context, a store.Auction) {
	if a.ChannelID == "" || a.MessageID == "" {
		return
	}
	e, comps := b.auctionPost(ctx, a)
	if a.Settled {
		comps = []discordgo.MessageComponent{}
		e.Color = colorMuted
	}
	embeds := []*discordgo.MessageEmbed{e}
	_, _ = b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: a.ChannelID, ID: a.MessageID, Embeds: &embeds, Components: &comps})
}

// ---- settling ----

// settleAuctions closes every auction whose time is up.
func (b *Bot) settleAuctions(ctx context.Context, now time.Time) {
	due, err := b.store.DueAuctions(ctx, now)
	if err != nil {
		slog.Error("due auctions", "err", err)
		return
	}
	for _, d := range due {
		a, sold, err := b.store.SettleAuction(ctx, d.ID, now)
		if err != nil {
			if !errors.Is(err, store.ErrEnded) {
				slog.Error("settle auction", "err", err)
			}
			continue
		}
		b.refreshAuction(ctx, a)
		it, _ := game.ItemByKey(a.ItemKey)
		names, _ := b.store.CharacterNames(ctx, []int64{a.SellerID, a.TopBidder})
		winner, seller := names[a.TopBidder], names[a.SellerID]
		switch {
		case sold:
			b.post(ctx, a.GuildID, &discordgo.MessageEmbed{Title: "🔨 Sold: " + it.Name, Color: colorGood,
				Description: fmt.Sprintf("**%s** takes **%s** for **%d gold**.", winner.Name, it.Name, a.TopBid)})
			b.dmNote(winner.UserID, "🔨 You won the auction", fmt.Sprintf("**%s** is yours, for %d gold. It's on your estate page.", it.Name, a.TopBid), colorGood)
			if a.SellerID != 0 {
				b.dmNote(seller.UserID, "🔨 Your auction", fmt.Sprintf("**%s** sold to **%s** for **%d gold**.", it.Name, winner.Name, a.TopBid), colorGood)
			}
		case a.SellerID != 0:
			b.dmNote(seller.UserID, "🔨 Your auction", fmt.Sprintf("Nobody bid on **%s**. It stays with you.", it.Name), colorMuted)
		}
	}
}

// ---- your own auction ----

// auctionSetupView is where a player puts one of their pieces up for auction.
func auctionSetupView(c store.Character, h store.Holding, reserve, days int, note string) *discordgo.InteractionResponseData {
	it, _ := h.Item()
	res := game.PlayerReserve(it, game.AuctionReserves[reserve])
	desc := fmt.Sprintf("Put **%s** up for auction. Anyone at court can bid; the highest bid when time runs out takes it, and the gold is yours. "+
		"If nobody bids your reserve, it stays with you. Until then you can't sell or trade it.", it.Name)
	id := func(r, d int) string { return fmt.Sprintf("auc:setup:%d:%d:%d", h.ID, r, d) }
	var reserves, lengths []discordgo.MessageComponent
	for n, share := range game.AuctionReserves {
		style := discordgo.SecondaryButton
		if n == reserve {
			style = discordgo.PrimaryButton
		}
		reserves = append(reserves, button(fmt.Sprintf("Reserve %d gold", game.PlayerReserve(it, share)), id(n, days), style, false))
	}
	for _, d := range game.AuctionDays {
		style := discordgo.SecondaryButton
		if d == days {
			style = discordgo.PrimaryButton
		}
		lengths = append(lengths, button(fmt.Sprintf("%d day%s", d, plural(d)), id(reserve, d), style, false))
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "🔨 Auction " + it.Name, Description: withNote(note, desc), Color: colorPanel,
			Fields: []*discordgo.MessageEmbedField{fact("Worth", fmt.Sprintf("%d gold", it.Price)), fact("Reserve", fmt.Sprintf("%d gold", res)),
				fact("Bidding lasts", fmt.Sprintf("%d game day%s", days, plural(days)))}}},
		Components: []discordgo.MessageComponent{row(reserves...), row(lengths...), row(
			withEmoji(button("Put it up for auction", fmt.Sprintf("auc:start:%d:%d:%d", h.ID, reserve, days), discordgo.SuccessButton, false), "🔨"),
			backTo("estate:view"),
		)},
	}
}

// auctionablePieces are a character's pieces they may auction.
func auctionablePieces(c store.Character) []discordgo.SelectMenuOption {
	var opts []discordgo.SelectMenuOption
	for _, h := range c.Holdings {
		if it, ok := h.Item(); ok && game.Auctionable(it) && len(opts) < 25 {
			opts = append(opts, discordgo.SelectMenuOption{Label: truncate(it.Name, 100), Value: fmt.Sprint(h.ID),
				Emoji: &discordgo.ComponentEmoji{Name: "🔨"}, Description: truncate(fmt.Sprintf("Worth %d gold", it.Price), 100)})
		}
	}
	return opts
}

func (b *Bot) onAuctionSetup(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, start bool, holdingID int64, reserve, days int) {
	h, ok := c.Holding(holdingID)
	it, iok := h.Item()
	if !ok || !iok || !game.Auctionable(it) || reserve < 0 || reserve >= len(game.AuctionReserves) || !validDays(days) {
		b.update(i, b.estatePageNote(c, "You can't auction that."))
		return
	}
	if !start {
		b.update(i, auctionSetupView(c, h, reserve, days, ""))
		return
	}
	now := time.Now()
	a, err := b.store.OpenAuction(ctx, store.Auction{GuildID: c.GuildID, ItemKey: it.Key, SellerID: c.ID, HoldingID: h.ID,
		Reserve: game.PlayerReserve(it, game.AuctionReserves[reserve]), StartedAt: now, EndsAt: now.Add(time.Duration(days) * b.t.Day)})
	if err != nil {
		b.update(i, auctionSetupView(c, h, reserve, days, estateErr(err)))
		return
	}
	b.announceAuction(ctx, a)
	b.update(i, b.estatePageNote(c, fmt.Sprintf("🔨 **%s** is up for auction. The court has been told; the bidding ends %s.", it.Name, relTime(a.EndsAt))))
}

func validDays(d int) bool {
	for _, x := range game.AuctionDays {
		if x == d {
			return true
		}
	}
	return false
}

// lotAvailable reports whether a merchant has a lot left to sell at a court.
func (b *Bot) lotAvailable(ctx context.Context, guildID string) bool {
	owners, err := b.store.ItemOwners(ctx, guildID)
	if err != nil {
		return false
	}
	owned := map[string]bool{}
	for k := range owners {
		owned[k] = true
	}
	_, ok := game.PickLot(b.rng, owned)
	return ok
}
