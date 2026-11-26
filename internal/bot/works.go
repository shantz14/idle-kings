package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	registerActivity(game.KindWork, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.worksPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveWork,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			name := "a public work"
			if w, ok := game.WorkByKey(a.Goal); ok {
				name = "your " + strings.ToLower(w.Name)
			}
			return fmt.Sprintf("Builders are at work on %s. It opens <t:%d:R>.", name, a.EndsAt.Unix())
		},
	})
	componentRoutes["work"] = (*Bot).onWork
}

// Public works custom IDs:
//   work:build:<work>   pay for a public work and break ground

func (b *Bot) onWork(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok || len(parts) < 2 || parts[0] != "build" {
		return
	}
	b.onBuildWork(ctx, i, c, parts[1])
}

// workTime is a work's build time in real time.
func (b *Bot) workTime(w game.WorkDef) time.Duration {
	return time.Duration(w.Days * float64(b.t.Day))
}

func (b *Bot) worksPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	building := ""
	if a, err := b.store.WorkInProgress(ctx, c.ID); err == nil {
		building = b.activityLine(ctx, a)
	} else if c.RealmFallen {
		building = "The realm has fallen. Nobody is building anything now."
	}
	times := map[string]time.Duration{}
	for _, w := range game.WorkDefs {
		times[w.Key] = b.workTime(w)
	}
	return worksView(c, times, building, note)
}

func worksView(c store.Character, times map[string]time.Duration, building, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	sb.WriteString("Pay for something the whole city will use, with your family's name carved on it. ")
	sb.WriteString("It takes days to build, but you can get on with other things meanwhile. ")
	sb.WriteString("Once it opens, its **Senate vote weight is yours for good**, and it adds to your estate.\n")
	sb.WriteString("You can build one at a time, and one of each.\n\n")
	var btns []discordgo.MessageComponent
	for _, w := range game.WorkDefs {
		built := slices.Contains(c.Monuments, w.Key)
		status := ""
		if built {
			status = " ✅ *built*"
		}
		fmt.Fprintf(&sb, "%s **%s** — %d gold, %s to build, **+%s vote weight**%s\n%s\n\n",
			w.Emoji, w.Name, w.Cost, humanDuration(times[w.Key]), game.Num(w.VoteWeight), status, w.Description)
		btns = append(btns, button(fmt.Sprintf("%s (%d gold)", w.Name, w.Cost), "work:build:"+w.Key,
			discordgo.SuccessButton, built || building != "" || w.Cost > c.Wealth))
	}
	fmt.Fprintf(&sb, "You have **%d gold**.", c.Wealth)
	if building != "" {
		sb.WriteString("\n\n" + building)
	}
	if note != "" {
		sb.WriteString("\n\n" + note)
	}
	btns = append(btns, button("Back", "menu:cat:"+string(game.CatMonetary), discordgo.SecondaryButton, false))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "Endow a Public Work", Description: sb.String(), Color: colorGold}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func (b *Bot) onBuildWork(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, key string) {
	w, ok := game.WorkByKey(key)
	if !ok {
		return
	}
	if c.RealmFallen {
		b.update(i, b.worksPanel(ctx, c, ""))
		return
	}
	now := time.Now()
	a, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindWork, Goal: w.Key, Public: true,
		StartedAt: now, EndsAt: now.Add(b.workTime(w)),
	}, w.Cost, nil)
	if err != nil {
		b.update(i, b.worksPanel(ctx, c, err.Error()))
		return
	}
	title := game.WorkTitle(c.FamilyName, w)
	where := b.announce(ctx, a, &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s House %s breaks ground on %s", w.Emoji, c.FamilyName, strings.ToLower(article(w.Name))+" "+strings.ToLower(w.Name)),
		Description: fmt.Sprintf("%s pays for %s, a gift to the city. It opens <t:%d:R>.", c.FullName(), title, a.EndsAt.Unix()),
		Color:       colorGold,
	}, nil)
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "Ground is broken",
			Description: fmt.Sprintf("You paid **%d gold** for %s. It opens <t:%d:R>, and I'll DM you when it does. You're free to do other things meanwhile.%s",
				w.Cost, title, a.EndsAt.Unix(), where),
			Color: colorGreen}},
		Components: []discordgo.MessageComponent{row(button("Back", "menu:home", discordgo.SecondaryButton, false))},
	})
}

func (b *Bot) resolveWork(ctx context.Context, a store.Activity, now time.Time) {
	c, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	sen, err2 := b.store.Senators(ctx, a.GuildID)
	if err := errors.Join(err1, err2); err != nil {
		slog.Error("work load", "err", err)
		return
	}
	w, ok := game.WorkByKey(a.Goal)
	if !ok { // removed from the catalogue: refund
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: {Wealth: a.Spend}}}, now)
		return
	}
	res := game.CompleteWork(w, c.Person(), c.FamilyName, sen)
	out := store.Outcome{
		Deltas:    map[int64]game.Delta{c.ID: res.Delta},
		Realm:     res.Realm,
		Opinions:  res.Opinions,
		Monuments: []store.NewMonument{{CharacterID: c.ID, Key: w.Key}},
	}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply work", "err", err)
		}
		return
	}
	_, _ = b.dm(c.UserID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
		Title: game.WorkTitle(c.FamilyName, w) + " is open", Description: res.Text, Color: colorGreen,
	}}})
	b.announceEnd(a, &discordgo.MessageEmbed{Title: game.WorkTitle(c.FamilyName, w), Description: res.Public, Color: colorGreen})
}

// monumentsField lists a character's finished public works, or nil.
func monumentsField(c store.Character) *discordgo.MessageEmbedField {
	var lines []string
	for _, k := range c.Monuments {
		if w, ok := game.WorkByKey(k); ok {
			lines = append(lines, fmt.Sprintf("%s **%s** (+%s vote weight)", w.Emoji, game.WorkTitle(c.FamilyName, w), game.Num(w.VoteWeight)))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return &discordgo.MessageEmbedField{Name: "Public works", Value: strings.Join(lines, "\n")}
}
