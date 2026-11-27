package game

import "strings"

// Happenings are things abroad in the realm for a few days: a manticore in
// the forest, a troll under a bridge, a circus in town. One may begin on any
// Senate day. While it lasts, hunts, journeys and parties may run into it
// (its event takes the place of one of their usual DM events), and some
// outcomes end it for good: the beast is slain, the troll driven off.
// Placeholder numbers — tune freely.

const (
	// HappeningChance is the chance a new happening begins on a Senate day.
	HappeningChance = 0.4
	// MaxHappenings is how many can be abroad at once.
	MaxHappenings = 2
	// HappeningEncounter is the chance an activity a happening touches runs
	// into it, in place of one of its usual events.
	HappeningEncounter = 0.5
)

// Where a happening can be met. Journeys are split: some things are only
// met at home, some only abroad.
const (
	MeetHunt          = KindHunt
	MeetParty         = KindParty
	MeetJourney       = KindJourney // journeys in the realm
	MeetJourneyAbroad = "journey_abroad"
)

// HappeningDef is one thing that can be abroad in the realm.
type HappeningDef struct {
	Key      string
	Name     string // "A Manticore in the Royal Forest"
	Emoji    string
	Announce string // posted when it begins
	Ended    string // posted when someone ends it; %s is their name
	Fade     string // posted when it fades on its own
	Hint     string // a line on the panels it touches
	Weight   float64
	MinDays  int
	MaxDays  int
	Sectors  map[SectorKey]SectorChange // a shock when it begins
	Realm    Realm                      // tracker changes when it begins
	Trophy   string                     // an item the one who ends it takes home ("" none)
	Feat     string                     // the title the one who ends it earns (titles.go)
	Events   map[string]PartyEvent      // by where it's met (Meet…)
}

// Meets reports where the happening can be met, in a fixed order.
func (h HappeningDef) Meets() []string {
	var out []string
	for _, m := range []string{MeetHunt, MeetJourney, MeetJourneyAbroad, MeetParty} {
		if _, ok := h.Events[m]; ok {
			out = append(out, m)
		}
	}
	return out
}

// happeningEventKey is the event key a happening's event is stored under.
func happeningEventKey(h, where string) string { return "hap_" + h + "_" + where }

// HappeningOfEvent is the happening an event belongs to, if any.
func HappeningOfEvent(eventKey string) (HappeningDef, bool) {
	if !strings.HasPrefix(eventKey, "hap_") {
		return HappeningDef{}, false
	}
	for _, h := range HappeningDefs {
		if strings.HasPrefix(eventKey, "hap_"+h.Key+"_") {
			return h, true
		}
	}
	return HappeningDef{}, false
}

func HappeningByKey(k string) (HappeningDef, bool) { return happeningIndex.get(k) }

// PickHappening picks a happening to begin, weighted, never one already
// abroad nor the one that ended last. ok is false if none can.
func PickHappening(r Roller, active []string, last string) (HappeningDef, bool) {
	var pool []HappeningDef
	total := 0.0
	for _, h := range HappeningDefs {
		if h.Key == last || contains(active, h.Key) {
			continue
		}
		pool = append(pool, h)
		total += h.Weight
	}
	if total <= 0 {
		return HappeningDef{}, false
	}
	x := float64(r.IntN(10000)) / 10000 * total
	for _, h := range pool {
		if x -= h.Weight; x < 0 {
			return h, true
		}
	}
	return pool[len(pool)-1], true
}

// HappeningDays is how many game days a happening lasts.
func HappeningDays(r Roller, h HappeningDef) int {
	return h.MinDays + r.IntN(h.MaxDays-h.MinDays+1)
}

// HappeningEvents are the events of the active happenings met at where.
func HappeningEvents(where string, active []string) []PartyEvent {
	var out []PartyEvent
	for _, k := range active {
		if h, ok := HappeningByKey(k); ok {
			if ev, ok := h.Events[where]; ok {
				out = append(out, ev)
			}
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- the happenings ----

// slay is an outcome that ends the happening.
func slay(text string, renown float64, quality float64) EventOutcome {
	return EventOutcome{Text: text, Quality: quality, Delta: Delta{Renown: renown, Stats: Stats{Prowess: 1}}, Ends: true}
}

func check2(base float64, a Attr, wa float64, b Attr, wb float64) *Check {
	return &Check{Base: base, Weights: map[Attr]float64{a: wa, b: wb}}
}

var HappeningDefs = []HappeningDef{
	{
		Key: "manticore", Name: "A Manticore in the Royal Forest", Emoji: "🦁", Weight: 1, MinDays: 3, MaxDays: 5,
		Feat:     "Manticore-Slayer",
		Announce: "Woodcutters swear they have seen it: a lion's body, a man's face, a tail of poisoned spikes. **A manticore** hunts in the royal forest. Whoever kills it will never buy their own wine again.",
		Ended:    "⚔️ **%s** has slain the manticore of the royal forest! Its spiked tail hangs in their hall.",
		Fade:     "The manticore has not been seen for days. Perhaps it has gone back to whatever desert bred it.",
		Hint:     "🦁 A manticore stalks the royal forest. A hunt may find it.",
		Trophy:   "manticore_tail",
		Events: map[string]PartyEvent{
			MeetHunt: {Title: "The manticore",
				Prompt: "The hounds go silent. In the clearing ahead, something with a lion's body and a man's sad face lifts its spiked tail.",
				Choices: []EventChoice{
					{Key: "fight", Label: "Charge it", Check: check2(10, AttrProwess, 5, AttrMartial, 2),
						Success: slay("Your spear finds its heart as the tail lashes past your ear. The manticore is dead, and you killed it.", 40, 20),
						Failure: EventOutcome{Text: "A spike takes you in the shoulder. Your riders drag you clear as it vanishes into the trees.", Quality: -12, Condition: CondWounded}},
					{Key: "nets", Label: "Drive it into nets", Check: chk(20, AttrMartial, 5),
						Success: slay("Beaters, nets and a dozen spears. It dies tangled and screaming, and the glory is shared, mostly yours.", 30, 15),
						Failure: EventOutcome{Text: "It tears through the nets and two beaters. The hunt is over.", Quality: -10}},
					{Key: "leave", Label: "Call off the hunt",
						Success: EventOutcome{Text: "You ride home with nothing. Wise, some say.", Quality: -4}},
				},
				Default: "leave"},
			MeetJourney: {Title: "The manticore on the road",
				Prompt: "At dusk, something follows your party through the trees, its tail rattling like dry seed pods.",
				Choices: []EventChoice{
					{Key: "fight", Label: "Turn and fight", Check: check2(5, AttrProwess, 5, AttrMartial, 2),
						Success: slay("You stand your ground and kill it on the road. Every inn you pass will tell the story.", 40, 15),
						Failure: EventOutcome{Text: "It takes a horse and nearly takes you.", Quality: -12, Condition: CondWounded}},
					{Key: "flee", Label: "Ride for the nearest town",
						Success: EventOutcome{Text: "You ride all night and arrive shaking, but whole.", Quality: -3}},
				},
				Default: "flee"},
		},
	},
	{
		Key: "wyvern", Name: "A Wyvern over the Western Hills", Emoji: "🐉", Weight: 1, MinDays: 3, MaxDays: 5,
		Feat:     "Wyvern-Slayer",
		Announce: "A **wyvern** circles over the western hills, taking sheep and the occasional shepherd. The miners won't go up to the workings.",
		Ended:    "⚔️ **%s** has brought down the wyvern of the western hills! The miners are going back up.",
		Fade:     "The wyvern has flown north. The miners count their sheep and go back to work.",
		Hint:     "🐉 A wyvern hunts over the western hills.",
		Sectors:  map[SectorKey]SectorChange{SectorMining: {Health: -0.2}},
		Trophy:   "wyvern_skull",
		Events: map[string]PartyEvent{
			MeetJourney: {Title: "The wyvern",
				Prompt: "A shadow passes over the road, and then the wyvern drops out of the sun.",
				Choices: []EventChoice{
					{Key: "fight", Label: "Stand and fight", Check: check2(5, AttrProwess, 4, AttrMartial, 3),
						Success: slay("Your crossbowmen hole its wings and you finish it on the ground. A dragon-slayer, near enough.", 35, 15),
						Failure: EventOutcome{Text: "It carries off a pack horse and leaves you burned and bleeding.", Quality: -12, Condition: CondWounded}},
					{Key: "fire", Label: "Drive it off with fire", Check: chk(25, AttrMartial, 4),
						Success: EventOutcome{Text: "Torches and shouting. It wheels away to find easier meat.", Quality: 4},
						Failure: EventOutcome{Text: "It doesn't care about your torches.", Quality: -8}},
					{Key: "hide", Label: "Take cover in the rocks",
						Success: EventOutcome{Text: "You wait it out under an overhang. Hours lost, nobody hurt.", Quality: -3}},
				},
				Default: "hide"},
			MeetHunt: {Title: "The wyvern's lair",
				Prompt: "Your huntsman points to a cave high on the ridge, littered with bones. Something large is breathing inside.",
				Choices: []EventChoice{
					{Key: "smoke", Label: "Smoke it out and fight", Check: check2(5, AttrProwess, 4, AttrMartial, 3),
						Success: slay("It comes out angry and half-blind, and dies on your spears.", 35, 20),
						Failure: EventOutcome{Text: "It comes out angry, and it is you who runs.", Quality: -12, Condition: CondWounded}},
					{Key: "leave", Label: "Let sleeping wyverns lie",
						Success: EventOutcome{Text: "You hunt elsewhere. Nobody argues.", Quality: -2}},
				},
				Default: "leave"},
		},
	},
	{
		Key: "white_hart", Name: "The White Hart", Emoji: "🦌", Weight: 0.8, MinDays: 2, MaxDays: 3,
		Feat:     "Friend of the White Hart",
		Announce: "Foresters have seen **the White Hart**, the stag of the old stories, pale as frost. They say whoever catches it will be remembered forever. They say a lot of things.",
		Ended:    "🦌 **%s** has run down the White Hart! The court is already writing songs.",
		Fade:     "The White Hart is gone, as it always goes, back into the stories.",
		Hint:     "🦌 The White Hart has been seen in the forest.",
		Events: map[string]PartyEvent{
			MeetHunt: {Title: "The White Hart",
				Prompt: "Between the birches stands a stag as white as new snow, watching you. Your riders go very quiet.",
				Choices: []EventChoice{
					{Key: "chase", Label: "Give chase", Check: check2(10, AttrProwess, 4, AttrRenown, 2),
						Success: slay("A day's ride, and at the end of it the hart lets you lay a hand on its neck before it vanishes. You will be remembered.", 25, 20),
						Failure: EventOutcome{Text: "You ride until your horse is blown. It was never really there.", Quality: -6}},
					{Key: "bow", Label: "Bow your head and let it go",
						Success: EventOutcome{Text: "It dips its antlers to you and walks away. The rest of the hunt goes strangely well.", Quality: 12, Delta: Delta{Renown: 3}}},
				},
				Default: "bow"},
		},
	},
	{
		Key: "boar", Name: "The Erymanthian Boar", Emoji: "🐗", Weight: 1, MinDays: 3, MaxDays: 4,
		Feat:     "Tamer of the Erymanthian Boar",
		Announce: "A **boar the size of an ox** is tearing up the farms east of the city. The farmers call it the Erymanthian Boar, and want a Hercules.",
		Ended:    "🐗 **%s** has taken the Erymanthian Boar! The eastern farms are safe.",
		Fade:     "The great boar has gone back into the mountains. The farmers replant.",
		Hint:     "🐗 The Erymanthian Boar is trampling the eastern farms.",
		Sectors:  map[SectorKey]SectorChange{SectorAgriculture: {Health: -0.15}},
		Events: map[string]PartyEvent{
			MeetHunt: {Title: "The Erymanthian Boar",
				Prompt: "The ground shakes. The great boar bursts from a thicket, tusks like scythes.",
				Choices: []EventChoice{
					{Key: "alive", Label: "Drive it into the snow and take it alive", Check: chk(10, AttrMartial, 6),
						Success: slay("You run it into a drift and bind it, as Hercules did. You parade it through the city gate.", 35, 20),
						Failure: EventOutcome{Text: "It turns in the snow and gores your best horse.", Quality: -10, Condition: CondWounded}},
					{Key: "kill", Label: "Kill it", Check: chk(15, AttrProwess, 6),
						Success: slay("One clean thrust behind the shoulder. The feast that night is talked about for years.", 25, 15),
						Failure: EventOutcome{Text: "Your spear snaps. You are lucky to keep your legs.", Quality: -10, Condition: CondWounded}},
					{Key: "leave", Label: "Let it pass",
						Success: EventOutcome{Text: "It thunders past and away. Your guests are pale but grateful.", Quality: -3}},
				},
				Default: "leave"},
		},
	},
	{
		Key: "troll", Name: "A Troll under the North Bridge", Emoji: "🧌", Weight: 1, MinDays: 3, MaxDays: 5,
		Feat:     "Who Freed the North Bridge",
		Announce: "A **troll** has moved in under the north bridge and charges a toll to cross: gold, a goat, or a riddle it can't answer. Caravans are going the long way round.",
		Ended:    "🧌 **%s** has rid the north bridge of its troll! The caravans are crossing again.",
		Fade:     "The troll under the north bridge has wandered off, or turned to stone at dawn. Nobody is sure which.",
		Hint:     "🧌 A troll guards the north bridge.",
		Sectors:  map[SectorKey]SectorChange{SectorTrade: {Health: -0.1}},
		Events: map[string]PartyEvent{
			MeetJourney: {Title: "The troll under the bridge",
				Prompt: "A grey hand the size of a cart wheel grips the bridge rail. \"Toll,\" says the troll. \"Gold, or a riddle. Or I eat a horse.\"",
				Choices: []EventChoice{
					{Key: "pay", Label: "Pay the toll (25 gold)",
						Success: EventOutcome{Text: "It bites the coins, grunts, and lets you pass.", Quality: 0, Delta: Delta{Wealth: -25}}},
					{Key: "riddle", Label: "Ask it a riddle", Check: chk(20, AttrIntrigue, 6),
						Success: slay("\"What have I got in my pocket?\" It thinks until sunrise, and turns to stone. The bridge is free.", 20, 12),
						Failure: EventOutcome{Text: "It answers at once and eats your best horse.", Quality: -8}},
					{Key: "fight", Label: "Fight it", Check: check2(0, AttrProwess, 5, AttrMartial, 3),
						Success: slay("It goes into the river with your spear in it and doesn't come up.", 30, 12),
						Failure: EventOutcome{Text: "It throws you into the river instead.", Quality: -10, Condition: CondWounded}},
				},
				Default: "pay"},
		},
	},
	{
		Key: "grendel", Name: "Grendel Stalks the Feast Halls", Emoji: "👹", Weight: 0.8, MinDays: 2, MaxDays: 4,
		Feat:     "Bane of Grendel",
		Announce: "Something came to a feast in the upper city last night and left with three guests. Nobody saw it clearly, but the old soldiers have a name for it: **Grendel**. Hosts are barring their doors.",
		Ended:    "👹 **%s** has torn the arm from Grendel in their own feast hall! The city can party again.",
		Fade:     "Grendel has gone back to its fen. The feast halls light their lamps again.",
		Hint:     "👹 Grendel stalks the feast halls. Party at your peril.",
		Events: map[string]PartyEvent{
			MeetParty: {Title: "Grendel at the door",
				Prompt: "The music stops. The great doors bulge inward, and a grey, clawed thing steps into your hall.",
				Choices: []EventChoice{
					{Key: "wrestle", Label: "Wrestle it barehanded", Check: chk(0, AttrProwess, 7),
						Success: slay("You grip its arm and do not let go until it tears free. Grendel flees to die in its fen. Your hall will be in songs.", 40, 25),
						Failure: EventOutcome{Text: "It throws you across the hall and takes a guest with it.", Quality: -20, Condition: CondWounded}},
					{Key: "bar", Label: "Bar the inner doors", Check: chk(20, AttrMartial, 5),
						Success: EventOutcome{Text: "Your guards hold the doors until dawn. A long night, but everyone lives.", Quality: -4}},
					{Key: "flee", Label: "Everyone out the back",
						Success: EventOutcome{Text: "The party is over. Grendel eats the roast.", Quality: -15}},
				},
				Default: "bar"},
		},
	},
	{
		Key: "horseman", Name: "The Headless Horseman", Emoji: "🎃", Weight: 0.8, MinDays: 2, MaxDays: 3,
		Feat:     "Who Outrode the Headless Horseman",
		Announce: "Travellers on the night roads tell of a rider in a black cloak, with a carved pumpkin where his head should be. **The Headless Horseman** rides.",
		Ended:    "🎃 **%s** has outridden the Headless Horseman to the bridge, and he is gone!",
		Fade:     "No one has seen the Headless Horseman for nights. The roads are only dangerous in the usual ways.",
		Hint:     "🎃 The Headless Horseman rides the night roads.",
		Events: map[string]PartyEvent{
			MeetJourney: {Title: "The Headless Horseman",
				Prompt: "Hoofbeats behind you in the dark. You turn, and the rider behind you has no head, only a grinning pumpkin held high.",
				Choices: []EventChoice{
					{Key: "race", Label: "Race him to the bridge", Check: chk(10, AttrProwess, 6),
						Success: slay("You cross the bridge a length ahead. He cannot follow, and with a scream he is gone for good.", 25, 12),
						Failure: EventOutcome{Text: "The pumpkin hits you square in the back. You wake in a ditch.", Quality: -10, Condition: CondWounded}},
					{Key: "polite", Label: "Offer him a better pumpkin", Check: chk(20, AttrDiplomacy, 5),
						Success: EventOutcome{Text: "He considers it, bows from the neck down, and rides off. Oddly polite.", Quality: 6, Delta: Delta{Renown: 2}},
						Failure: EventOutcome{Text: "He takes offense, and your pumpkin.", Quality: -6}},
					{Key: "turn", Label: "Turn back",
						Success: EventOutcome{Text: "You spend the night in a barn and arrive a day late.", Quality: -4}},
				},
				Default: "turn"},
		},
	},
	{
		Key: "piper", Name: "The Pied Piper Comes to Town", Emoji: "🐀", Weight: 0.8, MinDays: 2, MaxDays: 3,
		Feat:     "Who Piped the Rats Away",
		Announce: "Rats are pouring out of the granaries, and a stranger in a coat of many colors has arrived, offering to pipe them away for a price. **The Pied Piper** is in town.",
		Ended:    "🐀 **%s** has paid the Pied Piper his price, and he has led every rat in the city into the river.",
		Fade:     "The Pied Piper has moved on, unpaid. The rats have not. Parents are keeping an eye on their children.",
		Hint:     "🐀 The Pied Piper is in town, and so are the rats.",
		Realm:    Realm{AttrPopularity: -3},
		Events: map[string]PartyEvent{
			MeetParty: {Title: "The Pied Piper",
				Prompt: "Rats are running along your banquet tables. A man in a patchwork coat bows at the door: \"For a fair price, I'll clear them.\"",
				Choices: []EventChoice{
					{Key: "pay", Label: "Pay him (40 gold)",
						Success: EventOutcome{Text: "He plays a tune, and every rat in your hall, and then the city, follows him to the river.", Quality: 12, Delta: Delta{Wealth: -40, Renown: 10}, Ends: true}},
					{Key: "refuse", Label: "Refuse",
						Success: EventOutcome{Text: "He shrugs and plays on in the street. The rats stay for dessert.", Quality: -8}},
					{Key: "cheat", Label: "Promise to pay, then don't", Check: chk(20, AttrIntrigue, 5),
						Success: EventOutcome{Text: "He clears the hall, and you slip away without paying. He gives you a long look.", Quality: 10},
						Failure: EventOutcome{Text: "He plays a different tune. Half your guests follow him out of the door and don't come back.", Quality: -20}},
				},
				Default: "refuse"},
		},
	},
	{
		Key: "kraken", Name: "The Kraken off the Coast", Emoji: "🌊", Weight: 0.8, MinDays: 3, MaxDays: 5,
		Feat:     "Kraken-Slayer",
		Announce: "Two merchantmen have gone down in calm water, dragged under by arms as long as masts. **The Kraken** has risen off the coast. No captain will sail.",
		Ended:    "🌊 **%s** has driven the Kraken back into the deep! The harbor is open again.",
		Fade:     "The Kraken has sunk back into the deep. The captains sail again, carefully.",
		Hint:     "🌊 The Kraken haunts the sea lanes. Journeys abroad must cross them.",
		Sectors:  map[SectorKey]SectorChange{SectorTrade: {Health: -0.2}, SectorTimber: {Health: 0.1}},
		Events: map[string]PartyEvent{
			MeetJourneyAbroad: {Title: "The Kraken",
				Prompt: "The sea boils beside the ship, and a grey arm thick as an oak wraps around the bow.",
				Choices: []EventChoice{
					{Key: "fight", Label: "Axes to the rail!", Check: check2(0, AttrProwess, 4, AttrMartial, 4),
						Success: slay("You hack through the arm and the beast sinks, screaming, into the deep. The sailors carry you off the ship on their shoulders.", 40, 15),
						Failure: EventOutcome{Text: "It takes the cargo and half the crew. You lose most of your purse to the sea.", Quality: -15, Delta: Delta{Wealth: -40}, Condition: CondWounded}},
					{Key: "outsail", Label: "Crowd on sail and run", Check: chk(20, AttrMartial, 5),
						Success: EventOutcome{Text: "The wind is with you. You leave it thrashing in your wake.", Quality: 2},
						Failure: EventOutcome{Text: "It holds the ship for an hour before it lets go. Your cargo is gone.", Quality: -10, Delta: Delta{Wealth: -25}}},
					{Key: "pray", Label: "Pray",
						Success: EventOutcome{Text: "It lets go, for reasons of its own.", Quality: -4}},
				},
				Default: "pray"},
		},
	},
	{
		Key: "werewolf", Name: "The Werewolf of the Ashwood", Emoji: "🐺", Weight: 1, MinDays: 2, MaxDays: 4,
		Feat:     "Bane of the Ashwood Werewolf",
		Announce: "The moon is full, and in the Ashwood something howls that is not a wolf. Two charcoal-burners are dead. The priests are blessing silver. **A werewolf** walks.",
		Ended:    "🐺 **%s** has put a silver arrow through the werewolf of the Ashwood! The charcoal-burners can sleep.",
		Fade:     "The moon wanes, and the werewolf of the Ashwood is only someone, somewhere, with a bad conscience.",
		Hint:     "🐺 A werewolf walks the Ashwood under the full moon.",
		Events: map[string]PartyEvent{
			MeetHunt: {Title: "The werewolf",
				Prompt: "Under the full moon, something tall and grey and almost a man watches your camp from the tree line.",
				Choices: []EventChoice{
					{Key: "silver", Label: "Silver arrows (30 gold)", Check: chk(10, AttrProwess, 6),
						Success: EventOutcome{Text: "The arrow takes it in the heart. In the morning, the body is a man's, and nobody you know. You're a hero.", Quality: 18, Delta: Delta{Wealth: -30, Renown: 30, Stats: Stats{Prowess: 1}}, Ends: true},
						Failure: EventOutcome{Text: "You miss, and it doesn't.", Quality: -12, Delta: Delta{Wealth: -30}, Condition: CondWounded}},
					{Key: "fire", Label: "Build up the fires and wait for dawn",
						Success: EventOutcome{Text: "A long night of howling. At dawn it's gone.", Quality: -4}},
				},
				Default: "fire"},
			MeetJourney: {Title: "Howling on the road",
				Prompt: "A howl, very close, and then another, closer. Your horses are terrified.",
				Choices: []EventChoice{
					{Key: "fight", Label: "Stand and fight", Check: check2(0, AttrProwess, 5, AttrMartial, 2),
						Success: slay("It leaps; your blade is quicker. In the morning, the body is a man's.", 30, 12),
						Failure: EventOutcome{Text: "It drags down your guard captain and nearly you.", Quality: -12, Condition: CondWounded}},
					{Key: "ride", Label: "Ride hard for home",
						Success: EventOutcome{Text: "You ride until the howling fades. Your horses will never be the same.", Quality: -3}},
				},
				Default: "ride"},
		},
	},
	{
		Key: "circus", Name: "The Night Circus Comes to Town", Emoji: "🎪", Weight: 0.8, MinDays: 2, MaxDays: 3,
		Announce: "Black and white tents have appeared overnight outside the city walls. **The Night Circus** is open from dusk to dawn, and the whole city is going.",
		Fade:     "One morning the black and white tents are simply gone. The city talks about nothing else for a week.",
		Hint:     "🎪 The Night Circus is in town.",
		Realm:    Realm{AttrPopularity: 3},
		Events: map[string]PartyEvent{
			MeetParty: {Title: "The Night Circus",
				Prompt: "Performers in black and white arrive at your door: fire-eaters, an illusionist, a contortionist. \"We heard there was a party.\"",
				Choices: []EventChoice{
					{Key: "hire", Label: "Hire them for the night (40 gold)",
						Success: EventOutcome{Text: "Fire, illusion and wonders. Nobody will forget your party.", Quality: 18, Delta: Delta{Wealth: -40}}},
					{Key: "free", Label: "Invite them in as guests", Check: chk(20, AttrDiplomacy, 5),
						Success: EventOutcome{Text: "They perform for the joy of it. A night of wonders, free.", Quality: 14},
						Failure: EventOutcome{Text: "They're offended, and leave with the silverware.", Quality: -8}},
					{Key: "turn_away", Label: "Turn them away",
						Success: EventOutcome{Text: "They bow and vanish into the dark. Your guests look wistfully after them.", Quality: -4}},
				},
				Default: "turn_away"},
		},
	},
	{
		Key: "caravan", Name: "Marco Polo's Caravan from Cathay", Emoji: "🐫", Weight: 0.8, MinDays: 2, MaxDays: 3,
		Announce: "A caravan half a mile long is coming up the eastern road: silk, spices and porcelain from Cathay, led by a Venetian named **Marco Polo**, who has stories to tell.",
		Fade:     "Marco Polo's caravan has sold its last silk and gone home to Venice to write a book about it.",
		Hint:     "🐫 Marco Polo's caravan is on the eastern road.",
		Sectors:  map[SectorKey]SectorChange{SectorTrade: {Health: 0.15}, SectorLuxury: {Health: 0.1}},
		Events: map[string]PartyEvent{
			MeetJourney: {Title: "Marco Polo's caravan",
				Prompt: "You fall in with the great caravan from Cathay. Marco Polo invites you to share his fire.",
				Choices: []EventChoice{
					{Key: "trade", Label: "Trade with him", Check: chk(20, AttrStewardship, 6),
						Success: EventOutcome{Text: "Hard bargaining under the stars. You come out ahead.", Quality: 10, Delta: Delta{Wealth: 40}},
						Failure: EventOutcome{Text: "He is a Venetian. You come out behind.", Quality: -2, Delta: Delta{Wealth: -15}}},
					{Key: "stories", Label: "Listen to his stories",
						Success: EventOutcome{Text: "Kublai Khan, paper money, stones that burn. You come home full of tales the court can't get enough of.", Quality: 8, Delta: Delta{Influence: 1}}},
				},
				Default: "stories"},
		},
	},
	{
		Key: "feast_day", Name: "A Saint's Feast Day", Emoji: "⭐", Weight: 0.8, MinDays: 1, MaxDays: 2,
		Announce: "It is **Saint Crispin's Day**. The bells ring, the bakers give away bread, and every house in the city is throwing open its doors.",
		Fade:     "The feast day is over. The city sweeps up and goes back to work.",
		Hint:     "⭐ It's a feast day: the city is in the mood for a party.",
		Realm:    Realm{AttrPopularity: 2},
		Events: map[string]PartyEvent{
			MeetParty: {Title: "Saint Crispin's Day",
				Prompt: "The bells are ringing, and a crowd of poor families has gathered outside your gate, as is the custom on a feast day.",
				Choices: []EventChoice{
					{Key: "alms", Label: "Give alms (25 gold)",
						Success: EventOutcome{Text: "Bread and coin for everyone at the gate. The whole street sings your name.", Quality: 10, Delta: Delta{Wealth: -25, Renown: 5}}},
					{Key: "invite", Label: "Invite them in", Check: chk(25, AttrDiplomacy, 5),
						Success: EventOutcome{Text: "Lords and laborers at one table. The most talked-about party of the year.", Quality: 14, Delta: Delta{Renown: 3}},
						Failure: EventOutcome{Text: "Your noble guests are not amused.", Quality: -8}},
					{Key: "ignore", Label: "Keep the gate shut",
						Success: EventOutcome{Text: "The crowd drifts off to a more generous house.", Quality: -4}},
				},
				Default: "ignore"},
		},
	},
	{
		Key: "mummers", Name: "The Bloody Mummers Ride", Emoji: "🗡️", Weight: 0.8, MinDays: 3, MaxDays: 4,
		Feat:     "Breaker of the Bloody Mummers",
		Announce: "**The Bloody Mummers**, sellswords turned brigands, are riding the roads in their motley, burning farms and robbing anyone they meet. The roads are not safe.",
		Ended:    "🗡️ **%s** has scattered the Bloody Mummers! The roads are safe again, or as safe as they get.",
		Fade:     "The Bloody Mummers have taken their loot and gone to sell their swords to someone else.",
		Hint:     "🗡️ The Bloody Mummers are riding the roads.",
		Realm:    Realm{AttrStability: -3},
		Events: map[string]PartyEvent{
			MeetJourney: {Title: "The Bloody Mummers",
				Prompt: "Twenty riders in motley and stolen armor block the road. Their leader grins with too few teeth. \"Your purse, or your ears.\"",
				Choices: []EventChoice{
					{Key: "fight", Label: "Fight", Check: check2(0, AttrMartial, 5, AttrProwess, 3),
						Success: slay("Your guards break their charge and the Mummers scatter into the hills, leaving their dead.", 25, 12),
						Failure: EventOutcome{Text: "They take your purse, and very nearly your ears.", Quality: -12, Delta: Delta{Wealth: -40}, Condition: CondWounded}},
					{Key: "pay", Label: "Hand over a purse (30 gold)",
						Success: EventOutcome{Text: "They take it, laugh, and ride on.", Quality: -4, Delta: Delta{Wealth: -30}}},
				},
				Default: "pay"},
		},
	},
}

func init() {
	// The Venetian merchant's auction: no event to meet, just a lot to bid on
	// (auction.go; the bot opens the auction when it begins).
	HappeningDefs = append(HappeningDefs, HappeningDef{
		Key: KindAuctionHappening, Name: "A Venetian Merchant's Auction", Emoji: "🔨", Weight: 0.8, MinDays: 1, MaxDays: 2,
		Announce: "A Venetian merchant has set up his table in the Forum, with one curiosity to sell to the highest bidder. **The bidding is open.** He starts low; whether it ends low is up to the court.",
		Fade:     "The Venetian merchant has packed his table and gone.",
		Hint:     "🔨 A Venetian merchant is holding an auction in the Forum.",
	})
}

// happeningTrophies are the prizes the slayers of the great beasts take
// home. Never sold in the shop; one per court.
var happeningTrophies = []Item{
	good("manticore_tail", "The Manticore's Tail", KindRelic, 8, 400, ShelfNone, true, Effects{PartyBonus: 2},
		"A spiked tail as long as a man, hung over the hearth. Guests ask about it all night."),
	good("wyvern_skull", "A Wyvern's Skull", KindRelic, 8, 400, ShelfNone, true, Effects{Stats: Stats{Martial: 5}},
		"Bleached white, with teeth as long as daggers. Officers come just to see it."),
}

func init() {
	Items = append(Items, happeningTrophies...)
	// Every happening event gets its key, and the activity pools can find
	// it through EventByKey like any other event.
	for i := range HappeningDefs {
		h := &HappeningDefs[i]
		for _, where := range h.Meets() {
			ev := h.Events[where]
			ev.Key = happeningEventKey(h.Key, where)
			h.Events[where] = ev
			happeningEvents = append(happeningEvents, ev)
		}
	}
}

// happeningEvents is every happening's event, for EventByKey.
var happeningEvents []PartyEvent
