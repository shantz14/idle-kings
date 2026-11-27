package game

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
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
	LeanFaith     Lean = "faith"     // honors the gods and the temples
	LeanLand      Lean = "land"      // good for the farms (derived from Agriculture)
	LeanCommerce  Lean = "commerce"  // good for trade (derived from Trade and Luxury Goods)
	LeanOrder     Lean = "order"     // good for law and order (derived from Stability)
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
	TraitReckless      Trait = "reckless"
	TraitLandowner     Trait = "landowner"
	TraitMerchant      Trait = "merchant_prince"
	TraitStern         Trait = "stern"
	TraitPious         Trait = "pious"
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
		map[Lean]float64{LeanTradition: 2, LeanRisk: -1.5}, []Trait{TraitRiskTaker, TraitReckless}},
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
	{TraitReckless, "Reckless", "Will gamble the realm on a good chance.",
		map[Lean]float64{LeanRisk: 1.5, LeanMilitary: 0.5}, []Trait{TraitConservative}},
	{TraitLandowner, "Landowner", "Owns half the countryside, and votes for the farms.",
		map[Lean]float64{LeanLand: 2}, nil},
	{TraitMerchant, "Merchant prince", "Made a fortune in trade, and votes for the markets.",
		map[Lean]float64{LeanCommerce: 2, LeanProfit: 0.5}, nil},
	{TraitStern, "Stern", "Stands for law and order above all.",
		map[Lean]float64{LeanOrder: 2, LeanScheme: -0.5}, []Trait{TraitScheming}},
	{TraitPious, "Pious", "Honors the gods and the old rites.",
		map[Lean]float64{LeanFaith: 2.5, LeanTradition: 0.5}, nil},
}

func TraitByKey(t Trait) (TraitInfo, bool) {
	return traitIndex.get(t)
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

// GenerateSenate draws n distinct personas (see personas.go), each with
// their own fixed traits, and gives them random influence and bribe prices.
func GenerateSenate(r Roller, n int) []Senator {
	perm := shuffle(r, len(Personas))
	out := make([]Senator, 0, n)
	for i := 0; i < n && i < len(perm); i++ {
		out = append(out, seat(r, Personas[perm[i]]))
	}
	return out
}

// seat makes a persona a senator, with random influence and bribe price.
func seat(r Roller, p Persona) Senator {
	return Senator{
		Name:      p.Name,
		Traits:    append([]Trait(nil), p.Traits...),
		Influence: float64(senatorInfluenceMin + r.IntN(senatorInfluenceSpan)),
		BasePrice: senatorPriceMin + r.IntN(senatorPriceSpan),
	}
}

// SenateKind is how the admin chose the Senate when starting the game.
type SenateKind string

const (
	SenateStandard SenateKind = "standard" // StandardSenate, the default
	SenateRandom   SenateKind = "random"   // SenateSize personas drawn at random
)

// StandardSenate is a balanced Senate: every opposing pair of traits is
// one against one, war is two Hawks against two Doves, and the farms, trade
// and law and order each have a champion (TestStandardSenateIsBalanced).
var StandardSenate = []string{
	"Julius Caesar",      // Hawk, Reckless
	"Tywin Lannister",    // Hawk, Economical
	"Saruman",            // Scheming, Big spender
	"Elminster",          // Cosmopolitan, Dove
	"Thorin Oakenshield", // Isolationist, Conservative
	"Eddard Stark",       // Incorruptible, Stern
	"Joan of Arc",        // Populist, Landowner
	"Midas",              // Dove, Merchant prince
}

// NewSenate seats a court's Senate. Each senator still gets a random
// influence and bribe price, even in the standard Senate.
func NewSenate(r Roller, kind SenateKind) []Senator {
	if kind == SenateRandom {
		return GenerateSenate(r, SenateSize)
	}
	out := make([]Senator, 0, len(StandardSenate))
	for _, name := range StandardSenate {
		if p, ok := PersonaByName(name); ok {
			out = append(out, seat(r, p))
		}
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
	// The bytes of "matterID:senatorID:opt", without fmt's allocations.
	var buf [64]byte
	b := strconv.AppendInt(buf[:0], matterID, 10)
	b = append(b, ':')
	b = strconv.AppendInt(b, senatorID, 10)
	b = append(b, ':')
	b = append(b, opt...)
	h := fnv.New64a()
	h.Write(b)
	return float64(h.Sum64()%2001)/1000 - 1 // [-1, 1]
}

// optionView is what an option does to the realm as every senator sees it.
// It doesn't depend on the senator, so a matter works it out once per
// option instead of once per senator.
type optionView struct {
	opt                MatterOption
	leans              map[Lean]float64
	expected           Realm
	danger, reckless   float64
	fatal, recklessEnd bool
}

func viewOption(o MatterOption, realm Realm) optionView {
	v := optionView{opt: o, leans: o.AllLeans(realm), expected: o.ExpectedRealm(realm)}
	v.danger, v.fatal = o.RealmDanger(realm)
	v.reckless, v.recklessEnd = danger(realm, v.expected)
	return v
}

// viewOptions views every option of a matter, in order.
func viewOptions(ev MatterEvent, realm Realm) []optionView {
	out := make([]optionView, len(ev.Options))
	for i, o := range ev.Options {
		out[i] = viewOption(o, realm)
	}
	return out
}

// Score is how much a senator likes an option.
func (s Senator) Score(opt MatterOption, realm Realm, matterID int64) float64 {
	return s.score(viewOption(opt, realm), realm, matterID)
}

func (s Senator) score(v optionView, realm Realm, matterID int64) float64 {
	score := 0.0
	for _, t := range s.Traits {
		ti, _ := TraitByKey(t)
		for l, w := range ti.Likes {
			score += w * v.leans[l]
		}
	}
	// Every senator cares a little about the realm, most about whatever is low.
	for a, d := range v.expected {
		need := (TrackerMax - realm[a]) / realmNeedScale
		score += realmCare * d * need
	}
	score += lawScore(s, v.opt)
	danger, _ := s.dangerOf(v)
	return score - danger + noise(matterID, s.ID, v.opt.Key)
}

// scores is how much a senator likes each option of a matter, by key.
func (s Senator) scores(views []optionView, realm Realm, matterID int64) map[string]float64 {
	out := make(map[string]float64, len(views))
	for _, v := range views {
		out[v.opt.Key] = s.score(v, realm, matterID)
	}
	return out
}

// preferredOf is the best-scoring option, the first one on ties.
func preferredOf(ev MatterEvent, sc map[string]float64) string {
	best, bestScore := "", math.Inf(-1)
	for _, o := range ev.Options {
		if v := sc[o.Key]; v > bestScore {
			best, bestScore = o.Key, v
		}
	}
	return best
}

// dangerOf is danger for an option already viewed.
func (s Senator) dangerOf(v optionView) (float64, bool) {
	if s.Has(TraitReckless) {
		return v.reckless, v.recklessEnd
	}
	return v.danger, v.fatal
}

// Preferred is the option the senator votes for unless bribed.
func (s Senator) Preferred(ev MatterEvent, realm Realm, matterID int64) string {
	return preferredOf(ev, s.scores(viewOptions(ev, realm), realm, matterID))
}

// decide is how a senator actually votes when the matter closes: their own
// preference, pulled toward players they like and away from players they
// don't.
func (s Senator) decide(ev MatterEvent, sc map[string]float64, players []Player, ops CourtOpinions) string {
	best, bestScore := "", math.Inf(-1)
	for _, o := range ev.Options {
		if v := sc[o.Key] + followBonus(s, o.Key, players, ops); v > bestScore {
			best, bestScore = o.Key, v
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
	views := viewOptions(ev, realm)
	for _, v := range views {
		if _, fatal := s.dangerOf(v); v.opt.Key == optKey && fatal { // no price is worth the realm
			return 0, false
		}
	}
	sc := s.scores(views, realm, matterID)
	pref := preferredOf(ev, sc)
	base := float64(s.BasePrice)
	if s.Has(TraitGreedy) {
		base *= greedyBribeFactor
	}
	if pref == optKey {
		return int(math.Ceil(base * agreeBribeFactor)), true
	}
	gap := sc[pref] - s.wantScore(sc, optKey, realm, matterID)
	return int(math.Ceil(base * (1 + gap))), true
}

// wantScore is the score of the option a briber wants. An unknown option
// scores as the zero option did before (no leans, no effects).
func (s Senator) wantScore(sc map[string]float64, optKey string, realm Realm, matterID int64) float64 {
	if v, ok := sc[optKey]; ok {
		return v
	}
	return s.Score(MatterOption{}, realm, matterID)
}

// VoteOffer is what this senator will pay a player for their vote. They
// always want it for their preferred option. The offer grows with the
// player's influence and with how strongly the senator cares. ok is false
// for senators who don't buy votes.
func (s Senator) VoteOffer(ev MatterEvent, voterInfluence float64, realm Realm, matterID int64) (opt string, gold int, ok bool) {
	if s.Has(TraitIncorruptible) {
		return "", 0, false
	}
	scores := s.scores(viewOptions(ev, realm), realm, matterID)
	opt = preferredOf(ev, scores)
	best, second := math.Inf(-1), math.Inf(-1)
	for _, o := range ev.Options {
		sc := scores[o.Key]
		if sc > best {
			best, second = sc, best
		} else if sc > second {
			second = sc
		}
	}
	care := math.Min(best-second, offerCareMax) // how much winning matters to them
	mult := 1.0
	switch {
	case s.Has(TraitBigSpender):
		mult = bigSpenderOffer
	case s.Has(TraitEconomical), s.Has(TraitGreedy):
		mult = thriftyOffer
	}
	gold = int(math.Ceil((1 + care) * voterInfluence * VoteOfferRate * mult))
	return opt, gold, gold > 0
}

// How senators are made and how they price things. Placeholders to tune.
const (
	senatorInfluenceMin, senatorInfluenceSpan = 8, 9   // influence 8–16
	senatorPriceMin, senatorPriceSpan         = 15, 21 // base bribe price 15–35

	realmNeedScale = 50   // how far below the top a tracker must be to be a need
	realmCare      = 0.06 // how much every senator cares about the realm's needs

	greedyBribeFactor = 0.5 // the Greedy sell cheap
	agreeBribeFactor  = 0.5 // paying a senator to vote as they would anyway
	offerCareMax      = 3.0 // caps how much more a senator pays for a vote they care about
	bigSpenderOffer   = 1.5
	thriftyOffer      = 0.6 // Economical and Greedy senators
)

// VoteOfferRate is gold per point of influence a senator pays for a vote
// they mildly care about.
const VoteOfferRate = 1.2

// BestVoteOffers is, per option, the senator paying the most for it.
// ops is the senators' opinions of the voter (nil = all neutral).
func BestVoteOffers(ev MatterEvent, senators []Senator, voterInfluence float64, realm Realm, matterID int64, ops Opinions) map[string]VoteOfferFrom {
	out := map[string]VoteOfferFrom{}
	for _, s := range senators {
		opt, gold, ok := s.VoteOfferFor(ev, voterInfluence, realm, matterID, ops[s.ID])
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

// Theme groups related Senate events.
type Theme string

const (
	ThemeBarbarians Theme = "barbarians"
	ThemeBandits    Theme = "bandits"
	ThemeFood       Theme = "food"
	ThemeSickness   Theme = "sickness"
	ThemeForeign    Theme = "foreign"
	ThemeSoldiers   Theme = "soldiers"
	ThemeMoney      Theme = "money"
	ThemeFaith      Theme = "faith"
	ThemeWorks      Theme = "works" // walls, aqueducts, libraries
	ThemePeople     Theme = "people"
	ThemeSea        Theme = "sea"
	ThemeLand       Theme = "land" // fires, floods, forests
)

// MatterCooldown is how many matters must pass before an event can be
// picked again.
const MatterCooldown = 10

// lastTheme is the theme of the most recent event in recent (oldest first).
func lastTheme(recent []string) Theme {
	if len(recent) == 0 {
		return ""
	}
	e, _ := MatterByKey(recent[len(recent)-1])
	return e.Theme
}

// FollowUpAllowed reports whether a queued follow-up may open now. Chains may
// break the cooldown, but never repeat the matter just before.
func FollowUpAllowed(key string, recent []string) bool {
	return len(recent) == 0 || recent[len(recent)-1] != key
}

// PickMatter chooses a random non-follow-up event, favoring events whose Bias
// trackers are low. recent is the latest event keys, oldest first: none of
// the last MatterCooldown repeat, and the theme of the last one is skipped.
// If that leaves nothing, the theme rule and then the cooldown are relaxed.
func PickMatter(r Roller, realm Realm, recent []string) MatterEvent {
	if len(recent) > MatterCooldown {
		recent = recent[len(recent)-MatterCooldown:]
	}
	if e, ok := pickMatter(r, realm, recent, lastTheme(recent), nil, 0); ok {
		return e
	}
	if e, ok := pickMatter(r, realm, recent, "", nil, 0); ok {
		return e
	}
	e, _ := pickMatter(r, realm, nil, "", nil, 0)
	return e
}

// pickMatter draws from the catalogue. laws nil leaves motions out;
// otherwise motions come up when MotionAllowed, and motions to pass a law
// only while there is room for one. Events whose Earliest is above held (the
// matters the court has held) are left out; laws nil leaves them all out.
func pickMatter(r Roller, realm Realm, recent []string, avoid Theme, laws *Laws, held int) (MatterEvent, bool) {
	skip := map[string]bool{}
	for _, k := range recent {
		skip[k] = true
	}
	var pool []int // indices into MatterEvents, so nothing big is copied
	var weights []float64
	total := 0.0
	for i := range MatterEvents {
		e := &MatterEvents[i]
		if e.FollowUpOnly || skip[e.Key] || (avoid != "" && e.Theme == avoid) || (e.Earliest > 0 && (laws == nil || held < e.Earliest)) {
			continue
		}
		w := 1.0
		if e.Motion != "" {
			if laws == nil || !MotionAllowed(*e, *laws) || (!e.Repeal && laws.Full()) {
				continue
			}
			w = motionWeight
		}
		for a, b := range e.Bias {
			w += b * (50 - realm[a]) / 50
		}
		w = math.Max(w, 0.2)
		pool = append(pool, i)
		weights = append(weights, w)
		total += w
	}
	x := float64(r.IntN(10000)) / 10000 * total
	for i, w := range weights {
		if x < w {
			return MatterEvents[pool[i]], true
		}
		x -= w
	}
	if len(pool) == 0 {
		return MatterEvent{}, false
	}
	return MatterEvents[pool[len(pool)-1]], true
}

// ---- resolution ----

// Player is a court member as the senate sees them.
type Player struct {
	Person
	Role   string
	Vote   string // option key, "" if they didn't vote
	Weight float64
	Items  []string // estate item keys, for the Witchcraft Act
}

type MatterInput struct {
	MatterID int64
	Event    MatterEvent
	Realm    Realm
	Sectors  SectorState
	Senators []Senator
	Bribes   map[int64]string // senator ID -> option they were paid to vote for
	Players  []Player
	Opinions CourtOpinions // senators' opinions of players; nil = all neutral
	Laws     Laws          // in force when the vote closes
	Now      time.Time     // when it closes, for the laws it passes
	// Petitioner is the player who petitioned for this matter (petition.go),
	// 0 if nobody did.
	Petitioner int64
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
	Realm        Realm       // after
	Sectors      SectorState // after
	Laws         Laws        // after
	LawChanges   LawChanges
	Fallen       bool
	FallenReason string
	Deltas       map[int64]Delta
	Notices      map[int64][]string
	FollowUps    []QueuedEvent
	Opinions     []OpinionChange // how each senator now feels about each voter
	// Secret is true if the votes must not be shown (the Secret Ballot).
	Secret bool
}

func (res *MatterResult) delta(id int64, d Delta) { addDelta(res.Deltas, id, d) }

// ResolveMatter counts the votes, picks the winner, rolls its outcome and
// works out what happens to the realm and to each player.
func ResolveMatter(r Roller, in MatterInput) MatterResult {
	ev := in.Event
	res := MatterResult{Tally: map[string]float64{}, Deltas: map[int64]Delta{}, Notices: map[int64][]string{}}
	if ev.Auto {
		return resolveAuto(r, in, res)
	}
	secret := in.Laws.Rules().SecretBallot
	res.Secret = secret

	// Every senator's view of every option, worked out once for the vote
	// and for how they take each player's vote afterwards.
	views := viewOptions(ev, in.Realm)
	senScores := make(map[int64]map[string]float64, len(in.Senators))
	for _, s := range in.Senators {
		senScores[s.ID] = s.scores(views, in.Realm, in.MatterID)
	}
	senVotes := map[int64]string{}
	for _, s := range in.Senators {
		opt := s.decide(ev, senScores[s.ID], in.Players, in.Opinions)
		if b, ok := in.Bribes[s.ID]; ok {
			opt = b
		}
		senVotes[s.ID] = opt
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
	res.rollOutcome(r, in)
	out := res.Outcome
	res.applyOutcome(in)
	// A petition is extra business, not a Senate day: no drift, no daily laws.
	if !res.Fallen && in.Petitioner == 0 {
		res.Realm.Apply(res.Realm.Drift())
		res.Sectors.Drift()
		// The laws' daily costs come after the day's recovery, and can be
		// what finally sinks the realm.
		ApplyDailyLaws(res.Realm, res.Sectors, res.Laws)
		res.Fallen, res.FallenReason = res.Realm.Fallen()
	}

	for _, p := range in.Players {
		d := out.forPlayer(p)
		if p.Vote == winner.Key {
			d.Merge(Delta{Influence: WinningSideInfluence})
			d.Merge(out.Supporters)
		}
		if desc := d.Describe(); desc != "" {
			res.delta(p.ID, d)
			res.Notices[p.ID] = append(res.Notices[p.ID], fmt.Sprintf("The Senate voted to **%s**. For you: %s", strings.ToLower(winner.Label), desc))
		}
		_, voted := ev.Option(p.Vote)
		if abstain := res.Laws.Rules().AbstainInfluence; !voted && abstain != 0 {
			g := Delta{Influence: ScaleInfluence(abstain, p.Sheet[AttrInfluence])}
			res.delta(p.ID, g)
			res.Notices[p.ID] = append(res.Notices[p.ID], "You kept out of the vote, and the court respects your restraint. ("+g.Describe()+")")
		}
		if !voted || secret { // under the Secret Ballot, no senator knows how you voted
			continue
		}
		// Opinions move quietly; players see them on the senators' screens.
		for _, s := range in.Senators {
			res.Opinions = append(res.Opinions, OpinionChange{SenatorID: s.ID, CharacterID: p.ID,
				Delta: voteOpinion(ev, senScores[s.ID], senVotes[s.ID], p.Vote)})
		}
	}
	petitionerResult(&res, in)
	witchHunt(r, &res, in.Players, res.Laws)
	res.queueFollowUps(r)
	return res
}

// rollOutcome rolls the winning option's check, if it has one.
func (res *MatterResult) rollOutcome(r Roller, in MatterInput) {
	w := res.Winner
	res.Outcome, res.Succeeded = w.Success, true
	if w.Check != nil {
		if ok, _ := w.Check.Roll(r, in.Realm.Sheet(), LawCheckMod(in.Laws, in.Event), Normal); !ok {
			res.Outcome, res.Succeeded = w.Failure, false
		}
	}
}

// applyOutcome works out the realm, sectors and laws after the outcome.
func (res *MatterResult) applyOutcome(in MatterInput) {
	out := res.Outcome
	res.Realm = in.Realm.Clone()
	res.Realm.Apply(out.Realm)
	res.Sectors = in.Sectors.Fill().Clone()
	res.Sectors.Apply(out.Sectors)
	res.Laws, res.LawChanges = in.Laws.Apply(out.Enact, out.Repeal, in.Now)
	res.Fallen, res.FallenReason = res.Realm.Fallen()
}

// queueFollowUps rolls which follow-ups the outcome queues. A fallen realm
// has no tomorrow.
func (res *MatterResult) queueFollowUps(r Roller) {
	if res.Fallen {
		return
	}
	for _, f := range res.Outcome.FollowUps {
		if float64(r.IntN(100)) < f.Chance*100 {
			res.FollowUps = append(res.FollowUps, QueuedEvent{Event: f.Event, Days: f.Days})
		}
	}
}

// forPlayer is what an outcome does to every player in a role.
func (out MatterOutcome) forPlayer(p Player) Delta {
	var d Delta
	d.Merge(out.All)
	if rd, ok := out.Roles[p.Role]; ok {
		d.Merge(rd)
	}
	return d
}

// witchHunt fines owners of magic items under the Witchcraft Act, once per
// Senate day.
func witchHunt(r Roller, res *MatterResult, players []Player, ls Laws) {
	if !ls.Rules().WitchHunt {
		return
	}
	for _, p := range players {
		for _, k := range p.Items {
			it, ok := ItemByKey(k)
			if !ok || !it.Magic || r.IntN(100) >= LawWitchFineChance {
				continue
			}
			fine := max(1, int(float64(it.Price)*LawWitchFineRate))
			res.delta(p.ID, Delta{Wealth: -fine})
			res.Notices[p.ID] = append(res.Notices[p.ID],
				fmt.Sprintf("🔥 The witch-finders came for your %s. You paid them to go away. (-%d gold)", it.Name, fine))
		}
	}
}

// resolveAuto applies an Auto event's one option: no vote, no opinions, and
// no day passes (no drift, no daily laws), since it isn't the Senate's daily
// matter.
func resolveAuto(r Roller, in MatterInput, res MatterResult) MatterResult {
	ev := in.Event
	res.Laws = in.Laws.Clone()
	if len(ev.Options) == 0 {
		res.Realm, res.Sectors = in.Realm.Clone(), in.Sectors.Fill().Clone()
		return res
	}
	res.Winner = ev.Options[0]
	res.rollOutcome(r, in)
	res.applyOutcome(in)
	out := res.Outcome
	for _, p := range in.Players {
		if d := out.forPlayer(p); d.Describe() != "" {
			res.Deltas[p.ID] = d
			res.Notices[p.ID] = append(res.Notices[p.ID], fmt.Sprintf("%s For you: %s", out.Text, d.Describe()))
		}
	}
	res.queueFollowUps(r)
	return res
}
