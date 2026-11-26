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
		Key: KindDuel, Name: "Challenge to a Duel", Category: CatMartial, Public: true,
		Description: "Call out another player. If they accept, you fight 30 minutes later, and the court can bet on it. Renown, blood and gold are at stake.",
	},
	{
		Key: KindHunt, Name: "Hunt in the Royal Forest", Category: CatMartial, Public: true,
		Description: fmt.Sprintf("Ride out with hounds and horses. Takes 2 hours and costs %d gold; others may join.", HuntCost),
	},
	{
		Key: KindJourney, Name: "Undertake a Journey", Category: CatMartial, Public: true,
		Description: "Travel the provinces or go abroad for gold, influence, renown or learning. Takes 3 hours, 6 abroad. The court will know you're on the road, and the roads are not safe.",
	},
	{
		Key: KindParty, Name: "Throw a Party", Category: CatDiplomacy, Public: true,
		Description: "Host the court at your estate for an evening. Takes 2 hours; others may attend.",
	},
	{
		Key: KindSpeech, Name: "Give a Speech", Category: CatDiplomacy, Public: true,
		Description: "Speak in the Forum for a cause. Takes an hour; others may come to cheer, or to heckle.",
	},
	{
		Key: KindCanvass, Name: "Canvass the Districts", Category: CatDiplomacy,
		Description: "Shake hands across the city. Takes 3 hours. Free, and always worth a little influence.",
	},
	{
		Key: KindPetition, Name: "Petition the Senate", Category: CatDiplomacy, Public: true,
		Description: fmt.Sprintf("Lobby to put a matter before the Senate: pass a lasting law, or repeal one. Takes 2 hours and costs %d gold. It always reaches the floor, as an extra vote alongside the day's matter. Your name goes on it, win or lose.", PetitionCost),
	},
	{
		Key: KindCourt, Name: "Court a Senator", Category: CatDiplomacy,
		Description: "Call on a senator and try to win them over. Takes an hour; their opinion of you may rise, or fall.",
	},
	{
		Key: KindCircus, Name: "Bread and Circuses", Category: CatMonetary, Public: true,
		Description: "Feed and entertain the city at your own expense. Takes 2 hours. The people's mood rises, and so does your influence.",
	},
	{
		Key: KindWork, Name: "Endow a Public Work", Category: CatMonetary, Public: true,
		Description: "Pay for a statue, road, temple or aqueduct bearing your name. Takes days to build, but its Senate vote weight is yours for good.",
	},
	{
		Key: KindInvest, Name: "Invest in a Venture", Category: CatMonetary,
		Description: "Buy into one of the realm's industries for a share of its daily earnings. Takes 3 hours to strike the deal.",
	},
	{
		Key: KindPlantSpies, Name: "Plant Spies", Category: CatIntrigue,
		Description: "Slip an agent into another player's household, and hear what they do in private. Takes an hour. Your network builds up over time; a clumsy agent may be caught.",
	},
	{
		Key: KindRootOut, Name: "Root Out Spies", Category: CatIntrigue,
		Description: "Search your own household for other players' agents. Takes 2 hours.",
	},
	{
		Key: KindDossier, Name: "Your Dossier", Category: CatIntrigue,
		Description: "Everything your spies have caught your rivals doing. Denounce them whenever you like; it doesn't take your action.",
	},
	{
		Key: KindEmbezzle, Name: "Embezzle from the Treasury", Category: CatIntrigue,
		Description: "Quietly move the Republic's silver into your own purse. Takes 2 hours. If you're caught, everyone will know.",
	},
	{
		Key: KindAid, Name: "Aid the Realm", Category: CatMonetary,
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
