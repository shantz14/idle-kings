package game

// ---- Speak to a Senator: greetings ----

// OpinionBand is the five words of OpinionWord, as keys.
type OpinionBand string

const (
	BandHostile     OpinionBand = "hostile"
	BandUnfriendly  OpinionBand = "unfriendly"
	BandIndifferent OpinionBand = "indifferent"
	BandFriendly    OpinionBand = "friendly"
	BandDevoted     OpinionBand = "devoted"
)

var OpinionBands = []OpinionBand{BandHostile, BandUnfriendly, BandIndifferent, BandFriendly, BandDevoted}

// BandOf is the band an opinion falls in, matching OpinionWord.
func BandOf(op float64) OpinionBand {
	switch {
	case op <= -60:
		return BandHostile
	case op <= -20:
		return BandUnfriendly
	case op < 20:
		return BandIndifferent
	case op < 60:
		return BandFriendly
	default:
		return BandDevoted
	}
}

type greetingKey struct {
	Band  OpinionBand
	Again bool // they've spoken before
}

// greetings are what a senator says when you call, by opinion and whether
// you've met. Lines for one of the senator's traits are preferred.
var greetings = map[greetingKey][]askLine{
	{BandHostile, false}: {
		{"", "\"I know who you are. Say what you came to say and go.\""},
		{"", "\"I don't believe we've been introduced, and I'd prefer to keep it that way.\""},
		{TraitScheming, "\"Curious, seeing you here. Who sent you?\""},
	},
	{BandHostile, true}: {
		{"", "\"You again. My patience has limits.\""},
		{"", "\"Back already? The door is behind you.\""},
		{TraitStern, "\"I have nothing to say to you.\""},
		{TraitScheming, "\"Still alive, I see. How disappointing.\""},
	},
	{BandUnfriendly, false}: {
		{"", "\"Ah. One of the new families. What do you want?\""},
		{"", "\"I've heard of you. Not all of it good.\""},
		{TraitScheming, "\"Curious, seeing you here.\""},
	},
	{BandUnfriendly, true}: {
		{"", "\"Hello again. Make it quick.\""},
		{"", "\"You do keep coming back.\""},
		{TraitStern, "\"Be brief.\""},
		{TraitGreedy, "\"Unless you've brought something, I'm busy.\""},
	},
	{BandIndifferent, false}: {
		{"", "\"Welcome. I don't think we've met.\""},
		{"", "\"A new face. Sit, if you like.\""},
		{TraitScheming, "\"Curious, seeing you here. Sit down.\""},
		{TraitCosmopolitan, "\"Welcome, welcome. Wine? It's from across the sea.\""},
		{TraitStern, "\"You have a few minutes. Use them well.\""},
	},
	{BandIndifferent, true}: {
		{"", "\"Hello again.\""},
		{"", "\"Back so soon? What is it this time?\""},
		{TraitStern, "\"Be brief.\""},
		{TraitPious, "\"The gods keep you. What brings you back?\""},
		{TraitMerchant, "\"Business or pleasure? I'm hoping business.\""},
	},
	{BandFriendly, false}: {
		{"", "\"So you're the one everyone speaks well of. Come in.\""},
		{"", "\"At last we meet. I've heard good things.\""},
	},
	{BandFriendly, true}: {
		{"", "\"Hello again, friend.\""},
		{"", "\"Always a pleasure. Sit down.\""},
		{TraitBigSpender, "\"You're just in time for dinner. I insist.\""},
		{TraitScheming, "\"Ah, you. Close the door behind you.\""},
	},
	{BandDevoted, false}: {
		{"", "\"An honour. I've wanted to meet you for some time.\""},
	},
	{BandDevoted, true}: {
		{"", "\"My dear friend! Come in, come in.\""},
		{"", "\"There you are. I was hoping you'd call.\""},
		{TraitStern, "\"You. Good. Sit.\""},
		{TraitPopulist, "\"The people's favourite, and mine. Welcome back.\""},
	},
}

// Greeting is what the senator says when you call on them. visits is how
// many times you've called before; it also picks the line, so it changes
// from one visit to the next.
func Greeting(s Senator, op float64, visits int) string {
	return pickLine(greetings[greetingKey{BandOf(op), visits > 0}], s, visits+int(s.ID))
}
