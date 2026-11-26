package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Character creator custom IDs (one ephemeral popup, updated in place):
//   cc:start                 Join button on the /start message
//   cc:editnames             open the names modal again
//   cc:color / cc:symbol     crest selects
//   cc:pt:<stat>:add|sub     assign a point
//   cc:pt:reset              clear assigned points
//   cc:bg / cc:role          background and role selects
//   cc:step:<n>              go to a step
//   cc:confirm               create the character
// Modals: cc:names:new (from Join) and cc:names:edit (from the popup).

const idCreatorStart = "cc:start"

const (
	stepNames = iota
	stepCrest
	stepStats
	stepBackground
	stepRole
)

var stepTitles = []string{"Name", "Crest", "Stats", "Background", "Role"}

var statBlurbs = map[game.Stat]string{
	game.Diplomacy:   "parties, persuasion, charm",
	game.Martial:     "commanding soldiers and guards",
	game.Stewardship: "money, trade, running things",
	game.Intrigue:    "rumors, secrets, schemes",
	game.Prowess:     "your own sword arm",
}

type draft struct {
	Build game.Build `json:"build"`
	Step  int        `json:"step"`
}

func (b *Bot) loadDraft(ctx context.Context, guildID, userID string) (draft, error) {
	var d draft
	_, err := b.store.GetDraft(ctx, guildID, userID, &d)
	if d.Build.Points == nil {
		d.Build.Points = game.Stats{}
	}
	return d, err
}

// maxStep is the furthest step the draft has unlocked.
func maxStep(d draft) int {
	bd := d.Build
	switch {
	case strings.TrimSpace(bd.Name) == "" || strings.TrimSpace(bd.Family) == "":
		return stepNames
	case !bd.Crest.Complete():
		return stepCrest
	case bd.PointsLeft() != 0:
		return stepStats
	case bd.Background == "":
		return stepBackground
	default:
		return stepRole
	}
}

// creatorGate says why the caller can't use the creator here, or "".
func (b *Bot) creatorGate(ctx context.Context, i *discordgo.InteractionCreate) string {
	if i.GuildID == "" {
		return "Join from the court's server: use the Join button there, or `/join`."
	}
	court, err := b.store.GetCourt(ctx, i.GuildID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return "This server has no court yet. The admin needs to `/claim-admin` and `/start`."
	case err != nil:
		return "Something went wrong."
	case court.Status == "setup":
		return "The game hasn't started yet."
	case court.Status == "fallen":
		return "The realm has fallen. There is no court left to join."
	}
	if _, err := b.store.GetCharacter(ctx, i.GuildID, userOf(i).ID); err == nil {
		return "You already have a place at court. Try `/court`."
	}
	return ""
}

func namesModal(id string, d draft) *discordgo.InteractionResponse {
	field := func(cid, label, value string, required bool, max int) discordgo.MessageComponent {
		return row(discordgo.TextInput{CustomID: cid, Label: label, Style: discordgo.TextInputShort,
			Value: value, Required: required, MaxLength: max})
	}
	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: id, Title: "Your name and family",
			Components: []discordgo.MessageComponent{
				field("name", "Your name", d.Build.Name, true, game.MaxNameLen),
				field("family", "Family name (your house)", d.Build.Family, true, game.MaxNameLen),
				field("motto", "Family motto (optional)", d.Build.Crest.Motto, false, game.MaxMottoLen),
			},
		},
	}
}

// onJoin starts or resumes the creator (Join button or /join).
func (b *Bot) onJoin(ctx context.Context, i *discordgo.InteractionCreate) {
	if msg := b.creatorGate(ctx, i); msg != "" {
		b.reply(i, msg)
		return
	}
	d, err := b.loadDraft(ctx, i.GuildID, userOf(i).ID)
	if err != nil {
		b.fail(i, err)
		return
	}
	if maxStep(d) == stepNames {
		if err := b.s.InteractionRespond(i.Interaction, namesModal("cc:names:new", d)); err != nil {
			slog.Error("names modal", "err", err)
		}
		return
	}
	b.respond(i, ephemeral(b.creatorView(d, "Welcome back. Your character is where you left it.")))
}

func (b *Bot) onCreator(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) == 0 {
		return
	}
	if parts[0] == "start" {
		b.onJoin(ctx, i)
		return
	}
	if msg := b.creatorGate(ctx, i); msg != "" {
		b.update(i, &discordgo.InteractionResponseData{Content: msg, Embeds: []*discordgo.MessageEmbed{}})
		return
	}
	uid := userOf(i).ID
	d, err := b.loadDraft(ctx, i.GuildID, uid)
	if err != nil {
		b.fail(i, err)
		return
	}
	value := func() string {
		if v := i.MessageComponentData().Values; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	note := ""
	switch parts[0] {
	case "editnames":
		if err := b.s.InteractionRespond(i.Interaction, namesModal("cc:names:edit", d)); err != nil {
			slog.Error("names modal", "err", err)
		}
		return
	case "color":
		if _, ok := game.CrestColor(value()); ok {
			d.Build.Crest.Color = value()
		}
	case "symbol":
		if _, ok := game.CrestSymbol(value()); ok {
			d.Build.Crest.Symbol = value()
		}
	case "pt":
		b.assignPoint(&d, parts[1:])
	case "bg":
		if _, ok := game.BackgroundByKey(value()); ok {
			d.Build.Background = value()
		}
	case "role":
		if _, ok := game.RoleByKey(value()); ok {
			d.Build.Role = value()
		}
	case "step":
		if len(parts) > 1 {
			d.Step = int(atoi(parts[1]))
		}
	case "confirm":
		b.finishCreator(ctx, i, d)
		return
	}
	if d.Step > maxStep(d) {
		d.Step = maxStep(d)
	}
	if d.Step < stepCrest {
		d.Step = stepCrest
	}
	if err := b.store.SaveDraft(ctx, i.GuildID, uid, d); err != nil {
		b.fail(i, err)
		return
	}
	b.update(i, b.creatorView(d, note))
}

func (b *Bot) assignPoint(d *draft, parts []string) {
	if len(parts) == 1 && parts[0] == "reset" {
		d.Build.Points = game.Stats{}
		return
	}
	if len(parts) < 2 {
		return
	}
	st := game.Stat(parts[0])
	if _, ok := statBlurbs[st]; !ok {
		return
	}
	switch parts[1] {
	case "add":
		if d.Build.PointsLeft() > 0 && d.Build.Points[st] < game.MaxPointsPerStat {
			d.Build.Points[st]++
		}
	case "sub":
		if d.Build.Points[st] > 0 {
			d.Build.Points[st]--
		}
	}
}

func (b *Bot) onCreatorModal(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	if len(parts) < 2 || parts[0] != "names" {
		return
	}
	fromPopup := parts[1] == "edit"
	fail := func(msg string) {
		retry := []discordgo.MessageComponent{row(button("Try again", "cc:editnames", discordgo.PrimaryButton, false))}
		if fromPopup {
			b.update(i, &discordgo.InteractionResponseData{Content: msg, Components: retry})
		} else {
			b.respond(i, ephemeral(&discordgo.InteractionResponseData{Content: msg, Components: retry}))
		}
	}
	if msg := b.creatorGate(ctx, i); msg != "" {
		b.reply(i, msg)
		return
	}
	uid := userOf(i).ID
	d, err := b.loadDraft(ctx, i.GuildID, uid)
	if err != nil {
		b.fail(i, err)
		return
	}
	f := modalValues(i.ModalSubmitData())
	name, family, motto := strings.TrimSpace(f["name"]), strings.TrimSpace(f["family"]), strings.TrimSpace(f["motto"])
	if name == "" || family == "" {
		fail("Your character needs a name and a family name.")
		return
	}
	if taken, err := b.store.FamilyTaken(ctx, i.GuildID, family); err != nil {
		b.fail(i, err)
		return
	} else if taken {
		fail(fmt.Sprintf("House %s already sits at this court. Choose another family name.", family))
		return
	}
	d.Build.Name, d.Build.Family, d.Build.Crest.Motto = name, family, motto
	if d.Step < stepCrest {
		d.Step = stepCrest
	}
	if err := b.store.SaveDraft(ctx, i.GuildID, uid, d); err != nil {
		b.fail(i, err)
		return
	}
	if fromPopup {
		b.update(i, b.creatorView(d, ""))
	} else {
		b.respond(i, ephemeral(b.creatorView(d, "")))
	}
}

func (b *Bot) finishCreator(ctx context.Context, i *discordgo.InteractionCreate, d draft) {
	if err := d.Build.Validate(); err != nil {
		b.update(i, b.creatorView(d, "Not quite ready: "+strings.ReplaceAll(err.Error(), "\n", "; ")+"."))
		return
	}
	uid := userOf(i).ID
	c, err := b.store.CreateCharacter(ctx, i.GuildID, uid, d.Build)
	switch {
	case errors.Is(err, store.ErrFamilyTaken):
		d.Step = stepCrest
		_ = b.store.SaveDraft(ctx, i.GuildID, uid, d)
		b.update(i, b.creatorView(d, fmt.Sprintf("Someone just took House %s. Use **Edit name & motto** to pick another family name.", d.Build.Family)))
		return
	case errors.Is(err, store.ErrExists):
		b.update(i, &discordgo.InteractionResponseData{Content: "You already have a place at court.", Embeds: []*discordgo.MessageEmbed{}})
		return
	case err != nil:
		b.fail(i, err)
		return
	}
	_ = b.store.DeleteDraft(ctx, i.GuildID, uid)

	role, _ := game.RoleByKey(c.Role)
	bg, _ := game.BackgroundByKey(c.Background)
	welcome := b.profileEmbed(ctx, c, true)
	welcome.Title = "Welcome to court, " + c.Name
	welcome.Footer = &discordgo.MessageEmbedFooter{Text: "Next: /court to act · /senate to vote · /help to learn the ropes"}
	b.update(i, &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{welcome}})

	channel := i.ChannelID
	if court, err := b.store.GetCourt(ctx, i.GuildID); err == nil && court.AnnouncementsChannelID != "" {
		channel = court.AnnouncementsChannelID
	}
	desc := fmt.Sprintf("**%s** · %s\n<@%s>", role.Name, bg.Name, c.UserID)
	if m := c.Crest.Motto; m != "" {
		desc = fmt.Sprintf("*\"%s\"*\n\n", m) + desc
	}
	_, _ = b.s.ChannelMessageSendEmbed(channel, &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s %s of House %s joins the court", c.Crest.Emoji(), c.Name, c.FamilyName),
		Description: desc,
		Color:       colorGold,
	})
}

// ---- the popup ----

func previewLine(bd game.Build) string {
	line := fmt.Sprintf("%s **%s** of House **%s**", bd.Crest.Emoji(), bd.Name, bd.Family)
	if bd.Crest.Motto != "" {
		line += fmt.Sprintf("\n*\"%s\"*", bd.Crest.Motto)
	}
	return line
}

func statsBlock(st game.Stats, extra map[game.Stat]string) string {
	var sb strings.Builder
	sb.WriteString("```\n")
	for _, s := range game.AllStats {
		fmt.Fprintf(&sb, "%-12s %2s  %-10s %s\n", s, game.Num(st[s]), strings.Repeat("■", int(st[s])), extra[s])
	}
	sb.WriteString("```")
	return sb.String()
}

func bonusText(s game.Stats) string { return game.Delta{Stats: s}.Describe() }

func estateLines(keys []string) string {
	var sb strings.Builder
	for _, k := range keys {
		it, ok := game.ItemByKey(k)
		if !ok {
			continue
		}
		icon := "📦"
		if it.Kind == game.KindHome {
			icon = "🏠"
		}
		fmt.Fprintf(&sb, "%s **%s** (value %s)", icon, it.Name, game.Num(it.Value))
		if e := it.Effects.Describe(); e != "" {
			sb.WriteString(": " + e)
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "**Estate value: %s**", game.Num(game.EstateValue(keys)))
	return sb.String()
}

func (b *Bot) creatorView(d draft, note string) *discordgo.InteractionResponseData {
	bd := d.Build
	step := d.Step
	if step < stepCrest {
		step = stepCrest
	}
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Create your character · step %d of %d: %s", step+1, len(stepTitles), stepTitles[step]),
		Color: colorGold,
	}
	var sb strings.Builder
	if note != "" {
		sb.WriteString("> " + note + "\n\n")
	}
	sb.WriteString(previewLine(bd) + "\n\n")
	var comps []discordgo.MessageComponent
	back := func(to int) discordgo.MessageComponent {
		return button("Back", fmt.Sprintf("cc:step:%d", to), discordgo.SecondaryButton, false)
	}
	next := func(to int, enabled bool) discordgo.MessageComponent {
		return button("Next", fmt.Sprintf("cc:step:%d", to), discordgo.PrimaryButton, !enabled)
	}

	switch step {
	case stepCrest:
		sb.WriteString("Choose your family's color and symbol. They appear beside your name everywhere in the game.")
		comps = []discordgo.MessageComponent{
			crestSelect("cc:color", "Choose a color…", game.CrestColors, bd.Crest.Color),
			crestSelect("cc:symbol", "Choose a symbol…", game.CrestSymbols, bd.Crest.Symbol),
			row(button("Edit name & motto", "cc:editnames", discordgo.SecondaryButton, false), next(stepStats, bd.Crest.Complete())),
		}

	case stepStats:
		fmt.Fprintf(&sb, "Every stat starts at %d. Spend **%d** points, at most %d in one stat. Your background and role add a few more later.\n\n",
			game.BaseStat, game.FreePoints, game.MaxPointsPerStat)
		pts := map[game.Stat]string{}
		for s, v := range bd.Points {
			if v > 0 {
				pts[s] = fmt.Sprintf("(+%s)", game.Num(v))
			}
		}
		sb.WriteString(statsBlock(bd.AssignedStats(), pts))
		fmt.Fprintf(&sb, "\nPoints left: **%d**", bd.PointsLeft())
		var blurbs []string
		for _, s := range game.AllStats {
			blurbs = append(blurbs, fmt.Sprintf("**%s**: %s", s, statBlurbs[s]))
		}
		embed.Fields = []*discordgo.MessageEmbedField{{Name: "What the stats do", Value: strings.Join(blurbs, "\n")}}
		var plus, minus []discordgo.MessageComponent
		for _, s := range game.AllStats {
			name := strings.ToUpper(string(s[:1])) + string(s[1:])
			plus = append(plus, button("+ "+name, fmt.Sprintf("cc:pt:%s:add", s), discordgo.SuccessButton,
				bd.PointsLeft() == 0 || bd.Points[s] >= game.MaxPointsPerStat))
			minus = append(minus, button("− "+name, fmt.Sprintf("cc:pt:%s:sub", s), discordgo.SecondaryButton, bd.Points[s] <= 0))
		}
		comps = []discordgo.MessageComponent{row(plus...), row(minus...), row(
			button("Reset", "cc:pt:reset", discordgo.DangerButton, bd.PointsSpent() == 0),
			back(stepCrest), next(stepBackground, bd.PointsLeft() == 0),
		)}

	case stepBackground:
		var opts []discordgo.SelectMenuOption
		for _, bg := range game.Backgrounds {
			opts = append(opts, discordgo.SelectMenuOption{Label: bg.Name, Value: bg.Key, Description: bg.Summary, Default: bg.Key == bd.Background})
		}
		if bg, ok := game.BackgroundByKey(bd.Background); ok {
			fmt.Fprintf(&sb, "**%s**\n*%s*", bg.Name, bg.Story)
			embed.Fields = []*discordgo.MessageEmbedField{
				{Name: "Stat bonus", Value: bonusText(bg.StatBonus), Inline: true},
				{Name: "Renown", Value: game.Num(bg.Renown), Inline: true},
				{Name: "Gold", Value: fmt.Sprint(bg.Wealth), Inline: true},
				{Name: "Starting estate", Value: estateLines(bg.EstateItems())},
			}
		} else {
			sb.WriteString("Where do you come from? Your background adds stat points and decides your starting renown, gold and estate: your home and a possession or two.")
		}
		comps = []discordgo.MessageComponent{
			row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "cc:bg", Placeholder: "Choose your background…", Options: opts}),
			row(back(stepStats), next(stepRole, bd.Background != "")),
		}

	case stepRole:
		var opts []discordgo.SelectMenuOption
		for _, r := range game.Roles {
			opts = append(opts, discordgo.SelectMenuOption{Label: r.Name, Value: r.Key,
				Description: truncate(r.Description, 100), Default: r.Key == bd.Role})
		}
		if r, ok := game.RoleByKey(bd.Role); ok {
			fmt.Fprintf(&sb, "**%s**: %s", r.Name, r.Description)
			start := bd.Final()
			effects := game.SumEffects(start.Items)
			eff := start.Stats.Clone()
			eff.Add(effects.Stats)
			fromItems := map[game.Stat]string{}
			for s, v := range effects.Stats {
				if v != 0 {
					fromItems[s] = fmt.Sprintf("(%+g from estate)", v)
				}
			}
			embed.Fields = []*discordgo.MessageEmbedField{
				{Name: "Stat bonus", Value: bonusText(r.StatBonus), Inline: true},
				{Name: "Influence", Value: game.Num(r.Influence), Inline: true},
				{Name: "Salary", Value: fmt.Sprintf("%d gold/day", r.Salary), Inline: true},
				{Name: "Your starting character", Value: statsBlock(eff, fromItems) + fmt.Sprintf(
					"Influence **%s** · Renown **%s** · Gold **%d** · Income **%d**/day · Estate **%s**",
					game.Num(start.Influence), game.Num(start.Renown), start.Wealth,
					start.DailyIncome+effects.DailyIncome, game.Num(game.EstateValue(start.Items)))},
			}
		} else {
			sb.WriteString("Finally, your place at court. Your role adds a little to your stats, sets your starting influence and salary, and some events and actions are only for certain roles.")
		}
		comps = []discordgo.MessageComponent{
			row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: "cc:role", Placeholder: "Choose your role…", Options: opts}),
			row(back(stepBackground), button("Join the court", "cc:confirm", discordgo.SuccessButton, bd.Role == "")),
		}
	}
	embed.Description = sb.String()
	return &discordgo.InteractionResponseData{Content: "", Embeds: []*discordgo.MessageEmbed{embed}, Components: comps}
}

func crestSelect(id, placeholder string, parts []game.CrestPart, chosen string) discordgo.ActionsRow {
	var opts []discordgo.SelectMenuOption
	for _, p := range parts {
		opts = append(opts, discordgo.SelectMenuOption{Label: p.Name, Value: p.Key, Description: p.Emoji + " " + p.Name,
			Emoji: &discordgo.ComponentEmoji{Name: p.Emoji}, Default: p.Key == chosen})
	}
	return row(discordgo.SelectMenu{MenuType: discordgo.StringSelectMenu, CustomID: id, Placeholder: placeholder, Options: opts})
}
