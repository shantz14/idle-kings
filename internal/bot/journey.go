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
	registerActivity(game.KindJourney, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.journeyPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveJourney,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			place := "the road"
			if d, ok := game.JourneyDestByKey(a.Goal); ok {
				place = d.Place
			}
			return fmt.Sprintf("You are travelling to %s. You'll be home <t:%d:R>.", place, a.EndsAt.Unix())
		},
		eventWhere: "On the road",
	})
	componentRoutes["journey"] = (*Bot).onJourney
}

// Journey custom IDs:
//   journey:go:<dest>              set out
//   journey:hire:<journey>         public "Hire bandits" button → private band panel
//   journey:band:<journey>:<band>  pay a band to rob the traveller
// Road events use the shared party:event:<event>:<choice> IDs.

func (b *Bot) onJourney(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) < 2 {
		return
	}
	switch parts[0] {
	case "go":
		b.onSetOut(ctx, i, parts[1])
	case "hire":
		b.onHirePanel(ctx, i, atoi(parts[1]))
	case "band":
		if len(parts) > 2 {
			b.onHireBand(ctx, i, atoi(parts[1]), parts[2])
		}
	}
}

// ---- setting out ----

func (b *Bot) journeyPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	busy := ""
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		busy = b.activityLine(ctx, a)
	} else if c.RealmFallen {
		busy = "The realm has fallen. Every road is a bandit's road now."
	}
	return journeyView(b.t.Journey, c.Wealth, busy, note)
}

func journeyView(t game.PartyTiming, wealth int, busy, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Travel the realm for %s, or go abroad for %s. ", humanDuration(t.Duration), humanDuration(2*t.Duration))
	sb.WriteString("I'll DM you when something happens on the road. The court is told you've left, and anyone may pay bandits to meet you on the way home. ")
	sb.WriteString("Your martial skill, your sword arm and the guards your estate can afford are what stand between them and your purse.\n\n")
	var btns []discordgo.MessageComponent
	for _, d := range game.JourneyDests {
		where := "in the realm"
		if d.Abroad {
			where = "abroad"
		}
		fmt.Fprintf(&sb, "%s **%s** (%s, %d gold) — %s\n", d.Emoji, d.Name, where, d.Cost, d.Description)
		btns = append(btns, button(truncate(d.Name, 80), "journey:go:"+d.Key, discordgo.SuccessButton, busy != "" || wealth < d.Cost))
	}
	fmt.Fprintf(&sb, "\nYou have **%d gold**.", wealth)
	if busy != "" {
		sb.WriteString("\n" + busy)
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	btns = append(btns, backButton(game.KindJourney))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Undertake a Journey", Description: sb.String(), Color: colorGold}},
		Components: rows(btns),
	}
}

func (b *Bot) onSetOut(ctx context.Context, i *discordgo.InteractionCreate, key string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	d, ok := game.JourneyDestByKey(key)
	if !ok {
		return
	}
	if c.RealmFallen {
		b.update(i, b.journeyPanel(ctx, c, ""))
		return
	}
	now := time.Now()
	t := d.Scaled(b.t.Journey)
	ends := now.Add(t.Duration)
	pool := game.JourneyEventsFor(d)
	var events []store.NewEvent
	n := min(1+b.rng.IntN(2), len(pool))
	for _, idx := range b.rng.Perm(len(pool))[:n] {
		fires := now.Add(t.EventEarliest + time.Duration(b.rng.IntN(int((t.EventLatest-t.EventEarliest)/time.Second)+1))*time.Second)
		deadline := fires.Add(t.AnswerWindow)
		if latest := ends.Add(-10 * time.Second); deadline.After(latest) {
			deadline = latest
		}
		events = append(events, store.NewEvent{Key: pool[idx].Key, FiresAt: fires, Deadline: deadline})
	}
	a, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindJourney, Goal: d.Key,
		Public: true, StartedAt: now, EndsAt: ends,
	}, d.Cost, events)
	if err != nil {
		b.update(i, b.journeyPanel(ctx, c, err.Error()))
		return
	}
	where := b.announce(ctx, a, &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s %s sets out for %s", d.Emoji, c.FullName(), d.Place),
		Description: fmt.Sprintf("**%s**: %s. Expected home <t:%d:R>. The roads are long, and not everyone on them is friendly.", d.Name, strings.ToLower(d.Description[:1])+d.Description[1:], ends.Unix()),
		Color:       colorGold,
	}, []discordgo.MessageComponent{row(button("Hire bandits", fmt.Sprintf("journey:hire:%d", a.ID), discordgo.DangerButton, false))})
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "On the road",
			Description: fmt.Sprintf("**%s**. You spent **%d gold**. You'll be home <t:%d:R>.%s\nWatch your DMs: I'll message you when something happens on the road.",
				d.Name, d.Cost, ends.Unix(), where),
			Color: colorGreen,
		}},
		Components: []discordgo.MessageComponent{homeRow()},
	})
}

// ---- hiring bandits ----

// hireCheck loads a journey someone wants to rob, or says why they can't.
func (b *Bot) hireCheck(ctx context.Context, i *discordgo.InteractionCreate, id int64) (store.Activity, store.Character, store.Character, string) {
	a, err := b.store.GetActivity(ctx, id)
	if err != nil || a.Kind != game.KindJourney {
		return a, store.Character{}, store.Character{}, "That journey doesn't exist."
	}
	c, err := b.store.GetCharacter(ctx, a.GuildID, userOf(i).ID)
	if err != nil {
		return a, c, store.Character{}, "You have no place at this court yet."
	}
	traveler, err := b.store.GetCharacterByID(ctx, a.HostID)
	switch {
	case err != nil:
		return a, c, traveler, "That journey doesn't exist."
	case a.Resolved || !time.Now().Before(a.EndsAt):
		return a, c, traveler, "They're already home."
	case a.HostID == c.ID:
		return a, c, traveler, "It's your own journey. Hire guards, not bandits."
	}
	return a, c, traveler, ""
}

func (b *Bot) onHirePanel(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	a, c, traveler, msg := b.hireCheck(ctx, i, id)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	b.respond(i, ephemeral(hireView(a, c, traveler)))
}

func hireView(a store.Activity, c, traveler store.Character) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Pay a band to meet **%s** on the road home. Nobody will know it was you, unless they beat your bandits and one of them talks. ", traveler.FullName())
	sb.WriteString("If the robbery works, the bandits split the loot with you. A tougher band costs more, but is harder to beat.\n\n")
	var btns []discordgo.MessageComponent
	for _, bd := range game.BanditBands {
		fmt.Fprintf(&sb, "**%s** (%d gold) — %s\n", bd.Name, bd.Price, bd.Desc)
		btns = append(btns, button(fmt.Sprintf("%s: %d gold", bd.Name, bd.Price), fmt.Sprintf("journey:band:%d:%s", a.ID, bd.Key), discordgo.DangerButton, c.Wealth < bd.Price))
	}
	fmt.Fprintf(&sb, "\nYou have **%d gold**. You can only hire one band for this journey.", c.Wealth)
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Hire bandits", Description: sb.String(), Color: colorRed}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func (b *Bot) onHireBand(ctx context.Context, i *discordgo.InteractionCreate, id int64, key string) {
	a, c, traveler, msg := b.hireCheck(ctx, i, id)
	if msg != "" {
		b.update(i, &discordgo.InteractionResponseData{Content: msg, Embeds: []*discordgo.MessageEmbed{}, Components: []discordgo.MessageComponent{}})
		return
	}
	band, ok := game.BanditBandByKey(key)
	if !ok {
		return
	}
	var text string
	switch err := b.store.HireBandits(ctx, a.ID, c.ID, band, time.Now()); {
	case errors.Is(err, store.ErrExists):
		text = "You've already paid a band to meet them."
	case errors.Is(err, store.ErrEnded):
		text = "They're already home."
	case errors.Is(err, store.ErrOwnEvent):
		text = "It's your own journey."
	case err != nil:
		text = err.Error()
	default:
		text = fmt.Sprintf("You paid **%s** %d gold. They'll be waiting for %s on the road home. You'll hear how it went <t:%d:R>.",
			strings.ToLower(band.Name), band.Price, traveler.FullName(), a.EndsAt.Unix())
		defer b.leak(ctx, c, game.SecretHiredBandits(c.FullName(), traveler.FullName(), strings.ToLower(band.Name), band.Price))
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Hire bandits", Description: text, Color: colorRed}},
		Components: []discordgo.MessageComponent{},
	})
}

// ---- coming home ----

func (b *Bot) resolveJourney(ctx context.Context, a store.Activity, now time.Time) {
	open, err := b.store.OpenEvents(ctx, a.ID)
	if err != nil {
		slog.Error("open events", "err", err)
		return
	}
	for _, e := range open {
		b.expireEvent(ctx, e, now)
	}
	c, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	ev, err2 := b.eventTotals(ctx, a.ID)
	parts, err3 := b.store.Participants(ctx, a.ID)
	sen, err4 := b.store.Senators(ctx, a.GuildID)
	d, okDest := game.JourneyDestByKey(a.Goal)
	before, okRealm := b.realmBefore(ctx, a.GuildID)
	if err := errors.Join(err1, err2, err3, err4); err != nil || !okDest || !okRealm {
		slog.Error("journey load", "err", err, "dest", a.Goal)
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now)
		return
	}

	res := game.ResolveJourney(b.rng, c.Person(), d, ev.quality)
	td := res.Delta
	td.Merge(ev.delta)
	conds := append([]game.ConditionChange{}, ev.conditions...)
	out := store.Outcome{Deltas: map[int64]game.Delta{}, Realm: res.Realm}
	lines := []string{withDelta(fmt.Sprintf("%s %s", d.Emoji, res.Text), res.Delta)}

	// What the bandits can find: never more than the traveller has.
	purse := min(res.Purse(d), max(c.Wealth+td.Wealth, 0))
	type hirerNote struct {
		userID, text string
	}
	var notes []hirerNote
	var publics []string
	robbed, beaten := 0, 0
	for _, p := range parts {
		band, ok := game.BandFromGoal(p.Goal)
		if !ok {
			continue
		}
		am := game.ResolveAmbush(b.rng, c.Person(), p.Character.Person(), band, purse, ev.quality, sen)
		purse -= am.Loot
		td.Merge(am.Traveler)
		hd := out.Deltas[p.Character.ID]
		hd.Merge(am.Hirer)
		out.Deltas[p.Character.ID] = hd
		out.Opinions = append(out.Opinions, am.Opinions...)
		if am.Wounded {
			conds = append(conds, game.ConditionChange{CharacterID: c.ID, Key: game.CondWounded})
		}
		lines = append(lines, am.ToTraveler)
		notes = append(notes, hirerNote{p.Character.UserID, am.ToHirer})
		if am.Public != "" {
			publics = append(publics, am.Public)
		}
		if am.Loot > 0 {
			robbed++
		} else {
			beaten++
		}
	}
	out.Deltas[c.ID] = td
	out.Conditions = b.newConditions(conds, now)
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply journey", "err", err)
		}
		return
	}
	if res.Gift != "" {
		if it, ok := game.ItemByKey(res.Gift); ok {
			if _, err := b.store.BuyItem(ctx, c.ID, c.GuildID, it.Key, 0, now); err != nil {
				slog.Error("journey gift", "err", err)
			} else {
				lines = append(lines, fmt.Sprintf("🎁 You bring home a gift: **%s**. It's in your estate.", it.Name))
			}
		}
	}
	if len(res.Realm) > 0 {
		b.afterRealmChange(ctx, a.GuildID, before)
	}

	title := fmt.Sprintf("Home from %s", d.Place)
	color := colorGreen
	if !res.Success || robbed > 0 {
		color = colorGold
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: title, Description: strings.Join(lines, "\n\n"), Color: color,
	}}})
	for _, n := range notes {
		_, _ = b.dm(n.userID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
			Title: "Word from the road", Description: n.text, Color: colorRed,
		}}})
	}

	public := fmt.Sprintf("**%s** has come home from %s", c.FullName(), d.Place)
	if res.Success {
		public += ", and the journey was a success."
	} else {
		public += ", with little to show for it."
	}
	switch {
	case robbed > 0 && beaten > 0:
		public += " Bandits fell on them more than once on the road: some were beaten off, some got away with gold."
	case robbed > 0:
		public += " Bandits robbed them on the road."
	case beaten > 0:
		public += " Bandits ambushed them on the road, and were beaten off."
	}
	if len(publics) > 0 {
		public += "\n\n" + strings.Join(publics, "\n")
	}
	b.announceEnd(a, &discordgo.MessageEmbed{Title: fmt.Sprintf("%s %s is home", d.Emoji, c.FullName()), Description: public, Color: color})
}
