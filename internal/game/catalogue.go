package game

import (
	"hash/fnv"
	"math/rand/v2"
	"time"
)

// The shop's catalogue: everything you can buy. Starting estates are in
// estate.go (Shelf none). Appended to Items at init. Placeholder numbers:
// a new player earns about 10–15 gold a day, so the next house up is about
// a week away and a castle is a long-term goal.

func init() {
	for i := range shopItems {
		shopItems[i].Price = discount(shopItems[i].Price)
	}
	Items = append(Items, shopItems...)
}

// magic marks an enchanted or uncanny item (see the Witchcraft Act).
func magic(it Item) Item {
	it.Magic = true
	return it
}

// ShopPriceScale makes everything in the shop cheaper than the list prices
// below. Starting estates keep their prices (they only matter for selling).
const ShopPriceScale = 0.8

// discount applies ShopPriceScale, rounded to the nearest 5 gold.
func discount(price int) int {
	return max(5, int(float64(price)*ShopPriceScale/5+0.5)*5)
}

func prop(key, name string, tier Tier, value float64, price int, shelf Shelf, unique bool, fx Effects, desc string) Item {
	return Item{Key: key, Name: name, Kind: KindProperty, Tier: tier, Value: value, Price: price,
		Shelf: shelf, Unique: unique, Effects: fx, Description: desc}
}

func good(key, name string, kind ItemKind, value float64, price int, shelf Shelf, unique bool, fx Effects, desc string) Item {
	return Item{Key: key, Name: name, Kind: kind, Value: value, Price: price,
		Shelf: shelf, Unique: unique, Effects: fx, Description: desc}
}

func pro(n float64) Stats { return Stats{Prowess: n} }

var shopItems = []Item{
	// ---------- properties: always in stock ----------
	prop("forum_rooms", "Rooms near the Forum", TierHumble, 4, 60, ShelfFixed, false, Effects{},
		"Two clean rooms above a wine shop, a short walk from the Senate."),
	prop("modest_townhouse", "Modest Townhouse", TierModest, 9, 150, ShelfFixed, false, Effects{PartyBonus: 1},
		"A respectable house on a respectable street, with a small courtyard for guests."),
	prop("country_cottage", "Country Cottage", TierModest, 7, 200, ShelfFixed, false, Effects{DailyIncome: 2},
		"A stone cottage with a vegetable garden and a few goats that pay their way."),
	magic(prop("leaky_cauldron", "The Leaky Cauldron", TierFine, 18, 900, ShelfMarket, true, Effects{PartyBonus: 2},
		"A crooked old inn whose back wall opens onto a hidden street of wizards' shops.")),
	prop("winterfell", "Winterfell", TierCastle, 52, 5000, ShelfMarket, true, Effects{Stats: Stats{Martial: 5}, PartyBonus: 3},
		"A great grey castle in the cold north, built over hot springs. Winter is coming; its lord is ready."),

	// ---------- properties: the market ----------
	prop("prancing_pony", "The Prancing Pony", TierFine, 15, 600, ShelfMarket, true, Effects{DailyIncome: 2},
		"A busy inn at a crossroads. Rangers brood in the corner, and the rent comes in on time."),
	prop("bag_end", "Bag End", TierFine, 18, 800, ShelfMarket, true, Effects{PartyBonus: 2},
		"The finest hole under the hill: round doors, full pantries, and guests who never want to leave."),
	prop("arbor_vineyards", "The Vineyards of the Arbor", TierFine, 16, 900, ShelfMarket, true, Effects{DailyIncome: 8},
		"The sweetest red in the realm comes from these slopes, and it sells by the barrel."),
	prop("yawning_portal", "The Yawning Portal", TierFine, 15, 700, ShelfMarket, true, Effects{RumorNotice: 5},
		"A famous tavern built around a well that drops into a dungeon. Adventurers talk, and you listen."),
	// mid-tier: a goal a couple of weeks in (1,100–1,700 before the discount)
	prop("green_dragon", "The Green Dragon Inn", TierFine, 19, 1300, ShelfMarket, true, Effects{DailyIncome: 5, PartyBonus: 1},
		"The best beer in the Shire, a fire that never goes out, and farmers who drink until the rent is paid."),
	prop("inn_last_home", "The Inn of the Last Home", TierFine, 18, 1100, ShelfMarket, true, Effects{DailyIncome: 3, PartyBonus: 1},
		"An inn built high in the branches of a great vallenwood tree. Old friends meet here, and adventures begin."),
	magic(prop("the_burrow", "The Burrow", TierFine, 17, 1100, ShelfMarket, true, Effects{PartyBonus: 3},
		"Crooked, cluttered and held up mostly by magic. Nobody leaves hungry, and everyone wants to come back.")),
	prop("house_vettii", "The House of the Vettii", TierFine, 20, 1200, ShelfMarket, true, Effects{DailyIncome: 9},
		"Two freed slaves made a fortune in wine and built this house to prove it. The frescoes still smell of money."),
	prop("villa_mysteries", "The Villa of the Mysteries", TierFine, 20, 1400, ShelfMarket, true, Effects{PartyBonus: 2, RumorNotice: 4},
		"A red-walled villa where secret rites were painted on every wall. Guests whisper here, and you hear it."),
	prop("villa_livia", "The Villa of Livia", TierGrand, 23, 1500, ShelfMarket, true, Effects{PartyBonus: 2, VoteWeight: 1},
		"An empress's garden villa, painted floor to ceiling with birds and fruit trees. Senators' wives fight over invitations."),
	prop("waterdeep_manor", "A Manor in Waterdeep's Sea Ward", TierGrand, 24, 1600, ShelfMarket, true, Effects{VoteWeight: 1, PartyBonus: 1, DailyIncome: 3},
		"A walled mansion among the noble houses of the City of Splendors. The masked lords may be your neighbors."),
	prop("villa_jovis", "The Villa Jovis", TierGrand, 25, 1700, ShelfMarket, true, Effects{Stats: Stats{Intrigue: 5}, RumorNotice: 4},
		"An emperor's palace on a cliff above the sea, where he went to hide from Rome and plot against it."),
	prop("candlekeep", "Candlekeep", TierGrand, 24, 1700, ShelfMarket, true, Effects{Stats: Stats{Stewardship: 5}, RumorNotice: 3},
		"A fortress-library on a sea cliff. Visitors pay with a book it doesn't have, and the monks know every secret in them."),

	prop("villa_papyri", "The Villa of the Papyri", TierGrand, 28, 1800, ShelfMarket, true, Effects{PartyBonus: 3},
		"A seaside villa with a library of two thousand scrolls and a garden of bronze statues."),
	prop("hadrians_villa", "Hadrian's Villa", TierGrand, 30, 2200, ShelfMarket, true, Effects{PartyBonus: 3, DailyIncome: 3},
		"An emperor's retreat: pools, theaters and a private island inside a moat."),
	prop("phandalin_farms", "The Homesteads of Phandalin", TierGrand, 26, 2000, ShelfMarket, true, Effects{DailyIncome: 14},
		"A frontier town's worth of farms and orchards, now that someone has cleared out the goblins."),
	prop("ciceros_house", "Cicero's House on the Palatine", TierGrand, 27, 1900, ShelfMarket, true, Effects{VoteWeight: 2, PartyBonus: 1},
		"The great orator's home. Senators still drop by to argue in the atrium."),
	prop("knossos", "The Palace of Knossos", TierPalace, 42, 3800, ShelfMarket, true, Effects{PartyBonus: 5},
		"A maze of painted halls and grand staircases. Don't go down to the basement."),
	prop("hanging_gardens", "The Hanging Gardens", TierPalace, 45, 4200, ShelfMarket, true, Effects{PartyBonus: 4, DailyIncome: 5},
		"Terraces of trees and flowers climbing toward the sky, watered by a machine nobody understands."),
	prop("alhambra", "The Alhambra", TierPalace, 40, 3600, ShelfMarket, true, Effects{PartyBonus: 3, VoteWeight: 2},
		"A red fortress-palace of courtyards, fountains and carved ceilings. Foreign envoys beg to visit."),
	magic(prop("castle_ravenloft", "Castle Ravenloft", TierCastle, 55, 6000, ShelfMarket, true, Effects{Stats: Stats{Martial: 5, Prowess: 5}, DailyIncome: 6},
		"A gothic fortress above a valley of mist. The previous owner left in a hurry. Mostly.")),
	prop("camelot", "Camelot", TierCastle, 60, 8000, ShelfMarket, true, Effects{PartyBonus: 6, VoteWeight: 3},
		"A shining castle with a round table where nobody sits at the head. Owning it makes some senators nervous."),

	// ---------- estate goods: always in stock ----------
	good("painted_vase", "A Painted Vase", KindArt, 2, 40, ShelfFixed, false, Effects{},
		"Pretty, well made, and exactly like the one your neighbor has."),
	good("ancestor_bust", "Bronze Bust of an Ancestor", KindArt, 3, 90, ShelfFixed, false, Effects{PartyBonus: 1},
		"A stern bronze face for the entrance hall. Guests feel watched, in a good way."),
	good("magna_carta", "The Magna Carta", KindArtifact, 10, 1500, ShelfFixed, true, Effects{VoteWeight: 3},
		"The great charter that told a king he wasn't above the law. Senators lower their voices near it."),
	good("wine_cellar", "A Cellar of Fine Wine", KindPossession, 4, 250, ShelfFixed, false, Effects{PartyBonus: 1},
		"Racks of good red from the southern hills. Guests stay later, and talk more."),
	good("scroll_shelf", "A Shelf of Scrolls", KindPossession, 4, 450, ShelfFixed, false, Effects{Stats: Stats{Stewardship: 5}},
		"Histories, treatises on farming and a merchant's guide to every port. Dull, and very useful."),

	// ---------- armory: always in stock ----------
	good("hunting_spear", "A Hunting Spear", KindWeapon, 2, 150, ShelfFixed, false, Effects{Stats: pro(3)},
		"Ash shaft, iron head, a crossbar to stop the boar running up it."),
	good("gladius", "Legionary's Gladius", KindWeapon, 3, 300, ShelfFixed, false, Effects{Stats: pro(5)},
		"A short, well-balanced sword from the legion armories."),
	good("damascus_blade", "A Damascus Steel Blade", KindWeapon, 5, 650, ShelfFixed, false, Effects{Stats: Stats{Prowess: 7, Martial: 5}},
		"Watered steel from the eastern forges, patterned like smoke. It holds an edge through a whole campaign."),
	good("mail_shirt", "Mail Shirt", KindArmor, 3, 350, ShelfFixed, false, Effects{Stats: pro(5)},
		"Heavy, hot and worth every link when someone swings at you."),
	good("breastplate", "A Steel Breastplate", KindArmor, 5, 600, ShelfFixed, false, Effects{Stats: pro(6)},
		"Polished steel from the city forges, fitted to you by a smith who knows his trade."),
	good("destrier", "A Trained Destrier", KindEquipment, 5, 500, ShelfFixed, false, Effects{Stats: Stats{Prowess: 3, Martial: 5}},
		"A heavy warhorse that doesn't flinch at steel or shouting."),

	// ---------- the market: art ----------
	good("mona_lisa", "The Mona Lisa", KindArt, 8, 500, ShelfMarket, true, Effects{PartyBonus: 2},
		"She smiles at every guest, and no two of them agree on why."),
	good("venus_de_milo", "The Venus de Milo", KindArt, 12, 900, ShelfMarket, true, Effects{PartyBonus: 3},
		"A marble goddess, armless and flawless."),
	good("bayeux_tapestry", "The Bayeux Tapestry", KindArt, 10, 700, ShelfMarket, true, Effects{PartyBonus: 2},
		"Seventy meters of embroidered invasion. Guests read it from end to end."),
	good("nefertiti_bust", "The Bust of Nefertiti", KindArt, 7, 450, ShelfMarket, true, Effects{PartyBonus: 2},
		"A painted queen with a perfect profile, three thousand years old."),
	good("winged_victory", "The Winged Victory of Samothrace", KindArt, 14, 1200, ShelfMarket, true, Effects{PartyBonus: 3},
		"A headless goddess landing on a ship's prow, wings still spread."),
	magic(good("dragon_eggs", "Three Dragon Eggs", KindArt, 9, 600, ShelfMarket, true, Effects{PartyBonus: 2},
		"Scaled, heavy and warm to the touch. Probably just stone. Probably.")),
	magic(good("marauders_map", "The Marauder's Map", KindArtifact, 8, 550, ShelfMarket, true, Effects{RumorNotice: 8},
		"A map of the city that shows where everyone in it is, if you know the words.")),
	magic(good("lyre_orpheus", "The Lyre of Orpheus", KindArt, 6, 400, ShelfMarket, true, Effects{PartyBonus: 2},
		"It once charmed the lord of the dead. Your guests are easier.")),

	good("laocoon", "The Laocoön", KindArt, 13, 1000, ShelfMarket, true, Effects{PartyBonus: 3},
		"A priest and his sons wrestling sea serpents in marble. Guests can't look away."),
	good("lamassu", "The Lamassu of Nimrud", KindArt, 10, 800, ShelfMarket, true, Effects{PartyBonus: 2},
		"A winged bull with a king's bearded head, taller than two men. It guarded an Assyrian palace gate."),
	good("book_of_kells", "The Book of Kells", KindArt, 9, 700, ShelfMarket, true, Effects{PartyBonus: 2, Stats: Stats{Stewardship: 5}},
		"The gospels in gold and lapis, every page a maze of knots and beasts. Monks kept its accounts in the margins."),
	good("terracotta_squad", "A Squad of the Terracotta Army", KindArt, 14, 1200, ShelfMarket, true, Effects{PartyBonus: 3, Stats: Stats{Martial: 5}},
		"Twelve clay soldiers, no two faces alike, still standing in formation for a dead emperor."),
	magic(good("dorian_gray", "The Portrait of Dorian Gray", KindArt, 11, 900, ShelfMarket, true, Effects{PartyBonus: 3, Stats: Stats{Diplomacy: 5}},
		"A beautiful young man in oils. Keep it in the attic. Somehow you look younger every year.")),

	// ---------- the market: artifacts ----------
	good("iron_throne", "The Iron Throne", KindArtifact, 15, 1400, ShelfMarket, true, Effects{VoteWeight: 3},
		"A thousand swords melted into a chair. Uncomfortable, and everyone wants it."),
	magic(good("palantir", "A Palantír", KindArtifact, 10, 900, ShelfMarket, true, Effects{RumorNotice: 10},
		"A seeing-stone that shows faraway things. Be careful who's looking back.")),
	good("crown_charlemagne", "The Crown of Charlemagne", KindArtifact, 12, 1000, ShelfMarket, true, Effects{VoteWeight: 2},
		"An eight-sided crown of gold and gems, worn by an emperor of half the world."),
	magic(good("bag_of_holding", "A Bag of Holding", KindArtifact, 6, 500, ShelfMarket, true, Effects{DailyIncome: 6},
		"Bigger on the inside. Your factors use it to carry goods past every toll gate.")),
	good("antikythera", "The Antikythera Mechanism", KindArtifact, 8, 650, ShelfMarket, true, Effects{Stats: Stats{Stewardship: 5}},
		"Bronze gears that predict eclipses, and the best days to sign a contract."),
	magic(good("deck_of_many_things", "The Deck of Many Things", KindArtifact, 12, 1100, ShelfMarket, true, Effects{VoteWeight: 2, Stats: Stats{Diplomacy: 5}},
		"Draw a card and change your fate. You drew one already: a better seat in the Senate.")),
	good("holy_grail", "The Holy Grail", KindArtifact, 9, 700, ShelfMarket, true, Effects{PartyBonus: 3},
		"Knights searched for it all their lives. Pilgrims will queue at your door."),

	good("rosetta_stone", "The Rosetta Stone", KindArtifact, 11, 1300, ShelfMarket, true, Effects{Stats: Stats{Diplomacy: 5}, VoteWeight: 1},
		"One decree carved in three scripts. With it, no envoy's language is a mystery to you."),
	good("codex_justinian", "The Codex of Justinian", KindArtifact, 12, 1500, ShelfMarket, true, Effects{VoteWeight: 2, Stats: Stats{Stewardship: 5}},
		"Every law of the empire in one book. Quote it in the Senate and the room goes quiet."),
	magic(good("tome_leadership", "A Tome of Leadership and Influence", KindArtifact, 10, 1400, ShelfMarket, true, Effects{Stats: Stats{Diplomacy: 10}},
		"Read it cover to cover in a week and you'll never lose an argument again. The ink fades once it's done its work.")),
	magic(good("portable_hole", "A Portable Hole", KindArtifact, 9, 1400, ShelfMarket, true, Effects{DailyIncome: 8},
		"A black cloth circle that unfolds into a hole ten feet deep. The tax collectors never find what you keep in it.")),

	magic(good("one_ring", "The One Ring", KindArtifact, 15, 2000, ShelfMarket, true, Effects{Stats: Stats{Intrigue: 10}},
		"Plain gold, warm to the touch, and very hard to take off. Nobody notices you when you wear it.")),

	good("arkenstone", "The Arkenstone", KindArtifact, 16, 1500, ShelfMarket, true, Effects{PartyBonus: 4, VoteWeight: 1},
		"The Heart of the Mountain, a white jewel that drinks the light. Dwarf kings went to war over it."),
	magic(good("silmaril", "A Silmaril", KindArtifact, 17, 1600, ShelfMarket, true, Effects{PartyBonus: 3, VoteWeight: 2},
		"A jewel holding the light of two lost trees. Every oath sworn over it has ended badly.")),
	magic(good("philosophers_stone", "The Philosopher's Stone", KindArtifact, 12, 1600, ShelfMarket, true, Effects{DailyIncome: 10},
		"Blood-red and heavier than it looks. Lead goes into your cellar and gold comes out.")),
	good("golden_fleece", "The Golden Fleece", KindArtifact, 15, 1400, ShelfMarket, true, Effects{PartyBonus: 3, VoteWeight: 1},
		"Stolen from a dragon at the edge of the world by a crew of heroes. It proves the right to rule."),
	magic(good("pandoras_box", "Pandora's Box", KindArtifact, 9, 900, ShelfMarket, true, Effects{Stats: Stats{Intrigue: 5}, RumorNotice: 6},
		"Never open it. You've opened it a little. Now you hear every secret in the city.")),
	magic(good("aladdins_lamp", "Aladdin's Lamp", KindArtifact, 10, 1000, ShelfMarket, true, Effects{DailyIncome: 5, PartyBonus: 1},
		"A dented brass lamp. Rub it and something vast and amused grants small favors, mostly gold.")),

	// ---------- the market: war relics ----------
	good("leonidas_shield", "The Shield of Leonidas", KindRelic, 10, 900, ShelfMarket, true, Effects{Stats: Stats{Martial: 5}, VoteWeight: 1},
		"Come back with your shield, or on it. Three hundred stood behind this one."),
	good("horn_of_gondor", "The Horn of Gondor", KindRelic, 6, 450, ShelfMarket, true, Effects{Stats: Stats{Martial: 5}},
		"Cloven in two, and it still sounds when the realm needs help."),
	good("hannibal_tusk", "A Tusk from Hannibal's Elephants", KindRelic, 7, 500, ShelfMarket, true, Effects{Stats: Stats{Martial: 5}},
		"Carried over the mountains to the gates of Rome, and kept as a warning."),
	good("napoleons_bicorne", "Napoleon's Bicorne", KindRelic, 6, 450, ShelfMarket, true, Effects{RumorNotice: 5, Stats: Stats{Martial: 5}},
		"A short general's tall hat. Wearing it, you see the whole battlefield."),
	good("alexanders_helm", "The Helm of Alexander", KindArmor, 8, 700, ShelfMarket, true, Effects{Stats: Stats{Martial: 5, Prowess: 5}},
		"Worn by the king who conquered the known world before he was thirty."),

	good("eagle_ninth", "The Eagle of the Ninth Legion", KindRelic, 10, 900, ShelfMarket, true, Effects{Stats: Stats{Martial: 5}, VoteWeight: 1},
		"The lost standard of a legion that marched into the northern mists and never came back. Until now."),
	good("caesars_wreath", "Caesar's Laurel Wreath", KindRelic, 13, 1200, ShelfMarket, true, Effects{VoteWeight: 2, Stats: Stats{Diplomacy: 5}},
		"He wore it to every triumph, and to hide his thinning hair. Senators bow to it out of habit."),
	good("spirit_banner", "Genghis Khan's Spirit Banner", KindRelic, 13, 1300, ShelfMarket, true, Effects{Stats: Stats{Martial: 10}},
		"Horsehair tails on a spear. Where it went, the greatest empire the world has seen followed."),
	good("torc_vercingetorix", "The Torc of Vercingetorix", KindRelic, 7, 600, ShelfMarket, true, Effects{Stats: Stats{Martial: 5}, PartyBonus: 1},
		"A twisted gold neck ring from the Gaulish king who nearly beat Caesar."),

	// ---------- the market: weapons ----------
	magic(good("excalibur", "Excalibur", KindWeapon, 8, 1200, ShelfMarket, true, Effects{Stats: Stats{Prowess: 14, Martial: 5}},
		"Drawn from a stone by the rightful ruler, then thrown into a lake. Now it's in the shop.")),
	magic(good("mjolnir", "Mjölnir", KindWeapon, 9, 1500, ShelfMarket, true, Effects{Stats: Stats{Prowess: 15, Martial: 5}},
		"A short-handled hammer that always comes back. Only the worthy can lift it, and you can, mostly.")),
	good("longclaw", "Longclaw", KindWeapon, 7, 900, ShelfMarket, true, Effects{Stats: pro(10)},
		"A bastard sword of rippling dark steel with a wolf's-head pommel."),
	magic(good("vorpal_sword", "A Vorpal Sword", KindWeapon, 6, 700, ShelfMarket, true, Effects{Stats: pro(9)},
		"One, two! One, two! And through and through, the vorpal blade went snicker-snack.")),
	magic(good("poseidons_trident", "Poseidon's Trident", KindWeapon, 5, 600, ShelfMarket, true, Effects{Stats: pro(6), PartyBonus: 1},
		"Three points of sea-green bronze. Guests ask you to summon a wave; you never can.")),
	magic(good("sting", "Sting", KindWeapon, 8, 1100, ShelfMarket, true, Effects{Stats: Stats{Prowess: 9, Intrigue: 5}},
		"An elven short sword that glows blue when enemies are near. Very useful at parties.")),
	good("sword_of_damocles", "The Sword of Damocles", KindWeapon, 5, 500, ShelfMarket, true, Effects{Stats: pro(5)},
		"It hung by a single hair over a tyrant's head. Now it hangs over your mantel."),
	good("robin_hoods_bow", "Robin Hood's Longbow", KindWeapon, 6, 650, ShelfMarket, true, Effects{Stats: pro(6)},
		"It once split an arrow down the middle. It still prefers to rob the rich."),

	good("anduril", "Andúril, Flame of the West", KindWeapon, 11, 1400, ShelfMarket, true, Effects{Stats: Stats{Prowess: 13, Martial: 5}},
		"The sword that was broken, forged anew for the heir of kings. Armies of the dead answer to it."),
	good("glamdring", "Glamdring", KindWeapon, 10, 1200, ShelfMarket, true, Effects{Stats: Stats{Prowess: 11, Martial: 5}},
		"The Foe-hammer, an elven blade from a fallen city. Goblins know it, and run."),
	good("durendal", "Durendal", KindWeapon, 9, 1100, ShelfMarket, true, Effects{Stats: pro(11)},
		"Roland's sword. He tried to break it on a rock so the enemy wouldn't have it, and the rock broke."),
	good("ice", "Ice", KindWeapon, 8, 1000, ShelfMarket, true, Effects{Stats: pro(10)},
		"A greatsword of smoky Valyrian steel, as long as a man is tall. Its lords use it for executions themselves."),
	magic(good("gungnir", "Gungnir", KindWeapon, 11, 1400, ShelfMarket, true, Effects{Stats: pro(12)},
		"Odin's spear, carved with runes. Once thrown, it never misses.")),
	magic(good("blackrazor", "Blackrazor", KindWeapon, 10, 1300, ShelfMarket, true, Effects{Stats: Stats{Prowess: 12, Intrigue: 5}},
		"A black sword that sings softly and hungers for souls. It gives you ideas.")),
	magic(good("sword_of_kas", "The Sword of Kas", KindWeapon, 12, 1600, ShelfMarket, true, Effects{Stats: pro(16)},
		"Forged for a vampire lord's champion, who used it to betray him. It still wants a master to turn on.")),
	good("legolas_bow", "Legolas's Bow", KindWeapon, 7, 900, ShelfMarket, true, Effects{Stats: pro(8)},
		"A long bow of the Galadhrim. Its owner counts his kills out loud."),
	good("tells_crossbow", "William Tell's Crossbow", KindWeapon, 5, 600, ShelfMarket, true, Effects{Stats: pro(6)},
		"It shot an apple off a boy's head, and then a tyrant through the heart."),

	// ---------- the market: armor ----------
	good("spartacus_helm", "The Helm of Spartacus", KindArmor, 5, 450, ShelfMarket, true, Effects{Stats: pro(5)},
		"Worn by the gladiator who made Rome tremble."),
	good("joan_of_arc_armor", "Joan of Arc's Armor", KindArmor, 9, 1000, ShelfMarket, true, Effects{Stats: Stats{Prowess: 7, Martial: 5}},
		"Plain white plate, worn by a farm girl who led an army."),
	good("mithril_shirt", "A Mithril Shirt", KindArmor, 8, 900, ShelfMarket, true, Effects{Stats: pro(10)},
		"Light as a feather and hard as dragon scales. Worth more than the Shire."),
	magic(good("plate_plus_one", "Plate Armor +1", KindArmor, 12, 1100, ShelfMarket, true, Effects{Stats: pro(6), PartyBonus: 2},
		"Shiny, enchanted, and exactly one better than ordinary plate. Guests can't stop staring.")),

	good("achilles_shield", "The Shield of Achilles", KindArmor, 13, 1300, ShelfMarket, true, Effects{Stats: pro(11), PartyBonus: 1},
		"Forged by a god, with the whole world worked into its bronze: cities, weddings, harvests and war."),
	magic(good("aegis", "The Aegis", KindArmor, 12, 1200, ShelfMarket, true, Effects{Stats: Stats{Prowess: 9, Martial: 5}},
		"Athena's goatskin shield with a gorgon's face at its center. Enemies falter at the sight of it.")),
	magic(good("dragon_scale_mail", "Dragon Scale Mail", KindArmor, 10, 1100, ShelfMarket, true, Effects{Stats: pro(10)},
		"Made from a red dragon's hide. Fire slides off it, and so do swords.")),
	magic(good("elven_chain", "Elven Chain", KindArmor, 8, 800, ShelfMarket, true, Effects{Stats: Stats{Prowess: 7, Intrigue: 5}},
		"Mail so fine it folds into a pocket and makes no sound when you move.")),
	good("black_prince_armor", "The Black Prince's Armor", KindArmor, 9, 900, ShelfMarket, true, Effects{Stats: Stats{Prowess: 8, Martial: 5}},
		"Blackened plate from the prince who won his spurs at sixteen and never lost a battle."),

	// ---------- the market: equipment ----------
	good("direwolf_pup", "A Direwolf Pup", KindEquipment, 5, 500, ShelfMarket, true, Effects{Stats: pro(5)},
		"It will be the size of a horse within a year. Feed it well."),
	magic(good("hippogriff", "A Hippogriff", KindEquipment, 6, 550, ShelfMarket, true, Effects{Stats: pro(5), RumorNotice: 3},
		"Bow before it and it might let you ride. Nothing escapes its eye from the sky.")),
	good("bucephalus", "Bucephalus", KindEquipment, 8, 900, ShelfMarket, true, Effects{Stats: Stats{Martial: 5, Prowess: 6}},
		"The horse only Alexander could tame. He's yours now, if he agrees."),
	magic(good("flying_carpet", "A Flying Carpet", KindEquipment, 10, 1000, ShelfMarket, true, Effects{Stats: pro(5), PartyBonus: 2},
		"Woven in a far desert city. Guests remember the night you arrived by air.")),
	good("shadowfax", "Shadowfax", KindEquipment, 10, 1000, ShelfMarket, true, Effects{Stats: Stats{Prowess: 7, Martial: 5}},
		"The lord of all horses. He runs faster than the wind and chooses his own rider."),
	good("incitatus", "Incitatus", KindEquipment, 7, 700, ShelfMarket, true, Effects{Stats: pro(3), VoteWeight: 1},
		"The emperor's favorite horse, with a marble stable and an ivory manger. He was nearly made a consul."),
	magic(good("boots_of_speed", "Boots of Speed", KindEquipment, 6, 700, ShelfMarket, true, Effects{Stats: pro(6)},
		"Click the heels together and the world slows down around you.")),
	magic(good("cloak_elvenkind", "A Cloak of Elvenkind", KindEquipment, 6, 650, ShelfMarket, true, Effects{Stats: Stats{Prowess: 4, Intrigue: 5}},
		"Grey-green and light as a leaf. Wrapped in it, you vanish against any wall.")),
	magic(good("ogre_gauntlets", "Gauntlets of Ogre Power", KindEquipment, 7, 850, ShelfMarket, true, Effects{Stats: pro(8)},
		"Thick iron gloves that make you as strong as an ogre. Shaking hands takes practice.")),
}

// ---- the rotating market ----

// How many market pieces of each section are on sale each period.
const MarketPerSection = 2

// MarketPeriod is which rotation now falls in, and when it ends.
func MarketPeriod(now time.Time, rotation time.Duration) (int64, time.Time) {
	p := now.Unix() / int64(rotation.Seconds())
	return p, time.Unix((p+1)*int64(rotation.Seconds()), 0)
}

// Market is the rotating market's stock for a court in a period. It depends
// only on the court and the period, so buying a piece never reshuffles the
// day: owned pieces stay listed as sold.
func Market(guildID string, period int64) []Item {
	h := fnv.New64a()
	h.Write([]byte(guildID))
	r := rand.New(rand.NewPCG(h.Sum64(), uint64(period)))
	pools := map[Section][]Item{}
	for _, it := range Items {
		if it.Shelf == ShelfMarket {
			pools[it.Section()] = append(pools[it.Section()], it)
		}
	}
	var out []Item
	for _, s := range Sections {
		pool := pools[s.Key]
		r.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		out = append(out, pool[:min(MarketPerSection, len(pool))]...)
	}
	return out
}

// FixedShelf is everything always in stock.
func FixedShelf() []Item {
	var out []Item
	for _, it := range Items {
		if it.Shelf == ShelfFixed {
			out = append(out, it)
		}
	}
	return out
}

// InStock reports whether the shop sells key right now (ownership aside).
func InStock(key, guildID string, period int64) bool {
	it, ok := ItemByKey(key)
	if !ok {
		return false
	}
	switch it.Shelf {
	case ShelfFixed:
		return true
	case ShelfMarket:
		for _, m := range Market(guildID, period) {
			if m.Key == key {
				return true
			}
		}
	}
	return false
}
