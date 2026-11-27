package game

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// ---- Undertake a Journey (Martial) ----
//
// A player travels across the realm or abroad for gold, influence, renown
// or learning. The departure is announced, and anyone at court may secretly
// pay bandits to waylay them on the road home. All numbers are placeholders.

const KindJourney = "journey"

var (
	DefaultJourneyTiming = PartyTiming{Duration: 3 * time.Hour, EventEarliest: 20 * time.Minute, EventLatest: 150 * time.Minute, AnswerWindow: 20 * time.Minute}
	FastJourneyTiming    = PartyTiming{Duration: 4 * time.Minute, EventEarliest: 30 * time.Second, EventLatest: 150 * time.Second, AnswerWindow: 60 * time.Second}
)

// JourneyDest is somewhere to travel. Abroad journeys take twice as long.
type JourneyDest struct {
	Key         string
	Name        string // "Escort a Trade Caravan"
	Place       string // "the provinces", for announcements
	Emoji       string
	Abroad      bool
	Cost        int // gold up front: travel funds, cargo, gifts
	Description string
	Check       Check
	Stat        Stat   // practised on the way
	Success     Delta  // on a successful journey
	Failure     Delta  // on a failed one
	Realm       Realm  // tracker change on success
	Gift        string // item key sometimes given on a great success
	GoodText    string
	BadText     string
}

// Scaled is the timing for this destination: abroad, everything stretches.
func (d JourneyDest) Scaled(t PartyTiming) PartyTiming {
	if !d.Abroad {
		return t
	}
	return PartyTiming{Duration: 2 * t.Duration, EventEarliest: 2 * t.EventEarliest, EventLatest: 2 * t.EventLatest, AnswerWindow: t.AnswerWindow}
}

var JourneyDests = []JourneyDest{
	{
		Key: "caravan", Name: "Escort a Trade Caravan", Place: "the provinces", Emoji: "🐪", Cost: 60,
		Description: "Buy a cargo of wine and oil and see it safely to the market towns. Sold well, it pays back nearly double.",
		Check:       Check{Base: 35, Weights: map[Attr]float64{AttrStewardship: 3, AttrMartial: 2}},
		Stat:        Stewardship,
		Success:     Delta{Wealth: 105},
		Failure:     Delta{Wealth: 30},
		GoodText:    "The cargo sold for a fine price in every town along the road.",
		BadText:     "The markets were glutted. You sold at a loss just to be rid of it.",
	},
	{
		Key: "provinces", Name: "Progress Through the Provinces", Place: "the provinces", Emoji: "🌾", Cost: 20,
		Description: "Ride from town to town, shaking hands and hearing petitions. The countryside remembers who came to see it.",
		Check:       Check{Base: 35, Weights: map[Attr]float64{AttrDiplomacy: 3, AttrRenown: 2}},
		Stat:        Diplomacy,
		Success:     Delta{Influence: 2, Renown: 3},
		Failure:     Delta{Influence: 0.3},
		Realm:       Realm{AttrPopularity: 2},
		GoodText:    "Cheering crowds in every square. The provinces will speak your name for months.",
		BadText:     "Muddy roads, empty squares and a magistrate who mistook you for a tax collector.",
	},
	{
		Key: "carthage", Name: "Embassy to Carthage", Place: "Carthage", Emoji: "⛵", Abroad: true, Cost: 80,
		Description: "Sail to the merchant empire with gifts and fine words. A good embassy warms Carthage to the Republic, and to you.",
		Check:       Check{Base: 30, Weights: map[Attr]float64{AttrDiplomacy: 3, AttrInfluence: 0.5}},
		Stat:        Diplomacy,
		Success:     Delta{Influence: 3, Wealth: 60},
		Failure:     Delta{Influence: 0.5},
		Realm:       Realm{AttrForeign: 3},
		Gift:        "tyrian_cloak",
		GoodText:    "The Carthaginian council feasted you for a week and loaded your ship with presents.",
		BadText:     "The suffetes kept you waiting for days, then sent you home with a polite letter.",
	},
	{
		Key: "gondor", Name: "Tourney at Minas Tirith", Place: "Gondor", Emoji: "🏆", Abroad: true, Cost: 70,
		Description: "Ride to the White City and break lances before the King of Gondor. Winners go home rich and famous.",
		Check:       Check{Base: 25, Weights: map[Attr]float64{AttrProwess: 4, AttrMartial: 2}},
		Stat:        Prowess,
		Success:     Delta{Renown: 10, Wealth: 50, Stats: Stats{Prowess: 1.5}},
		Failure:     Delta{Renown: 1},
		Realm:       Realm{AttrForeign: 1},
		Gift:        "gondor_shield",
		GoodText:    "You unhorsed knight after knight. The King of Gondor himself handed you the prize.",
		BadText:     "Unhorsed in the first pass, in front of the whole White City.",
	},
	{
		Key: "alexandria", Name: "Seek the Library of Alexandria", Place: "Alexandria", Emoji: "📜", Abroad: true, Cost: 60,
		Description: "Spend weeks among the scrolls of the greatest library in the world. You come home wiser, and with secrets.",
		Check:       Check{Base: 35, Weights: map[Attr]float64{AttrStewardship: 3, AttrIntrigue: 2}},
		Stat:        Stewardship,
		Success:     Delta{Renown: 3, Stats: Stats{Stewardship: 1.5, Intrigue: 1.5}},
		Failure:     Delta{Stats: Stats{Stewardship: 0.5}},
		Gift:        "alexandrian_scroll",
		GoodText:    "The librarians opened the locked stacks for you. Your notebooks are full.",
		BadText:     "The librarians found you tiresome and kept you to the public reading room.",
	},
}

func JourneyDestByKey(k string) (JourneyDest, bool) {
	for _, d := range JourneyDests {
		if d.Key == k {
			return d, true
		}
	}
	return JourneyDest{}, false
}

// journeyGifts are the foreign presents a great journey may bring home.
// They aren't sold in the shop, but can be sold or traded like anything else.
var journeyGifts = []Item{
	good("tyrian_cloak", "A Cloak of Tyrian Purple", KindArt, 4, 150, ShelfNone, false, Effects{PartyBonus: 1},
		"A gift from the Carthaginian council, dyed with the purple only kings can afford."),
	good("gondor_shield", "A Shield from Minas Tirith", KindArmor, 3, 300, ShelfNone, false, Effects{Stats: pro(5)},
		"Painted with the White Tree, and dented from a famous tourney."),
	good("alexandrian_scroll", "A Copy from the Library of Alexandria", KindArtifact, 3, 150, ShelfNone, false, Effects{Stats: Stats{Stewardship: 5}},
		"A scholar's careful copy of a treatise nobody else in the Republic has read."),
}

func init() { Items = append(Items, journeyGifts...) }

const (
	journeyGiftMargin = 25   // a success by this much may bring a gift
	journeyGiftChance = 50   // percent, when it does
	journeyQualityAmb = 0.5  // share of the journey's quality that helps against bandits
	journeyWoundOdds  = 50   // percent chance a full robbery wounds the traveller
	journeyHirerShare = 0.5  // of the loot, to whoever paid the bandits
	robbedMargin      = -30  // at or below: robbed
	namedMargin       = 30   // at or above (plus the hirer's cunning): a bandit talks
	namedPerIntrigue  = 2.5  // the hirer's Intrigue covers their tracks
	robbedShare       = 0.6  // of the purse, on a full robbery
	partlyShare       = 0.25 // on a partial one
	namedRenown       = 15   // the hirer, once named
	namedOpinion      = -4.0 // every senator
	namedOutraged     = -8.0 // Stern and Incorruptible senators
)

// JourneyResult is how the journey itself went, before any bandits.
type JourneyResult struct {
	Success bool
	Delta   Delta
	Realm   Realm
	Gift    string // item key, or ""
	Text    string
}

// ResolveJourney rolls the journey. quality is the sum of the road events.
func ResolveJourney(r Roller, p Person, d JourneyDest, quality float64) JourneyResult {
	ok, margin := d.Check.Roll(r, p.Sheet, quality, Normal)
	res := JourneyResult{Success: ok}
	if ok {
		res.Delta = d.Success
		res.Realm = d.Realm
		res.Text = d.GoodText
		if d.Gift != "" && margin >= journeyGiftMargin && r.IntN(100) < journeyGiftChance {
			res.Gift = d.Gift
		}
	} else {
		res.Delta = d.Failure
		res.Text = d.BadText
	}
	res.Delta = ScaleGain(res.Delta, p.Sheet[AttrInfluence])
	res.Delta.Merge(Practice(d.Stat, ok))
	return res
}

// Purse is the gold on the road home that bandits can take: the travel
// funds and goods, plus whatever the journey earned.
func (r JourneyResult) Purse(d JourneyDest) int { return d.Cost + max(r.Delta.Wealth, 0) }

// ---- bandits ----

type BanditBand struct {
	Key      string
	Name     string
	Price    int
	Strength float64 // off the traveller's defence roll
	Reward   Delta   // to a traveller who beats them
	Desc     string
}

var BanditBands = []BanditBand{
	{Key: "cutthroats", Name: "A few cutthroats", Price: 40, Strength: 0,
		Reward: Delta{Renown: 3, Stats: Stats{Prowess: 1, Martial: 0.5}},
		Desc:   "Half a dozen desperate men with clubs and knives. Cheap, and easily scared."},
	{Key: "outlaws", Name: "The Bloody Mummers", Price: 100, Strength: 15,
		Reward: Delta{Renown: 6, Stats: Stats{Prowess: 1.5, Martial: 1}},
		Desc:   "Twenty cutthroats from every land, in motley and stolen armor, who know every pass and ambush on the road."},
	{Key: "sellswords", Name: "The Black Company", Price: 250, Strength: 30,
		Reward: Delta{Renown: 10, Stats: Stats{Prowess: 2.5, Martial: 1.5}},
		Desc:   "The most famous sellsword company in the world, between wars, armored and mounted. Anyone who beats them will be famous."},
}

// Phrase is the band's name mid-sentence: "a few cutthroats", "the Black
// Company". Only the leading article is lowercased.
func (b BanditBand) Phrase() string {
	if b.Name == "" {
		return ""
	}
	r := []rune(b.Name)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func BanditBandByKey(k string) (BanditBand, bool) {
	for _, b := range BanditBands {
		if b.Key == k {
			return b, true
		}
	}
	return BanditBand{}, false
}

// BanditGoal is how a hired band is stored on the journey.
func BanditGoal(band string) string { return "bandits:" + band }

// BandFromGoal reads a stored band back.
func BandFromGoal(goal string) (BanditBand, bool) {
	k, ok := strings.CutPrefix(goal, "bandits:")
	if !ok {
		return BanditBand{}, false
	}
	return BanditBandByKey(k)
}

// Defence: martial command of the escort, a strong sword arm, and the guards
// and horses an estate can afford.
var ambushCheck = Check{Base: 35, Weights: map[Attr]float64{AttrMartial: 3, AttrProwess: 3, AttrEstate: 0.3}}

// AmbushOutcome is how one band's attack went.
type AmbushOutcome int

const (
	AmbushRobbed AmbushOutcome = iota
	AmbushPartly
	AmbushDrivenOff
	AmbushNamed // driven off, and a captured bandit named who paid them
)

type AmbushResult struct {
	Outcome    AmbushOutcome
	Loot       int // gold taken from the traveller
	Cut        int // the hirer's share
	Wounded    bool
	Traveler   Delta
	Hirer      Delta
	Opinions   []OpinionChange // of the hirer, when named
	ToTraveler string
	ToHirer    string
	Public     string // only when named
}

// ResolveAmbush rolls one band's attack on a traveller carrying purse gold.
// quality is how well the journey went (good planning helps on the road).
func ResolveAmbush(r Roller, traveler, hirer Person, band BanditBand, purse int, quality float64, senators []Senator) AmbushResult {
	_, m := ambushCheck.Roll(r, traveler.Sheet, quality*journeyQualityAmb-band.Strength, Normal)
	purse = max(purse, 0)
	var res AmbushResult
	name := band.Phrase()
	switch {
	case m <= robbedMargin:
		res.Outcome = AmbushRobbed
		res.Loot = int(float64(purse) * robbedShare)
		res.Wounded = r.IntN(100) < journeyWoundOdds
		res.ToTraveler = fmt.Sprintf("On the road home, %s fell on your party and took **%d gold**.", name, res.Loot)
		if res.Wounded {
			res.ToTraveler += " You took a wound fighting them off."
		}
		res.ToTraveler += " Someone paid them; they knew exactly when you'd pass."
	case m <= 0:
		res.Outcome = AmbushPartly
		res.Loot = int(float64(purse) * partlyShare)
		res.ToTraveler = fmt.Sprintf("On the road home, %s ambushed your party. You fought your way through, but they made off with **%d gold**. Someone paid them.", name, res.Loot)
	default:
		res.Outcome = AmbushDrivenOff
		res.Traveler = band.Reward
		res.Traveler.Merge(Practice(Martial, true))
		res.ToTraveler = fmt.Sprintf("On the road home, %s ambushed your party. You beat them off and rode on with your purse.", name)
		if float64(m) >= namedMargin+namedPerIntrigue*hirer.Sheet[AttrIntrigue] {
			res.Outcome = AmbushNamed
			res.ToTraveler += fmt.Sprintf(" One of them, bound and frightened, told you who paid them: **%s**.", hirer.Name)
			res.Hirer = Delta{Renown: -namedRenown}
			res.Public = fmt.Sprintf("Bandits waylaid **%s** on the road, and were beaten. A captured bandit swears **%s** paid them.", traveler.Name, hirer.Name)
			for _, s := range senators {
				d := namedOpinion
				if s.Has(TraitStern) || s.Has(TraitIncorruptible) {
					d = namedOutraged
				}
				res.Opinions = append(res.Opinions, OpinionChange{SenatorID: s.ID, CharacterID: hirer.ID, Delta: d})
			}
		}
		res.ToTraveler = withEffects(res.ToTraveler, res.Traveler)
	}
	res.Cut = int(float64(res.Loot) * journeyHirerShare)
	res.Traveler.Merge(Delta{Wealth: -res.Loot})
	res.Hirer.Merge(Delta{Wealth: res.Cut})
	switch res.Outcome {
	case AmbushRobbed:
		res.ToHirer = fmt.Sprintf("Your bandits robbed %s on the road. Your share: **%d gold**.", traveler.Name, res.Cut)
	case AmbushPartly:
		res.ToHirer = fmt.Sprintf("Your bandits hit %s on the road, but only got away with a little. Your share: **%d gold**.", traveler.Name, res.Cut)
	case AmbushDrivenOff:
		res.ToHirer = fmt.Sprintf("%s beat off your bandits. Your gold is gone, but nobody knows it was yours.", traveler.Name)
	case AmbushNamed:
		res.ToHirer = withEffects(fmt.Sprintf("%s beat off your bandits and took one alive. He talked. The whole court knows you paid them, and the Senate thinks less of you.", traveler.Name), res.Hirer)
	}
	return res
}

// ---- road events ----

// JourneyEvents can happen on any journey; JourneyAbroadEvents only abroad.
var (
	JourneyEvents = []PartyEvent{
		{
			Key: "road_bridge", Title: "The bridge is out",
			Prompt: "Spring floods have taken the bridge. The river is high and brown.",
			Choices: []EventChoice{
				{Key: "ford", Label: "Find a ford and cross", Check: chk(20, AttrMartial, 6),
					Success: EventOutcome{Text: "You find the shallows and bring everyone across.", Quality: 6},
					Failure: EventOutcome{Text: "A wagon overturns midstream. You lose a day drying everything out.", Quality: -8}},
				{Key: "around", Label: "Go the long way round",
					Success: EventOutcome{Text: "Three extra days on the road, but nothing lost.", Quality: -2}},
			},
			Default: "around",
		},
		{
			Key: "road_toll", Title: "Walder Frey's toll",
			Prompt: "Old Walder Frey has chained the bridge at the Twins shut, and wants silver to open it.",
			Choices: []EventChoice{
				{Key: "pay", Label: "Pay the toll (15 gold)",
					Success: EventOutcome{Text: "Lord Frey cackles and drops the chain. Robbery with manners.", Quality: 2, Delta: Delta{Wealth: -15}}},
				{Key: "talk", Label: "Talk your way through", Check: chk(20, AttrDiplomacy, 6),
					Success: EventOutcome{Text: "By the end he's apologizing, and offers you dinner.", Quality: 5},
					Failure: EventOutcome{Text: "He doubles the price for wasting his time.", Quality: -3, Delta: Delta{Wealth: -30}}},
				{Key: "force", Label: "Break the chain", Check: chk(15, AttrProwess, 6),
					Success: EventOutcome{Text: "His many sons take one look at your escort and step aside.", Quality: 4, Delta: Delta{Renown: 2}},
					Failure: EventOutcome{Text: "A scuffle, a bloody nose, and you pay anyway.", Quality: -5, Delta: Delta{Wealth: -15}}},
			},
			Default: "pay",
		},
		{
			Key: "road_stranger", Title: "A wounded stranger",
			Prompt: "A man lies bleeding by the road. He says bandits took everything he had.",
			Choices: []EventChoice{
				{Key: "help", Label: "Tend him and take him along",
					Success: EventOutcome{Text: "He's a road warden. He knows where the bandits lie in wait, and tells you.", Quality: 6}},
				{Key: "pass", Label: "Ride past",
					Success: EventOutcome{Text: "Probably a trick. Probably.", Quality: 0}},
			},
			Default: "pass",
		},
		{
			Key: "road_inn", Title: "A crowded inn",
			Prompt: "The only inn for miles is full of rough drovers who don't like the look of you.",
			Choices: []EventChoice{
				{Key: "buy", Label: "Buy a round for the house (10 gold)",
					Success: EventOutcome{Text: "Friends for life, or at least for the night. They tell you which roads are watched.", Quality: 5, Delta: Delta{Wealth: -10}}},
				{Key: "camp", Label: "Camp outside", Check: chk(25, AttrMartial, 5),
					Success: EventOutcome{Text: "A cold night, but a well-kept watch.", Quality: 2},
					Failure: EventOutcome{Text: "Someone cuts your horses loose in the night.", Quality: -7}},
			},
			Default: "camp",
		},
	}
	JourneyAbroadEvents = []PartyEvent{
		{
			Key: "road_storm", Title: "A storm at sea",
			Prompt: "Black clouds roll in from the west. The captain wants to run for harbor; the harbor is two days back.",
			Choices: []EventChoice{
				{Key: "ride", Label: "Ride it out", Check: chk(20, AttrMartial, 6),
					Success: EventOutcome{Text: "A wild night, but you keep the crew at their posts and the ship afloat.", Quality: 6},
					Failure: EventOutcome{Text: "The mast cracks. You limp on, days late and half your cargo overboard.", Quality: -10}},
				{Key: "turn", Label: "Turn back for harbor",
					Success: EventOutcome{Text: "Safe, dry, and late.", Quality: -3}},
			},
			Default: "turn",
		},
		{
			Key: "road_dothraki", Title: "Dothraki outriders",
			Prompt: "A dozen Dothraki riders shadow your party along the ridge, closer every hour.",
			Choices: []EventChoice{
				{Key: "gift", Label: "Send them a gift (20 gold)",
					Success: EventOutcome{Text: "They take the gift, laugh, and ride away.", Quality: 2, Delta: Delta{Wealth: -20}}},
				{Key: "parley", Label: "Ride out and parley", Check: chk(15, AttrDiplomacy, 6),
					Success: EventOutcome{Text: "You share wine with their leader, who swears you're under his protection.", Quality: 8},
					Failure: EventOutcome{Text: "They take the parley as an insult, and some of your horses as payment.", Quality: -8}},
				{Key: "fight", Label: "Charge them", Check: chk(10, AttrProwess, 6),
					Success: EventOutcome{Text: "They scatter. Your escort will tell this story for years.", Quality: 6, Delta: Delta{Renown: 3}},
					Failure: EventOutcome{Text: "Horse archers. You lose men and pride.", Quality: -10, Condition: CondWounded}},
			},
			Default: "gift",
		},
	}
)

// JourneyEventsFor is the pool of road events for a destination.
func JourneyEventsFor(d JourneyDest) []PartyEvent {
	if d.Abroad {
		return append(append([]PartyEvent{}, JourneyEvents...), JourneyAbroadEvents...)
	}
	return JourneyEvents
}
