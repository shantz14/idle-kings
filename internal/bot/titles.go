package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Titles and seasons (game/titles.go). Deeds are recorded where they happen
// (addDeeds), feats are given with awardFeat, and a season ends after its
// last Senate day (checkSeason). The leaderboard is /players, with a tab per
// score.
//
// Leaderboard custom IDs:
//   info:board:<tab>   switch the /players tab (influence, prestige, coin, duels)

const (
	boardInfluence = "influence"
	boardPrestige  = "prestige"
	boardCoin      = "coin"
	boardDuels     = "duels"
)

var boardTabs = []struct{ key, label, emoji string }{
	{boardInfluence, "Influence", "🏛️"},
	{boardPrestige, "Prestige", "👑"},
	{boardCoin, "Master of Coin", "💰"},
	{boardDuels, "Duels", "⚔️"},
}

// addDeeds records deeds toward this season's titles. Failures are logged:
// a lost deed shouldn't spoil the action that earned it.
func (b *Bot) addDeeds(ctx context.Context, guildID string, deeds ...store.Deed) {
	if err := b.store.AddDeeds(ctx, guildID, deeds, time.Now()); err != nil {
		slog.Error("add deeds", "err", err)
	}
}

// awardFeat gives a character a feat, once, and tells them and the court.
func (b *Bot) awardFeat(ctx context.Context, c store.Character, key string) {
	def, ok := game.TitleByKey(key)
	if !ok {
		return
	}
	fresh, err := b.store.AwardFeat(ctx, c.GuildID, c.ID, key, time.Now())
	if err != nil {
		slog.Error("award feat", "err", err)
		return
	}
	if !fresh {
		return
	}
	label := game.Title{Key: key}.Label()
	b.dmNote(c.UserID, "🏆 A new title", fmt.Sprintf("You are now known as **%s**. %s\n\nIt adds to your house's prestige, for good.", label, def.Description), colorGood)
	b.post(ctx, c.GuildID, &discordgo.MessageEmbed{
		Title:       "🏆 A new title at court",
		Description: fmt.Sprintf("**%s** is now known as **%s**. %s", c.FullName(), label, def.Description),
		Color:       colorGood,
	})
}

// ---- scores ----

// prestigeOf is a character's Great House score.
func prestigeOf(c store.Character, titles []game.Title) float64 {
	return game.Prestige(c.Renown, c.Influence, game.MonumentEffects(c.Monuments).VoteWeight, titles)
}

// wealthOf is a character's Master of Coin score: gold, public stakes and
// the list price of their estate. Secret stakes never count, so the board
// can't give them away.
func wealthOf(c store.Character) int {
	stakes := 0.0
	for k, v := range c.Ventures {
		if d, ok := game.SectorByKey(k); ok && !d.Secret {
			stakes += v.Stake
		}
	}
	estate := 0
	for _, k := range c.Items {
		if it, ok := game.ItemByKey(k); ok {
			estate += it.Price
		}
	}
	return game.Wealth(c.Wealth, stakes, estate)
}

// ---- the season's end ----

// checkSeason ends the court's season if its last Senate day has passed.
func (b *Bot) checkSeason(ctx context.Context, guildID string) {
	se, err := b.store.Season(ctx, guildID)
	if err != nil {
		slog.Error("season", "err", err)
		return
	}
	if se.SenateDays >= game.SeasonDays {
		b.endSeason(ctx, guildID, se.Number)
	}
}

// seasonAward is one season title's winners.
type seasonAward struct {
	Title   game.TitleDef
	Winners []store.Character
}

// endSeason crowns the season's leaders and starts the next season.
func (b *Bot) endSeason(ctx context.Context, guildID string, season int) {
	awards, err := b.seasonAwards(ctx, guildID, season)
	if err != nil {
		slog.Error("season awards", "err", err)
		return
	}
	var given []store.TitleAward
	for _, a := range awards {
		for _, w := range a.Winners {
			given = append(given, store.TitleAward{CharacterID: w.ID, Key: a.Title.Key})
		}
	}
	if err := b.store.EndSeason(ctx, guildID, season, given, time.Now()); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("end season", "err", err)
		}
		return
	}
	b.post(ctx, guildID, seasonEndEmbed(season, awards))
	for _, a := range awards {
		for _, w := range a.Winners {
			label := game.Title{Key: a.Title.Key, Season: season}.Label()
			b.dmNote(w.UserID, "🏆 Season "+fmt.Sprint(season)+" is over",
				fmt.Sprintf("The court has crowned you **%s**. %s\n\nThe title is yours for good, and adds to your prestige.", label, a.Title.Description), colorGood)
		}
	}
}

// seasonAwards works out who wins each season title.
func (b *Bot) seasonAwards(ctx context.Context, guildID string, season int) ([]seasonAward, error) {
	chars, err := b.store.CharactersInCourt(ctx, guildID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]store.Character, len(chars))
	ids := make([]int64, 0, len(chars))
	for _, c := range chars {
		byID[c.ID] = c
		ids = append(ids, c.ID)
	}
	titles, err := b.store.TitlesOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []seasonAward
	for _, t := range game.SeasonTitles() {
		var scores []game.Score
		switch {
		case t.Key == game.TitleGreatHouse:
			for _, c := range chars {
				scores = append(scores, game.Score{CharacterID: c.ID, Value: prestigeOf(c, titles[c.ID])})
			}
		case t.Key == game.TitleMasterOfCoin:
			for _, c := range chars {
				scores = append(scores, game.Score{CharacterID: c.ID, Value: float64(wealthOf(c))})
			}
		case t.Deed != "":
			tot, err := b.store.SeasonTotals(ctx, guildID, season, t.Deed)
			if err != nil {
				return nil, err
			}
			for id, v := range tot {
				if _, ok := byID[id]; ok {
					scores = append(scores, game.Score{CharacterID: id, Value: v})
				}
			}
		}
		a := seasonAward{Title: t}
		for _, id := range game.Winners(scores) {
			a.Winners = append(a.Winners, byID[id])
		}
		out = append(out, a)
	}
	return out, nil
}

func seasonEndEmbed(season int, awards []seasonAward) *discordgo.MessageEmbed {
	var lines []string
	for _, a := range awards {
		if len(a.Winners) == 0 {
			continue
		}
		var names []string
		for _, w := range a.Winners {
			names = append(names, fmt.Sprintf("%s **%s**", w.Crest.Emoji(), w.FullName()))
		}
		lines = append(lines, fmt.Sprintf("%s **%s**: %s\n> %s", a.Title.Emoji, a.Title.Name, strings.Join(names, " and "), a.Title.Description))
	}
	desc := "Nobody at court earned a title this season."
	if len(lines) > 0 {
		desc = strings.Join(lines, "\n\n")
	}
	return &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🏆 Season %d is over", season),
		Description: truncate(desc, 4000),
		Color:       colorGood,
		Footer:      footer(fmt.Sprintf("The titles are kept for good. Season %d begins: %d Senate days.", season+1, game.SeasonDays)),
	}
}

// ---- the leaderboard ----

// boardEntry is one line on the leaderboard.
type boardEntry struct {
	Character store.Character
	Line      string // what follows the name
}

// playersPage is /players: the court ranked by one of the boards.
func (b *Bot) playersPage(ctx context.Context, guildID, tab string) *discordgo.InteractionResponseData {
	chars, err := b.store.CharactersInCourt(ctx, guildID)
	if err != nil || len(chars) == 0 {
		return noteView("Nobody has joined the court yet.", colorMuted)
	}
	se, _ := b.store.Season(ctx, guildID)
	entries, foot := b.boardEntries(ctx, guildID, tab, chars, se)
	return playersView(tab, entries, se, foot)
}

func (b *Bot) boardEntries(ctx context.Context, guildID, tab string, chars []store.Character, se store.Season) ([]boardEntry, string) {
	type scored struct {
		c store.Character
		v float64
	}
	var list []scored
	var foot string
	var line func(s scored) string
	switch tab {
	case boardPrestige:
		ids := make([]int64, len(chars))
		for n, c := range chars {
			ids[n] = c.ID
		}
		titles, _ := b.store.TitlesOf(ctx, ids)
		for _, c := range chars {
			list = append(list, scored{c, prestigeOf(c, titles[c.ID])})
		}
		line = func(s scored) string {
			return fmt.Sprintf("Prestige **%s** · Renown %s · Influence %s", game.Num(s.v), game.Num(s.c.Renown), game.Num(s.c.Influence))
		}
		foot = "Prestige is renown, influence, public works and titles. The most prestigious house is crowned Great House when the season ends."
	case boardCoin:
		for _, c := range chars {
			list = append(list, scored{c, float64(wealthOf(c))})
		}
		line = func(s scored) string {
			return fmt.Sprintf("Worth **%s gold** in gold, ventures and estate", game.Num(s.v))
		}
		foot = "The richest house is crowned Master of Coin when the season ends. Secret ventures aren't counted."
	case boardDuels:
		tot, _ := b.store.SeasonTotals(ctx, guildID, se.Number, game.DeedDuelWon)
		for _, c := range chars {
			list = append(list, scored{c, tot[c.ID]})
		}
		line = func(s scored) string {
			return fmt.Sprintf("**%s** duel%s won this season", game.Num(s.v), plural(int(s.v)))
		}
		foot = "Whoever wins the most duels this season is crowned Champion of the Arena."
	default:
		sen, _ := b.store.Senators(ctx, guildID)
		total := senateTotalOf(sen, chars)
		for _, c := range chars {
			list = append(list, scored{c, c.VoteWeight()})
		}
		line = func(s scored) string {
			return fmt.Sprintf("Influence %s (%s%% of the Senate) · Renown %s · Estate %s",
				game.Num(s.c.Influence), game.Num(percentOf(s.v, total)), game.Num(s.c.Renown), game.Num(s.c.EstateValue()))
		}
		foot = "Ranked by weight in the Senate."
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].c.ID < list[j].c.ID
	})
	out := make([]boardEntry, len(list))
	for n, s := range list {
		out[n] = boardEntry{Character: s.c, Line: line(s)}
	}
	return out, foot
}

// playersView renders a leaderboard tab.
func playersView(tab string, entries []boardEntry, se store.Season, foot string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🏆 **Season %d** · %d Senate day%s left\n\n", se.Number, se.DaysLeft(), plural(se.DaysLeft()))
	var opts []discordgo.SelectMenuOption
	for n, e := range entries {
		c := e.Character
		role, _ := game.RoleByKey(c.Role)
		bg, _ := game.BackgroundByKey(c.Background)
		fmt.Fprintf(&sb, "**%d.** %s **%s** · %s\n*%s*\n\n", n+1, c.Crest.Emoji(), c.FullName(), role.Name, e.Line)
		if len(opts) < 25 {
			opts = append(opts, discordgo.SelectMenuOption{Label: truncate(c.FullName(), 100), Value: fmt.Sprint(c.ID),
				Description: truncate(role.Name+" · "+bg.Name, 100)})
		}
	}
	var tabs []discordgo.MessageComponent
	name := boardTabs[0].label
	for _, t := range boardTabs {
		style := discordgo.SecondaryButton
		if t.key == tab || (tab == "" && t.key == boardInfluence) {
			style, name = discordgo.PrimaryButton, t.label
		}
		btn := button(t.label, "info:board:"+t.key, style, false)
		btn.Emoji = &discordgo.ComponentEmoji{Name: t.emoji}
		tabs = append(tabs, btn)
	}
	comps := []discordgo.MessageComponent{row(tabs...)}
	if len(opts) > 0 {
		comps = append(comps, row(discordgo.SelectMenu{
			MenuType: discordgo.StringSelectMenu, CustomID: "info:profile", Placeholder: "Open a profile…", Options: opts}))
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: titled(emojiCourt, "The Court · "+name), Description: truncate(sb.String(), 4000), Color: colorPanel,
			Footer: footer(foot)}},
		Components: comps,
	}
}

// titlesField lists a character's titles, or nil.
func titlesField(ts []game.Title) *discordgo.MessageEmbedField {
	if len(ts) == 0 {
		return nil
	}
	lines := make([]string, len(ts))
	for n, t := range ts {
		lines[n] = t.Label()
	}
	return &discordgo.MessageEmbedField{Name: "🏆 Titles", Value: joinLines(lines, 1000)}
}
