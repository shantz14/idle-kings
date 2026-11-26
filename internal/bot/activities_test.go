package bot

import (
	"testing"

	"github.com/shantz14/idle-kings/internal/game"
)

// Every action in the menu needs a registered panel, and every timed one a
// resolver and a busy line.
func TestEveryActionIsRegistered(t *testing.T) {
	panelOnly := map[string]bool{game.KindDossier: true, game.KindAid: true}
	for _, a := range game.Actions {
		d, ok := activityDefs[a.Key]
		switch {
		case !ok || d.panel == nil:
			t.Errorf("%s: no panel registered", a.Key)
		case panelOnly[a.Key]:
		case d.resolve == nil || d.busy == nil:
			t.Errorf("%s: timed action without a resolver or busy line", a.Key)
		}
	}
}
