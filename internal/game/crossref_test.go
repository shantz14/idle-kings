package game

import (
	"strings"
	"testing"
)

// Keys that point at other catalogues must name something that exists: a
// typo here is silently ignored at runtime.

func TestLawKeysExist(t *testing.T) {
	trackers := map[Attr]bool{}
	for _, tr := range Trackers {
		trackers[tr.Key] = true
	}
	for _, d := range LawDefs {
		for k := range d.Revenue {
			if _, ok := SectorByKey(k); !ok {
				t.Errorf("law %s: revenue for unknown sector %q", d.Key, k)
			}
		}
		for k := range d.DailySectors {
			if _, ok := SectorByKey(k); !ok {
				t.Errorf("law %s: daily change to unknown sector %q", d.Key, k)
			}
		}
		for k := range d.Daily {
			if !trackers[k] {
				t.Errorf("law %s: daily change to unknown tracker %q", d.Key, k)
			}
		}
		for tr := range d.Traits {
			if _, ok := TraitByKey(tr); !ok {
				t.Errorf("law %s: feeling for unknown trait %q", d.Key, tr)
			}
		}
	}
}

func TestMatterRoleKeysExist(t *testing.T) {
	for _, ev := range MatterEvents {
		for _, o := range ev.Options {
			for _, out := range []MatterOutcome{o.Success, o.Failure} {
				for role := range out.Roles {
					if _, ok := RoleByKey(role); !ok {
						t.Errorf("%s/%s: unknown role %q", ev.Key, o.Key, role)
					}
				}
			}
		}
	}
}

func TestEventKeysAreUniqueAndConditionsExist(t *testing.T) {
	seen := map[string]string{}
	lists := map[string][]PartyEvent{
		"party": PartyEvents, "hunt": HuntEvents, "duel": DuelEvents,
		"journey": JourneyEvents, "abroad": JourneyAbroadEvents, "happening": happeningEvents,
	}
	for name, list := range lists {
		for _, e := range list {
			if other, dup := seen[e.Key]; dup {
				t.Errorf("event key %q is in both %s and %s: EventByKey can only find one", e.Key, other, name)
			}
			seen[e.Key] = name
			for _, c := range e.Choices {
				for _, out := range []EventOutcome{c.Success, c.Failure} {
					if out.Condition == "" {
						continue
					}
					if _, ok := ConditionByKey(out.Condition); !ok {
						t.Errorf("%s event %s/%s: unknown condition %q", name, e.Key, c.Key, out.Condition)
					}
				}
			}
			for _, role := range e.Roles {
				if _, ok := RoleByKey(role); !ok {
					t.Errorf("%s event %s: unknown role %q", name, e.Key, role)
				}
			}
		}
	}
}

func TestSchemesHaveWords(t *testing.T) {
	for _, s := range SchemeDefs {
		if s.Action == "" || strings.Count(s.Success, "%s") != 1 {
			t.Errorf("scheme %s needs an Action and a Success with one %%s", s.Key)
		}
	}
	for _, w := range workTiers {
		if w.Bigger == "" {
			t.Errorf("work tier %s needs a Bigger word", w.Suffix)
		}
	}
}
