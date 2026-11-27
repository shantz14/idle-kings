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
	registerActivity(game.KindConspiracy, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.conspiracyPanel(ctx, c, "")
		},
		resolve: (*Bot).resolvePlotExpired,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("You are leading a conspiracy. It falls apart <t:%d:R> unless you strike.", a.EndsAt.Unix())
		},
	})
	registerActivity(game.KindStrike, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.conspiracyPanel(ctx, c, "")
		},
		resolve: (*Bot).resolveStrike,
		busy: func(b *Bot, ctx context.Context, a store.Activity) string {
			return fmt.Sprintf("Your conspiracy is striking. You'll know how it went <t:%d:R>.", a.EndsAt.Unix())
		},
	})
	componentRoutes["consp"] = (*Bot).onConspiracy
}

// Conspiracy custom IDs:
//   consp:home                                     the panel (leading, or choosing a scheme)
//   consp:scheme:<scheme>                          choose a target for a scheme
//   consp:who:<scheme>                             (user select) a player target
//   consp:sen:<scheme>                             (select) a senator target
//   consp:item:<target>                            (select) what to steal (0 = their coffers)
//   consp:go:<scheme>:<p|s>:<target>:<item>:<purse> start the plot
//   consp:inv                                      (user select) invite a player
//   consp:invsen                                   (select) invite a senator → ask panel
//   consp:ask:<senator>:<gift 0|1>                 put it to the senator
//   consp:strike                                   strike (takes your action)
//   consp:abandon                                  give the plot up
//   consp:mine                                     plots you've been asked into
//   consp:plot:<id>                                one of them
//   consp:join:<id> / consp:decline:<id>           answer an invitation (DM)
//   consp:leave:<id> / consp:betray:<id>           get out, quietly or not

func (b *Bot) onConspiracy(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	switch parts[0] {
	case "join", "decline", "leave", "betray", "plot":
		if len(parts) > 1 {
			b.onPlotMember(ctx, i, parts[0], atoi(parts[1]))
		}
		return
	}
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch {
	case parts[0] == "home":
		b.update(i, b.conspiracyPanel(ctx, c, ""))
	case parts[0] == "scheme" && len(parts) > 1:
		sc, ok := game.SchemeByKey(parts[1])
		if !ok {
			return
		}
		b.update(i, b.targetPanel(ctx, c, sc, ""))
	case parts[0] == "who" && len(parts) > 1:
		b.onPlotTargetPlayer(ctx, i, c, parts[1])
	case parts[0] == "sen" && len(parts) > 1:
		sc, ok := game.SchemeByKey(parts[1])
		vals := i.MessageComponentData().Values
		if !ok || len(vals) == 0 {
			return
		}
		b.update(i, plotPurseView(c, sc, game.TargetSenator, atoi(vals[0]), 0, b.senatorName(ctx, c.GuildID, atoi(vals[0])), ""))
	case parts[0] == "item" && len(parts) > 1:
		vals := i.MessageComponentData().Values
		t, err := b.store.GetCharacterByID(ctx, atoi(parts[1]))
		if len(vals) == 0 || err != nil || t.GuildID != c.GuildID {
			return
		}
		sc, _ := game.SchemeByKey(game.SchemeSteal)
		b.update(i, plotPurseView(c, sc, game.TargetPlayer, t.ID, atoi(vals[0]), t.FullName(), ""))
	case parts[0] == "go" && len(parts) > 5:
		b.onPlotStart(ctx, i, c, parts[1:])
	case parts[0] == "inv":
		b.onInvitePlayer(ctx, i, c)
	case parts[0] == "invsen":
		vals := i.MessageComponentData().Values
		if len(vals) > 0 {
			b.update(i, b.askSenatorPanel(ctx, c, atoi(vals[0]), ""))
		}
	case parts[0] == "ask" && len(parts) > 2:
		b.onAskSenator(ctx, i, c, atoi(parts[1]), parts[2] == "1")
	case parts[0] == "strike":
		b.onStrike(ctx, i, c)
	case parts[0] == "abandon":
		b.onAbandonPlot(ctx, i, c)
	case parts[0] == "mine":
		b.update(i, b.myPlotsPanel(ctx, c, ""))
	}
}

// ---- loading a plot ----

// plotView is a plot with everyone in it loaded.
type plotView struct {
	K        store.Conspiracy
	Scheme   game.SchemeDef
	Leader   store.Character
	Members  []store.Character // players who joined
	Invited  []store.Character // players asked, not yet answered
	Senators []game.Senator    // senators who joined
	Senate   []game.Senator    // the whole Senate
	Target   *store.Character  // a player target
	SenTgt   *game.Senator     // a senator target
	ItemName string            // what a theft is after
}

func (b *Bot) loadPlot(ctx context.Context, k store.Conspiracy) (plotView, error) {
	sc, ok := game.SchemeByKey(k.Goal)
	if !ok {
		return plotView{}, fmt.Errorf("unknown scheme %q", k.Goal)
	}
	pv := plotView{K: k, Scheme: sc}
	ids := []int64{k.HostID}
	for _, m := range k.Members {
		if m.Kind == store.MemberPlayer && (m.Status == store.MemberJoined || m.Status == store.MemberInvited) {
			ids = append(ids, m.ID)
		}
	}
	if k.TargetKind == game.TargetPlayer {
		ids = append(ids, k.TargetID)
	}
	chars, err1 := b.store.CharactersByIDs(ctx, ids)
	sen, err2 := b.store.Senators(ctx, k.GuildID)
	if err := errors.Join(err1, err2); err != nil {
		return pv, err
	}
	leader, ok := chars[k.HostID]
	if !ok {
		return pv, store.ErrNotFound
	}
	pv.Leader, pv.Senate = leader, sen
	for _, m := range k.Members {
		switch {
		case m.Kind == store.MemberPlayer && m.Status == store.MemberJoined:
			if c, ok := chars[m.ID]; ok {
				pv.Members = append(pv.Members, c)
			}
		case m.Kind == store.MemberPlayer && m.Status == store.MemberInvited:
			if c, ok := chars[m.ID]; ok {
				pv.Invited = append(pv.Invited, c)
			}
		case m.Kind == store.MemberSenator && m.Status == store.MemberJoined:
			for _, s := range sen {
				if s.ID == m.ID {
					pv.Senators = append(pv.Senators, s)
				}
			}
		}
	}
	if k.TargetKind == game.TargetPlayer {
		t, ok := chars[k.TargetID]
		if !ok {
			return pv, store.ErrNotFound
		}
		pv.Target = &t
		if sc.Key == game.SchemeSteal {
			pv.ItemName = "the gold in their coffers"
			if h, ok := t.Holding(k.TargetItem); ok {
				if it, ok := h.Item(); ok {
					pv.ItemName = it.Name
				}
			}
		}
	} else {
		for _, s := range sen {
			if s.ID == k.TargetID {
				pv.SenTgt = &s
			}
		}
		if pv.SenTgt == nil {
			return pv, store.ErrNotFound
		}
	}
	return pv, nil
}

func (pv plotView) targetName() string {
	if pv.Target != nil {
		return pv.Target.FullName()
	}
	if pv.SenTgt != nil {
		return "Senator " + pv.SenTgt.Name
	}
	return "someone"
}

func (pv plotView) defender() game.Defender {
	if pv.Target != nil {
		p := pv.Target.Person()
		return game.Defender{Player: &p}
	}
	return game.Defender{Senator: pv.SenTgt}
}

// rules is the plot as the rules see it. spies are the target's networks
// on each plotter (players only).
func (pv plotView) rules(spies map[int64]float64) game.Plot {
	purse, _ := game.ConspiracyPurseByKey(pv.K.Purse)
	p := game.Plot{
		Scheme: pv.Scheme, Leader: game.Plotter{Person: pv.Leader.Person(), Spies: spies[pv.Leader.ID]},
		Senators: pv.Senators, Purse: purse, Progress: pv.K.Progress, Warned: pv.K.Warned,
	}
	for _, m := range pv.Members {
		p.Members = append(p.Members, game.Plotter{Person: m.Person(), Spies: spies[m.ID]})
	}
	return p
}

// targetSpies is the target's spy level on everyone, by character.
func (b *Bot) targetSpies(ctx context.Context, pv plotView) map[int64]float64 {
	out := map[int64]float64{}
	if pv.Target == nil {
		return out
	}
	nets, err := b.store.SpyNetworksBy(ctx, pv.Target.ID)
	if err != nil {
		slog.Error("plot spies", "err", err)
	}
	for _, n := range nets {
		out[n.TargetID] = n.Value
	}
	return out
}

func (b *Bot) senatorName(ctx context.Context, guildID string, id int64) string {
	sen, _ := b.store.Senators(ctx, guildID)
	for _, s := range sen {
		if s.ID == id {
			return "Senator " + s.Name
		}
	}
	return "a senator"
}

// ---- the panel ----

func (b *Bot) conspiracyPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	asked, err := b.store.ConspiraciesOf(ctx, c.ID)
	if err != nil {
		slog.Error("conspiracies of", "err", err)
	}
	k, err := b.store.LedConspiracy(ctx, c.ID)
	if errors.Is(err, store.ErrNotFound) {
		return schemesView(len(asked), c.RealmFallen, note)
	} else if err != nil {
		return noteView("Your conspiracy can't be found right now. Try again in a moment.", colorMuted)
	}
	pv, err := b.loadPlot(ctx, k)
	if err != nil {
		return noteView("Your conspiracy can't be found right now. Try again in a moment.", colorMuted)
	}
	striking := false
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil && a.Kind == game.KindStrike && a.TargetID == k.ID {
		striking = true
	}
	busy := ""
	if !striking {
		busy = b.spyBusy(ctx, c)
	}
	return leaderView(pv, b.askable(pv), pv.rules(nil).Power(), striking, busy, b.t.Strike, len(asked), note)
}

// askable lists the senators the leader can still ask in.
func (b *Bot) askable(pv plotView) []game.Senator {
	var out []game.Senator
	for _, s := range pv.Senate {
		if pv.SenTgt != nil && s.ID == pv.SenTgt.ID {
			continue
		}
		if _, asked := pv.K.Member(store.MemberSenator, s.ID); asked {
			continue
		}
		out = append(out, s)
	}
	return out
}

func schemesView(asked int, fallen bool, note string) *discordgo.InteractionResponseData {
	var sb strings.Builder
	sb.WriteString("Choose a scheme and a target, then bring others in: players with the right skills, senators with influence, gold for hired hands. ")
	sb.WriteString("The plot ripens over a few days **without taking your action**, faster with more behind it. But every day, the more people know, the likelier it is to be found out. ")
	sb.WriteString("When you dare, **strike**; striking early is riskier.\n\n")
	for _, s := range game.SchemeDefs {
		fmt.Fprintf(&sb, "%s **%s**\n%s\n\n", s.Emoji, s.Name, s.Desc)
	}
	e := actionEmbed(game.KindConspiracy, withNote(note, strings.TrimSpace(sb.String())))
	e.Color = colorSecret
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Lasts", fmt.Sprintf("%d days, unless you strike", game.ConspiracyDays)),
		fact("Seen by", "🔒 Private"),
	}
	e.Footer = footer("⚠️ Every plot is a crime. If it's found out, the target learns who; if a strike goes wrong, the whole court does.")
	var btns []discordgo.MessageComponent
	for _, s := range game.SchemeDefs {
		btns = append(btns, withEmoji(button(s.Name, "consp:scheme:"+s.Key, discordgo.PrimaryButton, fallen), s.Emoji))
	}
	nav := []discordgo.MessageComponent{backButton(game.KindConspiracy)}
	if asked > 0 {
		nav = append([]discordgo.MessageComponent{withEmoji(button(fmt.Sprintf("Plots you're in (%d)", asked), "consp:mine", discordgo.SecondaryButton, false), "🤝")}, nav...)
	}
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{e},
		Components: []discordgo.MessageComponent{row(btns...), row(nav...)},
	}
}

func leaderView(pv plotView, askable []game.Senator, power float64, striking bool, busy string, strike time.Duration, asked int, note string) *discordgo.InteractionResponseData {
	sc := pv.Scheme
	desc := fmt.Sprintf("%s You are plotting to %s **%s**.", sc.Emoji, sc.Verb(), pv.targetName())
	if pv.ItemName != "" {
		desc += fmt.Sprintf(" You're after **%s**.", pv.ItemName)
	}
	desc += "\n\nBring in players with Intrigue and " + statName(sc.Stat) + ", and senators with influence. Every one of them makes the plot stronger, and one more mouth to keep shut."
	if striking {
		desc = withNote("⏳ Your people are moving. You'll know how it went soon.", desc)
	}
	var in []string
	for _, m := range pv.Members {
		in = append(in, "🤝 "+m.FullName())
	}
	for _, s := range pv.Senators {
		in = append(in, "🎩 Senator "+s.Name)
	}
	for _, m := range pv.Invited {
		in = append(in, "✉️ "+m.FullName()+" *(asked)*")
	}
	inText := "Only you, so far."
	if len(in) > 0 {
		inText = joinLines(in, 1000)
	}
	e := actionEmbed(game.KindConspiracy, withNote(busyNote(busy, note), desc))
	e.Color = colorSecret
	e.Fields = []*discordgo.MessageEmbedField{
		fact("Ripeness", game.ProgressStage(pv.K.Progress)),
		fact("Strength", game.PowerWord(power)),
		fact("Falls apart", relTime(pv.K.EndsAt)),
		{Name: "In on it", Value: inText},
	}
	e.Footer = footer(fmt.Sprintf("🗡️ Striking takes %s and your action. The riper the plot, the better your odds.", humanDuration(strike)))
	var comps []discordgo.MessageComponent
	if !striking {
		comps = append(comps, userSelect("consp:inv", "Invite a player…"))
		if len(askable) > 0 {
			var opts []discordgo.SelectMenuOption
			for _, s := range askable {
				if len(opts) == 25 {
					break
				}
				opts = append(opts, discordgo.SelectMenuOption{Label: s.Name, Value: fmt.Sprint(s.ID),
					Description: truncate(fmt.Sprintf("%s · influence %s", s.TraitNames(), game.Num(s.Influence)), 100)})
			}
			comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "consp:invsen",
				Placeholder: "Invite a senator…", Options: opts}))
		}
	}
	btns := []discordgo.MessageComponent{
		withEmoji(button("Strike", "consp:strike", discordgo.DangerButton, striking || busy != ""), "🗡️"),
		withEmoji(button("Abandon the plot", "consp:abandon", discordgo.SecondaryButton, striking), "🕯️"),
	}
	if asked > 0 {
		btns = append(btns, withEmoji(button(fmt.Sprintf("Plots you're in (%d)", asked), "consp:mine", discordgo.SecondaryButton, false), "🤝"))
	}
	btns = append(btns, backButton(game.KindConspiracy))
	comps = append(comps, row(btns...))
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
}

func statName(s game.Stat) string {
	n := string(s)
	return strings.ToUpper(n[:1]) + n[1:]
}

// ---- starting a plot ----

func (b *Bot) targetPanel(ctx context.Context, c store.Character, sc game.SchemeDef, note string) *discordgo.InteractionResponseData {
	var sen []game.Senator
	if sc.Aims(game.TargetSenator) {
		var err error
		if sen, err = b.store.Senators(ctx, c.GuildID); err != nil {
			slog.Error("senators", "err", err)
		}
	}
	return targetView(sc, sen, note)
}

func targetView(sc game.SchemeDef, senators []game.Senator, note string) *discordgo.InteractionResponseData {
	e := &discordgo.MessageEmbed{Title: sc.Emoji + " " + sc.Name, Description: withNote(note, sc.Desc+"\n\nWho is it aimed at?"), Color: colorSecret}
	var comps []discordgo.MessageComponent
	if sc.Aims(game.TargetPlayer) {
		comps = append(comps, userSelect("consp:who:"+sc.Key, "Choose a player…"))
	}
	if sc.Aims(game.TargetSenator) && len(senators) > 0 {
		var opts []discordgo.SelectMenuOption
		for _, s := range senators {
			if len(opts) == 25 {
				break
			}
			opts = append(opts, discordgo.SelectMenuOption{Label: s.Name, Value: fmt.Sprint(s.ID),
				Description: truncate(fmt.Sprintf("%s · influence %s", s.TraitNames(), game.Num(s.Influence)), 100)})
		}
		comps = append(comps, row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "consp:sen:" + sc.Key,
			Placeholder: "Choose a senator…", Options: opts}))
	}
	comps = append(comps, row(backTo("consp:home")))
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: comps}
}

func (b *Bot) onPlotTargetPlayer(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, scheme string) {
	sc, ok := game.SchemeByKey(scheme)
	vals := i.MessageComponentData().Values
	if !ok || len(vals) == 0 {
		return
	}
	t, err := b.store.GetCharacter(ctx, c.GuildID, vals[0])
	if err != nil || t.ID == c.ID {
		b.update(i, b.targetPanel(ctx, c, sc, "Pick another player who has a place at this court."))
		return
	}
	if sc.Key == game.SchemeSteal {
		b.update(i, stealView(t))
		return
	}
	b.update(i, plotPurseView(c, sc, game.TargetPlayer, t.ID, 0, t.FullName(), ""))
}

// stealView picks what to steal: a possession, or their gold.
func stealView(t store.Character) *discordgo.InteractionResponseData {
	opts := []discordgo.SelectMenuOption{{Label: "The gold in their coffers", Value: "0", Emoji: &discordgo.ComponentEmoji{Name: "🪙"},
		Description: "A share of whatever gold they have when you strike."}}
	for _, h := range t.Holdings {
		it, ok := h.Item()
		if !ok || it.IsProperty() || len(opts) == 25 {
			continue
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: truncate(it.Name, 100), Value: fmt.Sprint(h.ID),
			Description: truncate(fmt.Sprintf("Worth about %d gold", it.Price), 100)})
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "💰 Steal from " + t.FullName(),
			Description: "What are your burglars after? Houses and lands can't be carried off.", Color: colorSecret}},
		Components: []discordgo.MessageComponent{
			row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: fmt.Sprintf("consp:item:%d", t.ID),
				Placeholder: "Choose the prize…", Options: opts}),
			row(backTo("consp:scheme:" + game.SchemeSteal)),
		},
	}
}

func tkCode(k game.TargetKind) string {
	if k == game.TargetSenator {
		return "s"
	}
	return "p"
}

func plotPurseView(c store.Character, sc game.SchemeDef, tk game.TargetKind, target, item int64, name, note string) *discordgo.InteractionResponseData {
	desc := fmt.Sprintf("You will plot to %s **%s**.\n\nGold buys hired hands: cutthroats, forgers or burglars. They make the plot stronger, and don't talk.", sc.Verb(), name)
	var btns []discordgo.MessageComponent
	for _, p := range game.ConspiracyPurses {
		label := p.Name
		if p.Gold > 0 {
			label = fmt.Sprintf("%s (%d gold)", p.Name, p.Gold)
		}
		btns = append(btns, button(label, fmt.Sprintf("consp:go:%s:%s:%d:%d:%s", sc.Key, tkCode(tk), target, item, p.Key), discordgo.SuccessButton, p.Gold > c.Wealth))
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: sc.Emoji + " " + sc.Name, Description: withNote(note, desc), Color: colorSecret,
			Fields: []*discordgo.MessageEmbedField{fact("Your purse", fmt.Sprintf("%d gold", c.Wealth))}}},
		Components: []discordgo.MessageComponent{row(btns...), row(backTo("consp:scheme:" + sc.Key))},
	}
}

func (b *Bot) onPlotStart(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, p []string) {
	sc, ok1 := game.SchemeByKey(p[0])
	purse, ok2 := game.ConspiracyPurseByKey(p[4])
	tk := game.TargetPlayer
	if p[1] == "s" {
		tk = game.TargetSenator
	}
	if !ok1 || !ok2 || !sc.Aims(tk) {
		return
	}
	if c.RealmFallen {
		b.update(i, b.conspiracyPanel(ctx, c, "The realm has fallen. There is nothing left to plot for."))
		return
	}
	target, item := atoi(p[2]), atoi(p[3])
	name := ""
	if tk == game.TargetPlayer {
		t, err := b.store.GetCharacterByID(ctx, target)
		if err != nil || t.GuildID != c.GuildID || t.ID == c.ID {
			b.update(i, b.conspiracyPanel(ctx, c, "Pick another player who has a place at this court."))
			return
		}
		name = t.FullName()
	} else {
		name = b.senatorName(ctx, c.GuildID, target)
	}
	now := time.Now()
	ends := now.Add(time.Duration(game.ConspiracyDays) * b.t.Day)
	_, err := b.store.StartConspiracy(ctx, store.Activity{
		GuildID: c.GuildID, HostID: c.ID, Goal: sc.Key, TargetID: target, StartedAt: now, EndsAt: ends,
	}, purse.Gold, store.Conspiracy{TargetKind: tk, TargetItem: item, Purse: purse.Key, NextStepAt: now.Add(b.t.Day)})
	if err != nil {
		b.update(i, b.conspiracyPanel(ctx, c, err.Error()))
		return
	}
	b.update(i, b.conspiracyPanel(ctx, c, fmt.Sprintf("The plot against **%s** begins. Now find people you can trust.", name)))
	b.leak(ctx, c, game.SecretConspire(c.FullName(), "", sc.Verb(), name))
}

func (b *Bot) onAbandonPlot(ctx context.Context, i *discordgo.InteractionCreate, c store.Character) {
	k, err := b.store.LedConspiracy(ctx, c.ID)
	if err != nil {
		b.update(i, b.conspiracyPanel(ctx, c, ""))
		return
	}
	if a, err := b.store.HostedActivity(ctx, c.ID); err == nil && a.Kind == game.KindStrike {
		b.update(i, b.conspiracyPanel(ctx, c, "Your people are already moving. It's too late to call them back."))
		return
	}
	pv, _ := b.loadPlot(ctx, k)
	if err := b.store.EndConspiracy(ctx, k.ID, store.Outcome{}, time.Now()); err != nil {
		b.update(i, b.conspiracyPanel(ctx, c, ""))
		return
	}
	for _, m := range pv.Members {
		b.dmNote(m.UserID, "🕯️ A plot abandoned", fmt.Sprintf("**%s** has given up the plot against **%s**. Burn the letters.", c.FullName(), pv.targetName()), colorMuted)
	}
	b.update(i, b.conspiracyPanel(ctx, c, "You burn the letters and send your friends home. Nobody need ever know."))
}

// ---- inviting ----

func (b *Bot) onInvitePlayer(ctx context.Context, i *discordgo.InteractionCreate, c store.Character) {
	vals := i.MessageComponentData().Values
	k, err := b.store.LedConspiracy(ctx, c.ID)
	if len(vals) == 0 || err != nil {
		b.update(i, b.conspiracyPanel(ctx, c, ""))
		return
	}
	t, err := b.store.GetCharacter(ctx, c.GuildID, vals[0])
	switch {
	case err != nil || t.ID == c.ID:
		b.update(i, b.conspiracyPanel(ctx, c, "Pick another player who has a place at this court."))
		return
	case k.TargetKind == game.TargetPlayer && t.ID == k.TargetID:
		b.update(i, b.conspiracyPanel(ctx, c, "You can hardly ask them to plot against themselves."))
		return
	}
	pv, err := b.loadPlot(ctx, k)
	if err != nil {
		b.fail(i, err)
		return
	}
	if err := b.store.InviteMember(ctx, k.ID, store.MemberPlayer, t.ID, store.MemberInvited, 0, time.Now()); errors.Is(err, store.ErrInvited) {
		b.update(i, b.conspiracyPanel(ctx, c, fmt.Sprintf("You've already asked **%s**.", t.FullName())))
		return
	} else if err != nil {
		b.update(i, b.conspiracyPanel(ctx, c, err.Error()))
		return
	}
	b.deferUpdate(i)
	_, _ = b.dm(t.UserID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{inviteEmbed(pv)},
		Components: []discordgo.MessageComponent{inviteButtons(k.ID)},
	})
	b.update(i, b.conspiracyPanel(ctx, c, fmt.Sprintf("A sealed letter goes to **%s**. You'll hear when they answer.", t.FullName())))
}

func inviteEmbed(pv plotView) *discordgo.MessageEmbed {
	desc := fmt.Sprintf("**%s** has taken you aside. They are plotting to %s **%s**, and want you in.\n\n%s",
		pv.Leader.FullName(), pv.Scheme.Verb(), pv.targetName(), pv.Scheme.Desc)
	return &discordgo.MessageEmbed{
		Title: "🕸️ An invitation to a conspiracy", Description: desc, Color: colorNotice,
		Fields: []*discordgo.MessageEmbedField{fact("It falls apart", relTime(pv.K.EndsAt))},
		Footer: footer("🔒 Joining doesn't take your action. If the plot is found out, your name may be too. You can always leave, or betray it."),
	}
}

func inviteButtons(id int64) discordgo.ActionsRow {
	return row(
		withEmoji(button("Join the plot", fmt.Sprintf("consp:join:%d", id), discordgo.PrimaryButton, false), "🤝"),
		withEmoji(button("Decline", fmt.Sprintf("consp:decline:%d", id), discordgo.SecondaryButton, false), "✋"),
		withEmoji(button("Betray it", fmt.Sprintf("consp:betray:%d", id), discordgo.DangerButton, false), "🐍"),
	)
}

func (b *Bot) askSenatorPanel(ctx context.Context, c store.Character, senID int64, note string) *discordgo.InteractionResponseData {
	sen, err := b.store.Senators(ctx, c.GuildID)
	ops, err2 := b.store.Opinions(ctx, c.ID)
	if errors.Join(err, err2) != nil {
		return noteView("The Senate can't be reached right now. Try again in a moment.", colorMuted)
	}
	for _, s := range sen {
		if s.ID == senID {
			return askSenatorView(c, s, ops[s.ID], note)
		}
	}
	return b.conspiracyPanel(ctx, c, "")
}

func askSenatorView(c store.Character, s game.Senator, op float64, note string) *discordgo.InteractionResponseData {
	gift := game.JoinGiftCost(s)
	desc := fmt.Sprintf("Put it to **Senator %s** (%s). Whether they join depends on how they feel about you, and about your target, and on who they are.\n\n"+
		"If they refuse, they may not keep your secret: the honest ones are the likeliest to talk.", s.Name, s.TraitNames())
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "🎩 Invite Senator " + s.Name, Description: withNote(note, desc), Color: colorSecret,
			Fields: []*discordgo.MessageEmbedField{
				fact("Opinion of you", fmt.Sprintf("%s (%s)", game.OpinionNum(op), game.OpinionWord(op))),
				fact("Influence", game.Num(s.Influence)),
				fact("Your purse", fmt.Sprintf("%d gold", c.Wealth)),
			},
			Footer: footer("🔒 A private word. Spies may hear of it."),
		}},
		Components: []discordgo.MessageComponent{row(
			withEmoji(button("Ask them", fmt.Sprintf("consp:ask:%d:0", s.ID), discordgo.PrimaryButton, false), "🤫"),
			withEmoji(button(fmt.Sprintf("Ask, with a gift (%d gold)", gift), fmt.Sprintf("consp:ask:%d:1", s.ID), discordgo.SuccessButton, gift > c.Wealth), "🎁"),
			backTo("consp:home"),
		)},
	}
}

func (b *Bot) onAskSenator(ctx context.Context, i *discordgo.InteractionCreate, c store.Character, senID int64, gift bool) {
	k, err := b.store.LedConspiracy(ctx, c.ID)
	if err != nil {
		b.update(i, b.conspiracyPanel(ctx, c, ""))
		return
	}
	pv, err := b.loadPlot(ctx, k)
	if err != nil {
		b.fail(i, err)
		return
	}
	var s *game.Senator
	for _, x := range b.askable(pv) {
		if x.ID == senID {
			s = &x
		}
	}
	if s == nil {
		b.update(i, b.conspiracyPanel(ctx, c, "You can't ask that senator."))
		return
	}
	ops, err := b.store.CourtOpinions(ctx, c.GuildID)
	if err != nil {
		b.fail(i, err)
		return
	}
	opTarget := 0.0
	if pv.Target != nil {
		opTarget = ops.Of(s.ID, pv.Target.ID)
	}
	res := game.ResolveJoin(b.rng, *s, pv.Scheme, c.ID, ops.Of(s.ID, c.ID), opTarget, gift)
	status, cost := store.MemberDeclined, 0
	if res.Joined {
		status = store.MemberJoined
	}
	if gift {
		cost = game.JoinGiftCost(*s)
	}
	now := time.Now()
	if err := b.store.InviteMember(ctx, k.ID, store.MemberSenator, s.ID, status, cost, now); err != nil {
		b.update(i, b.askSenatorPanel(ctx, c, s.ID, err.Error()))
		return
	}
	out := store.Outcome{Deltas: map[int64]game.Delta{c.ID: res.Practice}, Opinions: res.Opinions}
	if res.Reported {
		b.deferUpdate(i)
		b.reportPlot(ctx, pv, "Senator "+s.Name, out, now)
	} else if err := b.store.ApplyOutcome(ctx, c.GuildID, out, now); err != nil {
		slog.Error("ask senator", "err", err)
	}
	b.update(i, b.conspiracyPanel(ctx, c, res.Text))
	b.leak(ctx, c, game.SecretConspire(c.FullName(), "", pv.Scheme.Verb(), pv.targetName()))
}

// reportPlot is someone warning the target of a plot and naming its leader.
// The plot goes on, but the target is on guard.
func (b *Bot) reportPlot(ctx context.Context, pv plotView, who string, out store.Outcome, now time.Time) {
	leader := pv.Leader
	switch {
	case pv.Target != nil:
		out.Denunciations = append(out.Denunciations, plotReport(pv, pv.Target.ID, leader))
	case pv.SenTgt != nil:
		out.Opinions = append(out.Opinions, game.OpinionChange{SenatorID: pv.SenTgt.ID, CharacterID: leader.ID, Delta: senTargetNamedOpinion})
	}
	if err := b.store.WarnPlot(ctx, pv.K.ID, game.MemberNamed, out, now); err != nil {
		slog.Error("report plot", "err", err)
		return
	}
	if pv.Target != nil {
		b.dmNote(pv.Target.UserID, "🐍 A warning",
			fmt.Sprintf("%s sends word: **%s** is plotting to %s you. They're filed in your dossier.", who, leader.FullName(), pv.Scheme.Verb()), colorBad)
	}
}

// plotReport files a plotter in someone's dossier.
func plotReport(pv plotView, owner int64, plotter store.Character) store.Denunciation {
	return store.Denunciation{GuildID: pv.K.GuildID, OwnerID: owner, ActorID: plotter.ID, Crime: game.SecretKindConspiracy,
		Detail: fmt.Sprintf("%s is part of a plot to %s **%s**.", plotter.FullName(), pv.Scheme.Verb(), pv.targetName())}
}

const (
	senTargetNamedOpinion  = -15.0 // a senator who learns who plots against them
	senTargetLeaderOpinion = -30.0
	betrayOpinion          = 10.0 // a senator grateful for the warning
)

// ---- members ----

// plotFor loads a plot for one of its members, from a button that may be
// in a DM. The character is the caller's at the plot's court.
func (b *Bot) plotFor(ctx context.Context, i *discordgo.InteractionCreate, id int64) (plotView, store.Character, bool) {
	k, err := b.store.GetConspiracy(ctx, id)
	if err != nil {
		b.reply(i, "That plot can't be found.")
		return plotView{}, store.Character{}, false
	}
	c, err := b.store.GetCharacter(ctx, k.GuildID, userOf(i).ID)
	if err != nil {
		b.reply(i, "That plot isn't at your court.")
		return plotView{}, c, false
	}
	if k.Resolved {
		b.update(i, noteView("That plot is over.", colorMuted))
		return plotView{}, c, false
	}
	pv, err := b.loadPlot(ctx, k)
	if err != nil {
		b.fail(i, err)
		return pv, c, false
	}
	return pv, c, true
}

func (b *Bot) onPlotMember(ctx context.Context, i *discordgo.InteractionCreate, what string, id int64) {
	pv, c, ok := b.plotFor(ctx, i, id)
	if !ok {
		return
	}
	m, asked := pv.K.Member(store.MemberPlayer, c.ID)
	if !asked || (m.Status != store.MemberInvited && m.Status != store.MemberJoined) {
		b.update(i, noteView("You're not part of that plot.", colorMuted))
		return
	}
	now := time.Now()
	switch what {
	case "plot":
		b.update(i, memberView(pv, m.Status, ""))
	case "join":
		if err := b.store.SetMemberStatus(ctx, id, store.MemberPlayer, c.ID, store.MemberInvited, store.MemberJoined); err != nil {
			b.update(i, memberView(pv, m.Status, "You've already answered."))
			return
		}
		b.dmNote(pv.Leader.UserID, "🤝 A new conspirator", fmt.Sprintf("**%s** is with you against **%s**.", c.FullName(), pv.targetName()), colorGood)
		b.update(i, memberView(pv, store.MemberJoined, "You're in. Keep it quiet."))
		b.leak(ctx, c, game.SecretConspire(c.FullName(), pv.Leader.FullName(), pv.Scheme.Verb(), pv.targetName()))
	case "decline":
		if err := b.store.SetMemberStatus(ctx, id, store.MemberPlayer, c.ID, store.MemberInvited, store.MemberDeclined); err != nil {
			b.update(i, memberView(pv, m.Status, "You've already answered."))
			return
		}
		b.dmNote(pv.Leader.UserID, "✋ An invitation declined", fmt.Sprintf("**%s** wants no part in it.", c.FullName()), colorMuted)
		b.update(i, noteView("You decline, and say nothing more about it.", colorMuted))
	case "leave":
		if err := b.store.SetMemberStatus(ctx, id, store.MemberPlayer, c.ID, store.MemberJoined, store.MemberLeft); err != nil {
			b.update(i, memberView(pv, m.Status, ""))
			return
		}
		b.dmNote(pv.Leader.UserID, "🚪 A conspirator leaves", fmt.Sprintf("**%s** has left the plot against **%s**.", c.FullName(), pv.targetName()), colorBad)
		b.update(i, noteView("You quietly step away from the plot.", colorMuted))
	case "betray":
		b.deferUpdate(i)
		b.betrayPlot(ctx, i, pv, c, now)
	}
}

func memberView(pv plotView, status, note string) *discordgo.InteractionResponseData {
	desc := fmt.Sprintf("%s **%s** is plotting to %s **%s**.", pv.Scheme.Emoji, pv.Leader.FullName(), pv.Scheme.Verb(), pv.targetName())
	e := &discordgo.MessageEmbed{Title: "🕸️ A conspiracy", Description: withNote(note, desc), Color: colorSecret,
		Fields: []*discordgo.MessageEmbedField{
			fact("Ripeness", game.ProgressStage(pv.K.Progress)),
			fact("Falls apart", relTime(pv.K.EndsAt)),
		},
		Footer: footer("🐍 Betraying it warns the target, names the leader to them, and ends the plot. It also puts the leader in your dossier."),
	}
	var btns []discordgo.MessageComponent
	id := pv.K.ID
	if status == store.MemberInvited {
		btns = inviteButtons(id).Components
	} else {
		btns = []discordgo.MessageComponent{
			withEmoji(button("Leave quietly", fmt.Sprintf("consp:leave:%d", id), discordgo.SecondaryButton, false), "🚪"),
			withEmoji(button("Betray it", fmt.Sprintf("consp:betray:%d", id), discordgo.DangerButton, false), "🐍"),
		}
	}
	return &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{e}, Components: []discordgo.MessageComponent{row(btns...)}}
}

func (b *Bot) myPlotsPanel(ctx context.Context, c store.Character, note string) *discordgo.InteractionResponseData {
	ks, err := b.store.ConspiraciesOf(ctx, c.ID)
	if err != nil {
		slog.Error("conspiracies of", "err", err)
	}
	var lines []string
	var btns []discordgo.MessageComponent
	for _, k := range ks {
		pv, err := b.loadPlot(ctx, k)
		if err != nil {
			continue
		}
		m, _ := k.Member(store.MemberPlayer, c.ID)
		state := "you're in"
		if m.Status == store.MemberInvited {
			state = "you've been asked"
		}
		lines = append(lines, fmt.Sprintf("%s **%s** against **%s** (%s)", pv.Scheme.Emoji, pv.Leader.FullName(), pv.targetName(), state))
		if len(btns) < 4 {
			btns = append(btns, withEmoji(button(truncate("Against "+pv.targetName(), 80), fmt.Sprintf("consp:plot:%d", k.ID), discordgo.PrimaryButton, false), pv.Scheme.Emoji))
		}
	}
	desc := "Nobody has asked you into a plot."
	if len(lines) > 0 {
		desc = joinLines(lines, 3000)
	}
	btns = append(btns, backTo("consp:home"))
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "🤝 Plots you're in", Description: withNote(note, desc), Color: colorSecret}},
		Components: []discordgo.MessageComponent{row(btns...)},
	}
}

func (b *Bot) betrayPlot(ctx context.Context, i *discordgo.InteractionCreate, pv plotView, c store.Character, now time.Time) {
	out := store.Outcome{Denunciations: []store.Denunciation{plotReport(pv, c.ID, pv.Leader)}}
	switch {
	case pv.Target != nil:
		out.Denunciations = append(out.Denunciations, plotReport(pv, pv.Target.ID, pv.Leader))
	case pv.SenTgt != nil:
		out.Opinions = []game.OpinionChange{
			{SenatorID: pv.SenTgt.ID, CharacterID: pv.Leader.ID, Delta: senTargetLeaderOpinion},
			{SenatorID: pv.SenTgt.ID, CharacterID: c.ID, Delta: betrayOpinion},
		}
	}
	if err := b.store.EndConspiracy(ctx, pv.K.ID, out, now); err != nil {
		b.update(i, noteView("That plot is over.", colorMuted))
		return
	}
	if pv.Target != nil {
		b.dmNote(pv.Target.UserID, "🐍 A conspiracy betrayed",
			fmt.Sprintf("**%s** has told you everything: **%s** was plotting to %s you. The plot is broken, and they're filed in your dossier.",
				c.FullName(), pv.Leader.FullName(), pv.Scheme.Verb()), colorBad)
	}
	b.dmNote(pv.Leader.UserID, "🐍 Betrayed", fmt.Sprintf("Someone has told **%s** everything. Your plot is broken, and they know it was you.", pv.targetName()), colorBad)
	for _, m := range pv.Members {
		if m.ID != c.ID {
			b.dmNote(m.UserID, "🐍 Betrayed", fmt.Sprintf("The plot against **%s** has been betrayed. It's over.", pv.targetName()), colorBad)
		}
	}
	b.update(i, noteView(fmt.Sprintf("You've told **%s** everything. **%s** is filed in your dossier too.", pv.targetName(), pv.Leader.FullName()), colorSecret))
	b.leak(ctx, c, game.SecretBetrayal(c.FullName(), targetIDOf(pv), pv.targetName(), pv.Leader.FullName()))
}

func targetIDOf(pv plotView) int64 {
	if pv.Target != nil {
		return pv.Target.ID
	}
	return 0
}

// ---- each game day ----

// conspiracyTick ripens every plot whose day has come, and rolls whether
// its target finds out.
func (b *Bot) conspiracyTick(ctx context.Context, now time.Time) {
	due, err := b.store.DueConspiracies(ctx, now)
	if err != nil {
		slog.Error("due conspiracies", "err", err)
		return
	}
	for _, k := range due {
		func() {
			defer recoverPanic("conspiracy day", "id", k.ID)
			b.plotDay(ctx, k, now)
		}()
	}
}

func (b *Bot) plotDay(ctx context.Context, k store.Conspiracy, now time.Time) {
	next := k.NextStepAt.Add(b.t.Day)
	if next.Before(now) {
		next = now.Add(b.t.Day)
	}
	pv, err := b.loadPlot(ctx, k)
	if err != nil {
		// The target is gone (a senator replaced): the plot falls apart.
		_ = b.store.StepConspiracy(ctx, k, k.Progress, k.Warned, next, true, store.Outcome{}, now)
		return
	}
	plot := pv.rules(b.targetSpies(ctx, pv))
	res := game.StepDay(b.rng, plot, pv.defender())
	b.plotFound(ctx, pv, res, func(warned game.Discovery, broken bool, out store.Outcome) error {
		return b.store.StepConspiracy(ctx, k, res.Progress, warned, next, broken, out, now)
	})
}

// plotFound applies what a target learned of a plot (through save) and
// tells everyone who needs telling.
func (b *Bot) plotFound(ctx context.Context, pv plotView, res game.DayResult, save func(game.Discovery, bool, store.Outcome) error) {
	warned := max(pv.K.Warned, res.Found)
	if res.Found == game.Unsuspected {
		if err := save(warned, false, store.Outcome{}); err != nil && !errors.Is(err, store.ErrEnded) {
			slog.Error("plot day", "err", err)
		}
		return
	}
	named := pv.Leader
	if res.Found == game.MemberNamed && res.Named >= 0 && res.Named < len(pv.Members) {
		named = pv.Members[res.Named]
	}
	broken := res.Found == game.LeaderNamed
	var out store.Outcome
	switch {
	case pv.Target != nil && res.Found >= game.MemberNamed:
		out.Denunciations = []store.Denunciation{plotReport(pv, pv.Target.ID, named)}
	case pv.SenTgt != nil && res.Found == game.MemberNamed:
		out.Opinions = []game.OpinionChange{{SenatorID: pv.SenTgt.ID, CharacterID: named.ID, Delta: senTargetNamedOpinion}}
	case pv.SenTgt != nil && res.Found == game.LeaderNamed:
		out.Opinions = []game.OpinionChange{{SenatorID: pv.SenTgt.ID, CharacterID: named.ID, Delta: senTargetLeaderOpinion}}
	}
	if err := save(warned, broken, out); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("plot found", "err", err)
		}
		return
	}
	verb := pv.Scheme.Verb()
	if pv.Target != nil {
		var text string
		switch res.Found {
		case game.Suspected:
			text = fmt.Sprintf("A servant overheard something they shouldn't have. Someone is plotting to %s you. You don't know who, yet.", verb)
		case game.MemberNamed:
			text = fmt.Sprintf("Your people have found one of the plotters: **%s** is part of a conspiracy to %s you. They're filed in your dossier.", named.FullName(), verb)
		case game.LeaderNamed:
			text = fmt.Sprintf("You've uncovered the whole plot: **%s** was planning to %s you. It's broken now, and they're filed in your dossier.", named.FullName(), verb)
		}
		b.dmNote(pv.Target.UserID, "🕸️ A plot against you", text, colorBad)
	}
	if broken {
		b.dmNote(pv.Leader.UserID, "🕸️ Your plot is uncovered",
			fmt.Sprintf("**%s** has found out everything, and knows you were behind it. The plot is broken.", pv.targetName()), colorBad)
		for _, m := range pv.Members {
			b.dmNote(m.UserID, "🕸️ The plot is uncovered", fmt.Sprintf("**%s** found out about the plot against them. It's over.", pv.targetName()), colorBad)
		}
		if pv.SenTgt != nil {
			b.post(ctx, pv.K.GuildID, &discordgo.MessageEmbed{Title: "🕸️ A plot uncovered", Color: colorBad,
				Description: fmt.Sprintf("**Senator %s** has uncovered a plot to %s them, and names **%s** as its author.", pv.SenTgt.Name, verb, pv.Leader.FullName())})
		}
	}
}

// rootOutPlots gives a player searching their household a chance to find
// the plots against them.
func (b *Bot) rootOutPlots(ctx context.Context, c store.Character) {
	ks, err := b.store.ConspiraciesAgainst(ctx, c.ID)
	if err != nil {
		slog.Error("conspiracies against", "err", err)
		return
	}
	now := time.Now()
	for _, k := range ks {
		pv, err := b.loadPlot(ctx, k)
		if err != nil {
			continue
		}
		res := game.Uncover(b.rng, pv.rules(b.targetSpies(ctx, pv)), pv.defender())
		b.plotFound(ctx, pv, res, func(warned game.Discovery, broken bool, out store.Outcome) error {
			if broken {
				return b.store.EndConspiracy(ctx, k.ID, out, now)
			}
			return b.store.WarnPlot(ctx, k.ID, warned, out, now)
		})
	}
}

// resolvePlotExpired is a plot that ran out of time.
func (b *Bot) resolvePlotExpired(ctx context.Context, a store.Activity, now time.Time) {
	k, err := b.store.GetConspiracy(ctx, a.ID)
	if err != nil {
		_ = b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now)
		return
	}
	pv, perr := b.loadPlot(ctx, k)
	if err := b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now); err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("plot expired", "err", err)
		}
		return
	}
	if perr != nil {
		return
	}
	text := fmt.Sprintf("You waited too long. People got nervous, letters went unanswered, and the plot against **%s** fell apart.", pv.targetName())
	b.dmNote(pv.Leader.UserID, "🕯️ Your plot fell apart", text, colorMuted)
	for _, m := range pv.Members {
		b.dmNote(m.UserID, "🕯️ A plot fell apart", fmt.Sprintf("The plot against **%s** came to nothing.", pv.targetName()), colorMuted)
	}
}

// ---- striking ----

func (b *Bot) onStrike(ctx context.Context, i *discordgo.InteractionCreate, c store.Character) {
	k, err := b.store.LedConspiracy(ctx, c.ID)
	if err != nil {
		b.update(i, b.conspiracyPanel(ctx, c, ""))
		return
	}
	if busy := b.spyBusy(ctx, c); busy != "" {
		b.update(i, b.conspiracyPanel(ctx, c, ""))
		return
	}
	pv, err := b.loadPlot(ctx, k)
	if err != nil {
		b.fail(i, err)
		return
	}
	now := time.Now()
	ends := now.Add(b.t.Strike)
	if _, err := b.store.StartStrike(ctx, k, ends, now); err != nil {
		b.update(i, b.conspiracyPanel(ctx, c, err.Error()))
		return
	}
	b.update(i, startedView("🗡️ The plot strikes", fmt.Sprintf("Your people move against **%s**. I'll DM you how it went.", pv.targetName()),
		fact("You'll know", relTime(ends))))
}

func (b *Bot) resolveStrike(ctx context.Context, a store.Activity, now time.Time) {
	k, err := b.store.GetConspiracy(ctx, a.TargetID)
	if err != nil || k.Resolved {
		b.strikeFizzles(ctx, a, now)
		return
	}
	pv, err := b.loadPlot(ctx, k)
	if err != nil {
		b.strikeFizzles(ctx, a, now)
		return
	}
	plot := pv.rules(b.targetSpies(ctx, pv))
	res := game.ResolveStrike(b.rng, plot, pv.defender(), pv.Senate)
	out := store.Outcome{Deltas: res.Deltas, Opinions: res.Opinions}
	addDelta := func(id int64, d game.Delta) {
		cur := out.Deltas[id]
		cur.Merge(d)
		out.Deltas[id] = cur
	}
	var (
		heir     game.Heir
		newSen   game.Senator
		stolen   string
		extra    string
		forgery  game.Forgery
		leaderID = pv.Leader.ID
	)
	if res.Success {
		switch {
		case pv.Scheme.Key == game.SchemeAssassinate && pv.Target != nil:
			t := pv.Target
			heir = game.Inherit(game.PickHeirName(b.rng, t.Name), t.Stats, t.Renown, t.Influence, t.Wealth)
			out.Deaths = []store.Death{{CharacterID: t.ID, Heir: heir}}
			extra = fmt.Sprintf("\n\nTheir heir, **%s %s**, takes up the house.", heir.Name, t.FamilyName)
		case pv.SenTgt != nil:
			newSen = game.ReplaceSenator(b.rng, pv.Senate, pv.SenTgt.ID)
			out.Senators = []game.Senator{newSen}
			extra = fmt.Sprintf("\n\n**Senator %s** takes the empty seat.", newSen.Name)
			if pv.Scheme.Key == game.SchemeDisgrace {
				addDelta(leaderID, game.DisgraceReward(pv.Leader.Influence))
			}
		case pv.Scheme.Key == game.SchemeFrame:
			forgery = game.PickForgery(b.rng)
			out.Denunciations = []store.Denunciation{{GuildID: k.GuildID, OwnerID: leaderID, ActorID: pv.Target.ID,
				Crime: forgery.Crime, Detail: fmt.Sprintf(forgery.Detail, pv.Target.FullName())}}
			extra = fmt.Sprintf("\n\n📁 The evidence says **%s**. It's filed in your dossier: denounce them or whisper it whenever it suits you.", game.MisdeedWord(forgery.Crime))
		case pv.Scheme.Key == game.SchemeSteal:
			t := pv.Target
			if h, ok := t.Holding(k.TargetItem); ok && k.TargetItem != 0 {
				if it, ok := h.Item(); ok && !it.IsProperty() {
					out.Transfers = []store.ItemTransfer{{HoldingID: h.ID, From: t.ID, To: leaderID}}
					stolen = "**" + it.Name + "**"
				}
			}
			if stolen == "" {
				gold := game.CofferTake(t.Wealth)
				addDelta(t.ID, game.Delta{Wealth: -gold})
				addDelta(leaderID, game.Delta{Wealth: gold})
				stolen = fmt.Sprintf("**%d gold**", gold)
			}
			extra = fmt.Sprintf("\n\nThey came back with %s.", stolen)
		}
	}
	if res.Wounded {
		out.Conditions = b.newConditions([]game.ConditionChange{{CharacterID: pv.Target.ID, Key: game.CondWounded}}, now)
	}
	if err := b.store.ResolveStrike(ctx, a.ID, k.ID, out, now); err != nil {
		if errors.Is(err, store.ErrEnded) {
			b.strikeFizzles(ctx, a, now)
		} else {
			slog.Error("resolve strike", "err", err)
		}
		return
	}

	color := resultColor(res.Success)
	b.dmNote(pv.Leader.UserID, pv.Scheme.Emoji+" Your plot struck", withDelta(res.Text+extra, out.Deltas[leaderID]), color)
	memberText := fmt.Sprintf("The plot against **%s** failed. Lie low.", pv.targetName())
	if res.Success {
		memberText = fmt.Sprintf("The plot against **%s** worked.%s", pv.targetName(), extra)
	}
	for _, m := range pv.Members {
		b.dmNote(m.UserID, pv.Scheme.Emoji+" The plot struck", memberText, color)
	}
	if res.Exposed {
		b.post(ctx, k.GuildID, &discordgo.MessageEmbed{Title: "📢 A plot foiled", Description: res.Public, Color: colorBad})
	}
	switch {
	case res.Success:
		b.afterPlotSuccess(ctx, pv, heir, newSen, stolen)
		b.leak(ctx, pv.Leader, game.SecretPlotDone(pv.Scheme.Crime, pv.Leader.FullName(), pv.Scheme.Verb(), pv.targetName()))
	case pv.Scheme.Key == game.SchemeAssassinate && pv.Target != nil && !res.Exposed:
		text := "Someone tried to kill you. The blade missed. You don't know who sent it."
		if res.Wounded {
			text = "Someone tried to kill you. You'll live, but you're wounded. You don't know who sent the blade."
		}
		b.dmNote(pv.Target.UserID, "🗡️ An attempt on your life", text, colorBad)
	}
}

// strikeFizzles ends a strike whose plot was already over.
func (b *Bot) strikeFizzles(ctx context.Context, a store.Activity, now time.Time) {
	if err := b.store.ApplyResult(ctx, a.ID, store.Outcome{}, now); err != nil {
		return
	}
	if c, err := b.store.GetCharacterByID(ctx, a.HostID); err == nil {
		b.dmNote(c.UserID, "🕯️ Too late", "By the time your people moved, the plot was already over.", colorMuted)
	}
}

// afterPlotSuccess tells the target and the court what happened.
func (b *Bot) afterPlotSuccess(ctx context.Context, pv plotView, heir game.Heir, newSen game.Senator, stolen string) {
	guild := pv.K.GuildID
	switch {
	case pv.Scheme.Key == game.SchemeAssassinate && pv.Target != nil:
		t := pv.Target
		_, _ = b.dm(t.UserID, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{{
				Title: "💀 " + t.FullName() + " is dead",
				Description: fmt.Sprintf("You were murdered. Nobody saw the hand that did it.\n\nYour house goes on. **%s %s**, your heir, takes up your seat with your estate, your monuments and your ventures, "+
					"but only some of your gold, your renown and your friends. Spies you had now serve them, at half strength.", heir.Name, t.FamilyName),
				Color:  colorNotice,
				Footer: footer(fmt.Sprintf("Your heir is called %s until you name them.", heir.Name)),
			}},
			Components: []discordgo.MessageComponent{row(withEmoji(button("Name your heir", fmt.Sprintf("heir:name:%d", t.ID), discordgo.PrimaryButton, false), "👶"))},
		})
		b.post(ctx, guild, &discordgo.MessageEmbed{Title: "💀 A death at court", Color: colorBad,
			Description: fmt.Sprintf("**%s** of %s is dead, murdered by an unknown hand. Their heir, **%s %s**, takes up the house.", t.FullName(), t.House(), heir.Name, t.FamilyName)})
	case pv.Scheme.Key == game.SchemeAssassinate:
		b.post(ctx, guild, &discordgo.MessageEmbed{Title: "💀 A senator is dead", Color: colorSenate,
			Description: fmt.Sprintf("**Senator %s** was found dead this morning. Nobody knows who did it. **%s** takes the empty seat (%s).", pv.SenTgt.Name, newSen.Name, newSen.TraitNames())})
	case pv.Scheme.Key == game.SchemeDisgrace:
		b.post(ctx, guild, &discordgo.MessageEmbed{Title: "📜 A senator disgraced", Color: colorSenate,
			Description: fmt.Sprintf("A scandal has swept **Senator %s** from the Senate. **%s** takes the empty seat (%s).", pv.SenTgt.Name, newSen.Name, newSen.TraitNames())})
	case pv.Scheme.Key == game.SchemeSteal:
		b.dmNote(pv.Target.UserID, "💰 Burglars", fmt.Sprintf("Thieves broke into your house in the night and took %s. Nobody saw who sent them.", stolen), colorBad)
	}
}

// ---- the heir ----

func init() {
	componentRoutes["heir"] = (*Bot).onHeir
	modalRoutes["heir"] = (*Bot).onHeirModal
}

// Heir custom IDs:
//   heir:name:<character>   open the naming modal (from the death DM)
//   modal heir:name:<character>

func (b *Bot) heirFor(ctx context.Context, i *discordgo.InteractionCreate, id int64) (store.Character, bool) {
	c, err := b.store.GetCharacterByID(ctx, id)
	if err != nil || c.UserID != userOf(i).ID {
		b.reply(i, "That isn't your house.")
		return c, false
	}
	if unnamed, err := b.store.HeirUnnamed(ctx, c.ID); err != nil || !unnamed {
		b.reply(i, fmt.Sprintf("Your heir already has a name: **%s**.", c.FullName()))
		return c, false
	}
	return c, true
}

func (b *Bot) onHeir(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if parts[0] != "name" || len(parts) < 2 {
		return
	}
	c, ok := b.heirFor(ctx, i, atoi(parts[1]))
	if !ok {
		return
	}
	if err := b.s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: fmt.Sprintf("heir:name:%d", c.ID), Title: "Name your heir",
			Components: []discordgo.MessageComponent{row(discordgo.TextInput{
				CustomID: "name", Label: "Their name (House " + truncate(c.FamilyName, 20) + ")", Style: discordgo.TextInputShort,
				Value: c.Name, Required: true, MinLength: 1, MaxLength: game.MaxNameLen,
			})},
		},
	}); err != nil {
		slog.Error("heir modal", "err", err)
	}
}

func (b *Bot) onHeirModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if parts[0] != "name" || len(parts) < 2 {
		return
	}
	c, ok := b.heirFor(ctx, i, atoi(parts[1]))
	if !ok {
		return
	}
	name := strings.TrimSpace(modalValues(i.ModalSubmitData())["name"])
	if name == "" || len(name) > game.MaxNameLen {
		b.reply(i, "That name won't do. Try another.")
		return
	}
	if err := b.store.NameHeir(ctx, c.ID, name); err != nil {
		b.reply(i, "Your heir already has a name.")
		return
	}
	b.reply(i, fmt.Sprintf("**%s %s** takes up the house. Long may they live.", name, c.FamilyName))
	b.post(ctx, c.GuildID, &discordgo.MessageEmbed{Title: "👶 An heir is named", Color: colorPanel,
		Description: fmt.Sprintf("The heir of %s is **%s %s**.", c.House(), name, c.FamilyName)})
}
