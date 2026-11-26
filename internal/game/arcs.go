package game

import "fmt"

// Story arcs string Senate matters into chapters, so the realm's story moves
// forward. A matter belongs to an arc through MatterEvent.Arc and Chapter.
// The opener is an ordinary event; later chapters are FollowUpOnly and are
// queued by the previous chapter's outcomes.
//
// Strict arcs always continue: every outcome of every chapter before the last
// queues a later chapter (chance 1). Loose arcs are the older event chains,
// where the story can end early.

type ArcDef struct {
	Key      string
	Name     string
	Emoji    string
	Chapters int
	Strict   bool
}

var Arcs = []ArcDef{
	{Key: "ashwood_witch", Name: "Baba Yaga of the Ashwood", Emoji: "🌲", Chapters: 4, Strict: true},
	{Key: "fallen_star", Name: "The Fallen Star", Emoji: "☄️", Chapters: 4, Strict: true},
	{Key: "barrow_king", Name: "Acererak, the Barrow King", Emoji: "💀", Chapters: 5, Strict: true},
	{Key: "elixir", Name: "The Elixir of Youth", Emoji: "⚗️", Chapters: 3, Strict: true},

	{Key: "northern_tribes", Name: "The Dothraki", Emoji: "🪓", Chapters: 3},
	{Key: "eastern_road", Name: "The King of the Eastern Road", Emoji: "🗡️", Chapters: 2},
	{Key: "hungry_year", Name: "The Hungry Year", Emoji: "🌾", Chapters: 2},
	{Key: "black_lances", Name: "The Golden Company", Emoji: "🛡️", Chapters: 2},
	{Key: "plague", Name: "The Plague", Emoji: "🐀", Chapters: 2},
	{Key: "gladiators", Name: "The Gladiators' Revolt", Emoji: "⚔️", Chapters: 2},
	{Key: "pirates", Name: "The Pirates of the Teeth", Emoji: "🏴‍☠️", Chapters: 2},
}

func ArcByKey(k string) (ArcDef, bool) {
	for _, a := range Arcs {
		if a.Key == k {
			return a, true
		}
	}
	return ArcDef{}, false
}

// ArcLabel is the line shown above an arc's matter: "💀 The Barrow King ·
// Chapter 3 of 5". Empty for matters outside any arc.
func (e MatterEvent) ArcLabel() string {
	a, ok := ArcByKey(e.Arc)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s %s · Chapter %d of %d", a.Emoji, a.Name, e.Chapter, a.Chapters)
}

// ActiveArcs are the arcs with a chapter waiting in the queue.
func ActiveArcs(queued []string) map[string]bool {
	out := map[string]bool{}
	for _, k := range queued {
		if e, ok := MatterByKey(k); ok && e.Arc != "" {
			out[e.Arc] = true
		}
	}
	return out
}

// PickMatterAvoiding is PickMatter, but never opens an arc that is already
// under way (one of active), and may bring up motions to pass or repeal a
// law (see MotionAllowed).
func PickMatterAvoiding(r Roller, realm Realm, recent []string, active map[string]bool, laws Laws) MatterEvent {
	if len(recent) > MatterCooldown {
		recent = recent[len(recent)-MatterCooldown:]
	}
	var skip []string
	for _, e := range MatterEvents {
		if e.Arc != "" && active[e.Arc] {
			skip = append(skip, e.Key)
		}
	}
	all := append(skip, recent...)
	if e, ok := pickMatter(r, realm, all, lastTheme(recent), &laws); ok {
		return e
	}
	if e, ok := pickMatter(r, realm, all, "", &laws); ok {
		return e
	}
	return PickMatter(r, realm, recent)
}

// ---- the Chronicle ----

// Decided is a resolved matter, for the Chronicle.
type Decided struct {
	EventKey string
	Winner   string // option key
}

type ArcStatus int

const (
	ArcContinues ArcStatus = iota // a chapter is queued
	ArcOver                       // the last chapter has been decided
	ArcResting                    // a loose arc stopped early; it may start again
)

// ChronicleEntry is where one story stands.
type ChronicleEntry struct {
	Arc     ArcDef
	Chapter int
	Title   string // the latest chapter's title
	Choice  string // what the Senate chose, as the option's label
	Status  ArcStatus
}

// Chronicle summarizes every arc seen in the decisions (newest first),
// using queued (upcoming event keys) to tell whether it continues. Entries
// come in order of the most recent chapter.
func Chronicle(decisions []Decided, queued []string) []ChronicleEntry {
	active := ActiveArcs(queued)
	seen := map[string]bool{}
	var out []ChronicleEntry
	for _, d := range decisions {
		e, ok := MatterByKey(d.EventKey)
		if !ok || e.Arc == "" || seen[e.Arc] {
			continue
		}
		a, ok := ArcByKey(e.Arc)
		if !ok {
			continue
		}
		seen[e.Arc] = true
		entry := ChronicleEntry{Arc: a, Chapter: e.Chapter, Title: e.Title, Status: ArcResting}
		if o, ok := e.Option(d.Winner); ok {
			entry.Choice = o.Label
		}
		switch {
		case active[e.Arc]:
			entry.Status = ArcContinues
		case e.Chapter >= a.Chapters:
			entry.Status = ArcOver
		}
		out = append(out, entry)
	}
	return out
}

// Line renders an entry for the realm page.
func (c ChronicleEntry) Line() string {
	s := fmt.Sprintf("%s **%s** — chapter %d of %d: *%s*", c.Arc.Emoji, c.Arc.Name, c.Chapter, c.Arc.Chapters, c.Title)
	if c.Choice != "" {
		s += fmt.Sprintf(" (the Senate chose: %s)", c.Choice)
	}
	switch c.Status {
	case ArcContinues:
		s += ". The next chapter is coming."
	case ArcOver:
		s += ". The story is over."
	default:
		s += ". For now, the matter rests."
	}
	return s
}
