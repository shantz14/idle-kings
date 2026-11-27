package bot

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/shantz14/idle-kings/internal/game"
)

// sampleCourt is n players with stakes in every industry, the first biggest.
func sampleCourt(n int) (map[int64]game.Stakes, game.Stakes, map[int64]owner) {
	raw := map[int64]game.Stakes{}
	owners := map[int64]owner{}
	for i := 1; i <= n; i++ {
		st := game.Stakes{}
		for k, d := range game.SectorDefs {
			st[d.Key] = float64(2000/i + 100*k)
		}
		raw[int64(i)] = st
		col := game.CrestColors[i%len(game.CrestColors)].Key
		owners[int64(i)] = owner{Name: fmt.Sprintf("Player%02d House%02d", i, i), Crest: game.Crest{Color: col, Symbol: "lion"}}
	}
	eff, totals := game.EffectiveStakes(nil, raw)
	return eff, totals, owners
}

func TestOwnershipCards(t *testing.T) {
	eff, totals, owners := sampleCourt(40)
	for _, viewer := range []int64{0, 1, 7} {
		cards := ownershipCards(eff, totals, owners, viewer)
		if len(cards) != len(game.SectorDefs) {
			t.Fatalf("%d cards", len(cards))
		}
		for _, c := range cards {
			sum := 0.0
			for _, s := range c.Slices {
				sum += s.Share
				if c.Sector.Secret && s.Name != "" && !s.You {
					t.Errorf("viewer %d: %s names %s", viewer, c.Sector.Name, s.Name)
				}
			}
			if math.Abs(sum-1) > 1e-9 {
				t.Errorf("%s: shares sum to %v", c.Sector.Name, sum)
			}
			checkLimits(t, "legend "+string(c.Sector.Key), &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{{Title: "x",
				Fields: []*discordgo.MessageEmbedField{{Name: "list", Value: legendList(c)}, {Name: "all", Value: legendLines(cards)}}}}})
		}
	}
	// Nobody has invested yet: all houses.
	empty := ownershipCards(nil, nil, nil, 1)
	if s := empty[0].Slices; len(s) != 1 || s[0].Share != 1 {
		t.Fatalf("empty sector: %+v", s)
	}
}

func TestChartsRender(t *testing.T) {
	out := os.Getenv("IDLE_KINGS_CHARTS") // a directory to write the PNGs to, to look at them
	for _, n := range []int{0, 1, 3, 40} {
		eff, totals, owners := sampleCourt(n)
		cards := ownershipCards(eff, totals, owners, 1)
		for name, data := range map[string][]byte{
			fmt.Sprintf("pie-%d.png", n):  pieImage(cards[0]),
			fmt.Sprintf("grid-%d.png", n): pieGrid(cards),
			fmt.Sprintf("mine-%d.png", n): pieGrid(ownedCards(cards)),
		} {
			if n == 0 && strings.HasPrefix(name, "mine") {
				if data != nil {
					t.Errorf("%s: a grid with nothing owned should be empty", name)
				}
				continue
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if b := img.Bounds(); b.Dx() < 100 || b.Dy() < 100 {
				t.Errorf("%s: %v", name, b)
			}
			if out != "" {
				_ = os.WriteFile(filepath.Join(out, name), data, 0o644)
			}
		}
	}
}
