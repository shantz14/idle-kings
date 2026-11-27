package bot

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// Every view checkLimits sees can be written out as text, to review how the
// popups read without a Discord token:
//
//	IDLE_KINGS_VIEWS=/tmp/views.txt go test ./internal/bot
var (
	viewsMu sync.Mutex
	views   = map[string]string{}
)

func recordView(where string, d *discordgo.InteractionResponseData) {
	if os.Getenv("IDLE_KINGS_VIEWS") == "" {
		return
	}
	viewsMu.Lock()
	defer viewsMu.Unlock()
	if _, ok := views[where]; !ok {
		views[where] = renderView(d)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if path := os.Getenv("IDLE_KINGS_VIEWS"); path != "" {
		keys := make([]string, 0, len(views))
		for k := range views {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&sb, "════════ %s ════════\n%s\n", k, views[k])
		}
		if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

var styleNames = map[discordgo.ButtonStyle]string{
	discordgo.PrimaryButton: "blue", discordgo.SecondaryButton: "grey",
	discordgo.SuccessButton: "green", discordgo.DangerButton: "red", discordgo.LinkButton: "link",
}

// renderView is a plain-text picture of a message.
func renderView(d *discordgo.InteractionResponseData) string {
	var sb strings.Builder
	if d.Content != "" {
		sb.WriteString(d.Content + "\n")
	}
	for _, e := range d.Embeds {
		fmt.Fprintf(&sb, "┃ #%06X\n", e.Color)
		if e.Author != nil {
			fmt.Fprintf(&sb, "┃ (%s)\n", e.Author.Name)
		}
		if e.Title != "" {
			fmt.Fprintf(&sb, "┃ ## %s\n", e.Title)
		}
		for _, l := range strings.Split(e.Description, "\n") {
			if e.Description != "" {
				fmt.Fprintf(&sb, "┃ %s\n", l)
			}
		}
		for _, f := range e.Fields {
			inline := ""
			if f.Inline {
				inline = " (inline)"
			}
			fmt.Fprintf(&sb, "┃ ▸ %s%s\n", f.Name, inline)
			for _, l := range strings.Split(f.Value, "\n") {
				fmt.Fprintf(&sb, "┃     %s\n", l)
			}
		}
		if e.Footer != nil {
			fmt.Fprintf(&sb, "┃ — %s\n", e.Footer.Text)
		}
	}
	for _, c := range d.Components {
		r, ok := c.(discordgo.ActionsRow)
		if !ok {
			continue
		}
		var parts []string
		for _, x := range r.Components {
			switch v := x.(type) {
			case discordgo.Button:
				label := v.Label
				if v.Emoji != nil {
					label = v.Emoji.Name + " " + label
				}
				dis := ""
				if v.Disabled {
					dis = ", off"
				}
				parts = append(parts, fmt.Sprintf("[%s (%s%s)]", label, styleNames[v.Style], dis))
			case discordgo.SelectMenu:
				parts = append(parts, fmt.Sprintf("[▾ %s (%d options)]", v.Placeholder, len(v.Options)))
			}
		}
		sb.WriteString(strings.Join(parts, " ") + "\n")
	}
	return sb.String()
}
