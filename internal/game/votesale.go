package game

import (
	"fmt"
	"math"
	"time"
)

// ---- Selling your vote to a senator: the risk of a scandal ----
//
// A senator only pays for the option they already want, so a sale would be
// free gold whenever it matches your own mind. The cost is the chance that
// it comes out. Bigger bribes are harder to hide, a habit of selling gets
// noticed, enemy spies are watching, and Intrigue covers your tracks. Only
// a scandal costs anything: the Senate never learns of a clean sale.

const (
	scandalBase        = 8.0
	scandalPerDoubling = 6.0  // per doubling of gold/scandalGoldUnit
	scandalGoldUnit    = 25.0 // gold
	scandalPerRecent   = 7.0  // per other recent sale
	scandalPerSpy      = 0.2  // per point of the strongest hostile spy network
	scandalPerIntrigue = 2.5  // off, per point of Intrigue
	scandalMinChance   = 3
	scandalMaxChance   = 60
	scandalRenown      = 1.5
	scandalInfluence   = 1.0
	scandalOpinion     = -6.0  // senators who wanted another outcome
	scandalOutraged    = -12.0 // Incorruptible and Stern senators
	VoteSaleMemoryDays = 3     // game days a sale counts toward the habit
)

// VoteSaleMemory is how far back recent sales are counted.
func VoteSaleMemory(t Timings) time.Duration { return VoteSaleMemoryDays * t.Day }

// VoteSaleInput is everything that decides whether a sale comes out.
type VoteSaleInput struct {
	Seller   Person
	Senator  Senator
	Matter   MatterEvent
	Option   string // the option sold for
	Realm    Realm  // the trackers, to know what each senator wanted
	MatterID int64
	Gold     int
	Recent   int     // other votes sold to senators lately
	Spies    float64 // the strongest spy network another player has on the seller
}

// VoteScandalChance is the hidden percent chance a sale comes out.
func VoteScandalChance(in VoteSaleInput) int {
	f := scandalBase +
		scandalPerDoubling*math.Log2(1+float64(max(in.Gold, 0))/scandalGoldUnit) +
		scandalPerRecent*float64(max(in.Recent, 0)) +
		scandalPerSpy*math.Max(in.Spies, 0) -
		scandalPerIntrigue*in.Seller.Sheet[AttrIntrigue]
	return min(max(int(f), scandalMinChance), scandalMaxChance)
}

type VoteSaleResult struct {
	Caught   bool
	Delta    Delta
	Opinions []OpinionChange
	Text     string // to the seller, when caught
	Public   string // posted when caught
}

// ResolveVoteSale rolls whether a sale comes out. When it does, the seller
// loses renown and influence, and every senator who wanted a different
// outcome thinks less of them. The buyer and those who agreed don't mind.
func ResolveVoteSale(r Roller, in VoteSaleInput, senators []Senator) VoteSaleResult {
	if r.IntN(100) >= VoteScandalChance(in) {
		return VoteSaleResult{}
	}
	res := VoteSaleResult{
		Caught: true,
		Delta:  Delta{Renown: -scandalRenown, Influence: -scandalInfluence},
		Text:   "Word got out. Half the Forum knows whose gold bought your vote, and the senators who wanted another outcome won't forget it.",
		Public: fmt.Sprintf("**%s** took %s's gold for their vote on *%s*.", in.Seller.Name, in.Senator.Name, in.Matter.Title),
	}
	for _, s := range senators {
		if s.ID == in.Senator.ID || s.Preferred(in.Matter, in.Realm, in.MatterID) == in.Option {
			continue
		}
		d := scandalOpinion
		if s.Has(TraitIncorruptible) || s.Has(TraitStern) {
			d = scandalOutraged
		}
		res.Opinions = append(res.Opinions, OpinionChange{SenatorID: s.ID, CharacterID: in.Seller.ID, Delta: d})
	}
	return res
}
