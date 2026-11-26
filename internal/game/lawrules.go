package game

// LawEffects are the everyday rules a law bends, as data. A new law that
// only changes numbers needs a LawDefs entry, not code in every rule it
// touches. Zero means "no effect"; factors multiply when several laws set
// them, amounts add up, and flags combine with or.
type LawEffects struct {
	// The Senate.
	SecretBallot      bool              // nobody's vote is revealed
	AbstainInfluence  float64           // influence for sitting out a vote (through ScaleInfluence)
	FranchisePerScore float64           // vote weight per point of EstateScore
	ThemeCheck        map[Theme]float64 // percent added to Senate rolls on matters of a theme
	BribePrice        float64           // factor on every bribe
	BribesExposed     bool              // a bribe may come out (BribeExposure)

	// Money.
	ShopPrices      float64 // factor on shop prices
	Income          float64 // factor on every player's gold/day
	PropertyTaxRate float64 // share of each property's price (but the residence) taxed daily
	PermitFee       int     // gold to start a party, speech or circus

	// Magic.
	MagicSellBack float64 // what the shop pays for magic items, as a share of the price
	WitchHunt     bool    // magic items risk a fine each Senate day

	// Rumors.
	RumorNotice float64 // percent added to notice a rumor
	SlanderFine int     // gold a named rumor-monger pays their victim

	// Duels.
	ChallengeRenown  float64 // renown a challenger loses for issuing one
	ChallengeOpinion float64 // Stern senators, toward the challenger
	NoDeclineShame   bool    // nobody minds a refused challenge
	DuelWinRenown    float64 // factor on the winner's renown
	DeclineRenown    float64 // renown lost for refusing a challenge
	DeclineOpinion   float64 // Hawk, Stern and Conservative senators, on a refusal
	BetCut           float64 // share of every winning bet the court takes

	// Industry.
	MonopolyLeader float64 // factor on the biggest player stake in each sector
	MonopolyRest   float64 // factor on every other stake

	// Spies.
	LeakChance    float64 // factor on spy leak chances
	LeakDetail    int     // levels of detail added to a leak (up to Full)
	RootBonus     float64 // percent added to Root Out Spies
	PaysInformers bool    // denunciations hit harder and the treasury pays for them

	// Crimes are the secret kinds this law makes crimes (see IsCrime).
	Crimes []string
}

// Rules are the effects of every law in force, folded into one.
type Rules = LawEffects

// Rules folds the effects of every law in force.
func (ls Laws) Rules() Rules {
	var r Rules
	for _, l := range ls {
		if d, ok := LawByKey(l.Key); ok {
			r = r.with(d.Effects)
		}
	}
	return r
}

// with combines two sets of effects.
func (r Rules) with(e LawEffects) Rules {
	r.SecretBallot = r.SecretBallot || e.SecretBallot
	r.AbstainInfluence += e.AbstainInfluence
	r.FranchisePerScore += e.FranchisePerScore
	for th, v := range e.ThemeCheck {
		if r.ThemeCheck == nil {
			r.ThemeCheck = map[Theme]float64{}
		}
		r.ThemeCheck[th] += v
	}
	r.BribePrice = mulFactor(r.BribePrice, e.BribePrice)
	r.BribesExposed = r.BribesExposed || e.BribesExposed

	r.ShopPrices = mulFactor(r.ShopPrices, e.ShopPrices)
	r.Income = mulFactor(r.Income, e.Income)
	r.PropertyTaxRate += e.PropertyTaxRate
	r.PermitFee += e.PermitFee

	r.MagicSellBack = max(r.MagicSellBack, e.MagicSellBack)
	r.WitchHunt = r.WitchHunt || e.WitchHunt

	r.RumorNotice += e.RumorNotice
	r.SlanderFine += e.SlanderFine

	r.ChallengeRenown += e.ChallengeRenown
	r.ChallengeOpinion += e.ChallengeOpinion
	r.NoDeclineShame = r.NoDeclineShame || e.NoDeclineShame
	r.DuelWinRenown = mulFactor(r.DuelWinRenown, e.DuelWinRenown)
	r.DeclineRenown += e.DeclineRenown
	r.DeclineOpinion += e.DeclineOpinion
	r.BetCut += e.BetCut

	r.MonopolyLeader = mulFactor(r.MonopolyLeader, e.MonopolyLeader)
	r.MonopolyRest = mulFactor(r.MonopolyRest, e.MonopolyRest)

	r.LeakChance = mulFactor(r.LeakChance, e.LeakChance)
	r.LeakDetail += e.LeakDetail
	r.RootBonus += e.RootBonus
	r.PaysInformers = r.PaysInformers || e.PaysInformers

	r.Crimes = append(r.Crimes, e.Crimes...)
	return r
}

// mulFactor multiplies two factors where 0 means "none".
func mulFactor(a, b float64) float64 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	}
	return a * b
}

// Monopolies reports whether stakes are weighed by the Charter of Monopolies.
func (r Rules) Monopolies() bool { return r.MonopolyLeader != 0 || r.MonopolyRest != 0 }

// IsCrime reports whether a secret of this kind is a crime under these rules.
func (r Rules) IsCrime(kind string) bool {
	if m, ok := MisdeedDefs[kind]; ok && m.AlwaysCrime {
		return true
	}
	for _, k := range r.Crimes {
		if k == kind {
			return true
		}
	}
	return false
}
