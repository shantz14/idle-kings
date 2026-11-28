package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Happenings: monsters, wonders and visitors abroad in the realm for a few
// days (game.HappeningDefs). One may begin on each Senate day; hunts,
// journeys and parties may run into them; some event outcomes end them.

// rollHappening may start a new happening in a court on a Senate day.
func (b *Bot) rollHappening(ctx context.Context, guildID string, now time.Time) {
	if float64(b.rng.IntN(1000))/1000 >= game.HappeningChance {
		return
	}
	b.startHappening(ctx, guildID, "", now)
}

// startHappening starts key (or a random pick if "") in a court, announces
// it and applies its shocks. It returns the happening's name, or "" if none
// could start.
func (b *Bot) startHappening(ctx context.Context, guildID, key string, now time.Time) string {
	active, err := b.store.ActiveHappenings(ctx, guildID)
	if err != nil {
		slog.Error("active happenings", "err", err)
		return ""
	}
	keys := happeningKeys(active)
	var h game.HappeningDef
	var ok bool
	if key != "" {
		h, ok = game.HappeningByKey(key)
		if !ok || slicesContains(keys, key) {
			return ""
		}
	} else {
		if len(active) >= game.MaxHappenings {
			return ""
		}
		last, _ := b.store.LastHappening(ctx, guildID)
		if h, ok = game.PickHappening(b.rng, keys, last); !ok {
			return ""
		}
	}
	if h.Key == game.KindAuctionHappening && !b.lotAvailable(ctx, guildID) {
		return "" // the merchant has nothing left to sell here
	}
	days := game.HappeningDays(b.rng, h)
	ends := now.Add(time.Duration(days) * b.t.Day)
	before, hadRealm := b.realmBefore(ctx, guildID)
	if _, err := b.store.StartHappening(ctx, guildID, h.Key, now, ends, h.Sectors); err != nil {
		slog.Error("start happening", "err", err)
		return ""
	}
	if len(h.Realm) > 0 {
		if err := b.store.ApplyOutcome(ctx, guildID, store.Outcome{Realm: h.Realm}, now); err != nil {
			slog.Error("happening realm", "err", err)
		}
	}
	e := &discordgo.MessageEmbed{
		Title:       h.Emoji + " " + h.Name,
		Description: h.Announce,
		Color:       colorNotice,
		Fields:      []*discordgo.MessageEmbedField{fact("⏳ Fades", relTime(ends))},
		Footer:      footer(happeningWhere(h)),
	}
	if fx := happeningEffects(h); fx != "" {
		e.Fields = append(e.Fields, fact("📋 While it lasts", fx))
	}
	b.post(ctx, guildID, e)
	if h.Key == game.KindAuctionHappening {
		b.openNPCAuction(ctx, guildID, now, ends)
	}
	if hadRealm && len(h.Realm) > 0 {
		b.afterRealmChange(ctx, guildID, before)
	}
	return h.Name
}

// fadeHappenings ends every happening whose time is up.
func (b *Bot) fadeHappenings(ctx context.Context, now time.Time) {
	due, err := b.store.DueHappenings(ctx, now)
	if err != nil {
		slog.Error("due happenings", "err", err)
		return
	}
	for _, hp := range due {
		if err := b.store.EndHappening(ctx, hp.ID, 0, now); err != nil {
			continue // someone ended it first
		}
		if h, ok := game.HappeningByKey(hp.Key); ok {
			b.post(ctx, hp.GuildID, &discordgo.MessageEmbed{Title: h.Emoji + " " + h.Name, Description: h.Fade, Color: colorMuted})
		}
	}
}

// happeningEnded is called when an event's outcome ends its happening: the
// one it happened to (characterID) gets the glory, and the court is told.
func (b *Bot) happeningEnded(ctx context.Context, eventKey string, characterID int64, now time.Time) {
	h, ok := game.HappeningOfEvent(eventKey)
	if !ok {
		return
	}
	c, err := b.store.GetCharacterByID(ctx, characterID)
	if err != nil {
		return
	}
	active, err := b.store.ActiveHappenings(ctx, c.GuildID)
	if err != nil {
		return
	}
	for _, hp := range active {
		if hp.Key != h.Key {
			continue
		}
		if err := b.store.EndHappening(ctx, hp.ID, c.ID, now); err != nil {
			return
		}
		desc := fmt.Sprintf(h.Ended, c.FullName())
		if h.Trophy != "" {
			if it, ok := game.ItemByKey(h.Trophy); ok {
				if _, err := b.store.BuyItem(ctx, c.ID, c.GuildID, it.Key, 0, now); err == nil {
					desc += fmt.Sprintf("\n\n🏆 They take home **%s**.", it.Name)
				}
			}
		}
		b.post(ctx, c.GuildID, &discordgo.MessageEmbed{Title: h.Emoji + " " + h.Name, Description: desc, Color: colorGood})
		if h.Feat != "" {
			b.awardFeat(ctx, c, game.HappeningFeat(h.Key))
		}
		return
	}
}

// withHappening may swap one of an activity's events for an active
// happening's, if one is met at where.
func (b *Bot) withHappening(ctx context.Context, guildID, where string, events []store.NewEvent) []store.NewEvent {
	if len(events) == 0 {
		return events
	}
	active, err := b.store.ActiveHappenings(ctx, guildID)
	if err != nil || len(active) == 0 {
		return events
	}
	pool := game.HappeningEvents(where, happeningKeys(active))
	if len(pool) == 0 || float64(b.rng.IntN(1000))/1000 >= game.HappeningEncounter {
		return events
	}
	events[0].Key = pool[b.rng.IntN(len(pool))].Key
	return events
}

// happeningNote is the line an action's panel shows about happenings met
// there, or "".
func (b *Bot) happeningNote(ctx context.Context, guildID string, where ...string) string {
	active, err := b.store.ActiveHappenings(ctx, guildID)
	if err != nil {
		return ""
	}
	var lines []string
	for _, hp := range active {
		h, ok := game.HappeningByKey(hp.Key)
		if !ok {
			continue
		}
		for _, w := range where {
			if _, met := h.Events[w]; met {
				lines = append(lines, h.Hint)
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

// happeningsField lists what's abroad in the realm, for /realm.
func happeningsField(active []store.Happening) *discordgo.MessageEmbedField {
	var lines []string
	for _, hp := range active {
		if h, ok := game.HappeningByKey(hp.Key); ok {
			lines = append(lines, fmt.Sprintf("%s **%s** · fades %s\n*%s*", h.Emoji, h.Name, relTime(hp.EndsAt), happeningWhere(h)))
		}
	}
	if len(lines) == 0 {
		lines = []string{"All is quiet in the realm."}
	}
	return &discordgo.MessageEmbedField{Name: "🌍 Abroad in the realm", Value: joinLines(lines, 1000)}
}

// happeningWhere says where a happening can be met.
func happeningWhere(h game.HappeningDef) string {
	names := map[string]string{game.MeetHunt: "hunts", game.MeetJourney: "journeys at home",
		game.MeetJourneyAbroad: "journeys abroad", game.MeetParty: "parties"}
	var parts []string
	for _, m := range h.Meets() {
		parts = append(parts, names[m])
	}
	if len(parts) == 0 {
		return ""
	}
	return "Met on " + strings.Join(parts, " and ") + "."
}

// happeningEffects describes a happening's shocks, or "".
func happeningEffects(h game.HappeningDef) string {
	var parts []string
	if s := game.DescribeChanges(h.Sectors); s != "" {
		parts = append(parts, s)
	}
	if s := h.Realm.Describe(); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}

func happeningKeys(active []store.Happening) []string {
	keys := make([]string, 0, len(active))
	for _, hp := range active {
		keys = append(keys, hp.Key)
	}
	return keys
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
