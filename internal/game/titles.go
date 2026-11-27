package game

import (
	"fmt"
	"math"
	"sort"
)

// ---- Seasons, titles and deeds ----
//
// A season is SeasonDays Senate days. When it ends the court crowns the
// leaders with season titles (Great House, Master of Coin, …) and play goes
// on; a realm that falls crowns nobody. Feats are lifetime titles earned once
// by doing something remarkable: slaying a happening's beast, building every
// public work. Titles add to prestige. Placeholder numbers — tune freely.

// SeasonDays is how many Senate days a season lasts.
const SeasonDays = 30

// Deed kinds: what the season's deed titles count (store.deeds).
const (
	DeedDuelWon     = "duel_won"     // amount 1 per duel won
	DeedWinningVote = "winning_vote" // amount 1 per vote on the winning side
	DeedCrisisAid   = "crisis_aid"   // gold given to a tracker in danger
	DeedDuelRefused = "duel_refused" // amount 1 per challenge refused (or never answered)
)

// Season titles.
const (
	TitleGreatHouse   = "great_house"
	TitleMasterOfCoin = "master_of_coin"
	TitleChampion     = "champion"
	TitleKingmaker    = "kingmaker"
	TitleSaviour      = "saviour"
)

// Feats that aren't a happening's.
const (
	FeatPatron   = "patron"
	FeatUnrobbed = "unrobbed"
	FeatWhispers = "master_of_whispers"
)

// featPrefix marks a happening's feat: "feat_manticore".
const featPrefix = "feat_"

// TitleDef is a season title or a feat.
type TitleDef struct {
	Key         string
	Name        string
	Emoji       string
	Description string
	Season      bool   // given at the end of a season, not earned once
	Deed        string // for season deed titles: the deed kind counted
}

var TitleDefs = []TitleDef{
	{Key: TitleGreatHouse, Name: "Great House", Emoji: "👑", Season: true,
		Description: "The most prestigious house at court: famous, powerful and decorated."},
	{Key: TitleMasterOfCoin, Name: "Master of Coin", Emoji: "💰", Season: true,
		Description: "The richest house at court, in gold, ventures and estate."},
	{Key: TitleChampion, Name: "Champion of the Arena", Emoji: "⚔️", Season: true, Deed: DeedDuelWon,
		Description: "Won the most duels this season."},
	{Key: TitleKingmaker, Name: "Kingmaker", Emoji: "🗳️", Season: true, Deed: DeedWinningVote,
		Description: "Voted with the winning side more often than anyone this season."},
	{Key: TitleSaviour, Name: "Saviour of the Realm", Emoji: "🛡️", Season: true, Deed: DeedCrisisAid,
		Description: "Gave the most to the realm while it was in danger this season."},
	{Key: FeatPatron, Name: "Patron of the City", Emoji: "🏛️",
		Description: "Built every public work: the statue, the road, the temple and the aqueduct."},
	{Key: FeatUnrobbed, Name: "Unrobbed", Emoji: "🛡️",
		Description: "Drove off a sellsword company hired to rob them on the road."},
	{Key: FeatWhispers, Name: "Master of Whispers", Emoji: "🕸️",
		Description: fmt.Sprintf("Had %s spies in %d houses at once.", SpyTiers[len(SpyTiers)-1].Name, whispersHouses)},
}

// whispersHouses is how many houses need top-tier spies for the feat.
const whispersHouses = 3

func init() {
	for _, h := range HappeningDefs {
		if h.Feat != "" {
			TitleDefs = append(TitleDefs, TitleDef{Key: HappeningFeat(h.Key), Name: h.Feat, Emoji: h.Emoji,
				Description: fmt.Sprintf("Ended %s.", lowerFirst(h.Name))})
		}
	}
}

// HappeningFeat is the title key for ending a happening.
func HappeningFeat(happening string) string { return featPrefix + happening }

func TitleByKey(k string) (TitleDef, bool) { return titleIndex.get(k) }

// SeasonTitles are the titles given at the end of every season, in order.
func SeasonTitles() []TitleDef {
	var out []TitleDef
	for _, t := range TitleDefs {
		if t.Season {
			out = append(out, t)
		}
	}
	return out
}

// Title is a title someone holds. Season is 0 for a feat.
type Title struct {
	Key    string
	Season int
}

// Label is how a held title is shown: "👑 Great House (Season 2)".
func (t Title) Label() string {
	d, ok := TitleByKey(t.Key)
	if !ok {
		return t.Key
	}
	if t.Season > 0 {
		return fmt.Sprintf("%s %s (Season %d)", d.Emoji, d.Name, t.Season)
	}
	return d.Emoji + " " + d.Name
}

// CountTitles splits held titles into season titles and feats.
func CountTitles(ts []Title) (season, feats int) {
	for _, t := range ts {
		if t.Season > 0 {
			season++
		} else {
			feats++
		}
	}
	return season, feats
}

// ---- the scores ----

const (
	prestigePerInfluence = 10.0  // per point of influence or monument vote weight
	prestigePerTitle     = 100.0 // per season title won before
	prestigePerFeat      = 25.0
)

// Prestige is the Great House score: fame, power and honours.
func Prestige(renown, influence, monumentWeight float64, titles []Title) float64 {
	season, feats := CountTitles(titles)
	return math.Round(math.Max(renown, 0) + prestigePerInfluence*(math.Max(influence, 0)+monumentWeight) +
		prestigePerTitle*float64(season) + prestigePerFeat*float64(feats))
}

// Wealth is the Master of Coin score: gold on hand, the gold staked in the
// realm's public industries, and the list price of everything in the estate.
// Secret stakes are left out, so the leaderboard never gives them away.
func Wealth(gold int, publicStakes float64, estatePrices int) int {
	return max(gold, 0) + int(math.Round(publicStakes)) + estatePrices
}

// Score is one house's score for a title.
type Score struct {
	CharacterID int64
	Value       float64
}

// Winners are the houses with the top score, all of them on a tie. Nobody
// wins with a score of zero or less.
func Winners(scores []Score) []int64 {
	best := 0.0
	for _, s := range scores {
		best = math.Max(best, s.Value)
	}
	if best <= 0 {
		return nil
	}
	var out []int64
	for _, s := range scores {
		if s.Value == best {
			out = append(out, s.CharacterID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Ranked sorts scores best first (ties by ID, for a stable leaderboard).
func Ranked(scores []Score) []Score {
	out := append([]Score(nil), scores...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return out[i].CharacterID < out[j].CharacterID
	})
	return out
}

// ---- feats ----

// PatronEarned reports whether a house has built every public work.
func PatronEarned(monuments []string) bool {
	for _, w := range WorkDefs {
		if !contains(monuments, w.Key) {
			return false
		}
	}
	return true
}

// WhispersEarned reports whether a spymaster has top-tier spies in enough
// houses.
func WhispersEarned(levels []float64) bool {
	top := SpyTiers[len(SpyTiers)-1]
	n := 0
	for _, v := range levels {
		if SpyTierOf(v).Name == top.Name {
			n++
		}
	}
	return n >= whispersHouses
}

// UnrobbedBand is the bandit band whose defeat earns the Unrobbed feat.
const UnrobbedBand = "sellswords"
