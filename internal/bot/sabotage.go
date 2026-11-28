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

func init() {
	registerActivity(game.KindSabotage, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return sabotageView(b.t.Sabotage, b.spyBusy(ctx, c), "")
		},
		resolve: (*Bot).resolveSabotage,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("Your saboteurs are at work. You'll hear <t:%d:R>.", a.EndsAt.Unix())
		},
	})
	registerActivity(game.KindSmear, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return smearView(b.t.Smear, b.spyBusy(ctx, c), "")
		},
		resolve: (*Bot).resolveSmear,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("Your criers are spreading the word. You'll hear <t:%d:R>.", a.EndsAt.Unix())
		},
	})
	componentRoutes["sab"] = (*Bot).onSabotage
	componentRoutes["smear"] = (*Bot).onSmear
}

// Sabotage custom IDs:
//   sab:who                         (user select) the rival → what to hit
//   sab:what:<target>               (select) v:<sector> or p:<holding> → purse
//   sab:go:<target>:<what>:<purse>  start
//
// Smear custom IDs:
//   smear:who                       (user select) the rival → purse
//   smear:go:<target>:<purse>       start

// ---- sabotage ----

func sabotageView(d time.Duration, busy, note string) *discordgo.InteractionResponseData {
	desc := "Send agents against a rival. Burn a shipyard, start a strike at a mine, and their share of that industry earns less for days. " +
		"Or send vandals to one of their houses, even the one they live in: it gives them nothing until they pay to repair it.\n\n" +
		"Your Intrigue against theirs, helped by gold and by your spies in their court."
	e := actionEmbed(game.KindSabotage, withNote(busyNote(busy, note), desc))
	e.Color = colorSecret
	e.Fields = []*discordgo.MessageEmbedField{fact("Takes", humanDuration(d)), fact("Seen by", "🔒 Private, unless you're caught")}
	e.Footer = footer("⚠️ A clumsy attempt is noticed. A very clumsy one gives up your name. Sabotage is a crime, and spies can report it.")
	comps := []discordgo.MessageComponent{}
	if busy == "" {
		comps = append(comps, userSelect("sab:who", "Whom will you strike at?"))
	}
	comps = append(comps, row(backButton(game.KindSabotage)))
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
}

// sabotageTargets are what can be hit: a rival's public stakes, and their
// properties that aren't already damaged.
func sabotageTargets(t store.Character) []discordgo.SelectMenuOption {
	var opts []discordgo.SelectMenuOption
	for _, d := range game.SectorDefs {
		v, ok := t.Ventures[d.Key]
		if !ok || d.Secret {
			continue
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate("Their stake in "+d.Name, 100), Value: "v:" + string(d.Key),
			Emoji: &discordgo.ComponentEmoji{Name: d.Emoji}, Description: truncate(fmt.Sprintf("They own %s of it", pct(v.Share)), 100)})
	}
	home, _ := t.Residence()
	for _, h := range t.Holdings {
		it, ok := h.Item()
		if !ok || !it.IsProperty() || h.Repair > 0 || len(opts) >= 25 {
			continue
		}
		desc := it.Tier.String()
		if desc == "" {
			desc = "A property of theirs"
		}
		if h.ID == home.ID {
			desc = "Where they live"
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate(it.Name, 100), Value: fmt.Sprintf("p:%d", h.ID),
			Emoji: &discordgo.ComponentEmoji{Name: "🔥"}, Description: truncate(desc, 100)})
	}
	if len(opts) > 25 {
		opts = opts[:25]
	}
	return opts
}

func sabotageWhatView(t store.Character, note string) *discordgo.InteractionResponseData {
	opts := sabotageTargets(t)
	desc := fmt.Sprintf("What of **%s**'s will you strike at?", t.FullName())
	comps := []discordgo.MessageComponent{}
	if len(opts) == 0 {
		desc = fmt.Sprintf("**%s** has nothing worth burning: no ventures you can touch, and every house of theirs is already in ruins.", t.FullName())
	} else {
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: fmt.Sprintf("sab:what:%d", t.ID),
			Placeholder: "Their ventures and properties…", Options: opts}))
	}
	comps = append(comps, row(backTo("menu:act:"+game.KindSabotage)))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "🔥 Sabotage " + t.FullName(), Description: withNote(note, desc), Color: colorSecret}},
		Components: comps,
	}
}

// sabotageWhat names a sabotage target ("v:trade", "p:12") on t.
func sabotageWhat(t store.Character, what string) (kind, name string, ok bool) {
	k, v, _ := strings.Cut(what, ":")
	switch k {
	case "v":
		d, ok := game.SectorByKey(game.SectorKey(v))
		if !ok || d.Secret {
			return "", "", false
		}
		if _, owns := t.Ventures[d.Key]; !owns {
			return "", "", false
		}
		return game.SabotageVenture, d.Name, true
	case "p":
		h, ok := t.Holding(atoi(v))
		if !ok {
			return "", "", false
		}
		it, ok := h.Item()
		if !ok || !it.IsProperty() {
			return "", "", false
		}
		return game.SabotageProperty, it.Name, true
	}
	return "", "", false
}

func sabotagePurseView(c, t store.Character, what, name, note string) *discordgo.InteractionResponseData {
	var btns []discordgo.MessageComponent
	for _, p := range game.SpyPurses {
		label := p.Name
		if p.Gold > 0 {
			label = fmt.Sprintf("%s (%d gold)", p.Name, p.Gold)
		}
		btns = append(btns, button(label, fmt.Sprintf("sab:go:%d:%s:%s", t.ID, what, p.Key), discordgo.DangerButton, p.Gold > c.Wealth))
	}
	desc := fmt.Sprintf("Your agents will go after **%s** of **%s**. How much gold will you spend on them?", name, t.FullName())
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "🔥 Sabotage " + t.FullName(), Description: withNote(note, desc), Color: colorSecret,
			Fields: []*discordgo.MessageEmbedField{fact("Your purse", fmt.Sprintf("%d gold", c.Wealth))}}},
		Components: []discordgo.MessageComponent{row(btns...), row(backTo("menu:act:" + game.KindSabotage))},
	}
}

func (b *Bot) onSabotage(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "who":
		vals := i.MessageComponentData().Values
		if len(vals) == 0 {
			return
		}
		t, err := b.store.GetCharacter(ctx, c.GuildID, vals[0])
		if err != nil || t.ID == c.ID {
			b.update(i, sabotageView(b.t.Sabotage, b.spyBusy(ctx, c), "Pick another player who has a place at this court."))
			return
		}
		b.update(i, sabotageWhatView(t, ""))
	case parts[0] == "what" && len(parts) > 1:
		vals := i.MessageComponentData().Values
		t, err := b.store.GetCharacterByID(ctx, atoi(parts[1]))
		if len(vals) == 0 || err != nil || t.GuildID != c.GuildID {
			return
		}
		_, name, ok := sabotageWhat(t, vals[0])
		if !ok {
			b.update(i, sabotageWhatView(t, "That's no longer theirs to lose."))
			return
		}
		b.update(i, sabotagePurseView(c, t, vals[0], name, ""))
	case parts[0] == "go" && len(parts) > 4:
		b.onSabotageGo(ctx, i, c, atoi(parts[1]), parts[2]+":"+parts[3], parts[4])
	}
}

func (b *Bot) onSabotageGo(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, targetID int64, what, purseKey string) {
	t, err := b.store.GetCharacterByID(ctx, targetID)
	if err != nil || t.GuildID != c.GuildID || t.ID == c.ID {
		return
	}
	p, ok := game.PurseByKey(purseKey)
	if !ok {
		return
	}
	kind, name, ok := sabotageWhat(t, what)
	if !ok {
		b.update(i, sabotageWhatView(t, "That's no longer theirs to lose."))
		return
	}
	if busy := b.spyBusy(ctx, c); busy != "" {
		b.update(i, sabotageView(b.t.Sabotage, busy, ""))
		return
	}
	now := time.Now()
	ends := now.Add(b.t.Sabotage)
	if _, err := b.store.StartActivity(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Kind: game.KindSabotage, Goal: what + ":" + p.Key, TargetID: t.ID,
		StartedAt: now, EndsAt: ends,
	}, p.Gold, nil); err != nil {
		b.update(i, sabotagePurseView(c, t, what, name, err.Error()))
		return
	}
	b.update(i, startedView("🔥 Saboteurs on their way", fmt.Sprintf("Your agents slip away toward **%s** of **%s**. I'll DM you how it went.", name, t.FullName()),
		fact("You'll hear", relTime(ends))))
	b.leak(ctx, c, game.SecretSabotage(c.FullName(), t.FullName(), game.SabotageWhat(kind, name)))
}

func (b *Bot) resolveSabotage(ctx context.Context, a store.Activity, now time.Time) {
	c, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	t, err2 := b.store.GetCharacterByID(ctx, a.TargetID)
	sen, err3 := b.store.Senators(ctx, a.GuildID)
	parts := strings.Split(a.Goal, ":")
	if err := errors.Join(err1, err2, err3); err != nil || len(parts) != 3 {
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now)
		return
	}
	what := parts[0] + ":" + parts[1]
	p, _ := game.PurseByKey(parts[2])
	kind, name, ok := sabotageWhat(t, what)
	if !ok { // sold, or no longer theirs: the agents come home
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: {Wealth: a.Spend}}}, now)
		b.dmNote(c.UserID, "🔥 Sabotage called off", fmt.Sprintf("By the time your agents arrived, it was no longer **%s**'s. They bring your gold back.", t.FullName()), colorMuted)
		return
	}
	spies, _ := b.store.SpyLevel(ctx, c.ID, t.ID)
	res := game.ResolveSabotage(b.rng, game.SabotageInput{Actor: c.Person(), Target: t.Person(), Spies: spies, Purse: p}, sen)
	out := store.Outcome{Deltas: map[int64]game.Delta{c.ID: res.Actor}, Opinions: res.Opinions}
	var victim, actor string
	if res.Success {
		sb := store.Sabotage{GuildID: a.GuildID, CharacterID: t.ID}
		if kind == game.SabotageVenture {
			sb.Sector, sb.Factor = game.SectorKey(parts[1]), res.VentureFactor
			sb.Until = now.Add(game.SabotageDays * b.t.Day)
			hit := "earns half"
			if res.VentureFactor == 0 {
				hit = "earns nothing"
			}
			actor = fmt.Sprintf("Fire and riot: **%s**'s stake in %s %s for %d days.", t.FullName(), name, hit, game.SabotageDays)
			victim = fmt.Sprintf("Saboteurs struck at your interests in **%s**: fires, a strike, broken tools. Your share %s for %d days. Nobody knows who sent them.", name, hit, game.SabotageDays)
		} else {
			h, _ := t.Holding(atoi(parts[1]))
			it, _ := h.Item()
			sb.HoldingID, sb.RepairCost = h.ID, game.RepairCost(it)
			actor = fmt.Sprintf("Your vandals did their work. **%s**'s %s is good for nothing until they pay to repair it.", t.FullName(), name)
			victim = fmt.Sprintf("Vandals broke into **%s** in the night: smashed, burned, defiled. It gives you nothing until it's repaired (%d gold, on your estate page). Nobody knows who sent them.", name, sb.RepairCost)
		}
		out.Sabotage = []store.Sabotage{sb}
	} else {
		actor = fmt.Sprintf("Your agents couldn't get near **%s** of **%s**. They came home with nothing done.", name, t.FullName())
		switch {
		case res.Named:
			actor = fmt.Sprintf("Your agent was caught at **%s**'s %s, and talked. They know it was you.", t.FullName(), name)
			victim = fmt.Sprintf("Your guards caught a saboteur at your %s. Under questioning, they named **%s**.", name, c.FullName())
		case res.Noticed:
			victim = fmt.Sprintf("Your guards chased off men lurking at your %s with oil and torches. Someone wants to hurt you.", name)
		}
	}
	if err := b.store.ApplyResult(ctx, a.ID, out, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply sabotage", "err", err)
		}
		return
	}
	b.dmNote(c.UserID, "🔥 Your sabotage", withDelta(actor, res.Actor), resultColor(res.Success))
	if victim != "" {
		b.dmNote(t.UserID, "🔥 Sabotage", victim, colorBad)
	}
}

// ---- smears ----

func smearView(d time.Duration, busy, note string) *discordgo.InteractionResponseData {
	desc := "Pay printers and street-criers to spread lies about a rival. If the lies stick, they lose influence and the Senate thinks less of them. " +
		"Your Intrigue against theirs; gold buys more criers. They may catch you at it, learn your name, or answer the lies before anyone believes them."
	e := actionEmbed(game.KindSmear, withNote(busyNote(busy, note), desc))
	e.Color = colorSecret
	e.Fields = []*discordgo.MessageEmbedField{fact("Takes", humanDuration(d)), fact("Seen by", "🔒 Private, unless they find you out")}
	comps := []discordgo.MessageComponent{}
	if busy == "" {
		comps = append(comps, userSelect("smear:who", "Whom will you smear?"))
	}
	comps = append(comps, row(backButton(game.KindSmear)))
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
}

func smearPurseView(c, t store.Character, note string) *discordgo.InteractionResponseData {
	var btns []discordgo.MessageComponent
	for _, p := range game.SpyPurses {
		label := p.Name
		if p.Gold > 0 {
			label = fmt.Sprintf("%s (%d gold)", p.Name, p.Gold)
		}
		btns = append(btns, button(label, fmt.Sprintf("smear:go:%d:%s", t.ID, p.Key), discordgo.DangerButton, p.Gold > c.Wealth))
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "🗞️ Smear " + t.FullName(), Color: colorSecret,
			Description: withNote(note, fmt.Sprintf("How much will you spend on lies about **%s**?", t.FullName())),
			Fields:      []*discordgo.MessageEmbedField{fact("Your purse", fmt.Sprintf("%d gold", c.Wealth))}}},
		Components: []discordgo.MessageComponent{row(btns...), row(backTo("menu:act:" + game.KindSmear))},
	}
}

func (b *Bot) onSmear(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "who":
		vals := i.MessageComponentData().Values
		if len(vals) == 0 {
			return
		}
		t, err := b.store.GetCharacter(ctx, c.GuildID, vals[0])
		if err != nil || t.ID == c.ID {
			b.update(i, smearView(b.t.Smear, b.spyBusy(ctx, c), "Pick another player who has a place at this court."))
			return
		}
		b.update(i, smearPurseView(c, t, ""))
	case parts[0] == "go" && len(parts) > 2:
		t, err := b.store.GetCharacterByID(ctx, atoi(parts[1]))
		p, ok := game.PurseByKey(parts[2])
		if err != nil || !ok || t.GuildID != c.GuildID || t.ID == c.ID {
			return
		}
		if busy := b.spyBusy(ctx, c); busy != "" {
			b.update(i, smearView(b.t.Smear, busy, ""))
			return
		}
		now := time.Now()
		ends := now.Add(b.t.Smear)
		if _, err := b.store.StartActivity(ctx, store.Activity{
			GuildID: c.GuildID, HostID: c.ID, Kind: game.KindSmear, Goal: p.Key, TargetID: t.ID, StartedAt: now, EndsAt: ends,
		}, p.Gold, nil); err != nil {
			b.update(i, smearPurseView(c, t, err.Error()))
			return
		}
		b.update(i, startedView("🗞️ The presses are rolling", fmt.Sprintf("Your criers take to the streets with stories about **%s**. I'll DM you how it went.", t.FullName()),
			fact("You'll hear", relTime(ends))))
		b.leak(ctx, c, game.SecretSmear(c.FullName(), t.ID, t.FullName()))
	}
}

func (b *Bot) resolveSmear(ctx context.Context, a store.Activity, now time.Time) {
	c, err1 := b.store.GetCharacterByID(ctx, a.HostID)
	t, err2 := b.store.GetCharacterByID(ctx, a.TargetID)
	if err := errors.Join(err1, err2); err != nil {
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now)
		return
	}
	p, _ := game.PurseByKey(a.Goal)
	res := game.ResolveSmear(b.rng, game.SmearInput{Actor: c.Person(), Target: t.Person(), Purse: p, Laws: c.Laws})
	if err := b.store.ApplyResult(ctx, a.ID, store.Outcome{Deltas: res.Deltas, Opinions: res.Opinions}, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("apply smear", "err", err)
		}
		return
	}
	b.dmNotices(res.Notices, map[int64]string{c.ID: c.UserID, t.ID: t.UserID}, "🗞️ A smear campaign")
}
