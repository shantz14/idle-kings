// Package bot is the Discord layer: slash commands, button menus, and the
// background loop that runs timed activities.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

type Bot struct {
	s         *discordgo.Session
	store     *store.Store
	rng       *lockedRand
	t         game.Timings
	timing    game.PartyTiming  // = t.Party
	senate    game.SenateTiming // = t.Senate
	courtFor  time.Duration     // = t.Court
	investFor time.Duration     // = t.Invest
	deals     dealDrafts        // estate offers being put together
	dmChans   sync.Map          // user ID → DM channel ID, so each DM is one call
	bg        sync.WaitGroup    // background work (see background)
	handlers  sync.WaitGroup    // interactions being handled
	acks      sync.Map          // interaction ID → *ack (see send)
	closing   atomic.Bool       // set on shutdown: new interactions are turned away
	lastTick  atomic.Int64      // unix time the last tick finished, for Healthy
	// Retry counts for rows the tick keeps failing to resolve.
	activityRetries, eventRetries, matterRetries retryGuard
}

// lockedRand makes one RNG safe for concurrent handlers.
type lockedRand struct {
	mu sync.Mutex
	r  *rand.Rand
}

func (l *lockedRand) IntN(n int) int { l.mu.Lock(); defer l.mu.Unlock(); return l.r.IntN(n) }
func (l *lockedRand) Perm(n int) []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Perm(n)
}

func New(token string, st *store.Store, t game.Timings) (*Bot, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	s.Identify.Intents = discordgo.IntentsGuilds
	s.Client.Timeout = restTimeout // so one stuck REST call can't hold up a tick
	b := &Bot{
		s: s, store: st, t: t, timing: t.Party, senate: t.Senate, courtFor: t.Court, investFor: t.Invest,
		rng: &lockedRand{r: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))},
	}
	s.AddHandler(b.onInteraction)
	return b, nil
}

// restTimeout bounds each Discord REST call.
const restTimeout = 20 * time.Second

// tickEvery is how often the game loop runs.
const tickEvery = 15 * time.Second

// Run connects, registers commands, and runs the game loop until ctx ends.
// On the way out it stops taking interactions, closes the gateway, and
// waits for handlers and background work, so the store can close after it.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.s.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}
	defer func() {
		b.closing.Store(true)
		_ = b.s.Close() // the gateway; REST calls still work for what's in flight
		b.handlers.Wait()
		b.bg.Wait()
	}()
	b.lastTick.Store(time.Now().Unix())
	go b.registerCommands(ctx)
	slog.Info("bot online", "user", b.s.State.User.Username)

	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		// A tick runs to the end even if a shutdown starts during it: it
		// stops between ticks, never halfway through resolving something.
		b.tick(context.WithoutCancel(ctx))
		b.lastTick.Store(time.Now().Unix())
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return nil
		case <-t.C:
		}
	}
}

// Healthy reports whether the game loop finished a tick within maxAge.
func (b *Bot) Healthy(maxAge time.Duration) bool {
	return time.Since(time.Unix(b.lastTick.Load(), 0)) <= maxAge
}

// registerCommands publishes the slash commands, retrying with backoff: a
// Discord hiccup at boot shouldn't stop the game from running.
func (b *Bot) registerCommands(ctx context.Context) {
	wait := 5 * time.Second
	for {
		// Global so they also work in DMs with the bot.
		_, err := b.s.ApplicationCommandBulkOverwrite(b.s.State.User.ID, "", commands())
		if err == nil {
			slog.Info("commands registered")
			return
		}
		slog.Error("register commands; retrying", "err", err, "in", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 5*time.Minute)
	}
}

// tickTimeout bounds one tick, so a stuck Discord call can't stall the loop.
const tickTimeout = 2 * time.Minute

func (b *Bot) tick(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, tickTimeout)
	defer cancel()
	defer recoverPanic("tick")
	defer b.activityRetries.endTick()
	defer b.eventRetries.endTick()
	defer b.matterRetries.endTick()
	now := time.Now()
	if due, err := b.store.DueEvents(ctx, now); err != nil {
		slog.Error("due events", "err", err)
	} else {
		for _, e := range due {
			func() {
				defer recoverPanic("fire event", "id", e.ID)
				b.fireEvent(ctx, e, now)
			}()
		}
	}
	if expired, err := b.store.ExpiredEvents(ctx, now); err != nil {
		slog.Error("expired events", "err", err)
	} else {
		for _, e := range expired {
			if !b.eventRetries.try(e.ID) {
				slog.Warn("abandoned event", "id", e.ID)
				_ = b.store.ResolveEvent(ctx, e.ID, "", game.EventOutcome{}, now)
				continue
			}
			func() {
				defer recoverPanic("expire event", "id", e.ID)
				b.expireEvent(ctx, e, now)
			}()
		}
	}
	if ended, err := b.store.EndedActivities(ctx, now); err != nil {
		slog.Error("ended activities", "err", err)
	} else {
		for _, a := range ended {
			b.resolveActivity(ctx, a, now)
		}
	}
	func() {
		defer recoverPanic("senate tick")
		b.senateTick(ctx, now)
	}()
	func() {
		defer recoverPanic("conspiracy tick")
		b.conspiracyTick(ctx, now)
	}()
	// Each job on its own, so one that panics can't stop the rest (income).
	func() {
		defer recoverPanic("fade happenings")
		b.fadeHappenings(ctx, now)
	}()
	func() {
		defer recoverPanic("settle auctions")
		b.settleAuctions(ctx, now)
	}()
	func() {
		defer recoverPanic("pay income")
		if err := b.store.PayIncome(ctx, now); err != nil {
			slog.Error("pay income", "err", err)
		}
	}()
}

// dm sends a private message; players with closed DMs just miss it.
func (b *Bot) dm(userID string, m *discordgo.MessageSend) (*discordgo.Message, error) {
	chID, err := b.dmChannel(userID)
	if err != nil {
		slog.Warn("open dm", "user", userID, "err", err)
		return nil, err
	}
	msg, err := b.s.ChannelMessageSendComplex(chID, m)
	if err != nil {
		b.dmChans.Delete(userID) // the channel may be gone; reopen next time
		slog.Warn("send dm", "user", userID, "err", err)
	}
	return msg, err
}

// dmChannel opens (once) the DM channel with a user.
func (b *Bot) dmChannel(userID string) (string, error) {
	if id, ok := b.dmChans.Load(userID); ok {
		return id.(string), nil
	}
	ch, err := b.s.UserChannelCreate(userID)
	if err != nil {
		return "", err
	}
	b.dmChans.Store(userID, ch.ID)
	return ch.ID, nil
}
