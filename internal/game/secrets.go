package game

import "fmt"

// ---- secrets: what spies can learn ----
//
// Every private action makes a Secret. Each player with a spy network on the
// actor rolls LeakChance; on a hit, RollDetail picks how much they learn.

// Detail is how much of a secret leaks.
type Detail int

const (
	Vague Detail = iota
	Partial
	Full
)

// Secret is one private action, told three ways.
type Secret struct {
	Kind    string
	Vague   string
	Partial string
	Full    string
	// Skip lists characters who already know (e.g. the other side of a
	// deal); they are never told.
	Skip []int64
}

// Text returns the secret at a level of detail.
func (s Secret) Text(d Detail) string {
	switch d {
	case Full:
		return s.Full
	case Partial:
		return s.Partial
	}
	return s.Vague
}

// LeakChance is the percent chance that a network at this level reports a
// secret: about 5% at Low, 55% at Extremely High.
func LeakChance(level float64) int {
	return int(level * 0.6)
}

// RollLeak decides whether a network reports a secret, and how much of it.
// Under the Informers' Law spies report more often, and tell more.
func RollLeak(r Roller, level float64, ls Laws) (Detail, bool) {
	chance := LeakChance(level)
	if ls.Has(LawInformers) {
		chance = int(float64(chance) * LawInformerLeak)
	}
	if level <= 0 || r.IntN(100) >= chance {
		return Vague, false
	}
	n := float64(r.IntN(100))
	d := Vague
	switch {
	case n < level*0.5:
		d = Full
	case n < level*1.2:
		d = Partial
	}
	if ls.Has(LawInformers) && d < Full {
		d++
	}
	return d, true
}

// ---- the catalogue ----
// actor is how the acting player is named, e.g. "Marcus Vane".

func SecretBribe(actor, senator, matter, option string, gold int) Secret {
	return Secret{
		Kind:    "bribe",
		Vague:   fmt.Sprintf("%s's steward was seen carrying a heavy purse toward the Senate house.", actor),
		Partial: fmt.Sprintf("%s has bribed a senator over *%s*.", actor, matter),
		Full:    fmt.Sprintf("%s paid **Senator %s** %d gold to vote **%s** on *%s*.", actor, senator, gold, option, matter),
	}
}

// SecretHiredBandits is paying bandits to rob a traveller.
func SecretHiredBandits(actor, traveler, band string, gold int) Secret {
	return Secret{
		Kind:    "bandits",
		Vague:   fmt.Sprintf("%s's men were seen drinking with rough company in a roadside tavern.", actor),
		Partial: fmt.Sprintf("%s has paid bandits to waylay someone on the road.", actor),
		Full:    fmt.Sprintf("%s paid %s %d gold to rob **%s** on the road home.", actor, band, gold, traveler),
	}
}

func SecretSoldToSenator(actor, senator, matter, option string, gold int) Secret {
	return Secret{
		Kind:    "sold_vote",
		Vague:   fmt.Sprintf("%s came home from the Senate richer than they left.", actor),
		Partial: fmt.Sprintf("%s has sold their vote on *%s* to a senator.", actor, matter),
		Full:    fmt.Sprintf("%s sold their vote on *%s* to **Senator %s** for %d gold. They must vote **%s**.", actor, matter, senator, gold, option),
	}
}

// SecretVoteOffer is a vote deal offered to another player. buying is true
// when the actor offers to pay for the other's vote.
func SecretVoteOffer(actor string, other int64, otherName, matter, option string, price int, buying bool) Secret {
	s := Secret{Kind: "vote_offer", Skip: []int64{other},
		Vague: fmt.Sprintf("%s sent a sealed letter to another house about the Senate's business.", actor)}
	if buying {
		s.Partial = fmt.Sprintf("%s is trying to buy another player's vote on *%s*.", actor, matter)
		s.Full = fmt.Sprintf("%s offered **%s** %d gold to vote **%s** on *%s*.", actor, otherName, price, option, matter)
	} else {
		s.Partial = fmt.Sprintf("%s is offering to sell their vote on *%s* to another player.", actor, matter)
		s.Full = fmt.Sprintf("%s offered to sell their vote on *%s* to **%s** for %d gold.", actor, matter, otherName, price)
	}
	return s
}

// SecretVoteDeal is the actor accepting (or refusing) a vote deal.
func SecretVoteDeal(actor string, other int64, otherName, matter, option string, price int, accepted bool) Secret {
	if !accepted {
		return Secret{Kind: "vote_deal", Skip: []int64{other},
			Vague:   fmt.Sprintf("%s burned a letter unread.", actor),
			Partial: fmt.Sprintf("%s turned down a vote deal on *%s*.", actor, matter),
			Full:    fmt.Sprintf("%s turned down **%s**'s vote deal on *%s* (%d gold).", actor, otherName, matter, price),
		}
	}
	return Secret{Kind: "vote_deal", Skip: []int64{other},
		Vague:   fmt.Sprintf("Gold changed hands between %s and another house.", actor),
		Partial: fmt.Sprintf("%s struck a vote deal with another player on *%s*.", actor, matter),
		Full:    fmt.Sprintf("%s struck a deal with **%s** on *%s*: %d gold for a vote for **%s**.", actor, otherName, matter, price, option),
	}
}

func SecretVote(actor, matter, option string) Secret {
	return Secret{
		Kind:    "vote",
		Vague:   fmt.Sprintf("%s has cast their vote on *%s*.", actor, matter),
		Partial: fmt.Sprintf("%s has cast their vote on *%s*, and seemed pleased with it.", actor, matter),
		Full:    fmt.Sprintf("%s voted **%s** on *%s*.", actor, option, matter),
	}
}

func SecretCourt(actor, senator, approach string) Secret {
	return Secret{
		Kind:    "court",
		Vague:   fmt.Sprintf("%s has gone out calling on someone of importance.", actor),
		Partial: fmt.Sprintf("%s is calling on a senator.", actor),
		Full:    fmt.Sprintf("%s is calling on **Senator %s** (%s).", actor, senator, approach),
	}
}

// SecretCourtResult is how a visit to a senator went.
func SecretCourtResult(actor, senator string, warmed, cooled bool) Secret {
	mood := "nothing much came of it"
	switch {
	case warmed:
		mood = "the senator was charmed"
	case cooled:
		mood = "the senator was glad to see them go"
	}
	return Secret{
		Kind:    "court_result",
		Vague:   fmt.Sprintf("%s came home from a visit.", actor),
		Partial: fmt.Sprintf("%s came home from calling on a senator; %s.", actor, mood),
		Full:    fmt.Sprintf("%s called on **Senator %s**, and %s.", actor, senator, mood),
	}
}

func SecretRumor(actor string, victim int64, victimName string) Secret {
	return Secret{
		Kind: "rumor", Skip: []int64{victim},
		Vague:   fmt.Sprintf("%s has been whispering in corners at a party.", actor),
		Partial: fmt.Sprintf("%s is going to a party to spread rumors about someone.", actor),
		Full:    fmt.Sprintf("%s is going to a party to spread rumors about **%s**.", actor, victimName),
	}
}

func SecretPlant(actor string, victim int64, victimName string) Secret {
	return Secret{
		Kind:    "plant",
		Vague:   fmt.Sprintf("%s met with some unsavoury characters in the lower city.", actor),
		Partial: fmt.Sprintf("%s is trying to plant spies in another player's court.", actor),
		Full:    fmt.Sprintf("%s is trying to plant spies in **%s**'s court.", actor, victimName),
	}
}

func SecretRootOut(actor string) Secret {
	return Secret{
		Kind:    "rootout",
		Vague:   fmt.Sprintf("%s's household seems tense, and the servants are being watched.", actor),
		Partial: fmt.Sprintf("%s suspects there are spies in their house.", actor),
		Full:    fmt.Sprintf("%s is questioning their servants, hunting for spies. Your agents there should keep their heads down.", actor),
	}
}

// SecretInvest is money put into a secret sector (public ones are on the profile).
func SecretInvest(actor, sector string, gold int) Secret {
	return Secret{
		Kind:    "invest",
		Vague:   fmt.Sprintf("%s's men have been seen in the lower city, where honest money doesn't go.", actor),
		Partial: fmt.Sprintf("%s has quietly bought into one of the lower city's rackets.", actor),
		Full:    fmt.Sprintf("%s put %d gold into **%s**.", actor, gold, sector),
	}
}

// SecretEstateOffer is an estate deal offered to another player. summary
// describes it, e.g. "the Riverside Villa for 500 gold".
func SecretEstateOffer(actor string, other int64, otherName, summary string) Secret {
	return Secret{Kind: "estate_offer", Skip: []int64{other},
		Vague:   fmt.Sprintf("%s's steward has been seen with an appraiser.", actor),
		Partial: fmt.Sprintf("%s is trying to strike a deal over their estate with another house.", actor),
		Full:    fmt.Sprintf("%s offered **%s** a deal: %s.", actor, otherName, summary),
	}
}

// SecretEstateDeal is the actor accepting an estate deal.
func SecretEstateDeal(actor string, other int64, otherName, summary string) Secret {
	return Secret{Kind: "estate_deal", Skip: []int64{other},
		Vague:   fmt.Sprintf("Carts have been moving between %s's estate and another house's.", actor),
		Partial: fmt.Sprintf("%s has made a deal with another house over their estates.", actor),
		Full:    fmt.Sprintf("%s accepted **%s**'s deal: %s.", actor, otherName, summary),
	}
}

// SecretCanvass is a day spent working the districts for support.
func SecretCanvass(actor string) Secret {
	return Secret{
		Kind:    "canvass",
		Vague:   fmt.Sprintf("%s has been seen out in the city a great deal.", actor),
		Partial: fmt.Sprintf("%s is shaking hands in the districts.", actor),
		Full:    fmt.Sprintf("%s is canvassing the districts, building support for the Senate.", actor),
	}
}

func SecretEmbezzle(actor string, gold int) Secret {
	return Secret{
		Kind:    "embezzle",
		Vague:   fmt.Sprintf("%s has been spending more freely than their income explains.", actor),
		Partial: fmt.Sprintf("%s has had their hand in the treasury.", actor),
		Full:    fmt.Sprintf("%s stole **%d gold** from the treasury.", actor, gold),
	}
}

// SecretMagic is buying an enchanted item while the Witchcraft Act stands.
func SecretMagic(actor, item string) Secret {
	return Secret{
		Kind:    "magic",
		Vague:   fmt.Sprintf("Something heavy and carefully wrapped was carried into %s's house after dark.", actor),
		Partial: fmt.Sprintf("%s has bought something enchanted, in spite of the Witchcraft Act.", actor),
		Full:    fmt.Sprintf("%s bought **%s**, in spite of the Witchcraft Act.", actor, item),
	}
}
