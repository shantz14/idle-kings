package game

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// KindInvest is the activity kind for Invest in a Venture.
const KindInvest = "invest"

var (
	DefaultInvestDuration = 3 * time.Hour
	FastInvestDuration    = 3 * time.Minute
)

// SectorKey names one of the realm's industries.
type SectorKey string

const (
	SectorAgriculture SectorKey = "agriculture"
	SectorMining      SectorKey = "mining"
	SectorAlchemy     SectorKey = "alchemy"
	SectorLuxury      SectorKey = "luxury"
	SectorTrade       SectorKey = "trade"
	SectorTimber      SectorKey = "timber"
	SectorBlackMarket SectorKey = "black_market"
)

// SectorDef is a sector in the catalogue. Placeholder numbers — tune freely.
//
// A sector earns Capacity × Health × (tracker modifier) gold a day, split
// between its owners by stake. The realm's NPC houses hold HouseStake, so a
// player's share is their stake over HouseStake plus every player's stake.
type SectorDef struct {
	Key         SectorKey
	Name        string
	Emoji       string
	Description string
	BaseRevenue float64 // starting capacity, gold/day
	HouseStake  float64 // stake held by NPC houses
	Growth      float64 // capacity added per gold invested
	Deal        Check   // the investor's hidden roll for how good a deal they get
	// Secret sectors are hidden from public profiles, and a badly failed
	// deal exposes the investor.
	Secret bool
	// Trackers shift revenue with the realm: each point a tracker sits
	// above 50 changes revenue by weight percent.
	Trackers map[Attr]float64
}

func stewardDeal() Check {
	return Check{Base: 30, Weights: map[Attr]float64{AttrStewardship: 6}}
}

var SectorDefs = []SectorDef{
	{Key: SectorAgriculture, Name: "Agriculture", Emoji: "🌾",
		Description: "Farms, orchards and herds in the countryside. Steady, until someone burns them.",
		BaseRevenue: 50, HouseStake: 1200, Growth: 0.04, Deal: stewardDeal(),
		Trackers: map[Attr]float64{AttrPopularity: 0.2}},
	{Key: SectorMining, Name: "Mining", Emoji: "⛏️",
		Description: "Iron, stone and silver from the hills. Rich veins and sudden collapses.",
		BaseRevenue: 40, HouseStake: 900, Growth: 0.045, Deal: stewardDeal(),
		Trackers: map[Attr]float64{AttrMilitary: 0.4}},
	{Key: SectorAlchemy, Name: "Alchemy & Apothecaries", Emoji: "⚗️",
		Description: "Cures, poisons, fire-powder and stranger things, brewed on Crucible Street. The guild pays well and asks few questions.",
		BaseRevenue: 40, HouseStake: 1000, Growth: 0.04, Deal: stewardDeal(),
		Trackers: map[Attr]float64{AttrStability: 0.3, AttrPopularity: 0.2}},
	{Key: SectorLuxury, Name: "Luxury Goods", Emoji: "💎",
		Description: "Silks, perfume, wine and jewels for the great houses. Booms with good times and foreign friends.",
		BaseRevenue: 35, HouseStake: 800, Growth: 0.04, Deal: stewardDeal(),
		Trackers: map[Attr]float64{AttrPopularity: 0.4, AttrForeign: 0.4}},
	{Key: SectorTrade, Name: "Trade", Emoji: "🐪",
		Description: "Caravans on the roads and ships in the harbor. Only as safe as the roads.",
		BaseRevenue: 45, HouseStake: 1000, Growth: 0.04, Deal: stewardDeal(),
		Trackers: map[Attr]float64{AttrStability: 0.5, AttrForeign: 0.5}},
	{Key: SectorTimber, Name: "Timber & Shipbuilding", Emoji: "🪵",
		Description: "Logging camps and shipyards. Walls, palisades and fleets all start here.",
		BaseRevenue: 35, HouseStake: 900, Growth: 0.04, Deal: stewardDeal(),
		Trackers: map[Attr]float64{AttrMilitary: 0.2}},
	{Key: SectorBlackMarket, Name: "The Black Market", Emoji: "🗝️",
		Description: "Smuggling, gambling dens and fences in the lower city. Pays best when the law is weakest. Your stake stays secret, unless a deal goes badly wrong.",
		BaseRevenue: 30, HouseStake: 500, Growth: 0.05,
		Deal:   Check{Base: 30, Weights: map[Attr]float64{AttrIntrigue: 6}},
		Secret: true, Trackers: map[Attr]float64{AttrStability: -1}},
}

func SectorByKey(k SectorKey) (SectorDef, bool) {
	for _, d := range SectorDefs {
		if d.Key == k {
			return d, true
		}
	}
	return SectorDef{}, false
}

// Investment limits and the preset amounts offered as buttons.
const (
	InvestMin = 25
	InvestMax = 1000
)

var InvestPresets = []int{25, 50, 100, 250}

// ---- realm state ----

// Sector is one sector's state in a realm.
type Sector struct {
	Capacity float64 `json:"capacity"` // gold/day in normal conditions
	Health   float64 `json:"health"`   // 1 = normal; Senate shocks move it, it drifts back
}

const (
	HealthMin = 0.2
	HealthMax = 1.5
	// HealthDrift is how far health moves back toward 1 each day.
	HealthDrift = 0.1
	// Tracker modifiers are clamped to this range.
	trackerModMin = 0.5
	trackerModMax = 1.5
)

// SectorState is every sector in a realm.
type SectorState map[SectorKey]Sector

func StartingSectors() SectorState {
	s := SectorState{}
	for _, d := range SectorDefs {
		s[d.Key] = Sector{Capacity: d.BaseRevenue, Health: 1}
	}
	return s
}

// Fill adds any catalogue sector the state is missing.
func (s SectorState) Fill() SectorState {
	if s == nil {
		s = SectorState{}
	}
	for _, d := range SectorDefs {
		if _, ok := s[d.Key]; !ok {
			s[d.Key] = Sector{Capacity: d.BaseRevenue, Health: 1}
		}
	}
	return s
}

func (s SectorState) Clone() SectorState {
	out := make(SectorState, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// SectorChange is what a Senate outcome does to a sector. Health is a
// temporary shock (−0.25 = a quarter of revenue lost, recovering daily);
// Capacity is permanent, in gold/day.
type SectorChange struct {
	Health   float64
	Capacity float64
}

// Apply adds changes, clamping health.
func (s SectorState) Apply(ch map[SectorKey]SectorChange) {
	for k, c := range ch {
		sec, ok := s[k]
		if !ok {
			continue
		}
		sec.Health = math.Min(HealthMax, math.Max(HealthMin, sec.Health+c.Health))
		sec.Capacity = math.Max(0, sec.Capacity+c.Capacity)
		s[k] = sec
	}
}

// Drift moves every sector's health one day back toward normal.
func (s SectorState) Drift() {
	for k, sec := range s {
		sec.Health += (1 - sec.Health) * HealthDrift
		s[k] = sec
	}
}

// Modifier is how the realm's trackers currently shift a sector's revenue.
func (d SectorDef) Modifier(realm Realm) float64 {
	m := 1.0
	for a, w := range d.Trackers {
		m += w * (realm[a] - 50) / 100
	}
	return math.Min(trackerModMax, math.Max(trackerModMin, m))
}

// Revenue is the sector's gold per day, before it is split between owners.
func (s SectorState) Revenue(d SectorDef, realm Realm, laws Laws) float64 {
	sec := s[d.Key]
	return sec.Capacity * sec.Health * d.Modifier(realm) * LawRevenue(laws, d.Key)
}

// Condition is a sector's fortunes in a word, relative to its capacity.
func (s SectorState) Condition(d SectorDef, realm Realm, laws Laws) string {
	sec := s[d.Key]
	switch f := sec.Health * d.Modifier(realm) * LawRevenue(laws, d.Key); {
	case f >= 1.2:
		return "booming"
	case f >= 1.05:
		return "thriving"
	case f >= 0.95:
		return "steady"
	case f >= 0.75:
		return "struggling"
	default:
		return "in ruins"
	}
}

// Stakes is how much of each sector someone owns (or, for totals, how much
// all players own together).
type Stakes map[SectorKey]float64

// Share is the fraction of a sector a stake owns, given every player's stake.
func Share(d SectorDef, stake, playerTotal float64) float64 {
	if stake <= 0 {
		return 0
	}
	return stake / (d.HouseStake + playerTotal)
}

// VentureIncome is a player's gold per day from their stakes. mine and
// totals are effective stakes (see EffectiveStakes).
func VentureIncome(s SectorState, realm Realm, laws Laws, mine, totals Stakes) float64 {
	var sum float64
	for _, d := range SectorDefs {
		if st := mine[d.Key]; st > 0 {
			sum += Share(d, st, totals[d.Key]) * s.Revenue(d, realm, laws)
		}
	}
	return sum
}

// EffectiveStakes is every player's stakes as the laws count them, and the
// players' combined effective stake per sector. Under the Charter of
// Monopolies the biggest player stake in each sector counts for more and
// every other for less; otherwise stakes count as they are.
func EffectiveStakes(laws Laws, by map[int64]Stakes) (map[int64]Stakes, Stakes) {
	out, totals := map[int64]Stakes{}, Stakes{}
	rules := laws.Rules()
	leader := map[SectorKey]int64{}
	if rules.Monopolies() {
		best := map[SectorKey]float64{}
		for id, st := range by {
			for k, v := range st {
				// Ties go to the lower id, so the leader doesn't depend on map order.
				if v > best[k] || (v == best[k] && v > 0 && id < leader[k]) {
					best[k], leader[k] = v, id
				}
			}
		}
	}
	for id, st := range by {
		eff := Stakes{}
		for k, v := range st {
			switch {
			case !rules.Monopolies():
			case leader[k] == id:
				v *= mulFactor(1, rules.MonopolyLeader)
			default:
				v *= mulFactor(1, rules.MonopolyRest)
			}
			eff[k] = v
			totals[k] += v
		}
		out[id] = eff
	}
	return out, totals
}

// ---- investing ----

// DealTier is how good a deal an investor struck.
type DealTier int

const (
	DealBad DealTier = iota
	DealFair
	DealGreat
)

// Deal margins and how much stake each tier buys per gold.
const (
	dealGreatMargin  = 40
	dealExposeMargin = -40
)

var dealStake = map[DealTier]float64{DealBad: 0.6, DealFair: 1.0, DealGreat: 1.5}

// ExposedDelta is what being caught in the Black Market costs.
var ExposedDelta = Delta{Renown: -1}

type InvestResult struct {
	Tier     DealTier
	Stake    float64 // stake gained
	Capacity float64 // capacity the gold added to the sector
	Exposed  bool    // secret sector, and the deal went badly wrong
	Practice Delta   // Stewardship practice (Intrigue in a secret sector)
	Text     string  // what the investor is told; never shows the roll
}

// Invest rolls the deal. The gold is spent and the sector grows whatever the
// roll; the roll decides how much of it the investor ends up owning.
func Invest(r Roller, investor Sheet, d SectorDef, gold int) InvestResult {
	_, m := d.Deal.Roll(r, investor, 0, Normal)
	res := InvestResult{Tier: DealBad, Capacity: float64(gold) * d.Growth}
	switch {
	case m >= dealGreatMargin:
		res.Tier = DealGreat
	case m > 0:
		res.Tier = DealFair
	}
	res.Stake = float64(gold) * dealStake[res.Tier]
	res.Exposed = d.Secret && m <= dealExposeMargin
	if d.Secret {
		res.Practice = Practice(Intrigue, m > 0)
	} else {
		res.Practice = Practice(Stewardship, m > 0)
	}

	switch {
	case d.Secret && res.Tier == DealGreat:
		res.Text = "Your go-between knew exactly whose palm to grease. You bought in cheap."
	case d.Secret && res.Tier == DealFair:
		res.Text = "A quiet word in a back room, a purse changes hands, and you have a cut."
	case res.Exposed:
		res.Text = "Your go-between talked. Half the court knows where your money went."
	case d.Secret:
		res.Text = "The gang bosses saw you coming and squeezed you for every coin."
	case res.Tier == DealGreat:
		res.Text = "A shrewd deal. You bought in for far less than it's worth."
	case res.Tier == DealFair:
		res.Text = "A fair deal. You paid what the stake is worth."
	default:
		res.Text = "You overpaid. The sellers are still laughing."
	}
	return res
}

// DescribeChanges renders sector changes like "🌾 Agriculture −25% · ⛏️ Mining +12/day".
func DescribeChanges(ch map[SectorKey]SectorChange) string {
	var parts []string
	for _, d := range SectorDefs {
		c, ok := ch[d.Key]
		if !ok {
			continue
		}
		if c.Health != 0 {
			parts = append(parts, fmt.Sprintf("%s %s %s%%", d.Emoji, d.Name, signed(c.Health*100)))
		}
		if c.Capacity != 0 {
			parts = append(parts, fmt.Sprintf("%s %s %s gold/day for good", d.Emoji, d.Name, signed(c.Capacity)))
		}
	}
	return strings.Join(parts, " · ")
}

func signed(v float64) string {
	if v > 0 {
		return "+" + Num(v)
	}
	return Num(v)
}

// AllSectors is a change to every sector at once.
func AllSectors(health float64) map[SectorKey]SectorChange {
	m := map[SectorKey]SectorChange{}
	for _, d := range SectorDefs {
		m[d.Key] = SectorChange{Health: health}
	}
	return m
}
