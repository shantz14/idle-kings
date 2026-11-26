package game

import (
	"fmt"
	"math"
	"time"
)

// Opinion is what one senator thinks of one player, from -100 to 100.
// Players see the number; what it does is hidden in the background:
// bribe prices, what senators pay for your vote, whether they follow your
// lead in the Senate, and how well courting them goes.

const (
	OpinionMin = -100.0
	OpinionMax = 100.0
)

func ClampOpinion(v float64) float64 { return math.Max(OpinionMin, math.Min(OpinionMax, v)) }

// OpinionChange moves one senator's opinion of one player. SenatorID 0
// means every senator at the player's court.
type OpinionChange struct {
	SenatorID   int64
	CharacterID int64
	Delta       float64
}

// Opinions is a player's standing with each senator, by senator ID.
// Missing senators count as 0.
type Opinions map[int64]float64

// CourtOpinions is every senator's opinion of every player: senator ID →
// character ID → opinion.
type CourtOpinions map[int64]Opinions

func (c CourtOpinions) Of(senatorID, charID int64) float64 { return c[senatorID][charID] }

// ---- how opinion shows ----

// OpinionWord is a one-word description of an opinion.
func OpinionWord(v float64) string {
	switch {
	case v <= -60:
		return "Hostile"
	case v <= -20:
		return "Unfriendly"
	case v < 20:
		return "Indifferent"
	case v < 60:
		return "Friendly"
	default:
		return "Devoted"
	}
}

// OpinionNum is a signed whole number, e.g. "+35", "-20", "0".
func OpinionNum(v float64) string {
	n := int(math.Round(v))
	if n > 0 {
		return fmt.Sprintf("+%d", n)
	}
	return fmt.Sprint(n)
}

// ---- what opinion does (all hidden) ----

// Thresholds below which a senator won't deal with you.
const (
	RefuseBribeBelow = -50.0 // won't take your gold
	RefuseBuyBelow   = -50.0 // won't buy your vote
)

// OpinionPriceFactor scales a bribe: half price at +100, 1.5× at -100.
func OpinionPriceFactor(op float64) float64 { return 1 - ClampOpinion(op)/200 }

// OpinionOfferFactor scales what a senator pays for your vote: 1.5× at +100,
// half at -100.
func OpinionOfferFactor(op float64) float64 { return 1 + ClampOpinion(op)/200 }

// OpinionCheckMod is the success bonus, in percent, for checks against a
// senator: ±20 at the extremes.
func OpinionCheckMod(op float64) float64 { return ClampOpinion(op) / 5 }

// Following: when a matter is decided, a senator's score for each option is
// nudged by the opinion they hold of the players who voted for it. Friends
// pull them along; people they dislike push them away.
const (
	followPer100 = 1.5 // score per player at ±100 opinion
	followCap    = 2.5 // most it can move one option's score
)

func followBonus(s Senator, opt string, players []Player, ops CourtOpinions) float64 {
	b := 0.0
	for _, p := range players {
		if p.Vote == opt {
			b += followPer100 * ops.Of(s.ID, p.ID) / 100
		}
	}
	return math.Max(-followCap, math.Min(followCap, b))
}

// BribePriceFor is BribePrice adjusted for the briber's standing. ok is
// false if they can't be bought, or won't take this player's gold.
func (s Senator) BribePriceFor(ev MatterEvent, optKey string, realm Realm, matterID int64, op float64) (price int, ok bool) {
	price, ok = s.BribePrice(ev, optKey, realm, matterID)
	if !ok || op < RefuseBribeBelow {
		return 0, false
	}
	return max(1, int(math.Ceil(float64(price)*OpinionPriceFactor(op)))), true
}

// VoteOfferFor is VoteOffer adjusted for the seller's standing.
func (s Senator) VoteOfferFor(ev MatterEvent, voterInfluence float64, realm Realm, matterID int64, op float64) (opt string, gold int, ok bool) {
	opt, gold, ok = s.VoteOffer(ev, voterInfluence, realm, matterID)
	if !ok || op < RefuseBuyBelow {
		return "", 0, false
	}
	gold = int(math.Ceil(float64(gold) * OpinionOfferFactor(op)))
	return opt, gold, gold > 0
}

// ---- what changes opinion ----

// Placeholders to tune. Kept small so no single act swings a senator far.
const (
	opinionVoteWith    = 2.0  // voting the same way, plus how much they cared
	opinionVoteAgainst = -1.0 // voting the other way, minus how much they cared
	opinionCareMax     = 3.0

	OpinionBribe       = 3.0 // a senator you paid
	OpinionBribeGreedy = 6.0
	OpinionSoldVote    = 4.0 // a senator you sold your vote to

	OpinionPartySuccess = 2.0  // every senator, when your party is the talk of the court
	OpinionPartyFailure = -1.0 // every senator, when it falls flat
	OpinionRumored      = -1.0 // every senator, when rumors about you stick

	// OpinionDecay is the share of every opinion kept each Senate day, so
	// old grudges and old favors fade.
	OpinionDecay = 0.97
)

// BribeOpinion is how much paying a senator warms them to you.
func BribeOpinion(s Senator) float64 {
	if s.Has(TraitGreedy) {
		return OpinionBribeGreedy
	}
	return OpinionBribe
}

// voteOpinion is how a senator feels about a player's vote: warmer if they
// voted together, cooler if not, more so the more the senator cared.
func voteOpinion(s Senator, ev MatterEvent, realm Realm, matterID int64, senVote, playerVote string) float64 {
	score := func(key string) float64 {
		o, _ := ev.Option(key)
		return s.Score(o, realm, matterID)
	}
	if senVote == playerVote {
		next := math.Inf(-1)
		for _, o := range ev.Options {
			if o.Key != senVote {
				next = math.Max(next, score(o.Key))
			}
		}
		care := math.Max(0, math.Min(score(senVote)-next, opinionCareMax))
		return opinionVoteWith + care
	}
	care := math.Max(0, math.Min(score(senVote)-score(playerVote), opinionCareMax))
	return opinionVoteAgainst - care
}

// ---- Court a Senator ----

// KindCourt is the activity kind for Court a Senator.
const KindCourt = "court"

// How long a visit to a senator takes.
const (
	DefaultCourtDuration = time.Hour
	FastCourtDuration    = 90 * time.Second
)

// Approach is how a player courts a senator.
type Approach struct {
	Key         string
	Name        string
	Description string
	Check       Check
	Gift        bool              // costs gold (see GiftCost)
	Likes       map[Trait]float64 // percent bonus (or penalty) by the senator's traits
	Insulted    []Trait           // these senators take offense outright
	Practice    Stat              // the stat this approach practises
}

var Approaches = []Approach{
	{
		Key: "flatter", Name: "Flatter", Description: "Praise their speeches and their family. Uses Diplomacy. Traditionalists enjoy it; schemers see through it.",
		Check:    Check{Base: 20, Weights: map[Attr]float64{AttrDiplomacy: 5, AttrRenown: 0.5}},
		Likes:    map[Trait]float64{TraitConservative: 10, TraitPopulist: 5, TraitPious: 5, TraitScheming: -10, TraitIncorruptible: -5},
		Practice: Diplomacy,
	},
	{
		Key: "gift", Name: "Bring a Gift", Description: "A fine gift, chosen with care. Costs gold. Uses Diplomacy and Stewardship. The greedy love it; never try it on the incorruptible.",
		Check: Check{Base: 35, Weights: map[Attr]float64{AttrDiplomacy: 3, AttrStewardship: 2}},
		Gift:  true,
		Likes: map[Trait]float64{TraitGreedy: 20, TraitBigSpender: 10, TraitMerchant: 10, TraitEconomical: -10},
		// Incorruptible senators read any gift as a bribe.
		Insulted: []Trait{TraitIncorruptible},
		Practice: Stewardship,
	},
	{
		Key: "gossip", Name: "Share Secrets", Description: "Trade whispers about the rest of the court. Uses Intrigue. Schemers love it; the incorruptible despise it.",
		Check:    Check{Base: 20, Weights: map[Attr]float64{AttrIntrigue: 5, AttrInfluence: 0.5}},
		Likes:    map[Trait]float64{TraitScheming: 15, TraitRiskTaker: 5, TraitIncorruptible: -15, TraitConservative: -5, TraitStern: -10, TraitPious: -5},
		Practice: Intrigue,
	},
	{
		Key: "war", Name: "Talk Strategy", Description: "Talk soldiers, borders and old campaigns. Uses Martial. Hawks love it; doves hate it.",
		Check:    Check{Base: 20, Weights: map[Attr]float64{AttrMartial: 5, AttrRenown: 0.5}},
		Likes:    map[Trait]float64{TraitHawk: 15, TraitIsolationist: 5, TraitStern: 5, TraitDove: -15, TraitCosmopolitan: -5},
		Practice: Martial,
	},
}

func ApproachByKey(k string) (Approach, bool) {
	for _, a := range Approaches {
		if a.Key == k {
			return a, true
		}
	}
	return Approach{}, false
}

// GiftCost is what a gift for this senator costs: they expect more the more
// a bribe would cost.
func GiftCost(s Senator) int { return 10 + s.BasePrice }

// Outcomes by margin. Positive gains shrink as opinion rises, so the last
// stretch to Devoted is the hardest.
const (
	courtGreatMargin = 30
	courtGreat       = 12.0
	courtGood        = 6.0
	courtBadMargin   = -30
	courtBad         = -4.0
	courtInsult      = -15.0
)

type CourtInput struct {
	Player   Person
	Senator  Senator
	Approach Approach
	Opinion  float64 // the senator's opinion of the player before courting
}

type CourtResult struct {
	Delta    float64 // opinion change
	Success  bool
	Practice Delta // stat gain from the attempt (none if insulted)
	Text     string
}

// CourtMod is the situational bonus for courting this senator this way.
func CourtMod(a Approach, s Senator, op float64) float64 {
	mod := OpinionCheckMod(op)
	for _, t := range s.Traits {
		mod += a.Likes[t]
	}
	return mod
}

// ResolveCourt decides how courting a senator went.
func ResolveCourt(r Roller, in CourtInput) CourtResult {
	s, a := in.Senator, in.Approach
	for _, t := range a.Insulted {
		if s.Has(t) {
			return CourtResult{Delta: courtInsult, Text: fmt.Sprintf("%s returns your gift unopened, and tells half the Senate you tried to buy them.", s.Name)}
		}
	}
	res := resolveCourt(r, in)
	if a.Practice != "" {
		res.Practice = Practice(a.Practice, res.Success)
	}
	return res
}

func resolveCourt(r Roller, in CourtInput) CourtResult {
	s, a := in.Senator, in.Approach
	ok, margin := a.Check.Roll(r, in.Player.Sheet, CourtMod(a, s, in.Opinion), Normal)
	scale := 1.0
	if in.Opinion > 0 {
		scale = 1 - in.Opinion/150
	}
	switch {
	case ok && margin >= courtGreatMargin:
		return CourtResult{Delta: courtGreat * scale, Success: true, Text: fmt.Sprintf("%s is delighted with you. They walk you to the door themselves.", s.Name)}
	case ok:
		return CourtResult{Delta: courtGood * scale, Success: true, Text: fmt.Sprintf("%s enjoyed your company.", s.Name)}
	case margin > courtBadMargin:
		return CourtResult{Text: fmt.Sprintf("%s was polite, but unmoved.", s.Name)}
	default:
		return CourtResult{Delta: courtBad, Text: fmt.Sprintf("You overstayed your welcome. %s was glad to see you go.", s.Name)}
	}
}
