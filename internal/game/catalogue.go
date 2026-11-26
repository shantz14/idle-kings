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
	prop("two_fountains", "House of the Two Fountains", TierFine, 18, 900, ShelfMarket, true, Effects{PartyBonus: 2},
		"A famous old house whose twin fountains have watered three generations of gossip."),
	prop("hillfort_castle", "Hillfort Castle", TierCastle, 52, 5000, ShelfMarket, true, Effects{Stats: Stats{Martial: 1}, PartyBonus: 3},
		"A grim stone fortress on the northern border, with a garrison that salutes whoever owns it."),

	// ---------- properties: the market ----------
	prop("merchant_courtyard", "Merchant's Courtyard House", TierFine, 15, 600, ShelfMarket, true, Effects{DailyIncome: 2},
		"Shops on the street side, a quiet courtyard on the other, and rent from both."),
	prop("riverside_villa", "Riverside Villa", TierFine, 18, 800, ShelfMarket, true, Effects{PartyBonus: 2},
		"Terraced gardens running down to the river, where guests arrive by boat."),
	prop("vineyard_estate", "Vineyard Estate", TierFine, 16, 900, ShelfMarket, true, Effects{DailyIncome: 8},
		"Rolling vines and a press house. The wine is good, and it sells."),
	prop("harbor_house", "The Harbor Master's House", TierFine, 15, 700, ShelfMarket, true, Effects{RumorNotice: 5},
		"It overlooks every ship that comes and goes, and every sailor who talks too much."),
	prop("domus_hill", "Domus on the Hill", TierGrand, 28, 1800, ShelfMarket, true, Effects{PartyBonus: 3},
		"A grand townhouse among the oldest families, with a dining hall for forty."),
	prop("seaside_villa", "Seaside Villa", TierGrand, 30, 2200, ShelfMarket, true, Effects{PartyBonus: 3, DailyIncome: 3},
		"White walls, a private beach and fishponds that feed half the coast."),
	prop("olive_estate", "The Great Olive Estate", TierGrand, 26, 2000, ShelfMarket, true, Effects{DailyIncome: 14},
		"Ten thousand olive trees and the presses to go with them."),
	prop("senators_townhouse", "The Old Senator's Townhouse", TierGrand, 27, 1900, ShelfMarket, true, Effects{VoteWeight: 2, PartyBonus: 1},
		"Its last three owners all became consuls. Senators still drop by out of habit."),
	prop("marble_palace", "The Marble Palace", TierPalace, 42, 3800, ShelfMarket, true, Effects{PartyBonus: 5},
		"Every wall is marble, every floor is mosaic, and every guest is impressed."),
	prop("lakeside_palace", "The Lakeside Palace", TierPalace, 45, 4200, ShelfMarket, true, Effects{PartyBonus: 4, DailyIncome: 5},
		"A summer palace on its own lake, with an island pavilion for the most private dinners."),
	prop("keshi_pavilion", "The Keshi Pavilion", TierPalace, 40, 3600, ShelfMarket, true, Effects{PartyBonus: 3, VoteWeight: 2},
		"Built by a Keshi prince in his homeland's style. Foreign envoys always ask to visit."),
	prop("castle_greywatch", "Castle Greywatch", TierCastle, 55, 6000, ShelfMarket, true, Effects{Stats: Stats{Martial: 1, Prowess: 1}, DailyIncome: 6},
		"A castle with a forest, three villages and a tournament field."),
	prop("old_royal_castle", "The Old Royal Castle", TierCastle, 60, 8000, ShelfMarket, true, Effects{PartyBonus: 6, VoteWeight: 3},
		"The seat of the kings before the Republic. Owning it makes some senators nervous."),

	// ---------- possessions: always in stock ----------
	good("painted_vase", "A Painted Vase", KindArt, 2, 40, ShelfFixed, false, Effects{},
		"Pretty, well made, and exactly like the one your neighbor has."),
	good("ancestor_bust", "Bronze Bust of an Ancestor", KindArt, 3, 90, ShelfFixed, false, Effects{PartyBonus: 1},
		"A stern bronze face for the entrance hall. Guests feel watched, in a good way."),
	good("gladius", "Legionary's Gladius", KindWeapon, 3, 300, ShelfFixed, false, Effects{Stats: pro(1)},
		"A short, well-balanced sword from the legion armories."),
	good("mail_shirt", "Mail Shirt", KindArmor, 3, 350, ShelfFixed, false, Effects{Stats: pro(1)},
		"Heavy, hot and worth every link when someone swings at you."),
	good("sword_of_varrow", "The Sword of Varrow", KindWeapon, 8, 1200, ShelfFixed, true, Effects{Stats: Stats{Prowess: 2, Martial: 1}},
		"Carried by the general who held Varrow for a year. The blade has never been sharpened, and never needed it."),
	good("first_seal", "The First Seal of the Senate", KindArtifact, 10, 1500, ShelfFixed, true, Effects{VoteWeight: 3},
		"The seal that stamped the Republic's first law. Senators lower their voices near it."),

	// ---------- the market: art ----------
	good("founder_portrait", "Portrait of the Founding Consul", KindArt, 8, 500, ShelfMarket, true, Effects{PartyBonus: 2},
		"The only portrait painted from life. His eyes follow you around the room."),
	good("marble_nymph", "The Weeping Marble Nymph", KindArt, 12, 900, ShelfMarket, true, Effects{PartyBonus: 3},
		"A statue so lifelike that guests swear they've seen her tears move."),
	good("sea_mosaic", "Mosaic of the Sea Gods", KindArt, 10, 700, ShelfMarket, true, Effects{PartyBonus: 2},
		"A floor of a hundred thousand tiles: gods, sea monsters and one very surprised fisherman."),
	good("silk_screens", "Keshi Silk Screens", KindArt, 7, 450, ShelfMarket, true, Effects{PartyBonus: 2},
		"Painted silk panels of cranes and mountains, shipped from Kesh at ruinous cost."),
	good("bronze_charioteer", "The Bronze Charioteer", KindArt, 14, 1200, ShelfMarket, true, Effects{PartyBonus: 3},
		"Life-size, mid-race, reins in hand. Pulled from a shipwreck a century ago."),
	good("palace_frescoes", "Frescoes from the Old Palace", KindArt, 9, 600, ShelfMarket, true, Effects{PartyBonus: 2},
		"Cut from the walls of the kings' palace before it was pulled down."),
	good("world_map", "A Painted Map of the Known World", KindArt, 8, 550, ShelfMarket, true, Effects{VoteWeight: 1},
		"Every road, river and rumored kingdom. Senators come to argue over it."),
	good("lyre_ithren", "The Lyre of Ithren", KindArt, 6, 400, ShelfMarket, true, Effects{PartyBonus: 2},
		"The great poet's own lyre. A musician who plays it at your parties is never short of an audience."),

	// ---------- the market: artifacts ----------
	good("ivory_scepter", "The Ivory Scepter of the Last King", KindArtifact, 15, 1400, ShelfMarket, true, Effects{VoteWeight: 3},
		"He was holding it when the Senate deposed him. It has been passed around the great families ever since."),
	good("oracle_bowl", "The Oracle's Bowl", KindArtifact, 10, 900, ShelfMarket, true, Effects{RumorNotice: 10},
		"Fill it with water, and secrets swim to the surface. So people say."),
	good("tribal_crown", "The Crown of the Hill Tribes", KindArtifact, 12, 1000, ShelfMarket, true, Effects{VoteWeight: 2},
		"Taken in the first barbarian war. The tribes still want it back."),
	good("orsini_ledgers", "The Lost Ledgers of House Orsini", KindArtifact, 6, 500, ShelfMarket, true, Effects{DailyIncome: 6},
		"Debts nobody remembered they owed, until now."),
	good("keshi_astrolabe", "The Astrolabe of Kesh", KindArtifact, 8, 650, ShelfMarket, true, Effects{Stats: Stats{Stewardship: 1}},
		"Brass rings that chart the stars, and the best days to sign a contract."),
	good("first_law", "Tablets of the First Law", KindArtifact, 12, 1100, ShelfMarket, true, Effects{VoteWeight: 2, Stats: Stats{Diplomacy: 1}},
		"Bronze tablets with the Republic's founding laws. Quoting them wins arguments."),
	good("reliquary", "A Saint's Reliquary", KindArtifact, 9, 700, ShelfMarket, true, Effects{PartyBonus: 3},
		"Gold, garnets and a saint's knucklebone. Pilgrims will queue at your door."),

	// ---------- the market: war relics ----------
	good("third_legion_standard", "Standard of the Third Legion", KindRelic, 10, 900, ShelfMarket, true, Effects{Stats: Stats{Martial: 1}, VoteWeight: 1},
		"The eagle the Third carried at the Battle of the Fords. Veterans salute it."),
	good("war_horn", "A Barbarian War-horn", KindRelic, 6, 450, ShelfMarket, true, Effects{Stats: Stats{Martial: 1}},
		"Taken from a dead chieftain. One blast still empties a room."),
	good("shield_of_varrow", "The Broken Shield of Varrow", KindRelic, 7, 500, ShelfMarket, true, Effects{Stats: Stats{Martial: 1}},
		"Split down the middle by an axe, and carried home anyway."),
	good("admirals_spyglass", "The Admiral's Spyglass", KindRelic, 6, 450, ShelfMarket, true, Effects{RumorNotice: 5, Stats: Stats{Martial: 1}},
		"It spotted the pirate fleet in time. Now it watches your neighbors."),
	good("generals_helm", "Helm of the Unknown General", KindRelic, 8, 700, ShelfMarket, true, Effects{Stats: Stats{Martial: 1, Prowess: 1}},
		"Found on a battlefield with no body beneath it. Whoever he was, he won."),

	// ---------- the market: weapons ----------
	good("keshi_sabre", "Keshi Steel Sabre", KindWeapon, 7, 900, ShelfMarket, true, Effects{Stats: pro(2)},
		"Folded steel with a blue sheen. It cuts silk floating in the air."),
	good("champions_spear", "The Champion's Spear", KindWeapon, 6, 700, ShelfMarket, true, Effects{Stats: pro(2)},
		"Carried by twelve champions of the Grand Games. None of them lost while holding it."),
	good("trident_net", "A Gladiator's Trident and Net", KindWeapon, 5, 600, ShelfMarket, true, Effects{Stats: pro(1), PartyBonus: 1},
		"Strange to fight with and stranger to fight against. Guests love the stories."),
	good("venn_rapier", "The Rapier of Mira Venn", KindWeapon, 8, 1100, ShelfMarket, true, Effects{Stats: Stats{Prowess: 2, Intrigue: 1}},
		"The duelist-spy Mira Venn won forty duels and lost no secrets. This was her blade."),
	good("headsmans_axe", "The Headsman's Axe", KindWeapon, 5, 500, ShelfMarket, true, Effects{Stats: pro(1)},
		"Retired from public service. Heavy, grim, and very effective."),
	good("huntmasters_bow", "The Huntmaster's Bow", KindWeapon, 6, 650, ShelfMarket, true, Effects{Stats: pro(1)},
		"Horn and yew, strung for a giant. The royal huntmasters used nothing else."),

	// ---------- the market: armor ----------
	good("gladiator_helm", "Gladiator's Helm", KindArmor, 5, 450, ShelfMarket, true, Effects{Stats: pro(1)},
		"A visored bronze helm, dented by a hundred bouts."),
	good("legate_cuirass", "Legate's Cuirass", KindArmor, 9, 1000, ShelfMarket, true, Effects{Stats: Stats{Prowess: 1, Martial: 1}},
		"Molded bronze armor that marks a commander from across a battlefield."),
	good("keshi_scale", "Scale Armor of the Keshi Guard", KindArmor, 8, 900, ShelfMarket, true, Effects{Stats: pro(2)},
		"Thousands of lacquered scales, light as cloth and hard as stone."),
	good("parade_armor", "The Silver Parade Armor", KindArmor, 12, 1100, ShelfMarket, true, Effects{Stats: pro(1), PartyBonus: 2},
		"Made for triumphs, not battles. It still turns a blade."),

	// ---------- the market: equipment ----------
	good("wolfhounds", "A Pack of Wolfhounds", KindEquipment, 5, 500, ShelfMarket, true, Effects{Stats: pro(1)},
		"Six grey hounds bred to bring down wolves. Gentle with children. Mostly."),
	good("hunting_falcon", "A Hunting Falcon", KindEquipment, 6, 550, ShelfMarket, true, Effects{Stats: pro(1), RumorNotice: 3},
		"A peregrine with a hood of red leather. Nothing in the sky escapes her, and little on the ground."),
	good("steppe_stallion", "Black Stallion of the Steppes", KindEquipment, 8, 900, ShelfMarket, true, Effects{Stats: Stats{Martial: 1, Prowess: 1}},
		"Wild-born, tall and fast, and only tolerates one rider at a time."),
	good("racing_chariot", "A Keshi Racing Chariot", KindEquipment, 10, 1000, ShelfMarket, true, Effects{Stats: pro(1), PartyBonus: 2},
		"Gilded wheels and a team of four. Racing it through the streets is frowned upon, and done anyway."),
}

// ---- the rotating market ----

// How many market pieces are on sale each period.
const (
	MarketProperties  = 2
	MarketPossessions = 2
)

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
	var props, goods []Item
	for _, it := range Items {
		if it.Shelf != ShelfMarket {
			continue
		}
		if it.IsProperty() {
			props = append(props, it)
		} else {
			goods = append(goods, it)
		}
	}
	pick := func(pool []Item, n int) []Item {
		r.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		return pool[:min(n, len(pool))]
	}
	return append(pick(props, MarketProperties), pick(goods, MarketPossessions)...)
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
