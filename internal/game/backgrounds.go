package game

// Background is a character's backstory. It adds stat points and decides
// their starting renown, gold and estate.
type Background struct {
	Key       string
	Name      string
	Summary   string // one line, for menus (max 100 chars)
	Story     string
	StatBonus Stats
	Renown    float64
	Wealth    int
	Home      string   // item key
	Items     []string // item keys
}

// Backgrounds is the list offered in the character creator. Placeholder numbers.
var Backgrounds = []Background{
	{
		Key: "fallen_nobility", Name: "Fallen Nobility",
		Summary:   "An old, proud name and an empty purse.",
		Story:     "Your family once sat at the head of the Senate. Then came bad marriages, worse investments, and one very unwise rebellion. The name still opens doors. The money is long gone.",
		StatBonus: Stats{Diplomacy: 10, Intrigue: 5}, Renown: 60, Wealth: 20,
		Home: "crumbling_manor", Items: []string{"signet_ring"},
	},
	{
		Key: "soldiers_family", Name: "Soldier's Family",
		Summary:   "Three generations in the legions.",
		Story:     "Your grandfather held the bridge at Corran. Your father marched with the Ninth. You grew up on drill yards and war stories, and you know which end of a sword to hold.",
		StatBonus: Stats{Martial: 10, Prowess: 5}, Renown: 30, Wealth: 35,
		Home: "smallholding", Items: []string{"fathers_sword"},
	},
	{
		Key: "merchant_house", Name: "Merchant House",
		Summary:   "New money, and plenty of it.",
		Story:     "Your family made its fortune in wool and salt. The old houses sneer at your accent, then quietly ask for loans.",
		StatBonus: Stats{Stewardship: 10, Diplomacy: 5}, Renown: 20, Wealth: 100,
		Home: "townhouse", Items: []string{"ledger"},
	},
	{
		Key: "temple_ward", Name: "Temple Ward",
		Summary:   "Raised by priests, who taught you more than prayers.",
		Story:     "Left on the temple steps as a baby, you were raised among priests and pilgrims. You learned to read, to keep accounts, and to listen at doors.",
		StatBonus: Stats{Diplomacy: 5, Stewardship: 5, Intrigue: 5}, Renown: 20, Wealth: 25,
		Home: "temple_cell", Items: []string{"relic"},
	},
	{
		Key: "gutter_born", Name: "Gutter-born",
		Summary:   "You clawed your way up from the lower city.",
		Story:     "You grew up in the tenements by the river, where you learned to fight, to lie, and to run. Somehow you ended up at court. You still have friends in low places.",
		StatBonus: Stats{Intrigue: 10, Prowess: 5}, Renown: 10, Wealth: 15,
		Home: "rented_room", Items: []string{"informants"},
	},
	{
		Key: "foreign_exile", Name: "Foreign Exile",
		Summary:   "A noble of another land, far from home.",
		Story:     "A coup at home sent your family fleeing with what it could carry. Here you are an outsider, but a well-connected one, and your old court still writes to you.",
		StatBonus: Stats{Diplomacy: 10, Intrigue: 5}, Renown: 40, Wealth: 50,
		Home: "hill_villa", Items: []string{"letters", "tapestry"},
	},
	{
		Key: "country_squire", Name: "Country Squire",
		Summary:   "Landed gentry from the hill country.",
		Story:     "Your family has farmed the same hills for two hundred years. You came to the capital with mud on your boots and a good head for numbers.",
		StatBonus: Stats{Stewardship: 10, Martial: 5}, Renown: 30, Wealth: 45,
		Home: "hill_farm", Items: []string{"warhorse"},
	},
	{
		Key: "freed_gladiator", Name: "Freed Gladiator",
		Summary:   "You fought your way to freedom in the arena.",
		Story:     "Sold to the arena as a child, you fought for ten years and won your freedom before a roaring crowd. The crowd hasn't forgotten you. The great houses wish they could.",
		StatBonus: Stats{Prowess: 10, Martial: 5}, Renown: 40, Wealth: 30,
		Home: "arena_shack", Items: []string{"rudis"},
	},
}

func BackgroundByKey(key string) (Background, bool) {
	for _, b := range Backgrounds {
		if b.Key == key {
			return b, true
		}
	}
	return Background{}, false
}

// EstateItems is the background's starting home followed by its possessions.
func (b Background) EstateItems() []string {
	return append([]string{b.Home}, b.Items...)
}
