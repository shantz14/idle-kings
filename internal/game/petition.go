package game

import (
	"fmt"
	"time"
)

// ---- Petition the Senate (Diplomacy) ----
//
// A player lobbies the Senate's clerks and senators to put a matter on the
// agenda. It always reaches the floor, as an extra vote alongside the day's
// matter (not a Senate day: no drift, no daily laws), and the
// petitioner's name is on it: they gain if it passes and look foolish if it
// doesn't. Petitions are general: today they pass or repeal laws, and new
// kinds only need a PetitionDef.

const KindPetition = "petition"

const (
	DefaultPetitionDuration = 2 * time.Hour
	FastPetitionDuration    = 2 * time.Minute
)

// PetitionCost is the gold spent on clerks, dinners and copies of the
// motion. Placeholder.
const PetitionCost = 30

// PetitionDef is something a player can petition the Senate for.
type PetitionDef struct {
	Key   string // "pass:<law>" or "repeal:<law>"
	Name  string
	Desc  string
	Event string // the matter it puts on the agenda
	// Lapses are the laws that would lapse if the Senate passed it (the
	// oldest, when every slot is taken). Players are warned.
	Lapses []LawKey
}

// Petitions are what a player can petition for under the laws in force: any
// law not in force, or the repeal of any law that is. The cap never blocks a
// petition: passing one past MaxLaws makes the oldest lapse (Lapses).
func Petitions(ls Laws) []PetitionDef {
	var out []PetitionDef
	for _, d := range LawDefs {
		if ls.Has(d.Key) {
			out = append(out, PetitionDef{Key: "repeal:" + string(d.Key), Name: "Repeal " + d.Title(),
				Desc: d.Emoji + " " + d.Desc, Event: RepealKey(d.Key)})
		} else {
			out = append(out, PetitionDef{Key: "pass:" + string(d.Key), Name: "Pass " + d.Title(),
				Desc: d.Emoji + " " + d.Desc, Event: MotionKey(d.Key), Lapses: ls.WouldLapse(d.Key)})
		}
	}
	return out
}

// PetitionByKey finds a petition that is open under the laws in force.
func PetitionByKey(key string, ls Laws) (PetitionDef, bool) {
	for _, p := range Petitions(ls) {
		if p.Key == key {
			return p, true
		}
	}
	return PetitionDef{}, false
}

// Placeholders to tune.
const (
	petitionWinInfl    = 1.5 // the motion passes (through ScaleInfluence)
	petitionWinRenown  = 1.0
	petitionLoseRenown = 0.5 // the Senate votes it down
)

type PetitionResult struct {
	Success bool
	Delta   Delta
	Text    string
}

// ResolvePetition puts the matter on the floor. A petition always gets there;
// the stakes are in the vote (petitionerResult).
func ResolvePetition(r Roller, p Person, def PetitionDef) PetitionResult {
	return PetitionResult{Success: true, Delta: Practice(Diplomacy, true),
		Text: fmt.Sprintf("The clerks have your petition to **%s** on the agenda. The Senate takes it up at once, alongside the day's business.", lowerFirst(def.Name))}
}

// petitionerResult rewards whoever petitioned for the matter if the Senate
// went their way (anything but the status quo), and shames them if not.
func petitionerResult(res *MatterResult, in MatterInput) {
	if in.Petitioner == 0 {
		return
	}
	var current float64
	for _, p := range in.Players {
		if p.ID == in.Petitioner {
			current = p.Sheet[AttrInfluence]
		}
	}
	if res.Winner.Key != in.Event.Default {
		d := Delta{Influence: ScaleInfluence(petitionWinInfl, current), Renown: petitionWinRenown}
		res.delta(in.Petitioner, d)
		res.Notices[in.Petitioner] = append(res.Notices[in.Petitioner], "The Senate granted your petition. ("+d.Describe()+")")
		return
	}
	d := Delta{Renown: -petitionLoseRenown}
	res.delta(in.Petitioner, d)
	res.Notices[in.Petitioner] = append(res.Notices[in.Petitioner], "The Senate voted down your petition. ("+d.Describe()+")")
}
