package bot

import (
	"regexp"
	"testing"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

// TestCommandsFitDiscord checks slash command names and descriptions
// against Discord's rules, which it only enforces when the bot starts.
func TestCommandsFitDiscord(t *testing.T) {
	name := regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	var check func(where, n, d string, opts []*discordgo.ApplicationCommandOption)
	check = func(where, n, d string, opts []*discordgo.ApplicationCommandOption) {
		if !name.MatchString(n) {
			t.Errorf("%s: bad name %q", where, n)
		}
		if l := utf8.RuneCountInString(d); l == 0 || l > 100 {
			t.Errorf("%s: description of %d characters", where, l)
		}
		if len(opts) > 25 {
			t.Errorf("%s: %d options", where, len(opts))
		}
		required := true
		for _, o := range opts {
			if o.Required && !required {
				t.Errorf("%s: required option %q after an optional one", where, o.Name)
			}
			required = required && o.Required
			for _, c := range o.Choices {
				if utf8.RuneCountInString(c.Name) > 100 {
					t.Errorf("%s/%s: choice %q too long", where, o.Name, c.Name)
				}
			}
			check(where+"/"+o.Name, o.Name, o.Description, o.Options)
		}
	}
	for _, c := range commands() {
		check(c.Name, c.Name, c.Description, c.Options)
	}
}
