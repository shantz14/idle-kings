package game

import (
	"fmt"
	"strings"
)

// Realm warnings are posted when a tracker that can end the realm crosses
// a line, so players see trouble coming and can act.
const (
	WarnLow    = 25.0
	WarnDanger = 10.0
)

// RealmWarnings lists warnings for every tracker that fell below a line
// between before and after. It's stateless: only crossings warn.
func RealmWarnings(before, after Realm) []string {
	var out []string
	for _, t := range Trackers {
		if t.Fall == "" {
			continue
		}
		b, a := before[t.Key], after[t.Key]
		name := strings.ToLower(t.Name)
		switch {
		case a <= TrackerMin:
		case b >= WarnDanger && a < WarnDanger:
			out = append(out, fmt.Sprintf("🚨 **The realm is in danger.** %s is at %s. If it reaches 0, everyone loses. Aid the Realm, or vote carefully.", t.Name, Num(a)))
		case b >= WarnLow && a < WarnLow:
			out = append(out, fmt.Sprintf("⚠️ The %s is running low (%s).", name, Num(a)))
		}
	}
	return out
}

// ---- sector previews on vote options ----

// SectorArrow is how an option moves one sector: Dir is -2..2.
type SectorArrow struct {
	Sector  SectorDef
	Dir     int
	Lasting bool // capacity, not a passing shock
	IfFails bool
}

// bigShock is the health or capacity change that counts as a big move.
const (
	bigHealth   = 0.15
	bigCapacity = 6.0
)

// SectorPreview lists how an option would move each sector, success first.
func SectorPreview(o MatterOption) []SectorArrow {
	var out []SectorArrow
	add := func(out []SectorArrow, ch map[SectorKey]SectorChange, fails bool) []SectorArrow {
		for _, d := range SectorDefs {
			c, ok := ch[d.Key]
			if !ok || (c.Health == 0 && c.Capacity == 0) {
				continue
			}
			a := SectorArrow{Sector: d, IfFails: fails, Lasting: c.Capacity != 0}
			v, big := c.Health, bigHealth
			if c.Capacity != 0 {
				v, big = c.Capacity, bigCapacity
			}
			switch {
			case v >= big:
				a.Dir = 2
			case v > 0:
				a.Dir = 1
			case v <= -big:
				a.Dir = -2
			default:
				a.Dir = -1
			}
			out = append(out, a)
		}
		return out
	}
	out = add(out, o.Success.Sectors, false)
	if o.Check != nil {
		out = add(out, o.Failure.Sectors, true)
	}
	return out
}

// DescribeArrows renders a preview: "🌾 Agriculture ▼ · ⛏️ Mining ▲▲ (lasting)".
func DescribeArrows(arrows []SectorArrow) string {
	var ok, fail []string
	for _, a := range arrows {
		s := a.Sector.Emoji + " " + a.Sector.Name + " " + map[int]string{2: "▲▲", 1: "▲", -1: "▼", -2: "▼▼"}[a.Dir]
		if a.Lasting {
			s += " (lasting)"
		}
		if a.IfFails {
			fail = append(fail, s)
		} else {
			ok = append(ok, s)
		}
	}
	out := strings.Join(ok, " · ")
	if len(fail) > 0 {
		if out != "" {
			out += " · "
		}
		out += "if it fails: " + strings.Join(fail, " · ")
	}
	return out
}
