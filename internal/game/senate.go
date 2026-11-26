package game

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"
)

// SenateTiming controls the daily matter cycle.
type SenateTiming struct {
	FirstDelay time.Duration // after /start, before the first matter
	Interval   time.Duration // between matters
	VoteWindow time.Duration // how long voting stays open
}

var (
	DefaultSenateTiming = SenateTiming{FirstDelay: time.Hour, Interval: 24 * time.Hour, VoteWindow: 12 * time.Hour}
	FastSenateTiming    = SenateTiming{FirstDelay: 30 * time.Second, Interval: 6 * time.Minute, VoteWindow: 4 * time.Minute}
)

// SenateSize is how many AI senators each court gets.
const SenateSize = 8

// WinningSideInfluence is what every player on the winning side gains.
const WinningSideInfluence = 0.5

// ---- leans and traits ----

// Lean is a quality of an option that senators like or dislike.
type Lean string

const (
	LeanCost      Lean = "cost"      // spends the treasury (derived)
	LeanProfit    Lean = "profit"    // fills the treasury (derived)
	LeanRisk      Lean = "risk"      // outcome is a gamble (derived)
	LeanMilitary  Lean = "military"  // uses force
	LeanTradition Lean = "tradition" // the old ways, the status quo
	LeanForeign   Lean = "foreign"   // friendly to outsiders
	LeanPopular   Lean = "popular"   // good for the common people
	LeanScheme    Lean = "scheme"    // underhanded
)

type Trait string

const (
	TraitConservative  Trait = "conservative"
	TraitRiskTaker     Trait = "risk_taker"
	TraitEconomical    Trait = "economical"
	TraitBigSpender    Trait = "big_spender"
	TraitScheming      Trait = "scheming"
	TraitHawk          Trait = "hawk"
	TraitDove          Trait = "dove"
	TraitPopulist      Trait = "populist"
	TraitIsolationist  Trait = "isolationist"
	TraitCosmopolitan  Trait = "cosmopolitan"
	TraitGreedy        Trait = "greedy"
	TraitIncorruptible Trait = "incorruptible"
)

type TraitInfo struct {
	Key       Trait
	Name      string
	Desc      string
	Likes     map[Lean]float64
	Conflicts []Trait
}

var TraitList = []TraitInfo{
	{TraitConservative, "Conservative", "Favors the old ways and dislikes gambles.",
		map[Lean]float64{LeanTradition: 2, LeanRisk: -1.5}, []Trait{TraitRiskTaker}},
	{TraitRiskTaker, "Risk-taker", "Loves a bold gamble.",
		map[Lean]float64{LeanRisk: 2, LeanTradition: -0.5}, []Trait{TraitConservative}},
	{TraitEconomical, "Economical", "Hates spending the treasury's money.",
		map[Lean]float64{LeanCost: -2, LeanProfit: 1}, []Trait{TraitBigSpender}},
	{TraitBigSpender, "Big spender", "Thinks money is for spending.",
		map[Lean]float64{LeanCost: 1.5, LeanPopular: 0.5}, []Trait{TraitEconomical}},
	{TraitScheming, "Scheming", "Prefers the knife in the dark to the open hand.",
		map[Lean]float64{LeanScheme: 2.5}, []Trait{TraitIncorruptible}},
	{TraitHawk, "Hawk", "Answers every problem with soldiers.",
		map[Lean]float64{LeanMilitary: 2}, []Trait{TraitDove}},
	{TraitDove, "Dove", "Avoids bloodshed and seeks peace abroad.",
		map[Lean]float64{LeanMilitary: -1.5, LeanForeign: 1}, []Trait{TraitHawk}},
	{TraitPopulist, "Populist", "Courts the common people.",
		map[Lean]float64{LeanPopular: 2}, nil},
	{TraitIsolationist, "Isolationist", "Distrusts foreigners.",
		map[Lean]float64{LeanForeign: -2}, []Trait{TraitCosmopolitan}},
	{TraitCosmopolitan, "Cosmopolitan", "Admires foreign ways and foreign friends.",
		map[Lean]float64{LeanForeign: 2}, []Trait{TraitIsolationist}},
	{TraitGreedy, "Greedy", "Likes profit, and is cheap to bribe.",
		map[Lean]float64{LeanProfit: 1.5}, []Trait{TraitIncorruptible}},
	{TraitIncorruptible, "Incorruptible", "Cannot be bribed and despises scheming.",
		map[Lean]float64{LeanScheme: -1.5}, []Trait{TraitGreedy, TraitScheming}},
}

func TraitByKey(t Trait) (TraitInfo, bool) {
	for _, ti := range TraitList {
		if ti.Key == t {
			return ti, true
		}
	}
	return TraitInfo{}, false
}

// ---- senators ----

type Senator struct {
	ID        int64
	Name      string
	Traits    []Trait
	Influence float64
	BasePrice int // base bribe price; actual price depends on how they lean
}

func (s Senator) Has(t Trait) bool {
	for _, x := range s.Traits {
		if x == t {
			return true
		}
	}
	return false
}

func (s Senator) TraitNames() string {
	var names []string
	for _, t := range s.Traits {
		if ti, ok := TraitByKey(t); ok {
			names = append(names, ti.Name)
		}
	}
	return strings.Join(names, ", ")
}

var senatorNames = []string{
	"Aurelia Vance", "Cassian Morrow", "Dorian Halbrecht", "Elara Quint", "Fenwick Ashdown",
	"Galen Strake", "Helena Corvaine", "Isidore Pell", "Junia Marlowe", "Lucan Thorne",
	"Maren Ostric", "Octavian Reyes", "Petronella Vask", "Quintus Harrow", "Rosalind Crane",
	"Severin Dusk", "Tullia Brand", "Varro Kell",
}

// GenerateSenate makes n senators with distinct names and 2 compatible traits each.
func GenerateSenate(r Roller, n int) []Senator {
	perm := shuffle(r, len(senatorNames))
	out := make([]Senator, 0, n)
	for i := 0; i < n && i < len(perm); i++ {
		s := Senator{
			Name:      senatorNames[perm[i]],
			Influence: float64(8 + r.IntN(9)), // 8–16
			BasePrice: 15 + r.IntN(21),        // 15–35
		}
		for len(s.Traits) < 2 {
			t := TraitList[r.IntN(len(TraitList))]
			if s.Has(t.Key) || conflicts(s.Traits, t) {
				continue
			}
			s.Traits = append(s.Traits, t.Key)
		}
		out = append(out, s)
	}
	return out
}

func conflicts(have []Trait, t TraitInfo) bool {
	for _, h := range have {
		for _, c := range t.Conflicts {
			if h == c {
				return true
			}
		}
	}
	return false
}

func shuffle(r Roller, n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := r.IntN(i + 1)
		p[i], p[j] = p[j], p[i]
	}
	return p
}

// ---- how senators decide ----

// noise is a small, fixed per-senator-per-matter wobble so votes aren't
// perfectly predictable but bribe prices stay stable while voting is open.
func noise(matterID, senatorID int64, opt string) float64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%d:%d:%s", matterID, senatorID, opt)
	return float64(h.Sum64()%2001)/1000 - 1 // [-1, 1]
}

// Score is how much a senator likes an option.
func (s Senator) Score(opt MatterOption, realm Realm, matterID int64) float64 {
	leans := opt.AllLeans(realm)
	score := 0.0
	for _, t := range s.Traits {
		ti, _ := TraitByKey(t)
		for l, w := range ti.Likes {
			score += w * leans[l]
		}
	}
	// Every senator cares a little about the realm, most about whatever is low.
	for a, d := range opt.ExpectedRealm(realm) {
		need := (TrackerMax - realm[a]) / 50
		score += 0.06 * d * need
	}
	return score + noise(matterID, s.ID, opt.Key)
}

// Preferred is the option the senator votes for unless bribed.
func (s Senator) Preferred(ev MatterEvent, realm Realm, matterID int64) string {
	best, bestScore := "", math.Inf(-1)
	for _, o := range ev.Options {
		if sc := s.Score(o, realm, matterID); sc > bestScore {
			best, bestScore = o.Key, sc
		}
	}
	return best
}

// BribePrice is what it costs to buy this senator's vote for an option.
// The further it is from what they want, the more it costs. ok is false if
// they can't be bought.
func (s Senator) BribePrice(ev MatterEvent, optKey string, realm Realm, matterID int64) (price int, ok bool) {
	if s.Has(TraitIncorruptible) {
		return 0, false
	}
	pref := s.Preferred(ev, realm, matterID)
	base := float64(s.BasePrice)
	if s.Has(TraitGreedy) {
		base *= 0.5
	}
	if pref == optKey {
		return int(math.Ceil(base / 2)), true
	}
	var prefOpt, want MatterOption
	for _, o := range ev.Options {
		if o.Key == pref {
			prefOpt = o
		}
		if o.Key == optKey {
			want = o
		}
	}
	gap := s.Score(prefOpt, realm, matterID) - s.Score(want, realm, matterID)
	return int(math.Ceil(base * (1 + gap))), true
}

// VoteOffer is what this senator will pay a player for their vote. They
// always want it for their preferred option. The offer grows with the
// player's influence and with how strongly the senator cares. ok is false
// for senators who don't buy votes.
func (s Senator) VoteOffer(ev MatterEvent, voterInfluence float64, realm Realm, matterID int64) (opt string, gold int, ok bool) {
	if s.Has(TraitIncorruptible) {
		return "", 0, false
	}
	opt = s.Preferred(ev, realm, matterID)
	best, second := math.Inf(-1), math.Inf(-1)
	for _, o := range ev.Options {
		sc := s.Score(o, realm, matterID)
		if sc > best {
			best, second = sc, best
		} else if sc > second {
			second = sc
		}
	}
	care := math.Min(best-second, 3) // how much winning matters to them
	mult := 1.0
	switch {
	case s.Has(TraitBigSpender):
		mult = 1.5
	case s.Has(TraitEconomical), s.Has(TraitGreedy):
		mult = 0.6
	}
	gold = int(math.Ceil((1 + care) * voterInfluence * VoteOfferRate * mult))
	return opt, gold, gold > 0
}

// VoteOfferRate is gold per point of influence a senator pays for a vote
// they mildly care about.
const VoteOfferRate = 1.2

// BestVoteOffers is, per option, the senator paying the most for it.
func BestVoteOffers(ev MatterEvent, senators []Senator, voterInfluence float64, realm Realm, matterID int64) map[string]VoteOfferFrom {
	out := map[string]VoteOfferFrom{}
	for _, s := range senators {
		opt, gold, ok := s.VoteOffer(ev, voterInfluence, realm, matterID)
		if ok && gold > out[opt].Gold {
			out[opt] = VoteOfferFrom{Senator: s, Gold: gold}
		}
	}
	return out
}

type VoteOfferFrom struct {
	Senator Senator
	Gold    int
}

// ---- picking the day's matter ----

// PickMatter chooses a random non-follow-up event, avoiding recent ones and
// favoring events whose Bias trackers are low.
func PickMatter(r Roller, realm Realm, recent []string) MatterEvent {
	skip := map[string]bool{}
	for _, k := range recent {
		skip[k] = true
	}
	var pool []MatterEvent
	var weights []float64
	total := 0.0
	for _, e := range MatterEvents {
		if e.FollowUpOnly || skip[e.Key] {
			continue
		}
		w := 1.0
		for a, b := range e.Bias {
			w += b * (50 - realm[a]) / 50
		}
		w = math.Max(w, 0.2)
		pool = append(pool, e)
		weights = append(weights, w)
		total += w
	}
	x := float64(r.IntN(10000)) / 10000 * total
	for i, w := range weights {
		if x < w {
			return pool[i]
		}
		x -= w
	}
	return pool[len(pool)-1]
}

// ---- resolution ----

// Player is a court member as the senate sees them.
type Player struct {
	Person
	Role   string
	Vote   string // option key, "" if they didn't vote
	Weight float64
}

type MatterInput struct {
	MatterID int64
	Event    MatterEvent
	Realm    Realm
	Senators []Senator
	Bribes   map[int64]string // senator ID -> option they were paid to vote for
	Players  []Player
}

type CastVote struct {
	Name   string
	Option string
	Weight float64
	AI     bool
}

type QueuedEvent struct {
	Event string
	Days  int
}

type MatterResult struct {
	Winner       MatterOption
	Tally        map[string]float64
	Votes        []CastVote
	Succeeded    bool // the option's roll, if it had one
	Outcome      MatterOutcome
	Realm        Realm // after
	Fallen       bool
	FallenReason string
	Deltas       map[int64]Delta
	Notices      map[int64][]string
	FollowUps    []QueuedEvent
}

// ResolveMatter counts the votes, picks the winner, rolls its outcome and
// works out what happens to the realm and to each player.
func ResolveMatter(r Roller, in MatterInput) MatterResult {
	ev := in.Event
	res := MatterResult{Tally: map[string]float64{}, Deltas: map[int64]Delta{}, Notices: map[int64][]string{}}

	for _, s := range in.Senators {
		opt := s.Preferred(ev, in.Realm, in.MatterID)
		if b, ok := in.Bribes[s.ID]; ok {
			opt = b
		}
		res.Tally[opt] += s.Influence
		res.Votes = append(res.Votes, CastVote{Name: s.Name, Option: opt, Weight: s.Influence, AI: true})
	}
	for _, p := range in.Players {
		if _, ok := ev.Option(p.Vote); !ok {
			continue
		}
		res.Tally[p.Vote] += p.Weight
		res.Votes = append(res.Votes, CastVote{Name: p.Name, Option: p.Vote, Weight: p.Weight})
	}
	sort.SliceStable(res.Votes, func(i, j int) bool { return res.Votes[i].Weight > res.Votes[j].Weight })

	// Highest weight wins; ties go to the default (status quo).
	winner, _ := ev.Option(ev.Default)
	best := res.Tally[winner.Key]
	for _, o := range ev.Options {
		if res.Tally[o.Key] > best {
			winner, best = o, res.Tally[o.Key]
		}
	}
	res.Winner = winner

	res.Outcome, res.Succeeded = winner.Success, true
	if winner.Check != nil {
		if ok, _ := winner.Check.Roll(r, in.Realm.Sheet(), 0, Normal); !ok {
			res.Outcome, res.Succeeded = winner.Failure, false
		}
	}
	out := res.Outcome

	res.Realm = in.Realm.Clone()
	res.Realm.Apply(out.Realm)
	res.Fallen, res.FallenReason = res.Realm.Fallen()
	if !res.Fallen {
		res.Realm.Apply(res.Realm.Drift())
	}

	for _, p := range in.Players {
		var d Delta
		d.Merge(out.All)
		if rd, ok := out.Roles[p.Role]; ok {
			d.Merge(rd)
		}
		if p.Vote == winner.Key {
			d.Merge(Delta{Influence: WinningSideInfluence})
			d.Merge(out.Supporters)
		}
		if desc := d.Describe(); desc != "" {
			res.Deltas[p.ID] = d
			res.Notices[p.ID] = append(res.Notices[p.ID], fmt.Sprintf("The Senate voted to **%s**. For you: %s", strings.ToLower(winner.Label), desc))
		}
	}

	if !res.Fallen {
		for _, f := range out.FollowUps {
			if float64(r.IntN(100)) < f.Chance*100 {
				res.FollowUps = append(res.FollowUps, QueuedEvent{Event: f.Event, Days: f.Days})
			}
		}
	}
	return res
}
