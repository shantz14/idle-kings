package game

import (
	"fmt"
	"math"
	"time"
)

// KindSpeech is the activity kind for Give a Speech in the Forum. It runs
// like a party: the speaker picks a cause, others may attend to cheer or
// heckle, and it all resolves at the end.
const KindSpeech = "speech"

var (
	DefaultSpeechDuration = time.Hour
	FastSpeechDuration    = 2 * time.Minute
)

// Cause is what a speech argues for. Senators who share it warm to a good
// speaker; those who oppose it cool.
type Cause struct {
	Key         string
	Name        string
	Description string
	Pleases     []Trait
	Annoys      []Trait
}

var Causes = []Cause{
	{"ancestors", "Honour the Ancestors", "The old ways made us great. Traditionalists will cheer; gamblers will yawn.",
		[]Trait{TraitConservative}, []Trait{TraitRiskTaker}},
	{"people", "Champion the People", "Bread, work and dignity for the city. Populists love it; misers worry about the cost.",
		[]Trait{TraitPopulist}, []Trait{TraitEconomical}},
	{"strength", "Call for Strength", "Steel on the borders and fear in our enemies. Hawks approve; doves don't.",
		[]Trait{TraitHawk}, []Trait{TraitDove}},
	{"peace", "Call for Peace", "Enough blood. Doves approve; hawks call it weakness.",
		[]Trait{TraitDove}, []Trait{TraitHawk}},
	{"gates", "Open Our Gates", "Trade and friendship with the world. Cosmopolitans approve; isolationists bristle.",
		[]Trait{TraitCosmopolitan}, []Trait{TraitIsolationist}},
}

func CauseByKey(k string) (Cause, bool) {
	for _, c := range Causes {
		if c.Key == k {
			return c, true
		}
	}
	return Cause{}, false
}

const (
	GoalCheer  Goal = "cheer"
	GoalHeckle Goal = "heckle"
)

var SpeechGoals = []GoalInfo{
	{GoalCheer, "Cheer", "Applaud in the right places. Helps the speech, and earns you a little goodwill."},
	{GoalHeckle, "Heckle", "Shout them down. Win the exchange and the crowd is yours; lose it and you're the fool."},
}

func SpeechGoalName(g Goal) string {
	for _, gi := range SpeechGoals {
		if gi.Key == g {
			return gi.Name
		}
	}
	return string(g)
}

// ---- numbers (placeholders to tune) ----

var speechCheck = Check{Base: 15, Weights: map[Attr]float64{AttrDiplomacy: 4, AttrRenown: 1}}

const (
	speechGreatMargin = 30
	speechInfluence   = 1.5
	speechRenown      = 2
	speechGreatScale  = 1.5
	speechFailRenown  = -3
	speechFailInfl    = -0.3

	cheerQuality    = 5.0
	cheerInfluence  = 0.1
	heckleWin       = -12.0 // speech quality when a heckler wins
	heckleLose      = 3.0   // the speaker's comeback
	heckleInfluence = 0.5
	heckleFoolish   = -3 // a heckler who loses, in renown

	causePleased = 2.0
	causeAnnoyed = -1.0
)

// HeckleChance is the heckler's percent chance to win the exchange.
func HeckleChance(heckler, speaker Sheet) int {
	f := 50 + 5*(heckler[AttrDiplomacy]-speaker[AttrDiplomacy]) + 0.5*(heckler[AttrRenown]-speaker[AttrRenown])
	return int(math.Max(MinChance, math.Min(MaxChance, f)))
}

// SpeechChance exposes the speaker's odds at a given quality. Not shown to players.
func SpeechChance(speaker Sheet, quality float64) int { return speechCheck.Chance(speaker, quality) }

type Listener struct {
	Person
	Goal Goal
}

type SpeechInput struct {
	Speaker  Person
	Cause    Cause
	Audience []Listener
	Senators []Senator
}

// ResolveSpeech decides how the speech went. Hecklers are always named:
// heckling is done in public.
func ResolveSpeech(r Roller, in SpeechInput) PartyResult {
	res := PartyResult{Ledger: newLedger()}
	sp := in.Speaker
	quality := 0.0
	bestHeckler, bestMargin := "", 0

	for _, l := range in.Audience {
		switch l.Goal {
		case GoalCheer:
			quality += cheerQuality
			d := Delta{Influence: ScaleInfluence(cheerInfluence, l.Sheet[AttrInfluence])}
			res.delta(l.ID, d)
			res.notice(l.ID, "%s", withEffects(fmt.Sprintf("You cheered %s's speech from the front row.", sp.Name), d))
		case GoalHeckle:
			m := Margin(r, HeckleChance(l.Sheet, sp.Sheet), Normal)
			res.delta(l.ID, Practice(Diplomacy, m > 0))
			if m > 0 {
				quality += heckleWin
				d := Delta{Influence: ScaleInfluence(heckleInfluence, l.Sheet[AttrInfluence])}
				res.delta(l.ID, d)
				res.notice(l.ID, "%s", withEffects(fmt.Sprintf("You heckled %s, and the crowd laughed with you.", sp.Name), d))
				res.notice(sp.ID, "%s heckled you, and the crowd laughed along.", l.Ref())
				if m > bestMargin {
					bestHeckler, bestMargin = l.Name, m
				}
			} else {
				quality += heckleLose
				d := Delta{Renown: heckleFoolish}
				res.delta(l.ID, d)
				res.notice(l.ID, "%s", withEffects(fmt.Sprintf("You heckled %s, and they made a fool of you.", sp.Name), d))
				res.notice(sp.ID, "%s tried to heckle you. Your answer brought the house down.", l.Ref())
			}
		}
	}

	ok, margin := speechCheck.Roll(r, sp.Sheet, quality, Normal)
	res.Success = ok
	var d Delta
	if ok {
		scale := 1.0
		text := "Your speech was well received."
		if margin >= speechGreatMargin {
			scale, text = speechGreatScale, "Your speech will be quoted for years."
		}
		d = Delta{Influence: ScaleInfluence(speechInfluence, sp.Sheet[AttrInfluence]) * scale, Renown: speechRenown * scale}
		res.notice(sp.ID, "%s", withEffects(text, d))
		res.Public = fmt.Sprintf("**%s** spoke in the Forum: *%s*. The crowd roared.", sp.Name, in.Cause.Name)
		var reactions []TraitReaction
		for _, t := range in.Cause.Pleases {
			reactions = append(reactions, TraitReaction{t, causePleased})
		}
		for _, t := range in.Cause.Annoys {
			reactions = append(reactions, TraitReaction{t, causeAnnoyed})
		}
		res.Opinions = append(res.Opinions, TraitReactions(in.Senators, sp.ID, reactions...)...)
	} else {
		d = Delta{Influence: speechFailInfl, Renown: speechFailRenown}
		res.notice(sp.ID, "%s", withEffects("Your speech was a humiliation.", d))
		res.Public = fmt.Sprintf("**%s** spoke in the Forum: *%s*. It did not go well.", sp.Name, in.Cause.Name)
		if bestHeckler != "" {
			res.Public += fmt.Sprintf(" **%s** had the crowd laughing at every line.", bestHeckler)
		}
	}
	d.Merge(Practice(Diplomacy, ok))
	res.delta(sp.ID, d)
	return res
}
