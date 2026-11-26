package game

// Category groups actions in the menu.
type Category string

const (
	CatDiplomacy Category = "diplomacy"
	CatMonetary  Category = "monetary"
	CatIntrigue  Category = "intrigue"
	CatMartial   Category = "martial"
)

type CategoryInfo struct {
	Key  Category
	Name string
}

var Categories = []CategoryInfo{
	{CatDiplomacy, "Diplomacy"},
	{CatMonetary, "Monetary"},
	{CatIntrigue, "Intrigue"},
	{CatMartial, "Martial"},
}

// ActionDef is a menu entry. Public actions are announced to the court and
// may let other players join in.
type ActionDef struct {
	Key         string
	Name        string
	Category    Category
	Description string
	Public      bool
	Roles       []string // empty = every role
}

var Actions = []ActionDef{
	{
		Key: KindParty, Name: "Throw a Party", Category: CatDiplomacy, Public: true,
		Description: "Host the court at your estate for an evening. Lasts 2 hours; others may attend.",
	},
}

// ActionsFor lists the actions in a category that a role can take.
func ActionsFor(c Category, role string) []ActionDef {
	var out []ActionDef
	for _, a := range Actions {
		if a.Category == c && forRole(a.Roles, role) {
			out = append(out, a)
		}
	}
	return out
}
