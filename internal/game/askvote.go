package game

import (
	"fmt"
	"math"
	"strings"
)

// ---- Ask how they'll vote ----
//
// A player can ask each senator once per matter how they mean to vote. The
// senator tells the truth, refuses, or lies, depending on what they think
// of the player, the player's Diplomacy and their own traits. The player
// is never told which.

// AskReply is how a senator answers.
type AskReply string

const (
	AskTruth  AskReply = "truth"
	AskLie    AskReply = "lie"
	AskRefuse AskReply = "refuse"
)

// Placeholders to tune.
const (
	// AskHostileBelow is the opinion below which a senator won't confide in
	// you at all: the scheming lie, everyone else shows you the door.
	AskHostileBelow = -40.0
	// askLieMargin: a failed ask that misses by less than this is a
	// refusal; worse, and a senator who will lie, lies.
	askLieMargin = -25
)

var askCheck = Check{Base: 30, Weights: map[Attr]float64{AttrDiplomacy: 5, AttrIntrigue: 1}}

// askTraitMods make senators more or less open about their votes.
var askTraitMods = map[Trait]float64{
	TraitScheming:     -15,
	TraitStern:        -5,
	TraitIsolationist: -5,
	TraitPopulist:     5,
	TraitCosmopolitan: 5,
	TraitBigSpender:   5,
}

// askHonest senators never lie about their vote; they refuse instead.
var askHonest = []Trait{TraitIncorruptible, TraitStern}

// askLiars always lie rather than refuse when they won't tell you.
var askLiars = []Trait{TraitScheming}

// SenatorIntent is how a senator would vote if the matter closed now: the
// option they were paid for, or their own view pulled toward the players
// they like who have voted so far. It is the same rule ResolveMatter uses.
func SenatorIntent(ev MatterEvent, realm Realm, matterID int64, s Senator, players []Player, ops CourtOpinions, bribe string) string {
	if _, ok := ev.Option(bribe); ok {
		return bribe
	}
	return s.decide(ev, s.scores(viewOptions(ev, realm), realm, matterID), players, ops)
}

type AskInput struct {
	Player   Person
	Senator  Senator
	Opinion  float64 // the senator's opinion of the player
	Event    MatterEvent
	Realm    Realm
	MatterID int64
	Truth    string // SenatorIntent
	// BribedByYou: the player paid this senator on this matter.
	BribedByYou bool
}

type AskResult struct {
	Kind     AskReply
	Option   string // what they said they'd vote for ("" on a refusal)
	Text     string
	Practice Delta
}

func (s Senator) hasAny(ts []Trait) bool {
	for _, t := range ts {
		if s.Has(t) {
			return true
		}
	}
	return false
}

// AskMod is the situational bonus for asking this senator.
func AskMod(s Senator, op float64) float64 {
	mod := OpinionCheckMod(op)
	for _, t := range s.Traits {
		mod += askTraitMods[t]
	}
	return mod
}

// ResolveAsk decides how a senator answers.
func ResolveAsk(r Roller, in AskInput) AskResult {
	s := in.Senator
	if in.BribedByYou {
		return in.reply(AskTruth, in.Truth, Delta{})
	}
	if in.Opinion < AskHostileBelow {
		if s.hasAny(askLiars) {
			return in.reply(AskLie, in.lie(), Delta{})
		}
		return in.reply(AskRefuse, "", Delta{})
	}
	ok, margin := askCheck.Roll(r, in.Player.Sheet, AskMod(s, Regard(in.Opinion, sheetRenown(in.Player.Sheet))), Normal)
	practice := Practice(Diplomacy, ok)
	switch {
	case ok:
		return in.reply(AskTruth, in.Truth, practice)
	case s.hasAny(askHonest):
		return in.reply(AskRefuse, "", practice)
	case s.hasAny(askLiars), margin <= askLieMargin:
		if lie := in.lie(); lie != "" {
			return in.reply(AskLie, lie, practice)
		}
	}
	return in.reply(AskRefuse, "", practice)
}

// lie is the option the senator likes best after the one they'll really
// vote for, so it sounds believable. "" if there is nothing else to name.
func (in AskInput) lie() string {
	sc := in.Senator.scores(viewOptions(in.Event, in.Realm), in.Realm, in.MatterID)
	best, bestScore := "", math.Inf(-1)
	for _, o := range in.Event.Options {
		if o.Key != in.Truth && sc[o.Key] > bestScore {
			best, bestScore = o.Key, sc[o.Key]
		}
	}
	return best
}

func (in AskInput) reply(kind AskReply, opt string, practice Delta) AskResult {
	if kind == AskLie && opt == "" { // nothing to lie about
		kind = AskRefuse
	}
	res := AskResult{Kind: kind, Option: opt, Practice: practice}
	res.Text = AskText(in.Senator, kind, in.BribedByYou, in.Event, opt, in.MatterID)
	return res
}

// ---- what they say ----

type askLine struct {
	Trait Trait // "" = anyone
	Text  string
}

// askLines are the answers, by kind. %s is the option, in lower case. A
// lie reads exactly like the truth.
var askLines = map[AskReply][]askLine{
	AskTruth: {
		{"", "\"I'll vote to %s.\""},
		{"", "\"Since you ask: I mean to vote to %s.\""},
		{"", "\"It's no secret. I'll vote to %s.\""},
		{TraitStern, "\"I will vote to %s. Don't ask me to change my mind.\""},
		{TraitScheming, "\"Between us? I'll vote to %s. Don't make me regret telling you.\""},
		{TraitPopulist, "\"I'll vote to %s, and the people will thank me for it.\""},
		{TraitGreedy, "\"Today I'll vote to %s. Tomorrow, who knows?\""},
		{TraitReckless, "\"I'll vote to %s, and the dice can fall where they like.\""},
	},
	AskRefuse: {
		{"", "\"How I vote is my business.\""},
		{"", "\"You'll find out with everyone else.\""},
		{"", "\"I haven't made up my mind, and I wouldn't tell you if I had.\""},
		{TraitStern, "\"A senator's vote is not gossip.\""},
		{TraitIncorruptible, "\"My vote is not for sale, and it is not for sharing.\""},
		{TraitGreedy, "\"My memory is poor these days. Perhaps something shiny would help it.\""},
		{TraitPious, "\"The gods know how I'll vote. That will have to do.\""},
	},
}

// askBribedLine is what a senator you paid says.
const askBribedLine = "\"As we agreed: I'll vote to %s.\""

// AskText is what the senator says. The same matter and senator always
// pick the same line, so asking again reads the same.
func AskText(s Senator, kind AskReply, bribed bool, ev MatterEvent, opt string, matterID int64) string {
	label := ""
	if o, ok := ev.Option(opt); ok {
		label = strings.ToLower(o.Label)
	}
	if bribed {
		return fmt.Sprintf(askBribedLine, label)
	}
	lineKind := kind
	if kind == AskLie {
		lineKind = AskTruth // a lie sounds like the truth
	}
	line := pickLine(askLines[lineKind], s, int(matterID+s.ID))
	if strings.Contains(line, "%s") {
		return fmt.Sprintf(line, label)
	}
	return line
}

// pickLine prefers lines for one of the senator's traits, then anyone's.
func pickLine(lines []askLine, s Senator, seed int) string {
	var own, any []string
	for _, l := range lines {
		switch {
		case l.Trait == "":
			any = append(any, l.Text)
		case s.Has(l.Trait):
			own = append(own, l.Text)
		}
	}
	pool := any
	if len(own) > 0 {
		pool = own
	}
	if len(pool) == 0 {
		return ""
	}
	if seed < 0 {
		seed = -seed
	}
	return pool[seed%len(pool)]
}
