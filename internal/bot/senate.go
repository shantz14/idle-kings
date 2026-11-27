package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	componentRoutes["senate"] = (*Bot).onSenate
}

// Senate custom IDs:
//   senate:vote:<matter>:<option>
//   senate:bribe:<matter>                     pick a senator to bribe
//   senate:bribewho:<matter>                  (select) senator chosen
//   senate:pay:<matter>:<senator>:<option>    pay the bribe
//   senate:roster                             who sits in the Senate

func (b *Bot) onSenate(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	switch {
	case parts[0] == "roster":
		b.onRoster(ctx, i)
	case parts[0] == "vote" && len(parts) > 2:
		b.onVote(ctx, i, atoi(parts[1]), parts[2])
	case parts[0] == "bribe" && len(parts) > 1:
		b.onBribe(ctx, i, atoi(parts[1]))
	case parts[0] == "bribewho" && len(parts) > 1:
		if vals := i.MessageComponentData().Values; len(vals) > 0 {
			b.onBribeWho(ctx, i, atoi(parts[1]), atoi(vals[0]))
		}
	case parts[0] == "pay" && len(parts) > 3:
		b.onPay(ctx, i, atoi(parts[1]), atoi(parts[2]), parts[3])
	default:
		b.onVoteTrade(ctx, i, parts)
	}
}

// ---- the daily cycle ----

// ensureRealm gives a running court its realm and senate (the standard
// Senate, if /start didn't already make one).
func (b *Bot) ensureRealm(ctx context.Context, guildID string, now time.Time) (store.RealmState, error) {
	return b.ensureRealmWith(ctx, guildID, now, game.SenateStandard)
}

// ensureRealmWith is ensureRealm with the admin's choice of Senate. It only
// matters the first time, when the realm is made.
func (b *Bot) ensureRealmWith(ctx context.Context, guildID string, now time.Time, kind game.SenateKind) (store.RealmState, error) {
	st, err := b.store.GetRealm(ctx, guildID)
	if !errors.Is(err, store.ErrNotFound) {
		return st, err
	}
	senate := game.NewSenate(b.rng, kind)
	if err := b.store.CreateRealm(ctx, guildID, game.StartingRealm(), senate, now.Add(b.senate.FirstDelay)); err != nil {
		return st, err
	}
	return b.store.GetRealm(ctx, guildID)
}

func (b *Bot) senateTick(ctx context.Context, now time.Time) {
	courts, err := b.store.RunningCourts(ctx)
	if err != nil {
		slog.Error("running courts", "err", err)
		return
	}
	for _, court := range courts {
		st, err := b.ensureRealm(ctx, court.GuildID, now)
		if err != nil {
			slog.Error("ensure realm", "guild", court.GuildID, "err", err)
			continue
		}
		if !st.Alive() {
			continue
		}
		open, err := b.store.OpenMatters(ctx, court.GuildID)
		if err != nil {
			slog.Error("open matters", "guild", court.GuildID, "err", err)
			continue
		}
		var day, petition bool
		for _, m := range open {
			if m.PetitionerID != 0 {
				petition = true
			} else {
				day = true
			}
		}
		// A petition runs alongside the day's matter, one at a time.
		if !petition {
			b.openPetition(ctx, court, st, now)
		}
		if !day && !now.Before(st.NextMatterAt) {
			b.openMatter(ctx, court, st, now)
		}
	}

	closed, err := b.store.ClosedMatters(ctx, now)
	if err != nil {
		slog.Error("closed matters", "err", err)
		return
	}
	for _, m := range closed {
		b.resolveMatter(ctx, m, now)
	}
}

func (b *Bot) openMatter(ctx context.Context, court store.Court, st store.RealmState, now time.Time) {
	recent, err := b.store.RecentEventKeys(ctx, court.GuildID, game.MatterCooldown)
	if err != nil {
		slog.Error("recent matters", "err", err)
		return
	}
	var ev game.MatterEvent
	qid, key, err := b.store.NextQueued(ctx, court.GuildID, now)
	switch {
	case err != nil:
		qid = 0
	case !game.FollowUpAllowed(key, recent):
		// Never the same matter twice in a row: it waits a day.
		if err := b.store.DeferQueued(ctx, qid, now.Add(b.senate.Interval)); err != nil {
			slog.Error("defer follow-up", "err", err)
		}
		qid = 0
	default:
		ev, _ = game.MatterByKey(key)
	}
	if ev.Key == "" {
		queued, err := b.store.QueuedEvents(ctx, court.GuildID)
		if err != nil {
			slog.Error("queued matters", "err", err)
		}
		held, err := b.store.MattersHeld(ctx, court.GuildID)
		if err != nil {
			slog.Error("matters held", "err", err)
		}
		ev = game.PickMatterAvoiding(b.rng, st.Trackers, recent, game.ActiveArcs(queued), st.Laws, held)
	}
	closes, next := now.Add(b.senate.VoteWindow), now.Add(b.senate.Interval)
	if ev.Auto { // a bill comes due: no vote, and it doesn't take the day's matter
		closes, next = now, now
	}
	m, err := b.store.CreateMatter(ctx, court.GuildID, ev.Key, qid, now, closes, next)
	if err != nil {
		slog.Error("create matter", "err", err)
		return
	}
	if !ev.Auto {
		b.announceMatter(ctx, court, m, ev, st)
	}
}

// openPetition puts the next petitioned matter on the floor, as an extra
// vote alongside the day's matter: the Senate's schedule doesn't move.
func (b *Bot) openPetition(ctx context.Context, court store.Court, st store.RealmState, now time.Time) {
	qid, key, err := b.store.NextPetition(ctx, court.GuildID, now)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("next petition", "err", err)
		}
		return
	}
	m, err := b.store.CreateMatter(ctx, court.GuildID, key, qid, now, now.Add(b.senate.VoteWindow), st.NextMatterAt)
	if err != nil {
		slog.Error("create petition matter", "err", err)
		return
	}
	if ev, ok := game.MatterByKey(key); ok { // an unknown one is closed with no effect
		b.announceMatter(ctx, court, m, ev, st)
	}
}

func (b *Bot) announceMatter(ctx context.Context, court store.Court, m store.Matter, ev game.MatterEvent, st store.RealmState) {
	if court.AnnouncementsChannelID == "" {
		return
	}
	msg, err := b.s.ChannelMessageSendComplex(court.AnnouncementsChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{b.matterEmbedFor(ctx, m, ev, st.Trackers, st.Laws)},
		Components: matterButtons(m, ev, true, false),
	})
	if err != nil {
		slog.Warn("announce matter", "err", err)
		return
	}
	_ = b.store.SetMatterMessage(ctx, m.ID, msg.ChannelID, msg.ID)
}

func (b *Bot) resolveMatter(ctx context.Context, m store.Matter, now time.Time) {
	ev, ok := game.MatterByKey(m.EventKey)
	st, err := b.store.GetRealm(ctx, m.GuildID)
	if err != nil {
		slog.Error("realm", "err", err)
		return
	}
	if !ok { // event removed from the catalogue; close it with no effect
		_ = b.store.ResolveMatter(ctx, m, store.MatterResolution{Realm: st.Trackers, Laws: st.Laws}, now)
		return
	}
	senators, err1 := b.store.Senators(ctx, m.GuildID)
	bribes, err2 := b.store.Bribes(ctx, m.ID)
	votes, err3 := b.store.Votes(ctx, m.ID)
	chars, err4 := b.store.CharactersInCourt(ctx, m.GuildID)
	ops, err5 := b.store.CourtOpinions(ctx, m.GuildID)
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		slog.Error("load matter", "err", err)
		return
	}

	in := game.MatterInput{MatterID: m.ID, Event: ev, Realm: st.Trackers, Sectors: st.Sectors, Senators: senators, Bribes: map[int64]string{}, Opinions: ops,
		Laws: st.Laws, Now: now, Petitioner: m.PetitionerID}
	for sid, br := range bribes {
		in.Bribes[sid] = br.Option
	}
	users := map[int64]string{}
	for _, c := range chars {
		users[c.ID] = c.UserID
		in.Players = append(in.Players, game.Player{Person: c.Person(), Role: c.Role, Vote: votes[c.ID], Weight: c.VoteWeight(), Items: c.Items})
	}
	res := game.ResolveMatter(b.rng, in)
	ventureNotices(chars, res)

	decay := game.OpinionDecay
	if ev.Auto || m.PetitionerID != 0 { // not a Senate day
		decay = 0
	}
	err = b.store.ResolveMatter(ctx, m, store.MatterResolution{
		Summary: map[string]any{"winner": res.Winner.Key, "succeeded": res.Succeeded, "tally": res.Tally},
		Realm:   res.Realm, Sectors: res.Sectors, Laws: res.Laws, Fallen: res.Fallen, FallenReason: res.FallenReason,
		Deltas: res.Deltas, FollowUps: res.FollowUps, DayLength: b.senate.Interval,
		Opinions: res.Opinions, OpinionDecay: decay,
	}, now)
	if err != nil {
		if !errors.Is(err, store.ErrEnded) {
			slog.Error("resolve matter", "err", err)
		}
		return
	}

	for id, lines := range res.Notices {
		if uid, ok := users[id]; ok {
			_, _ = b.dm(uid, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{{
				Title: titled(emojiSenate, "The Senate decided: "+ev.Title), Description: strings.Join(lines, "\n"), Color: colorSenate,
			}}})
		}
	}

	channel := m.ChannelID
	if channel == "" {
		if court, err := b.store.GetCourt(ctx, m.GuildID); err == nil {
			channel = court.AnnouncementsChannelID
		}
	}
	if channel == "" {
		return
	}
	result := decisionEmbed(ev, res)
	if ev.Auto {
		result = billEmbed(ev, res)
	}
	_, _ = b.s.ChannelMessageSendEmbed(channel, result)
	if m.MessageID != "" {
		closed := b.matterEmbedFor(ctx, m, ev, st.Trackers, nil)
		closed.Color = colorMuted
		closed.Fields = append(optionFields(ev, res.Winner.Key), &discordgo.MessageEmbedField{Name: "✅ Decided", Value: "**" + res.Winner.Label + "**"})
		embeds := []*discordgo.MessageEmbed{closed}
		empty := []discordgo.MessageComponent{}
		_, _ = b.s.ChannelMessageEditComplex(&discordgo.MessageEdit{Channel: m.ChannelID, ID: m.MessageID, Embeds: &embeds, Components: &empty})
	}
	if res.Fallen {
		b.announceFall(ctx, m.GuildID, channel, res.FallenReason)
		return
	}
	b.postWarnings(channel, game.RealmWarnings(st.Trackers, res.Realm))
}

// billEmbed announces an Auto event: a bill coming due, with no vote.
func billEmbed(ev game.MatterEvent, res game.MatterResult) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{
		Title:       "🧾 The bill comes due: " + ev.Title,
		Description: ev.Prompt + "\n\n" + res.Outcome.Text,
		Color:       colorBad,
	}
	if f := changesField(res); f != nil {
		e.Fields = append(e.Fields, f)
	}
	e.Fields = append(e.Fields, realmField(res.Realm))
	e.Footer = footer("No vote: an old decision's cost has come due.")
	return e
}

// changesField is what a decision did to the realm, its industries and its
// laws, or nil if nothing.
func changesField(res game.MatterResult) *discordgo.MessageEmbedField {
	var parts []string
	for _, s := range []string{res.Outcome.Realm.Describe(), game.DescribeChanges(res.Outcome.Sectors), res.LawChanges.Describe()} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return &discordgo.MessageEmbedField{Name: "📋 What changed", Value: truncate(strings.Join(parts, "\n"), 1024)}
}

// realmField is the realm's trackers, for the end of a Senate embed.
func realmField(r game.Realm) *discordgo.MessageEmbedField {
	return &discordgo.MessageEmbedField{Name: emojiRealm + " The realm", Value: trackersLine(r)}
}

// ---- panels ----

func trackersLine(r game.Realm) string {
	var parts []string
	for _, t := range game.Trackers {
		parts = append(parts, fmt.Sprintf("%s %s **%s**", t.Emoji, t.Name, game.Num(r[t.Key])))
	}
	return strings.Join(parts, "  ·  ")
}

// matterEmbedFor is matterEmbed with the petitioner's name on a petition,
// and a warning for any option that would make a law lapse under laws (nil
// for a decided matter).
func (b *Bot) matterEmbedFor(ctx context.Context, m store.Matter, ev game.MatterEvent, r game.Realm, laws game.Laws) *discordgo.MessageEmbed {
	embed := matterEmbed(m, ev, r)
	if f := lapseField(ev, laws); f != nil {
		embed.Fields = append([]*discordgo.MessageEmbedField{f}, embed.Fields...)
	}
	if m.PetitionerID != 0 {
		if p, err := b.store.GetCharacterByID(ctx, m.PetitionerID); err == nil {
			embed.Footer = footer("📜 Brought before the Senate by the petition of " + p.FullName() + ", House " + p.FamilyName)
		}
	}
	return embed
}

// lapseField warns which law each option would push out when every law slot
// is taken, or nil if none would.
func lapseField(ev game.MatterEvent, laws game.Laws) *discordgo.MessageEmbedField {
	var lines []string
	for _, o := range ev.Options {
		seen := map[game.LawKey]bool{}
		var names []string
		for _, out := range []game.MatterOutcome{o.Success, o.Failure} {
			_, ch := laws.Apply(out.Enact, out.Repeal, time.Time{})
			for _, k := range ch.Lapsed {
				if !seen[k] {
					seen[k] = true
					names = append(names, "**"+lawName(k)+"**")
				}
			}
		}
		if len(names) > 0 {
			lines = append(lines, fmt.Sprintf("• If *%s* wins, %s lapses to make room.", o.Label, strings.Join(names, " and ")))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return &discordgo.MessageEmbedField{Name: fmt.Sprintf("⚠️ All %d laws stand", game.MaxLaws), Value: strings.Join(lines, "\n")}
}

// lawName is "🪙 Debased Coinage".
func lawName(k game.LawKey) string {
	d, _ := game.LawByKey(k)
	return d.Emoji + " " + d.Name
}

func matterEmbed(m store.Matter, ev game.MatterEvent, r game.Realm) *discordgo.MessageEmbed {
	var sb strings.Builder
	if m.PetitionerID != 0 {
		sb.WriteString("*📜 A petition, alongside today's business*\n\n")
	} else if l := ev.ArcLabel(); l != "" {
		sb.WriteString("*" + l + "*\n\n")
	}
	sb.WriteString(ev.Prompt)
	title := titled(emojiSenate, "The Senate convenes: "+ev.Title)
	if m.PetitionerID != 0 {
		title = "📜 A petition before the Senate: " + ev.Title
	}
	fields := optionFields(ev, "")
	fields = append(fields,
		realmField(r),
		&discordgo.MessageEmbedField{Name: "⏳ Voting closes", Value: fmt.Sprintf("%s · ties go to *%s*", relTime(m.ClosesAt), optionLabel(ev, ev.Default))},
	)
	return &discordgo.MessageEmbed{
		Title:       title,
		Description: sb.String(),
		Color:       colorSenate,
		Fields:      fields,
		Footer:      footer(emojiVote + " Your vote counts in proportion to your influence."),
	}
}

// optionFields is one field per option: its hint, and how it moves the
// industries. winner, if set, is marked.
func optionFields(ev game.MatterEvent, winner string) []*discordgo.MessageEmbedField {
	var out []*discordgo.MessageEmbedField
	for n, o := range ev.Options {
		val := o.Hint
		if s := game.DescribeArrows(game.SectorPreview(o)); s != "" {
			val += "\n*" + s + "*"
		}
		if strings.TrimSpace(val) == "" {
			val = "—"
		}
		name := fmt.Sprintf("%s %s", optionMark(n), o.Label)
		if winner != "" && o.Key == winner {
			name = "🏆 " + o.Label
		}
		out = append(out, &discordgo.MessageEmbedField{Name: truncate(name, 256), Value: truncate(val, 1024)})
	}
	return out
}

// optionMark numbers an option: 1️⃣, 2️⃣, …
func optionMark(n int) string {
	marks := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣"}
	if n < len(marks) {
		return marks[n]
	}
	return "▫️"
}

func optionLabel(ev game.MatterEvent, key string) string {
	if o, ok := ev.Option(key); ok {
		return o.Label
	}
	return key
}

// matterButtons are a matter's vote and deal buttons. roster and back add
// the shared buttons, which a panel with two matters shows only once.
func matterButtons(m store.Matter, ev game.MatterEvent, roster, back bool) []discordgo.MessageComponent {
	var votes []discordgo.MessageComponent
	for n, o := range ev.Options {
		votes = append(votes, withEmoji(button(o.Label, fmt.Sprintf("senate:vote:%d:%s", m.ID, o.Key), discordgo.PrimaryButton, false), optionMark(n)))
	}
	extra := []discordgo.MessageComponent{
		withEmoji(button("Bribe a senator", fmt.Sprintf("senate:bribe:%d", m.ID), discordgo.SecondaryButton, false), "💰"),
		withEmoji(button("Sell your vote", fmt.Sprintf("senate:sell:%d", m.ID), discordgo.SecondaryButton, false), "🪙"),
		withEmoji(button("Buy a vote", fmt.Sprintf("senate:buy:%d", m.ID), discordgo.SecondaryButton, false), emojiDeal),
	}
	if roster {
		extra = append(extra, withEmoji(button("The Senate", "senate:roster", discordgo.SecondaryButton, false), emojiSenate))
	}
	if back {
		extra = append(extra, backTo("menu:home"))
	}
	return []discordgo.MessageComponent{row(votes...), row(extra...)}
}

func decisionEmbed(ev game.MatterEvent, res game.MatterResult) *discordgo.MessageEmbed {
	desc := fmt.Sprintf("🏆 **%s**\n%s", res.Winner.Label, res.Outcome.Text)
	var tally strings.Builder
	for _, o := range ev.Options {
		mark := "▫️"
		if o.Key == res.Winner.Key {
			mark = "🏆"
		}
		if res.Secret { // the Secret Ballot: only the urns' weight is counted out
			fmt.Fprintf(&tally, "%s **%s** · %s\n", mark, o.Label, game.Num(res.Tally[o.Key]))
			continue
		}
		var names []string
		for _, v := range res.Votes {
			if v.Option == o.Key {
				names = append(names, v.Name)
			}
		}
		if len(names) == 0 {
			names = []string{"nobody"}
		}
		fmt.Fprintf(&tally, "%s **%s** · %s\n*%s*\n", mark, o.Label, game.Num(res.Tally[o.Key]), strings.Join(names, ", "))
	}
	votes := emojiVote + " Votes, by influence"
	if res.Secret {
		votes = emojiVote + " Votes, by secret ballot"
	}
	color := colorGood
	if !res.Succeeded {
		color = colorBad
	}
	fields := []*discordgo.MessageEmbedField{}
	if f := changesField(res); f != nil {
		fields = append(fields, f)
	}
	fields = append(fields, &discordgo.MessageEmbedField{Name: votes, Value: truncate(strings.TrimSpace(tally.String()), 1024)}, realmField(res.Realm))
	return &discordgo.MessageEmbed{
		Title:       titled(emojiSenate, "The Senate has decided: "+ev.Title),
		Description: desc,
		Color:       color,
		Fields:      fields,
	}
}

// senatePanel is the /senate view: the realm, and today's matter if any.
func (b *Bot) senatePanel(ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
	st, err := b.ensureRealm(ctx, c.GuildID, time.Now())
	if err != nil {
		return ephemeral(noteView(emojiWarn+" Something went wrong.", colorMuted))
	}
	realm := &discordgo.MessageEmbed{Title: titled(emojiRealm, "The realm"), Description: trackersLine(st.Trackers), Color: colorPanel,
		Fields: []*discordgo.MessageEmbedField{lawsField(st.Laws)}}
	if !st.Alive() {
		realm.Description += "\n\n🔥 **The realm has fallen.** " + st.FallenReason
		realm.Color = colorBad
	}
	embeds := []*discordgo.MessageEmbed{realm}
	var comps []discordgo.MessageComponent
	open, _ := b.store.OpenMatters(ctx, c.GuildID)
	open = slices.DeleteFunc(open, func(m store.Matter) bool { _, ok := game.MatterByKey(m.EventKey); return !ok })
	for i, m := range open {
		ev, _ := game.MatterByKey(m.EventKey)
		last := i == len(open)-1 // the shared buttons go on the last one only
		embeds = append(embeds, b.matterEmbedFor(ctx, m, ev, st.Trackers, st.Laws))
		comps = append(comps, matterButtons(m, ev, last, last)...)
	}
	if len(open) == 0 && st.Alive() {
		realm.Description += fmt.Sprintf("\n\n%s The Senate next convenes %s.", emojiSenate, relTime(st.NextMatterAt))
	}
	if comps == nil {
		comps = []discordgo.MessageComponent{row(
			withEmoji(button("The Senate", "senate:roster", discordgo.SecondaryButton, false), emojiSenate),
			backTo("menu:home"),
		)}
	}
	return ephemeral(&discordgo.InteractionResponseData{Embeds: embeds, Components: comps})
}

// senateTotal is the Senate's whole vote weight: every senator and player.
func (b *Bot) senateTotal(ctx context.Context, guildID string) float64 {
	sen, _ := b.store.Senators(ctx, guildID)
	chars, _ := b.store.CharactersInCourt(ctx, guildID)
	return senateTotalOf(sen, chars)
}

// senateTotalOf is senateTotal for senators and players already loaded.
func senateTotalOf(sen []game.Senator, chars []store.Character) float64 {
	total := 0.0
	for _, s := range sen {
		total += s.Influence
	}
	for _, c := range chars {
		total += c.VoteWeight()
	}
	return total
}

// percentOf is weight as a percentage of the Senate's total.
func percentOf(weight, total float64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * weight / total
}

// share is one vote weight's percentage of the Senate. In a loop, use
// senateTotal once and percentOf instead.
func (b *Bot) share(ctx context.Context, guildID string, weight float64) float64 {
	return percentOf(weight, b.senateTotal(ctx, guildID))
}

func (b *Bot) onRoster(ctx context.Context, i *discordgo.InteractionCreate) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	sen, err := b.store.Senators(ctx, c.GuildID)
	if err != nil {
		b.fail(i, err)
		return
	}
	chars, _ := b.store.CharactersInCourt(ctx, c.GuildID)
	sort.Slice(chars, func(a, b int) bool { return chars[a].VoteWeight() > chars[b].VoteWeight() })
	ops, _ := b.store.Opinions(ctx, c.ID)
	total := senateTotalOf(sen, chars)

	var sb strings.Builder
	sb.WriteString("**Senators** · their opinion of you\n")
	for _, s := range sen {
		fmt.Fprintf(&sb, "%s **%s** · %s%% of the vote\n*%s*\n", opinionTag(ops[s.ID]), s.Name,
			game.Num(percentOf(s.Influence, total)), s.TraitNames())
	}
	sb.WriteString("\n**Players**\n")
	for _, p := range chars {
		role, _ := game.RoleByKey(p.Role)
		fmt.Fprintf(&sb, "%s **%s** · %s · %s%% of the vote\n", p.Crest.Emoji(), p.FullName(), role.Name,
			game.Num(percentOf(p.VoteWeight(), total)))
	}
	var traits strings.Builder
	seen := map[game.Trait]bool{}
	for _, s := range sen {
		for _, t := range s.Traits {
			if !seen[t] {
				seen[t] = true
				ti, _ := game.TraitByKey(t)
				fmt.Fprintf(&traits, "• **%s**: %s\n", ti.Name, ti.Desc)
			}
		}
	}
	var opts []discordgo.SelectMenuOption
	for _, s := range sen {
		opts = append(opts, discordgo.SelectMenuOption{Label: s.Name, Value: fmt.Sprint(s.ID),
			Description: truncate(fmt.Sprintf("Opinion %s · %s · influence %s", game.OpinionNum(ops[s.ID]), s.TraitNames(), game.Num(s.Influence)), 100)})
	}
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: titled(emojiSenate, "The Senate"), Description: truncate(sb.String(), 4000), Color: colorSenate,
			Fields: []*discordgo.MessageEmbedField{{Name: "🎭 Traits", Value: truncate(traits.String(), 1024)}},
		}},
		Components: []discordgo.MessageComponent{row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu,
			CustomID: "info:senator", Placeholder: "Learn more about a senator…", Options: opts})},
	}))
}

// ---- voting ----

// matterFor loads an open matter and the caller's character in its court.
func (b *Bot) matterFor(ctx context.Context, i *discordgo.InteractionCreate, matterID int64) (store.Matter, game.MatterEvent, store.Character, string) {
	m, err := b.store.GetMatter(ctx, matterID)
	if err != nil {
		return m, game.MatterEvent{}, store.Character{}, "That matter doesn't exist."
	}
	ev, ok := game.MatterByKey(m.EventKey)
	if !ok {
		return m, ev, store.Character{}, "That matter no longer exists."
	}
	c, err := b.store.GetCharacter(ctx, m.GuildID, userOf(i).ID)
	if err != nil {
		return m, ev, c, "You have no place at this court yet."
	}
	if m.Resolved || !time.Now().Before(m.ClosesAt) {
		return m, ev, c, "Voting on this matter has closed."
	}
	return m, ev, c, ""
}

func (b *Bot) onVote(ctx context.Context, i *discordgo.InteractionCreate, matterID int64, opt string) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	o, ok := ev.Option(opt)
	if !ok {
		b.reply(i, "Unknown option.")
		return
	}
	if err := b.store.CastVote(ctx, m.ID, c.ID, opt, time.Now()); errors.Is(err, store.ErrEnded) {
		b.reply(i, "Voting on this matter has closed.")
		return
	} else if errors.Is(err, store.ErrVoteSold) {
		sold, _, _ := b.store.SoldVote(ctx, m.ID, c.ID)
		b.reply(i, fmt.Sprintf("🔒 You sold your vote on this matter. You will vote to **%s**.", strings.ToLower(optionLabel(ev, sold))))
		return
	} else if err != nil {
		b.fail(i, err)
		return
	}
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{
		Title:       emojiVote + " Vote cast",
		Description: fmt.Sprintf("You voted to **%s** on *%s*.", strings.ToLower(o.Label), ev.Title),
		Color:       colorGood,
		Fields: []*discordgo.MessageEmbedField{
			fact("Your voice", game.Num(b.share(ctx, m.GuildID, c.VoteWeight()))+"% of the Senate"),
			fact("Change it until", relTime(m.ClosesAt)),
		},
	}}}))
	b.leak(ctx, c, game.SecretVote(c.FullName(), ev.Title, o.Label))
}

// ---- bribes ----

func senatorSelect(m store.Matter, senators []game.Senator, bribes map[int64]store.Bribe, ops game.Opinions) discordgo.ActionsRow {
	var opts []discordgo.SelectMenuOption
	for _, s := range senators {
		desc := fmt.Sprintf("Opinion %s · %s · influence %s", game.OpinionNum(ops[s.ID]), s.TraitNames(), game.Num(s.Influence))
		if _, paid := bribes[s.ID]; paid {
			desc += " · already paid"
		}
		opts = append(opts, discordgo.SelectMenuOption{Label: s.Name, Value: fmt.Sprint(s.ID), Description: truncate(desc, 100)})
	}
	return row(discordgo.SelectMenu{
		MenuType: discordgo.StringSelectMenu, CustomID: fmt.Sprintf("senate:bribewho:%d", m.ID),
		Placeholder: "Choose a senator…", Options: opts,
	})
}

func (b *Bot) onBribe(ctx context.Context, i *discordgo.InteractionCreate, matterID int64) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.reply(i, msg)
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	bribes, err2 := b.store.Bribes(ctx, m.ID)
	ops, err3 := b.store.Opinions(ctx, c.ID)
	if err := errors.Join(err1, err2, err3); err != nil {
		b.fail(i, err)
		return
	}
	b.respond(i, ephemeral(&discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title: "💰 Bribe a senator",
			Description: fmt.Sprintf("*%s*\n\nPay a senator to vote your way. Each can be bought once per matter. "+
				"The less they like your option, or you, the more it costs.", ev.Title),
			Color:  colorSecret,
			Fields: []*discordgo.MessageEmbedField{fact("Your purse", fmt.Sprintf("%d gold", c.Wealth))},
			Footer: footer("🔒 A private deal. Spies may hear of it."),
		}},
		Components: []discordgo.MessageComponent{senatorSelect(m, sen, bribes, ops)},
	}))
}

func findSenator(sen []game.Senator, id int64) (game.Senator, bool) {
	for _, s := range sen {
		if s.ID == id {
			return s, true
		}
	}
	return game.Senator{}, false
}

func (b *Bot) onBribeWho(ctx context.Context, i *discordgo.InteractionCreate, matterID, senatorID int64) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.update(i, noteView(msg, colorMuted))
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	bribes, err2 := b.store.Bribes(ctx, m.ID)
	st, err3 := b.store.GetRealm(ctx, m.GuildID)
	ops, err4 := b.store.Opinions(ctx, c.ID)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		b.fail(i, err)
		return
	}
	s, ok := findSenator(sen, senatorID)
	if !ok {
		return
	}
	op := ops[s.ID]

	desc := fmt.Sprintf("*%s* · influence %s\n**Their opinion of you**\n%s\n", s.TraitNames(), game.Num(s.Influence), opinionBlock(op))
	var btns []discordgo.MessageComponent
	if br, paid := bribes[s.ID]; paid {
		if br.CharacterID == c.ID {
			desc += fmt.Sprintf("✅ You've already paid them to vote to **%s**.", strings.ToLower(optionLabel(ev, br.Option)))
		} else {
			desc += "🔒 Someone has already paid them for this matter."
		}
	} else if _, can := s.BribePrice(ev, ev.Default, st.Trackers, m.ID); !can {
		desc += "🚫 They cannot be bought."
	} else if _, can := s.BribePriceUnder(ev, ev.Default, st.Trackers, m.ID, op, st.Laws); !can {
		desc += "🚫 They won't take your gold. They don't think highly enough of you."
	} else {
		if st.Laws.Has(game.LawBribery) {
			desc += quote("⚖️ Bribery is a crime. It costs more, and if it comes out, it will cost you your good name.") + "\n"
		}
		desc += fmt.Sprintf("You have %s. Pick the vote you're paying for:", goldText(c.Wealth))
		for _, o := range ev.Options {
			price, _ := s.BribePriceUnder(ev, o.Key, st.Trackers, m.ID, op, st.Laws)
			btns = append(btns, button(fmt.Sprintf("%s: %d gold", o.Label, price),
				fmt.Sprintf("senate:pay:%d:%d:%s", m.ID, s.ID, o.Key), discordgo.SuccessButton, price > c.Wealth))
		}
	}
	comps := []discordgo.MessageComponent{senatorSelect(m, sen, bribes, ops)}
	if len(btns) > 0 {
		comps = append(comps, row(btns...))
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "💰 Senator " + s.Name, Description: desc, Color: colorSecret}},
		Components: comps,
	})
}

func (b *Bot) onPay(ctx context.Context, i *discordgo.InteractionCreate, matterID, senatorID int64, opt string) {
	m, ev, c, msg := b.matterFor(ctx, i, matterID)
	if msg != "" {
		b.update(i, noteView(msg, colorMuted))
		return
	}
	sen, err1 := b.store.Senators(ctx, m.GuildID)
	st, err2 := b.store.GetRealm(ctx, m.GuildID)
	ops, err3 := b.store.Opinions(ctx, c.ID)
	if err := errors.Join(err1, err2, err3); err != nil {
		b.fail(i, err)
		return
	}
	s, ok := findSenator(sen, senatorID)
	if !ok {
		return
	}
	if _, ok := ev.Option(opt); !ok {
		return
	}
	price, can := s.BribePriceUnder(ev, opt, st.Trackers, m.ID, ops[s.ID], st.Laws)
	if !can {
		b.update(i, &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{{Title: "💰 Senator " + s.Name, Description: "🚫 " + s.Name + " won't take your gold.", Color: colorMuted}},
		})
		return
	}
	var text string
	color := colorMuted
	switch err := b.store.PayBribe(ctx, m.ID, s.ID, c.ID, opt, price, game.BribeOpinion(s), time.Now()); {
	case errors.Is(err, store.ErrBought):
		text = "Someone got to " + s.Name + " first."
	case errors.Is(err, store.ErrEnded):
		text = "Voting on this matter has closed."
	case err != nil:
		text = err.Error()
	default:
		color = colorGood
		text = fmt.Sprintf("Gold changes hands. **%s** will vote to **%s**, and thinks a little better of you.\n*-%d gold · opinion %s*",
			s.Name, strings.ToLower(optionLabel(ev, opt)), price, game.OpinionNum(game.BribeOpinion(s)))
		text += b.bribeExposure(ctx, c, s, sen, st.Laws)
		defer b.leak(ctx, c, game.SecretBribe(c.FullName(), s.Name, ev.Title, optionLabel(ev, opt), price))
	}
	b.update(i, &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{Title: "💰 Senator " + s.Name, Description: text, Color: color}},
	})
}

// bribeExposure rolls whether a bribe comes out under the Law Against
// Bribery, applies and announces it, and returns a line for the briber.
func (b *Bot) bribeExposure(ctx context.Context, c store.Character, s game.Senator, sen []game.Senator, ls game.Laws) string {
	exposed, d, ops := game.BribeExposure(b.rng, c.Person(), sen, ls)
	if !exposed {
		return ""
	}
	if err := b.store.ApplyOutcome(ctx, c.GuildID, store.Outcome{Deltas: map[int64]game.Delta{c.ID: d}, Opinions: ops}, time.Now()); err != nil {
		slog.Error("bribe exposed", "err", err)
		return ""
	}
	b.postCourt(ctx, c.GuildID, &discordgo.MessageEmbed{
		Title:       "⚖️ A bribe comes to light",
		Description: fmt.Sprintf("**%s** (%s) was seen paying Senator **%s**. Bribery is a crime under the law, and the court will not forget.", c.FullName(), c.House(), s.Name),
		Color:       colorBad,
	})
	return "\n\n" + quote(fmt.Sprintf("⚖️ But you were seen, and bribery is a crime. The whole court knows.\n*%s*", d.Describe()))
}

// ventureNotices tells each player how the decision moved their venture
// income. It compares income now with income under the new sectors, realm
// and laws (which include the day's drift).
func ventureNotices(chars []store.Character, res game.MatterResult) {
	raw := map[int64]game.Stakes{}
	for _, c := range chars {
		raw[c.ID] = c.Stakes()
	}
	by, totals := game.EffectiveStakes(res.Laws, raw)
	for _, c := range chars {
		if len(c.Ventures) == 0 {
			continue
		}
		after := game.VentureIncome(res.Sectors, res.Realm, res.Laws, by[c.ID], totals)
		diff := after - c.VentureIncome
		if diff > -0.5 && diff < 0.5 {
			continue
		}
		word := "Your ventures do better"
		if diff < 0 {
			word = "Your ventures suffer"
		}
		res.Notices[c.ID] = append(res.Notices[c.ID], fmt.Sprintf("%s: %s gold/day, %s in all.", word, signedNum(diff), game.Num(after)))
	}
}

func signedNum(v float64) string {
	if v > 0 {
		return "+" + game.Num(v)
	}
	return game.Num(v)
}
