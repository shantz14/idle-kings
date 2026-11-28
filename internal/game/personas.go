package game

// Persona is a famous figure who can sit in the Senate, with traits that
// suit them. Each court draws SenateSize of them. Senators are stored by
// name, so PersonaByName finds the bio for display.
type Persona struct {
	Name   string
	Bio    string
	Traits []Trait
}

var Personas = []Persona{
	// ---- history and legend ----
	{"Cato the Elder", "A stern old moralist who ends every speech the same way, and has never taken a coin he didn't earn.",
		[]Trait{TraitConservative, TraitIncorruptible}},
	{"Marcus Crassus", "The richest man in the city, and he would like you to know it. Everything he owns is for sale, at a price.",
		[]Trait{TraitGreedy, TraitMerchant}},
	{"Cicero", "The finest orator alive. He has unmasked conspiracies before, and would rather argue than fight.",
		[]Trait{TraitStern, TraitDove}},
	{"Julius Caesar", "A brilliant general with a habit of crossing rivers he was told not to cross. The die is cast.",
		[]Trait{TraitHawk, TraitReckless}},
	{"Marcus Brutus", "A defender of the old Republic, who will do whatever it takes to keep it.",
		[]Trait{TraitConservative, TraitStern}},
	{"Joan of Arc", "A farm girl who heard voices in her father's fields and led the common folk to war. She speaks for the villages, and for the land.",
		[]Trait{TraitPopulist, TraitLandowner}},
	{"Cincinnatus", "He left his plough to save the Republic once, then went straight back to his fields.",
		[]Trait{TraitLandowner, TraitIncorruptible}},
	{"Niccolò Machiavelli", "Believes it is better to be feared than loved, and that the ends justify the means.",
		[]Trait{TraitScheming, TraitRiskTaker}},
	{"Cesare Borgia", "Ambitious, ruthless and charming, with a poisoner on retainer.",
		[]Trait{TraitScheming, TraitReckless}},
	{"Cardinal Richelieu", "The red eminence: a churchman who serves the gods and the state, in whatever order suits him.",
		[]Trait{TraitPious, TraitScheming}},
	{"Midas", "A king whose every touch turns to gold. He would rather buy a peace than fight a war, and can always afford to.",
		[]Trait{TraitDove, TraitMerchant}},
	{"Lorenzo de' Medici", "A banker and patron of the arts, who thinks a city's glory is worth every coin.",
		[]Trait{TraitMerchant, TraitBigSpender}},
	{"Thomas Becket", "An archbishop who defied a king for the sake of the temples, and would do it again.",
		[]Trait{TraitPious, TraitIncorruptible}},
	{"Morgan le Fay", "An enchantress from a far-off isle, fond of strangers, secrets and dangerous gambles.",
		[]Trait{TraitCosmopolitan, TraitReckless}},

	// ---- modern fantasy ----
	{"Tywin Lannister", "The head of the richest house in the land, who always pays his debts and spends nothing he doesn't have to.",
		[]Trait{TraitHawk, TraitEconomical}},
	{"Petyr Baelish", "A master of coin who believes chaos is a ladder, and climbs it.",
		[]Trait{TraitScheming, TraitGreedy}},
	{"Lord Varys", "The spymaster who claims to serve the realm, and the people in it.",
		[]Trait{TraitCosmopolitan, TraitPopulist}},
	{"Olenna Tyrell", "The Queen of Thorns, matriarch of the realm's breadbasket, with a tongue sharper than any blade.",
		[]Trait{TraitLandowner, TraitScheming}},
	{"Eddard Stark", "An honorable lord from the cold north, who would rather lose than lie.",
		[]Trait{TraitIncorruptible, TraitStern}},
	{"Tyrion Lannister", "He drinks, and he knows things, and he is very fond of foreign wine.",
		[]Trait{TraitCosmopolitan, TraitBigSpender}},
	{"Stannis Baratheon", "Just, joyless and devout. The law is the law.",
		[]Trait{TraitStern, TraitPious}},
	{"Thorin Oakenshield", "A proud dwarf king who trusts no outsider, keeps the old ways, and will not share one coin of his mountain.",
		[]Trait{TraitIsolationist, TraitConservative}},
	{"Saruman", "A wizard turned industrialist with a voice that could talk a king into anything. He dreams of forges and furnaces, and works through other people's hands.",
		[]Trait{TraitScheming, TraitBigSpender}},

	// ---- Dungeons & Dragons ----
	{"Strahd von Zarovich", "A pale count from a misty valley who never leaves his castle by day, and lets nobody leave his lands.",
		[]Trait{TraitIsolationist, TraitScheming}},
	{"Elminster", "An ancient wizard with a pipe and a long beard, who has seen every kingdom rise and fall and counsels patience.",
		[]Trait{TraitCosmopolitan, TraitDove}},
	{"Lord Neverember", "The merchant lord of a city of jewels, forever spending public gold on public works with his name on them.",
		[]Trait{TraitMerchant, TraitBigSpender}},
}

func PersonaByName(name string) (Persona, bool) { return personaIndex.get(name) }
