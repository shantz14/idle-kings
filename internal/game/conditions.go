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
}

const CondWounded = "wounded"

var ConditionDefs = []ConditionDef{
	{Key: CondWounded, Name: "Wounded", Emoji: "🩹", Description: "Still healing. Weaker with a blade and in command.",
		Effects: Effects{Stats: Stats{Prowess: -10, Martial: -5}}, Days: 3},
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
}

// Ends is when a condition given now ends.
func (d ConditionDef) Ends(now time.Time, day time.Duration) time.Time {
	return now.Add(time.Duration(d.Days * float64(day)))
}
