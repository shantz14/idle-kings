// Package bot is the Discord layer: slash commands, button menus, and the
// background loop that runs timed activities.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
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
	deferred  sync.Map          // interaction IDs acknowledged by deferUpdate
	// Retry counts for rows the tick keeps failing to resolve.
	activityRetries, eventRetries retryGuard
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
	b := &Bot{
		s: s, store: st, t: t, timing: t.Party, senate: t.Senate, courtFor: t.Court, investFor: t.Invest,
		rng: &lockedRand{r: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))},
	}
	s.AddHandler(b.onInteraction)
	return b, nil
}

// Run connects, registers commands, and runs the game loop until ctx ends.
func (b *Bot) Run(ctx context.Context) error {
	if err := b.s.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}
	defer b.s.Close()
	defer b.bg.Wait() // let background DMs finish before the session closes

	// Global so they also work in DMs with the bot.
	if _, err := b.s.ApplicationCommandBulkOverwrite(b.s.State.User.ID, "", commands()); err != nil {
		return fmt.Errorf("register commands: %w", err)
	}
	slog.Info("bot online", "user", b.s.State.User.Username)

	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		b.tick(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
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
	b.fadeHappenings(ctx, now)
	if err := b.store.PayIncome(ctx, now); err != nil {
		slog.Error("pay income", "err", err)
	}
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
