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

// ---- /admin: testing tools for the court's admin ----
//
// Every subcommand acts on `player` (default: the admin themselves) and
// answers privately. Nothing here is announced or leaked to spies.

func adminCommand(ctxs *[]discordgo.InteractionContextType, install *[]discordgo.ApplicationIntegrationType) *discordgo.ApplicationCommand {
	player := &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionUser, Name: "player",
		Description: "Who (defaults to you)"}
	num := func(name, desc string) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionNumber, Name: name, Description: desc, Required: true}
	}
	intOpt := func(name, desc string) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionInteger, Name: name, Description: desc, Required: true}
	}
	sub := func(name, desc string, opts ...*discordgo.ApplicationCommandOption) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionSubCommand, Name: name, Description: desc,
			Options: append(opts, player)}
	}
	var haps []*discordgo.ApplicationCommandOptionChoice
	for _, h := range game.HappeningDefs {
		haps = append(haps, &discordgo.ApplicationCommandOptionChoice{Name: truncate(h.Emoji+" "+h.Name, 100), Value: h.Key})
	}
	var stats []*discordgo.ApplicationCommandOptionChoice
	for _, st := range game.AllStats {
		stats = append(stats, &discordgo.ApplicationCommandOptionChoice{Name: statName(st), Value: string(st)})
	}
	return &discordgo.ApplicationCommand{
		Name: "admin", Description: "Admin: testing tools (give gold, items, stats; rush timers)",
		Contexts: ctxs, IntegrationTypes: install,
		Options: []*discordgo.ApplicationCommandOption{
			sub("gold", "Give gold (negative takes it away)", intOpt("amount", "How much gold")),
			sub("influence", "Give influence", num("amount", "How much influence")),
			sub("renown", "Give renown", intOpt("amount", "How much renown")),
			sub("stat", "Raise a stat (in shown points)",
				&discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: "stat", Description: "Which stat",
					Required: true, Choices: stats},
				num("amount", "How many points")),
			sub("item", "Give an estate item, free",
				&discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: "item", Description: "Which item",
					Required: true, Autocomplete: true}),
			sub("opinion", "Change senators' opinion of a player",
				num("amount", "How much (-100 to 100)"),
				&discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: "senator",
					Description: "Which senator (defaults to every senator)", Autocomplete: true}),
			sub("spies", "Change a player's spy network on someone",
				&discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionUser, Name: "target", Description: "Whose court", Required: true},
				num("amount", "How much (the level is 0 to 100)")),
			sub("rush", "End a player's running actions now, and move their conspiracy on a day"),
			sub("happening", "Start a happening in the realm now (a manticore, a troll…)",
				&discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: "happening",
					Description: "Which one", Required: true, Choices: haps}),
		},
	}
}

// adminOptions are a subcommand's options by name.
type adminOptions map[string]*discordgo.ApplicationCommandInteractionDataOption

func adminSub(i *discordgo.InteractionCreate) (string, adminOptions) {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return "", nil
	}
	sub := data.Options[0]
	opts := adminOptions{}
	for _, o := range sub.Options {
		opts[o.Name] = o
	}
	return sub.Name, opts
}

func (o adminOptions) num(name string) float64 {
	if v, ok := o[name]; ok {
		switch x := v.Value.(type) {
		case float64:
			return x
		case int64:
			return float64(x)
		}
	}
	return 0
}

func (o adminOptions) str(name string) string {
	if v, ok := o[name]; ok {
		s, _ := v.Value.(string)
		return s
	}
	return ""
}

// isAdmin reports whether the caller holds this court, answering if not.
func (b *Bot) isAdmin(ctx context.Context, i *discordgo.InteractionCreate) bool {
	court, err := b.store.GetCourt(ctx, i.GuildID)
	if err != nil || court.AdminUserID != userOf(i).ID {
		b.reply(i, "Only the court's admin can use that.")
		return false
	}
	return true
}

func (b *Bot) cmdAdmin(ctx context.Context, i *discordgo.InteractionCreate) {
	if !b.isAdmin(ctx, i) {
		return
	}
	name, opts := adminSub(i)
	uid := userOf(i).ID
	if p := opts.str("player"); p != "" {
		uid = p
	}
	c, err := b.store.GetCharacter(ctx, i.GuildID, uid)
	if err != nil {
		b.reply(i, "That player has no character at this court.")
		return
	}
	now := time.Now()
	who := "**" + c.FullName() + "**"
	delta := func(d game.Delta) error {
		return b.store.ApplyOutcome(ctx, c.GuildID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: d}}, now)
	}
	var msg string
	switch name {
	case "gold":
		n := int(opts.num("amount"))
		err = delta(game.Delta{Wealth: n})
		msg = fmt.Sprintf("%s: %+d gold.", who, n)
	case "influence":
		n := opts.num("amount")
		err = delta(game.Delta{Influence: n})
		msg = fmt.Sprintf("%s: %s influence.", who, signed(n))
	case "renown":
		n := opts.num("amount")
		err = delta(game.Delta{Renown: n})
		msg = fmt.Sprintf("%s: %s renown.", who, signed(game.RoundRenown(n)))
	case "stat":
		st, n := game.Stat(opts.str("stat")), opts.num("amount")
		err = delta(game.Delta{Stats: game.Stats{st: n}})
		msg = fmt.Sprintf("%s: %s %s.", who, signed(n), statName(st))
	case "item":
		it, ok := adminItem(opts.str("item"))
		if !ok {
			b.reply(i, "There's no item by that name.")
			return
		}
		if _, err = b.store.BuyItem(ctx, c.ID, c.GuildID, it.Key, 0, now); err == nil {
			msg = fmt.Sprintf("%s now owns **%s**.", who, it.Name)
		}
	case "opinion":
		n := opts.num("amount")
		ch := game.OpinionChange{CharacterID: c.ID, Delta: n}
		target := "every senator"
		if s := opts.str("senator"); s != "" {
			sen, ok := b.adminSenator(ctx, c.GuildID, s)
			if !ok {
				b.reply(i, "There's no senator by that name.")
				return
			}
			ch.SenatorID, target = sen.ID, "Senator "+sen.Name
		}
		err = b.store.ChangeOpinions(ctx, []game.OpinionChange{ch})
		msg = fmt.Sprintf("%s: %s opinion with %s.", who, signed(n), target)
	case "spies":
		t, terr := b.store.GetCharacter(ctx, i.GuildID, opts.str("target"))
		if terr != nil || t.ID == c.ID {
			b.reply(i, "Pick another player who has a place at this court.")
			return
		}
		n := opts.num("amount")
		if err = b.store.ChangeSpies(ctx, []game.SpyChange{{Owner: c.ID, Target: t.ID, Delta: n}}); err == nil {
			lvl, _ := b.store.SpyLevel(ctx, c.ID, t.ID)
			msg = fmt.Sprintf("%s's spies in **%s**'s court: %s (%s).", who, t.FullName(), game.SpyTierOf(lvl).Name, game.Num(lvl))
		}
	case "happening":
		if n := b.startHappening(ctx, c.GuildID, opts.str("happening"), now); n != "" {
			msg = fmt.Sprintf("**%s** is abroad in the realm. The court has been told.", n)
		} else {
			msg = "That happening is already abroad (or doesn't exist)."
		}
	case "rush":
		if err = b.store.RushCharacter(ctx, c.ID, now); err == nil {
			msg = fmt.Sprintf("%s's actions end at the next tick (within 15 seconds), and their conspiracy moves on a day.", who)
		}
	default:
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			err = errors.New("not found")
		}
		b.reply(i, err.Error())
		return
	}
	slog.Info("admin", "cmd", name, "guild", c.GuildID, "character", c.ID)
	if name != "rush" && name != "spies" && name != "opinion" && name != "happening" {
		if after, err := b.store.GetCharacterByID(ctx, c.ID); err == nil {
			msg += "\n" + standingLine(after)
		}
	}
	b.respond(i, ephemeral(noteView("🛠️ "+msg, colorMuted)))
}

func signed(v float64) string {
	if v > 0 {
		return "+" + game.Num(v)
	}
	return game.Num(v)
}

// adminItem finds an item by key or name (autocomplete sends the key).
func adminItem(s string) (game.Item, bool) {
	if it, ok := game.ItemByKey(s); ok {
		return it, true
	}
	for _, it := range game.Items {
		if strings.EqualFold(it.Name, s) {
			return it, true
		}
	}
	return game.Item{}, false
}

func (b *Bot) adminSenator(ctx context.Context, guildID, s string) (game.Senator, bool) {
	sen, _ := b.store.Senators(ctx, guildID)
	for _, x := range sen {
		if fmt.Sprint(x.ID) == s || strings.EqualFold(x.Name, s) {
			return x, true
		}
	}
	return game.Senator{}, false
}

// onAdminAutocomplete suggests items and senators as the admin types.
func (b *Bot) onAdminAutocomplete(ctx context.Context, i *discordgo.InteractionCreate) {
	_, opts := adminSub(i)
	var focused *discordgo.ApplicationCommandInteractionDataOption
	for _, o := range opts {
		if o.Focused {
			focused = o
		}
	}
	if focused == nil {
		return
	}
	typed := strings.ToLower(fmt.Sprint(focused.Value))
	var choices []*discordgo.ApplicationCommandOptionChoice
	add := func(name, value string) {
		if len(choices) < 25 && strings.Contains(strings.ToLower(name), typed) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: truncate(name, 100), Value: value})
		}
	}
	switch focused.Name {
	case "item":
		for _, it := range game.Items {
			add(it.Name, it.Key)
		}
	case "senator":
		sen, _ := b.store.Senators(ctx, i.GuildID)
		for _, s := range sen {
			add(s.Name, fmt.Sprint(s.ID))
		}
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	}); err != nil {
		slog.Warn("admin autocomplete", "err", err)
	}
}
