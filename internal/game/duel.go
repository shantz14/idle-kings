package game

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// KindDuel is the activity kind for Challenge to a Duel. The challenger is
// the host, the defender is the activity's target and the wager is its
// spend. Its goal holds the state: pending, then accepted.
const KindDuel = "duel"

const (
	DuelPending  = "pending"
	DuelAccepted = "accepted"
)

// DuelTiming controls a duel: how long the defender has to answer, how long
// the bout lasts once accepted, and when the stance choice arrives.
type DuelTiming struct {
	Answer         time.Duration
	Bout           time.Duration
	StanceEarliest time.Duration
	StanceLatest   time.Duration
	StanceWindow   time.Duration
}

var (
	DefaultDuelTiming = DuelTiming{Answer: 4 * time.Hour, Bout: 30 * time.Minute,
		StanceEarliest: 5 * time.Minute, StanceLatest: 15 * time.Minute, StanceWindow: 10 * time.Minute}
	FastDuelTiming = DuelTiming{Answer: 2 * time.Minute, Bout: 2 * time.Minute,
		StanceEarliest: 20 * time.Second, StanceLatest: 40 * time.Second, StanceWindow: 60 * time.Second}
)

// Wagers and bets, in gold. Placeholders to tune.
var (
	DuelWagers = []int{0, 25, 50, 100}
	DuelBets   = []int{10, 25, 50, 100}
)

const (
	DuelMaxWager = 1000
	DuelMaxBet   = 1000
)

// ---- stances ----

const (
	StanceAttack = "attack"
	StanceGuard  = "guard"
	StanceTrick  = "trick"
	StanceYield  = "yield"

	// DuelStanceEvent is the DM each duelist gets mid-bout.
	DuelStanceEvent = "duel_stance"
)

var DuelEvents = []PartyEvent{
	{
		Key: DuelStanceEvent, Title: "Choose your stance",
		Prompt: "Steel rings on steel. Your opponent circles, looking for an opening. How do you fight?",
		Choices: []EventChoice{
			{Key: StanceAttack, Label: "Press the attack", Success: EventOutcome{Text: "You go at them hard."}},
			{Key: StanceGuard, Label: "Hold your guard", Success: EventOutcome{Text: "You wait for them to come to you."}},
			{Key: StanceTrick, Label: "Try a trick", Success: EventOutcome{Text: "You feint, and look for something clever."}},
			{Key: StanceYield, Label: "Yield", Success: EventOutcome{Text: "You lower your blade. Better shamed than bleeding."}},
		},
		Default: StanceGuard,
	},
}

// stanceBeats: attack beats trick, trick beats guard, guard beats attack.
func stanceBeats(a, b string) bool {
	return (a == StanceAttack && b == StanceTrick) || (a == StanceTrick && b == StanceGuard) || (a == StanceGuard && b == StanceAttack)
}

const duelStanceBonus = 15.0

// StanceMod is the challenger's bonus (or penalty) from the two stances.
func StanceMod(challenger, defender string) float64 {
	switch {
	case stanceBeats(challenger, defender):
		return duelStanceBonus
	case stanceBeats(defender, challenger):
		return -duelStanceBonus
	}
	return 0
}

// ---- the bout ----

// DuelChance is the percent chance the challenger wins, before the roll.
func DuelChance(challenger, defender Sheet, mod float64) int {
	f := 50 + 5*(challenger[AttrProwess]-defender[AttrProwess]) + 1*(challenger[AttrMartial]-defender[AttrMartial]) + mod
	return int(math.Max(MinChance, math.Min(MaxChance, f)))
}

// Placeholders to tune.
const (
	duelRenown          = 1.0 // the loser's renown that goes to the winner
	duelUpsetRenown     = 0.5 // extra for beating a better fighter
	duelDecisive        = 30  // a margin this large always wounds the loser
	duelWoundChance     = 25  // percent, for a closer loss
	DuelDeclineRenown   = 0.3 // lost by refusing a challenge
	duelHawkWinner      = 4.0 // senator opinions after the bout
	duelHawkBraveLoser  = 1.0
	duelRiskTakerWinner = 3.0
	duelDoveBoth        = -3.0
	duelHawkDecline     = -3.0
)

// Bet is a spectator's gold on one duelist.
type Bet struct {
	CharacterID int64
	SideID      int64 // the duelist they backed
	Amount      int
}

type DuelInput struct {
	Challenger, Defender Person
	ChallengerRenown     float64 // so the loser never loses renown they don't have
	DefenderRenown       float64
	ChallengerStance     string // "" = guard
	DefenderStance       string
	Wager                int // each duelist's stake
	Bets                 []Bet
	Senators             []Senator
	Laws                 Laws
}

type DuelResult struct {
	Winner, Loser Person
	Yielded       bool
	Wounded       bool // the loser
	Deltas        map[int64]Delta
	Opinions      []OpinionChange
	Conditions    []ConditionChange
	Notices       map[int64][]string
	Public        string
	Realm         Realm // the court's cut of the bets, under the Code of Honor
}

// ResolveDuel fights the bout. All rolls are hidden.
func ResolveDuel(r Roller, in DuelInput) DuelResult {
	cs, ds := orGuard(in.ChallengerStance), orGuard(in.DefenderStance)
	res := DuelResult{Deltas: map[int64]Delta{}, Notices: map[int64][]string{}}
	add := func(id int64, d Delta) {
		cur := res.Deltas[id]
		cur.Merge(d)
		res.Deltas[id] = cur
	}

	challengerWins, margin := false, 0
	switch {
	case cs == StanceYield:
		res.Yielded = true
	case ds == StanceYield:
		res.Yielded, challengerWins = true, true
	default:
		margin = Margin(r, DuelChance(in.Challenger.Sheet, in.Defender.Sheet, StanceMod(cs, ds)), Normal)
		challengerWins = margin > 0
	}
	res.Winner, res.Loser = in.Defender, in.Challenger
	loserRenown := in.ChallengerRenown
	if challengerWins {
		res.Winner, res.Loser = in.Challenger, in.Defender
		loserRenown = in.DefenderRenown
	}
	w, l := res.Winner, res.Loser

	// Renown changes hands.
	loss := math.Min(duelRenown, math.Max(loserRenown, 0))
	if res.Yielded {
		loss /= 2
	}
	gain := loss
	if !res.Yielded && w.Sheet[AttrProwess] < l.Sheet[AttrProwess] {
		gain += duelUpsetRenown
	}
	if in.Laws.Has(LawHonor) {
		gain *= LawHonorRenown
	}
	add(w.ID, Delta{Renown: gain, Wealth: 2 * in.Wager})
	add(l.ID, Delta{Renown: -loss})
	add(w.ID, Practice(Prowess, true))
	add(l.ID, Practice(Prowess, false))

	// Wounds.
	if !res.Yielded && (abs(margin) >= duelDecisive || r.IntN(100) < duelWoundChance) {
		res.Wounded = true
		res.Conditions = append(res.Conditions, ConditionChange{CharacterID: l.ID, Key: CondWounded})
	}

	// Senators.
	for _, s := range in.Senators {
		var dw, dl float64
		if s.Has(TraitHawk) {
			dw += duelHawkWinner
			if !res.Yielded {
				dl += duelHawkBraveLoser
			}
		}
		if s.Has(TraitRiskTaker) {
			dw += duelRiskTakerWinner
		}
		if s.Has(TraitDove) {
			dw += duelDoveBoth
			dl += duelDoveBoth
		}
		if dw != 0 {
			res.Opinions = append(res.Opinions, OpinionChange{SenatorID: s.ID, CharacterID: w.ID, Delta: dw})
		}
		if dl != 0 {
			res.Opinions = append(res.Opinions, OpinionChange{SenatorID: s.ID, CharacterID: l.ID, Delta: dl})
		}
	}

	// Bets.
	payouts := BetPayouts(in.Bets, w.ID)
	cut := 0.0
	if in.Laws.Has(LawHonor) {
		cut = LawHonorBetCut
	}
	kept := 0
	for _, b := range in.Bets {
		got := payouts[b.CharacterID]
		if b.SideID == w.ID && cut > 0 {
			c := int(float64(got) * cut)
			got -= c
			kept += c
		}
		if got > 0 {
			add(b.CharacterID, Delta{Wealth: got})
		}
		switch {
		case b.SideID == w.ID:
			res.Notices[b.CharacterID] = append(res.Notices[b.CharacterID],
				fmt.Sprintf("You bet %d gold on %s, and won. (+%d gold)", b.Amount, w.Name, got))
		case got > 0:
			res.Notices[b.CharacterID] = append(res.Notices[b.CharacterID],
				fmt.Sprintf("You bet %d gold on %s. Nobody backed the winner, so every bet was returned.", b.Amount, l.Name))
		default:
			res.Notices[b.CharacterID] = append(res.Notices[b.CharacterID],
				fmt.Sprintf("You bet %d gold on %s, and lost it.", b.Amount, l.Name))
		}
	}

	if kept > 0 {
		res.Realm = Realm{AttrTreasury: float64(kept) / GoldPerTreasury}
	}

	// Words.
	how := "wins the duel"
	switch {
	case res.Yielded:
		how = "wins the duel when their opponent yields"
	case abs(margin) >= duelDecisive:
		how = "wins the duel decisively"
	case abs(margin) < 10:
		how = "wins the duel by a hair"
	}
	res.Public = fmt.Sprintf("**%s** %s against **%s**.", w.Name, how, l.Name)
	if res.Wounded {
		res.Public += fmt.Sprintf(" %s is carried off the field, wounded.", l.Name)
	}
	if in.Wager > 0 {
		res.Public += fmt.Sprintf(" The %d gold purse goes to the victor.", 2*in.Wager)
	}
	wd, ld := res.Deltas[w.ID], res.Deltas[l.ID]
	res.Notices[w.ID] = append(res.Notices[w.ID], withEffects(fmt.Sprintf("You defeated %s.", l.Name), wd))
	lose := fmt.Sprintf("%s defeated you.", w.Name)
	if res.Yielded {
		lose = fmt.Sprintf("You yielded to %s.", w.Name)
	}
	if res.Wounded {
		lose += " You are wounded, and will be weaker for a few days."
	}
	res.Notices[l.ID] = append(res.Notices[l.ID], withEffects(lose, ld))
	return res
}

func orGuard(s string) string {
	switch s {
	case StanceAttack, StanceGuard, StanceTrick, StanceYield:
		return s
	}
	return StanceGuard
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// BetPayouts splits the whole pot among those who backed the winner, in
// proportion to their stakes. Gold is conserved exactly: leftovers from
// rounding go to the biggest winning bets. If nobody backed the winner,
// everyone gets their stake back.
func BetPayouts(bets []Bet, winnerID int64) map[int64]int {
	out := map[int64]int{}
	pot, backed := 0, 0
	for _, b := range bets {
		pot += b.Amount
		if b.SideID == winnerID {
			backed += b.Amount
		}
	}
	if backed == 0 {
		for _, b := range bets {
			out[b.CharacterID] += b.Amount
		}
		return out
	}
	var winners []Bet
	paid := 0
	for _, b := range bets {
		if b.SideID == winnerID {
			n := pot * b.Amount / backed
			out[b.CharacterID] += n
			paid += n
			winners = append(winners, b)
		}
	}
	sort.SliceStable(winners, func(i, j int) bool { return winners[i].Amount > winners[j].Amount })
	for i := 0; paid < pot; i = (i + 1) % len(winners) {
		out[winners[i].CharacterID]++
		paid++
	}
	return out
}

// DeclineOpinions is how senators react to a refused challenge. Under the
// Ban on Dueling nobody minds; under the Code of Honor the Stern and the
// Conservative join the Hawks.
func DeclineOpinions(senators []Senator, defenderID int64, ls Laws) []OpinionChange {
	if ls.Has(LawDuelBan) {
		return nil
	}
	var out []OpinionChange
	for _, s := range senators {
		d := 0.0
		if s.Has(TraitHawk) {
			d += duelHawkDecline
		}
		if ls.Has(LawHonor) && (s.Has(TraitHawk) || s.Has(TraitStern) || s.Has(TraitConservative)) {
			d += LawHonorDeclineOpinion
		}
		if d != 0 {
			out = append(out, OpinionChange{SenatorID: s.ID, CharacterID: defenderID, Delta: d})
		}
	}
	return out
}
