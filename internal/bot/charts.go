package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/shantz14/idle-kings/internal/game"
)

// Ownership pie charts: who owns how much of each industry. The image holds
// only shapes, industry names and percentages; who is who is in the embed's
// legend, which can show crest emoji.

// othersCutoff: players below this share are merged into one "others" slice.
const othersCutoff = 0.02

// crestRGB is each crest color as a wedge color.
var crestRGB = map[string]color.RGBA{
	"red": {0xE7, 0x4C, 0x3C, 0xFF}, "orange": {0xE6, 0x7E, 0x22, 0xFF}, "gold": {0xF1, 0xC4, 0x0F, 0xFF},
	"green": {0x2E, 0xCC, 0x71, 0xFF}, "blue": {0x34, 0x98, 0xDB, 0xFF}, "purple": {0x9B, 0x59, 0xB6, 0xFF},
	"brown": {0x9C, 0x64, 0x3C, 0xFF}, "black": {0x12, 0x13, 0x15, 0xFF}, "white": {0xEC, 0xF0, 0xF1, 0xFF},
}

var (
	houseRGB  = color.RGBA{0x6B, 0x75, 0x80, 0xFF} // the old NPC houses
	othersRGB = color.RGBA{0xA0, 0xA6, 0xAD, 0xFF} // small players, merged
	secretRGB = color.RGBA{0x4A, 0x23, 0x5A, 0xFF} // hidden Black Market investors
	textRGB   = color.RGBA{0xF2, 0xF3, 0xF5, 0xFF}
)

// pieSlice is one wedge, with its legend entry.
type pieSlice struct {
	Mark  string // legend emoji: a crest square, 🏛️, 🕶️, ▫️
	Name  string // "" for the houses and merged slices
	Share float64
	Color color.RGBA
	Hatch bool // drawn striped: the secret investors
	You   bool
}

// pieCard is one industry's pie.
type pieCard struct {
	Sector game.SectorDef
	Slices []pieSlice
}

// owner is what a chart needs to know about a player.
type owner struct {
	Name  string
	Crest game.Crest
}

// ---- who owns what ----

// ownershipCards builds every industry's pie. eff and totals are effective
// stakes. In a secret industry only the viewer is named; everyone else is
// one hidden slice (viewer 0 = nobody is named, for public posts).
func ownershipCards(eff map[int64]game.Stakes, totals game.Stakes, owners map[int64]owner, viewer int64) []pieCard {
	colors := ownerColors(owners)
	var out []pieCard
	for _, d := range game.SectorDefs {
		players, houses := game.Ownership(d, eff, totals)
		card := pieCard{Sector: d}
		var others, hidden float64
		for _, p := range players {
			o, known := owners[p.CharacterID]
			switch {
			case d.Secret && p.CharacterID != viewer:
				hidden += p.Share
			case p.CharacterID != viewer && (p.Share < othersCutoff || !known):
				others += p.Share
			default:
				card.Slices = append(card.Slices, pieSlice{Mark: crestMark(o.Crest), Name: o.Name, Share: p.Share,
					Color: colors[p.CharacterID], You: p.CharacterID == viewer})
			}
		}
		if others > 0 {
			card.Slices = append(card.Slices, pieSlice{Mark: "▫️", Name: "", Share: others, Color: othersRGB})
		}
		if hidden > 0 {
			card.Slices = append(card.Slices, pieSlice{Mark: "🕶️", Share: hidden, Color: secretRGB, Hatch: true})
		}
		card.Slices = append(card.Slices, pieSlice{Mark: "🏛️", Share: houses, Color: houseRGB})
		out = append(out, card)
	}
	return out
}

// ownerColors gives each player their crest color. Players who share a
// color get lighter shades of it, in id order, so a court's colors stay the
// same from one chart to the next.
func ownerColors(owners map[int64]owner) map[int64]color.RGBA {
	ids := make([]int64, 0, len(owners))
	for id := range owners {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	seen := map[string]int{}
	out := map[int64]color.RGBA{}
	for _, id := range ids {
		key := owners[id].Crest.Color
		base, ok := crestRGB[key]
		if !ok {
			base = othersRGB
		}
		out[id] = lighten(base, 0.3*float64(seen[key]))
		seen[key]++
	}
	return out
}

func lighten(c color.RGBA, f float64) color.RGBA {
	f = math.Min(f, 0.8)
	mix := func(v uint8) uint8 { return uint8(float64(v) + (255-float64(v))*f) }
	return color.RGBA{mix(c.R), mix(c.G), mix(c.B), 0xFF}
}

func crestMark(c game.Crest) string {
	if p, ok := game.CrestColor(c.Color); ok {
		return p.Emoji
	}
	return "▫️"
}

// cardFor finds one industry's pie.
func cardFor(cards []pieCard, key game.SectorKey) (pieCard, bool) {
	for _, c := range cards {
		if c.Sector.Key == key {
			return c, true
		}
	}
	return pieCard{}, false
}

// ownedCards keeps the industries the viewer has a slice of.
func ownedCards(cards []pieCard) []pieCard {
	var out []pieCard
	for _, c := range cards {
		for _, s := range c.Slices {
			if s.You {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// ---- legends ----

func (s pieSlice) label() string {
	switch {
	case s.Hatch:
		return "secret investors"
	case s.Mark == "🏛️":
		return "the old houses"
	case s.Name == "":
		return "small investors"
	case s.You:
		return "**" + s.Name + "** (you)"
	}
	return "**" + s.Name + "**"
}

// legendList is one owner per line, for a single industry.
func legendList(c pieCard) string {
	var lines []string
	for _, s := range c.Slices {
		lines = append(lines, fmt.Sprintf("%s %s · %s", s.Mark, s.label(), pct(s.Share)))
	}
	return joinLines(lines, 1000)
}

// legendLine is one industry's players on one line, for the overview. The
// old houses are left out: the picture shows them.
func legendLine(c pieCard) string {
	var parts []string
	for _, s := range c.Slices {
		if s.Mark != "🏛️" {
			parts = append(parts, fmt.Sprintf("%s %s %s", s.Mark, strings.ReplaceAll(s.label(), " (you)", ""), pct(s.Share)))
		}
	}
	return truncate(c.Sector.Emoji+" "+strings.Join(parts, " · "), 300)
}

// invested keeps the industries any player owns part of.
func invested(cards []pieCard) []pieCard {
	var out []pieCard
	for _, c := range cards {
		if len(c.Slices) > 1 { // more than the houses
			out = append(out, c)
		}
	}
	return out
}

// legendLines is the overview's legend: one line per industry players own
// part of, or a pointer to investing if none.
func legendLines(cards []pieCard) string {
	var lines []string
	for _, c := range invested(cards) {
		lines = append(lines, legendLine(c))
	}
	if len(lines) == 0 {
		return "Nobody has invested yet. *Monetary → Invest in a Venture*"
	}
	return joinLines(lines, 1000)
}

// ---- loading ----

// ownership loads every industry's pie for a court. viewer 0 for public posts.
func (b *Bot) ownership(ctx context.Context, guildID string, viewer int64) ([]pieCard, error) {
	raw, _, err1 := b.store.GuildStakes(ctx, guildID)
	st, err2 := b.store.GetRealm(ctx, guildID)
	if err := errors.Join(err1, err2); err != nil {
		return nil, err
	}
	eff, totals := game.EffectiveStakes(st.Laws, raw)
	ids := make([]int64, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	names, err := b.store.CharacterNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	owners := map[int64]owner{}
	for id, n := range names {
		owners[id] = owner{Name: n.FullName, Crest: n.Crest}
	}
	return ownershipCards(eff, totals, owners, viewer), nil
}

// chartFile is a PNG attachment, and the URL an embed shows it by.
func chartFile(name string, data []byte) (*discordgo.File, string) {
	return &discordgo.File{Name: name, ContentType: "image/png", Reader: bytes.NewReader(data)}, "attachment://" + name
}

// attachChart puts a chart on an embed and its file on the response.
func attachChart(d *discordgo.InteractionResponseData, e *discordgo.MessageEmbed, name string, data []byte) {
	if len(data) == 0 {
		return
	}
	f, url := chartFile(name, data)
	d.Files = append(d.Files, f)
	e.Image = &discordgo.MessageEmbedImage{URL: url}
}

// ---- drawing ----

// The pictures are drawn at about twice the size Discord shows them, with
// large type, so they stay sharp when scaled down to a phone.

const supersample = 3

var (
	tileRGB  = color.RGBA{0x38, 0x3A, 0x40, 0xFF} // each industry's card
	mutedRGB = color.RGBA{0xB5, 0xBA, 0xC1, 0xFF} // small print
)

// paint blends c over the pixel at (x, y) with coverage cov (0..1).
func paint(img *image.RGBA, x, y int, c color.RGBA, cov float64) {
	if cov <= 0 || !(image.Point{x, y}.In(img.Rect)) {
		return
	}
	bg := img.RGBAAt(x, y)
	mix := func(f, b uint8) uint8 { return uint8(float64(f)*cov + float64(b)*(1-cov) + 0.5) }
	img.SetRGBA(x, y, color.RGBA{mix(c.R, bg.R), mix(c.G, bg.G), mix(c.B, bg.B), mix(255, bg.A)})
}

// roundRect fills a rounded rectangle with smooth corners.
func roundRect(img *image.RGBA, r image.Rectangle, radius float64, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			in := 0
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					px := float64(x) + (float64(sx)+0.5)/supersample
					py := float64(y) + (float64(sy)+0.5)/supersample
					// Distance into the nearest corner's circle, if in a corner.
					cx := math.Max(float64(r.Min.X)+radius, math.Min(px, float64(r.Max.X)-radius))
					cy := math.Max(float64(r.Min.Y)+radius, math.Min(py, float64(r.Max.Y)-radius))
					if math.Hypot(px-cx, py-cy) <= radius {
						in++
					}
				}
			}
			paint(img, x, y, c, float64(in)/supersample/supersample)
		}
	}
}

// drawDonut draws a ring of wedges from twelve o'clock, clockwise, with a
// small gap between wedges.
func drawDonut(img *image.RGBA, cx, cy, outer, inner float64, slices []pieSlice) {
	sum := 0.0
	for _, s := range slices {
		sum += s.Share
	}
	if sum <= 0 {
		return
	}
	var ends []float64 // cumulative angle at each wedge's end
	acc := 0.0
	for _, s := range slices {
		acc += s.Share / sum * 2 * math.Pi
		ends = append(ends, acc)
	}
	visible := 0
	for _, s := range slices {
		if s.Share > 0 {
			visible++
		}
	}
	gap := outer * 0.025 // half the gap between wedges, in pixels
	for py := int(cy - outer - 1); py <= int(cy+outer+1); py++ {
		for px := int(cx - outer - 1); px <= int(cx+outer+1); px++ {
			var rs, gs, bs, hits float64
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					x := float64(px) + (float64(sx)+0.5)/supersample - cx
					y := float64(py) + (float64(sy)+0.5)/supersample - cy
					d := math.Hypot(x, y)
					if d > outer || d < inner {
						continue
					}
					a := math.Atan2(x, -y)
					if a < 0 {
						a += 2 * math.Pi
					}
					if visible > 1 {
						near := false
						for _, e := range append([]float64{0}, ends...) {
							if math.Abs(angleDiff(a, e))*d < gap {
								near = true
								break
							}
						}
						if near {
							continue
						}
					}
					i := min(sort.SearchFloat64s(ends, a), len(slices)-1)
					c := slices[i].Color
					if slices[i].Hatch && int(x+y+1000)%14 < 5 {
						c = lighten(c, 0.25)
					}
					rs, gs, bs, hits = rs+float64(c.R), gs+float64(c.G), bs+float64(c.B), hits+1
				}
			}
			if hits > 0 {
				paint(img, px, py, color.RGBA{uint8(rs / hits), uint8(gs / hits), uint8(bs / hits), 0xFF}, hits/supersample/supersample)
			}
		}
	}
}

func angleDiff(a, b float64) float64 {
	return math.Mod(a-b+3*math.Pi, 2*math.Pi) - math.Pi
}

var (
	fontOnce sync.Once
	fontData *opentype.Font
)

func face(size float64) font.Face {
	fontOnce.Do(func() { fontData, _ = opentype.Parse(gobold.TTF) })
	if fontData == nil {
		return nil
	}
	f, err := opentype.NewFace(fontData, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return f
}

// drawText writes centered text in a color with its baseline at y.
func drawText(img *image.RGBA, f font.Face, c color.RGBA, cx, y int, s string) {
	if f == nil {
		return
	}
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: f}
	d.Dot = fixed.P(cx-d.MeasureString(s).Round()/2, y)
	d.DrawString(s)
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// drawTile is one industry: a rounded card with its name, a donut, and
// the headline share in the middle. s scales everything (1 = grid size).
func drawTile(img *image.RGBA, x, y int, s float64, c pieCard) {
	w, h := int(360*s), int(410*s)
	roundRect(img, image.Rect(x, y, x+w, y+h), 28*s, tileRGB)
	drawText(img, face(40*s), textRGB, x+w/2, y+int(58*s), shortName(c.Sector.Name))
	cx, cy := float64(x)+float64(w)/2, float64(y)+235*s
	drawDonut(img, cx, cy, 145*s, 92*s, c.Slices)
	big, small := headline(c)
	drawText(img, face(60*s), textRGB, int(cx), int(cy+12*s), big)
	drawText(img, face(28*s), mutedRGB, int(cx), int(cy+48*s), small)
}

// headline is the number in a donut's middle: your share, or the players'
// share if you own none.
func headline(c pieCard) (string, string) {
	for _, s := range c.Slices {
		if s.You {
			return roundPct(s.Share), "yours"
		}
	}
	for _, s := range c.Slices {
		if s.Mark == "🏛️" {
			return roundPct(1 - s.Share), "players"
		}
	}
	return "", ""
}

// roundPct is a share as a whole percent, or one decimal under 10%.
func roundPct(f float64) string {
	p := f * 100
	if p > 0 && p < 9.95 {
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}

// pieImage is one industry's big tile, for the Invest card.
func pieImage(c pieCard) []byte {
	const s = 1.4
	img := image.NewRGBA(image.Rect(0, 0, int(360*s), int(410*s)))
	drawTile(img, 0, 0, s, c)
	return encode(img)
}

// pieGrid is industry tiles four to a row, on a transparent background.
func pieGrid(cards []pieCard) []byte {
	if len(cards) == 0 {
		return nil
	}
	const w, h, gap = 360, 410, 20
	cols := min(4, len(cards))
	rows := (len(cards) + cols - 1) / cols
	img := image.NewRGBA(image.Rect(0, 0, cols*w+(cols-1)*gap, rows*h+(rows-1)*gap))
	for n, c := range cards {
		drawTile(img, (n%cols)*(w+gap), (n/cols)*(h+gap), 1, c)
	}
	return encode(img)
}

// shortName fits an industry's name on its tile.
func shortName(s string) string {
	s = strings.TrimPrefix(s, "The ")
	if i := strings.Index(s, " & "); i > 0 {
		s = s[:i]
	}
	return s
}
