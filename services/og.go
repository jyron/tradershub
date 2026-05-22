package services

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// OG images: 1200×630 PNG cards rendered server-side for /og/* endpoints.
// Twitter/Discord/Slack crawlers pull these into link previews, which is what
// makes "I just submitted my bot" tweets actually look like something.
//
// We render with stdlib image + golang.org/x/image's embedded Go fonts so
// there's no system-font dependency on the Railway container.

const (
	OGWidth  = 1200
	OGHeight = 630
)

var (
	colBg       = color.RGBA{0x12, 0x12, 0x14, 0xff}
	colInk      = color.RGBA{0xf5, 0xf5, 0xf5, 0xff}
	colInk2     = color.RGBA{0x9a, 0x9a, 0xa0, 0xff}
	colAccent   = color.RGBA{0xff, 0x66, 0x33, 0xff}
	colUp       = color.RGBA{0x4a, 0xd2, 0x95, 0xff}
	colDown     = color.RGBA{0xe8, 0x58, 0x58, 0xff}
	colDivider  = color.RGBA{0x2a, 0x2a, 0x2e, 0xff}

	fontInit sync.Once
	fontBold *opentype.Font
	fontReg  *opentype.Font
	fontErr  error
)

func loadFonts() error {
	fontInit.Do(func() {
		fontBold, fontErr = opentype.Parse(gobold.TTF)
		if fontErr != nil {
			return
		}
		fontReg, fontErr = opentype.Parse(goregular.TTF)
	})
	return fontErr
}

func newFace(parsed *opentype.Font, sizePx float64) (font.Face, error) {
	return opentype.NewFace(parsed, &opentype.FaceOptions{
		Size: sizePx, DPI: 72, Hinting: font.HintingFull,
	})
}

// OGCardInput is the minimal data set every card variant uses.
type OGCardInput struct {
	Kind        string // "bot" | "leaderboard" | "trade"
	Title       string
	Subtitle    string
	BigMetric   string
	BigPositive bool
	Stats       []OGStat // up to 3
	Rows        []OGRow  // for leaderboard
	Footer      string
}

type OGStat struct{ Label, Value string }
type OGRow struct {
	Rank    int
	Name    string
	Metric  string
	Positiv bool
}

// Render returns a PNG-encoded OG card.
func RenderOGCard(in OGCardInput) ([]byte, error) {
	if err := loadFonts(); err != nil {
		return nil, fmt.Errorf("load fonts: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, OGWidth, OGHeight))
	fillRect(img, img.Bounds(), colBg)

	// Accent strip down the left edge for brand recognition.
	fillRect(img, image.Rect(0, 0, 12, OGHeight), colAccent)

	// Brand mark in the top-left.
	brandFace, err := newFace(fontBold, 28)
	if err != nil {
		return nil, err
	}
	defer brandFace.Close()
	drawText(img, brandFace, 60, 70, "bot/trade", colInk)
	drawText(img, brandFace, brandTextWidth(brandFace, "bot/trade")+60+18, 70,
		"benchmark", colAccent)

	switch in.Kind {
	case "leaderboard":
		err = renderLeaderboardBody(img, in)
	default:
		err = renderHeroBody(img, in)
	}
	if err != nil {
		return nil, err
	}

	// Footer
	footFace, err := newFace(fontReg, 22)
	if err != nil {
		return nil, err
	}
	defer footFace.Close()
	footer := in.Footer
	if footer == "" {
		footer = "bot-trade.org · " + time.Now().UTC().Format("Jan 2, 2006")
	}
	// Anchor footer to the bottom rule so it doesn't collide with the stats
	// row on hero cards.
	drawText(img, footFace, 60, OGHeight-22, footer, colInk2)

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func renderHeroBody(img *image.RGBA, in OGCardInput) error {
	titleFace, err := newFace(fontBold, 88)
	if err != nil {
		return err
	}
	defer titleFace.Close()
	subFace, err := newFace(fontReg, 32)
	if err != nil {
		return err
	}
	defer subFace.Close()
	bigFace, err := newFace(fontBold, 180)
	if err != nil {
		return err
	}
	defer bigFace.Close()
	statKeyFace, err := newFace(fontReg, 20)
	if err != nil {
		return err
	}
	defer statKeyFace.Close()
	statValFace, err := newFace(fontBold, 36)
	if err != nil {
		return err
	}
	defer statValFace.Close()

	// Title (bot name) — clipped to keep within frame.
	title := truncateForFace(titleFace, in.Title, OGWidth-120)
	drawText(img, titleFace, 60, 200, title, colInk)

	if in.Subtitle != "" {
		drawText(img, subFace, 60, 245, in.Subtitle, colInk2)
	}

	// Big metric (e.g. "+12.34%")
	if in.BigMetric != "" {
		metricColor := colDown
		if in.BigPositive {
			metricColor = colUp
		}
		drawText(img, bigFace, 60, 430, in.BigMetric, metricColor)
	}

	// Stats strip above the footer. Keep 20 px clear above the footer
	// baseline so the two never overlap regardless of font hinting.
	if len(in.Stats) > 0 {
		fillRect(img, image.Rect(60, 470, OGWidth-60, 472), colDivider)
		colW := (OGWidth - 120) / 3
		for i, s := range in.Stats {
			if i >= 3 {
				break
			}
			x := 60 + i*colW
			drawText(img, statKeyFace, x, 510, s.Label, colInk2)
			drawText(img, statValFace, x, 550, s.Value, colInk)
		}
	}
	return nil
}

func renderLeaderboardBody(img *image.RGBA, in OGCardInput) error {
	titleFace, err := newFace(fontBold, 72)
	if err != nil {
		return err
	}
	defer titleFace.Close()
	subFace, err := newFace(fontReg, 28)
	if err != nil {
		return err
	}
	defer subFace.Close()
	rankFace, err := newFace(fontBold, 36)
	if err != nil {
		return err
	}
	defer rankFace.Close()
	nameFace, err := newFace(fontBold, 32)
	if err != nil {
		return err
	}
	defer nameFace.Close()
	metricFace, err := newFace(fontBold, 32)
	if err != nil {
		return err
	}
	defer metricFace.Close()

	drawText(img, titleFace, 60, 150, in.Title, colInk)
	if in.Subtitle != "" {
		drawText(img, subFace, 60, 185, in.Subtitle, colInk2)
	}

	startY := 240
	rowH := 60
	for i, r := range in.Rows {
		if i >= 5 {
			break
		}
		y := startY + i*rowH
		// Rank badge
		drawText(img, rankFace, 60, y, fmt.Sprintf("#%d", r.Rank), colAccent)
		// Bot name (truncated)
		name := truncateForFace(nameFace, r.Name, 640)
		drawText(img, nameFace, 160, y, name, colInk)
		// Metric (right-aligned)
		c := colDown
		if r.Positiv {
			c = colUp
		}
		mw := stringWidth(metricFace, r.Metric)
		drawText(img, metricFace, OGWidth-60-mw, y, r.Metric, c)
	}
	return nil
}

// drawText baseline-draws s at (x, y) using the given face.
func drawText(img *image.RGBA, face font.Face, x, y int, s string, c color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

func fillRect(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
}

func stringWidth(face font.Face, s string) int {
	a := font.MeasureString(face, s)
	return a.Ceil()
}

func brandTextWidth(face font.Face, s string) int {
	return stringWidth(face, s)
}

func truncateForFace(face font.Face, s string, maxPx int) string {
	if stringWidth(face, s) <= maxPx {
		return s
	}
	// Binary search would be cleaner; linear is fine at this scale.
	for i := len(s) - 1; i > 4; i-- {
		c := s[:i] + "…"
		if stringWidth(face, c) <= maxPx {
			return c
		}
	}
	return s
}

// FormatPctSigned returns "+12.34%" / "-5.20%".
func FormatPctSigned(v float64) string {
	sign := "+"
	if v < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.2f%%", sign, v)
}

// FormatMoney returns "$123,456" (no cents) with thousands separators.
func FormatMoney(v float64) string {
	rounded := math.Round(v)
	s := fmt.Sprintf("%.0f", math.Abs(rounded))
	out := []byte{}
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(ch))
	}
	if rounded < 0 {
		return "-$" + string(out)
	}
	return "$" + string(out)
}
