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
	registerActivity(game.KindSpeech, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.speechPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveSpeech,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("You are speaking in the Forum. You'll finish <t:%d:R>.", a.EndsAt.Unix())
		},
	})
	componentRoutes["speech"] = (*Bot).onSpeech
}

// Speech custom IDs:
//   speech:cause:<cause>         speaker picks a cause and takes the stand
//   speech:attend:<speech>       public "Attend the speech" button
//   speech:goal:<speech>:<goal>  attendee picks cheer or heckle

func (b *Bot) onSpeech(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) < 2 {
		return
	}
	switch parts[0] {
	case "cause":
		if c, ok := b.myCharacter(ctx, i); ok {
			b.onGiveSpeech(ctx, i, c, parts[1])
		}
	case "attend":
		b.onAttendSpeech(ctx, i, atoi(parts[1]))
	case "goal":
		if len(parts) > 2 {
			b.onSpeechGoal(ctx, i, atoi(parts[1]), game.Goal(parts[2]))
		}
	}
}

// ---- speaking ----

func (b *Bot) speechPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	return speechView(b.t.Speech, b.busyLine(ctx, c, "The realm has fallen. The Forum is rubble."), joinNotes(permitNote(c.Laws), note))
}

func speechView(dur time.Duration, busy, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	sb.WriteString("Take the stand in the Forum. Diplomacy carries a speech, and a famous name helps. ")
	sb.WriteString("A good one wins influence and a little renown; a bad one is a public humiliation. ")
	sb.WriteString("Senators who share your cause warm to you if it goes well, and those who oppose it cool.\n\n**Choose your cause**\n")
	for _, cause := range game.Causes {
		fmt.Fprintf(&sb, "• **%s**: %s\n", cause.Name, cause.Description)
	}
	var btns []discordgo.MessageComponent
	for _, cause := range game.Causes {
		btns = append(btns, button(cause.Name, "speech:cause:"+cause.Key, discordgo.PrimaryButton, busy != ""))
	}
	e := actionEmbed(game.KindSpeech, withNote(busyNote(busy, note), strings.TrimSpace(sb.String())))
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Cost", "Free"),
		fact("Lasts", humanDuration(dur)),
		fact("Crowd", "📣 Others may cheer or heckle"),
	}
	e.Footer = footer("Influence comes harder the more of it you already have.")
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{row(btns...),
			row(backButton(game.KindSpeech))},
	}
}

func (b *Bot) onGiveSpeech(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, causeKey string) {
	cause, ok := game.CauseByKey(causeKey)
	if !ok {
		return
	}
	if busy := b.busyLine(ctx, c, "fallen"); busy != "" {
		b.update(i, b.speechPanel(ctx, c, ""))
		return
	}
	if short := permitShort(c, 0); short != "" {
		b.update(i, b.speechPanel(ctx, c, short))
		return
	}
	now := time.Now()
	a, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindSpeech, Goal: cause.Key, Public: true,
		StartedAt: now, EndsAt: now.Add(b.t.Speech),
	}, 0, nil)
	if err != nil {
		b.update(i, b.speechPanel(ctx, c, err.Error()))
		return
	}
	permit := b.payPermit(ctx, c)
	where := b.announce(ctx, a, &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("📢 %s speaks in the Forum", c.FullName()),
		Description: fmt.Sprintf("A speech for **%s**. Come to cheer, or to heckle.", cause.Name),
		Color:       colorPanel,
		Fields:      []*discordgo.MessageEmbedField{fact("Ends", relTime(a.EndsAt))},
	}, []discordgo.MessageComponent{row(withEmoji(button("Attend the speech", fmt.Sprintf("speech:attend:%d", a.ID), discordgo.SuccessButton, false), "👂"))})
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "📢 You take the stand",
			Description: strings.TrimSpace(fmt.Sprintf("You speak for **%s**.%s%s", cause.Name, where, permit)),
			Color:       colorGood,
			Fields:      []*discordgo.MessageEmbedField{fact("You'll hear how it went", relTime(a.EndsAt))}}},
		Components: []discordgo.MessageComponent{homeRow()},
	})
}

// ---- attending ----

func (b *Bot) speechCheck(ctx context.Context, i *discordgo.InteractionCreate, id int64) (store.Activity, store.Character, string) {
	a, err := b.store.GetActivity(ctx, id)
	if err != nil || a.Kind != game.KindSpeech {
		return a, store.Character{}, "That speech doesn't exist."
	}
	c, err := b.store.GetCharacter(ctx, a.GuildID, userOf(i).ID)
	if err != nil {
		return a, c, "You have no place at this court yet."
	}
	switch {
	case a.Resolved || !time.Now().Before(a.EndsAt):
		return a, c, "That speech is over."
	case a.HostID == c.ID:
		return a, c, "It's your speech. You're already at the front."
	}
	return a, c, ""
}

func (b *Bot) onAttendSpeech(ctx context.Context, i *discordgo.InteractionCreate, id int64) {
	a, _, msg := b.speechCheck(ctx, i, id)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	speaker, _ := b.store.GetCharacterByID(ctx, a.HostID)
	b.respond(i, ephemeral(speechAttendView(a, speaker)))
}

func speechAttendView(a store.Activity, speaker store.Character) *discordgo.InteractionResponseData {
	cause, _ := game.CauseByKey(a.Goal)
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s is speaking for **%s**.\n\n", speaker.FullName(), cause.Name)
	var btns []discordgo.MessageComponent
	for _, g := range game.SpeechGoals {
		style := discordgo.PrimaryButton
		if g.Key == game.GoalHeckle {
			style = discordgo.DangerButton
		}
		btns = append(btns, button(g.Name, fmt.Sprintf("speech:goal:%d:%s", a.ID, g.Key), style, false))
	}
	sb.WriteString(goalList(game.SpeechGoals))
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "👂 Attend " + speaker.FullName() + "'s speech", Description: sb.String(), Color: colorPanel,
			Footer: footer("Heckling is done in public: the speaker will know it was you.")}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func (b *Bot) onSpeechGoal(ctx context.Context, i *discordgo.InteractionCreate, id int64, goal game.Goal) {
	a, c, msg := b.speechCheck(ctx, i, id)
	if msg != "" {
		b.update(i, noteView(msg, colorMuted))
		return
	}
	if goal != game.GoalCheer && goal != game.GoalHeckle {
		return
	}
	err := b.store.AddParticipant(ctx, a.ID, c.ID, string(goal), 0, time.Now())
	var text string
	color := colorMuted
	switch {
	case errors.Is(err, store.ErrExists):
		text = "You're already in the crowd."
	case errors.Is(err, store.ErrEnded):
		text = "That speech is over."
	case err != nil:
		slog.Error("attend speech", "err", err)
		text = "Something went wrong."
	default:
		color = colorGood
		text = fmt.Sprintf("👂 You're in the crowd to **%s**. You'll hear how it went %s.",
			strings.ToLower(game.SpeechGoalName(goal)), relTime(a.EndsAt))
	}
	b.update(i, noteView(text, color))
}

// ---- the end of the speech ----

func (b *Bot) resolveSpeech(ctx context.Context, a store.Activity, now time.Time) {
	sp, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	parts, err2 := b.store.Participants(ctx, a.ID)
	sen, err3 := b.store.Senators(ctx, a.GuildID)
	if err := errors.Join(err1, err2, err3); err != nil {
		slog.Error("speech load", "err", err)
		return
	}
	cause, _ := game.CauseByKey(a.Goal)
	in := game.SpeechInput{Speaker: sp.Person(), Cause: cause, Senators: sen}
	users := map[int64]string{sp.ID: sp.UserID}
	for _, p := range parts {
		in.Audience = append(in.Audience, game.Listener{Person: p.Character.Person(), Goal: game.Goal(p.Goal)})
		users[p.Character.ID] = p.Character.UserID
	}
	res := game.ResolveSpeech(b.rng, in)
	if err := b.store.ApplyResult(ctx, a.ID, store.Outcome{Deltas: res.Deltas, Opinions: res.Opinions}, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply speech", "err", err)
		}
		return
	}
	title := fmt.Sprintf("📢 %s's speech is over", sp.FullName())
	b.dmNotices(res.Notices, users, title)
	public := res.Public
	if n := len(parts); n > 0 {
		public += fmt.Sprintf(" %d came to listen.", n)
	}
	color := colorGood
	if !res.Success {
		color = colorBad
	}
	b.announceEnd(a, &discordgo.MessageEmbed{Title: title, Description: public, Color: color})
}
