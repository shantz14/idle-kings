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

func init() {
	registerActivity(game.KindInvest, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.investPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveInvest,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			name := "a venture"
			if d, ok := game.SectorByKey(game.SectorKey(a.Goal)); ok {
				name = d.Name
			}
			return fmt.Sprintf("You are negotiating a stake in %s. The deal is struck <t:%d:R>.", name, a.EndsAt.Unix())
		},
	})
	componentRoutes["invest"] = (*Bot).onInvest
	modalRoutes["invest"] = (*Bot).onInvestModal
}

// Invest in a Venture custom IDs:
//   invest:pick                    (select) sector chosen → amount panel
//   invest:spend:<sector>:<gold>   invest a preset amount
//   invest:custom:<sector>         open the custom amount popup
//   invest:amount:<sector>         (modal) custom amount submitted

func (b *Bot) onInvest(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "pick":
		if vals := i.MessageComponentData().Values; len(vals) > 0 {
			b.update(i, b.investAmountPanel(ctx, c, game.SectorKey(vals[0]), ""))
		}
	case parts[0] == "spend" && len(parts) > 2:
		b.onInvestGo(ctx, i, c, game.SectorKey(parts[1]), int(atoi(parts[2])))
	case parts[0] == "custom" && len(parts) > 1:
		if err := b.s.InteractionRespond(i.Interaction, amountModal("invest:amount:"+parts[1], "How much will you invest?",
			game.InvestMin, game.InvestMax)); err != nil {
			slog.Error("invest modal", "err", err)
		}
	}
}

func (b *Bot) onInvestModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) < 2 || parts[0] != "amount" {
		return
	}
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	gold, ok := modalAmount(i)
	if !ok {
		b.update(i, b.investAmountPanel(ctx, c, game.SectorKey(parts[1]), "That wasn't a whole number of gold."))
		return
	}
	b.onInvestGo(ctx, i, c, game.SectorKey(parts[1]), gold)
}

// amountModal asks for a whole number of gold.
func amountModal(id, title string, min, max int) *discordgo.InteractionResponse {
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: id, Title: truncate(title, 45),
			Components: []discordgo.MessageComponent{row(discordgo.TextInput{
				CustomID: "gold", Label: fmt.Sprintf("Gold (%d to %d)", min, max),
				Style: discordgo.TextInputShort, Required: true, MinLength: 1, MaxLength: 5, Placeholder: "e.g. 100",
			})},
		},
	}
}

// modalAmount reads the amountModal's value.
func modalAmount(i *discordgo.InteractionCreate) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(modalValues(i.ModalSubmitData())["gold"]))
	return n, err == nil
}

// pct renders a share as "7.7%".
func pct(share float64) string { return game.Num(share*100) + "%" }

// ---- panels ----

// investState loads what the invest panels show: the realm and whether the
// player is busy.
func (b *Bot) investState(ctx context.Context, c store.Character) (store.RealmState, string, error) {
	st, err := b.store.GetRealm(ctx, c.GuildID)
	if err != nil {
		return st, "", err
	}
	busy := ""
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		busy = b.activityLine(ctx, a)
	} else if !st.Alive() {
		busy = "The realm has fallen. There is nothing left to invest in."
	}
	return st, busy, nil
}

func (b *Bot) investPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	st, busy, err := b.investState(ctx, c)
	if err != nil {
		return noteView("The realm isn't ready yet. Ask the admin to `/start` the game.", colorMuted)
	}
	return investView(c, st, game.SectorKey(""), nil, busy, note, b.investFor)
}

func (b *Bot) investAmountPanel(ctx context.Context, c store.Character, key game.SectorKey, note string) *discordgo.InteractionResponseData {
	st, busy, err := b.investState(ctx, c)
	if err != nil {
		return noteView("The realm isn't ready yet.", colorMuted)
	}
	var own *pieCard
	if cards, err := b.ownership(ctx, c.GuildID, c.ID); err != nil {
		slog.Error("ownership", "err", err)
	} else if card, ok := cardFor(cards, key); ok {
		own = &card
	}
	return investView(c, st, key, own, busy, note, b.investFor)
}

// investView is the sector picker, plus the amount buttons once a sector is
// chosen, with who owns it (own, nil to leave it out). Revenue isn't a
// roll, so it's shown; the deal odds never are.
func investView(c store.Character, st store.RealmState, key game.SectorKey, own *pieCard, busy, note string, dur time.Duration) *discordgo.InteractionResponseData {
	intro := "Put gold into one of the realm's industries. It grows the sector and buys you a share of what it earns every day; " +
		"how big a share depends on how well you negotiate."
	e := actionEmbed(game.KindInvest, withNote(busyNote(busy, note), intro))
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Deal takes", humanDuration(dur)),
		fact("Your ventures", game.Num(c.VentureIncome)+" gold/day"),
		fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)),
	}
	e.Footer = footer(emojiSenate + " What the Senate decides moves these industries, and your income with them.")
	embeds := []*discordgo.MessageEmbed{e}

	var opts []discordgo.SelectMenuOption
	for _, d := range game.SectorDefs {
		desc := fmt.Sprintf("%s gold/day · %s", game.Num(st.Sectors.Revenue(d, st.Trackers, st.Laws)), st.Sectors.Condition(d, st.Trackers, st.Laws))
		if v, ok := c.Ventures[d.Key]; ok {
			desc += " · you own " + pct(v.Share)
		}
		opts = append(opts, discordgo.SelectMenuOption{
			Label: d.Emoji + " " + d.Name, Value: string(d.Key), Description: truncate(desc, 100), Default: d.Key == key,
		})
	}
	comps := []discordgo.MessageComponent{row(discordgo.SelectMenu{
		MenuType: discordgo.StringSelectMenu, CustomID: "invest:pick", Placeholder: "Choose an industry…", Options: opts,
	})}
	back := backButton(game.KindInvest)

	d, ok := game.SectorByKey(key)
	if !ok {
		return &discordgo.InteractionResponseData{Embeds: embeds, Components: append(comps, row(back))}
	}
	card := &discordgo.MessageEmbed{Title: d.Emoji + " " + d.Name, Description: d.Description, Color: colorPanel,
		Fields: []*discordgo.MessageEmbedField{
			fact("Earns", game.Num(st.Sectors.Revenue(d, st.Trackers, st.Laws))+" gold/day"),
			fact("Condition", st.Sectors.Condition(d, st.Trackers, st.Laws)),
		}}
	if v, ok := c.Ventures[d.Key]; ok {
		card.Fields = append(card.Fields, fact("You own", fmt.Sprintf("%s · %s gold/day", pct(v.Share), game.Num(v.Income))))
	}
	if own != nil {
		card.Fields = append(card.Fields, &discordgo.MessageEmbedField{Name: "👥 Who owns it", Value: legendList(*own)})
	}
	card.Footer = footer(fmt.Sprintf("Invest anything from %d to %d gold.", game.InvestMin, game.InvestMax))
	embeds = append(embeds, card)

	var btns []discordgo.MessageComponent
	for _, g := range game.InvestPresets {
		btns = append(btns, button(fmt.Sprintf("%d gold", g), fmt.Sprintf("invest:spend:%s:%d", d.Key, g),
			discordgo.SuccessButton, busy != "" || c.Wealth < g))
	}
	btns = append(btns, withEmoji(button("Custom amount", "invest:custom:"+string(d.Key), discordgo.PrimaryButton, busy != "" || c.Wealth < game.InvestMin), "✏️"))
	data := &discordgo.InteractionResponseData{Embeds: embeds, Components: append(comps, row(btns...), row(back))}
	if own != nil {
		attachChart(data, card, "ownership.png", pieImage(*own))
	}
	return data
}

// ---- starting ----

func (b *Bot) onInvestGo(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, key game.SectorKey, gold int) {
	d, ok := game.SectorByKey(key)
	if !ok {
		b.update(i, b.investPanel(ctx, c, "That industry doesn't exist."))
		return
	}
	switch {
	case gold < game.InvestMin || gold > game.InvestMax:
		b.update(i, b.investAmountPanel(ctx, c, key, fmt.Sprintf("Invest between %d and %d gold.", game.InvestMin, game.InvestMax)))
		return
	case gold > c.Wealth:
		b.update(i, b.investAmountPanel(ctx, c, key, fmt.Sprintf("You only have %d gold.", c.Wealth)))
		return
	}
	if c.RealmFallen {
		b.update(i, b.investAmountPanel(ctx, c, key, ""))
		return
	}
	now := time.Now()
	ends := now.Add(b.investFor)
	if _, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindInvest, Goal: string(d.Key), StartedAt: now, EndsAt: ends,
	}, gold, nil); err != nil {
		b.update(i, b.investAmountPanel(ctx, c, key, err.Error()))
		return
	}
	b.update(i, startedView("📈 Investing in "+d.Name, fmt.Sprintf("You put gold into %s %s. I'll DM you what you got for it.", d.Emoji, d.Name),
		fact("Invested", fmt.Sprintf("%d gold", gold)), fact("Deal struck", relTime(ends))))
}

// ---- resolving ----

func (b *Bot) resolveInvest(ctx context.Context, a store.Activity, now time.Time) {
	c, err := b.store.GetCharacterByID(ctx, a.HostID)
	if err != nil {
		slog.Error("invest host", "err", err)
		return
	}
	d, ok := game.SectorByKey(game.SectorKey(a.Goal))
	if !ok { // sector removed from the catalogue: refund and close
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: {Wealth: a.Spend}}}, now)
		return
	}
	res := game.Invest(b.rng, c.Sheet(), d, a.Spend)
	inv := store.Investment{ActivityID: a.ID, CharacterID: c.ID, GuildID: c.GuildID, Sector: d.Key,
		Stake: res.Stake, Capacity: res.Capacity}
	inv.Delta = res.Practice
	if res.Exposed {
		inv.Delta.Merge(game.ExposedDelta)
	}
	if err := b.store.ApplyInvestment(ctx, inv, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply investment", "err", err)
		}
		return
	}

	desc := res.Text
	if res.Exposed {
		desc = withDelta(desc, game.ExposedDelta)
	}
	if after, err := b.store.GetCharacterByID(ctx, c.ID); err == nil {
		if v, ok := after.Ventures[d.Key]; ok {
			desc += fmt.Sprintf("\n\nYou now own **%s** of %s, worth about **%s gold/day** to you.", pct(v.Share), d.Name, game.Num(v.Income))
		}
	}
	color := colorPanel
	switch res.Tier {
	case game.DealGreat:
		color = colorGood
	case game.DealBad:
		color = colorBad
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: fmt.Sprintf("%s Your investment in %s", d.Emoji, d.Name), Description: desc, Color: color,
	}}})
	if d.Secret {
		b.leak(ctx, c, game.SecretInvest(c.FullName(), d.Name, a.Spend))
	}

	if res.Exposed {
		b.post(ctx, c.GuildID, &discordgo.MessageEmbed{
			Title:       "📢 A scandal in the lower city",
			Description: fmt.Sprintf("Word is that **%s** has been putting money into smuggling rings and gambling dens.", c.FullName()),
			Color:       colorBad,
		})
	}
}
