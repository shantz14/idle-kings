package game

import (
	"fmt"
	"strings"
)

// Effects are an estate item's small, lasting bonuses. Only kinds the rules
// already use are here; add more as systems need them.
type Effects struct {
	Stats       Stats   `json:"stats,omitempty"`
	DailyIncome int     `json:"daily_income,omitempty"`
	PartyBonus  float64 `json:"party_bonus,omitempty"`  // percent added to parties you host
	RumorNotice float64 `json:"rumor_notice,omitempty"` // percent added to your rolls to notice rumors
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

// Describe renders effects like "+1 diplomacy · +3 gold/day".
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
		parts = append(parts, fmt.Sprintf("%+g%% to notice rumors", e.RumorNotice))
	}
	if e.VoteWeight != 0 {
		parts = append(parts, fmt.Sprintf("%+g Senate vote weight", e.VoteWeight))
	}
	return strings.Join(parts, " · ")
}

type ItemKind string

const (
	KindHome       ItemKind = "home"
	KindPossession ItemKind = "possession"
)

// Item is something in a character's estate. Value adds to estate value.
type Item struct {
	Key         string
	Name        string
	Kind        ItemKind
	Description string
	Value       float64
	Effects     Effects
}

// Items is the estate catalogue. Placeholder numbers.
var Items = []Item{
	// homes
	{Key: "crumbling_manor", Name: "Crumbling Manor", Kind: KindHome, Value: 12,
		Description: "A great house with a leaking roof and half its rooms shut. The hall still impresses guests.",
		Effects:     Effects{PartyBonus: 2}},
	{Key: "smallholding", Name: "Veteran's Smallholding", Kind: KindHome, Value: 5,
		Description: "A few fields granted to your family for its service.",
		Effects:     Effects{DailyIncome: 3}},
	{Key: "townhouse", Name: "Townhouse above the Shop", Kind: KindHome, Value: 8,
		Description: "Three narrow floors in the market district, with the family business on the ground floor.",
		Effects:     Effects{DailyIncome: 3}},
	{Key: "temple_cell", Name: "Cell in the Temple Quarter", Kind: KindHome, Value: 2,
		Description: "A bare room and a cot. The priests next door hear every confession in the city.",
		Effects:     Effects{RumorNotice: 5}},
	{Key: "rented_room", Name: "Rented Room in the Lower City", Kind: KindHome, Value: 1,
		Description: "One room, one window, and a landlord who doesn't ask questions."},
	{Key: "hill_villa", Name: "Rented Villa on the Hill", Kind: KindHome, Value: 9,
		Description: "A pleasant villa leased with the last of your foreign silver.",
		Effects:     Effects{PartyBonus: 2}},
	{Key: "hill_farm", Name: "Hill Farm with Orchards", Kind: KindHome, Value: 10,
		Description: "Apple orchards, a mill, and tenants who pay their rent.",
		Effects:     Effects{DailyIncome: 5}},
	{Key: "arena_shack", Name: "Shack by the Arena", Kind: KindHome, Value: 2,
		Description: "Close enough to hear the crowd roar on game days."},

	// possessions
	{Key: "signet_ring", Name: "Ancestral Signet Ring", Kind: KindPossession, Value: 4,
		Description: "The seal of your house, older than the Republic.",
		Effects:     Effects{Stats: Stats{Diplomacy: 1}}},
	{Key: "fathers_sword", Name: "Father's Sword", Kind: KindPossession, Value: 3,
		Description: "Plain, well kept, and notched from three campaigns.",
		Effects:     Effects{Stats: Stats{Prowess: 1}}},
	{Key: "ledger", Name: "Ledger of Debts", Kind: KindPossession, Value: 3,
		Description: "Half the market owes your family money, and it's all written down.",
		Effects:     Effects{DailyIncome: 3}},
	{Key: "relic", Name: "Relic of a Minor Saint", Kind: KindPossession, Value: 5,
		Description: "A finger bone in a silver box. The pious come from far away to see it.",
		Effects:     Effects{PartyBonus: 4}},
	{Key: "informants", Name: "Ring of Informants", Kind: KindPossession, Value: 2,
		Description: "Street children who tell you what they hear, for a coin or a meal.",
		Effects:     Effects{RumorNotice: 10}},
	{Key: "letters", Name: "Letters of Introduction", Kind: KindPossession, Value: 2,
		Description: "Sealed letters from a foreign court. They open senators' doors.",
		Effects:     Effects{VoteWeight: 2}},
	{Key: "tapestry", Name: "Exotic Tapestry", Kind: KindPossession, Value: 6,
		Description: "A woven hunting scene from your homeland. Guests always ask about it.",
		Effects:     Effects{PartyBonus: 3}},
	{Key: "warhorse", Name: "Prize Warhorse", Kind: KindPossession, Value: 5,
		Description: "A tall gray stallion, the envy of every officer in the city.",
		Effects:     Effects{Stats: Stats{Martial: 1}}},
	{Key: "rudis", Name: "The Wooden Sword of Freedom", Kind: KindPossession, Value: 3,
		Description: "Given to gladiators who win their freedom. The crowds still recognize it.",
		Effects:     Effects{PartyBonus: 2}},
}

func ItemByKey(key string) (Item, bool) {
	for _, it := range Items {
		if it.Key == key {
			return it, true
		}
	}
	return Item{}, false
}

// EstateValue totals the value of the given items.
func EstateValue(keys []string) float64 {
	v := 0.0
	for _, k := range keys {
		if it, ok := ItemByKey(k); ok {
			v += it.Value
		}
	}
	return v
}

// SumEffects totals the effects of the given items.
func SumEffects(keys []string) Effects {
	var e Effects
	for _, k := range keys {
		if it, ok := ItemByKey(k); ok {
			e.Add(it.Effects)
		}
	}
	return e
}
