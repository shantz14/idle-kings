package game

import (
	"errors"
	"fmt"
	"strings"
)

const (
	BaseStat         = 2 // every stat starts here
	FreePoints       = 6 // points the player assigns
	MaxPointsPerStat = 4 // most free points in one stat
	MaxNameLen       = 32
	MaxMottoLen      = 60
)

// Build is everything chosen in the character creator.
type Build struct {
	Name       string `json:"name"`
	Family     string `json:"family"`
	Crest      Crest  `json:"crest"`
	Points     Stats  `json:"points"`
	Background string `json:"background"`
	Role       string `json:"role"`
}

func (b Build) PointsSpent() int {
	n := 0
	for _, st := range AllStats {
		n += int(b.Points[st])
	}
	return n
}

func (b Build) PointsLeft() int { return FreePoints - b.PointsSpent() }

// Validate checks a finished build.
func (b Build) Validate() error {
	var errs []error
	if strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.Family) == "" {
		errs = append(errs, errors.New("a name and family name are required"))
	}
	if len(b.Name) > MaxNameLen || len(b.Family) > MaxNameLen || len(b.Crest.Motto) > MaxMottoLen {
		errs = append(errs, errors.New("a name or motto is too long"))
	}
	if !b.Crest.Complete() {
		errs = append(errs, errors.New("choose a crest color and symbol"))
	}
	for st, v := range b.Points {
		known := false
		for _, s := range AllStats {
			known = known || s == st
		}
		if !known || v < 0 || v > MaxPointsPerStat || v != float64(int(v)) {
			errs = append(errs, fmt.Errorf("invalid points for %s", st))
		}
	}
	if b.PointsSpent() != FreePoints {
		errs = append(errs, fmt.Errorf("spend exactly %d stat points", FreePoints))
	}
	if _, ok := BackgroundByKey(b.Background); !ok {
		errs = append(errs, errors.New("choose a background"))
	}
	if _, ok := RoleByKey(b.Role); !ok {
		errs = append(errs, errors.New("choose a role"))
	}
	return errors.Join(errs...)
}

// Start is a new character's starting state.
type Start struct {
	Stats       Stats
	Influence   float64
	Renown      float64
	Wealth      int
	DailyIncome int // base income (salary); estate income is added on top
	Items       []string
}

// AssignedStats is base plus free points: what the stats step shows.
func (b Build) AssignedStats() Stats {
	s := Stats{}
	for _, st := range AllStats {
		s[st] = BaseStat + b.Points[st]
	}
	return s
}

// Final works out the starting character. Unknown background/role keys
// contribute nothing; call Validate first.
func (b Build) Final() Start {
	st := Start{Stats: b.AssignedStats()}
	if bg, ok := BackgroundByKey(b.Background); ok {
		st.Stats.Add(bg.StatBonus)
		st.Renown, st.Wealth, st.Items = bg.Renown, bg.Wealth, bg.EstateItems()
	}
	if r, ok := RoleByKey(b.Role); ok {
		st.Stats.Add(r.StatBonus)
		st.Influence, st.DailyIncome = r.Influence, r.Salary
	}
	return st
}
