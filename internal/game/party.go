package game

import (
	"fmt"
	"math"
	"time"
)

// KindParty is the activity kind for Throw a Party.
const KindParty = "party"

// The host chooses how much gold to spend. Every doubling over the minimum
// adds the same success bonus and reward multiplier, so the first extra gold
// helps most. Placeholders to tune.
const (
	PartyMinSpend = 30
	PartyMaxSpend = 500

	partySpendBonus  = 8.0 // success percent per doubling of spend
	partySpendReward = 0.5 // extra reward multiplier per doubling of spend
)

// PartyBudget is a preset spend shown as a button.
type PartyBudget struct {
	Name string // also used in the public announcement ("a lavish party")
	Gold int
}

var PartyBudgets = []PartyBudget{
	{"Modest", 30},
	{"Fine", 75},
	{"Lavish", 150},
	{"Extravagant", 300},
}

// PartyBudgetName is the name of the largest preset the spend reaches.
func PartyBudgetName(gold int) string {
	name := PartyBudgets[0].Name
	for _, b := range PartyBudgets {
		if gold >= b.Gold {
			name = b.Name
		}
	}
	return name
}

func spendDoublings(gold int) float64 {
	g := min(max(gold, PartyMinSpend), PartyMaxSpend)
	return math.Log2(float64(g) / PartyMinSpend)
}

// PartySpendBonus is the success bonus, in percent, from the host's spend.
func PartySpendBonus(gold int) float64 { return partySpendBonus * spendDoublings(gold) }

// PartyRewardScale multiplies what the host gains from a successful party.
func PartyRewardScale(gold int) float64 { return 1 + partySpendReward*spendDoublings(gold) }

// PartyTiming controls how long a party runs and when its events fire.
type PartyTiming struct {
	Duration      time.Duration // whole party
	EventEarliest time.Duration // first moment an event can fire
	EventLatest   time.Duration // last moment an event can fire
	AnswerWindow  time.Duration // how long the host has to answer an event
}

var (
	DefaultPartyTiming = PartyTiming{Duration: 2 * time.Hour, EventEarliest: 15 * time.Minute, EventLatest: 100 * time.Minute, AnswerWindow: 15 * time.Minute}
	// FastPartyTiming is for manual testing.
	FastPartyTiming = PartyTiming{Duration: 4 * time.Minute, EventEarliest: 30 * time.Second, EventLatest: 150 * time.Second, AnswerWindow: 60 * time.Second}
)

// Goal is what a host or attendee wants out of a party.
type Goal string

const (
	GoalMakeFriends  Goal = "make_friends"
	GoalTalkBusiness Goal = "talk_business"
	GoalSpreadRumors Goal = "spread_rumors"
)

type GoalInfo struct {
	Key         Goal
	Name        string
	Description string
}

var (
	HostGoals = []GoalInfo{
		{GoalMakeFriends, "Make Friends", "A successful party grows your influence even more."},
		{GoalTalkBusiness, "Talk Business", "Work the room for deals. Can raise your stewardship and daily income."},
	}
	AttendeeGoals = []GoalInfo{
		{GoalMakeFriends, "Make Friends", "Mingle and win allies. Grows your influence."},
		{GoalTalkBusiness, "Talk Business", "Strike deals with the other guests. Can raise your stewardship and daily income."},
		{GoalSpreadRumors, "Spread Rumors", "Whisper about another player. Costs them influence if it works."},
	}
)

func GoalName(g Goal) string {
	for _, gi := range AttendeeGoals {
		if gi.Key == g {
			return gi.Name
		}
	}
	return string(g)
}

// ---- checks and numbers (all placeholders to tune) ----

var (
	// Host success: influence > estate > diplomacy > renown.
	partyCheck = Check{Base: 5, Weights: map[Attr]float64{AttrInfluence: 2.0, AttrEstate: 1.5, AttrDiplomacy: 1.0, AttrRenown: 0.9}}

	businessCheck = Check{Base: 20, Weights: map[Attr]float64{AttrStewardship: 6}}
	friendsCheck  = Check{Base: 20, Weights: map[Attr]float64{AttrDiplomacy: 5, AttrInfluence: 1}}
	rumorCheck    = Check{Base: 15, Weights: map[Attr]float64{AttrIntrigue: 6}}
	targetNotice  = Check{Base: 10, Weights: map[Attr]float64{AttrIntrigue: 5}}                 // rolled with disadvantage
	hostNotice    = Check{Base: 10, Weights: map[Attr]float64{AttrIntrigue: 5}, Difficulty: 20} // also disadvantage, and harder

	hostSuccess   = Delta{Influence: 2, Renown: 1, Stats: Stats{Diplomacy: 1.5}}
	hostFailure   = Delta{Renown: -2}
	hostFriends   = Delta{Influence: 2}
	hostBusiness  = Delta{DailyIncome: 2, Stats: Stats{Stewardship: 2.5}}
	guestFriends  = Delta{Influence: 1}
	guestBusiness = Delta{DailyIncome: 1, Stats: Stats{Stewardship: 1.25}}
	rumorDamage   = 2.0 // influence lost by the target

	// Guests do better at a good party.
	guestModGoodParty = 15.0
	guestModBadParty  = -10.0
)

// PartyChance exposes the host's odds at a given spend (before events). Not
// shown to players; useful for tests and tuning.
func PartyChance(host Sheet, spend int) int { return partyCheck.Chance(host, PartySpendBonus(spend)) }

// Notice tiers for spotting rumors. Margin thresholds.
const (
	noticeNamed  = 25 // learn who
	noticeCancel = 50 // learn who and stop it
)

// ---- events ----

// EventOutcome is the result of the host's choice in a party event.
// Quality adds directly to the party's success chance.
type EventOutcome struct {
	Text      string  `json:"text"`
	Quality   float64 `json:"quality"`
	Delta     Delta   `json:"delta"`
	Condition string  `json:"condition,omitempty"` // given to whoever the event happened to
	Ends      bool    `json:"ends,omitempty"`      // ends the happening the event belongs to
}

// EventChoice is one button on an event. With no Check it always gives Success.
type EventChoice struct {
	Key     string
	Label   string
	Check   *Check
	Success EventOutcome
	Failure EventOutcome
}

type PartyEvent struct {
	Key     string
	Roles   []string // empty = can happen to any host
	Title   string
	Prompt  string
	Choices []EventChoice
	Default string // chosen if the host doesn't answer in time
}

func chk(base float64, a Attr, w float64) *Check {
	return &Check{Base: base, Weights: map[Attr]float64{a: w}}
}

var PartyEvents = []PartyEvent{
	{
		Key: "wine", Title: "The wine is running out",
		Prompt: "Your steward whispers that the cellar is nearly dry, and the night is young.",
		Choices: []EventChoice{
			{Key: "buy", Label: "Send for more (15 gold)",
				Success: EventOutcome{Text: "Fresh casks arrive before anyone notices.", Quality: 8, Delta: Delta{Wealth: -15}}},
			{Key: "water", Label: "Water it down", Check: chk(25, AttrIntrigue, 6),
				Success: EventOutcome{Text: "No one can tell the difference.", Quality: 4},
				Failure: EventOutcome{Text: "A guest spits out their cup. Word spreads.", Quality: -10, Delta: Delta{Renown: -2}}},
			{Key: "end", Label: "Call it a night",
				Success: EventOutcome{Text: "The party ends early. Guests leave disappointed.", Quality: -12}},
		},
		Default: "end",
	},
	{
		Key: "duel", Title: "Swords are drawn",
		Prompt: "Two guests have come to blows over an insult and now stand with blades out in your hall.",
		Choices: []EventChoice{
			{Key: "step_in", Label: "Step between them", Check: chk(20, AttrProwess, 7),
				Success: EventOutcome{Text: "You disarm one with a flourish. The court applauds.", Quality: 8, Delta: Delta{Renown: 3}},
				Failure: EventOutcome{Text: "You take a cut to the arm and the duel goes on anyway.", Quality: -6}},
			{Key: "let_them", Label: "Let them settle it",
				Success: EventOutcome{Text: "Blood on your floor. People will talk about this party, for better or worse.", Quality: -4, Delta: Delta{Renown: 2}}},
		},
		Default: "let_them",
	},
	{
		Key: "uninvited", Title: "An uninvited count",
		Prompt: "Count Dracula, whom you did not invite, stands at your door in the dark. He will not cross the threshold until you ask him in.",
		Choices: []EventChoice{
			{Key: "welcome", Label: "Invite him in", Check: chk(25, AttrDiplomacy, 6),
				Success: EventOutcome{Text: "The Count is charmed, drinks nothing, and owes you for the courtesy.", Quality: 5, Delta: Delta{Influence: 1}},
				Failure: EventOutcome{Text: "He spends the night criticizing your hospitality, and two guests leave looking pale.", Quality: -6}},
			{Key: "turn_away", Label: "Leave him on the step",
				Success: EventOutcome{Text: "Your guests are impressed by your nerve. The Count is not.", Quality: 2, Delta: Delta{Influence: -0.5}}},
		},
		Default: "welcome",
	},
	{
		Key: "musicians", Title: "Dandelion wants more",
		Prompt: "Mid-song, the famous bard Dandelion and his troupe stop playing and demand double their fee.",
		Choices: []EventChoice{
			{Key: "pay", Label: "Pay them (20 gold)",
				Success: EventOutcome{Text: "The music swells again.", Quality: 5, Delta: Delta{Wealth: -20}}},
			{Key: "haggle", Label: "Talk them down", Check: chk(20, AttrDiplomacy, 6),
				Success: EventOutcome{Text: "They settle for a little extra and play their best.", Quality: 5, Delta: Delta{Wealth: -5}},
				Failure: EventOutcome{Text: "Dandelion packs up his lute and leaves, composing an unkind ballad about you. The hall goes quiet.", Quality: -10}},
		},
		Default: "haggle",
	},
	{
		Key: "insult", Title: "A drunk guest insults you",
		Prompt: "Loudly, in front of everyone, a guest mocks your family name.",
		Choices: []EventChoice{
			{Key: "wit", Label: "Answer with wit", Check: chk(20, AttrDiplomacy, 7),
				Success: EventOutcome{Text: "Your reply has the whole hall laughing at them instead.", Quality: 7, Delta: Delta{Renown: 2}},
				Failure: EventOutcome{Text: "Your comeback falls flat.", Quality: -5}},
			{Key: "guards", Label: "Have them thrown out", Check: chk(30, AttrMartial, 6),
				Success: EventOutcome{Text: "Your guards remove them quickly and quietly.", Quality: 2},
				Failure: EventOutcome{Text: "The scuffle knocks over a table of food.", Quality: -7}},
		},
		Default: "guards",
	},
	{
		Key: "fire", Title: "Fire in the kitchen",
		Prompt: "Smoke is pouring out of the kitchens.",
		Choices: []EventChoice{
			{Key: "take_charge", Label: "Take charge of it", Check: chk(25, AttrStewardship, 6),
				Success: EventOutcome{Text: "Buckets, servants, done. The roast is only a little charred.", Quality: 3},
				Failure: EventOutcome{Text: "The kitchen is ruined, and so is dinner.", Quality: -10, Delta: Delta{Wealth: -15}}},
			{Key: "distract", Label: "Keep the guests distracted", Check: chk(25, AttrDiplomacy, 6),
				Success: EventOutcome{Text: "Nobody even smelled the smoke.", Quality: 2, Delta: Delta{Wealth: -10}},
				Failure: EventOutcome{Text: "Guests flee the smoke into the garden.", Quality: -8, Delta: Delta{Wealth: -10}}},
		},
		Default: "distract",
	},
}

// Choice looks up a choice by key.
func (e PartyEvent) Choice(key string) (EventChoice, bool) {
	for _, c := range e.Choices {
		if c.Key == key {
			return c, true
		}
	}
	return EventChoice{}, false
}

// PartyEventsFor lists the events that can happen at a party hosted by role.
func PartyEventsFor(role string) []PartyEvent {
	var out []PartyEvent
	for _, e := range PartyEvents {
		if forRole(e.Roles, role) {
			out = append(out, e)
		}
	}
	return out
}

func forRole(roles []string, role string) bool {
	if len(roles) == 0 {
		return true
	}
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

func PartyEventByKey(key string) (PartyEvent, bool) {
	for _, e := range PartyEvents {
		if e.Key == key {
			return e, true
		}
	}
	return PartyEvent{}, false
}

// EventByKey finds an event in any activity's catalogue: parties, hunts
// and duels share the same DM event machinery.
func EventByKey(key string) (PartyEvent, bool) {
	for _, list := range [][]PartyEvent{PartyEvents, HuntEvents, DuelEvents, JourneyEvents, JourneyAbroadEvents, happeningEvents} {
		for _, e := range list {
			if e.Key == key {
				return e, true
			}
		}
	}
	return PartyEvent{}, false
}

// ResolveEventChoice rolls the host's choice. Unknown choices fall back to
// the event's default.
func ResolveEventChoice(r Roller, ev PartyEvent, choiceKey string, host Sheet) (EventChoice, EventOutcome) {
	choice, ok := ev.Choice(choiceKey)
	if !ok {
		choice, _ = ev.Choice(ev.Default)
	}
	if choice.Check == nil {
		return choice, choice.Success
	}
	if ok, _ := choice.Check.Roll(r, host, 0, Normal); ok {
		return choice, choice.Success
	}
	return choice, choice.Failure
}

// ---- resolution ----

// Person is a character as the rules see it.
type Person struct {
	ID      int64
	UserID  string
	Name    string
	Sheet   Sheet
	Effects Effects // from estate items
}

// Ref is how a person is named in notices.
func (p Person) Ref() string { return fmt.Sprintf("%s (<@%s>)", p.Name, p.UserID) }

type Attendee struct {
	Person
	Goal   Goal
	Target *Person // spread_rumors only
}

type PartyInput struct {
	Host      Person
	Goal      Goal
	Spend     int     // gold the host put into the party
	Quality   float64 // sum of event outcomes
	Attendees []Attendee
	Laws      Laws
}

type PartyResult struct {
	Success bool
	Ledger
	Public string // announced to the court
	// Opinions are changes with senators (SenatorID 0 = every senator).
	Conditions []ConditionChange // e.g. wounds from a hunting accident
}

// Ledger is what an action does to the players: their deltas (which may
// include losses they never hear about), private notices, senators'
// opinions and conditions.
type Ledger struct {
	Deltas     map[int64]Delta
	Notices    map[int64][]string
	Opinions   []OpinionChange
	Conditions []ConditionChange // e.g. wounds from a hunting accident
}

func newLedger() Ledger {
	return Ledger{Deltas: map[int64]Delta{}, Notices: map[int64][]string{}}
}

func (l *Ledger) delta(id int64, d Delta) { addDelta(l.Deltas, id, d) }

func (l *Ledger) notice(id int64, format string, args ...any) {
	l.Notices[id] = append(l.Notices[id], fmt.Sprintf(format, args...))
}

// addDelta merges d into a character's delta.
func addDelta(m map[int64]Delta, id int64, d Delta) {
	cur := m[id]
	cur.Merge(d)
	m[id] = cur
}

func withEffects(text string, d Delta) string {
	if s := d.Describe(); s != "" {
		return text + " (" + s + ")"
	}
	return text
}

// ResolveParty decides everything that happens when a party ends. All rolls
// are hidden; only notices and the public line reach players. Players only
// hear about rumors against them if they noticed them.
func ResolveParty(r Roller, in PartyInput) PartyResult {
	res := PartyResult{Ledger: newLedger()}
	host := in.Host

	// Host.
	success, _ := partyCheck.Roll(r, host.Sheet, in.Quality+host.Effects.PartyBonus+PartySpendBonus(in.Spend), Normal)
	res.Success = success
	var hd Delta
	if success {
		hd.Merge(hostSuccess)
		switch in.Goal {
		case GoalMakeFriends:
			hd.Merge(hostFriends)
		case GoalTalkBusiness:
			if ok, _ := businessCheck.Roll(r, host.Sheet, 0, Normal); ok {
				hd.Merge(hostBusiness)
			}
		}
		hd = ScaleGain(hd.Scale(PartyRewardScale(in.Spend)), host.Sheet[AttrInfluence])
		res.notice(host.ID, "%s", withEffects("Your party was a success.", hd))
		res.Public = fmt.Sprintf("**%s**'s party was the talk of the court.", host.Name)
		res.Opinions = append(res.Opinions, OpinionChange{CharacterID: host.ID, Delta: OpinionPartySuccess})
		res.notice(host.ID, "The Senate heard about it. Every senator thinks a little better of you (%s).", OpinionNum(OpinionPartySuccess))
	} else {
		hd.Merge(hostFailure)
		res.notice(host.ID, "%s", withEffects("Your party fell flat.", hd))
		res.Public = fmt.Sprintf("**%s**'s party was a forgettable affair.", host.Name)
		res.Opinions = append(res.Opinions, OpinionChange{CharacterID: host.ID, Delta: OpinionPartyFailure})
		res.notice(host.ID, "Word reached the Senate. Every senator thinks a little less of you (%s).", OpinionNum(OpinionPartyFailure))
	}
	res.delta(host.ID, hd)

	// Guests.
	mod := guestModBadParty
	if success {
		mod = guestModGoodParty
	}
	for _, a := range in.Attendees {
		switch a.Goal {
		case GoalMakeFriends:
			if ok, _ := friendsCheck.Roll(r, a.Sheet, mod, Normal); ok {
				g := ScaleGain(guestFriends, a.Sheet[AttrInfluence])
				res.delta(a.ID, g)
				res.notice(a.ID, "%s", withEffects(fmt.Sprintf("You made friends at %s's party.", host.Name), g))
			} else {
				res.notice(a.ID, "You mingled at %s's party, but made no new friends.", host.Name)
			}
		case GoalTalkBusiness:
			if ok, _ := businessCheck.Roll(r, a.Sheet, mod, Normal); ok {
				res.delta(a.ID, guestBusiness)
				res.notice(a.ID, "%s", withEffects(fmt.Sprintf("You struck a deal at %s's party.", host.Name), guestBusiness))
			} else {
				res.notice(a.ID, "No one at %s's party wanted to do business.", host.Name)
			}
		case GoalSpreadRumors:
			if a.Target != nil {
				resolveRumor(r, &res, host, a, in.Laws)
			}
		}
	}
	return res
}

func resolveRumor(r Roller, res *PartyResult, host Person, a Attendee, ls Laws) {
	t := *a.Target
	landed, _ := rumorCheck.Roll(r, a.Sheet, 0, Normal)

	// Target's own (disadvantaged) chance to notice, then the host's harder
	// one. If the target is the host, their own roll covers it. The Law of
	// Slander makes everyone readier to notice.
	rules := ls.Rules()
	slander := rules.RumorNotice
	tSaw, tm := targetNotice.Roll(r, t.Sheet, t.Effects.RumorNotice+slander, Disadvantage)
	hSaw, hm := false, 0
	if t.ID != host.ID {
		hSaw, hm = hostNotice.Roll(r, host.Sheet, host.Effects.RumorNotice+slander, Disadvantage)
	}
	// Under the Law of Slander, a rumor-monger who is named pays their victim.
	if fine := rules.SlanderFine; fine != 0 && ((tSaw && tm >= noticeNamed) || (hSaw && hm >= noticeNamed)) {
		res.delta(a.ID, Delta{Wealth: -fine})
		res.delta(t.ID, Delta{Wealth: fine})
		res.notice(a.ID, "You were named as a slanderer at %s's party, and the law makes you pay %s. (-%d gold)", host.Name, t.Name, fine)
		res.notice(t.ID, "Under the Law of Slander, %s must pay you for their lies. (+%d gold)", a.Name, fine)
	}
	cancelled := (tSaw && tm >= noticeCancel) || (hSaw && hm >= noticeCancel)
	stuck := landed && !cancelled

	if tSaw {
		switch {
		case tm >= noticeCancel:
			res.notice(t.ID, "At %s's party, %s was spreading rumors about you. You set the record straight before anyone believed them.", host.Name, a.Ref())
		case tm >= noticeNamed:
			res.notice(t.ID, "At %s's party, %s was spreading rumors about you.%s", host.Name, a.Ref(), rumorSuffix(landed, stuck))
		default:
			res.notice(t.ID, "At %s's party, someone was spreading rumors about you.%s", host.Name, rumorSuffix(landed, stuck))
		}
	}
	if hSaw {
		switch {
		case hm >= noticeCancel:
			res.notice(host.ID, "%s was spreading rumors about %s at your party. You put a stop to it.", a.Ref(), t.Ref())
		case hm >= noticeNamed:
			res.notice(host.ID, "%s was spreading rumors about %s at your party.", a.Ref(), t.Ref())
		default:
			res.notice(host.ID, "Someone at your party was spreading rumors.")
		}
	}

	switch {
	case !landed:
		res.notice(a.ID, "Your whispers about %s at %s's party didn't catch on.", t.Name, host.Name)
	case cancelled:
		res.notice(a.ID, "Your whispers about %s were stamped out before they spread.", t.Name)
	default:
		res.delta(t.ID, Delta{Influence: -rumorDamage})
		res.Opinions = append(res.Opinions, OpinionChange{CharacterID: t.ID, Delta: OpinionRumored})
		res.notice(a.ID, "Your whispers about %s spread through %s's party.", t.Name, host.Name)
	}
}

func rumorSuffix(landed, stuck bool) string {
	switch {
	case stuck:
		return fmt.Sprintf(" Some of it stuck. (-%s influence)", Num(rumorDamage))
	case landed:
		return " Someone put a stop to it before it spread."
	default:
		return " No one seemed to believe it."
	}
}
