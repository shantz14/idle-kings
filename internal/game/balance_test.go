package game

import (
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"testing"
)

// TestBalance simulates AI-only senates to check how often the realm falls.
// Run with: IDLE_KINGS_SIM=1 go test ./internal/game -run TestBalance -v
func TestBalance(t *testing.T) {
	if os.Getenv("IDLE_KINGS_SIM") == "" {
		t.Skip("set IDLE_KINGS_SIM=1 to run the balance simulation")
	}
	r := rand.New(rand.NewPCG(9, 9))
	falls, fallDay := 0, 0
	reasons := map[string]int{}
	wins := map[string]int{}
	end := map[Attr]float64{}
	const games, days = 300, 90
	for g := 0; g < games; g++ {
		sen := GenerateSenate(r, SenateSize)
		for i := range sen {
			sen[i].ID = int64(i + 1)
		}
		realm := StartingRealm()
		var queue []QueuedEvent
		var recent []string
		fell := false
		for d := 0; d < days; d++ {
			var ev MatterEvent
			if len(queue) > 0 {
				ev, _ = MatterByKey(queue[0].Event)
				queue = queue[1:]
			} else {
				ev = PickMatter(r, realm, recent)
			}
			recent = append(recent, ev.Key)
			if len(recent) > 5 {
				recent = recent[1:]
			}
			res := ResolveMatter(r, MatterInput{MatterID: int64(g*1000 + d), Event: ev, Realm: realm, Senators: sen})
			wins[ev.Key+"/"+res.Winner.Key]++
			realm = res.Realm
			queue = append(queue, res.FollowUps...)
			if res.Fallen {
				falls++
				fallDay += d
				reasons[res.FallenReason]++
				fell = true
				break
			}
		}
		if !fell {
			for k, v := range realm {
				end[k] += v / games
			}
		}
	}
	fmt.Printf("fell %d/%d, avg day %.1f\n", falls, games, float64(fallDay)/float64(max(falls, 1)))
	fmt.Println(reasons)
	fmt.Println("avg end (survivors):", end)
	var keys []string
	for k := range wins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-36s %d\n", k, wins[k])
	}
}
