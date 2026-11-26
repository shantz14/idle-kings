package game

// Bills: Auto events that follow cheap-now options. Senators only judge a
// vote by what it does today, so they walk into these; players who read the
// hints can see them coming.

func init() { MatterEvents = append(MatterEvents, billMatters...) }

var billMatters = []MatterEvent{
	{
		Key: "bill_bankers", Title: "The Bankers Want Repaying", Theme: ThemeMoney, FollowUpOnly: true, Auto: true,
		Prompt:  "The Iron Bank of Braavos has come for its gold, with interest. The Iron Bank will have its due.",
		Default: "pay",
		Options: []MatterOption{{Key: "pay", Label: "The bill comes due",
			Success: MatterOutcome{Text: "The loan is repaid, and then some.", Realm: R{AttrTreasury: -27, AttrForeign: -4}}}},
	},
	{
		Key: "bill_walls", Title: "The Patched Walls Give Way", Theme: ThemeWorks, FollowUpOnly: true, Auto: true,
		Prompt:  "A stretch of the patched wall collapses in the night, killing a watchman.",
		Default: "pay",
		Options: []MatterOption{{Key: "pay", Label: "The bill comes due",
			Success: MatterOutcome{Text: "The breach takes weeks to close, and the city feels unsafe.", Realm: R{AttrMilitary: -10, AttrStability: -5}}}},
	},
	{
		Key: "bill_lances", Title: "The Golden Company Comes to Collect", Theme: ThemeSoldiers, FollowUpOnly: true, Auto: true,
		Prompt:  "The Golden Company rides into the Forum holding the Senate's sealed promise.",
		Default: "pay",
		Options: []MatterOption{{Key: "pay", Label: "The bill comes due",
			Success: MatterOutcome{Text: "The Senate pays, with the sellswords' blades drawn in the Forum.", Realm: R{AttrTreasury: -22, AttrMilitary: -4, AttrStability: -3}}}},
	},
	{
		Key: "bill_spring", Title: "A Hungry Spring", Theme: ThemeFood, FollowUpOnly: true, Auto: true,
		Prompt:  "Spring comes, and the fields lie bare: the seed grain was sold abroad.",
		Default: "pay",
		Options: []MatterOption{{Key: "pay", Label: "The bill comes due",
			Success: MatterOutcome{Text: "Bread is scarce, and the people know whom to blame.", Sectors: SC{SectorAgriculture: {Health: -0.1}},
				Realm: R{AttrPopularity: -12, AttrStability: -6}}}},
	},
}
