package game

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// ---- lasting laws ----
//
// A law, once passed, stays in force until it is repealed (or lapses when a
// newer law pushes it out). Laws bend the everyday rules: votes, bribes,
// parties, duels, sectors, the shop, income and spies. Each rule reads the
// court's Laws where it applies (see the Law* constants below for the
// numbers). Some laws cost the realm a little every Senate day, and the
// senators, who only judge a vote by what it does now, never see that.
//
// Laws reach the Senate three ways: as an outcome of an ordinary matter
// (MatterOutcome.Enact/Repeal), as a motion the bot picks (generated below
// from LawDefs), or through a player's Petition the Senate (petition.go).

type LawKey string

const (
	LawSecretBallot  LawKey = "secret_ballot"
	LawBribery       LawKey = "bribery"
	LawFranchise     LawKey = "franchise"
	LawAbstainers    LawKey = "abstainers"
	LawSlander       LawKey = "slander"
	LawDuelBan       LawKey = "duel_ban"
	LawHonor         LawKey = "honor"
	LawGrainDole     LawKey = "grain_dole"
	LawMonopolies    LawKey = "monopolies"
	LawProhibition   LawKey = "prohibition"
	LawPropertyTax   LawKey = "property_tax"
	LawDebasement    LawKey = "debasement"
	LawInformers     LawKey = "informers"
	LawMartial       LawKey = "martial_law"
	LawSorcery       LawKey = "licensed_sorcery"
	LawWitchcraftAct LawKey = "witchcraft_act"
)

// MaxLaws is how many laws can stand at once. Passing another makes the
// oldest lapse.
const MaxLaws = 5

// Placeholder numbers for what each law does. Tune freely. Players see the
// law's Desc, never these.
const (
	// Secret Ballot: no numbers. Votes are hidden, deals run on trust.

	// Law Against Bribery
	LawBribeFactor       = 1.5  // bribes cost this much more
	LawBribeExposeRenown = 2.0  // renown lost when a bribe comes out
	LawBribeExposeOp     = -10. // Incorruptible and Stern senators, when it does

	// Landed Franchise: vote weight from EstateScore(value)
	LawFranchisePerScore = 0.15

	// Abstainers' Due: influence for not voting (through ScaleInfluence)
	LawAbstainInfluence = 0.4

	// Law of Slander
	LawSlanderNotice = 15.0 // percent added to notice rumors
	LawSlanderFine   = 15   // gold a named rumor-monger pays their target

	// Ban on Dueling
	LawDuelBanRenown  = 1.5 // renown a challenger loses for issuing one
	LawDuelBanOpinion = -5. // Stern senators, toward the challenger

	// Code of Honor
	LawHonorRenown         = 1.5 // winner's renown multiplier
	LawHonorDeclineRenown  = 1.5 // renown lost for refusing a challenge
	LawHonorDeclineOpinion = -4. // Hawk, Stern and Conservative senators
	LawHonorBetCut         = 0.1 // share of every betting pot the court takes

	// Charter of Monopolies: effective stake multipliers
	LawMonopolyLeader = 1.5 // the biggest player stake in a sector
	LawMonopolyRest   = 0.75

	// Property Tax: share of a property's price paid every day, for every
	// property but the residence.
	LawPropertyTaxRate = 0.01

	// Debased Coinage
	LawDebasedPrices = 1.25 // shop prices
	LawDebasedIncome = 0.85 // every player's gold/day

	// Informers' Law
	LawInformerLeak      = 1.5  // leak chance multiplier
	LawInformerRoot      = 15.0 // percent added to Root Out Spies
	LawDenounceReward    = 25   // gold paid to a denouncer
	LawDenounceTreasury  = 1.0  // what the reward costs the treasury
	LawDenounceGood      = 2.0  // Stern and Incorruptible senators, toward the denouncer
	LawDenounceInfluence = 0.3  // influence for a denunciation (through ScaleInfluence)

	// Martial Law
	LawPermitFee = 20 // gold to start a party, speech or circus

	// Licensed Sorcery
	LawSorceryCheck = 10.0 // percent added to Senate rolls on sorcery matters

	// Witchcraft Act
	LawWitchFineChance = 10  // percent per magic item per Senate day
	LawWitchFineRate   = 0.1 // share of the item's price
	LawWitchSellBack   = 0.8 // the shop pays this for magic items
)

// LawDef is a law in the catalogue.
type LawDef struct {
	Key   LawKey
	Name  string // "the Secret Ballot"-style names are made with Title
	Emoji string
	// Desc says what the law does, in words, for players. No numbers.
	Desc string
	// Motion is the prompt when the Senate considers passing it; Repeal is
	// the prompt when it considers repealing it.
	Motion string
	Repeal string
	// Rival is a law that can't stand with this one: passing either
	// repeals the other.
	Rival LawKey
	// Traits are how senators with each trait feel about passing it
	// (repealing it is the opposite).
	Traits map[Trait]float64
	// Pass and Unpass are what the motion to pass or repeal does to the
	// realm and sectors on the day, besides the law itself.
	Pass   MatterOutcome
	Unpass MatterOutcome
	// Daily is what the law does to the realm every Senate day it stands,
	// and DailySectors to the sectors.
	Daily        Realm
	DailySectors map[SectorKey]SectorChange
	// Revenue multiplies sector revenue while the law stands.
	Revenue map[SectorKey]float64
	// Effects are the everyday rules it bends (see Laws.Rules).
	Effects LawEffects
}

// Title is the law's name with "the", e.g. "the Secret Ballot".
func (d LawDef) Title() string { return "the " + d.Name }

var LawDefs = []LawDef{
	{Key: LawSecretBallot, Name: "Secret Ballot", Emoji: "🗳️",
		Desc:    "Nobody's vote is ever revealed. Senators can't tell who voted with them, a bought vote can't be checked, and only spies know the truth.",
		Motion:  "A tribune proposes that the Senate vote in secret, with stones dropped into covered urns. \"Let every senator vote his conscience, and not his patron's,\" he says. The old families are not pleased.",
		Repeal:  "The covered urns have hidden a great deal of treachery. A motion asks that votes be cast openly again, so the Senate can see who stands where.",
		Traits:  map[Trait]float64{TraitScheming: 2, TraitIncorruptible: -1.5, TraitStern: -0.5, TraitConservative: -0.5},
		Effects: LawEffects{SecretBallot: true}},
	{Key: LawBribery, Name: "Law Against Bribery", Emoji: "⚖️",
		Desc:    "Bribing a senator costs more, and a bribe that comes out costs the briber their good name, and the respect of the Stern and the Incorruptible. Selling your vote to a senator is a crime too.",
		Motion:  "Too much gold changes hands before every vote. A motion would make bribery of a senator a crime, tried before the whole Senate.",
		Repeal:  "The bribery trials drag on, and half the Senate fears the next accusation. A motion would quietly strike the law from the tablets.",
		Traits:  map[Trait]float64{TraitIncorruptible: 2.5, TraitStern: 1.5, TraitGreedy: -2.5, TraitScheming: -1.5},
		Effects: LawEffects{BribePrice: LawBribeFactor, BribesExposed: true, Crimes: []string{SecretKindBribe, SecretKindSoldVote}}},
	{Key: LawFranchise, Name: "Landed Franchise", Emoji: "🏰",
		Desc:    "A voice in the Senate is weighed by property. The finer your estate, the more your vote counts.",
		Motion:  "\"Those who own the realm should run it,\" say the landholders. A motion would give every great house extra weight in the Senate according to its estate.",
		Repeal:  "New men with no land complain that the Senate has become a club for the rich. A motion would weigh every voice by influence alone again.",
		Traits:  map[Trait]float64{TraitLandowner: 2.5, TraitConservative: 1, TraitPopulist: -2, TraitMerchant: -0.5},
		Effects: LawEffects{FranchisePerScore: LawFranchisePerScore}},
	{Key: LawAbstainers, Name: "Abstainers' Due", Emoji: "🤐",
		Desc:    "Sitting out a vote is a recognized right. Those who don't vote on a matter quietly gain a little standing for their restraint.",
		Motion:  "A senator who never votes proposes that silence be honored: \"The wise man knows when not to speak.\" Nobody is sure if he is joking.",
		Repeal:  "Half the court now abstains on purpose. A motion would end the custom and expect every member to take a side.",
		Traits:  map[Trait]float64{TraitScheming: 1, TraitDove: 0.5, TraitStern: -1, TraitHawk: -0.5},
		Effects: LawEffects{AbstainInfluence: LawAbstainInfluence}},
	{Key: LawSlander, Name: "Law of Slander", Emoji: "🗣️",
		Desc:    "Spreading rumors is a crime. Rumors are easier to notice, and a rumor-monger who is named must pay their victim.",
		Motion:  "After a season of whispered lies, a senator proposes that slander be punished by a fine paid to the slandered.",
		Repeal:  "The courts are full of nobles suing each other over gossip. A motion would let people say what they like again.",
		Traits:  map[Trait]float64{TraitStern: 1.5, TraitConservative: 0.5, TraitScheming: -2},
		Effects: LawEffects{RumorNotice: LawSlanderNotice, SlanderFine: LawSlanderFine, Crimes: []string{SecretKindRumor, SecretKindWhisper}}},
	{Key: LawDuelBan, Name: "Ban on Dueling", Emoji: "🚫",
		Desc:    "Dueling is forbidden. Issuing a challenge costs you renown and the favor of the Stern, and there is no shame in refusing one.",
		Motion:  "Another young noble lies dead in the street over an insult. A motion would ban dueling within the realm.",
		Repeal:  "The young bloods say the ban has made cowards of them all. A motion would let them settle their quarrels with steel again.",
		Rival:   LawHonor,
		Traits:  map[Trait]float64{TraitDove: 2, TraitStern: 1, TraitHawk: -2, TraitRiskTaker: -1},
		Effects: LawEffects{ChallengeRenown: LawDuelBanRenown, ChallengeOpinion: LawDuelBanOpinion, NoDeclineShame: true}},
	{Key: LawHonor, Name: "Code of Honor", Emoji: "🤺",
		Desc:    "Dueling is honored by law. Victory wins more renown, refusing a challenge is a disgrace, and the court takes a cut of every bet.",
		Motion:  "The officers propose a code of honor: duels fought by the rules, witnessed and taxed, and cowardice punished.",
		Repeal:  "The Code of Honor has filled the cemeteries. A motion would strike it from the law.",
		Rival:   LawDuelBan,
		Traits:  map[Trait]float64{TraitHawk: 2, TraitConservative: 1, TraitRiskTaker: 0.5, TraitDove: -2},
		Effects: LawEffects{DuelWinRenown: LawHonorRenown, DeclineRenown: LawHonorDeclineRenown, DeclineOpinion: LawHonorDeclineOpinion, BetCut: LawHonorBetCut}},
	{Key: LawGrainDole, Name: "Grain Dole", Emoji: "🌾",
		Desc:         "The state gives free grain to every citizen, every day. The people love it, the farms grow to feed it, and the treasury pays for it forever.",
		Motion:       "A tribune proposes a monthly dole of free grain to every citizen of the capital. The crowd outside the Senate house is very loud about it.",
		Repeal:       "The grain dole eats the treasury alive. A motion would end it, and the crowd outside is very loud about that, too.",
		Traits:       map[Trait]float64{TraitPopulist: 2.5, TraitBigSpender: 1, TraitLandowner: 0.5, TraitEconomical: -2.5},
		Unpass:       MatterOutcome{Realm: Realm{AttrPopularity: -8}},
		Daily:        Realm{AttrTreasury: -2, AttrPopularity: 1.5},
		DailySectors: map[SectorKey]SectorChange{SectorAgriculture: {Capacity: 0.3}}},
	{Key: LawMonopolies, Name: "Charter of Monopolies", Emoji: "📜",
		Desc:    "The state grants monopolies. In every industry, whoever at court holds the biggest stake takes a larger share, and everyone else's share shrinks.",
		Motion:  "The merchant princes propose royal charters: one great house to lead each industry, with the profits to match.",
		Repeal:  "The charters have made a few houses rich and the rest bitter. A motion would open the industries to fair competition again.",
		Traits:  map[Trait]float64{TraitMerchant: 2.5, TraitGreedy: 1, TraitPopulist: -1.5, TraitEconomical: -0.5},
		Effects: LawEffects{MonopolyLeader: LawMonopolyLeader, MonopolyRest: LawMonopolyRest}},
	{Key: LawProhibition, Name: "Prohibition of Alchemy", Emoji: "⚗️",
		Desc:    "Alchemy is outlawed. The alchemists' trade all but stops, and the Black Market grows fat on what they sell under the counter.",
		Motion:  "Another alchemist's cure has turned out to be poison. The temples demand that the whole unnatural art be outlawed.",
		Repeal:  "The alchemists have only moved into the cellars, and the Black Market is richer than ever. A motion would make their trade legal again.",
		Traits:  map[Trait]float64{TraitPious: 2.5, TraitConservative: 1, TraitStern: 0.5, TraitMerchant: -1, TraitRiskTaker: -0.5},
		Revenue: map[SectorKey]float64{SectorAlchemy: 0.15, SectorBlackMarket: 1.4}},
	{Key: LawPropertyTax, Name: "Property Tax", Emoji: "🏚️",
		Desc:    "Every property but the one you live in is taxed, every day. The treasury fills, and great landholders feel it.",
		Motion:  "The treasury is thin and the great houses own half the city. A motion would tax every property a family doesn't live in.",
		Repeal:  "The landholders say the property tax is ruining the old families. A motion would end it.",
		Traits:  map[Trait]float64{TraitPopulist: 1.5, TraitEconomical: 1, TraitLandowner: -2.5, TraitConservative: -0.5},
		Daily:   Realm{AttrTreasury: 1},
		Effects: LawEffects{PropertyTaxRate: LawPropertyTaxRate}},
	{Key: LawDebasement, Name: "Debased Coinage", Emoji: "🪙",
		Desc:    "The silver coins are cut with lead. When it passes, the treasury mints a one-time fortune in new coins; but while it stands, prices climb and every purse buys less.",
		Motion:  "The mint master has a clever idea: melt down the treasury's silver, mix in a little lead, and strike twice as many coins.",
		Repeal:  "Merchants weigh every coin and prices keep climbing. A motion would recall the bad coins and mint honest silver again, at great cost.",
		Traits:  map[Trait]float64{TraitGreedy: 1, TraitBigSpender: 1, TraitRiskTaker: 0.5, TraitConservative: -2, TraitMerchant: -2},
		Pass:    MatterOutcome{Realm: Realm{AttrTreasury: 15, AttrStability: -3}},
		Unpass:  MatterOutcome{Realm: Realm{AttrTreasury: -10}, Sectors: map[SectorKey]SectorChange{SectorTrade: {Health: 0.15}}},
		Effects: LawEffects{ShopPrices: LawDebasedPrices, Income: LawDebasedIncome}},
	{Key: LawInformers, Name: "Informers' Law", Emoji: "🕵️",
		Desc:    "The state pays informers. Spies everywhere learn more and more often, rooting them out is easier, and a crime your spies uncover can be denounced for a reward.",
		Motion:  "A magistrate proposes paying a reward to anyone who reports a crime against the state. \"Let every servant be our eyes.\"",
		Repeal:  "Nobody trusts their own household any more. A motion would stop paying informers.",
		Traits:  map[Trait]float64{TraitStern: 1.5, TraitScheming: 1.5, TraitIncorruptible: 1, TraitPopulist: -1, TraitDove: -0.5},
		Effects: LawEffects{LeakChance: LawInformerLeak, LeakDetail: 1, RootBonus: LawInformerRoot, PaysInformers: true}},
	{Key: LawMartial, Name: "Martial Law", Emoji: "🪖",
		Desc:    "Soldiers keep order in the streets. Order returns quickly, the people grow sullen, smugglers suffer, and every public gathering needs a permit.",
		Motion:  "The streets are unruly. A general proposes martial law: curfews, patrols, and a permit for every gathering.",
		Repeal:  "Soldiers on every corner and a curfew every night. A motion would send the army back to its barracks.",
		Traits:  map[Trait]float64{TraitStern: 2.5, TraitHawk: 1.5, TraitConservative: 0.5, TraitPopulist: -2, TraitDove: -1.5},
		Daily:   Realm{AttrStability: 3, AttrPopularity: -2.5},
		Revenue: map[SectorKey]float64{SectorBlackMarket: 0.7, SectorTrade: 0.9},
		Effects: LawEffects{PermitFee: LawPermitFee}},
	{Key: LawSorcery, Name: "Licensed Sorcery", Emoji: "🔮",
		Desc:    "Sorcerers and alchemists may practice under license. The alchemists' trade flourishes, and the realm handles the uncanny better.",
		Motion:  "The Alchemists' Guild offers a bargain: license sorcery, tax it, and let the wizards serve the state instead of hiding from it.",
		Repeal:  "The temples say the licensed sorcerers have brought curses on the city. A motion would revoke every license.",
		Rival:   LawWitchcraftAct,
		Traits:  map[Trait]float64{TraitCosmopolitan: 1, TraitRiskTaker: 1, TraitMerchant: 1, TraitPious: -2.5, TraitConservative: -1},
		Revenue: map[SectorKey]float64{SectorAlchemy: 1.25},
		Effects: LawEffects{ThemeCheck: map[Theme]float64{ThemeSorcery: LawSorceryCheck}}},
	{Key: LawWitchcraftAct, Name: "Witchcraft Act", Emoji: "🔥",
		Desc:    "Magic is suspect. Owning an enchanted item risks a fine, nervous owners sell them back for more, and the alchemists' trade suffers.",
		Motion:  "Strange lights, cursed wells, a calf with two heads. The priests want an act against witchcraft, and the hunting of every enchanted thing in the realm.",
		Repeal:  "The witch-finders have started fining senators. A motion would repeal the Witchcraft Act.",
		Rival:   LawSorcery,
		Traits:  map[Trait]float64{TraitPious: 2.5, TraitConservative: 1, TraitStern: 0.5, TraitCosmopolitan: -1, TraitRiskTaker: -0.5},
		Revenue: map[SectorKey]float64{SectorAlchemy: 0.8, SectorBlackMarket: 1.1},
		Effects: LawEffects{MagicSellBack: LawWitchSellBack, WitchHunt: true, Crimes: []string{SecretKindMagic}}},
}

func LawByKey(k LawKey) (LawDef, bool) {
	return lawIndex.get(k)
}

// LawName is a law's name, or its key if it isn't in the catalogue.
func LawName(k LawKey) string {
	if d, ok := LawByKey(k); ok {
		return d.Name
	}
	return string(k)
}

// ---- the laws in force ----

// Law is a law in force, and when it was passed.
type Law struct {
	Key   LawKey    `json:"key"`
	Since time.Time `json:"since"`
}

// Laws are a court's laws in force, oldest first.
type Laws []Law

func (ls Laws) Has(k LawKey) bool {
	for _, l := range ls {
		if l.Key == k {
			return true
		}
	}
	return false
}

func (ls Laws) Keys() []LawKey {
	out := make([]LawKey, len(ls))
	for i, l := range ls {
		out[i] = l.Key
	}
	return out
}

// Full reports whether passing another law would make one lapse.
func (ls Laws) Full() bool { return len(ls) >= MaxLaws }

func (ls Laws) Clone() Laws { return append(Laws(nil), ls...) }

// WouldLapse lists the laws that would lapse if k passed: the oldest, when
// every slot is taken and k doesn't replace its own rival. It is the warning
// players see, and follows Apply exactly.
func (ls Laws) WouldLapse(k LawKey) []LawKey {
	_, ch := ls.Apply([]LawKey{k}, nil, time.Time{})
	return ch.Lapsed
}

// LawChanges is what a decision did to the laws.
type LawChanges struct {
	Enacted  []LawKey
	Repealed []LawKey // by the decision, or because a rival was passed
	Lapsed   []LawKey // pushed out by MaxLaws
}

func (c LawChanges) Empty() bool {
	return len(c.Enacted) == 0 && len(c.Repealed) == 0 && len(c.Lapsed) == 0
}

// Apply repeals, then enacts, and returns the new laws. Enacting a law
// already in force does nothing; enacting one repeals its rival; past
// MaxLaws the oldest lapses.
func (ls Laws) Apply(enact, repeal []LawKey, now time.Time) (Laws, LawChanges) {
	out := ls.Clone()
	var ch LawChanges
	remove := func(k LawKey) bool {
		for i, l := range out {
			if l.Key == k {
				out = append(out[:i:i], out[i+1:]...)
				return true
			}
		}
		return false
	}
	for _, k := range repeal {
		if remove(k) {
			ch.Repealed = append(ch.Repealed, k)
		}
	}
	for _, k := range enact {
		d, ok := LawByKey(k)
		if !ok || out.Has(k) {
			continue
		}
		if d.Rival != "" && remove(d.Rival) {
			ch.Repealed = append(ch.Repealed, d.Rival)
		}
		for len(out) >= MaxLaws {
			ch.Lapsed = append(ch.Lapsed, out[0].Key)
			out = out[1:]
		}
		out = append(out, Law{Key: k, Since: now})
		ch.Enacted = append(ch.Enacted, k)
	}
	return out, ch
}

// Describe renders the changes for an announcement, one line each.
func (c LawChanges) Describe() string {
	var lines []string
	for _, k := range c.Enacted {
		d, _ := LawByKey(k)
		lines = append(lines, fmt.Sprintf("📜 **%s %s** is now law. %s", d.Emoji, d.Name, d.Desc))
	}
	for _, k := range c.Repealed {
		d, _ := LawByKey(k)
		lines = append(lines, fmt.Sprintf("📜 **%s %s** is repealed.", d.Emoji, d.Name))
	}
	for _, k := range c.Lapsed {
		d, _ := LawByKey(k)
		lines = append(lines, fmt.Sprintf("📜 **%s %s** lapses, to make room for newer laws.", d.Emoji, d.Name))
	}
	return strings.Join(lines, "\n")
}

// DescribeLaws lists the laws in force for an info page.
func DescribeLaws(ls Laws) string {
	if len(ls) == 0 {
		return "No laws beyond the old customs."
	}
	var lines []string
	for _, l := range ls {
		if d, ok := LawByKey(l.Key); ok {
			lines = append(lines, fmt.Sprintf("%s **%s**: %s", d.Emoji, d.Name, d.Desc))
		}
	}
	return strings.Join(lines, "\n")
}

// ---- how laws shape the realm ----

// lawScore is how a senator feels about the laws an option passes or repeals.
func lawScore(s Senator, o MatterOption) float64 {
	score := 0.0
	add := func(keys []LawKey, sign float64) {
		for _, k := range keys {
			d, _ := LawByKey(k)
			for _, t := range s.Traits {
				score += sign * d.Traits[t]
			}
		}
	}
	// A law's fate is the same whether a rolled option succeeds or not
	// unless the outcomes say otherwise, so judge by success.
	add(o.Success.Enact, 1)
	add(o.Success.Repeal, -1)
	return score
}

// ApplyDailyLaws is what the laws in force do to the realm and the sectors
// on a Senate day.
func ApplyDailyLaws(realm Realm, sectors SectorState, ls Laws) {
	for _, l := range ls {
		d, _ := LawByKey(l.Key)
		realm.Apply(d.Daily)
		sectors.Apply(d.DailySectors)
	}
}

// DailyLawRealm is the laws' combined daily effect on the realm.
func DailyLawRealm(ls Laws) Realm {
	out := Realm{}
	for _, l := range ls {
		d, _ := LawByKey(l.Key)
		for k, v := range d.Daily {
			out[k] += v
		}
	}
	return out
}

// LawRevenue multiplies a sector's revenue by every law in force.
func LawRevenue(ls Laws, k SectorKey) float64 {
	m := 1.0
	for _, l := range ls {
		d, _ := LawByKey(l.Key)
		if f, ok := d.Revenue[k]; ok {
			m *= f
		}
	}
	return m
}

// LawCheckMod is the bonus a Senate roll on this event gets from the laws.
func LawCheckMod(ls Laws, ev MatterEvent) float64 { return ls.Rules().ThemeCheck[ev.Theme] }

// ---- motions: the Senate considers passing or repealing a law ----

// ThemeLaw is the theme of every motion.
const ThemeLaw Theme = "law"

// motionWeight is how likely a motion is to be picked next to an ordinary
// event (weight 1), so laws stay occasional.
const motionWeight = 0.3

func MotionKey(k LawKey) string { return "law_" + string(k) }
func RepealKey(k LawKey) string { return "repeal_" + string(k) }

// motionMatters builds the pass and repeal motion for every law.
func motionMatters() []MatterEvent {
	var out []MatterEvent
	for _, d := range LawDefs {
		pass := d.Pass
		pass.Text = fmt.Sprintf("The Senate passes %s.", d.Title())
		pass.Enact = []LawKey{d.Key}
		hint := d.Desc
		if d.Rival != "" {
			hint += fmt.Sprintf(" It would repeal %s.", LawTitle(d.Rival))
		}
		out = append(out, MatterEvent{
			Key: MotionKey(d.Key), Title: "A Motion for " + d.Title(), Prompt: d.Motion, Theme: ThemeLaw,
			Motion: d.Key, Default: "reject",
			Options: []MatterOption{
				{Key: "pass", Label: "Pass it", Hint: hint, Success: pass},
				{Key: "reject", Label: "Reject it", Hint: "Things stay as they are.", Leans: L{LeanTradition: 1},
					Success: MatterOutcome{Text: fmt.Sprintf("The Senate rejects %s.", d.Title())}},
			},
		})
		unpass := d.Unpass
		unpass.Text = fmt.Sprintf("The Senate repeals %s.", d.Title())
		unpass.Repeal = []LawKey{d.Key}
		out = append(out, MatterEvent{
			Key: RepealKey(d.Key), Title: "A Motion to Repeal " + d.Title(), Prompt: d.Repeal, Theme: ThemeLaw,
			Motion: d.Key, Repeal: true, Default: "keep",
			Options: []MatterOption{
				{Key: "repeal", Label: "Repeal it", Hint: "The law is struck from the tablets.", Success: unpass},
				{Key: "keep", Label: "Keep it", Hint: "The law stands: " + d.Desc, Leans: L{LeanTradition: 1},
					Success: MatterOutcome{Text: fmt.Sprintf("The %s stands.", d.Name)}},
			},
		})
	}
	return out
}

// LawTitle is a law's name with "the".
func LawTitle(k LawKey) string { return "the " + LawName(k) }

// MotionAllowed reports whether a motion can come up under these laws: a
// motion to pass a law only if it isn't in force, and a motion to repeal one
// only if it is. Other events are always allowed.
func MotionAllowed(e MatterEvent, ls Laws) bool {
	if e.Motion == "" {
		return true
	}
	if e.Repeal {
		return ls.Has(e.Motion)
	}
	return !ls.Has(e.Motion)
}

func init() { MatterEvents = append(MatterEvents, motionMatters()...) }

// ---- what the laws do to everyday rules ----

// BribePriceUnder is BribePriceFor under the laws: the Law Against Bribery
// makes every bribe dearer.
func (s Senator) BribePriceUnder(ev MatterEvent, optKey string, realm Realm, matterID int64, op float64, ls Laws) (int, bool) {
	price, ok := s.BribePriceFor(ev, optKey, realm, matterID, op)
	if f := ls.Rules().BribePrice; ok && f != 0 {
		price = int(math.Ceil(float64(price) * f))
	}
	return price, ok
}

// bribeCover is the briber's roll to keep a bribe quiet under the Law
// Against Bribery.
var bribeCover = Check{Base: 70, Weights: map[Attr]float64{AttrIntrigue: 3}}

// BribeExposure decides whether a bribe comes out under the Law Against
// Bribery. If it does, the briber loses renown and the Incorruptible and
// Stern think less of them.
func BribeExposure(r Roller, briber Person, senators []Senator, ls Laws) (exposed bool, d Delta, ops []OpinionChange) {
	if !ls.Rules().BribesExposed {
		return false, Delta{}, nil
	}
	if ok, _ := bribeCover.Roll(r, briber.Sheet, 0, Normal); ok {
		return false, Delta{}, nil
	}
	return true, Delta{Renown: -LawBribeExposeRenown}, lawOpinions(senators, briber.ID, LawBribeExposeOp, TraitIncorruptible, TraitStern)
}

// lawOpinions is delta from every senator with one of traits toward a player.
func lawOpinions(senators []Senator, charID int64, delta float64, traits ...Trait) []OpinionChange {
	var out []OpinionChange
	for _, s := range senators {
		for _, t := range traits {
			if s.Has(t) {
				out = append(out, OpinionChange{SenatorID: s.ID, CharacterID: charID, Delta: delta})
				break
			}
		}
	}
	return out
}

// FranchiseWeight is the vote weight an estate is worth under the Landed
// Franchise, with diminishing returns (EstateScore).
func FranchiseWeight(estateValue float64, ls Laws) float64 {
	if per := ls.Rules().FranchisePerScore; per != 0 {
		return per * EstateScore(estateValue)
	}
	return 0
}

// IncomeUnder is a player's gold per day under the laws: Property Tax on
// every property but the residence (propertyPrices), then Debased Coinage
// on what's left. It never goes below zero.
func IncomeUnder(income int, propertyPrices []int, ls Laws) int {
	r := ls.Rules()
	if r.PropertyTaxRate != 0 {
		income -= propertyTax(propertyPrices, r.PropertyTaxRate)
	}
	if r.Income != 0 && income > 0 {
		income = int(float64(income) * r.Income)
	}
	return max(0, income)
}

// PropertyTax is the daily tax on these properties under the Property Tax.
func PropertyTax(prices []int) int { return propertyTax(prices, LawPropertyTaxRate) }

func propertyTax(prices []int, rate float64) int {
	tax := 0.0
	for _, p := range prices {
		tax += float64(p) * rate
	}
	return int(math.Round(tax))
}

// PermitFee is what Martial Law charges to start a public gathering.
func PermitFee(ls Laws) int { return ls.Rules().PermitFee }

// ChallengeCost is what issuing a challenge costs under the Ban on Dueling.
func ChallengeCost(senators []Senator, challengerID int64, ls Laws) (Delta, []OpinionChange) {
	r := ls.Rules()
	if r.ChallengeRenown == 0 && r.ChallengeOpinion == 0 {
		return Delta{}, nil
	}
	return Delta{Renown: -r.ChallengeRenown}, lawOpinions(senators, challengerID, r.ChallengeOpinion, TraitStern)
}

// DeclineCost is what refusing a challenge costs under the Code of Honor.
func DeclineCost(ls Laws) Delta {
	if d := ls.Rules().DeclineRenown; d != 0 {
		return Delta{Renown: -d}
	}
	return Delta{}
}

// IsCrime reports whether a secret of this kind is a crime under the laws,
// so exposing it takes it to the magistrates (see misdeeds.go).
func IsCrime(kind string, ls Laws) bool { return ls.Rules().IsCrime(kind) }
