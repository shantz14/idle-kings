package game

import "fmt"

// Category groups actions in the menu.
type Category string

const (
	CatDiplomacy Category = "diplomacy"
	CatMonetary  Category = "monetary"
	CatIntrigue  Category = "intrigue"
	CatMartial   Category = "martial"
)

type CategoryInfo struct {
	Key   Category
	Name  string
	Emoji string
}

var Categories = []CategoryInfo{
	{CatDiplomacy, "Diplomacy", "🤝"},
	{CatMonetary, "Monetary", "💰"},
	{CatIntrigue, "Intrigue", "🕵️"},
	{CatMartial, "Martial", "⚔️"},
}

// CategoryByKey finds a menu category.
func CategoryByKey(key Category) (CategoryInfo, bool) {
	for _, c := range Categories {
		if c.Key == key {
			return c, true
		}
	}
	return CategoryInfo{}, false
}

// ActionDef is a menu entry. Public actions are announced to the court and
// may let other players join in.
type ActionDef struct {
	Key         string
	Name        string
	Emoji       string
	Category    Category
	Description string
	Public      bool
	Roles       []string // empty = every role
}

var Actions = []ActionDef{
	{
		Key: KindDuel, Name: "Challenge to a Duel", Emoji: "⚔️", Category: CatMartial, Public: true,
		Description: "Call out another player. If they accept, you fight 30 minutes later, and the court can bet on it. Renown, blood and gold are at stake.",
	},
	{
		Key: KindHunt, Name: "Hunt in the Royal Forest", Emoji: "🦌", Category: CatMartial, Public: true,
		Description: fmt.Sprintf("Ride out with hounds and horses. Takes 2 hours and costs %d gold; others may join.", HuntCost),
	},
	{
		Key: KindJourney, Name: "Undertake a Journey", Emoji: "🧭", Category: CatMartial, Public: true,
		Description: "Travel the provinces or go abroad for gold, influence, renown or learning. Takes 3 hours, 6 abroad. The court will know you're on the road, and the roads are not safe.",
	},
	{
		Key: KindParty, Name: "Throw a Party", Emoji: "🎭", Category: CatDiplomacy, Public: true,
		Description: "Host the court at your estate for an evening. Takes 2 hours; others may attend.",
	},
	{
		Key: KindSpeech, Name: "Give a Speech", Emoji: "📢", Category: CatDiplomacy, Public: true,
		Description: "Speak in the Forum for a cause. Takes an hour; others may come to cheer, or to heckle.",
	},
	{
		Key: KindCanvass, Name: "Canvass the Districts", Emoji: "🗺️", Category: CatDiplomacy,
		Description: "Shake hands across the city. Takes 3 hours. Free, and always worth a little influence.",
	},
	{
		Key: KindPetition, Name: "Petition the Senate", Emoji: "📜", Category: CatDiplomacy, Public: true,
		Description: fmt.Sprintf("Lobby to put a lasting law, or its repeal, before the Senate. Takes 2 hours and costs %d gold. It always reaches the floor, and your name goes on it, win or lose.", PetitionCost),
	},
	{
		Key: KindCourt, Name: "Speak to a Senator", Emoji: "🎩", Category: CatDiplomacy,
		Description: "Call on a senator: ask how they'll vote, bribe them, sell them your vote, or spend an hour courting them. Their opinion of you may rise, or fall.",
	},
	{
		Key: KindCircus, Name: "Bread and Circuses", Emoji: "🎪", Category: CatMonetary, Public: true,
		Description: "Feed and entertain the city at your own expense. Takes 2 hours. The people's mood rises, and so does your influence.",
	},
	{
		Key: KindWork, Name: "Endow a Public Work", Emoji: "🏛️", Category: CatMonetary, Public: true,
		Description: "Pay for a statue, road, temple or aqueduct bearing your name. Takes days to build, but its Senate vote weight is yours for good.",
	},
	{
		Key: KindInvest, Name: "Invest in a Venture", Emoji: "📈", Category: CatMonetary,
		Description: "Buy into one of the realm's industries for a share of its daily earnings. Takes 3 hours to strike the deal.",
	},
	{
		Key: KindPlantSpies, Name: "Plant Spies", Emoji: "🕵️", Category: CatIntrigue,
		Description: "Slip an agent into another player's household, and hear what they do in private. Takes an hour. Your network builds up over time; a clumsy agent may be caught.",
	},
	{
		Key: KindRootOut, Name: "Root Out Spies", Emoji: "🔍", Category: CatIntrigue,
		Description: "Search your own household for other players' agents. Takes 2 hours.",
	},
	{
		Key: KindDossier, Name: "Your Dossier", Emoji: "📁", Category: CatIntrigue,
		Description: "Everything your spies have caught your rivals doing. Denounce them whenever you like; it doesn't take your action.",
	},
	{
		Key: KindEmbezzle, Name: "Embezzle from the Treasury", Emoji: "🪙", Category: CatIntrigue,
		Description: "Quietly move the Republic's silver into your own purse. Takes 2 hours. If you're caught, everyone will know.",
	},
	{
		Key: KindAid, Name: "Aid the Realm", Emoji: "🤲", Category: CatMonetary,
		Description: "Pay from your own purse for the treasury, the army or the city watch. Instant. The court honors those who give when the realm is in need.",
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

// ActionByKey finds a menu action by its kind.
func ActionByKey(key string) (ActionDef, bool) {
	for _, a := range Actions {
		if a.Key == key {
			return a, true
		}
	}
	return ActionDef{}, false
}
