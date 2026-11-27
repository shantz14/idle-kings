package game

import (
	"fmt"
	"math"
	"time"
)

// ---- Conspiracies (Intrigue) ----
//
// A player picks a scheme and a target, and invites other players and
// senators to join. The plot runs for some game days without taking anyone's
// action: each day it ripens (faster with more power behind it) and risks
// being found out (likelier with more mouths to keep shut). The leader
// strikes when they dare, with a one-hour action; striking early is riskier.
// All numbers are placeholders (see the design note's suggestions).

const (
	KindConspiracy = "conspiracy" // the plot itself: free, lasts ConspiracyDays
	KindStrike     = "strike"     // the leader's last move: takes the action
)

const (
	DefaultStrikeDuration = time.Hour
	FastStrikeDuration    = time.Minute
)

// ConspiracyDays is how many game days a plot holds together.
const ConspiracyDays = 5

// TargetKind is what a scheme is aimed at.
type TargetKind string

const (
	TargetPlayer  TargetKind = "player"
	TargetSenator TargetKind = "senator"
)

const (
	SchemeAssassinate = "assassinate"
	SchemeFrame       = "frame"
	SchemeSteal       = "steal"
	SchemeDisgrace    = "disgrace"
)

// SchemeDef is one kind of conspiracy.
type SchemeDef struct {
	Key     string
	Name    string
	Emoji   string
	Desc    string
	Targets []TargetKind
	Stat    Stat    // the second stat it uses, besides Intrigue
	Base    float64 // strike chance before power and defence, in percent
	// Crime is the secret kind of the deed itself, once done (a misdeed).
	Crime string
	// Pious and Dove senators balk at murder.
	Bloody bool
}

var SchemeDefs = []SchemeDef{
	{Key: SchemeAssassinate, Name: "Assassinate", Emoji: "🗡️",
		Desc:    "Have a rival or a senator killed.",
		Targets: []TargetKind{TargetPlayer, TargetSenator}, Stat: Prowess, Base: 10, Crime: SecretKindMurder, Bloody: true},
	{Key: SchemeFrame, Name: "Frame a Rival", Emoji: "🖋️",
		Desc:    "Forge the evidence of a crime they never did. It goes in your dossier, as good as real, to denounce when it suits you.",
		Targets: []TargetKind{TargetPlayer}, Stat: Diplomacy, Base: 30, Crime: SecretKindForgery},
	{Key: SchemeSteal, Name: "Steal a Treasure", Emoji: "💰",
		Desc:    "Break into their house for one of their treasures, or for the gold in their coffers. Houses and lands can't be carried off.",
		Targets: []TargetKind{TargetPlayer}, Stat: Stewardship, Base: 25, Crime: SecretKindTheft},
	{Key: SchemeDisgrace, Name: "Disgrace a Senator", Emoji: "📜",
		Desc:    "Bury a senator in scandal until the Senate strips them of their seat, and someone new sits in it.",
		Targets: []TargetKind{TargetSenator}, Stat: Diplomacy, Base: 25, Crime: SecretKindConspiracy},
}

func SchemeByKey(k string) (SchemeDef, bool) { return schemeIndex.get(k) }

// Aims reports whether the scheme can be aimed at that kind of target.
func (s SchemeDef) Aims(k TargetKind) bool {
	for _, t := range s.Targets {
		if t == k {
			return true
		}
	}
	return false
}

// StealCoffers is the "item" key for stealing gold instead of a treasure.
const (
	StealCofferShare = 0.3
	StealCofferMax   = 400
)

// CofferTake is how much gold a theft from someone's coffers takes.
func CofferTake(wealth int) int {
	return min(StealCofferMax, int(float64(wealth)*StealCofferShare))
}

// ConspiracyPurse is gold spent on cutthroats, forgers and burglars.
type ConspiracyPurse struct {
	Key   string
	Name  string
	Gold  int
	Power float64
}

var ConspiracyPurses = []ConspiracyPurse{
	{"none", "No gold", 0, 0},
	{"some", "Hired hands", 50, 5},
	{"many", "A small army of them", 150, 10},
	{"best", "The best money can buy", 400, 16},
}

func ConspiracyPurseByKey(k string) (ConspiracyPurse, bool) {
	for _, p := range ConspiracyPurses {
		if p.Key == k {
			return p, true
		}
	}
	return ConspiracyPurse{}, false
}

// ---- power ----

const (
	powerIntrigue     = 3.0  // per point of Intrigue
	powerStat         = 2.0  // per point of the scheme's other stat
	powerMember       = 0.7  // members count for less than the leader
	powerPerInfluence = 0.75 // a senator's weight in the plot
)

// Plotter is a player in a conspiracy, as the rules see them.
type Plotter struct {
	Person
	Spies float64 // the target's spy level on them (players only)
}

// Plot is a conspiracy as the rules see it.
type Plot struct {
	Scheme   SchemeDef
	Leader   Plotter
	Members  []Plotter // other players who joined
	Senators []Senator // senators who joined
	Purse    ConspiracyPurse
	Progress float64 // 0–100
	Warned   Discovery
}

func plotterPower(p Person, s SchemeDef) float64 {
	return powerIntrigue*p.Sheet[AttrIntrigue] + powerStat*p.Sheet[Attr(s.Stat)]
}

// Power is how much weight is behind the plot: members' skills, senators'
// influence and the purse.
func (p Plot) Power() float64 {
	pow := plotterPower(p.Leader.Person, p.Scheme) + p.Purse.Power
	for _, m := range p.Members {
		pow += powerMember * plotterPower(m.Person, p.Scheme)
	}
	for _, s := range p.Senators {
		pow += powerPerInfluence * s.Influence
	}
	return pow
}

// ---- ripening ----

const (
	progressBase     = 15.0
	progressPerPower = 0.5
	ProgressMax      = 100.0
)

// DailyProgress is how much a plot ripens in one game day.
func (p Plot) DailyProgress() float64 { return progressBase + progressPerPower*p.Power() }

// ProgressStage is how ripe a plot is, in words.
func ProgressStage(v float64) string {
	switch {
	case v >= ProgressMax:
		return "Ready"
	case v >= 65:
		return "Ripe"
	case v >= 30:
		return "Taking shape"
	}
	return "Barely begun"
}

// PowerWord is how strong a plot is, in words.
func PowerWord(v float64) string {
	switch {
	case v >= 60:
		return "A web through the whole city"
	case v >= 40:
		return "A dangerous cabal"
	case v >= 25:
		return "A handful of sworn friends"
	}
	return "Whispers between few"
}

// ---- discovery ----

// Discovery is how much the target knows of a plot against them.
type Discovery int

const (
	Unsuspected  Discovery = iota
	Suspected              // they know someone plots against them
	MemberNamed            // they know one of the plotters
	LeaderNamed            // they know who leads it, and it falls apart
	discoveryTop = LeaderNamed
)

const (
	discoverBase       = 6.0
	discoverPerMember  = 4.0 // each player or senator in on it, besides the leader
	discoverIntrigue   = 4.0 // per point of the target's Intrigue
	discoverLeader     = 3.0 // per point of the leader's Intrigue, against
	discoverSpyShare   = 0.25
	discoverMin        = 2
	discoverMax        = 60
	discoverMemberRoll = 60 // d100 at or above: a member is named
	discoverLeaderRoll = 90 // at or above: the leader is named
)

// Defender is who the plot is aimed at.
type Defender struct {
	Player  *Person  // nil for a senator
	Senator *Senator // nil for a player
}

// Name is how the target is named.
func (d Defender) Name() string {
	if d.Senator != nil {
		return "Senator " + d.Senator.Name
	}
	if d.Player != nil {
		return d.Player.Name
	}
	return "someone"
}

// DiscoverChance is the percent chance per game day that the target learns more.
func (p Plot) DiscoverChance(d Defender) int {
	c := discoverBase + discoverPerMember*float64(len(p.Members)+len(p.Senators)) -
		discoverLeader*p.Leader.Sheet[AttrIntrigue]
	if d.Player != nil {
		c += discoverIntrigue*d.Player.Sheet[AttrIntrigue] + d.Player.Effects.RumorNotice
		spy := p.Leader.Spies
		for _, m := range p.Members {
			spy = math.Max(spy, m.Spies)
		}
		c += discoverSpyShare * spy
	} else if d.Senator != nil {
		c += d.Senator.Influence / 2
		if d.Senator.Has(TraitScheming) || d.Senator.Has(TraitStern) {
			c += 6
		}
	}
	return int(math.Max(discoverMin, math.Min(discoverMax, c)))
}

// DayResult is one game day of a plot.
type DayResult struct {
	Progress float64   // the new progress
	Found    Discovery // what the target learned today (Unsuspected = nothing)
	Named    int       // index into Members for MemberNamed; -1 = the leader
}

// StepDay ripens the plot by a day and rolls whether the target learns more.
// A target only ever learns more than they knew.
func StepDay(r Roller, p Plot, d Defender) DayResult {
	res := discover(r, p, p.DiscoverChance(d))
	res.Progress = math.Min(ProgressMax, p.Progress+p.DailyProgress())
	return res
}

// uncoverFactor is how much likelier Root Out Spies is to find a plot than
// a quiet day is.
const uncoverFactor = 2

// Uncover is the target's search of their household (Root Out Spies)
// turning up a plot against them. Progress is unchanged.
func Uncover(r Roller, p Plot, d Defender) DayResult {
	res := discover(r, p, min(MaxChance, uncoverFactor*p.DiscoverChance(d)))
	res.Progress = p.Progress
	return res
}

func discover(r Roller, p Plot, chance int) DayResult {
	res := DayResult{Named: -1}
	if r.IntN(100) >= chance {
		return res
	}
	n := r.IntN(100)
	found := Suspected
	switch {
	case n >= discoverLeaderRoll:
		found = LeaderNamed
	case n >= discoverMemberRoll:
		found = MemberNamed
	}
	if found <= p.Warned {
		found = min(discoveryTop, p.Warned+1)
	}
	if found == MemberNamed && len(p.Members) > 0 {
		res.Named = r.IntN(len(p.Members))
	}
	res.Found = found
	return res
}

// ---- senators joining ----

const (
	joinBase          = 15.0
	joinOpinion       = 0.5 // per point of opinion of the leader
	joinTargetOpinion = 0.4 // per point of opinion of the target player, against
	joinGift          = 20.0
	reportBase        = 10
	reportStern       = 40
	reportIncorrupt   = 60
	reportDislike     = 20 // more, if they dislike the leader
	reportOpinion     = -10.0
)

// JoinGiftCost is the gift that tips a wavering senator.
func JoinGiftCost(s Senator) int { return 3 * GiftCost(s) }

// JoinChance is the percent chance a senator agrees to join. opLeader and
// opTarget are their opinions of the leader and (for a player target) of
// the target.
func JoinChance(s Senator, sc SchemeDef, opLeader, opTarget float64, gift bool) int {
	c := joinBase + joinOpinion*opLeader - joinTargetOpinion*opTarget
	mods := map[Trait]float64{
		TraitScheming: 25, TraitGreedy: 5, TraitRiskTaker: 10, TraitReckless: 10,
		TraitIncorruptible: -50, TraitStern: -30, TraitConservative: -10,
	}
	if sc.Bloody {
		mods[TraitPious], mods[TraitDove] = -20, -15
	}
	for _, t := range s.Traits {
		c += mods[t]
	}
	if gift {
		c += joinGift
		if s.Has(TraitGreedy) {
			c += joinGift / 2
		}
	}
	return int(math.Max(MinChance, math.Min(MaxChance, c)))
}

// JoinResult is a senator's answer to an invitation.
type JoinResult struct {
	Joined   bool
	Reported bool // they warned the target
	Opinions []OpinionChange
	Text     string
	Practice Delta
}

// ResolveJoin decides whether a senator joins a plot, and if they refuse,
// whether they report it.
func ResolveJoin(r Roller, s Senator, sc SchemeDef, leaderID int64, opLeader, opTarget float64, gift bool) JoinResult {
	ok := r.IntN(100) < JoinChance(s, sc, opLeader, opTarget, gift)
	res := JoinResult{Joined: ok, Practice: Practice(Intrigue, ok)}
	if ok {
		res.Text = fmt.Sprintf("Senator %s hears you out, then nods slowly. *\"Count me in. And we never spoke of this.\"*", s.Name)
		return res
	}
	report := reportBase
	switch {
	case s.Has(TraitIncorruptible):
		report = reportIncorrupt
	case s.Has(TraitStern):
		report = reportStern
	}
	if opLeader < 0 {
		report += reportDislike
	}
	if r.IntN(100) < report {
		res.Reported = true
		res.Opinions = []OpinionChange{{SenatorID: s.ID, CharacterID: leaderID, Delta: reportOpinion}}
		res.Text = fmt.Sprintf("Senator %s goes cold. *\"I will pretend I didn't hear that.\"* You have the feeling they won't.", s.Name)
		return res
	}
	res.Text = fmt.Sprintf("Senator %s shakes their head. *\"Not for me. But your secret is safe.\"*", s.Name)
	return res
}

// ---- striking ----

const (
	strikeEarly       = 0.4 // power counts this much at no progress, rising to 1 when ready
	defenceIntrigue   = 4.0 // per point of the target's Intrigue
	defenceProwess    = 3.0 // per point of the target's Prowess, against assassins
	defenceSenator    = 1.0 // per point of a senator's influence
	defenceWarned     = 10.0
	strikeExposed     = -25 // margin at or below: the leader is caught
	strikeWound       = -10 // margin at or below: a failed murder still wounds
	exposedRenown     = 20  // × the deed's severity
	exposedOpinion    = -6.0
	disgraceInfluence = 1.0 // the leader's share of a senator's fall
)

// StrikeChance is the percent chance the plot succeeds when the leader strikes.
func (p Plot) StrikeChance(d Defender) int {
	ripe := strikeEarly + (1-strikeEarly)*math.Min(1, p.Progress/ProgressMax)
	c := p.Scheme.Base + p.Power()*ripe - p.Defence(d)
	return int(math.Max(MinChance, math.Min(MaxChance, c)))
}

// Defence is how hard the target is to reach.
func (p Plot) Defence(d Defender) float64 {
	v := 0.0
	if p.Warned > Unsuspected {
		v += defenceWarned
	}
	if d.Player != nil {
		v += defenceIntrigue*d.Player.Sheet[AttrIntrigue] + d.Player.Effects.RumorNotice
		if p.Scheme.Key == SchemeAssassinate {
			v += defenceProwess * d.Player.Sheet[AttrProwess]
		}
	}
	if d.Senator != nil {
		v += defenceSenator * d.Senator.Influence
	}
	return v
}

// StrikeResult is how the leader's move went.
type StrikeResult struct {
	Success  bool
	Exposed  bool // the leader is named before the court
	Wounded  bool // a failed murder still hurt the target
	Deltas   map[int64]Delta
	Opinions []OpinionChange
	Text     string // for the leader
	Public   string // posted to the court when exposed
}

// ResolveStrike decides how the strike went. What success does (a death, a
// theft, a forgery, a fallen senator) is carried out by the caller.
func ResolveStrike(r Roller, p Plot, d Defender, senators []Senator) StrikeResult {
	m := Margin(r, p.StrikeChance(d), Normal)
	res := StrikeResult{Success: m > 0, Deltas: map[int64]Delta{}}
	add := func(id int64, x Delta) {
		cur := res.Deltas[id]
		cur.Merge(x)
		res.Deltas[id] = cur
	}
	add(p.Leader.ID, Practice(Intrigue, res.Success))
	for _, mem := range p.Members {
		add(mem.ID, Practice(Intrigue, res.Success))
	}
	name := d.Name()
	switch {
	case res.Success:
		res.Text = successText(p.Scheme.Key, name)
	case m > strikeExposed:
		res.Text = fmt.Sprintf("It came to nothing. %s slipped through your fingers, and the plot is spent. At least nobody knows it was you.", name)
		res.Wounded = p.Scheme.Key == SchemeAssassinate && d.Player != nil && m <= strikeWound
		if res.Wounded {
			res.Text = fmt.Sprintf("The blade found %s, but not deeply enough. They live, wounded. The plot is spent, and nobody knows it was you.", name)
		}
	default:
		res.Exposed = true
		sev := MisdeedDefs[p.Scheme.Crime].Severity
		add(p.Leader.ID, Delta{Renown: -exposedRenown * sev})
		res.Opinions = misdeedOpinions(senators, MisdeedDefs[p.Scheme.Crime], p.Leader.ID, exposedOpinion*sev)
		res.Text = fmt.Sprintf("It went wrong. Your agent was taken in the act against %s, and talked. Your name is on every tongue.", name)
		res.Public = fmt.Sprintf("A plot to %s **%s** has been foiled, and its author unmasked: **%s**.", p.Scheme.Verb(), name, p.Leader.Name)
	}
	return res
}

func successText(key, name string) string {
	switch key {
	case SchemeAssassinate:
		return fmt.Sprintf("It is done. %s is dead, and nobody saw the hand that did it.", name)
	case SchemeFrame:
		return fmt.Sprintf("The letters are forged, the seals are perfect, and the witnesses are paid. You hold the proof that %s is a criminal.", name)
	case SchemeSteal:
		return fmt.Sprintf("Your burglars were in and out of %s's house before the dogs woke.", name)
	case SchemeDisgrace:
		return fmt.Sprintf("The scandal broke this morning. %s has been stripped of their seat.", name)
	}
	return "It is done."
}

// Verb is what the plot does to its target: "murder", "rob"…
func (s SchemeDef) Verb() string {
	switch s.Key {
	case SchemeAssassinate:
		return "murder"
	case SchemeFrame:
		return "frame"
	case SchemeSteal:
		return "rob"
	case SchemeDisgrace:
		return "disgrace"
	}
	return "ruin"
}

// DisgraceReward is what bringing a senator down does for the leader.
func DisgraceReward(leaderInfluence float64) Delta {
	return Delta{Influence: ScaleInfluence(disgraceInfluence, leaderInfluence)}
}

// ---- forgeries ----

// Forgery is a misdeed a Frame a Rival plot can pin on its target.
type Forgery struct {
	Crime  string
	Detail string // %s is the target's name
}

var Forgeries = []Forgery{
	{SecretKindBribe, "%s has bribed a senator over the Senate's business."},
	{SecretKindEmbezzle, "%s has had their hand in the treasury."},
	{SecretKindBandits, "%s has paid bandits to waylay someone on the road."},
	{SecretKindSoldVote, "%s has sold their vote to a senator."},
}

// PickForgery picks the crime a frame pins on its target.
func PickForgery(r Roller) Forgery { return Forgeries[r.IntN(len(Forgeries))] }

// ---- senator replacement ----

// ReplaceSenator seats a persona nobody in the Senate already is, for a
// senator who died or was disgraced. Its ID is the empty seat's.
func ReplaceSenator(r Roller, seated []Senator, seatID int64) Senator {
	taken := map[string]bool{}
	for _, s := range seated {
		taken[s.Name] = true
	}
	var free []Persona
	for _, p := range Personas {
		if !taken[p.Name] {
			free = append(free, p)
		}
	}
	if len(free) == 0 {
		free = Personas
	}
	s := seat(r, free[r.IntN(len(free))])
	s.ID = seatID
	return s
}
