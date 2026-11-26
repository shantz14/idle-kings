package bot

import (
	"context"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/store"
)

// activityDef is everything the bot needs to run one kind of action
// (game.Kind…). Each action's file registers its own in init, so a new
// action needs no edits to the menu, the tick or the court page.
type activityDef struct {
	// panel is the action's popup, opened from the menu ("menu:act:<kind>").
	panel func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData
	// resolve runs when a timed activity of this kind ends. Panel-only
	// actions (like the dossier) leave it nil.
	resolve func(b *Bot, ctx context.Context, a store.Activity, now time.Time)
	// busy says what the host is doing, for the home panel.
	busy func(b *Bot, ctx context.Context, a store.Activity) string
	// eventWhere titles DM events sent during it ("On your hunt").
	eventWhere string
}

var activityDefs = map[string]activityDef{}

func registerActivity(kind string, d activityDef) {
	if _, dup := activityDefs[kind]; dup {
		panic("activity registered twice: " + kind)
	}
	activityDefs[kind] = d
}

// resolveActivity runs an ended activity's resolver. A panic is logged and
// the activity is marked done, so one bad row can't jam the tick forever.
func (b *Bot) resolveActivity(ctx context.Context, a store.Activity, now time.Time) {
	d, ok := activityDefs[a.Kind]
	if !ok || d.resolve == nil {
		slog.Error("unknown activity kind", "kind", a.Kind, "id", a.ID)
		return
	}
	if !b.activityRetries.try(a.ID) {
		b.abandon(ctx, a, now) // it has failed every tick for too long
		return
	}
	defer func() {
		if r := recover(); r != nil {
			recoverPanicValue(r, "resolve activity", "kind", a.Kind, "id", a.ID)
			b.abandon(ctx, a, now)
		}
	}()
	d.resolve(b, ctx, a, now)
}

// abandon marks an activity resolved without applying anything, for rows
// that can never resolve (their host is gone, or the resolver panicked).
func (b *Bot) abandon(ctx context.Context, a store.Activity, now time.Time) {
	if err := b.store.AbandonActivity(ctx, a.ID, now); err != nil {
		slog.Error("abandon activity", "id", a.ID, "err", err)
		return
	}
	slog.Warn("abandoned activity", "kind", a.Kind, "id", a.ID)
}

// maxRetries is how many ticks in a row something may fail to resolve
// before it is given up on (about ten minutes at one tick per 15s).
const maxRetries = 40

// retryGuard counts how many ticks in a row each id was still waiting, so
// a row that fails every time (a resolver that can't load what it needs)
// is eventually dropped instead of retried forever. Only the tick uses it.
type retryGuard struct {
	counts map[int64]int
	seen   map[int64]bool
}

// try records an attempt and reports whether to go ahead.
func (g *retryGuard) try(id int64) bool {
	if g.counts == nil {
		g.counts, g.seen = map[int64]int{}, map[int64]bool{}
	}
	g.seen[id] = true
	g.counts[id]++
	if g.counts[id] > maxRetries {
		delete(g.counts, id)
		return false
	}
	return true
}

// endTick forgets ids that weren't waiting this tick: they resolved.
func (g *retryGuard) endTick() {
	for id := range g.counts {
		if !g.seen[id] {
			delete(g.counts, id)
		}
	}
	clear(g.seen)
}
