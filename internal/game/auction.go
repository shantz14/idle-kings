package game

import "math"

// ---- Auctions ----
//
// An auction sells one unique piece to the highest bidder. Bids are open:
// each must beat the last by a step, and an outbid bidder gets their gold
// back. The highest bid when time runs out wins. A Venetian merchant's
// auction (a happening) brings curiosities the shop never sells, starting
// well under their worth, so a quiet court gets a bargain; players can put
// their own unique possessions up too. Placeholder numbers — tune freely.

// KindAuctionHappening is the happening that brings an NPC auction.
const KindAuctionHappening = "auction"

const (
	auctionStep    = 0.1 // each bid beats the last by this share
	auctionMinStep = 5   // gold
	// npcReserve is the share of a lot's list price an NPC auction opens at.
	npcReserve = 0.2
)

// AuctionDays are the lengths a player can choose, in game days.
var AuctionDays = []int{1, 2}

// AuctionReserves are the reserve prices a player can choose, as shares of
// the piece's list price.
var AuctionReserves = []float64{0.5, 0.8, 1}

// MinBid is the least the next bid can be.
func MinBid(top, reserve int) int {
	if top <= 0 {
		return max(reserve, 1)
	}
	return top + max(auctionMinStep, int(math.Ceil(float64(top)*auctionStep)))
}

// NPCReserve is what an NPC auction opens a lot at.
func NPCReserve(it Item) int { return max(10, int(math.Round(float64(it.Price)*npcReserve))) }

// PlayerReserve is a reserve chosen as a share of the piece's list price.
func PlayerReserve(it Item, share float64) int {
	return max(1, int(math.Round(float64(it.Price)*share)))
}

// Auctionable reports whether a player may put a piece up for auction: a
// unique possession (never a house).
func Auctionable(it Item) bool { return it.Unique && !it.IsProperty() }

// AuctionLots are the curiosities only a Venetian merchant's auction sells.
// They are modest on purpose: one may go for a song.
var AuctionLots = []Item{
	good("unicorn_horn", "A Unicorn Horn", KindArtifact, 6, 600, ShelfNone, true, Effects{PartyBonus: 2},
		"Spiralled, ivory-white and longer than your arm. A sailor swears it's a narwhal's. Nobody listens to sailors."),
	good("feejee_mermaid", "A Mermaid in a Jar", KindArtifact, 5, 450, ShelfNone, true, Effects{PartyBonus: 1, RumorNotice: 1},
		"Half monkey, half fish, all horror, and the guests can't stop looking at it. The servants won't dust it."),
	good("true_cross", "A Splinter of the True Cross", KindRelic, 6, 700, ShelfNone, true, Effects{Stats: Stats{Diplomacy: 5}},
		"One of several hundred in the world, each the only real one. Priests bow when they see it."),
	good("neros_lyre", "Nero's Lyre", KindArt, 6, 650, ShelfNone, true, Effects{PartyBonus: 2},
		"Slightly scorched. It is said to sound best by firelight."),
	good("golden_ass", "The Golden Ass of Apuleius", KindArt, 5, 500, ShelfNone, true, Effects{DailyIncome: 3},
		"A gilded donkey with a dreamy expression, from the story of the man turned into one. Merchants rub its nose for luck."),
	good("stuffed_dodo", "A Dodo, Stuffed", KindArt, 4, 300, ShelfNone, true, Effects{PartyBonus: 1, DailyIncome: 2},
		"The last of its kind, or near enough. It looks faintly surprised, as it did in life."),
	good("ptolemy_charts", "Ptolemy's Star Charts", KindArtifact, 6, 800, ShelfNone, true, Effects{Stats: Stats{Stewardship: 5}},
		"Every star in its crystal sphere, and every sphere in its place. Your accountants find it soothing."),
	good("bezoar", "A Bezoar against Poison", KindEquipment, 5, 550, ShelfNone, true, Effects{Stats: Stats{Prowess: 5}},
		"A stone from a goat's belly, set in gold. Drop it in your wine and no poison can touch you, they say. It hasn't failed yet."),
	good("shrunken_head", "A Shrunken Head from the Amazon", KindArtifact, 5, 500, ShelfNone, true, Effects{Stats: Stats{Intrigue: 5}},
		"Its lips are sewn shut. It keeps your secrets, and it keeps your visitors nervous."),
	good("codex_seraphinianus", "The Codex Seraphinianus", KindArtifact, 7, 900, ShelfNone, true, Effects{VoteWeight: 1},
		"An encyclopedia of a world that doesn't exist, in a script nobody can read. Senators borrow it to look clever."),
}

func init() { Items = append(Items, AuctionLots...) }

// PickLot draws a lot nobody at the court owns yet. ok is false if every lot
// has been sold.
func PickLot(r Roller, owned map[string]bool) (Item, bool) {
	var pool []Item
	for _, it := range AuctionLots {
		if !owned[it.Key] {
			pool = append(pool, it)
		}
	}
	if len(pool) == 0 {
		return Item{}, false
	}
	return pool[r.IntN(len(pool))], true
}
