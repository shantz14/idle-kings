package bot

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// Colors say what kind of message an embed is, not which screen it's on.
const (
	colorPanel  = 0xC9A227 // menus and panels
	colorGood   = 0x3BA55D // it worked
	colorBad    = 0xED4245 // it failed, or danger
	colorNotice = 0x5865F2 // a DM asking you to decide something
	colorSecret = 0x8E44AD // spies, schemes and private reports
	colorSenate = 0x3B6BA5 // matters before the Senate
	colorMuted  = 0x80848E // closed, expired, small replies
)

// Fixed emoji for screens that aren't menu actions.
const (
	emojiSenate   = "🏛️"
	emojiRealm    = "👑"
	emojiEstate   = "🏰"
	emojiShop     = "🛒"
	emojiHelp     = "📖"
	emojiSheet    = "📜"
	emojiCourt    = "👥"
	emojiDeal     = "🤝"
	emojiVote     = "🗳️"
	emojiAnnounce = "📣"
	emojiWarn     = "⚠️"
)

func standingLine(c store.Character) string {
	return fmt.Sprintf("Influence **%s** · Renown **%s** · **%d** gold", game.Num(c.Influence), game.Num(c.Renown), c.Wealth)
}

// actionTitle is an action's menu name with its emoji: "🎭 Throw a Party".
func actionTitle(kind string) string {
	a, ok := game.ActionByKey(kind)
	if !ok {
		return kind
	}
	return titled(a.Emoji, a.Name)
}

// titled puts an emoji in front of a title, if there is one.
func titled(emoji, title string) string {
	if emoji == "" {
		return title
	}
	return emoji + " " + title
}

// panelEmbed is a menu or action panel.
func panelEmbed(title, desc string) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: title, Description: desc, Color: colorPanel}
}

// actionEmbed is an action's panel, titled with its emoji and name.
func actionEmbed(kind, desc string) *discordgo.MessageEmbed {
	return panelEmbed(actionTitle(kind), desc)
}

// resultColor is green for a success, red otherwise.
func resultColor(ok bool) int {
	if ok {
		return colorGood
	}
	return colorBad
}

// fact is a short inline field: a number or a word under a label.
func fact(name, value string) *discordgo.MessageEmbedField {
	return &discordgo.MessageEmbedField{Name: name, Value: value, Inline: true}
}

// footer is an embed's small print.
func footer(text string) *discordgo.MessageEmbedFooter {
	if text == "" {
		return nil
	}
	return &discordgo.MessageEmbedFooter{Text: text}
}

// quote sets a note apart as a block quote, for status lines at the top of
// a panel. Empty stays empty.
func quote(note string) string {
	if note == "" {
		return ""
	}
	return "> " + strings.ReplaceAll(note, "\n", "\n> ")
}

// withNote puts a quoted note above a panel's text.
func withNote(note, desc string) string {
	if note == "" {
		return desc
	}
	return quote(note) + "\n\n" + desc
}

// goldText is "**120** gold".
func goldText(n int) string { return fmt.Sprintf("**%d** gold", n) }

// relTime is a Discord relative timestamp: "in 2 hours".
func relTime(t time.Time) string { return fmt.Sprintf("<t:%d:R>", t.Unix()) }

// noteView is a one-line answer as a small embed.
func noteView(msg string, color int) *discordgo.InteractionResponseData {
	return &discordgo.InteractionResponseData{Content: "", Embeds: []*discordgo.MessageEmbed{{Description: msg, Color: color}}}
}

// bar is a ten-block meter for a 0–100 value.
func bar(v float64) string {
	n := int(v/10 + 0.5)
	n = max(0, min(10, n))
	return strings.Repeat("█", n) + strings.Repeat("░", 10-n)
}

// backTo is a grey Back button to a custom ID.
func backTo(id string) discordgo.Button {
	b := button("Back", id, discordgo.SecondaryButton, false)
	b.Emoji = &discordgo.ComponentEmoji{Name: "↩️"}
	return b
}

// withEmoji puts an emoji on a button.
func withEmoji(b discordgo.Button, emoji string) discordgo.Button {
	if emoji != "" {
		b.Emoji = &discordgo.ComponentEmoji{Name: emoji}
	}
	return b
}
