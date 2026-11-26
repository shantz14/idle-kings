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
		return &discordgo.InteractionResponseData{Content: "The realm isn't ready yet. Ask the admin to /start the game."}
	}
	return investView(c, st, game.SectorKey(""), busy, note, b.investFor)
}

func (b *Bot) investAmountPanel(ctx context.Context, c store.Character, key game.SectorKey, note string) *discordgo.InteractionResponseData {
	st, busy, err := b.investState(ctx, c)
	if err != nil {
		return &discordgo.InteractionResponseData{Content: "The realm isn't ready yet."}
	}
	return investView(c, st, key, busy, note, b.investFor)
}

// investView is the sector picker, plus the amount buttons once a sector is
// chosen. Revenue isn't a roll, so it's shown; the deal odds never are.
func investView(c store.Character, st store.RealmState, key game.SectorKey, busy, note string, dur time.Duration) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Put gold into one of the realm's industries. Your money grows the sector, and buys you a share of what it earns every day. ")
	fmt.Fprintf(&sb, "How big a share depends on how well you negotiate. It takes %s to strike the deal.\n\n", humanDuration(dur))
	sb.WriteString("What happens in the Senate moves these industries, and your income with them.")
	if c.VentureIncome > 0 {
		fmt.Fprintf(&sb, "\n\nYour ventures earn you **%s gold/day**.", game.Num(c.VentureIncome))
	}
	if busy != "" {
		sb.WriteString("\n\n" + busy)
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	embeds := []*discordgo.MessageEmbed{{Title: "Invest in a Venture", Description: sb.String(), Color: colorGold}}

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
	back := button("Back", "menu:cat:"+string(game.CatMonetary), discordgo.SecondaryButton, false)

	d, ok := game.SectorByKey(key)
	if !ok {
		return &discordgo.InteractionResponseData{Embeds: embeds, Components: append(comps, row(back))}
	}
	var desc strings.Builder
	fmt.Fprintf(&desc, "%s\n\nIt earns **%s gold/day** and is **%s**.", d.Description,
		game.Num(st.Sectors.Revenue(d, st.Trackers, st.Laws)), st.Sectors.Condition(d, st.Trackers, st.Laws))
	if v, ok := c.Ventures[d.Key]; ok {
		fmt.Fprintf(&desc, "\nYou own **%s** of it, worth **%s gold/day** to you.", pct(v.Share), game.Num(v.Income))
	}
	fmt.Fprintf(&desc, "\n\nYou have **%d gold**. Invest anything from %d to %d.", c.Wealth, game.InvestMin, game.InvestMax)
	embeds = append(embeds, &discordgo.MessageEmbed{Title: d.Emoji + " " + d.Name, Description: desc.String(), Color: colorGold})

	var btns []discordgo.MessageComponent
	for _, g := range game.InvestPresets {
		btns = append(btns, button(fmt.Sprintf("%d gold", g), fmt.Sprintf("invest:spend:%s:%d", d.Key, g),
			discordgo.SuccessButton, busy != "" || c.Wealth < g))
	}
	btns = append(btns, button("Custom amount", "invest:custom:"+string(d.Key), discordgo.PrimaryButton, busy != "" || c.Wealth < game.InvestMin))
	return &discordgo.InteractionResponseData{Embeds: embeds, Components: append(comps, row(btns...), row(back))}
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
	if st, err := b.store.GetRealm(ctx, c.GuildID); err == nil && !st.Alive() {
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
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "Investing in " + d.Name,
			Description: fmt.Sprintf("You put **%d gold** into %s %s. The deal will be struck <t:%d:R>, and I'll DM you what you got for it.",
				gold, d.Emoji, d.Name, ends.Unix()),
			Color: colorGreen,
		}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:home", discordgo.SecondaryButton, false))},
	})
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
		desc += fmt.Sprintf(" (%s)", game.ExposedDelta.Describe())
	}
	if after, err := b.store.GetCharacterByID(ctx, c.ID); err == nil {
		if v, ok := after.Ventures[d.Key]; ok {
			desc += fmt.Sprintf("\n\nYou now own **%s** of %s, worth about **%s gold/day** to you.", pct(v.Share), d.Name, game.Num(v.Income))
		}
	}
	color := colorGold
	switch res.Tier {
	case game.DealGreat:
		color = colorGreen
	case game.DealBad:
		color = colorRed
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: fmt.Sprintf("%s Your investment in %s", d.Emoji, d.Name), Description: desc, Color: color,
	}}})
	if d.Secret {
		b.leak(ctx, c, game.SecretInvest(c.FullName(), d.Name, a.Spend))
	}

	if res.Exposed {
		if court, err := b.store.GetCourt(ctx, c.GuildID); err == nil && court.AnnouncementsChannelID != "" {
			_, _ = b.s.ChannelMessageSendEmbed(court.AnnouncementsChannelID, &discordgo.MessageEmbed{
				Title:       "A scandal in the lower city",
				Description: fmt.Sprintf("Word is that **%s** has been putting money into smuggling rings and gambling dens.", c.FullName()),
				Color:       colorRed,
			})
		}
	}
}
