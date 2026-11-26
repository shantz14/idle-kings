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
	sectorEnd := map[SectorKey]float64{}
	lawDays := map[LawKey]int{}   // days each law stood
	fellUnder := map[LawKey]int{} // falls with each law in force
	survivors := 0
	const games, days = 300, 90
	for g := 0; g < games; g++ {
		sen := GenerateSenate(r, SenateSize)
		for i := range sen {
			sen[i].ID = int64(i + 1)
		}
		realm := StartingRealm()
		sectors := StartingSectors()
		type queued struct {
			key string
			due int
		}
		var queue []queued
		var recent []string
		var laws Laws
		fell := false
		resolve := func(d int, ev MatterEvent) bool {
			res := ResolveMatter(r, MatterInput{MatterID: int64(g*1000 + d), Event: ev, Realm: realm, Sectors: sectors, Senators: sen, Laws: laws})
			wins[ev.Key+"/"+res.Winner.Key]++
			realm, sectors, laws = res.Realm, res.Sectors, res.Laws
			if res.Fallen {
				for _, l := range laws {
					fellUnder[l.Key]++
				}
			}
			for _, f := range res.FollowUps {
				queue = append(queue, queued{f.Event, d + max(f.Days, 1)})
			}
			if res.Fallen {
				falls++
				fallDay += d
				reasons[res.FallenReason]++
				fell = true
			}
			return res.Fallen
		}
		for d := 0; d < days && !fell; d++ {
			// Bills come due on their day, on top of the daily matter.
			var rest []queued
			for _, q := range queue {
				if e, _ := MatterByKey(q.key); e.Auto && q.due <= d && !fell {
					resolve(d, e)
					continue
				}
				rest = append(rest, q)
			}
			queue = rest
			if fell {
				break
			}
			var ev MatterEvent
			for i, q := range queue {
				if q.due <= d && FollowUpAllowed(q.key, recent) {
					ev, _ = MatterByKey(q.key)
					queue = append(queue[:i:i], queue[i+1:]...)
					break
				}
			}
			if ev.Key == "" {
				ev = PickMatterAvoiding(r, realm, recent, nil, laws)
			}
			for _, l := range laws {
				lawDays[l.Key]++
			}
			recent = append(recent, ev.Key)
			if len(recent) > MatterCooldown {
				recent = recent[1:]
			}
			resolve(d, ev)
		}
		if !fell {
			for k, v := range realm {
				end[k] += v / games
			}
			survivors++
			for _, d := range SectorDefs {
				sectorEnd[d.Key] += sectors.Revenue(d, realm, nil) / d.BaseRevenue
			}
		}
	}
	fmt.Printf("fell %d/%d, avg day %.1f\n", falls, games, float64(fallDay)/float64(max(falls, 1)))
	fmt.Println(reasons)
	fmt.Println("avg end (survivors):", end)
	fmt.Println("sector revenue at the end vs. the start (survivors, no investment):")
	for _, d := range SectorDefs {
		fmt.Printf("  %-24s %.2fx\n", d.Name, sectorEnd[d.Key]/float64(max(survivors, 1)))
	}
	fmt.Println("laws: days in force (all games), falls with it in force:")
	for _, d := range LawDefs {
		fmt.Printf("  %-24s %6d days  %3d falls\n", d.Name, lawDays[d.Key], fellUnder[d.Key])
	}
	var keys []string
	for k := range wins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-36s %d\n", k, wins[k])
	}
}
