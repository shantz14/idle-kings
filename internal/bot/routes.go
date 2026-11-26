package bot

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// A route handles every custom ID with one prefix ("party:attend:7" goes to
// the "party" route with parts ["attend", "7"]). Each file registers its
// prefixes in init, next to the handler.
type route func(b *Bot, ctx context.Context, i *discordgo.InteractionCreate, parts []string)

var (
	componentRoutes = map[string]route{} // buttons and selects
	modalRoutes     = map[string]route{} // modal submits
)

// dispatch sends a custom ID to its route. parts always has at least one
// element, so handlers can switch on parts[0] without a length check.
func (b *Bot) dispatch(ctx context.Context, i *discordgo.InteractionCreate, routes map[string]route, customID string) {
	prefix, rest, _ := strings.Cut(customID, ":")
	r, ok := routes[prefix]
	if !ok {
		slog.Warn("unknown custom id", "id", customID)
		return
	}
	r(b, ctx, i, strings.Split(rest, ":"))
}

// backgroundTimeout bounds work handed to background.
const backgroundTimeout = 30 * time.Second

// background runs fn on its own goroutine, after the caller returns, with a
// context that outlives the caller's (an interaction's context ends when its
// handler does). Run waits for these before it returns.
func (b *Bot) background(ctx context.Context, what string, fn func(context.Context)) {
	b.bg.Add(1)
	go func() {
		defer b.bg.Done()
		defer recoverPanic(what)
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backgroundTimeout)
		defer cancel()
		fn(ctx)
	}()
}

// recoverPanic logs a panic instead of letting it take the bot down. Use it
// deferred, around one interaction or one tick job.
func recoverPanic(what string, args ...any) {
	if r := recover(); r != nil {
		recoverPanicValue(r, what, args...)
	}
}

// recoverPanicValue logs a value already taken from recover().
func recoverPanicValue(r any, what string, args ...any) {
	slog.Error("panic in "+what, append(args, "panic", r, "stack", string(debug.Stack()))...)
}
