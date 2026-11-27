package game

import "time"

// A condition is a temporary state on a character, like a wound. Its
// effects add to estate item effects while it lasts.
type ConditionDef struct {
	Key         string
	Name        string
	Emoji       string
	Description string
	Effects     Effects
	Days        float64 // how long it lasts, in game days (see Timings.Day)
	// InvestBonus helps the next investment's deal roll, and is used up by it.
	InvestBonus float64
}

const (
	CondWounded        = "wounded"
	CondGoodWord       = "good_word"       // talked business as a host
	CondGoodWordLesser = "good_word_guest" // talked business as a guest
)

var ConditionDefs = []ConditionDef{
	{Key: CondWounded, Name: "Wounded", Emoji: "🩹", Description: "Still healing. Weaker with a blade and in command.",
		Effects: Effects{Stats: Stats{Prowess: -10, Martial: -5}}, Days: 3},
	{Key: CondGoodWord, Name: "A Good Word", Emoji: "🤝", Description: "The merchants you charmed will put in a word for you: your next investment's deal is easier.",
		Days: 3, InvestBonus: 20},
	{Key: CondGoodWordLesser, Name: "A Friendly Word", Emoji: "🤝", Description: "Someone you met will put in a word for you: your next investment's deal is a little easier.",
		Days: 3, InvestBonus: 10},
}

// InvestBonus is the best deal bonus among a character's conditions, and the
// condition that gives it ("" for none). Investing uses it up.
func InvestBonus(cs []Condition) (float64, string) {
	best, key := 0.0, ""
	for _, c := range cs {
		if d, ok := ConditionByKey(c.Key); ok && d.InvestBonus > best {
			best, key = d.InvestBonus, c.Key
		}
	}
	return best, key
}

func ConditionByKey(k string) (ConditionDef, bool) {
	return conditionIndex.get(k)
}

// Condition is one a character has, until EndsAt.
type Condition struct {
	Key    string
	EndsAt time.Time
}

// ConditionEffects sums the effects of the given conditions.
func ConditionEffects(cs []Condition) Effects {
	var e Effects
	for _, c := range cs {
		if d, ok := ConditionByKey(c.Key); ok {
			e.Add(d.Effects)
		}
	}
	return e
}

// ConditionChange gives a character a condition (or extends it).
type ConditionChange struct {
	CharacterID int64
	Key         string
	Days        float64 // how long, in game days; 0 = the condition's own length
}

// Ends is when a condition given now ends.
func (d ConditionDef) Ends(now time.Time, day time.Duration) time.Time {
	return now.Add(time.Duration(d.Days * float64(day)))
}
