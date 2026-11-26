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

// Party custom IDs:
//   party:host:<goal>                 host picks a goal; shows the budget picker
//   party:spend:<goal>:<gold>         host picks a preset budget and starts the party
//   party:custom:<goal>               host opens the custom budget popup
//   party:amount:<goal>               (modal) custom budget submitted
//   party:attend:<activity>           public "Attend" button
//   party:agoal:<activity>:<goal>     attendee picks a goal
//   party:target:<activity>           attendee picks a rumor target (user select)
//   party:event:<event>:<choice>      host answers a party event

func (b *Bot) onParty(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) < 2 {
		return
	}
	switch parts[0] {
	case "host":
		if c, ok := b.myCharacter(ctx, i); ok {
			b.update(i, b.budgetPanel(c, game.Goal(parts[1]), ""))
		}
	case "spend":
		if len(parts) > 2 {
			b.onHostParty(ctx, i, game.Goal(parts[1]), int(atoi(parts[2])))
		}
	case "custom":
		if err := b.s.InteractionRespond(i.Interaction, amountModal("party:amount:"+parts[1], "How much will you spend?",
			game.PartyMinSpend, game.PartyMaxSpend)); err != nil {
			slog.Error("budget modal", "err", err)
		}
	case "attend":
		b.onAttend(ctx, i, atoi(parts[1]))
	case "agoal":
		if len(parts) > 2 {
			b.onAttendGoal(ctx, i, atoi(parts[1]), game.Goal(parts[2]))
		}
	case "target":
		b.onRumorTarget(ctx, i, atoi(parts[1]))
	case "event":
		if len(parts) > 2 {
			b.onEventChoice(ctx, i, atoi(parts[1]), parts[2])
		}
	}
}

func atoi(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }

// ---- hosting ----

// onPartyModal handles the custom budget popup.
func (b *Bot) onPartyModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) < 2 || parts[0] != "amount" {
		return
	}
	goal := game.Goal(parts[1])
	gold, ok := modalAmount(i)
	if !ok {
		if c, ok := b.myCharacter(ctx, i); ok {
			b.update(i, b.budgetPanel(c, goal, "That wasn't a whole number of gold."))
		}
		return
	}
	b.onHostParty(ctx, i, goal, gold)
}

// budgetPanel asks the host how much to spend once they've picked a goal.
func (b *Bot) budgetPanel(c store.Character, goal game.Goal, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Goal: **%s**.\n\n", game.GoalName(goal))
	sb.WriteString("How much gold will you put into the evening? Finer wine, better musicians and a grander hall make the party ")
	sb.WriteString("more likely to succeed, and you gain more from it if it does. Each extra coin helps a little less than the last.\n\n")
	fmt.Fprintf(&sb, "You have **%d gold**. Spend anything from %d to %d.", c.Wealth, game.PartyMinSpend, game.PartyMaxSpend)
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	var btns []discordgo.MessageComponent
	for _, bud := range game.PartyBudgets {
		btns = append(btns, button(fmt.Sprintf("%s (%d gold)", bud.Name, bud.Gold),
			fmt.Sprintf("party:spend:%s:%d", goal, bud.Gold), discordgo.SuccessButton, c.Wealth < bud.Gold))
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Throw a Party", Description: sb.String(), Color: colorGold}},
		Components: []discordgo.MessageComponent{
			row(btns...),
			row(button("Custom amount", "party:custom:"+string(goal), discordgo.PrimaryButton, c.Wealth < game.PartyMinSpend),
				button("Back", "menu:act:"+game.KindParty, discordgo.SecondaryButton, false)),
		},
	}
}

// article is "a" or "an" for a word.
func article(w string) string {
	if w != "" && strings.ContainsRune("AEIOUaeiou", rune(w[0])) {
		return "an"
	}
	return "a"
}

func (b *Bot) onHostParty(ctx context.Context, i *discordgo.InteractionCreate, goal game.Goal, spend int) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	if spend < game.PartyMinSpend || spend > game.PartyMaxSpend {
		b.update(i, b.budgetPanel(c, goal, fmt.Sprintf("Spend between %d and %d gold.", game.PartyMinSpend, game.PartyMaxSpend)))
		return
	}
	if spend > c.Wealth {
		b.update(i, b.budgetPanel(c, goal, fmt.Sprintf("You only have %d gold.", c.Wealth)))
		return
	}
	if st, err := b.store.GetRealm(ctx, c.GuildID); err == nil && !st.Alive() {
		b.update(i, b.partyPanel(ctx, c, "The realm has fallen. There is nothing left to celebrate."))
		return
	}
	now := time.Now()
	t := b.timing
	ends := now.Add(t.Duration)

	// One or two events at random moments in the party.
	var events []store.NewEvent
	pool := game.PartyEventsFor(c.Role)
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
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindParty, Goal: string(goal),
		Public: true, StartedAt: now, EndsAt: ends,
	}, spend, events)
	if err != nil {
		b.update(i, b.partyPanel(ctx, c, err.Error()))
		return
	}
	budget := strings.ToLower(game.PartyBudgetName(spend))

	where := ""
	if court, err := b.store.GetCourt(ctx, c.GuildID); err == nil && court.AnnouncementsChannelID != "" {
		msg, err := b.s.ChannelMessageSendComplex(court.AnnouncementsChannelID, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{{
				Title:       fmt.Sprintf("%s is throwing %s %s party", c.FullName(), article(budget), budget),
				Description: fmt.Sprintf("The doors of House %s's estate are open. The party ends <t:%d:R>.", c.FamilyName, ends.Unix()),
				Color:       colorGold,
			}},
			Components: []discordgo.MessageComponent{row(button("Attend the party", fmt.Sprintf("party:attend:%d", a.ID), discordgo.SuccessButton, false))},
		})
		if err != nil {
			slog.Warn("announce party", "err", err)
		} else {
			_ = b.store.SetAnnouncement(ctx, a.ID, msg.ChannelID, msg.ID)
			where = fmt.Sprintf(" The court has been told in <#%s>.", msg.ChannelID)
		}
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "Your party has begun",
			Description: fmt.Sprintf("Goal: **%s**. You spent **%d gold**. It ends <t:%d:R>.%s\nWatch your DMs: I'll message you if anything happens during the evening.",
				game.GoalName(goal), spend, ends.Unix(), where),
			Color: colorGreen,
		}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:home", discordgo.SecondaryButton, false))},
	})
}

// ---- attending ----

// attendCheck loads the party and the caller's character and validates.
func (b *Bot) attendCheck(ctx context.Context, i *discordgo.InteractionCreate, activityID int64) (store.Activity, store.Character, string) {
	a, err := b.store.GetActivity(ctx, activityID)
	if err != nil {
		return a, store.Character{}, "That party doesn't exist."
	}
	c, err := b.store.GetCharacter(ctx, a.GuildID, userOf(i).ID)
	if err != nil {
		return a, c, "You have no place at this court yet."
	}
	switch {
	case a.Resolved || !time.Now().Before(a.EndsAt):
		return a, c, "That party is over."
	case a.HostID == c.ID:
		return a, c, "It's your party. You're already there."
	}
	return a, c, ""
}

func (b *Bot) onAttend(ctx context.Context, i *discordgo.InteractionCreate, activityID int64) {
	a, _, msg := b.attendCheck(ctx, i, activityID)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	host, _ := b.store.GetCharacterByID(ctx, a.HostID)
	var sb strings.Builder
	var btns []discordgo.MessageComponent
	for _, g := range game.AttendeeGoals {
		fmt.Fprintf(&sb, "**%s** — %s\n", g.Name, g.Description)
		btns = append(btns, button(g.Name, fmt.Sprintf("party:agoal:%d:%s", a.ID, g.Key), discordgo.PrimaryButton, false))
	}
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: fmt.Sprintf("Attend %s's party", host.FullName()), Description: sb.String(), Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}))
}

func (b *Bot) onAttendGoal(ctx context.Context, i *discordgo.InteractionCreate, activityID int64, goal game.Goal) {
	a, c, msg := b.attendCheck(ctx, i, activityID)
	if msg != "" {
		b.update(i, &discordgo.InteractionResponseData{Content: msg, Embeds: []*discordgo.MessageEmbed{}})
		return
	}
	if goal == game.GoalSpreadRumors {
		b.update(i, &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{{Title: "Spread Rumors", Description: "Who will you whisper about?", Color: colorGold}},
			Components: []discordgo.MessageComponent{row(discordgo.SelectMenu{
				MenuType: discordgo.UserSelectMenu, CustomID: fmt.Sprintf("party:target:%d", a.ID), Placeholder: "Choose a player…",
			})},
		})
		return
	}
	b.joinParty(ctx, i, a, c, goal, 0)
}

func (b *Bot) onRumorTarget(ctx context.Context, i *discordgo.InteractionCreate, activityID int64) {
	a, c, msg := b.attendCheck(ctx, i, activityID)
	if msg != "" {
		b.update(i, &discordgo.InteractionResponseData{Content: msg, Embeds: []*discordgo.MessageEmbed{}})
		return
	}
	vals := i.MessageComponentData().Values
	if len(vals) == 0 {
		return
	}
	target, err := b.store.GetCharacter(ctx, a.GuildID, vals[0])
	if err != nil || target.ID == c.ID {
		// Leave the selector up so they can pick again.
		b.update(i, &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{{Title: "Spread Rumors",
				Description: "Pick another player who has a place at this court.", Color: colorRed}},
			Components: []discordgo.MessageComponent{row(discordgo.SelectMenu{
				MenuType: discordgo.UserSelectMenu, CustomID: fmt.Sprintf("party:target:%d", a.ID), Placeholder: "Choose a player…",
			})},
		})
		return
	}
	b.joinParty(ctx, i, a, c, game.GoalSpreadRumors, target.ID)
}

func (b *Bot) joinParty(ctx context.Context, i *discordgo.InteractionCreate, a store.Activity, c store.Character, goal game.Goal, targetID int64) {
	err := b.store.AddParticipant(ctx, a.ID, c.ID, string(goal), targetID, time.Now())
	var text string
	switch {
	case errors.Is(err, store.ErrExists):
		text = "You're already at this party."
	case errors.Is(err, store.ErrEnded):
		text = "That party is over."
	case err != nil:
		slog.Error("join party", "err", err)
		text = "Something went wrong."
	default:
		text = fmt.Sprintf("You're at the party to **%s**. You'll hear how it went <t:%d:R>.", strings.ToLower(game.GoalName(goal)), a.EndsAt.Unix())
		if goal == game.GoalSpreadRumors {
			if t, err := b.store.GetCharacterByID(ctx, targetID); err == nil {
				defer b.leak(ctx, c, game.SecretRumor(c.FullName(), t.ID, t.FullName()))
			}
		}
	}
	b.update(i, &discordgo.InteractionResponseData{Content: text, Embeds: []*discordgo.MessageEmbed{}})
}

// ---- events during the party ----

// eventWhere is the title prefix for an activity's DM events.
func (b *Bot) eventWhere(ctx context.Context, activityID int64) string {
	a, err := b.store.GetActivity(ctx, activityID)
	if err != nil {
		return "At your party"
	}
	switch a.Kind {
	case game.KindHunt:
		return "On your hunt"
	case game.KindDuel:
		return "On the dueling ground"
	}
	return "At your party"
}

// eventResults sums an activity's answered events: quality and the host's
// delta (events happen to the host), and any conditions they gave.
type eventResults struct {
	quality    float64
	delta      game.Delta
	conditions []game.ConditionChange
	choices    map[int64]string // character ID → last choice, e.g. duel stances
}

func (b *Bot) eventTotals(ctx context.Context, activityID int64) (eventResults, error) {
	out := eventResults{choices: map[int64]string{}}
	results, err := b.store.EventResults(ctx, activityID)
	if err != nil {
		return out, err
	}
	for _, e := range results {
		out.choices[e.CharacterID] = e.Choice
		if e.Result == nil {
			continue
		}
		out.quality += e.Result.Quality
		out.delta.Merge(e.Result.Delta)
		if e.Result.Condition != "" {
			out.conditions = append(out.conditions, game.ConditionChange{CharacterID: e.CharacterID, Key: e.Result.Condition})
		}
	}
	return out, nil
}

// newConditions turns condition changes into stored conditions ending a
// few game days from now.
func (b *Bot) newConditions(cs []game.ConditionChange, now time.Time) []store.NewCondition {
	var out []store.NewCondition
	for _, c := range cs {
		if d, ok := game.ConditionByKey(c.Key); ok {
			out = append(out, store.NewCondition{CharacterID: c.CharacterID, Key: c.Key, EndsAt: d.Ends(now, b.t.Day)})
		}
	}
	return out
}

func (b *Bot) fireEvent(ctx context.Context, e store.Event, now time.Time) {
	ev, ok := game.EventByKey(e.Key)
	if !ok {
		_ = b.store.MarkEventSent(ctx, e.ID, "", "", now)
		return
	}
	var btns []discordgo.MessageComponent
	for _, c := range ev.Choices {
		btns = append(btns, button(c.Label, fmt.Sprintf("party:event:%d:%s", e.ID, c.Key), discordgo.PrimaryButton, false))
	}
	def, _ := ev.Choice(ev.Default)
	msg, err := b.dm(e.UserID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{{
			Title: b.eventWhere(ctx, e.ActivityID) + ": " + ev.Title, Description: ev.Prompt, Color: colorGold,
			Fields: []*discordgo.MessageEmbedField{{Name: "Decide", Value: fmt.Sprintf("<t:%d:R>, or I'll assume: *%s*", e.Deadline.Unix(), def.Label)}},
		}},
		Components: []discordgo.MessageComponent{row(btns...)},
	})
	ch, id := "", ""
	if err == nil {
		ch, id = msg.ChannelID, msg.ID
	}
	if err := b.store.MarkEventSent(ctx, e.ID, ch, id, now); err != nil {
		slog.Error("mark event sent", "err", err)
	}
}

func eventResultEmbed(where string, ev game.PartyEvent, choice game.EventChoice, out game.EventOutcome, timedOut bool) *discordgo.MessageEmbed {
	head := "You chose: " + choice.Label
	if timedOut {
		head = "You didn't answer in time: " + choice.Label
	}
	return &discordgo.MessageEmbed{
		Title:       where + ": " + ev.Title,
		Description: fmt.Sprintf("%s\n*%s*\n\n%s", ev.Prompt, head, out.Text),
		Color:       colorGold,
	}
}

func (b *Bot) onEventChoice(ctx context.Context, i *discordgo.InteractionCreate, eventID int64, choiceKey string) {
	e, err := b.store.GetEvent(ctx, eventID)
	if err != nil || e.UserID != userOf(i).ID {
		b.reply(i, "That isn't yours to decide.")
		return
	}
	ev, ok := game.EventByKey(e.Key)
	if !ok {
		return
	}
	host, err := b.store.GetCharacterByID(ctx, e.CharacterID)
	if err != nil {
		b.fail(i, err)
		return
	}
	choice, out := game.ResolveEventChoice(b.rng, ev, choiceKey, host.Sheet())
	if err := b.store.ResolveEvent(ctx, e.ID, choice.Key, out, time.Now()); errors.Is(err, store.ErrEnded) {
		b.update(i, &discordgo.InteractionResponseData{Content: "Too late, the moment has passed."})
		return
	} else if err != nil {
		b.fail(i, err)
		return
	}
	b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{eventResultEmbed(b.eventWhere(ctx, e.ActivityID), ev, choice, out, false)}})
}

// expireEvent applies the default choice to an unanswered event.
func (b *Bot) expireEvent(ctx context.Context, e store.Event, now time.Time) {
	ev, ok := game.EventByKey(e.Key)
	if !ok {
		_ = b.store.ResolveEvent(ctx, e.ID, "", game.EventOutcome{}, now)
		return
	}
	host, err := b.store.GetCharacterByID(ctx, e.CharacterID)
	if err != nil {
		slog.Error("expire event", "err", err)
		return
	}
	choice, out := game.ResolveEventChoice(b.rng, ev, ev.Default, host.Sheet())
	if err := b.store.ResolveEvent(ctx, e.ID, choice.Key, out, now); err != nil {
		return // answered in the meantime
	}
	if e.DMMessageID != "" {
		embeds := []*discordgo.MessageEmbed{eventResultEmbed(b.eventWhere(ctx, e.ActivityID), ev, choice, out, true)}
		empty := []discordgo.MessageComponent{}
		_, _ = b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: e.DMChannelID, ID: e.DMMessageID, Embeds: &embeds, Components: &empty})
	}
}

// ---- end of the party ----

func (b *Bot) resolveParty(ctx context.Context, a store.Activity, now time.Time) {
	// Any event still open takes its default.
	open, err := b.store.OpenEvents(ctx, a.ID)
	if err != nil {
		slog.Error("open events", "err", err)
		return
	}
	for _, e := range open {
		b.expireEvent(ctx, e, now)
	}

	host, err := b.store.GetCharacterByID(ctx, a.HostID)
	if err != nil {
		slog.Error("party host", "err", err)
		return
	}
	in := game.PartyInput{Host: host.Person(), Goal: game.Goal(a.Goal), Spend: a.Spend}
	ev, err := b.eventTotals(ctx, a.ID)
	if err != nil {
		slog.Error("event results", "err", err)
		return
	}
	in.Quality = ev.quality
	eventDelta := ev.delta

	users := map[int64]string{host.ID: host.UserID}
	parts, err := b.store.Participants(ctx, a.ID)
	if err != nil {
		slog.Error("participants", "err", err)
		return
	}
	for _, p := range parts {
		att := game.Attendee{Person: p.Character.Person(), Goal: game.Goal(p.Goal)}
		users[p.Character.ID] = p.Character.UserID
		if p.TargetID != 0 {
			if t, err := b.store.GetCharacterByID(ctx, p.TargetID); err == nil {
				tp := t.Person()
				att.Target = &tp
				users[t.ID] = t.UserID
			}
		}
		in.Attendees = append(in.Attendees, att)
	}

	res := game.ResolveParty(b.rng, in)
	hd := res.Deltas[host.ID]
	hd.Merge(eventDelta)
	res.Deltas[host.ID] = hd

	out := store.Outcome{Deltas: res.Deltas, Opinions: res.Opinions,
		Conditions: b.newConditions(append(res.Conditions, ev.conditions...), now)}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply party", "err", err)
		}
		return
	}

	title := fmt.Sprintf("%s's party is over", host.FullName())
	for id, lines := range res.Notices {
		if uid, ok := users[id]; ok {
			_, _ = b.dm(uid, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
				Title: title, Description: strings.Join(lines, "\n\n"), Color: colorGold,
			}}})
		}
	}

	if a.AnnounceChannelID != "" {
		public := res.Public
		if n := len(parts); n > 0 {
			public += fmt.Sprintf(" %d guest%s attended.", n, plural(n))
		}
		color := colorGreen
		if !res.Success {
			color = colorRed
		}
		_, _ = b.s.ChannelMessageSendEmbed(a.AnnounceChannelID, &discordgo.MessageEmbed{Title: title, Description: public, Color: color})
		if a.AnnounceMessageID != "" {
			ended := []*discordgo.MessageEmbed{{Title: title, Description: public, Color: color}}
			empty := []discordgo.MessageComponent{}
			_, _ = b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: a.AnnounceChannelID, ID: a.AnnounceMessageID, Embeds: &ended, Components: &empty})
		}
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func humanDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		h := int(d / time.Hour)
		return fmt.Sprintf("%d hour%s", h, plural(h))
	}
	m := int(d / time.Minute)
	return fmt.Sprintf("%d minute%s", m, plural(m))
}
