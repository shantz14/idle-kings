package game

// Mystical events for parties and hunts. Appended to PartyEvents and
// HuntEvents at init. Placeholder numbers — tune freely.

func init() {
	PartyEvents = append(PartyEvents, magicPartyEvents...)
	HuntEvents = append(HuntEvents, magicHuntEvents...)
}

var magicPartyEvents = []PartyEvent{
	{
		Key: "fortune_teller", Title: "A fortune-teller at the door",
		Prompt: "An old woman with milky eyes is at the gate, offering to read your guests' palms. Some of them are already holding out their hands.",
		Choices: []EventChoice{
			{Key: "read", Label: "Let her read palms", Check: chk(25, AttrDiplomacy, 6),
				Success: EventOutcome{Text: "Her fortunes are flattering, uncanny, and the talk of the night.", Quality: 8, Delta: Delta{Renown: 0.2}},
				Failure: EventOutcome{Text: "She tells a senator's wife exactly where her husband spends his Tuesdays. The room goes cold.", Quality: -8}},
			{Key: "away", Label: "Turn her away",
				Success: EventOutcome{Text: "She curses your doorstep as she leaves. Probably nothing.", Quality: -2}},
		},
		Default: "away",
	},
	{
		Key: "cellar_ghost", Title: "Something in the wine cellar",
		Prompt: "A servant comes up from the cellar shaking. Someone is down there, he says, in old armor, and the candles won't stay lit.",
		Choices: []EventChoice{
			{Key: "seance", Label: "Hold a séance for the guests", Check: chk(20, AttrIntrigue, 6),
				Success: EventOutcome{Text: "The ghost raps out a long-dead scandal, to shrieks of delight. Nobody will forget your party.", Quality: 12, Delta: Delta{Renown: 0.3}},
				Failure: EventOutcome{Text: "The table lifts, the wine turns to vinegar, and half your guests leave in a hurry.", Quality: -12}},
			{Key: "brick", Label: "Brick up the cellar door",
				Success: EventOutcome{Text: "The masons work through the night. The music is loud enough to cover the knocking.", Quality: -3, Delta: Delta{Wealth: -10}}},
		},
		Default: "brick",
	},
	{
		Key: "alchemist_trick", Title: "An alchemist's party trick",
		Prompt: "A guest who deals in powders offers to make your fountain run with blue fire. He swears it's perfectly safe.",
		Choices: []EventChoice{
			{Key: "perform", Label: "Let him do it", Check: chk(20, AttrStewardship, 6),
				Success: EventOutcome{Text: "Blue flame dances on the water all night. It is the most beautiful thing anyone has seen.", Quality: 12},
				Failure: EventOutcome{Text: "The fountain explodes. Nobody is badly hurt, but the courtyard will never be the same.", Quality: -10, Delta: Delta{Wealth: -20}}},
			{Key: "decline", Label: "Politely decline",
				Success: EventOutcome{Text: "He pockets his powders, disappointed. The fountain lives.", Quality: 0}},
		},
		Default: "decline",
	},
}

var magicHuntEvents = []PartyEvent{
	{
		Key: "hunt_fey_lights", Title: "The Wild Hunt",
		Prompt: "Far-off horns, and pale lights bob between the trunks, just off the trail: the Wild Hunt is riding. The hounds won't go near them. They seem to be waiting for you.",
		Choices: []EventChoice{
			{Key: "follow", Label: "Follow them", Check: chk(20, AttrIntrigue, 6),
				Success: EventOutcome{Text: "They lead you to a clearing full of game, and then wink out. You come back with the best kill of the day, and a strange calm.", Quality: 14, Delta: Delta{Stats: Stats{Intrigue: 1}}},
				Failure: EventOutcome{Text: "You walk for what feels like an hour. The others say you were gone a day. You don't remember the way back.", Quality: -10, Condition: CondWounded}},
			{Key: "turn_back", Label: "Turn back to the trail",
				Success: EventOutcome{Text: "The lights drift away. Your huntsman makes a sign against the evil eye and says nothing.", Quality: 0}},
		},
		Default: "turn_back",
	},
	{
		Key: "hunt_witch_hut", Title: "Baba Yaga's hut",
		Prompt: "Deep in the wood, a hut of bones and turf stands on a pair of chicken legs. Smoke from the chimney. Baba Yaga's voice from inside says your name, though you never gave it.",
		Choices: []EventChoice{
			{Key: "knock", Label: "Knock, politely", Check: chk(25, AttrDiplomacy, 6),
				Success: EventOutcome{Text: "She gives you tea and tells you where the stags sleep. Your hunt is charmed.", Quality: 12},
				Failure: EventOutcome{Text: "She takes offense at something you never understand. Your horse is lame by evening.", Quality: -8}},
			{Key: "burn", Label: "Burn it down", Check: chk(25, AttrMartial, 5),
				Success: EventOutcome{Text: "Your riders put it to the torch. It burns green. Nothing comes out.", Quality: 4, Delta: Delta{Renown: 0.2}},
				Failure: EventOutcome{Text: "It burns green, and something comes out. You will feel her curse in your bones for days.", Quality: -8, Condition: CondWounded}},
			{Key: "ride_on", Label: "Ride on",
				Success: EventOutcome{Text: "You ride on. The voice laughs until you're out of earshot.", Quality: -2}},
		},
		Default: "ride_on",
	},
	{
		Key: "hunt_raven", Title: "A raven that speaks",
		Prompt: "A raven lands on your saddle horn and says, quite clearly, \"West. By the broken oak.\"",
		Choices: []EventChoice{
			{Key: "listen", Label: "Ride west to the broken oak",
				Success: EventOutcome{Text: "There's a stag at the broken oak, exactly as promised. The raven watches you take it.", Quality: 10}},
			{Key: "shoot", Label: "Shoot the raven", Check: chk(30, AttrProwess, 6),
				Success: EventOutcome{Text: "One arrow. The raven falls, and the forest goes very quiet. Your riders keep their distance afterwards.", Quality: -4, Delta: Delta{Renown: 0.1}},
				Failure: EventOutcome{Text: "You miss. It flies off, laughing in a voice like your father's.", Quality: -6}},
		},
		Default: "listen",
	},
}
