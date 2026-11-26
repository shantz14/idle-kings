package bot

import (
	"fmt"

	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

const (
	colorGold  = 0xC9A227
	colorGreen = 0x3BA55D
	colorRed   = 0xED4245
)

func standingLine(c store.Character) string {
	return fmt.Sprintf("Influence **%s** · Renown **%s** · Wealth **%d** gold", game.Num(c.Influence), game.Num(c.Renown), c.Wealth)
}
