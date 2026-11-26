package game

import "strings"

// Crest is a family's emblem: a color, a symbol and a motto.
type Crest struct {
	Color  string `json:"color,omitempty"`
	Symbol string `json:"symbol,omitempty"`
	Motto  string `json:"motto,omitempty"`
}

type CrestPart struct {
	Key   string
	Name  string
	Emoji string
}

var CrestColors = []CrestPart{
	{"red", "Red", "🟥"}, {"orange", "Orange", "🟧"}, {"gold", "Gold", "🟨"},
	{"green", "Green", "🟩"}, {"blue", "Blue", "🟦"}, {"purple", "Purple", "🟪"},
	{"brown", "Brown", "🟫"}, {"black", "Black", "⬛"}, {"white", "White", "⬜"},
}

var CrestSymbols = []CrestPart{
	{"lion", "Lion", "🦁"}, {"wolf", "Wolf", "🐺"}, {"eagle", "Eagle", "🦅"}, {"dragon", "Dragon", "🐉"},
	{"serpent", "Serpent", "🐍"}, {"bear", "Bear", "🐻"}, {"stag", "Stag", "🦌"}, {"boar", "Boar", "🐗"},
	{"owl", "Owl", "🦉"}, {"horse", "Horse", "🐎"}, {"bee", "Bee", "🐝"}, {"moon", "Moon", "🌙"},
	{"sun", "Sun", "☀️"}, {"star", "Star", "⭐"}, {"crown", "Crown", "👑"}, {"sword", "Sword", "🗡️"},
	{"shield", "Shield", "🛡️"}, {"tower", "Tower", "🏰"}, {"rose", "Rose", "🌹"}, {"tree", "Tree", "🌲"},
	{"flame", "Flame", "🔥"}, {"anchor", "Anchor", "⚓"}, {"key", "Key", "🔑"}, {"grapes", "Grapes", "🍇"},
}

func crestPart(list []CrestPart, key string) (CrestPart, bool) {
	for _, p := range list {
		if p.Key == key {
			return p, true
		}
	}
	return CrestPart{}, false
}

func CrestColor(key string) (CrestPart, bool)  { return crestPart(CrestColors, key) }
func CrestSymbol(key string) (CrestPart, bool) { return crestPart(CrestSymbols, key) }

// Complete reports whether both a valid color and symbol are chosen.
func (c Crest) Complete() bool {
	_, okC := CrestColor(c.Color)
	_, okS := CrestSymbol(c.Symbol)
	return okC && okS
}

// Emoji renders the crest, e.g. "🟥🦁". Unchosen parts show as ▫️.
func (c Crest) Emoji() string {
	var sb strings.Builder
	if p, ok := CrestColor(c.Color); ok {
		sb.WriteString(p.Emoji)
	} else {
		sb.WriteString("▫️")
	}
	if p, ok := CrestSymbol(c.Symbol); ok {
		sb.WriteString(p.Emoji)
	} else {
		sb.WriteString("▫️")
	}
	return sb.String()
}
