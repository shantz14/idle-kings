package game

import (
	"math/rand/v2"
	"testing"
)

func benchCourt() ([]Senator, []Player, Realm, SectorState) {
	r := rand.New(rand.NewPCG(1, 2))
	sen := GenerateSenate(r, SenateSize)
	for i := range sen {
		sen[i].ID = int64(i + 1)
	}
	var players []Player
	for i := 0; i < 20; i++ {
		players = append(players, Player{Person: Person{ID: int64(100 + i)}, Weight: 5})
	}
	return sen, players, StartingRealm(), StartingSectors()
}

func BenchmarkResolveMatter(b *testing.B) {
	sen, players, realm, sectors := benchCourt()
	r := rand.New(rand.NewPCG(3, 4))
	for n := 0; n < b.N; n++ {
		ev := MatterEvents[n%len(MatterEvents)]
		for i := range players {
			players[i].Vote = ev.Options[i%len(ev.Options)].Key
		}
		ResolveMatter(r, MatterInput{MatterID: int64(n), Event: ev, Realm: realm, Sectors: sectors, Senators: sen, Players: players})
	}
}

func BenchmarkBestVoteOffers(b *testing.B) {
	sen, _, realm, _ := benchCourt()
	for n := 0; n < b.N; n++ {
		ev := MatterEvents[n%len(MatterEvents)]
		BestVoteOffers(ev, sen, 20, realm, int64(n), nil, 0)
	}
}
