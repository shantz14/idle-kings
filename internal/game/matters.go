package game

import "math"

// MatterEvent is a daily matter the Senate votes on.
type MatterEvent struct {
	Key     string
	Title   string
	Prompt  string
	Options []MatterOption
	Default string // wins ties: the status quo
	// Theme groups related events; two random picks in a row never share one.
	Theme Theme
	// FollowUpOnly events only happen as consequences of earlier votes.
	FollowUpOnly bool
	// Auto events are not voted on: their one option happens as soon as
	// they come up (a bill that comes due). They are always FollowUpOnly.
	Auto bool
	// Bias makes the event more likely when a tracker is below 50 (positive)
	// or above 50 (negative).
	Bias map[Attr]float64
	// Arc and Chapter place the event in a story arc (see arcs.go).
	Arc     string
	Chapter int
}

// MatterOption is one choice. Hint is all players see before the vote;
// exact results are revealed after.
type MatterOption struct {
	Key   string
	Label string
	Hint  string
	// Leans are what senators judge the option on. Cost, profit and risk
	// are added automatically from the outcomes.
	Leans   map[Lean]float64
	Check   *Check // rolled against the realm's trackers; nil = always Success
	Success MatterOutcome
	Failure MatterOutcome
}

type MatterOutcome struct {
	Text       string
	Realm      Realm
	All        Delta            // every player
	Supporters Delta            // players who voted for this option
	Roles      map[string]Delta // players with this role, however they voted
	Sectors    map[SectorKey]SectorChange
	FollowUps  []FollowUp
}

// FollowUp may queue another event Days later.
type FollowUp struct {
	Event  string
	Chance float64 // 0–1
	Days   int
}

func (e MatterEvent) Option(key string) (MatterOption, bool) {
	for _, o := range e.Options {
		if o.Key == key {
			return o, true
		}
	}
	return MatterOption{}, false
}

func MatterByKey(key string) (MatterEvent, bool) {
	for _, e := range MatterEvents {
		if e.Key == key {
			return e, true
		}
	}
	return MatterEvent{}, false
}

// SuccessChance is the option's odds against the realm (100 if no roll).
func (o MatterOption) SuccessChance(realm Realm) float64 {
	if o.Check == nil {
		return 100
	}
	return float64(o.Check.Chance(realm.Sheet(), 0))
}

// ExpectedRealm is the average realm change, weighing success and failure.
func (o MatterOption) ExpectedRealm(realm Realm) Realm {
	p := o.SuccessChance(realm) / 100
	out := Realm{}
	for k, v := range o.Success.Realm {
		out[k] += p * v
	}
	for k, v := range o.Failure.Realm {
		out[k] += (1 - p) * v
	}
	return out
}

// ExpectedSectors is the average sector change, weighing success and failure.
func (o MatterOption) ExpectedSectors(realm Realm) map[SectorKey]SectorChange {
	p := o.SuccessChance(realm) / 100
	out := map[SectorKey]SectorChange{}
	add := func(ch map[SectorKey]SectorChange, w float64) {
		for k, c := range ch {
			cur := out[k]
			cur.Health += w * c.Health
			cur.Capacity += w * c.Capacity
			out[k] = cur
		}
	}
	add(o.Success.Sectors, p)
	add(o.Failure.Sectors, 1-p)
	return out
}

// sectorLean turns sector changes into a lean: a 10% shock is about 1, and
// 5 gold/day of permanent capacity about 1.
func sectorLean(ch map[SectorKey]SectorChange, keys ...SectorKey) float64 {
	v := 0.0
	for _, k := range keys {
		v += 10*ch[k].Health + ch[k].Capacity/5
	}
	return v / float64(len(keys))
}

// Realm danger: senators won't vote to end the realm, and shy away from
// draining a tracker that is already low. They only see what a vote does
// now, never the bills and follow-ups it brings later.
const (
	dangerZone   = 5.0  // worst-case value below this worries senators
	dangerWeight = 0.3  // score lost per point below the zone
	fatalPenalty = 25.0 // outweighs any trait, opinion or noise
)

// RealmDanger judges an option by its worst case (success or failure) on
// every tracker that can end the realm. fatal is true if it could.
func (o MatterOption) RealmDanger(realm Realm) (penalty float64, fatal bool) {
	worst := Realm{}
	for _, t := range Trackers {
		w := math.Min(o.Success.Realm[t.Key], 0)
		if o.Check != nil {
			w = math.Min(w, o.Failure.Realm[t.Key])
		}
		worst[t.Key] = w
	}
	return danger(realm, worst)
}

// RecklessDanger is how a reckless senator sees it: by the expected result,
// so a gamble that will probably come off doesn't scare them.
func (o MatterOption) RecklessDanger(realm Realm) (penalty float64, fatal bool) {
	return danger(realm, o.ExpectedRealm(realm))
}

func danger(realm, change Realm) (penalty float64, fatal bool) {
	for _, t := range Trackers {
		d := change[t.Key]
		if t.Fall == "" || d >= 0 {
			continue
		}
		after := realm[t.Key] + d
		switch {
		case after <= TrackerMin:
			fatal = true
			penalty += fatalPenalty
		case after < dangerZone:
			penalty += (dangerZone - after) * dangerWeight
		}
	}
	return penalty, fatal
}

// AllLeans returns the authored leans plus the derived ones: cost, profit
// and risk, and land, commerce and order from what the option does to the
// sectors and to Stability.
func (o MatterOption) AllLeans(realm Realm) map[Lean]float64 {
	out := map[Lean]float64{}
	for k, v := range o.Leans {
		out[k] = v
	}
	if t := o.ExpectedRealm(realm)[AttrTreasury]; t < 0 {
		out[LeanCost] += -t / 10
	} else if t > 0 {
		out[LeanProfit] += t / 10
	}
	if o.Check != nil {
		out[LeanRisk] += 1
	}
	sec := o.ExpectedSectors(realm)
	out[LeanLand] += sectorLean(sec, SectorAgriculture)
	out[LeanCommerce] += sectorLean(sec, SectorTrade, SectorLuxury)
	out[LeanOrder] += o.ExpectedRealm(realm)[AttrStability] / 4
	return out
}

// Shorthands for the catalogue below.
type (
	L  = map[Lean]float64
	R  = Realm
	D  = Delta
	SC = map[SectorKey]SectorChange
)

func roles(kv ...any) map[string]Delta {
	m := map[string]Delta{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1].(Delta)
	}
	return m
}

func flat(base float64) *Check { return &Check{Base: base} }

// MatterEvents is the catalogue. Placeholder numbers — tune freely.
var MatterEvents = []MatterEvent{
	// ---------- barbarians (chain) ----------
	{
		Key: "border_barbarians", Title: "Barbarians at the Border", Arc: "northern_tribes", Chapter: 1, Theme: ThemeBarbarians, Bias: map[Attr]float64{AttrMilitary: 1},
		Prompt:  "A Dothraki khalasar has ridden down from beyond the northern hills and made camp along the border. Farmers report missing cattle and burned barns.",
		Default: "ignore",
		Options: []MatterOption{
			{Key: "legions", Label: "Send the legions", Hint: "Use the army to drive them off. A fight could go either way.",
				Leans: L{LeanMilitary: 2}, Check: chk(30, AttrMilitary, 0.8),
				Success: MatterOutcome{Text: "The legions scatter the Dothraki back into the hills.",
					Realm: R{AttrMilitary: -3, AttrStability: 4}, Supporters: D{Renown: 0.2}, Roles: roles("officer", D{Renown: 0.5})},
				Failure: MatterOutcome{Text: "The legions are ambushed in the passes and limp home.", Sectors: SC{SectorAgriculture: {Health: -0.1}},
					Realm: R{AttrMilitary: -10, AttrStability: -4}, FollowUps: []FollowUp{{"barbarian_raid", 0.6, 1}}}},
			{Key: "tribute", Label: "Pay them to leave", Hint: "Costs the treasury. They may come back for more.",
				Leans: L{LeanMilitary: -1},
				Success: MatterOutcome{Text: "The khal takes the gold and rides away. For now.",
					Realm: R{AttrTreasury: -12, AttrStability: 2}, FollowUps: []FollowUp{{"barbarian_tribute", 0.4, 3}}}},
			{Key: "ignore", Label: "Ignore them", Hint: "Costs nothing today. The outer farms will pay for it.",
				Leans: L{LeanTradition: 0.5},
				Success: MatterOutcome{Text: "The Senate does nothing. The raids go on.", Sectors: SC{SectorAgriculture: {Health: -0.15}},
					Realm: R{AttrStability: -3, AttrPopularity: -2}, FollowUps: []FollowUp{{"barbarian_raid", 0.5, 2}}}},
		},
	},
	{
		Key: "barbarian_raid", Title: "Barbarians Burn a Town", Arc: "northern_tribes", Chapter: 2, Theme: ThemeBarbarians, FollowUpOnly: true,
		Prompt:  "Emboldened, the Dothraki have sacked a border town. Refugees are pouring toward the capital.",
		Default: "wait",
		Options: []MatterOption{
			{Key: "muster", Label: "Muster a defense", Hint: "Meet them in the field.",
				Leans: L{LeanMilitary: 2}, Check: chk(25, AttrMilitary, 0.8),
				Success: MatterOutcome{Text: "The legions push the raiders back over the hills.",
					Realm: R{AttrMilitary: -5, AttrStability: 5}, Roles: roles("officer", D{Renown: 1})},
				Failure: MatterOutcome{Text: "The defense collapses.", Sectors: SC{SectorAgriculture: {Health: -0.25}, SectorTimber: {Health: -0.1}},
					Realm: R{AttrMilitary: -12, AttrStability: -6}, FollowUps: []FollowUp{{"barbarian_siege", 0.8, 1}}}},
			{Key: "refugees", Label: "Take in the refugees", Hint: "Feed and house them. The town is lost either way. Workshops could use the hands.",
				Leans: L{LeanPopular: 2},
				Success: MatterOutcome{Text: "The refugees are fed and housed. The people approve.", Sectors: SC{SectorAgriculture: {Health: -0.05}, SectorTimber: {Health: 0.05}},
					Realm: R{AttrTreasury: -10, AttrPopularity: 5, AttrStability: -2}, FollowUps: []FollowUp{{"barbarian_siege", 0.3, 2}}}},
			{Key: "wait", Label: "Hold the capital and wait", Hint: "Let them tire themselves out. Farms and workshops will burn.",
				Leans: L{LeanTradition: 0.5},
				Success: MatterOutcome{Text: "The capital waits. The countryside burns.", Sectors: SC{SectorAgriculture: {Health: -0.25}, SectorTimber: {Health: -0.1}},
					Realm: R{AttrStability: -8, AttrPopularity: -6}, FollowUps: []FollowUp{{"barbarian_siege", 0.8, 1}}}},
		},
	},
	{
		Key: "barbarian_siege", Title: "A City Under Siege", Arc: "northern_tribes", Chapter: 3, Theme: ThemeBarbarians, FollowUpOnly: true,
		Prompt:  "The khalasars have united under a great khal and surrounded the city of Neverwinter. If it falls, the road to the capital is open.",
		Default: "abandon",
		Options: []MatterOption{
			{Key: "relieve", Label: "Send everything to relieve it", Hint: "All or nothing.",
				Leans: L{LeanMilitary: 2.5}, Check: chk(20, AttrMilitary, 0.9),
				Success: MatterOutcome{Text: "Neverwinter is relieved. The khal's braid is cut and sent to the capital.",
					Realm: R{AttrMilitary: -10, AttrStability: 10, AttrPopularity: 5}, Supporters: D{Renown: 1},
					Roles: roles("officer", D{Renown: 1.5, Influence: 1})},
				Failure: MatterOutcome{Text: "The relief army is destroyed beneath Neverwinter's walls.", Sectors: SC{SectorMining: {Health: -0.2}, SectorTrade: {Health: -0.1}},
					Realm: R{AttrMilitary: -25, AttrStability: -12}}},
			{Key: "ransom", Label: "Buy off the khal", Hint: "Very expensive. The great families will be asked to help.",
				Leans: L{LeanMilitary: -1},
				Success: MatterOutcome{Text: "The khal takes a fortune in silver and rides home across the grass sea.", Sectors: SC{SectorLuxury: {Health: -0.1}},
					Realm: R{AttrTreasury: -30, AttrStability: -4}, All: D{Wealth: -5}}},
			{Key: "abandon", Label: "Abandon Neverwinter", Hint: "Save the army. Lose the city. Neverwinter's workshops, farms and markets go with it.",
				Success: MatterOutcome{Text: "Neverwinter burns. Its people curse the Senate.", Sectors: SC{SectorMining: {Health: -0.3}, SectorTrade: {Health: -0.2}, SectorAgriculture: {Health: -0.2}},
					Realm: R{AttrStability: -18, AttrPopularity: -15, AttrTreasury: -10}}},
		},
	},
	{
		Key: "barbarian_tribute", Title: "The Tribes Want More", Arc: "northern_tribes", Chapter: 2, Theme: ThemeBarbarians, FollowUpOnly: true,
		Prompt:  "The khal you paid has returned, demanding twice as much.",
		Default: "refuse",
		Options: []MatterOption{
			{Key: "pay", Label: "Pay again", Hint: "Peace, at a price. They may be back.",
				Success: MatterOutcome{Text: "They take the gold and go home again.",
					Realm: R{AttrTreasury: -20, AttrStability: 1}, FollowUps: []FollowUp{{"barbarian_tribute", 0.4, 4}}}},
			{Key: "refuse", Label: "Refuse and man the walls", Hint: "Call their bluff.",
				Leans: L{LeanMilitary: 1.5}, Check: chk(30, AttrMilitary, 0.7),
				Success: MatterOutcome{Text: "They test the walls, find them strong, and do not return.",
					Realm: R{AttrMilitary: -4, AttrStability: 3}},
				Failure: MatterOutcome{Text: "They punish the refusal.",
					Realm: R{AttrMilitary: -8, AttrStability: -5}, FollowUps: []FollowUp{{"barbarian_raid", 0.7, 1}}}},
		},
	},

	// ---------- bandits (chain) ----------
	{
		Key: "bandit_roads", Title: "Bandits on the Trade Roads", Arc: "eastern_road", Chapter: 1, Theme: ThemeBandits, Bias: map[Attr]float64{AttrStability: 1},
		Prompt:  "Caravans on the eastern road are being robbed. Merchants refuse to travel without an escort.",
		Default: "nothing",
		Options: []MatterOption{
			{Key: "patrols", Label: "Hire sellswords to patrol", Hint: "Costs the treasury. Good for merchants.",
				Leans: L{LeanMilitary: 0.5},
				Success: MatterOutcome{Text: "Armed patrols make the road safe again.", Sectors: SC{SectorTrade: {Health: 0.1}},
					Realm: R{AttrTreasury: -10, AttrStability: 6}, Roles: roles("merchant", D{Wealth: 15})}},
			{Key: "tolls", Label: "Raise road tolls to pay for patrols", Hint: "Travelers pay. Merchants grumble. Smugglers love a toll road.",
				Leans: L{LeanPopular: -1},
				Success: MatterOutcome{Text: "Patrols return, paid for by the people who use the road.", Sectors: SC{SectorTrade: {Health: -0.1}, SectorBlackMarket: {Health: 0.1}},
					Realm: R{AttrTreasury: 3, AttrStability: 4, AttrPopularity: -4}, Roles: roles("merchant", D{Wealth: -5})}},
			{Key: "nothing", Label: "Leave it to the merchants", Hint: "Not the Senate's problem. Caravans will suffer.",
				Leans: L{LeanTradition: 0.5},
				Success: MatterOutcome{Text: "The robberies continue.", Sectors: SC{SectorTrade: {Health: -0.2}, SectorBlackMarket: {Health: 0.15}},
					Realm: R{AttrStability: -4}, Roles: roles("merchant", D{Wealth: -8}), FollowUps: []FollowUp{{"bandit_lord", 0.7, 2}}}},
		},
	},
	{
		Key: "bandit_lord", Title: "The King of the Eastern Road", Arc: "eastern_road", Chapter: 2, Theme: ThemeBandits, FollowUpOnly: true,
		Prompt:  "The road gangs now answer to one man, who calls himself the King of the Eastern Road.",
		Default: "ignore",
		Options: []MatterOption{
			{Key: "pardon", Label: "Offer him a pardon and a command", Hint: "Turn the wolf into a sheepdog. If he can be trusted.",
				Leans: L{LeanScheme: 2}, Check: chk(30, AttrStability, 0.5),
				Success: MatterOutcome{Text: "He swears loyalty, and his men become a new cohort.",
					Realm: R{AttrMilitary: 8, AttrStability: 5}, Roles: roles("cupbearer", D{Influence: 1})},
				Failure: MatterOutcome{Text: "He takes the pardon and the gold, and keeps raiding.", Sectors: SC{SectorTrade: {Health: -0.1}, SectorBlackMarket: {Health: 0.1}},
					Realm: R{AttrTreasury: -10, AttrStability: -8}, FollowUps: []FollowUp{{"bandit_lord", 0.5, 3}}}},
			{Key: "hunt", Label: "Hunt him down", Hint: "Send soldiers into the hills after him. The lower city's fences will lose a supplier.",
				Leans: L{LeanMilitary: 2}, Check: chk(25, AttrMilitary, 0.8),
				Success: MatterOutcome{Text: "His camp is burned and his head is displayed at the gate.", Sectors: SC{SectorBlackMarket: {Health: -0.25}, SectorTrade: {Health: 0.1}},
					Realm: R{AttrMilitary: -5, AttrStability: 10}, Roles: roles("officer", D{Renown: 1})},
				Failure: MatterOutcome{Text: "The soldiers are led into the marshes and cut apart.",
					Realm: R{AttrMilitary: -10, AttrStability: -8}, FollowUps: []FollowUp{{"bandit_lord", 0.5, 2}}}},
			{Key: "ignore", Label: "Ignore him", Hint: "He's only a bandit.",
				Success: MatterOutcome{Text: "His 'kingdom' grows.", Sectors: SC{SectorTrade: {Health: -0.15}, SectorBlackMarket: {Health: 0.15}},
					Realm: R{AttrStability: -12, AttrTreasury: -5}, Roles: roles("merchant", D{Wealth: -10}),
					FollowUps: []FollowUp{{"bandit_lord", 0.5, 2}}}},
		},
	},

	// ---------- hunger (chain) ----------
	{
		Key: "poor_harvest", Title: "A Poor Harvest", Arc: "hungry_year", Chapter: 1, Theme: ThemeFood, Bias: map[Attr]float64{AttrPopularity: 0.5},
		Prompt:  "The spring rains never came. Grain prices in the capital have doubled.",
		Default: "market",
		Options: []MatterOption{
			{Key: "granaries", Label: "Open the public granaries", Hint: "Costs the treasury. The people will remember.",
				Leans: L{LeanPopular: 2},
				Success: MatterOutcome{Text: "Free grain keeps the city fed.",
					Realm: R{AttrTreasury: -12, AttrPopularity: 8, AttrStability: 3}}},
			{Key: "import", Label: "Buy grain from abroad", Hint: "Cheaper, and our neighbors will be glad of the trade.",
				Leans: L{LeanForeign: 1.5},
				Success: MatterOutcome{Text: "Foreign grain ships arrive in time.", Sectors: SC{SectorTrade: {Health: 0.1}, SectorAgriculture: {Health: -0.05}},
					Realm: R{AttrTreasury: -8, AttrForeign: 4, AttrPopularity: 4},
					Roles: roles("emissary", D{Influence: 1}, "merchant", D{Wealth: 10})}},
			{Key: "seed", Label: "Sell the seed grain abroad", Hint: "Good silver now. But what will the farmers plant in spring? Bad for farms.",
				Leans: L{LeanForeign: 0.5},
				Success: MatterOutcome{Text: "The seed grain sails for Carthage, and silver comes back.", Sectors: SC{SectorAgriculture: {Health: -0.15}},
					Realm: R{AttrTreasury: 8, AttrPopularity: -2}, FollowUps: []FollowUp{{"bill_spring", 1, 4}}}},
			{Key: "market", Label: "Let the market sort it out", Hint: "Costs nothing. Grain sellers will do well.",
				Leans: L{LeanTradition: 0.5, LeanPopular: -1.5},
				Success: MatterOutcome{Text: "Prices soar. The grain merchants grow rich.", Sectors: SC{SectorAgriculture: {Health: 0.2}},
					Realm: R{AttrPopularity: -10, AttrStability: -4}, Roles: roles("merchant", D{Wealth: 25}),
					FollowUps: []FollowUp{{"bread_riots", 0.7, 1}}}},
		},
	},
	{
		Key: "bread_riots", Title: "Bread Riots", Arc: "hungry_year", Chapter: 2, Theme: ThemeFood, FollowUpOnly: true,
		Prompt:  "Hungry crowds have smashed the bakeries in the lower city and are marching on the Forum.",
		Default: "wait",
		Options: []MatterOption{
			{Key: "bread", Label: "Hand out free bread", Hint: "Expensive, but it works.",
				Leans: L{LeanPopular: 2},
				Success: MatterOutcome{Text: "The crowds go home with full bellies.",
					Realm: R{AttrTreasury: -15, AttrPopularity: 10, AttrStability: 4}}},
			{Key: "guard", Label: "Send in the city guard", Hint: "Restore order by force. It could go badly.",
				Leans: L{LeanMilitary: 1, LeanTradition: 1, LeanPopular: -1.5}, Check: chk(30, AttrMilitary, 0.6),
				Success: MatterOutcome{Text: "The crowds are dispersed. Few are hurt.", Sectors: SC{SectorBlackMarket: {Health: -0.1}},
					Realm: R{AttrStability: 5, AttrPopularity: -6}},
				Failure: MatterOutcome{Text: "The guard panics. There is a massacre in the Forum.", Sectors: SC{SectorBlackMarket: {Health: -0.1}, SectorAlchemy: {Health: -0.05}},
					Realm: R{AttrStability: -12, AttrPopularity: -15}}},
			{Key: "wait", Label: "Wait for it to burn out", Hint: "Riots always end eventually. Shops and workshops will be looted.",
				Success: MatterOutcome{Text: "The rioters loot the great houses before they tire.", Sectors: SC{SectorLuxury: {Health: -0.2}, SectorAlchemy: {Health: -0.1}},
					Realm: R{AttrStability: -10, AttrTreasury: -6}, All: D{Wealth: -5}}},
		},
	},

	// ---------- foreign affairs ----------
	{
		Key: "kesh_envoys", Title: "Envoys from Carthage", Theme: ThemeForeign,
		Prompt:  "Envoys from the merchant empire of Carthage offer a trade pact: lower tariffs on both sides.",
		Default: "refuse",
		Options: []MatterOption{
			{Key: "sign", Label: "Sign the pact", Hint: "Good for trade and for relations. Local guilds won't like it.",
				Leans: L{LeanForeign: 2, LeanTradition: -1},
				Success: MatterOutcome{Text: "The pact is sealed. Carthaginian ships fill the harbor.", Sectors: SC{SectorTrade: {Capacity: 5}, SectorLuxury: {Health: 0.15}},
					Realm: R{AttrForeign: 10, AttrTreasury: 6, AttrStability: -2}, Supporters: D{Influence: 0.3},
					Roles: roles("merchant", D{DailyIncome: 2}, "emissary", D{Renown: 1})}},
			{Key: "haggle", Label: "Demand better terms", Hint: "Could win a better deal, or insult them.",
				Leans: L{LeanScheme: 1}, Check: chk(30, AttrForeign, 0.6),
				Success: MatterOutcome{Text: "Carthage gives in. The terms are excellent.", Sectors: SC{SectorTrade: {Capacity: 5}, SectorLuxury: {Health: 0.1}},
					Realm: R{AttrForeign: 3, AttrTreasury: 15}, Roles: roles("merchant", D{DailyIncome: 2})},
				Failure: MatterOutcome{Text: "The envoys leave insulted.",
					Realm: R{AttrForeign: -10}}},
			{Key: "refuse", Label: "Send them home", Hint: "We don't need Carthaginian goods.",
				Leans: L{LeanForeign: -2, LeanTradition: 1},
				Success: MatterOutcome{Text: "The envoys sail home empty-handed.",
					Realm: R{AttrForeign: -6, AttrStability: 1}}},
		},
	},
	{
		Key: "border_dispute", Title: "A Border Dispute", Theme: ThemeForeign, Bias: map[Attr]float64{AttrForeign: 0.5},
		Prompt:  "The Hanseatic League claims the river valley along our border is theirs, and has sent in surveyors.",
		Default: "nothing",
		Options: []MatterOption{
			{Key: "press", Label: "Press our claim with force", Hint: "Win the valley, anger the League.",
				Leans: L{LeanMilitary: 2}, Check: chk(25, AttrMilitary, 0.7),
				Success: MatterOutcome{Text: "The League backs down. The valley is ours.",
					Realm: R{AttrForeign: -6, AttrTreasury: 8, AttrStability: 3}, Supporters: D{Renown: 0.5},
					Roles: roles("officer", D{Renown: 0.5})},
				Failure: MatterOutcome{Text: "Our soldiers are humiliated at the river.", Sectors: SC{SectorTrade: {Health: -0.1}},
					Realm: R{AttrForeign: -10, AttrMilitary: -8, AttrStability: -3}}},
			{Key: "negotiate", Label: "Negotiate a shared border", Hint: "Our emissaries will earn their keep.",
				Leans: L{LeanForeign: 2},
				Success: MatterOutcome{Text: "A treaty divides the valley fairly.",
					Realm: R{AttrForeign: 6, AttrTreasury: -3}, Roles: roles("emissary", D{Influence: 1, Renown: 0.5})}},
			{Key: "nothing", Label: "Ignore their claim", Hint: "Maps don't move rivers.",
				Success: MatterOutcome{Text: "The League takes our silence as weakness.",
					Realm: R{AttrForeign: -3, AttrStability: -1}}},
		},
	},
	{
		Key: "foreign_spy", Title: "A Spy in the Archives", Theme: ThemeForeign, Bias: map[Attr]float64{AttrForeign: 0.5},
		Prompt:  "A Hanseatic agent has been caught copying maps of our forts in the state archives.",
		Default: "return",
		Options: []MatterOption{
			{Key: "execute", Label: "Execute him publicly", Hint: "A warning to the League.",
				Leans: L{LeanTradition: 1, LeanForeign: -1},
				Success: MatterOutcome{Text: "The crowd cheers. The League does not.",
					Realm: R{AttrForeign: -8, AttrStability: 4}}},
			{Key: "turn", Label: "Turn him into our spy", Hint: "Clever, if it works.",
				Leans: L{LeanScheme: 2}, Check: flat(45),
				Success: MatterOutcome{Text: "He now sends the League maps of our own making.",
					Realm: R{AttrMilitary: 5}, Roles: roles("cupbearer", D{Influence: 1}, "scribe", D{Influence: 0.5})},
				Failure: MatterOutcome{Text: "He escapes with the real maps.",
					Realm: R{AttrMilitary: -6, AttrForeign: -4}}},
			{Key: "return", Label: "Quietly send him home", Hint: "Avoid a diplomatic incident.",
				Leans: L{LeanForeign: 1.5},
				Success: MatterOutcome{Text: "The League is grateful. Some here call it weakness.",
					Realm: R{AttrForeign: 4, AttrStability: -2}}},
		},
	},

	// ---------- soldiers ----------
	{
		Key: "mercenaries", Title: "The Golden Company", Arc: "black_lances", Chapter: 1, Theme: ThemeSoldiers,
		Prompt:  "The Golden Company, the most famous sellswords in the world, offer their swords to the Republic. They have never broken a contract.",
		Default: "decline",
		Options: []MatterOption{
			{Key: "hire", Label: "Hire them", Hint: "A big boost to the army. Mercenaries can be trouble. Their camps will need timber.",
				Leans: L{LeanMilitary: 2},
				Success: MatterOutcome{Text: "The Golden Company rides into the capital to cheering crowds.", Sectors: SC{SectorTimber: {Health: 0.1}},
					Realm: R{AttrTreasury: -15, AttrMilitary: 12}, FollowUps: []FollowUp{{"mercenary_pay", 0.7, 3}}}},
			{Key: "decline", Label: "Decline", Hint: "We have our own soldiers.",
				Leans: L{LeanTradition: 1},
				Success: MatterOutcome{Text: "They ride off to sell their swords elsewhere. Some say to our neighbors.",
					Realm: R{AttrForeign: -2}}},
		},
	},
	{
		Key: "mercenary_pay", Title: "The Golden Company Wants Its Pay", Arc: "black_lances", Chapter: 2, Theme: ThemeSoldiers, FollowUpOnly: true,
		Prompt:  "The Golden Company says the Senate broke the contract, and demands double pay, or else.",
		Default: "refuse",
		Options: []MatterOption{
			{Key: "pay", Label: "Pay them", Hint: "Keep the peace.",
				Success: MatterOutcome{Text: "The sellswords count their gold and stay loyal.", Realm: R{AttrTreasury: -15}}},
			{Key: "refuse", Label: "Refuse and dismiss them", Hint: "They may not go quietly.",
				Leans: L{LeanMilitary: 1}, Check: chk(25, AttrMilitary, 0.6),
				Success: MatterOutcome{Text: "The legions surround their camp. The Golden Company leaves peacefully.",
					Realm: R{AttrMilitary: -10, AttrStability: 2}},
				Failure: MatterOutcome{Text: "The sellswords turn bandit.", Sectors: SC{SectorTrade: {Health: -0.15}, SectorBlackMarket: {Health: 0.1}},
					Realm: R{AttrMilitary: -12, AttrStability: -8}, FollowUps: []FollowUp{{"bandit_lord", 0.5, 2}}}},
			{Key: "promise", Label: "Promise payment next month", Hint: "Costs nothing today. They will come to collect.",
				Leans: L{LeanScheme: 0.5},
				Success: MatterOutcome{Text: "The Golden Company accepts a sealed promise, and sharpens its swords.",
					FollowUps: []FollowUp{{"bill_lances", 1, 3}}}},
			{Key: "poison", Label: "Poison their captain", Hint: "A new captain might be more reasonable.",
				Leans: L{LeanScheme: 2.5}, Check: chk(35, AttrStability, 0.3),
				Success: MatterOutcome{Text: "The new captain is far more reasonable.",
					Realm: R{AttrTreasury: -3}, Roles: roles("cupbearer", D{Influence: 1})},
				Failure: MatterOutcome{Text: "The plot is discovered. The sellswords sack a town in revenge.",
					Realm: R{AttrMilitary: -12, AttrStability: -10, AttrForeign: -5}, Supporters: D{Renown: -0.5}}},
		},
	},
	{
		Key: "veterans", Title: "The Veterans Want Land", Theme: ThemeSoldiers,
		Prompt:  "Soldiers who served their twenty years are demanding the farmland they were promised.",
		Default: "refuse",
		Options: []MatterOption{
			{Key: "grant", Label: "Grant them public land", Hint: "Keeps the army loyal. New farms for the realm.",
				Leans: L{LeanPopular: 1, LeanMilitary: 1},
				Success: MatterOutcome{Text: "The veterans settle on new farms, loyal to the Senate.", Sectors: SC{SectorAgriculture: {Capacity: 4}},
					Realm: R{AttrTreasury: -10, AttrMilitary: 5, AttrStability: 3}, Roles: roles("officer", D{Influence: 1})}},
			{Key: "silver", Label: "Pay them off in silver", Hint: "Pricier than land, but the land stays ours.",
				Success: MatterOutcome{Text: "The veterans take the silver and grumble.",
					Realm: R{AttrTreasury: -14, AttrMilitary: 2}}},
			{Key: "refuse", Label: "Refuse", Hint: "A promise is not a contract.",
				Leans: L{LeanTradition: 0.5},
				Success: MatterOutcome{Text: "Angry veterans make poor soldiers.",
					Realm: R{AttrMilitary: -6, AttrStability: -4}, Roles: roles("officer", D{Renown: -0.5})}},
		},
	},
	{
		Key: "city_walls", Title: "The Crumbling Walls", Theme: ThemeWorks, Bias: map[Attr]float64{AttrMilitary: 0.5},
		Prompt:  "Engineers report that the capital's walls are cracking in a dozen places.",
		Default: "leave",
		Options: []MatterOption{
			{Key: "stone", Label: "Rebuild them in stone", Hint: "Expensive and lasting. Quarries and workshops will be busy.",
				Leans: L{LeanMilitary: 1, LeanTradition: 1},
				Success: MatterOutcome{Text: "New walls rise, taller than the old.", Sectors: SC{SectorMining: {Health: 0.15}, SectorTimber: {Health: 0.1}},
					Realm: R{AttrTreasury: -18, AttrMilitary: 8, AttrStability: 3}, Roles: roles("officer", D{Renown: 0.3})}},
			{Key: "patch", Label: "Patch the worst of it", Hint: "Cheap, and good enough for now. For now.",
				Success: MatterOutcome{Text: "The worst cracks are filled.", Sectors: SC{SectorTimber: {Health: 0.1}}, Realm: R{AttrTreasury: -6, AttrMilitary: 2},
					FollowUps: []FollowUp{{"bill_walls", 0.5, 3}}}},
			{Key: "leave", Label: "They've stood this long", Hint: "Costs nothing.",
				Leans:   L{LeanTradition: 0.5},
				Success: MatterOutcome{Text: "The walls keep cracking.", Realm: R{AttrMilitary: -3}}},
		},
	},

	// ---------- money ----------
	{
		Key: "raise_taxes", Title: "A Motion to Raise Taxes", Theme: ThemeMoney, Bias: map[Attr]float64{AttrTreasury: 1},
		Prompt:  "The treasurer proposes new taxes to refill the treasury.",
		Default: "keep",
		Options: []MatterOption{
			{Key: "everyone", Label: "Raise taxes on everyone", Hint: "Fills the treasury. Everyone pays, you too. Every business pays.",
				Leans: L{LeanPopular: -2},
				Success: MatterOutcome{Text: "The tax collectors are busy, and unpopular.", Sectors: AllSectors(-0.05),
					Realm: R{AttrTreasury: 15, AttrPopularity: -8}, All: D{Wealth: -5}}},
			{Key: "wealthy", Label: "Tax the great families", Hint: "The people love it. The great families (that's you) pay more.",
				Leans: L{LeanPopular: 1.5, LeanTradition: -1},
				Success: MatterOutcome{Text: "The great houses pay, and the people cheer.", Sectors: SC{SectorLuxury: {Health: -0.1}, SectorTrade: {Health: -0.05}},
					Realm: R{AttrTreasury: 14, AttrPopularity: 4, AttrStability: -2}, All: D{Wealth: -8},
					Roles: roles("merchant", D{Wealth: -7})}},
			{Key: "borrow", Label: "Borrow from the Iron Bank", Hint: "Gold now, and no one pays a coin today. The bankers will want it back, with interest.",
				Leans: L{LeanForeign: 0.5},
				Success: MatterOutcome{Text: "Iron Bank gold fills the treasury. The bankers of Braavos smile.",
					Realm: R{AttrTreasury: 15}, FollowUps: []FollowUp{{"bill_bankers", 1, 3}}}},
			{Key: "keep", Label: "Keep taxes as they are", Hint: "Nothing changes.",
				Leans:   L{LeanTradition: 1},
				Success: MatterOutcome{Text: "Taxes stay as they are."}},
		},
	},
	{
		Key: "silver_vein", Title: "Silver in the Hills", Theme: ThemeMoney,
		Prompt:  "Miners have struck a rich vein of silver in the western hills.",
		Default: "leave",
		Options: []MatterOption{
			{Key: "state_mine", Label: "Open a state mine", Hint: "Could pay off handsomely, or flood and waste money.",
				Leans: L{LeanTradition: 0.5}, Check: chk(55, AttrStability, 0.2),
				Success: MatterOutcome{Text: "The vein is rich. Silver flows into the treasury.", Sectors: SC{SectorMining: {Capacity: 6}}, Realm: R{AttrTreasury: 18}},
				Failure: MatterOutcome{Text: "The mine floods after a month of costly digging.", Sectors: SC{SectorMining: {Health: -0.2}}, Realm: R{AttrTreasury: -8}}},
			{Key: "sell", Label: "Sell the mining rights", Hint: "Quick money. Some families will do very well. Private miners dig faster.",
				Leans: L{LeanScheme: 0.5},
				Success: MatterOutcome{Text: "A group of great families buys the rights.", Sectors: SC{SectorMining: {Capacity: 9}},
					Realm: R{AttrTreasury: 10, AttrPopularity: -2}, Supporters: D{Wealth: 5},
					Roles: roles("merchant", D{DailyIncome: 3})}},
			{Key: "friends", Label: "Grant the mine to our friends", Hint: "The treasury gets nothing. Those who vote for it get a cut. (You profit if you vote for this.)",
				Leans: L{LeanScheme: 1.5},
				Success: MatterOutcome{Text: "The mine goes to a few grateful families, for a song.", Sectors: SC{SectorMining: {Capacity: 5}},
					Realm: R{AttrTreasury: -6, AttrPopularity: -3}, Supporters: D{Wealth: 25}}},
			{Key: "leave", Label: "Leave the hills alone", Hint: "The silver isn't going anywhere.",
				Leans:   L{LeanTradition: 1},
				Success: MatterOutcome{Text: "The silver stays in the ground."}},
		},
	},
	{
		Key: "treasury_scandal", Title: "Embezzlement in the Treasury", Theme: ThemeMoney,
		Prompt:  "A treasury clerk has been caught moving state silver into private accounts. He claims he was following orders.",
		Default: "ignore",
		Options: []MatterOption{
			{Key: "trial", Label: "Hold a public trial", Hint: "Justice, in front of everyone.",
				Leans: L{LeanTradition: 1, LeanPopular: 1},
				Success: MatterOutcome{Text: "The clerk names names. The people enjoy it.",
					Realm: R{AttrTreasury: 5, AttrPopularity: 6, AttrStability: -3}, Roles: roles("scribe", D{Influence: 1})}},
			{Key: "quiet", Label: "Settle it quietly", Hint: "Get the silver back with no fuss. If it stays quiet.",
				Leans: L{LeanScheme: 2}, Check: flat(60),
				Success: MatterOutcome{Text: "The silver is returned and nobody hears a word.",
					Realm: R{AttrTreasury: 8}, Roles: roles("cupbearer", D{Influence: 0.5})},
				Failure: MatterOutcome{Text: "The cover-up leaks.",
					Realm: R{AttrTreasury: 8, AttrPopularity: -10, AttrStability: -5}, Supporters: D{Renown: -0.5}}},
			{Key: "ignore", Label: "Look away", Hint: "Whose orders, anyway?",
				Success: MatterOutcome{Text: "The clerk keeps his post, and keeps stealing.", Realm: R{AttrTreasury: -10}}},
		},
	},

	// ---------- the people ----------
	{
		Key: "grand_games", Title: "The Grand Games", Theme: ThemePeople,
		Prompt:  "A senator proposes great games in the arena to celebrate the founding of the Republic.",
		Default: "none",
		Options: []MatterOption{
			{Key: "lavish", Label: "Spare no expense", Hint: "Very expensive. Very popular. Merchants of fine goods will do well.",
				Leans: L{LeanPopular: 1.5},
				Success: MatterOutcome{Text: "The games are talked about for years.", Sectors: SC{SectorLuxury: {Health: 0.15}, SectorAgriculture: {Health: 0.05}},
					Realm: R{AttrTreasury: -18, AttrPopularity: 12, AttrStability: 3}, Supporters: D{Renown: 0.5}}},
			{Key: "modest", Label: "Modest games", Hint: "A little fun for a little money.",
				Success: MatterOutcome{Text: "A pleasant afternoon in the arena.", Realm: R{AttrTreasury: -6, AttrPopularity: 5}}},
			{Key: "houses", Label: "Let the great houses run the games", Hint: "The treasury pays; those who vote for it run the games, and keep the takings. (You profit if you vote for this.)",
				Leans: L{LeanScheme: 1, LeanPopular: 0.5},
				Success: MatterOutcome{Text: "The games are fine, and the families who ran them finer still.", Sectors: SC{SectorLuxury: {Health: 0.05}},
					Realm: R{AttrTreasury: -10, AttrPopularity: 5}, Supporters: D{Wealth: 10, Influence: 1}}},
			{Key: "none", Label: "No games this year", Hint: "Save the money.",
				Leans:   L{LeanPopular: -0.5},
				Success: MatterOutcome{Text: "The arena stays empty, and the people notice.", Realm: R{AttrPopularity: -4}}},
		},
	},
	{
		Key: "prophet", Title: "A Prophet in the Forum", Theme: ThemeFaith, Bias: map[Attr]float64{AttrPopularity: 0.5},
		Prompt:  "A barefoot holy man who calls himself the High Sparrow draws crowds in the Forum, calling the Senate corrupt and foretelling its fall.",
		Default: "ignore",
		Options: []MatterOption{
			{Key: "arrest", Label: "Arrest him", Hint: "Silence him. His followers won't like it.",
				Leans:   L{LeanFaith: 1, LeanTradition: 1, LeanMilitary: 0.5, LeanPopular: -1},
				Success: MatterOutcome{Text: "The High Sparrow is dragged off in chains, smiling.", Realm: R{AttrStability: 3, AttrPopularity: -5}}},
			{Key: "buy", Label: "Pay him to preach for us", Hint: "Cheap, if he stays bought.",
				Leans: L{LeanScheme: 2, LeanFaith: -1}, Check: flat(50),
				Success: MatterOutcome{Text: "His sermons now praise the Senate's wisdom.", Sectors: SC{SectorBlackMarket: {Health: 0.05}},
					Realm: R{AttrTreasury: -6, AttrPopularity: 5, AttrStability: 2}},
				Failure: MatterOutcome{Text: "He takes the money and tells the crowd who paid him.",
					Realm: R{AttrTreasury: -6, AttrPopularity: -8}, Supporters: D{Renown: -0.5}}},
			{Key: "ignore", Label: "Let him preach", Hint: "Words are only words. He preaches against luxury.",
				Success: MatterOutcome{Text: "His crowds grow.", Sectors: SC{SectorLuxury: {Health: -0.1}}, Realm: R{AttrStability: -4}}},
		},
	},
	{
		Key: "plague", Title: "Sickness in the Lower City", Arc: "plague", Chapter: 1, Theme: ThemeSickness, Bias: map[Attr]float64{AttrStability: 0.3},
		Prompt:  "A fever is spreading through the crowded tenements by the river.",
		Default: "ignore",
		Options: []MatterOption{
			{Key: "quarantine", Label: "Seal off the district", Hint: "Harsh, but it should stop the spread. The lower city's workshops will close.",
				Leans: L{LeanTradition: 1, LeanPopular: -1},
				Success: MatterOutcome{Text: "Soldiers seal the district. The fever burns itself out inside.", Sectors: SC{SectorAlchemy: {Health: -0.15}, SectorBlackMarket: {Health: -0.1}},
					Realm: R{AttrStability: -3, AttrPopularity: -4, AttrTreasury: -3}, FollowUps: []FollowUp{{"plague_spreads", 0.15, 2}}}},
			{Key: "healers", Label: "Pay for healers", Hint: "Kind, and it may be enough.",
				Leans: L{LeanPopular: 1.5},
				Success: MatterOutcome{Text: "Healers work through the tenements day and night.",
					Realm: R{AttrTreasury: -12, AttrPopularity: 5}, FollowUps: []FollowUp{{"plague_spreads", 0.5, 2}}}},
			{Key: "ignore", Label: "It will pass", Hint: "Fevers come and go.",
				Success: MatterOutcome{Text: "The fever spreads quietly. The apothecaries sell out of every cure, real and otherwise.", Sectors: SC{SectorAlchemy: {Health: 0.1}},
					Realm: R{AttrPopularity: -5}, FollowUps: []FollowUp{{"plague_spreads", 0.9, 2}}}},
		},
	},
	{
		Key: "plague_spreads", Title: "The Plague Spreads", Arc: "plague", Chapter: 2, Theme: ThemeSickness, FollowUpOnly: true,
		Prompt:  "The fever has reached the upper city. Two senators are dead, and the army camps report cases.",
		Default: "pray",
		Options: []MatterOption{
			{Key: "burn", Label: "Burn the infected district", Hint: "Ends the plague. Thousands lose their homes.",
				Leans: L{LeanTradition: 0.5, LeanPopular: -2},
				Success: MatterOutcome{Text: "The fire stops the plague and leaves thousands homeless.", Sectors: SC{SectorAlchemy: {Health: -0.3}, SectorBlackMarket: {Health: -0.2}},
					Realm: R{AttrStability: -8, AttrPopularity: -15}}},
			{Key: "physicians", Label: "Send for foreign physicians", Hint: "Expensive, but they know this fever.",
				Leans: L{LeanForeign: 1.5},
				Success: MatterOutcome{Text: "Carthaginian physicians break the fever within a month.",
					Realm: R{AttrTreasury: -20, AttrForeign: 5, AttrPopularity: 3}, Roles: roles("emissary", D{Influence: 1})}},
			{Key: "pray", Label: "Pray it passes", Hint: "The gods will decide.",
				Leans: L{LeanFaith: 2, LeanTradition: 1.5},
				Success: MatterOutcome{Text: "It does not pass quickly.", Sectors: AllSectors(-0.15),
					Realm: R{AttrPopularity: -12, AttrStability: -8, AttrMilitary: -10}}},
		},
	},
}
