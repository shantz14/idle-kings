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
	registerActivity(game.KindHunt, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.huntPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveHunt,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("You are leading a hunt. It ends <t:%d:R>.", a.EndsAt.Unix())
		},
		eventWhere: "On your hunt",
	})
	componentRoutes["hunt"] = (*Bot).onHunt
}

// Hunt custom IDs:
//   hunt:host:<goal>              host picks a goal and rides out
//   hunt:join:<hunt>              public "Join the hunt" button
//   hunt:jgoal:<hunt>:<goal>      rider picks a goal
//   hunt:target:<hunt>            (user select) rider picks who has the accident
// Events during the hunt use the shared party:event:<event>:<choice> IDs.

func (b *Bot) onHunt(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) < 2 {
		return
	}
	switch parts[0] {
	case "host":
		b.onHostHunt(ctx, i, game.Goal(parts[1]))
	case "join":
		b.onJoinHunt(ctx, i, atoi(parts[1]))
	case "jgoal":
		if len(parts) > 2 {
			b.onHuntGoal(ctx, i, atoi(parts[1]), game.Goal(parts[2]))
		}
	case "target":
		b.onHuntTarget(ctx, i, atoi(parts[1]))
	}
}

// ---- hosting ----

func (b *Bot) huntPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	busy := ""
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil {
		busy = b.activityLine(ctx, a)
	} else if c.RealmFallen {
		busy = "The realm has fallen. The royal forest burns."
	} else if c.Wealth < game.HuntCost {
		busy = fmt.Sprintf("You only have %d gold.", c.Wealth)
	}
	return huntView(b.t.Hunt.Duration, busy, joinNotes(b.happeningNote(ctx, c.GuildID, game.MeetHunt), note))
}

func huntView(dur time.Duration, busy, note string) *discordgo.InteractionResponseData {
	desc := "Ride out with horses and hounds. It goes best with prowess, then martial skill, renown and a fine estate. " +
		"Riders get hurt out there, by accident or otherwise.\n\n**Choose your goal**\n" + goalList(game.HuntHostGoals)
	var btns []discordgo.MessageComponent
	for _, g := range game.HuntHostGoals {
		btns = append(btns, button(g.Name, "hunt:host:"+string(g.Key), discordgo.SuccessButton, busy != ""))
	}
	btns = append(btns, backButton(game.KindHunt))
	e := actionEmbed(game.KindHunt, withNote(busyNote(busy, note), desc))
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Cost", fmt.Sprintf("%d gold", game.HuntCost)),
		fact("Lasts", humanDuration(dur)),
		fact("Riders", "📣 Anyone may join"),
	}
	e.Footer = footer("🔔 The forest is full of surprises. I'll DM you when one comes.")
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func (b *Bot) onHostHunt(ctx context.Context, i *discordgo.InteractionCreate, goal game.Goal) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	valid := false
	for _, g := range game.HuntHostGoals {
		valid = valid || g.Key == goal
	}
	if !valid {
		return
	}
	if c.RealmFallen {
		b.update(i, b.huntPanel(ctx, c, ""))
		return
	}
	now := time.Now()
	t := b.t.Hunt
	ends := now.Add(t.Duration)
	var events []store.NewEvent
	n := min(1+b.rng.IntN(2), len(game.HuntEvents))
	for _, idx := range b.rng.Perm(len(game.HuntEvents))[:n] {
		fires := now.Add(t.EventEarliest + time.Duration(b.rng.IntN(int((t.EventLatest-t.EventEarliest)/time.Second)+1))*time.Second)
		deadline := fires.Add(t.AnswerWindow)
		if latest := ends.Add(-10 * time.Second); deadline.After(latest) {
			deadline = latest
		}
		events = append(events, store.NewEvent{Key: game.HuntEvents[idx].Key, FiresAt: fires, Deadline: deadline})
	}
	events = b.withHappening(ctx, c.GuildID, game.MeetHunt, events)
	a, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindHunt, Goal: string(goal),
		Public: true, StartedAt: now, EndsAt: ends,
	}, game.HuntCost, events)
	if err != nil {
		b.update(i, b.huntPanel(ctx, c, err.Error()))
		return
	}
	where := b.announce(ctx, a, &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🦌 %s rides out to hunt", c.FullName()),
		Description: fmt.Sprintf("Horns sound at House %s. Saddle up and ride along.", c.FamilyName),
		Color:       colorPanel,
		Fields:      []*discordgo.MessageEmbedField{fact("Returns", relTime(ends))},
	}, []discordgo.MessageComponent{row(withEmoji(button("Join the hunt", fmt.Sprintf("hunt:join:%d", a.ID), discordgo.SuccessButton, false), "🐎"))})

	v := startedView("🦌 The hunt is on", where,
		fact("Goal", game.HuntGoalName(goal)), fact("Spent", fmt.Sprintf("%d gold", game.HuntCost)), fact("Back", relTime(ends)))
	v.Embeds[0].Footer = footer("🔔 Watch your DMs: I'll message you when something happens in the forest.")
	b.update(i, v)
}

// ---- joining ----

func (b *Bot) huntJoinCheck(ctx context.Context, i *discordgo.InteractionCreate, id int64) (store.Activity, store.Character, string) {
	a, err := b.store.GetActivity(ctx, id)
	if err != nil || a.Kind != game.KindHunt {
		return a, store.Character{}, "That hunt doesn't exist."
	}
	c, err := b.store.GetCharacter(ctx, a.GuildID, userOf(i).ID)
	if err != nil {
		return a, c, "You have no place at this court yet."
	}
	switch {
	case a.Resolved || !time.Now().Before(a.EndsAt):
		return a, c, "That hunt is over."
	case a.HostID == c.ID:
		return a, c, "It's your hunt. You're already in the saddle."
	}
	return a, c, ""
}

func (b *Bot) onJoinHunt(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	a, _, msg := b.huntJoinCheck(ctx, i, id)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	host, _ := b.store.GetCharacterByID(ctx, a.HostID)
	b.respond(i, ephemeral(huntJoinView(a, host)))
}

func huntJoinView(a store.Activity, host store.Character) *discordgo.InteractionResponseData {
	var sb strings.Builder
	var btns []discordgo.MessageComponent
	sb.WriteString("What will you do in the forest?\n\n" + goalList(game.HuntGuestGoals))
	for _, g := range game.HuntGuestGoals {
		style := discordgo.PrimaryButton
		if g.Key == game.GoalAccident {
			style = discordgo.DangerButton
		}
		btns = append(btns, button(g.Name, fmt.Sprintf("hunt:jgoal:%d:%s", a.ID, g.Key), style, false))
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: fmt.Sprintf("🐎 Ride with %s", host.FullName()), Description: sb.String(), Color: colorPanel,
			Fields: []*discordgo.MessageEmbedField{fact("Returns", relTime(a.EndsAt))},
			Footer: footer("Riding along doesn't use your action.")}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func accidentPicker(id int64, note string) *discordgo.InteractionResponseData {
	desc := withNote(note, "Who will have the accident? They must be riding on this hunt (the host counts) when it ends, or your plan comes to nothing.")
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "🏹 Arrange an Accident", Description: desc, Color: colorSecret,
			Footer: footer("Victims and hosts may notice, and learn who did it.")}},
		Components: []discordgo.MessageComponent{row(discordgo.SelectMenu{
			MenuType: discordgo.UserSelectMenu, CustomID: fmt.Sprintf("hunt:target:%d", id), Placeholder: "Choose a rider…",
		})},
	}
}

func (b *Bot) onHuntGoal(ctx context.Context, i *discordgo.InteractionCreate, id int64, goal game.Goal) {
	a, c, msg := b.huntJoinCheck(ctx, i, id)
	if msg != "" {
		b.update(i, noteView(msg, colorMuted))
		return
	}
	if goal == game.GoalAccident {
		b.update(i, accidentPicker(a.ID, ""))
		return
	}
	b.joinHunt(ctx, i, a, c, goal, 0)
}

func (b *Bot) onHuntTarget(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	a, c, msg := b.huntJoinCheck(ctx, i, id)
	if msg != "" {
		b.update(i, noteView(msg, colorMuted))
		return
	}
	vals := i.MessageComponentData().Values
	if len(vals) == 0 {
		return
	}
	t, err := b.store.GetCharacter(ctx, a.GuildID, vals[0])
	if err != nil || t.ID == c.ID {
		b.update(i, accidentPicker(a.ID, "Pick another player who has a place at this court."))
		return
	}
	b.joinHunt(ctx, i, a, c, game.GoalAccident, t.ID)
}

func (b *Bot) joinHunt(ctx context.Context, i *discordgo.InteractionCreate, a store.Activity, c store.Character, goal game.Goal, targetID int64) {
	err := b.store.AddParticipant(ctx, a.ID, c.ID, string(goal), targetID, time.Now())
	var text string
	color := colorMuted
	switch {
	case errors.Is(err, store.ErrExists):
		text = "You're already riding on this hunt."
	case errors.Is(err, store.ErrEnded):
		text = "That hunt is over."
	case err != nil:
		slog.Error("join hunt", "err", err)
		text = "Something went wrong."
	default:
		color = colorGood
		text = fmt.Sprintf("🐎 You ride out to **%s**. You'll hear how it went %s.", strings.ToLower(game.HuntGoalName(goal)), relTime(a.EndsAt))
	}
	b.update(i, noteView(text, color))
}

// ---- the end of the hunt ----

func (b *Bot) resolveHunt(ctx context.Context, a store.Activity, now time.Time) {
	open, err := b.store.OpenEvents(ctx, a.ID)
	if err != nil {
		slog.Error("open events", "err", err)
		return
	}
	for _, e := range open {
		b.expireEvent(ctx, e, now)
	}
	host, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	ev, err2 := b.eventTotals(ctx, a.ID)
	parts, err3 := b.store.Participants(ctx, a.ID)
	sen, err4 := b.store.Senators(ctx, a.GuildID)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		slog.Error("hunt load", "err", err)
		return
	}

	in := game.HuntInput{Host: host.Person(), Goal: game.Goal(a.Goal), Quality: ev.quality, Senators: sen}
	users := map[int64]string{host.ID: host.UserID}
	targets := make([]int64, 0, len(parts))
	for _, p := range parts {
		if p.TargetID != 0 {
			targets = append(targets, p.TargetID)
		}
	}
	targeted, _ := b.store.CharactersByIDs(ctx, targets)
	for _, p := range parts {
		rider := game.HuntRider{Person: p.Character.Person(), Goal: game.Goal(p.Goal)}
		users[p.Character.ID] = p.Character.UserID
		if t, ok := targeted[p.TargetID]; ok && p.TargetID != 0 {
			tp := t.Person()
			rider.Target = &tp
			users[t.ID] = t.UserID
		}
		in.Riders = append(in.Riders, rider)
	}

	res := game.ResolveHunt(b.rng, in)
	hd := res.Deltas[host.ID]
	hd.Merge(ev.delta)
	res.Deltas[host.ID] = hd
	out := store.Outcome{Deltas: res.Deltas, Opinions: res.Opinions,
		Conditions: b.newConditions(append(res.Conditions, ev.conditions...), now)}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply hunt", "err", err)
		}
		return
	}

	title := fmt.Sprintf("🦌 %s's hunt is over", host.FullName())
	b.dmNotices(res.Notices, users, title)
	if a.AnnounceChannelID != "" {
		public := res.Public
		if n := len(parts); n > 0 {
			public += fmt.Sprintf(" %d rider%s rode along.", n, plural(n))
		}
		color := colorGood
		if !res.Success {
			color = colorBad
		}
		b.announceEnd(a, &discordgo.MessageEmbed{Title: title, Description: public, Color: color})
	}
}
