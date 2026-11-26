package game

import "fmt"

// KindHunt is the activity kind for Hunt in the Royal Forest. It runs like a
// party: the host picks a goal, gets events by DM, and others may join.
const KindHunt = "hunt"

// HuntCost is what the host pays for horses and hounds. Placeholder.
const HuntCost = 10

const (
	GoalHuntGlory   Goal = "hunt_glory"   // host or guest: renown
	GoalSaddleTalk  Goal = "saddle_talk"  // host: influence
	GoalRideFriends Goal = "ride_friends" // guest: influence
	GoalAccident    Goal = "accident"     // guest: wound another rider
)

var (
	HuntHostGoals = []GoalInfo{
		{GoalHuntGlory, "Chase Glory", "Bring down great game. A good hunt grows your renown, and your sword arm."},
		{GoalSaddleTalk, "Talk in the Saddle", "A long ride is good for frank talk. A good hunt grows your influence."},
	}
	HuntGuestGoals = []GoalInfo{
		{GoalHuntGlory, "Hunt for Glory", "Ride hard and be seen to. Grows your renown."},
		{GoalRideFriends, "Ride with Friends", "Keep good company. Grows your influence."},
		{GoalAccident, "Arrange an Accident", "A loose girth, a stray arrow. Wound another rider, if no one sees."},
	}
)

func HuntGoalName(g Goal) string {
	for _, gs := range [][]GoalInfo{HuntHostGoals, HuntGuestGoals} {
		for _, gi := range gs {
			if gi.Key == g {
				return gi.Name
			}
		}
	}
	return string(g)
}

// ---- checks and numbers (all placeholders to tune) ----

var (
	// Host success: prowess > martial > renown = estate (horses and hounds).
	huntCheck = Check{Base: 15, Weights: map[Attr]float64{AttrProwess: 2.0, AttrMartial: 1.5, AttrRenown: 0.5, AttrEstate: 0.5}}

	huntGloryCheck   = Check{Base: 20, Weights: map[Attr]float64{AttrProwess: 5}}
	huntFriendsCheck = Check{Base: 20, Weights: map[Attr]float64{AttrDiplomacy: 5, AttrInfluence: 1}}
	accidentCheck    = Check{Base: 10, Weights: map[Attr]float64{AttrIntrigue: 4, AttrProwess: 2}}

	huntHostSuccess  = Delta{Renown: 0.3, Stats: Stats{Prowess: 0.2, Martial: 0.2}}
	huntHostFailure  = Delta{Renown: -0.1}
	huntHostGlory    = Delta{Renown: 0.5}
	huntHostSaddle   = Delta{Influence: 1.5}
	huntGuestGlory   = Delta{Renown: 0.3, Stats: Stats{Prowess: 0.1}}
	huntGuestFriends = Delta{Influence: 1}

	accidentRenown     = 0.3 // the victim, thrown in front of everyone
	accidentCaught     = 0.5 // the culprit, once named
	accidentCaughtOpin = -2.0

	huntHawkOpinion         = 3.0
	huntConservativeOpinion = 2.0
)

// HuntChance exposes the host's odds before events. Not shown to players.
func HuntChance(host Sheet) int { return huntCheck.Chance(host, 0) }

// ---- events ----

var HuntEvents = []PartyEvent{
	{
		Key: "hunt_boar", Title: "A boar charges",
		Prompt: "A huge boar bursts from the bracken, straight at your horse.",
		Choices: []EventChoice{
			{Key: "spear", Label: "Stand and take it on the spear", Check: chk(20, AttrProwess, 7),
				Success: EventOutcome{Text: "The spear holds. The boar is yours, and everyone saw it.", Quality: 10, Delta: Delta{Renown: 0.3}},
				Failure: EventOutcome{Text: "The boar breaks the spear and gores your leg.", Quality: -6, Condition: CondWounded}},
			{Key: "tree", Label: "Get out of its way",
				Success: EventOutcome{Text: "You scramble clear. The boar escapes, and so does your dignity.", Quality: -5}},
		},
		Default: "tree",
	},
	{
		Key: "hunt_stag", Title: "The white stag",
		Prompt: "Through the trees: a white stag, the kind songs are written about. It's already running.",
		Choices: []EventChoice{
			{Key: "chase", Label: "Give chase", Check: chk(15, AttrProwess, 6),
				Success: EventOutcome{Text: "A long ride and a clean shot. The court will talk of nothing else.", Quality: 14, Delta: Delta{Renown: 0.5}},
				Failure: EventOutcome{Text: "Your horse misses a jump. You land hard, and the stag is gone.", Quality: -4, Condition: CondWounded}},
			{Key: "let_go", Label: "Let it go",
				Success: EventOutcome{Text: "Some things are better left as stories.", Quality: 0}},
		},
		Default: "let_go",
	},
	{
		Key: "hunt_lost", Title: "Lost in the deep wood",
		Prompt: "The trails all look the same, the light is going, and the party is strung out behind you.",
		Choices: []EventChoice{
			{Key: "lead", Label: "Take command and find the way", Check: chk(25, AttrMartial, 6),
				Success: EventOutcome{Text: "You rally the riders and lead them out in good order.", Quality: 6},
				Failure: EventOutcome{Text: "Hours of wrong turns. Everyone is cold and cross.", Quality: -8}},
			{Key: "map", Label: "Check the route you planned", Check: chk(25, AttrStewardship, 6),
				Success: EventOutcome{Text: "You planned for this. Back on the trail in minutes.", Quality: 5},
				Failure: EventOutcome{Text: "Your map is for a different forest, apparently.", Quality: -8}},
		},
		Default: "lead",
	},
	{
		Key: "hunt_poacher", Title: "A poacher caught",
		Prompt: "Your huntsmen drag a ragged man out of the trees with a royal deer over his shoulders.",
		Choices: []EventChoice{
			{Key: "hang", Label: "Hang him from an oak",
				Success: EventOutcome{Text: "The forest law is kept. Some of your guests look away.", Quality: -2, Delta: Delta{Renown: 0.2}}},
			{Key: "free", Label: "Let him go",
				Success: EventOutcome{Text: "He kisses your boots and vanishes. The common folk will hear of it.", Quality: 2, Delta: Delta{Influence: 0.3}}},
			{Key: "hire", Label: "Hire him as a tracker", Check: chk(25, AttrStewardship, 5),
				Success: EventOutcome{Text: "He knows every trail. The hunt goes better for it.", Quality: 8, Delta: Delta{Wealth: -5}},
				Failure: EventOutcome{Text: "He leads you in circles, then runs off with your purse.", Quality: -4, Delta: Delta{Wealth: -15}}},
		},
		Default: "free",
	},
	{
		Key: "hunt_storm", Title: "A storm rolls in",
		Prompt: "Thunder in the hills, and the first fat drops of rain.",
		Choices: []EventChoice{
			{Key: "press_on", Label: "Press on", Check: chk(20, AttrMartial, 6),
				Success: EventOutcome{Text: "Soaked, but the game runs in the rain too. A good day's sport.", Quality: 6},
				Failure: EventOutcome{Text: "A miserable, muddy failure of an afternoon.", Quality: -8}},
			{Key: "shelter", Label: "Shelter at a lodge (10 gold)",
				Success: EventOutcome{Text: "Wine by the fire until it passes. Pleasant, if short on hunting.", Quality: 1, Delta: Delta{Wealth: -10}}},
		},
		Default: "shelter",
	},
	{
		Key: "hunt_hounds", Title: "The hounds run off",
		Prompt: "Your pack catches a scent and is gone, baying, over the next hill.",
		Choices: []EventChoice{
			{Key: "rally", Label: "Ride after them and whip them in", Check: chk(20, AttrMartial, 6),
				Success: EventOutcome{Text: "You bring them back to heel, and they lead you to a fine stag.", Quality: 8},
				Failure: EventOutcome{Text: "They're three valleys away by now.", Quality: -8}},
			{Key: "buy", Label: "Borrow a forester's pack (15 gold)",
				Success: EventOutcome{Text: "Not your dogs, but they'll do.", Quality: 3, Delta: Delta{Wealth: -15}}},
		},
		Default: "buy",
	},
}

// ---- resolution ----

type HuntRider struct {
	Person
	Goal   Goal
	Target *Person // accident only
}

type HuntInput struct {
	Host     Person
	Goal     Goal
	Quality  float64 // sum of event outcomes
	Riders   []HuntRider
	Senators []Senator
}

// ResolveHunt decides everything that happens when a hunt ends. All rolls
// are hidden. Victims of an accident only hear who did it if they noticed.
func ResolveHunt(r Roller, in HuntInput) PartyResult {
	res := PartyResult{Deltas: map[int64]Delta{}, Notices: map[int64][]string{}}
	host := in.Host

	success, _ := huntCheck.Roll(r, host.Sheet, in.Quality, Normal)
	res.Success = success
	var hd Delta
	if success {
		hd.Merge(huntHostSuccess)
		switch in.Goal {
		case GoalHuntGlory:
			hd.Merge(huntHostGlory)
		case GoalSaddleTalk:
			hd.Merge(huntHostSaddle)
		}
		res.notice(host.ID, "%s", withEffects("Your hunt was a triumph.", hd))
		res.Public = fmt.Sprintf("**%s**'s hunt rode home with a cart full of game.", host.Name)
		for _, s := range in.Senators {
			var d float64
			if s.Has(TraitHawk) {
				d += huntHawkOpinion
			}
			if s.Has(TraitConservative) {
				d += huntConservativeOpinion
			}
			if d != 0 {
				res.Opinions = append(res.Opinions, OpinionChange{SenatorID: s.ID, CharacterID: host.ID, Delta: d})
			}
		}
	} else {
		hd.Merge(huntHostFailure)
		res.notice(host.ID, "%s", withEffects("Your hunt came home empty-handed.", hd))
		res.Public = fmt.Sprintf("**%s**'s hunt came home with nothing but mud.", host.Name)
	}
	res.delta(host.ID, hd)

	present := map[int64]bool{host.ID: true}
	for _, a := range in.Riders {
		present[a.ID] = true
	}
	mod := guestModBadParty
	if success {
		mod = guestModGoodParty
	}
	for _, a := range in.Riders {
		switch a.Goal {
		case GoalHuntGlory:
			if ok, _ := huntGloryCheck.Roll(r, a.Sheet, mod, Normal); ok {
				res.delta(a.ID, huntGuestGlory)
				res.notice(a.ID, "%s", withEffects(fmt.Sprintf("You rode hard on %s's hunt, and everyone saw it.", host.Name), huntGuestGlory))
			} else {
				res.notice(a.ID, "You spent %s's hunt at the back of the field.", host.Name)
			}
		case GoalRideFriends:
			if ok, _ := huntFriendsCheck.Roll(r, a.Sheet, mod, Normal); ok {
				res.delta(a.ID, huntGuestFriends)
				res.notice(a.ID, "%s", withEffects(fmt.Sprintf("You made friends on %s's hunt.", host.Name), huntGuestFriends))
			} else {
				res.notice(a.ID, "You rode alongside the others on %s's hunt, but nobody had much to say.", host.Name)
			}
		case GoalAccident:
			if a.Target == nil || !present[a.Target.ID] || a.Target.ID == a.ID {
				res.notice(a.ID, "Your mark never rode out on %s's hunt. The accident will have to wait.", host.Name)
				continue
			}
			resolveAccident(r, &res, host, a)
		}
	}
	return res
}

func resolveAccident(r Roller, res *PartyResult, host Person, a HuntRider) {
	t := *a.Target
	landed, _ := accidentCheck.Roll(r, a.Sheet, 0, Normal)

	tSaw, tm := targetNotice.Roll(r, t.Sheet, t.Effects.RumorNotice, Disadvantage)
	hSaw, hm := false, 0
	if t.ID != host.ID {
		hSaw, hm = hostNotice.Roll(r, host.Sheet, host.Effects.RumorNotice, Disadvantage)
	}
	cancelled := (tSaw && tm >= noticeCancel) || (hSaw && hm >= noticeCancel)
	named := (tSaw && tm >= noticeNamed) || (hSaw && hm >= noticeNamed)
	hurt := landed && !cancelled

	if tSaw {
		switch {
		case tm >= noticeCancel:
			res.notice(t.ID, "On %s's hunt, you caught %s loosening your saddle girth. You fixed it, and they know you saw.", host.Name, a.Ref())
		case tm >= noticeNamed:
			res.notice(t.ID, "On %s's hunt, %s arranged an accident for you.%s", host.Name, a.Ref(), accidentSuffix(landed, hurt))
		default:
			res.notice(t.ID, "On %s's hunt, someone arranged an accident for you.%s", host.Name, accidentSuffix(landed, hurt))
		}
	} else if hurt {
		res.notice(t.ID, "On %s's hunt, your horse threw you. A bad fall, and a wound to show for it. (-%s renown)", host.Name, Num(accidentRenown))
	}
	if hSaw {
		switch {
		case hm >= noticeCancel:
			res.notice(host.ID, "On your hunt, %s tried to arrange an accident for %s. You stopped it.", a.Ref(), t.Ref())
		case hm >= noticeNamed:
			res.notice(host.ID, "On your hunt, %s arranged an accident for %s.", a.Ref(), t.Ref())
		default:
			res.notice(host.ID, "Something about the fall on your hunt didn't look like an accident.")
		}
	}

	switch {
	case !landed:
		res.notice(a.ID, "You tried to arrange an accident for %s on %s's hunt, but the moment never came.", t.Name, host.Name)
	case cancelled:
		res.notice(a.ID, "Your accident for %s was spotted and undone before it happened.", t.Name)
	default:
		res.delta(t.ID, Delta{Renown: -accidentRenown})
		res.Conditions = append(res.Conditions, ConditionChange{CharacterID: t.ID, Key: CondWounded})
		if named {
			res.notice(a.ID, "On %s's hunt, %s took a bad fall.", host.Name, t.Name)
		} else {
			res.notice(a.ID, "On %s's hunt, %s took a bad fall. No one will ever prove it was you.", host.Name, t.Name)
		}
	}
	if named {
		res.delta(a.ID, Delta{Renown: -accidentCaught})
		res.Opinions = append(res.Opinions, OpinionChange{CharacterID: a.ID, Delta: accidentCaughtOpin})
		res.notice(a.ID, "You were seen. Word of what you tried is getting around. (-%s renown, and the Senate thinks less of you)", Num(accidentCaught))
	}
}

func accidentSuffix(landed, hurt bool) string {
	switch {
	case hurt:
		return fmt.Sprintf(" You took a bad fall, and a wound. (-%s renown)", Num(accidentRenown))
	case landed:
		return " Someone stopped it in time."
	default:
		return " It came to nothing."
	}
}
