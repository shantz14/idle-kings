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
	desc := "Pay for something the whole city will use, with your family's name carved on it. " +
		"Once it opens, it gives you **lasting influence**, and it adds to your estate."
	e := actionEmbed(game.KindWork, withNote(busyNote(building, note), desc))
	var btns []discordgo.MessageComponent
	for _, w := range game.WorkDefs {
		built := slices.Contains(c.Monuments, w.Key)
		name := w.Emoji + " " + w.Name
		if built {
			name += " ✅"
		}
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: name, Value: fmt.Sprintf(
			"%s\n**%d** gold · %s to build · **+%s** vote weight", w.Description, w.Cost, humanDuration(times[w.Key]), game.Num(w.VoteWeight))})
		btns = append(btns, withEmoji(button(fmt.Sprintf("%s (%d gold)", w.Name, w.Cost), "work:build:"+w.Key,
			discordgo.SuccessButton, built || building != "" || w.Cost > c.Wealth), w.Emoji))
	}
	e.Fields = append(e.Fields, fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)))
	e.Footer = footer("One at a time, and one of each. Building doesn't stop you doing other things.")
	btns = append(btns, backButton(game.KindWork))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{e},
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
		Description: fmt.Sprintf("%s pays for %s, a gift to the city.", c.FullName(), title),
		Color:       colorPanel,
		Fields:      []*discordgo.MessageEmbedField{fact("Opens", relTime(a.EndsAt))},
	}, nil)
	b.update(i, startedView("🏗️ Ground is broken",
		fmt.Sprintf("Work begins on %s. I'll DM you when it opens; you're free to do other things meanwhile.%s", title, where),
		fact("Spent", fmt.Sprintf("%d gold", w.Cost)), fact("Opens", relTime(a.EndsAt))))
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
		Title: w.Emoji + " " + game.WorkTitle(c.FamilyName, w) + " is open", Description: res.Text, Color: colorGood,
	}}})
	b.announceEnd(a, &discordgo.MessageEmbed{Title: w.Emoji + " " + game.WorkTitle(c.FamilyName, w), Description: res.Public, Color: colorGood})
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
	return &discordgo.MessageEmbedField{Name: "🏛️ Public works", Value: strings.Join(lines, "\n")}
}
