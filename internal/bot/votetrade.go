package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Vote deal custom IDs:
//   senate:sell:<matter>                         sell panel (senator offers + player picker)
//   senate:sellai:<matter>:<senator>             sell to a senator
//   senate:sellto:<matter>                       (user select) offer to a player → price modal
//   senate:sellprice:<matter>:<buyerChar>        (modal) send the sell offer
//   senate:buy:<matter>                          buy panel (player picker)
//   senate:buyfrom:<matter>                      (user select) seller chosen → option buttons
//   senate:buyopt:<matter>:<sellerChar>:<opt>    → price modal
//   senate:buyprice:<matter>:<sellerChar>:<opt>  (modal) send the buy offer
//   senate:offer:<offer>:a:<opt>                 accept (opt chosen by buyer on sell offers)
//   senate:offer:<offer>:d                       decline

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func userSelect(id, placeholder string) discordgo.ActionsRow {
	return row(discordgo.SelectMenu{MenuType: discordgo.UserSelectMenu, CustomID: id, Placeholder: placeholder})
}

func priceModal(id, title string) *discordgo.InteractionResponse {
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: id, Title: truncate(title, 45),
			Components: []discordgo.MessageComponent{row(discordgo.TextInput{
				CustomID: "price", Label: "Price in gold", Style: discordgo.TextInputShort,
				Required: true, MinLength: 1, MaxLength: 7, Placeholder: "e.g. 25",
			})},
		},
	}
}

func (b *Bot) onVoteTrade(ctx context.Context, i *discordgo.InteractionCreate, parts []string) bool {
	switch {
	case parts[0] == "sell" && len(parts) > 1:
		b.onSellPanel(ctx, i, atoi(parts[1]))
	case parts[0] == "sellai" && len(parts) > 2:
		b.onSellToSenator(ctx, i, atoi(parts[1]), atoi(parts[2]))
	case parts[0] == "sellto" && len(parts) > 1:
		b.onPickPlayer(ctx, i, atoi(parts[1]), "", true)
	case parts[0] == "buy" && len(parts) > 1:
		b.onBuyPanel(ctx, i, atoi(parts[1]))
	case parts[0] == "buyfrom" && len(parts) > 1:
		b.onPickPlayer(ctx, i, atoi(parts[1]), "", false)
	case parts[0] == "buyopt" && len(parts) > 3:
		_, ev, _, msg := b.matterFor(ctx, i, atoi(parts[1]))
		if msg != "" {
			b.reply(i, msg)
			return true
		}
		if err := b.s.InteractionRespond(i.Interaction, priceModal(
			fmt.Sprintf("senate:buyprice:%s:%s:%s", parts[1], parts[2], parts[3]),
			"Buy a vote: "+optionLabel(ev, parts[3]))); err != nil {
			slog.Error("price modal", "err", err)
		}
	case parts[0] == "offer" && len(parts) > 2:
		opt := ""
		if len(parts) > 3 {
			opt = parts[3]
		}
		b.onOfferAnswer(ctx, i, atoi(parts[1]), parts[2] == "a", opt)
	default:
		return false
	}
	return true
}

// ---- selling ----

func (b *Bot) onSellPanel(ctx context.Context, i *discordgo.InteractionCreate, matterID int64) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	if opt, _, sold := b.store.SoldVote(ctx, m.ID, c.ID); sold {
		b.reply(i, fmt.Sprintf("Your vote is already sold. You will vote to **%s**.", strings.ToLower(optionLabel(ev, opt))))
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	st, err2 := b.store.GetRealm(ctx, m.GuildID)
	ops, err3 := b.store.Opinions(ctx, c.ID)
	if err := errors.Join(err1, err2, err3); err != nil {
		b.fail(i, err)
		return
	}
	offers := game.BestVoteOffers(ev, sen, c.VoteWeight(), st.Trackers, m.ID, ops)
	var sb strings.Builder
	if st.Laws.Has(game.LawSecretBallot) {
		// Nobody can check a secret vote: senators won't pay for one, and
		// a player who buys one has only your word.
		offers = nil
		sb.WriteString("Under the Secret Ballot nobody can see how you vote, so a sold vote is a promise, not a lock. ")
		sb.WriteString("You can take the gold and vote however you like, and your buyer will never know.\n\n**Senators' offers**\n")
		sb.WriteString("Senators won't pay for a vote they can't check.\n")
	} else {
		sb.WriteString("Once sold, your vote is locked and you can't change it. Senators pay well, but a bought vote can come out, and the Senate remembers who sells.\n\n**Senators' offers**\n")
	}
	var btns []discordgo.MessageComponent
	for _, o := range ev.Options {
		off, ok := offers[o.Key]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "**%s** will pay **%d gold** for you to vote to *%s*.\n", off.Senator.Name, off.Gold, strings.ToLower(o.Label))
		btns = append(btns, button(truncate(fmt.Sprintf("%s: %d gold", o.Label, off.Gold), 80),
			fmt.Sprintf("senate:sellai:%d:%d", m.ID, off.Senator.ID), discordgo.SuccessButton, false))
	}
	if len(btns) == 0 {
		sb.WriteString("No senator wants to buy your vote on this matter.\n")
	}
	sb.WriteString("\nOr offer your vote to another player at a price you name. They choose which way you vote.")
	comps := []discordgo.MessageComponent{}
	if len(btns) > 0 {
		comps = append(comps, row(btns...))
	}
	comps = append(comps, userSelect(fmt.Sprintf("senate:sellto:%d", m.ID), "Offer your vote to a player…"))
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Sell your vote: " + ev.Title, Description: sb.String(), Color: colorGold}},
		Components: comps,
	}))
}

func (b *Bot) onSellToSenator(ctx context.Context, i *discordgo.InteractionCreate, matterID, senatorID int64) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.update(i, &discordgo.InteractionResponseData{Content: msg, Embeds: []*discordgo.MessageEmbed{}})
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	st, err2 := b.store.GetRealm(ctx, m.GuildID)
	ops, err3 := b.store.Opinions(ctx, c.ID)
	if err := errors.Join(err1, err2, err3); err != nil {
		b.fail(i, err)
		return
	}
	s, ok := findSenator(sen, senatorID)
	if !ok {
		return
	}
	opt, gold, ok := s.VoteOfferFor(ev, c.VoteWeight(), st.Trackers, m.ID, ops[s.ID])
	if !ok || st.Laws.Has(game.LawSecretBallot) {
		b.update(i, &discordgo.InteractionResponseData{Content: s.Name + " isn't buying.", Embeds: []*discordgo.MessageEmbed{}})
		return
	}
	now := time.Now()
	recent, err1 := b.store.RecentSenatorSales(ctx, c.ID, now.Add(-game.VoteSaleMemory(b.t)))
	nets, err2 := b.store.SpyNetworksOn(ctx, c.ID)
	if err := errors.Join(err1, err2); err != nil {
		b.fail(i, err)
		return
	}
	var spies float64
	for _, n := range nets {
		if n.OwnerID != c.ID {
			spies = max(spies, n.Value)
		}
	}
	sale := game.VoteSaleInput{Seller: c.Person(), Senator: s, Matter: ev, Option: opt, Realm: st.Trackers,
		MatterID: m.ID, Gold: gold, Recent: recent, Spies: spies}
	color := colorGold
	var text string
	switch err := b.store.SellVoteToSenator(ctx, m.ID, c.ID, opt, s, gold, game.OpinionSoldVote, now); {
	case errors.Is(err, store.ErrVoteSold):
		text = "Your vote is already sold."
	case errors.Is(err, store.ErrEnded):
		text = "Voting on this matter has closed."
	case err != nil:
		b.fail(i, err)
		return
	default:
		text = fmt.Sprintf("You sold your vote to **%s**. You will vote to **%s**. (+%d gold, their opinion of you %s)",
			s.Name, strings.ToLower(optionLabel(ev, opt)), gold, game.OpinionNum(game.OpinionSoldVote))
		defer b.leak(ctx, c, game.SecretSoldToSenator(c.FullName(), s.Name, ev.Title, optionLabel(ev, opt), gold))
		if res := game.ResolveVoteSale(b.rng, sale, sen); res.Caught {
			out := store.Outcome{Deltas: map[int64]game.Delta{c.ID: res.Delta}, Opinions: res.Opinions}
			if err := b.store.ApplyOutcome(ctx, m.GuildID, out, now); err != nil {
				slog.Error("vote scandal", "err", err)
				break
			}
			color = colorRed
			text += "\n\n" + res.Text
			if d := res.Delta.Describe(); d != "" {
				text += fmt.Sprintf(" (%s)", d)
			}
			if court, err := b.store.GetCourt(ctx, m.GuildID); err == nil && court.AnnouncementsChannelID != "" {
				_, _ = b.s.ChannelMessageSendEmbed(court.AnnouncementsChannelID, &discordgo.MessageEmbed{
					Title: "A bought vote", Description: res.Public, Color: colorRed,
				})
			}
		}
	}
	b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{Title: "Vote sold", Description: text, Color: color}}})
}

// ---- buying ----

func (b *Bot) onBuyPanel(ctx context.Context, i *discordgo.InteractionCreate, matterID int64) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Buy a vote: " + ev.Title, Color: colorGold,
			Description: fmt.Sprintf("Offer another player gold to vote your way. They'll get your offer by DM.\n\nYou have **%d gold**.", c.Wealth)}},
		Components: []discordgo.MessageComponent{userSelect(fmt.Sprintf("senate:buyfrom:%d", m.ID), "Whose vote do you want?")},
	}))
}

// onPickPlayer handles the player select for both selling (sell=true: the
// picked player is the buyer) and buying (the picked player is the seller).
func (b *Bot) onPickPlayer(ctx context.Context, i *discordgo.InteractionCreate, matterID int64, _ string, sell bool) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	vals := i.MessageComponentData().Values
	if len(vals) == 0 {
		return
	}
	other, err := b.store.GetCharacter(ctx, m.GuildID, vals[0])
	if err != nil || other.ID == c.ID {
		b.reply(i, "Pick another player who has a place at this court.")
		return
	}
	if sell {
		if err := b.s.InteractionRespond(i.Interaction, priceModal(
			fmt.Sprintf("senate:sellprice:%d:%d", m.ID, other.ID), "Sell your vote to "+other.Name)); err != nil {
			slog.Error("price modal", "err", err)
		}
		return
	}
	if _, _, sold := b.store.SoldVote(ctx, m.ID, other.ID); sold {
		b.reply(i, other.FullName()+"'s vote on this matter is already sold.")
		return
	}
	var btns []discordgo.MessageComponent
	for _, o := range ev.Options {
		btns = append(btns, button(o.Label, fmt.Sprintf("senate:buyopt:%d:%d:%s", m.ID, other.ID, o.Key), discordgo.PrimaryButton, false))
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Buy " + other.FullName() + "'s vote", Description: "How do you want them to vote?", Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...)},
	})
}

// onTradeModal handles the price modals. Returns false if not ours.
func (b *Bot) onTradeModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) bool {
	if len(parts) < 3 || (parts[0] != "sellprice" && parts[0] != "buyprice") {
		return false
	}
	m, ev, c, msg := b.matterFor(ctx, i, atoi(parts[1]))
	if msg != "" {
		b.reply(i, msg)
		return true
	}
	price, err := strconv.Atoi(strings.TrimSpace(modalValues(i.ModalSubmitData())["price"]))
	if err != nil || price <= 0 {
		b.reply(i, "The price must be a whole number of gold above 0.")
		return true
	}
	other, err := b.store.GetCharacterByID(ctx, atoi(parts[2]))
	if err != nil || other.GuildID != m.GuildID {
		b.reply(i, "That player isn't at this court.")
		return true
	}

	offer := store.VoteOffer{MatterID: m.ID, FromID: c.ID, ToID: other.ID, Price: price}
	var dmText string
	var btns []discordgo.MessageComponent
	if parts[0] == "sellprice" {
		if _, _, sold := b.store.SoldVote(ctx, m.ID, c.ID); sold {
			b.reply(i, "Your vote is already sold.")
			return true
		}
		offer.Kind = store.OfferSell
		dmText = fmt.Sprintf("**%s** (<@%s>) offers to sell you their vote on **%s** for **%d gold**. If you accept, you choose how they vote.",
			c.FullName(), c.UserID, ev.Title, price)
	} else {
		if len(parts) < 4 {
			return true
		}
		if _, ok := ev.Option(parts[3]); !ok {
			return true
		}
		if price > c.Wealth {
			b.reply(i, fmt.Sprintf("You only have %d gold.", c.Wealth))
			return true
		}
		offer.Kind, offer.Option = store.OfferBuy, parts[3]
		dmText = fmt.Sprintf("**%s** (<@%s>) offers you **%d gold** to vote to **%s** on **%s**. Your vote would be locked.",
			c.FullName(), c.UserID, price, strings.ToLower(optionLabel(ev, parts[3])), ev.Title)
	}
	offer, err = b.store.CreateOffer(ctx, offer, time.Now())
	if err != nil {
		b.fail(i, err)
		return true
	}
	if offer.Kind == store.OfferSell {
		for _, o := range ev.Options {
			btns = append(btns, button(truncate("Buy: "+o.Label, 80), fmt.Sprintf("senate:offer:%d:a:%s", offer.ID, o.Key), discordgo.SuccessButton, false))
		}
	} else {
		btns = append(btns, button("Accept", fmt.Sprintf("senate:offer:%d:a", offer.ID), discordgo.SuccessButton, false))
	}
	btns = append(btns, button("Decline", fmt.Sprintf("senate:offer:%d:d", offer.ID), discordgo.DangerButton, false))

	_, dmErr := b.dm(other.UserID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{Title: "A vote for sale", Description: dmText, Color: colorGold,
			Footer: &discordgo.MessageEmbedFooter{Text: "The offer lapses when voting closes."}}},
		Components: []discordgo.MessageComponent{row(btns...)},
	})
	if dmErr != nil {
		_, _ = b.store.DeclineOffer(ctx, offer.ID)
		b.reply(i, other.FullName()+" isn't accepting DMs, so the offer couldn't be sent.")
		return true
	}
	b.reply(i, fmt.Sprintf("Your offer has been sent to %s. I'll DM you their answer.", other.FullName()))
	b.leak(ctx, c, game.SecretVoteOffer(c.FullName(), other.ID, other.FullName(), ev.Title,
		optionLabel(ev, offer.Option), price, offer.Kind == store.OfferBuy))
	return true
}

// ---- answering an offer (in DMs) ----

func (b *Bot) onOfferAnswer(ctx context.Context, i *discordgo.InteractionCreate, offerID int64, accept bool, opt string) {
	o, err := b.store.GetOffer(ctx, offerID)
	if err != nil {
		b.reply(i, "That offer no longer exists.")
		return
	}
	me, err1 := b.store.GetCharacterByID(ctx, o.ToID)
	them, err2 := b.store.GetCharacterByID(ctx, o.FromID)
	m, err3 := b.store.GetMatter(ctx, o.MatterID)
	if err := errors.Join(err1, err2, err3); err != nil {
		b.fail(i, err)
		return
	}
	if me.UserID != userOf(i).ID {
		b.reply(i, "That offer isn't for you.")
		return
	}
	ev, _ := game.MatterByKey(m.EventKey)

	done := func(text string) {
		b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{Title: "A vote for sale", Description: text, Color: colorGold}}})
	}
	if !accept {
		if _, err := b.store.DeclineOffer(ctx, o.ID); errors.Is(err, store.ErrHandled) {
			done("You've already answered this offer.")
			return
		}
		done("You declined.")
		b.leak(ctx, me, game.SecretVoteDeal(me.FullName(), them.ID, them.FullName(), ev.Title, "", o.Price, false))
		_, _ = b.dm(them.UserID, &discordgo.MessageSend{Content: fmt.Sprintf("**%s** declined your vote offer on **%s**.", me.FullName(), ev.Title)})
		return
	}
	if o.Kind == store.OfferSell {
		if _, ok := ev.Option(opt); !ok {
			return
		}
	}
	o, err = b.store.AcceptOffer(ctx, o.ID, opt, time.Now())
	switch {
	case errors.Is(err, store.ErrHandled):
		done("You've already answered this offer.")
		return
	case errors.Is(err, store.ErrEnded):
		done("Too late: voting on this matter has closed.")
		return
	case errors.Is(err, store.ErrVoteSold):
		done("That vote has already been sold to someone else.")
		return
	case err != nil:
		done(err.Error())
		return
	}
	label := strings.ToLower(optionLabel(ev, o.Option))
	defer b.leak(ctx, me, game.SecretVoteDeal(me.FullName(), them.ID, them.FullName(), ev.Title, optionLabel(ev, o.Option), o.Price, true))
	defer b.leak(ctx, them, game.SecretVoteDeal(them.FullName(), me.ID, me.FullName(), ev.Title, optionLabel(ev, o.Option), o.Price, true))
	// Under the Secret Ballot a sold vote isn't locked: the deal runs on trust.
	trust := ""
	if me.Laws.Has(game.LawSecretBallot) {
		trust = " The ballot is secret, so this is a promise, not a lock."
	}
	if o.Kind == store.OfferSell { // I bought their vote
		done(fmt.Sprintf("Deal. You paid %d gold. **%s** has promised to vote to **%s**.%s", o.Price, them.FullName(), label, trust))
		_, _ = b.dm(them.UserID, &discordgo.MessageSend{Content: fmt.Sprintf(
			"**%s** bought your vote on **%s** for %d gold. You will vote to **%s**.%s", me.FullName(), ev.Title, o.Price, label, trust)})
	} else { // I sold my vote
		done(fmt.Sprintf("Deal. You received %d gold and will vote to **%s**.%s", o.Price, label, trust))
		_, _ = b.dm(them.UserID, &discordgo.MessageSend{Content: fmt.Sprintf(
			"**%s** accepted your offer on **%s**. They have promised to vote to **%s**. (-%d gold)%s", me.FullName(), ev.Title, label, o.Price, trust)})
	}
}
