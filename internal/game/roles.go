package game

// Role is a starting position at court. It adds a small stat bonus, sets
// court standing (influence) and salary, and gates role-only content
// (ActionDef.Roles, PartyEvent.Roles, MatterOutcome.Roles).
type Role struct {
	Key         string
	Name        string
	Description string
	StatBonus   Stats
	Influence   float64
	Salary      int // gold per day
}

// Roles is the list offered in the character creator. Placeholder numbers.
var Roles = []Role{
	{Key: "cupbearer", Name: "Cupbearer", Description: "Pours the wine and hears everything said over it.",
		StatBonus: Stats{Intrigue: 5, Diplomacy: 5}, Influence: 8, Salary: 8},
	{Key: "officer", Name: "Officer", Description: "A junior commander with soldiers who answer to them.",
		StatBonus: Stats{Martial: 10}, Influence: 6, Salary: 10},
	{Key: "merchant", Name: "Merchant", Description: "Trades on behalf of the court, and on their own behalf too.",
		StatBonus: Stats{Stewardship: 10}, Influence: 4, Salary: 12},
	{Key: "emissary", Name: "Foreign Emissary", Description: "Speaks for the Republic to foreign powers.",
		StatBonus: Stats{Diplomacy: 10}, Influence: 7, Salary: 9},
	{Key: "scribe", Name: "Scribe", Description: "Keeps the records, and knows which ones went missing.",
		StatBonus: Stats{Stewardship: 5, Intrigue: 5}, Influence: 6, Salary: 8},
}

// RoleByKey looks up a role.
func RoleByKey(key string) (Role, bool) {
	for _, r := range Roles {
		if r.Key == key {
			return r, true
		}
	}
	return Role{}, false
}
