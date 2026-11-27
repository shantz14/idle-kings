package game

import (
	"fmt"
	"math"
	"time"
)

// ---- diminishing returns ----

// influenceHalf is the influence at which everyday actions give half as much.
const influenceHalf = 30.0

// ScaleInfluence shrinks an everyday influence gain as the character's
// influence grows (half at 30, a third at 60), so the most powerful player
// can't simply grind their way to owning the Senate. Losses are not scaled.
func ScaleInfluence(gain, current float64) float64 {
	if gain <= 0 {
		return gain
	}
	return gain * influenceHalf / (influenceHalf + math.Max(current, 0))
}

// FreeKinds are activities that don't take up the character's one action:
// a public work is built by others while you get on with things, and a
// conspiracy ripens in the dark. Each kind is one at a time.
var FreeKinds = []string{KindWork, KindConspiracy}

// UsesSlot reports whether an activity kind takes up the character's action.
func UsesSlot(kind string) bool {
	for _, k := range FreeKinds {
		if k == kind {
			return false
		}
	}
	return true
}

// ---- Canvass the Districts ----

const KindCanvass = "canvass"

var (
	DefaultCanvassDuration = 3 * time.Hour
	FastCanvassDuration    = 90 * time.Second
)

// Placeholders to tune.
var canvassCheck = Check{Base: 30, Weights: map[Attr]float64{AttrDiplomacy: 5, AttrRenown: 0.5}}

const (
	canvassGreatMargin = 30
	canvassGreat       = 1.5
	canvassGood        = 1.0
	canvassPoor        = 0.2 // even a bad day of handshakes is worth something
)

type CanvassResult struct {
	Influence float64
	Success   bool
	Text      string
	Practice  Delta // a little Diplomacy from all those handshakes
}

// ResolveCanvass decides how a day of handshakes went.
func ResolveCanvass(r Roller, p Person) CanvassResult {
	ok, margin := canvassCheck.Roll(r, p.Sheet, 0, Normal)
	cur := p.Sheet[AttrInfluence]
	res := CanvassResult{Success: ok, Practice: Practice(Diplomacy, ok)}
	switch {
	case ok && margin >= canvassGreatMargin:
		res.Influence = ScaleInfluence(canvassGreat, cur)
		res.Text = "The districts loved you. Shopkeepers are naming their sons after you."
	case ok:
		res.Influence = ScaleInfluence(canvassGood, cur)
		res.Text = "A good day of handshakes, promises and babies kissed."
	default:
		res.Influence = ScaleInfluence(canvassPoor, cur)
		res.Text = "Doors closed in your face, mostly. A few people listened."
	}
	return res
}

// ---- Bread and Circuses ----

const KindCircus = "circus"

var (
	DefaultCircusDuration = 2 * time.Hour
	FastCircusDuration    = 2 * time.Minute
)

// CircusSize is a preset spend.
type CircusSize struct {
	Name string
	Gold int
}

var CircusSizes = []CircusSize{
	{"Grain Handout", 50},
	{"Festival", 150},
	{"Grand Feast", 400},
}

const (
	CircusMin = 30
	CircusMax = 1000
)

// CircusName is the largest preset the spend reaches, for announcements.
func CircusName(gold int) string {
	name := CircusSizes[0].Name
	for _, s := range CircusSizes {
		if gold >= s.Gold {
			name = s.Name
		}
	}
	return name
}

// Placeholders to tune.
var circusCheck = Check{Base: 40, Weights: map[Attr]float64{AttrStewardship: 5}}

const (
	circusInfluenceRate  = 0.8  // × log2(1 + gold/25)
	circusPopularityRate = 50.0 // gold per point of Popularity
	circusPopularityMax  = 8.0
	circusRiot           = 0.5 // share of the gains kept if it goes badly

	circusPopulist    = 2.0
	circusPopulistPer = 100.0 // +1 more per this much gold
	circusPopulistMax = 6.0
	circusBigSpender  = 2.0
	circusEconomical  = -2.0
)

// CircusInfluence is the raw influence for a spend, before scaling and luck.
func CircusInfluence(gold int) float64 {
	return circusInfluenceRate * math.Log2(1+float64(gold)/25)
}

// CircusPopularity is how much the people's mood rises for a spend.
func CircusPopularity(gold int) float64 {
	return math.Min(circusPopularityMax, float64(gold)/circusPopularityRate)
}

type CircusResult struct {
	Success    bool
	Delta      Delta
	Popularity float64
	Opinions   []OpinionChange
	Text       string
	Public     string
}

// ResolveCircus decides how the giveaway went.
func ResolveCircus(r Roller, p Person, gold int, senators []Senator) CircusResult {
	ok, _ := circusCheck.Roll(r, p.Sheet, 0, Normal)
	share := 1.0
	res := CircusResult{Success: ok}
	name := CircusName(gold)
	if ok {
		res.Text = fmt.Sprintf("Your %s went off without a hitch. The city is singing your name.", lowerFirst(name))
		res.Public = fmt.Sprintf("The city feasts at **%s**'s expense. The people will remember.", p.Name)
	} else {
		share = circusRiot
		res.Text = fmt.Sprintf("Your %s turned into a crush, and then a riot. Some of the goodwill survived.", lowerFirst(name))
		res.Public = fmt.Sprintf("**%s**'s %s ended in a brawl at the grain carts.", p.Name, lowerFirst(name))
	}
	res.Delta = Delta{Influence: ScaleInfluence(CircusInfluence(gold), p.Sheet[AttrInfluence]) * share}
	res.Delta.Merge(Practice(Stewardship, ok))
	res.Popularity = CircusPopularity(gold) * share
	res.Opinions = append(res.Opinions, TraitReactions(senators, p.ID,
		TraitReaction{TraitPopulist, math.Min(circusPopulistMax, circusPopulist+math.Floor(float64(gold)/circusPopulistPer))},
		TraitReaction{TraitBigSpender, circusBigSpender},
		TraitReaction{TraitEconomical, circusEconomical})...)
	return res
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}
