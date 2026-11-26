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

func (b *Bot) tick(ctx context.Context) {
	now := time.Now()
	if due, err := b.store.DueEvents(ctx, now); err != nil {
		slog.Error("due events", "err", err)
	} else {
		for _, e := range due {
			b.fireEvent(ctx, e, now)
		}
	}
	if expired, err := b.store.ExpiredEvents(ctx, now); err != nil {
		slog.Error("expired events", "err", err)
	} else {
		for _, e := range expired {
			b.expireEvent(ctx, e, now)
		}
	}
	if ended, err := b.store.EndedActivities(ctx, now); err != nil {
		slog.Error("ended activities", "err", err)
	} else {
		for _, a := range ended {
			switch a.Kind {
			case game.KindParty:
				b.resolveParty(ctx, a, now)
			case game.KindCourt:
				b.resolveCourt(ctx, a, now)
			case game.KindInvest:
				b.resolveInvest(ctx, a, now)
			case game.KindDuel:
				b.resolveDuel(ctx, a, now)
			case game.KindHunt:
				b.resolveHunt(ctx, a, now)
			case game.KindPlantSpies:
				b.resolvePlant(ctx, a, now)
			case game.KindRootOut:
				b.resolveRootOut(ctx, a, now)
			case game.KindEmbezzle:
				b.resolveEmbezzle(ctx, a, now)
			case game.KindCanvass:
				b.resolveCanvass(ctx, a, now)
			case game.KindSpeech:
				b.resolveSpeech(ctx, a, now)
			case game.KindCircus:
				b.resolveCircus(ctx, a, now)
			case game.KindWork:
				b.resolveWork(ctx, a, now)
			default:
				slog.Error("unknown activity kind", "kind", a.Kind, "id", a.ID)
			}
		}
	}
	b.senateTick(ctx, now)
	if err := b.store.PayIncome(ctx, now); err != nil {
		slog.Error("pay income", "err", err)
	}
}

// dm sends a private message; players with closed DMs just miss it.
func (b *Bot) dm(userID string, m *discordgo.MessageSend) (*discordgo.Message, error) {
	ch, err := b.s.UserChannelCreate(userID)
	if err != nil {
		slog.Warn("open dm", "user", userID, "err", err)
		return nil, err
	}
	msg, err := b.s.ChannelMessageSendComplex(ch.ID, m)
	if err != nil {
		slog.Warn("send dm", "user", userID, "err", err)
	}
	return msg, err
}
