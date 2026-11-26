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

// Duel custom IDs:
//   duel:who                           (user select) challenger picks an opponent → wager panel
//   duel:wager:<defender>:<gold>       issue the challenge with a preset wager
//   duel:custom:<defender>             open the custom wager popup
//   duel:amount:<defender>             (modal) custom wager submitted
//   duel:accept:<duel>                 defender accepts (DM button)
//   duel:decline:<duel>                defender declines (DM button)
//   duel:bet:<duel>:<side>             spectator opens the bet panel for one duelist
//   duel:stake:<duel>:<side>:<gold>    place a preset bet
//   duel:betcustom:<duel>:<side>       open the custom bet popup
//   duel:betamount:<duel>:<side>       (modal) custom bet submitted

func (b *Bot) onDuel(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	switch {
	case parts[0] == "who":
		if c, ok := b.myCharacter(ctx, i); ok {
			b.onDuelWho(ctx, i, c)
		}
	case parts[0] == "wager" && len(parts) > 2:
		if c, ok := b.myCharacter(ctx, i); ok {
			b.onChallenge(ctx, i, c, atoi(parts[1]), int(atoi(parts[2])))
		}
	case parts[0] == "custom" && len(parts) > 1:
		if err := b.s.InteractionRespond(i.Interaction, amountModal("duel:amount:"+parts[1], "How much will you wager?", 1, game.DuelMaxWager)); err != nil {
			slog.Error("wager modal", "err", err)
		}
	case parts[0] == "accept" && len(parts) > 1:
		b.onDuelAccept(ctx, i, atoi(parts[1]))
	case parts[0] == "decline" && len(parts) > 1:
		b.onDuelDecline(ctx, i, atoi(parts[1]))
	case parts[0] == "bet" && len(parts) > 2:
		b.onBetPanel(ctx, i, atoi(parts[1]), atoi(parts[2]), "", false)
	case parts[0] == "stake" && len(parts) > 3:
		b.onBet(ctx, i, atoi(parts[1]), atoi(parts[2]), int(atoi(parts[3])))
	case parts[0] == "betcustom" && len(parts) > 2:
		if err := b.s.InteractionRespond(i.Interaction, amountModal(fmt.Sprintf("duel:betamount:%s:%s", parts[1], parts[2]),
			"How much will you bet?", 1, game.DuelMaxBet)); err != nil {
			slog.Error("bet modal", "err", err)
		}
	}
}

func (b *Bot) onDuelModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	gold, ok := modalAmount(i)
	switch {
	case parts[0] == "amount" && len(parts) > 1:
		c, found := b.myCharacter(ctx, i)
		if !found {
			return
		}
		if !ok || gold < 1 || gold > game.DuelMaxWager {
			b.update(i, b.wagerPanel(ctx, c, atoi(parts[1]), fmt.Sprintf("Wager a whole number of gold, from 1 to %d.", game.DuelMaxWager)))
			return
		}
		b.onChallenge(ctx, i, c, atoi(parts[1]), gold)
	case parts[0] == "betamount" && len(parts) > 2:
		if !ok || gold < 1 || gold > game.DuelMaxBet {
			b.onBetPanel(ctx, i, atoi(parts[1]), atoi(parts[2]), fmt.Sprintf("Bet a whole number of gold, from 1 to %d.", game.DuelMaxBet), true)
			return
		}
		b.onBet(ctx, i, atoi(parts[1]), atoi(parts[2]), gold)
	}
}

// ---- challenging ----

// duelBusy says why the character can't issue a challenge, or "".
func (b *Bot) duelBusy(ctx context.Context, c store.Character) string {
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		return b.activityLine(ctx, a)
	}
	if c.RealmFallen {
		return "The realm has fallen. There is no honor left to fight for."
	}
	return ""
}

func (b *Bot) duelPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	return duelView(b.t.Duel, b.duelBusy(ctx, c), note)
}

func duelView(t game.DuelTiming, busy, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	sb.WriteString("Call out another player. They have " + humanDuration(t.Answer) + " to answer by DM. ")
	sb.WriteString("Refusing costs them a little renown, and the hawks in the Senate notice.\n\n")
	fmt.Fprintf(&sb, "If they accept, you fight %s later, and the court is told and may bet on it. ", humanDuration(t.Bout))
	sb.WriteString("Partway through the bout you'll each choose a stance. Prowess decides most fights, Martial helps a little.\n\n")
	sb.WriteString("The winner takes renown from the loser. A bad loss can leave you **wounded** for a few days. ")
	sb.WriteString("You may put gold on it too: your opponent must match the wager to accept, and the winner takes both.")
	if busy != "" {
		sb.WriteString("\n\n" + busy)
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	comps := []discordgo.MessageComponent{}
	if busy == "" {
		comps = append(comps, userSelect("duel:who", "Choose your opponent…"))
	}
	comps = append(comps, row(button("Back", "menu:cat:"+string(game.CatMartial), discordgo.SecondaryButton, false)))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Challenge to a Duel", Description: sb.String(), Color: colorGold}},
		Components: comps,
	}
}

func (b *Bot) onDuelWho(ctx context.Context, i *discordgo.InteractionCreate, c store.Character) {
	vals := i.MessageComponentData().Values
	if len(vals) == 0 {
		return
	}
	t, err := b.store.GetCharacter(ctx, c.GuildID, vals[0])
	if err != nil || t.ID == c.ID {
		b.update(i, b.duelPanel(ctx, c, "Pick another player who has a place at this court."))
		return
	}
	b.update(i, b.wagerPanel(ctx, c, t.ID, ""))
}

func (b *Bot) wagerPanel(ctx context.Context, c store.Character, defenderID int64, note string) *discordgo.InteractionResponseData {
	t, err := b.store.GetCharacterByID(ctx, defenderID)
	if err != nil || t.GuildID != c.GuildID {
		return b.duelPanel(ctx, c, "That player is gone.")
	}
	return wagerView(c, t, note)
}

func wagerView(c, t store.Character, note string) *discordgo.InteractionResponseData {
	desc := fmt.Sprintf("You will challenge **%s** (%s).\n\nWill you put gold on it? They must match your wager to accept, and the winner takes both.\n\nYou have **%d gold**.",
		t.FullName(), t.House(), c.Wealth)
	if note != "" {
		desc += "\n\n" + note
	}
	var btns []discordgo.MessageComponent
	for _, g := range game.DuelWagers {
		label := fmt.Sprintf("%d gold", g)
		if g == 0 {
			label = "No wager"
		}
		btns = append(btns, button(label, fmt.Sprintf("duel:wager:%d:%d", t.ID, g), discordgo.SuccessButton, g > c.Wealth))
	}
	btns = append(btns, button("Other amount", fmt.Sprintf("duel:custom:%d", t.ID), discordgo.PrimaryButton, c.Wealth < 1))
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Challenge " + t.FullName(), Description: desc, Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...),
			row(button("Back", "menu:act:"+game.KindDuel, discordgo.SecondaryButton, false))},
	}
}

func (b *Bot) onChallenge(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, defenderID int64, wager int) {
	t, err := b.store.GetCharacterByID(ctx, defenderID)
	if err != nil || t.GuildID != c.GuildID || t.ID == c.ID {
		b.update(i, b.duelPanel(ctx, c, "Pick another player who has a place at this court."))
		return
	}
	if wager < 0 || wager > game.DuelMaxWager {
		b.update(i, b.wagerPanel(ctx, c, t.ID, "That isn't a wager you can make."))
		return
	}
	if busy := b.duelBusy(ctx, c); busy != "" {
		b.update(i, b.duelPanel(ctx, c, ""))
		return
	}
	if _, err := b.store.PendingDuelAgainst(ctx, t.ID); err == nil {
		b.update(i, b.wagerPanel(ctx, c, t.ID, t.FullName()+" already has a challenge waiting for an answer."))
		return
	}
	now := time.Now()
	a, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindDuel, Goal: game.DuelPending, TargetID: t.ID,
		Public: true, StartedAt: now, EndsAt: now.Add(b.t.Duel.Answer),
	}, wager, nil)
	if err != nil {
		b.update(i, b.wagerPanel(ctx, c, t.ID, err.Error()))
		return
	}
	// Under the Ban on Dueling, issuing a challenge is itself an offense.
	lawNote := ""
	if sen, err := b.store.Senators(ctx, c.GuildID); err == nil {
		if d, ops := game.ChallengeCost(sen, c.ID, c.Laws); d.Describe() != "" {
			if err := b.store.ApplyOutcome(ctx, c.GuildID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: d}, Opinions: ops}, now); err != nil {
				slog.Error("duel ban", "err", err)
			}
			lawNote = fmt.Sprintf("\n\nDueling is banned. Calling someone out has cost you. (%s)", d.Describe())
		}
	}

	// The defender is asked by DM.
	purse := ""
	if wager > 0 {
		purse = fmt.Sprintf(" They have put **%d gold** on it; you must match it to accept.", wager)
	}
	refusing := "Refusing, or not answering, costs you a little renown."
	switch {
	case c.Laws.Has(game.LawDuelBan):
		refusing = "Dueling is banned: there is no shame in refusing."
	case c.Laws.Has(game.LawHonor):
		refusing = "Under the Code of Honor, refusing, or not answering, is a disgrace."
	}
	_, dmErr := b.dm(t.UserID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "You have been challenged to a duel",
			Description: fmt.Sprintf("**%s** (%s) demands satisfaction.%s\n\nAnswer <t:%d:R>. %s",
				c.FullName(), c.House(), purse, a.EndsAt.Unix(), refusing),
			Color: colorRed,
		}},
		Components: []discordgo.MessageComponent{row(
			button("Accept", fmt.Sprintf("duel:accept:%d", a.ID), discordgo.DangerButton, false),
			button("Decline", fmt.Sprintf("duel:decline:%d", a.ID), discordgo.SecondaryButton, false),
		)},
	})

	where := ""
	if court, err := b.store.GetCourt(ctx, c.GuildID); err == nil && court.AnnouncementsChannelID != "" {
		msg, err := b.s.ChannelMessageSendComplex(court.AnnouncementsChannelID, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{duelAnnouncement(a, c, t, "")},
		})
		if err != nil {
			slog.Warn("announce duel", "err", err)
		} else {
			_ = b.store.SetAnnouncement(ctx, a.ID, msg.ChannelID, msg.ID)
			where = fmt.Sprintf(" The court has been told in <#%s>.", msg.ChannelID)
		}
	}
	text := fmt.Sprintf("You have challenged **%s**. They have until <t:%d:R> to answer.%s", t.FullName(), a.EndsAt.Unix(), where)
	if wager > 0 {
		text += fmt.Sprintf(" Your wager of **%d gold** is held until then.", wager)
	}
	if dmErr != nil {
		text += "\n\nI couldn't DM them, so they may not see it. They can't answer until they open their DMs."
	}
	text += lawNote
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "The gauntlet is thrown", Description: text, Color: colorGreen}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:home", discordgo.SecondaryButton, false))},
	})
}

// duelAnnouncement is the public post for a duel in its current state.
// result is the outcome line once it's over.
func duelAnnouncement(a store.Activity, c, t store.Character, result string) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{Color: colorGold}
	purse := ""
	if a.Spend > 0 {
		purse = fmt.Sprintf(" A purse of **%d gold** each rides on it.", a.Spend)
	}
	switch {
	case result != "":
		e.Title = fmt.Sprintf("Duel: %s vs %s", c.FullName(), t.FullName())
		e.Description = result
	case a.Goal == game.DuelAccepted:
		e.Title = fmt.Sprintf("%s and %s will duel", c.FullName(), t.FullName())
		e.Description = fmt.Sprintf("The challenge is accepted. They fight <t:%d:R>.%s\n\nPlace your bets: whoever backs the winner splits the whole pot.",
			a.EndsAt.Unix(), purse)
		e.Color = colorRed
	default:
		e.Title = fmt.Sprintf("%s challenges %s to a duel", c.FullName(), t.FullName())
		e.Description = fmt.Sprintf("%s has until <t:%d:R> to answer.%s", t.FullName(), a.EndsAt.Unix(), purse)
	}
	return e
}

func betButtons(a store.Activity, c, t store.Character) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{row(
		button(truncate("Bet on "+c.FullName(), 80), fmt.Sprintf("duel:bet:%d:%d", a.ID, c.ID), discordgo.PrimaryButton, false),
		button(truncate("Bet on "+t.FullName(), 80), fmt.Sprintf("duel:bet:%d:%d", a.ID, t.ID), discordgo.PrimaryButton, false),
	)}
}

// editDuelAnnouncement updates the public post; comps nil removes buttons.
func (b *Bot) editDuelAnnouncement(a store.Activity, c, t store.Character, result string, comps []discordgo.MessageComponent) {
	if a.AnnounceChannelID == "" || a.AnnounceMessageID == "" {
		return
	}
	embeds := []*discordgo.MessageEmbed{duelAnnouncement(a, c, t, result)}
	if comps == nil {
		comps = []discordgo.MessageComponent{}
	}
	_, _ = b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: a.AnnounceChannelID, ID: a.AnnounceMessageID, Embeds: &embeds, Components: &comps})
}

// duelLine is how a challenger's main menu describes their duel.
func (b *Bot) duelLine(ctx context.Context, a store.Activity) string {
	name := "your opponent"
	if t, err := b.store.GetCharacterByID(ctx, a.TargetID); err == nil {
		name = t.FullName()
	}
	if a.Goal == game.DuelAccepted {
		return fmt.Sprintf("You duel %s <t:%d:R>.", name, a.EndsAt.Unix())
	}
	return fmt.Sprintf("You have challenged %s. They must answer <t:%d:R>.", name, a.EndsAt.Unix())
}

// ---- answering ----

// duelParties loads a duel and both duelists.
func (b *Bot) duelParties(ctx context.Context, id int64) (store.Activity, store.Character, store.Character, error) {
	a, err := b.store.GetActivity(ctx, id)
	if err != nil || a.Kind != game.KindDuel {
		return a, store.Character{}, store.Character{}, store.ErrNotFound
	}
	c, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	t, err2 := b.store.GetCharacterByID(ctx, a.TargetID)
	return a, c, t, errors.Join(err1, err2)
}

func (b *Bot) onDuelAccept(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	a, c, t, err := b.duelParties(ctx, id)
	if err != nil || t.UserID != userOf(i).ID {
		b.reply(i, "That challenge isn't yours to answer.")
		return
	}
	now := time.Now()
	dt := b.t.Duel
	ends := now.Add(dt.Bout)
	var events []store.NewEvent
	for _, who := range []int64{c.ID, t.ID} {
		span := int((dt.StanceLatest-dt.StanceEarliest)/time.Second) + 1
		fires := now.Add(dt.StanceEarliest + time.Duration(b.rng.IntN(span))*time.Second)
		deadline := fires.Add(dt.StanceWindow)
		if latest := ends.Add(-10 * time.Second); deadline.After(latest) {
			deadline = latest
		}
		events = append(events, store.NewEvent{CharacterID: who, Key: game.DuelStanceEvent, FiresAt: fires, Deadline: deadline})
	}
	a, err = b.store.AcceptDuel(ctx, a.ID, t.ID, ends, events, now)
	var text string
	switch {
	case errors.Is(err, store.ErrEnded):
		text = "That challenge has already ended."
	case err != nil:
		text = err.Error()
	}
	if text != "" {
		b.respond(i, &discordgo.InteractionResponseData{Content: text})
		return
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "You accepted the challenge",
			Description: fmt.Sprintf("You fight **%s** <t:%d:R>. Watch your DMs: you'll choose your stance during the bout.", c.FullName(), ends.Unix()),
			Color:       colorRed}},
	})
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title:       t.FullName() + " accepts your challenge",
		Description: fmt.Sprintf("You fight <t:%d:R>. Watch your DMs: you'll choose your stance during the bout.", ends.Unix()),
		Color:       colorRed,
	}}})
	b.editDuelAnnouncement(a, c, t, "", betButtons(a, c, t))
}

// declineOutcome is what refusing a challenge costs the defender.
func (b *Bot) declineOutcome(ctx context.Context, guildID string, defenderID int64) store.Outcome {
	sen, _ := b.store.Senators(ctx, guildID)
	st, _ := b.store.GetRealm(ctx, guildID)
	d := game.Delta{Renown: -game.DuelDeclineRenown}
	if st.Laws.Has(game.LawDuelBan) { // no shame in obeying the law
		d = game.Delta{}
	}
	d.Merge(game.DeclineCost(st.Laws))
	return store.Outcome{
		Deltas:   map[int64]game.Delta{defenderID: d},
		Opinions: game.DeclineOpinions(sen, defenderID, st.Laws),
	}
}

func (b *Bot) onDuelDecline(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	a, c, t, err := b.duelParties(ctx, id)
	if err != nil || t.UserID != userOf(i).ID {
		b.reply(i, "That challenge isn't yours to answer.")
		return
	}
	a, err = b.store.DeclineDuel(ctx, a.ID, t.ID, b.declineOutcome(ctx, a.GuildID, t.ID), time.Now())
	if err != nil {
		b.respond(i, &discordgo.InteractionResponseData{Content: "That challenge has already ended."})
		return
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "You declined the challenge",
			Description: fmt.Sprintf("You won't fight %s. Some will call it wisdom; others won't. (-%s renown)", c.FullName(), game.Num(game.DuelDeclineRenown)),
			Color:       colorGold}},
	})
	b.afterDecline(a, c, t, false)
}

// afterDecline tells the challenger and the court.
func (b *Bot) afterDecline(a store.Activity, c, t store.Character, timedOut bool) {
	how := "declined"
	if timedOut {
		how = "never answered"
	}
	refund := ""
	if a.Spend > 0 {
		refund = fmt.Sprintf(" Your %d gold wager is returned.", a.Spend)
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title:       "Your challenge was refused",
		Description: fmt.Sprintf("%s %s your challenge.%s", t.FullName(), how, refund),
		Color:       colorGold,
	}}})
	b.editDuelAnnouncement(a, c, t, fmt.Sprintf("**%s** %s **%s**'s challenge.", t.FullName(), how, c.FullName()), nil)
}

// ---- betting ----

func (b *Bot) onBetPanel(ctx context.Context, i *discordgo.InteractionCreate, id, sideID int64, note string, edit bool) {
	a, c, t, err := b.duelParties(ctx, id)
	if err != nil {
		b.reply(i, "That duel is gone.")
		return
	}
	me, err := b.store.GetCharacter(ctx, a.GuildID, userOf(i).ID)
	if err != nil {
		b.reply(i, "You have no place at this court yet.")
		return
	}
	side := c
	if sideID == t.ID {
		side = t
	}
	d := betView(a, side, me, note)
	if edit {
		b.update(i, d)
	} else {
		b.respond(i, ephemeral(d))
	}
}

func betView(a store.Activity, side, me store.Character, note string) *discordgo.InteractionResponseData {
	desc := fmt.Sprintf("Back **%s**. If they win, you and everyone else who backed them split the whole pot, by how much you each put in.\n\nYou have **%d gold**. One bet per duel.",
		side.FullName(), me.Wealth)
	if note != "" {
		desc += "\n\n" + note
	}
	var btns []discordgo.MessageComponent
	for _, g := range game.DuelBets {
		btns = append(btns, button(fmt.Sprintf("%d gold", g), fmt.Sprintf("duel:stake:%d:%d:%d", a.ID, side.ID, g), discordgo.SuccessButton, g > me.Wealth))
	}
	btns = append(btns, button("Other amount", fmt.Sprintf("duel:betcustom:%d:%d", a.ID, side.ID), discordgo.PrimaryButton, me.Wealth < 1))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Bet on " + side.FullName(), Description: desc, Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func (b *Bot) onBet(ctx context.Context, i *discordgo.InteractionCreate, id, sideID int64, gold int) {
	a, c, t, err := b.duelParties(ctx, id)
	if err != nil {
		b.update(i, &discordgo.InteractionResponseData{Content: "That duel is gone."})
		return
	}
	me, err := b.store.GetCharacter(ctx, a.GuildID, userOf(i).ID)
	if err != nil {
		b.update(i, &discordgo.InteractionResponseData{Content: "You have no place at this court yet."})
		return
	}
	side := c
	if sideID == t.ID {
		side = t
	}
	var text string
	switch err := b.store.PlaceBet(ctx, a.ID, me.ID, side.ID, gold, time.Now()); {
	case errors.Is(err, store.ErrEnded):
		text = "Betting on this duel has closed."
	case errors.Is(err, store.ErrExists):
		text = "You've already bet on this duel."
	case errors.Is(err, store.ErrNoBetting):
		text = "You can't bet on your own duel."
	case err != nil:
		text = err.Error()
	default:
		text = fmt.Sprintf("You put **%d gold** on **%s**. You'll hear how it went <t:%d:R>.", gold, side.FullName(), a.EndsAt.Unix())
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Bet on " + side.FullName(), Description: text, Color: colorGold}},
	})
}

// ---- the bout ----

func (b *Bot) resolveDuel(ctx context.Context, a store.Activity, now time.Time) {
	a, c, t, err := b.duelParties(ctx, a.ID)
	if err != nil {
		slog.Error("duel load", "err", err)
		return
	}
	if a.Goal == game.DuelPending { // nobody answered
		if a, err = b.store.DeclineDuel(ctx, a.ID, 0, b.declineOutcome(ctx, a.GuildID, t.ID), now); err != nil {
			if !errors.Is(err, store.ErrEnded) {
				slog.Error("expire duel", "err", err)
			}
			return
		}
		_, _ = b.dm(t.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
			Title:       "You let a challenge pass",
			Description: fmt.Sprintf("You never answered %s's challenge. The court noticed. (-%s renown)", c.FullName(), game.Num(game.DuelDeclineRenown)),
			Color:       colorGold,
		}}})
		b.afterDecline(a, c, t, true)
		return
	}

	open, err := b.store.OpenEvents(ctx, a.ID)
	if err != nil {
		slog.Error("open events", "err", err)
		return
	}
	for _, e := range open {
		b.expireEvent(ctx, e, now)
	}
	ev, err1 := b.eventTotals(ctx, a.ID)
	bets, err2 := b.store.DuelBets(ctx, a.ID)
	sen, err3 := b.store.Senators(ctx, a.GuildID)
	if err := errors.Join(err1, err2, err3); err != nil {
		slog.Error("duel load", "err", err)
		return
	}
	res := game.ResolveDuel(b.rng, game.DuelInput{
		Challenger: c.Person(), Defender: t.Person(), ChallengerRenown: c.Renown, DefenderRenown: t.Renown,
		ChallengerStance: ev.choices[c.ID], DefenderStance: ev.choices[t.ID],
		Wager: a.Spend, Bets: bets, Senators: sen, Laws: c.Laws,
	})
	out := store.Outcome{Deltas: res.Deltas, Opinions: res.Opinions, Conditions: b.newConditions(res.Conditions, now), Realm: res.Realm}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply duel", "err", err)
		}
		return
	}

	users := map[int64]string{c.ID: c.UserID, t.ID: t.UserID}
	for _, bt := range bets {
		if p, err := b.store.GetCharacterByID(ctx, bt.CharacterID); err == nil {
			users[p.ID] = p.UserID
		}
	}
	title := fmt.Sprintf("Duel: %s vs %s", c.FullName(), t.FullName())
	for id, lines := range res.Notices {
		if uid, ok := users[id]; ok {
			_, _ = b.dm(uid, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
				Title: title, Description: strings.Join(lines, "\n\n"), Color: colorGold,
			}}})
		}
	}
	if a.AnnounceChannelID != "" {
		public := res.Public
		if n := len(bets); n > 0 {
			public += fmt.Sprintf(" %d bet%s were settled.", n, plural(n))
		}
		_, _ = b.s.ChannelMessageSendEmbed(a.AnnounceChannelID, &discordgo.MessageEmbed{Title: title, Description: public, Color: colorRed})
		b.editDuelAnnouncement(a, c, t, public, nil)
	}
}
