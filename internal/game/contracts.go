package game

import (
	"fmt"
	"math"
)

// ---- Senate contracts and the leader of a vote ----
//
// A few options that grow an industry carry a contract: if the option
// passes and works, the biggest investor in that industry who voted for it
// is paid for the work. And whoever first voted for the winning side led
// the Senate there: they take the credit if it works, and the blame if its
// roll fails. Placeholder numbers — tune freely.

// contractOptions are the options with a contract, as "event/option", and
// the industry whose biggest investor gets it. Each option's success must
// grow that industry (TestContractsGrowTheirSector).
var contractOptions = map[string]SectorKey{
	"river_floods/levees":  SectorAgriculture,
	"forest_fires/replant": SectorTimber,
	"craft_fire/rebuild":   SectorAlchemy,
	"pirates/fleet":        SectorTimber,
	"kesh_envoys/sign":     SectorTrade,
	"veterans/grant":       SectorAgriculture,
	"silver_vein/sell":     SectorMining,
	"star_fall/mine":       SectorMining,
}

// contractDays is how many days of the new capacity's income a contract pays.
const contractDays = 10

// Contract is the industry an option's contract is for, if it has one.
func Contract(eventKey, optKey string) (SectorKey, bool) {
	k, ok := contractOptions[eventKey+"/"+optKey]
	return k, ok
}

// ContractPay is what a contract pays for an outcome: contractDays of the
// income the industry's new capacity earns.
func ContractPay(out MatterOutcome, k SectorKey) int {
	c := out.Sectors[k].Capacity
	if c <= 0 {
		return 0
	}
	return int(math.Round(c * IncomeScale * contractDays))
}

// ContractHint is the line under an option with a contract.
func ContractHint(k SectorKey) string {
	d, _ := SectorByKey(k)
	return fmt.Sprintf("📜 A contract goes to the biggest investor in %s %s who votes for this.", d.Emoji, d.Name)
}

// ContractWinner is who gets a contract: of the stakes held by the option's
// voters, the biggest (ties to the lower ID). ok is false if no voter has one.
func ContractWinner(stakes map[int64]float64) (int64, bool) {
	var best int64
	top := 0.0
	for id, s := range stakes {
		if s > top || (s == top && s > 0 && id < best) {
			best, top = id, s
		}
	}
	return best, top > 0
}

// The leader of a vote: the first player to vote for the winning side.
const (
	leaderRenown    = 5.0
	leaderInfluence = 1.0  // through ScaleInfluence
	leaderBlame     = -5.0 // renown, when its roll fails
	leaderBlameOp   = -2.0 // from each senator who voted against it
)

// LeaderResult is what leading the Senate to a decision does for the leader,
// and how each senator who voted against it takes a failure (0 = not at all).
func LeaderResult(succeeded bool, leaderInfl float64) (Delta, float64) {
	if !succeeded {
		return Delta{Renown: leaderBlame}, leaderBlameOp
	}
	return Delta{Renown: leaderRenown, Influence: ScaleInfluence(leaderInfluence, leaderInfl)}, 0
}
