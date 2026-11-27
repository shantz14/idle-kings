package game

import "fmt"

// KindWork is the activity kind for Endow a Public Work. The activity's goal
// is the work's key and it runs for the work's build time. It doesn't take
// up the character's action (see FreeKinds), but only one work can be under
// construction at a time, and each kind can only be built once.
const KindWork = "work"

// WorkDef is a public work a player can pay for. Once built it is a
// monument bearing the family's name, with a lasting Senate vote weight.
type WorkDef struct {
	Key         string
	Name        string
	Emoji       string
	Description string
	Cost        int
	Days        float64 // build time, in game days (Timings.Day)
	VoteWeight  float64
	Value       float64 // adds to estate value
	Realm       Realm   // applied once, when it opens
	Pleases     []Trait
}

// Placeholders to tune.
var WorkDefs = []WorkDef{
	{Key: "statue", Name: "Statue in the Forum", Emoji: "🗿", Cost: 150, Days: 1, VoteWeight: 0.5, Value: 2,
		Description: "Your ancestor in bronze, pointing nobly at something.",
		Realm:       Realm{AttrPopularity: 1}, Pleases: []Trait{TraitConservative}},
	{Key: "road", Name: "Paved Road", Emoji: "🛣️", Cost: 300, Days: 2, VoteWeight: 1, Value: 3,
		Description: "Good stone from the city gate to the market towns. Carts move, soldiers march.",
		Realm:       Realm{AttrStability: 2}, Pleases: []Trait{TraitEconomical, TraitBigSpender}},
	{Key: "temple", Name: "Temple", Emoji: "🏛️", Cost: 500, Days: 3, VoteWeight: 1.5, Value: 5,
		Description: "Marble columns and a bronze door, with your family's name over it for the gods to read.",
		Realm:       Realm{AttrStability: 2, AttrPopularity: 2}, Pleases: []Trait{TraitConservative}},
	{Key: "aqueduct", Name: "Aqueduct", Emoji: "🌉", Cost: 800, Days: 4, VoteWeight: 2.5, Value: 6,
		Description: "Clean water from the hills to every fountain in the city. Nobody forgets who paid for it.",
		Realm:       Realm{AttrPopularity: 5}, Pleases: []Trait{TraitPopulist}},
}

func WorkByKey(k string) (WorkDef, bool) {
	return workIndex.get(k)
}

// WorkTitle is the monument's name: "The Vane Aqueduct".
func WorkTitle(family string, w WorkDef) string {
	return fmt.Sprintf("The %s %s", family, w.Name)
}

// MonumentEffects sums the lasting effects of a character's finished works.
func MonumentEffects(keys []string) Effects {
	var e Effects
	for _, k := range keys {
		if w, ok := WorkByKey(k); ok {
			e.VoteWeight += w.VoteWeight
		}
	}
	return e
}

// MonumentValue is what a character's finished works add to estate value.
func MonumentValue(keys []string) float64 {
	v := 0.0
	for _, k := range keys {
		if w, ok := WorkByKey(k); ok {
			v += w.Value
		}
	}
	return v
}

// Placeholders to tune.
const (
	workOpeningRenown = 3
	workPleased       = 3.0
)

type WorkResult struct {
	Delta    Delta
	Realm    Realm
	Opinions []OpinionChange
	Text     string
	Public   string
}

// CompleteWork is what happens when a public work opens. Nothing is rolled:
// you paid, it got built.
func CompleteWork(w WorkDef, p Person, family string, senators []Senator) WorkResult {
	res := WorkResult{Delta: Delta{Renown: workOpeningRenown}, Realm: w.Realm.Clone()}
	res.Opinions = opinionsFrom(senators, p.ID, workPleased, w.Pleases...)

	title := WorkTitle(family, w)
	res.Text = withEffects(fmt.Sprintf("%s %s is finished. Your name is carved over it for all to see, and the Senate takes note: +%s Senate vote weight, for good.",
		w.Emoji, title, Num(w.VoteWeight)), res.Delta)
	res.Public = fmt.Sprintf("%s **%s** opens to the public, a gift from %s.", w.Emoji, title, p.Name)
	return res
}
