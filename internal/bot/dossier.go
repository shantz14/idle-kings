package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func init() {
	registerActivity(game.KindDossier, activityDef{
		panel: func(b *Bot, ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
			return b.dossierPanel(ctx, c)
		},
	})
	componentRoutes["dossier"] = (*Bot).onDossier
}

// Dossier custom IDs:
//   dossier:pick     (select) a report chosen → its page, with Denounce
//   dossier:home     back to the list
// Denouncing uses law:denounce:<denunciation>.

// dossierMax is how many reports the list shows (a select menu's limit).
const dossierMax = 25

func (b *Bot) onDossier(ctx context.Context, i *discordgo.InteractionCreate, parts []string) {
	c, ok := b.myCharacter(ctx, i)
	if !ok {
		return
	}
	switch parts[0] {
	case "home":
		b.update(i, b.dossierPanel(ctx, c))
	case "pick":
		vals := i.MessageComponentData().Values
		if len(vals) == 0 {
			return
		}
		id, _ := strconv.ParseInt(vals[0], 10, 64)
		d, err := b.store.GetDenunciation(ctx, id)
		if err != nil || d.OwnerID != c.ID || d.Used {
			b.update(i, b.dossierPanel(ctx, c))
			return
		}
		accused, err := b.store.GetCharacterByID(ctx, d.ActorID)
		if err != nil {
			b.update(i, b.dossierPanel(ctx, c))
			return
		}
		b.update(i, reportView(d, accused))
	}
}

// dossierEntry is one report, with the culprit's name.
type dossierEntry struct {
	Report store.Denunciation
	Name   string
}

func (b *Bot) dossierPanel(ctx context.Context, c store.Character) *discordgo.InteractionResponseData {
	reports, err := b.store.Dossier(ctx, c.ID)
	if err != nil {
		return noteView("📁 Your dossier is locked away somewhere. Try again later.", colorMuted)
	}
	ids := make([]int64, 0, len(reports))
	for _, d := range reports {
		ids = append(ids, d.ActorID)
	}
	names, _ := b.store.CharacterNames(ctx, ids)
	var entries []dossierEntry
	for _, d := range reports {
		name := "someone who has left court"
		if n, ok := names[d.ActorID]; ok {
			name = n.FullName
		}
		entries = append(entries, dossierEntry{d, name})
	}
	return dossierView(entries, c.Laws)
}

func dossierView(entries []dossierEntry, ls game.Laws) *discordgo.InteractionResponseData {
	var sb strings.Builder
	sb.WriteString("Everything your spies have caught your rivals doing. Reports keep forever: denounce them publicly, whisper it around quietly, or save it for when it hurts most.\n\n")
	foot := "📢 A denunciation shames them before the court. Under the Informers' Law it would hit harder, and pay."
	if ls.Has(game.LawInformers) {
		foot = "📢 While the Informers' Law stands, a denunciation hits harder and the treasury pays you for it."
	}
	sb.WriteString("**Reports**\n")
	comps := []discordgo.MessageComponent{}
	if len(entries) == 0 {
		sb.WriteString("Your spies haven't brought you anything worth using yet.")
	} else {
		var opts []discordgo.SelectMenuOption
		for n, e := range entries {
			if n == dossierMax {
				fmt.Fprintf(&sb, "\n…and %d older report%s. Use some of these first.", len(entries)-n, plural(len(entries)-n))
				break
			}
			fmt.Fprintf(&sb, "• **%s**: %s\n", e.Name, game.MisdeedWord(e.Report.Crime))
			opts = append(opts, discordgo.SelectMenuOption{
				Label:       truncate(e.Name, 100),
				Description: truncate(game.MisdeedWord(e.Report.Crime), 100),
				Value:       strconv.FormatInt(e.Report.ID, 10),
			})
		}
		comps = append(comps, row(discordgo.SelectMenu{CustomID: "dossier:pick", Placeholder: "Look at a report…", Options: opts}))
	}
	comps = append(comps, row(backButton(game.KindDossier)))
	desc := sb.String()
	if len(desc) > 4000 {
		desc = desc[:3999] + "…"
	}
	return &discordgo.InteractionResponseData{
		Embeds:     []*discordgo.MessageEmbed{{Title: "📁 Your Dossier", Description: desc, Color: colorSecret, Footer: footer(foot + " Using a report doesn't take your action.")}},
		Components: comps,
	}
}

func reportView(d store.Denunciation, accused store.Character) *discordgo.InteractionResponseData {
	force := "Shames them before the whole court, though nobody will pay you for it."
	if accused.Laws.Has(game.LawInformers) {
		force = "Under the Informers' Law it hits them hard, and the treasury pays you for it."
	}
	return &discordgo.InteractionResponseData{
		Embeds: []*discordgo.MessageEmbed{{
			Title:       fmt.Sprintf("📁 %s: %s", accused.FullName(), game.MisdeedWord(d.Crime)),
			Description: fmt.Sprintf("%s\n\n*Your spies reported this %s.*", quote(d.Detail), relTime(d.CreatedAt)),
			Color:       colorSecret,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "📢 Denounce", Value: force},
				{Name: "🗣️ Whisper", Value: "Softer, and nothing is posted. They'll only hear that someone is talking, unless their spies are watching you."},
			},
		}},
		Components: []discordgo.MessageComponent{row(
			withEmoji(button(truncate("Denounce "+accused.FullName(), 80), fmt.Sprintf("law:denounce:%d", d.ID), discordgo.DangerButton, false), "📢"),
			withEmoji(button("Whisper it", fmt.Sprintf("law:whisper:%d", d.ID), discordgo.PrimaryButton, false), "🗣️"),
			backTo("dossier:home"),
		)},
	}
}
