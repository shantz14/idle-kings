package game

import (
	"fmt"
	"math"
	"time"
)

// ---- Sabotage and Smear Campaigns (Intrigue) ----
//
// Two ways to hurt a rival directly. Sabotage sends agents against one of
// their ventures (their slice earns less for a few days) or one of their
// properties (it gives nothing until they pay for repairs). A smear campaign
// spreads lies about them without waiting for a party. Both are Intrigue
// against Intrigue, helped by a purse and by your spies in their court; a
// clumsy attempt is noticed, a very clumsy one names you. Placeholder numbers
// — tune freely.

const (
	KindSabotage = "sabotage"
	KindSmear    = "smear"
)

var (
	DefaultSabotageDuration = 2 * time.Hour
	FastSabotageDuration    = 2 * time.Minute
	DefaultSmearDuration    = 2 * time.Hour
	FastSmearDuration       = 2 * time.Minute
)

// What sabotage is aimed at.
const (
	SabotageVenture  = "venture"
	SabotageProperty = "property"
)

const (
	sabotageBase      = 40.0
	sabotagePerPoint  = 5.0 // per point of your Intrigue over theirs
	sabotageSpyShare  = 0.2 // of your spy level in their court
	sabotageGreat     = 30  // margin for the worst damage
	sabotageNoticed   = -25 // a miss this bad is noticed
	sabotageNamed     = -45 // a miss this bad names you
	sabotageNamedFame = 10  // renown the named saboteur loses
	sabotageNamedHate = -4.0

	// SabotageDays is how long a hit venture earns less.
	SabotageDays = 3
	// A hit venture earns this share; a great success, nothing.
	sabotageVentureFactor = 0.5
	sabotageVentureGreat  = 0.0
	// RepairShare is the share of a property's price its repair costs.
	RepairShare = 0.1
)

// SabotageInput is everything a sabotage needs.
type SabotageInput struct {
	Actor, Target Person
	Spies         float64 // the actor's spy level in the target's court
	Purse         SpyPurse
}

// SabotageResult is how it went. On success the caller damages the venture
// (VentureFactor) or the property.
type SabotageResult struct {
	Success       bool
	Great         bool
	Noticed       bool // the target learns someone tried
	Named         bool // and who
	VentureFactor float64
	Actor         Delta
	Opinions      []OpinionChange
}

// SabotageChance is the hidden chance of a sabotage.
func SabotageChance(in SabotageInput) int {
	c := sabotageBase + sabotagePerPoint*(in.Actor.Sheet[AttrIntrigue]-in.Target.Sheet[AttrIntrigue]) +
		sabotageSpyShare*in.Spies + in.Purse.Bonus - in.Target.Effects.RumorNotice
	return int(math.Max(MinChance, math.Min(MaxChance, c)))
}

// ResolveSabotage rolls a sabotage. senators react to a named saboteur.
func ResolveSabotage(r Roller, in SabotageInput, senators []Senator) SabotageResult {
	m := Margin(r, SabotageChance(in), Normal)
	res := SabotageResult{Success: m > 0, Great: m >= sabotageGreat}
	res.Actor = Practice(Intrigue, res.Success)
	switch {
	case res.Great:
		res.VentureFactor = sabotageVentureGreat
	case res.Success:
		res.VentureFactor = sabotageVentureFactor
	case m <= sabotageNamed:
		res.Noticed, res.Named = true, true
		res.Actor.Merge(Delta{Renown: -sabotageNamedFame})
		res.Opinions = TraitReactions(senators, in.Actor.ID,
			TraitReaction{TraitStern, sabotageNamedHate}, TraitReaction{TraitIncorruptible, sabotageNamedHate})
	case m <= sabotageNoticed:
		res.Noticed = true
	}
	return res
}

// RepairCost is what it costs to repair a sabotaged property.
func RepairCost(it Item) int {
	return max(5, int(math.Round(float64(it.Price)*RepairShare)))
}

// SumEffectsDamaged is SumEffects with some holdings damaged (by position in
// keys): a damaged piece gives nothing, but a damaged home is still home, so
// the other properties keep counting as before.
func SumEffectsDamaged(keys []string, damaged map[int]bool) Effects {
	if len(damaged) == 0 {
		return SumEffects(keys)
	}
	var e Effects
	home := false
	for n, k := range keys {
		it, ok := ItemByKey(k)
		switch {
		case !ok:
		case damaged[n]:
			home = home || it.IsProperty()
		case it.IsProperty() && home:
			e.Add(Effects{Stats: it.Effects.Stats, DailyIncome: it.Effects.DailyIncome})
		default:
			home = home || it.IsProperty()
			e.Add(it.Effects)
		}
	}
	return e
}

// ---- smears ----

var smearCheck = Check{Base: 25, Weights: map[Attr]float64{AttrIntrigue: 6}}

const (
	smearGreat      = 30
	smearInfluence  = 2.0
	smearGreatInfl  = 3.0
	smearOpinion    = -1.0
	smearGreatOp    = -2.0
	smearNamedFame  = 5 // renown a named smearer loses
	smearPerDefence = 3.0
)

// SmearInput is everything a smear campaign needs.
type SmearInput struct {
	Actor, Target Person
	Purse         SpyPurse
	Laws          Laws
}

// SmearResult is how a smear went: what to apply and who to tell.
type SmearResult struct {
	Ledger
	Landed bool
}

// ResolveSmear rolls a smear campaign and the target's chance to notice it,
// with the same three levels as a party rumor: they learn it happened, then
// who, then stop it.
func ResolveSmear(r Roller, in SmearInput) SmearResult {
	a, t := in.Actor, in.Target
	res := SmearResult{Ledger: newLedger()}
	ok, margin := smearCheck.Roll(r, a.Sheet, in.Purse.Bonus-smearPerDefence*t.Sheet[AttrIntrigue], Normal)
	res.delta(a.ID, Practice(Intrigue, ok))
	rules := in.Laws.Rules()
	saw, tm := targetNotice.Roll(r, t.Sheet, t.Effects.RumorNotice+rules.RumorNotice, Disadvantage)
	named := saw && tm >= noticeNamed
	stopped := saw && tm >= noticeCancel
	if named {
		res.delta(a.ID, Delta{Renown: -smearNamedFame})
		if fine := rules.SlanderFine; fine != 0 {
			res.delta(a.ID, Delta{Wealth: -fine})
			res.delta(t.ID, Delta{Wealth: fine})
			res.notice(t.ID, "Under the Law of Slander, %s must pay you for their lies. (+%d gold)", a.Name, fine)
			res.notice(a.ID, "You were named as a slanderer, and the law makes you pay %s. (-%d gold)", t.Name, fine)
		}
	}
	res.Landed = ok && !stopped
	switch {
	case !ok:
		res.notice(a.ID, "Your pamphlets about %s fell flat. Nobody believed a word.", t.Name)
	case stopped:
		res.notice(a.ID, "Your lies about %s were answered before they spread.", t.Name)
	default:
		infl, op := smearInfluence, smearOpinion
		if margin >= smearGreat {
			infl, op = smearGreatInfl, smearGreatOp
		}
		res.delta(t.ID, Delta{Influence: -infl})
		res.Opinions = append(res.Opinions, OpinionChange{CharacterID: t.ID, Delta: op})
		res.notice(a.ID, "Your lies about %s are all over the city. The Senate has heard them.", t.Name)
	}
	if saw {
		switch {
		case stopped:
			res.notice(t.ID, "%s has been spreading lies about you in the city. You answered them before anyone believed a word.", a.Ref())
		case named:
			res.notice(t.ID, "%s has been spreading lies about you in the city.%s", a.Ref(), rumorSuffix(ok, res.Landed))
		default:
			res.notice(t.ID, "Someone has been spreading lies about you in the city.%s", rumorSuffix(ok, res.Landed))
		}
	}
	if named {
		res.notice(a.ID, "Worse, %s found out it was you. (%s)", t.Name, Delta{Renown: -smearNamedFame}.Describe())
	}
	return res
}

// SabotageWhat is a sabotage target in words, for notices.
func SabotageWhat(kind, name string) string {
	if kind == SabotageVenture {
		return fmt.Sprintf("their stake in %s", name)
	}
	return name
}
