package game

import (
	"fmt"
	"math"
	"strings"
)

// Effects are an estate item's small, lasting bonuses. Only kinds the rules
// already use are here; add more as systems need them.
type Effects struct {
	Stats       Stats   `json:"stats,omitempty"`
	DailyIncome int     `json:"daily_income,omitempty"`
	PartyBonus  float64 `json:"party_bonus,omitempty"`  // percent added to parties you host
	RumorNotice float64 `json:"rumor_notice,omitempty"` // percent added to your rolls to notice rumors and spies (and against spies planted on you)
	VoteWeight  float64 `json:"vote_weight,omitempty"`  // added to your weight in the Senate
}

func (e *Effects) Add(o Effects) {
	if len(o.Stats) > 0 {
		if e.Stats == nil {
			e.Stats = Stats{}
		}
		e.Stats.Add(o.Stats)
	}
	e.DailyIncome += o.DailyIncome
	e.PartyBonus += o.PartyBonus
	e.RumorNotice += o.RumorNotice
	e.VoteWeight += o.VoteWeight
}

// Describe renders effects like "+5 diplomacy · +3 gold/day".
func (e Effects) Describe() string {
	var parts []string
	for _, st := range AllStats {
		if v := e.Stats[st]; v != 0 {
			parts = append(parts, fmt.Sprintf("%+g %s", v, st))
		}
	}
	if e.DailyIncome != 0 {
		parts = append(parts, fmt.Sprintf("%+d gold/day", e.DailyIncome))
	}
	if e.PartyBonus != 0 {
		parts = append(parts, fmt.Sprintf("%+g%% party success", e.PartyBonus))
	}
	if e.RumorNotice != 0 {
		parts = append(parts, fmt.Sprintf("%+g%% to notice rumors and spies", e.RumorNotice))
	}
	if e.VoteWeight != 0 {
		parts = append(parts, fmt.Sprintf("%+g Senate vote weight", e.VoteWeight))
	}
	return strings.Join(parts, " · ")
}

type ItemKind string

const (
	KindProperty   ItemKind = "property" // somewhere to live; see Estate
	KindPossession ItemKind = "possession"
	KindArt        ItemKind = "art"
	KindArtifact   ItemKind = "artifact"
	KindRelic      ItemKind = "relic" // war relics
	KindWeapon     ItemKind = "weapon"
	KindArmor      ItemKind = "armor"
	KindEquipment  ItemKind = "equipment"
)

// KindInfo is how a kind is shown.
type KindInfo struct {
	Kind  ItemKind
	Name  string // plural, for headings
	Emoji string
}

var Kinds = []KindInfo{
	{KindProperty, "Properties", "🏠"},
	{KindArt, "Art", "🖼️"},
	{KindArtifact, "Artifacts", "🏺"},
	{KindRelic, "War relics", "🚩"},
	{KindWeapon, "Weapons", "⚔️"},
	{KindArmor, "Armor", "🛡️"},
	{KindEquipment, "Equipment", "🐎"},
	{KindPossession, "Keepsakes", "📦"},
}

func KindByKey(k ItemKind) KindInfo {
	for _, ki := range Kinds {
		if ki.Kind == k {
			return ki
		}
	}
	return KindInfo{k, string(k), "📦"}
}

// Shelf is where the shop sells an item.
type Shelf string

const (
	ShelfNone   Shelf = ""       // not sold (starting estates)
	ShelfFixed  Shelf = "fixed"  // always in stock
	ShelfMarket Shelf = "market" // only on days the rotating market has it
)

// Tier ranks properties, humble to castle.
type Tier int

const (
	TierHumble Tier = iota + 1
	TierModest
	TierFine
	TierGrand
	TierPalace
	TierCastle
)

var tierNames = map[Tier]string{TierHumble: "Humble", TierModest: "Modest", TierFine: "Fine",
	TierGrand: "Grand", TierPalace: "Palace", TierCastle: "Castle"}

func (t Tier) String() string { return tierNames[t] }

// Item is something in a character's estate. Value adds to estate value.
// Unique items have one owner per court at a time.
type Item struct {
	Key         string
	Name        string
	Kind        ItemKind
	Tier        Tier // properties only
	Description string
	Value       float64
	Price       int // what the shop charges; sell-back is SellBackRate of it
	Unique      bool
	Shelf       Shelf
	Effects     Effects
	Magic       bool // enchanted or uncanny; see the Witchcraft Act
}

func (it Item) IsProperty() bool { return it.Kind == KindProperty }

// SellBackRate is the share of an item's price the shop pays to buy it back.
const SellBackRate = 0.6

func (it Item) SellBack() int { return int(float64(it.Price) * SellBackRate) }

// SellBackUnder is SellBack under the laws: under the Witchcraft Act,
// nervous owners get more for magic items.
func (it Item) SellBackUnder(ls Laws) int {
	if f := ls.Rules().MagicSellBack; it.Magic && f != 0 {
		return int(float64(it.Price) * f)
	}
	return it.SellBack()
}

// PriceUnder is what the shop charges under the laws (Debased Coinage
// raises every price).
func (it Item) PriceUnder(ls Laws) int {
	if f := ls.Rules().ShopPrices; f != 0 {
		return int(float64(it.Price)*f + 0.5)
	}
	return it.Price
}

// Items is the estate catalogue. Placeholder numbers.
var Items = []Item{
	// homes
	{Key: "crumbling_manor", Name: "Crumbling Manor", Kind: KindProperty, Value: 12, Price: 180,
		Description: "A great house with a leaking roof and half its rooms shut. The hall still impresses guests.",
		Effects:     Effects{PartyBonus: 2}},
	{Key: "smallholding", Name: "Veteran's Smallholding", Kind: KindProperty, Value: 5, Price: 80,
		Description: "A few fields granted to your family for its service.",
		Effects:     Effects{DailyIncome: 3}},
	{Key: "townhouse", Name: "Townhouse above the Shop", Kind: KindProperty, Value: 8, Price: 120,
		Description: "Three narrow floors in the market district, with the family business on the ground floor.",
		Effects:     Effects{DailyIncome: 3}},
	{Key: "temple_cell", Name: "Cell in the Temple Quarter", Kind: KindProperty, Value: 2, Price: 30,
		Description: "A bare room and a cot. The priests next door hear every confession in the city.",
		Effects:     Effects{RumorNotice: 5}},
	{Key: "rented_room", Name: "Rented Room in the Lower City", Kind: KindProperty, Value: 1, Price: 20,
		Description: "One room, one window, and a landlord who doesn't ask questions."},
	{Key: "hill_villa", Name: "Rented Villa on the Hill", Kind: KindProperty, Value: 9, Price: 140,
		Description: "A pleasant villa leased with the last of your foreign silver.",
		Effects:     Effects{PartyBonus: 2}},
	{Key: "hill_farm", Name: "Hill Farm with Orchards", Kind: KindProperty, Value: 10, Price: 160,
		Description: "Apple orchards, a mill, and tenants who pay their rent.",
		Effects:     Effects{DailyIncome: 5}},
	{Key: "arena_shack", Name: "Shack by the Arena", Kind: KindProperty, Value: 2, Price: 30,
		Description: "Close enough to hear the crowd roar on game days."},

	// possessions
	{Key: "signet_ring", Name: "Ancestral Signet Ring", Kind: KindPossession, Value: 4, Price: 60,
		Description: "The seal of your house, older than the Republic.",
		Effects:     Effects{Stats: Stats{Diplomacy: 5}}},
	{Key: "fathers_sword", Name: "Father's Sword", Kind: KindPossession, Value: 3, Price: 45,
		Description: "Plain, well kept, and notched from three campaigns.",
		Effects:     Effects{Stats: Stats{Prowess: 5}}},
	{Key: "ledger", Name: "Ledger of Debts", Kind: KindPossession, Value: 3, Price: 60,
		Description: "Half the market owes your family money, and it's all written down.",
		Effects:     Effects{DailyIncome: 3}},
	{Key: "relic", Name: "Relic of a Minor Saint", Kind: KindPossession, Value: 5, Price: 80,
		Description: "A finger bone in a silver box. The pious come from far away to see it.",
		Effects:     Effects{PartyBonus: 4}},
	{Key: "informants", Name: "Ring of Informants", Kind: KindPossession, Value: 2, Price: 40,
		Description: "Street children who tell you what they hear, for a coin or a meal.",
		Effects:     Effects{RumorNotice: 10}},
	{Key: "letters", Name: "Letters of Introduction", Kind: KindPossession, Value: 2, Price: 50,
		Description: "Sealed letters from a foreign court. They open senators' doors.",
		Effects:     Effects{VoteWeight: 2}},
	{Key: "tapestry", Name: "Exotic Tapestry", Kind: KindPossession, Value: 6, Price: 90,
		Description: "A woven hunting scene from your homeland. Guests always ask about it.",
		Effects:     Effects{PartyBonus: 3}},
	{Key: "warhorse", Name: "Prize Warhorse", Kind: KindPossession, Value: 5, Price: 80,
		Description: "A tall gray stallion, the envy of every officer in the city.",
		Effects:     Effects{Stats: Stats{Martial: 5}}},
	{Key: "rudis", Name: "The Wooden Sword of Freedom", Kind: KindPossession, Value: 3, Price: 45,
		Description: "Given to gladiators who win their freedom. The crowds still recognize it.",
		Effects:     Effects{PartyBonus: 2}},
}

func ItemByKey(key string) (Item, bool) {
	return itemIndex.get(key)
}

// OtherPropertyShare is how much a property you don't live in counts
// toward estate value.
const OtherPropertyShare = 0.5

// EstateValue totals the value of the given items. By convention the first
// property in keys is the residence: it and every possession count fully,
// other properties count OtherPropertyShare.
func EstateValue(keys []string) float64 {
	v := 0.0
	home := false
	for _, k := range keys {
		it, ok := ItemByKey(k)
		switch {
		case !ok:
		case it.IsProperty() && home:
			v += it.Value * OtherPropertyShare
		default:
			home = home || it.IsProperty()
			v += it.Value
		}
	}
	return v
}

// SumEffects totals the effects of the given items, residence first as in
// EstateValue. A property you don't live in only gives its income and
// stats; party bonus, rumor-noticing and vote weight come from where you live.
func SumEffects(keys []string) Effects {
	var e Effects
	home := false
	for _, k := range keys {
		it, ok := ItemByKey(k)
		switch {
		case !ok:
		case it.IsProperty() && home:
			e.Add(Effects{Stats: it.Effects.Stats, DailyIncome: it.Effects.DailyIncome})
		default:
			home = home || it.IsProperty()
			e.Add(it.Effects)
		}
	}
	return e
}

// EstateScore is how estate value feeds checks: full value at first, then
// diminishing, so a castle helps a lot but doesn't guarantee everything.
func EstateScore(value float64) float64 {
	if value <= 0 {
		return 0
	}
	return estateScoreScale * math.Log(1+value/estateScoreScale)
}

const estateScoreScale = 30
