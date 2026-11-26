package game

import "time"

// Timings gathers every real-time duration in the game, so fast mode for
// manual testing is one switch.
type Timings struct {
	Party    PartyTiming
	Senate   SenateTiming
	Court    time.Duration // Court a Senator
	Invest   time.Duration // Invest in a Venture
	Duel     DuelTiming
	Hunt     PartyTiming   // a hunt runs like a party: duration and host events
	Day      time.Duration // a "day" for conditions like Wounded
	Spy      time.Duration // Plant Spies
	RootOut  time.Duration // Root Out Spies
	Shop     time.Duration // how often the rotating market changes
	Canvass  time.Duration // Canvass the Districts
	Speech   time.Duration // Give a Speech in the Forum
	Circus   time.Duration // Bread and Circuses
	Embezzle time.Duration // Embezzle from the Treasury
	Petition time.Duration // Petition the Senate
	Journey  PartyTiming   // Undertake a Journey (abroad takes twice as long)
}

var (
	DefaultTimings = Timings{
		Party: DefaultPartyTiming, Senate: DefaultSenateTiming,
		Court: DefaultCourtDuration, Invest: DefaultInvestDuration,
		Duel: DefaultDuelTiming, Hunt: DefaultPartyTiming, Day: 24 * time.Hour,
		Spy: DefaultPlantDuration, RootOut: DefaultRootOutDuration,
		Shop:    24 * time.Hour,
		Canvass: DefaultCanvassDuration, Speech: DefaultSpeechDuration, Circus: DefaultCircusDuration,
		Embezzle: DefaultEmbezzleDuration,
		Petition: DefaultPetitionDuration,
		Journey:  DefaultJourneyTiming,
	}
	FastTimings = Timings{
		Party: FastPartyTiming, Senate: FastSenateTiming,
		Court: FastCourtDuration, Invest: FastInvestDuration,
		Duel: FastDuelTiming, Hunt: FastPartyTiming, Day: 2 * time.Minute,
		Spy: FastPlantDuration, RootOut: FastRootOutDuration,
		Shop:    10 * time.Minute,
		Canvass: FastCanvassDuration, Speech: FastSpeechDuration, Circus: FastCircusDuration,
		Embezzle: FastEmbezzleDuration,
		Petition: FastPetitionDuration,
		Journey:  FastJourneyTiming,
	}
)
