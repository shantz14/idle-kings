package bot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
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
	"brown": {0x9C, 0x64, 0x3C, 0xFF}, "black": {0x3A, 0x3D, 0x42, 0xFF}, "white": {0xEC, 0xF0, 0xF1, 0xFF},
}

var (
	houseRGB   = color.RGBA{0x6B, 0x75, 0x80, 0xFF} // the old NPC houses
	othersRGB  = color.RGBA{0xA0, 0xA6, 0xAD, 0xFF} // small players, merged
	secretRGB  = color.RGBA{0x4A, 0x23, 0x5A, 0xFF} // hidden Black Market investors
	outlineRGB = color.RGBA{0x1E, 0x1F, 0x22, 0xFF}
	textRGB    = color.RGBA{0xF2, 0xF3, 0xF5, 0xFF}
	gridRGB    = color.RGBA{0x2B, 0x2D, 0x31, 0xFF}
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

// legendLine is one industry on one line, for the overview.
func legendLine(c pieCard) string {
	var parts []string
	for _, s := range c.Slices {
		parts = append(parts, fmt.Sprintf("%s %s %s", s.Mark, strings.ReplaceAll(s.label(), " (you)", ""), pct(s.Share)))
	}
	return truncate(c.Sector.Emoji+" "+strings.Join(parts, " · "), 300)
}

// legendLines is the overview's legend, one industry per line.
func legendLines(cards []pieCard) string {
	var lines []string
	for _, c := range cards {
		lines = append(lines, legendLine(c))
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

const supersample = 3

// drawPie draws a pie centered at (cx, cy), starting at twelve o'clock and
// going clockwise, with dark lines between wedges and around the edge.
func drawPie(img *image.RGBA, cx, cy, r float64, slices []pieSlice) {
	var bounds []float64 // cumulative angle at each wedge's end, 0..2π
	sum := 0.0
	for _, s := range slices {
		sum += s.Share
	}
	if sum <= 0 {
		return
	}
	acc := 0.0
	for _, s := range slices {
		acc += s.Share / sum * 2 * math.Pi
		bounds = append(bounds, acc)
	}
	const line = 1.6 // outline width in pixels
	x0, x1 := int(cx-r-2), int(cx+r+2)
	y0, y1 := int(cy-r-2), int(cy+r+2)
	n := float64(supersample * supersample)
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			var rs, gs, bs, as float64
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					x := float64(px) + (float64(sx)+0.5)/supersample - cx
					y := float64(py) + (float64(sy)+0.5)/supersample - cy
					d := math.Hypot(x, y)
					if d > r {
						continue
					}
					a := math.Atan2(x, -y) // 0 at twelve o'clock, clockwise
					if a < 0 {
						a += 2 * math.Pi
					}
					i := sort.SearchFloat64s(bounds, a)
					if i >= len(slices) {
						i = len(slices) - 1
					}
					c := slices[i].Color
					if slices[i].Hatch && int(x+y+1000)%8 < 3 {
						c = lighten(c, 0.25)
					}
					// Near the rim or a wedge boundary: outline.
					edge := r - d
					if len(slices) > 1 {
						for _, bnd := range append([]float64{0}, bounds...) {
							if diff := math.Abs(angleDiff(a, bnd)) * d; diff < edge {
								edge = diff
							}
						}
					}
					if edge < line/2 {
						c = outlineRGB
					}
					rs, gs, bs, as = rs+float64(c.R), gs+float64(c.G), bs+float64(c.B), as+1
				}
			}
			if as == 0 {
				continue
			}
			// Premultiplied: color times coverage.
			cov := as / n
			// Blend over what's there (the grid's card, or transparency).
			bg := img.RGBAAt(px, py)
			blend := func(c float64, b uint8) uint8 { return uint8(c/as*cov + float64(b)*(1-cov)) }
			img.SetRGBA(px, py, color.RGBA{blend(rs, bg.R), blend(gs, bg.G), blend(bs, bg.B), blend(255*as, bg.A)})
		}
	}
}

func angleDiff(a, b float64) float64 {
	d := math.Mod(a-b+3*math.Pi, 2*math.Pi) - math.Pi
	return d
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

// drawText writes centered text with its baseline at y.
func drawText(img *image.RGBA, f font.Face, cx, y int, s string) {
	if f == nil {
		return
	}
	d := &font.Drawer{Dst: img, Src: image.NewUniform(textRGB), Face: f}
	w := d.MeasureString(s).Round()
	d.Dot = fixed.P(cx-w/2, y)
	d.DrawString(s)
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// pieImage is one big pie.
func pieImage(c pieCard) []byte {
	const size = 320
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	drawPie(img, size/2, size/2, size/2-8, c.Slices)
	return encode(img)
}

// pieGrid is small pies with their industry's name and the viewer's share
// (or the houses', if they own none), four to a row.
func pieGrid(cards []pieCard) []byte {
	if len(cards) == 0 {
		return nil
	}
	const cellW, cellH, r = 190, 222, 78
	cols := min(4, len(cards))
	rows := (len(cards) + cols - 1) / cols
	img := image.NewRGBA(image.Rect(0, 0, cols*cellW, rows*cellH))
	// A dark card behind the pies, so the labels read in light mode too.
	draw.Draw(img, img.Bounds(), image.NewUniform(gridRGB), image.Point{}, draw.Src)
	title, small := face(17), face(14)
	for n, c := range cards {
		x, y := (n%cols)*cellW, (n/cols)*cellH
		drawText(img, title, x+cellW/2, y+22, shortName(c.Sector.Name))
		drawPie(img, float64(x+cellW/2), float64(y+40+r), r, c.Slices)
		drawText(img, small, x+cellW/2, y+cellH-8, gridCaption(c))
	}
	return encode(img)
}

// shortName fits an industry's name under a small pie.
func shortName(s string) string {
	s = strings.TrimPrefix(s, "The ")
	if i := strings.Index(s, " & "); i > 0 {
		s = s[:i]
	}
	return s
}

func gridCaption(c pieCard) string {
	for _, s := range c.Slices {
		if s.You {
			return "you own " + pct(s.Share)
		}
	}
	for _, s := range c.Slices {
		if s.Mark == "🏛️" {
			return "players own " + pct(1-s.Share)
		}
	}
	return ""
}
