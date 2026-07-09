package apiv1

import (
	"bottrade/database"
	"bytes"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
)

var staticOGTag = regexp.MustCompile(`(?m)^\s*<meta (?:property="og:|name="twitter:)[^>]*>\n?`)

// MountRunPages serves the public run page with per-run Open Graph meta tags
// injected server-side (crawlers don't execute JS), plus a generated
// /run/:id/og.png share card with the equity curve and score. Unpublished or
// unknown runs get the untouched static page and a 404 image, so nothing
// about them leaks.
func MountRunPages(app *fiber.App, baseURL string) {
	app.Get("/run/:id", func(c *fiber.Ctx) error { return runPageWithMeta(c, baseURL) })
	app.Get("/run/:id/og.png", runOGImage)
}

type ogRunMeta struct {
	BotName    string
	Scenario   string
	ReturnPct  float64
	Sharpe     sql.NullFloat64
	MaxDD      sql.NullFloat64
	TradeCount int
	Liquidated bool
}

func loadOGRunMeta(runID string) (ogRunMeta, bool) {
	var m ogRunMeta
	var liquidated int
	err := database.DB.QueryRow(`
		SELECT COALESCE(NULLIF(l.bot_name, ''), k.name), s.name,
		       rr.return_pct, rr.sharpe, rr.max_drawdown, rr.trade_count, rr.liquidated
		  FROM run_leaderboard l
		  JOIN scenarios   s  ON s.id      = l.scenario_id
		  JOIN run_results rr ON rr.run_id = l.run_id
		  JOIN api_keys    k  ON k.id      = l.api_key_id
		 WHERE l.run_id = ?1
	`, runID).Scan(&m.BotName, &m.Scenario, &m.ReturnPct, &m.Sharpe, &m.MaxDD,
		&m.TradeCount, &liquidated)
	if err != nil {
		return m, false
	}
	m.Liquidated = liquidated != 0
	return m, true
}

func runPageWithMeta(c *fiber.Ctx, baseURL string) error {
	page, err := os.ReadFile("./static/run.html")
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	runID := c.Params("id")
	m, ok := loadOGRunMeta(runID)
	if !ok {
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		return c.Send(page)
	}

	title := fmt.Sprintf("%s — %+.2f%% on %s · BotTrade", m.BotName, m.ReturnPct, m.Scenario)
	desc := fmt.Sprintf("Return %+.2f%%", m.ReturnPct)
	if m.Sharpe.Valid {
		desc += fmt.Sprintf(" · Sharpe %.2f", m.Sharpe.Float64)
	}
	if m.MaxDD.Valid {
		desc += fmt.Sprintf(" · Max drawdown %.1f%%", m.MaxDD.Float64*100)
	}
	desc += fmt.Sprintf(" · %d trades on frozen market history. Reproducible AI trading benchmark.", m.TradeCount)

	esc := func(s string) string {
		r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
		return r.Replace(s)
	}
	tags := fmt.Sprintf(`  <meta property="og:type" content="website">
  <meta property="og:title" content="%s">
  <meta property="og:description" content="%s">
  <meta property="og:url" content="%s/run/%s">
  <meta property="og:image" content="%s/run/%s/og.png">
  <meta property="og:image:width" content="1200">
  <meta property="og:image:height" content="630">
  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:title" content="%s">
  <meta name="twitter:description" content="%s">
  <meta name="twitter:image" content="%s/run/%s/og.png">
</head>`,
		esc(title), esc(desc), baseURL, esc(runID), baseURL, esc(runID),
		esc(title), esc(desc), baseURL, esc(runID))

	// Strip the static page's generic og:/twitter: tags — crawlers honor the
	// first occurrence, so the per-run tags must be the only ones.
	stripped := staticOGTag.ReplaceAllString(string(page), "")
	out := strings.Replace(stripped, "</head>", tags, 1)
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.SendString(out)
}

// --- share-card image --------------------------------------------------------

var (
	ogBG     = color.RGBA{13, 17, 23, 255}
	ogText   = color.RGBA{230, 237, 243, 255}
	ogDim    = color.RGBA{139, 148, 158, 255}
	ogGrid   = color.RGBA{33, 40, 51, 255}
	ogGreen  = color.RGBA{63, 185, 80, 255}
	ogRed    = color.RGBA{248, 81, 73, 255}
	ogAmber  = color.RGBA{210, 153, 34, 255}
)

func runOGImage(c *fiber.Ctx) error {
	runID := c.Params("id")
	m, ok := loadOGRunMeta(runID)
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}

	rows, err := database.DB.Query(
		`SELECT equity FROM run_equity WHERE run_id = ?1 ORDER BY sim_time ASC`, runID)
	if err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	defer rows.Close()
	var equity []float64
	for rows.Next() {
		var e float64
		if err := rows.Scan(&e); err == nil {
			equity = append(equity, e)
		}
	}

	img := image.NewRGBA(image.Rect(0, 0, 1200, 630))
	fillRect(img, 0, 0, 1200, 630, ogBG)

	up := m.ReturnPct >= 0
	accent := ogGreen
	if !up {
		accent = ogRed
	}

	// Equity curve across the lower band.
	drawCurve(img, equity, 60, 300, 1140, 560, accent)

	// Headline return %.
	ret := fmt.Sprintf("%+.2f%%", m.ReturnPct)
	drawPixelText(img, ret, 60, 60, 11, accent)

	// Bot name and scenario.
	drawPixelText(img, clipText(m.BotName, 44), 60, 175, 4, ogText)
	drawPixelText(img, clipText(m.Scenario, 60), 60, 220, 3, ogDim)

	// Stats line.
	stats := fmt.Sprintf("%d TRADES", m.TradeCount)
	if m.Sharpe.Valid {
		stats = fmt.Sprintf("SHARPE %.2f   ", m.Sharpe.Float64) + stats
	}
	if m.Liquidated {
		stats += "   LIQUIDATED"
		drawPixelText(img, stats, 60, 255, 3, ogAmber)
	} else {
		drawPixelText(img, stats, 60, 255, 3, ogDim)
	}

	// Wordmark.
	drawPixelText(img, "BOT-TRADE.ORG", 60, 588, 3, ogText)
	drawPixelText(img, "REPRODUCIBLE AI TRADING BENCHMARK", 340, 592, 2, ogDim)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	c.Set(fiber.HeaderContentType, "image/png")
	c.Set(fiber.HeaderCacheControl, "public, max-age=86400")
	return c.Send(buf.Bytes())
}

func clipText(s string, max int) string {
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, col)
		}
	}
}

// drawCurve plots the equity series inside the given box with a baseline at
// the starting equity and a faint grid.
func drawCurve(img *image.RGBA, equity []float64, x0, y0, x1, y1 int, col color.RGBA) {
	for _, gy := range []int{y0, (y0 + y1) / 2, y1} {
		for x := x0; x < x1; x += 4 {
			img.SetRGBA(x, gy, ogGrid)
		}
	}
	if len(equity) < 2 {
		return
	}
	lo, hi := equity[0], equity[0]
	for _, e := range equity {
		if e < lo {
			lo = e
		}
		if e > hi {
			hi = e
		}
	}
	if hi == lo {
		hi = lo + 1
	}
	pad := (hi - lo) * 0.08
	lo, hi = lo-pad, hi+pad

	toXY := func(i int, e float64) (int, int) {
		x := x0 + i*(x1-x0)/(len(equity)-1)
		y := y1 - int(float64(y1-y0)*(e-lo)/(hi-lo))
		return x, y
	}

	// Baseline at starting equity.
	_, by := toXY(0, equity[0])
	for x := x0; x < x1; x += 7 {
		img.SetRGBA(x, by, ogDim)
	}

	px, py := toXY(0, equity[0])
	for i := 1; i < len(equity); i++ {
		x, y := toXY(i, equity[i])
		drawLine(img, px, py, x, y, col)
		px, py = x, y
	}
}

// drawLine draws a 3px-thick line segment (integer Bresenham).
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	e := dx + dy
	for {
		for oy := -1; oy <= 1; oy++ {
			for ox := -1; ox <= 1; ox++ {
				img.SetRGBA(x0+ox, y0+oy, col)
			}
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * e
		if e2 >= dy {
			e += dy
			x0 += sx
		}
		if e2 <= dx {
			e += dx
			y0 += sy
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// --- 5x7 pixel font (uppercase; unknown runes render as blanks) --------------

func drawPixelText(img *image.RGBA, s string, x, y, scale int, col color.RGBA) {
	cx := x
	for _, r := range strings.ToUpper(s) {
		if r == '—' || r == '–' {
			r = '-'
		}
		if r == '’' || r == '\'' {
			r = '.'
		}
		glyph, ok := pixelFont[r]
		if ok {
			for row := 0; row < 7; row++ {
				for colIdx := 0; colIdx < 5; colIdx++ {
					if glyph[row][colIdx] == '#' {
						fillRect(img, cx+colIdx*scale, y+row*scale,
							cx+(colIdx+1)*scale, y+(row+1)*scale, col)
					}
				}
			}
		}
		cx += 6 * scale
	}
}

var pixelFont = map[rune][7]string{
	' ': {"     ", "     ", "     ", "     ", "     ", "     ", "     "},
	'0': {" ### ", "#   #", "#  ##", "# # #", "##  #", "#   #", " ### "},
	'1': {"  #  ", " ##  ", "  #  ", "  #  ", "  #  ", "  #  ", " ### "},
	'2': {" ### ", "#   #", "    #", "  ## ", " #   ", "#    ", "#####"},
	'3': {" ### ", "#   #", "    #", "  ## ", "    #", "#   #", " ### "},
	'4': {"   # ", "  ## ", " # # ", "#  # ", "#####", "   # ", "   # "},
	'5': {"#####", "#    ", "#### ", "    #", "    #", "#   #", " ### "},
	'6': {" ### ", "#    ", "#    ", "#### ", "#   #", "#   #", " ### "},
	'7': {"#####", "    #", "   # ", "  #  ", "  #  ", "  #  ", "  #  "},
	'8': {" ### ", "#   #", "#   #", " ### ", "#   #", "#   #", " ### "},
	'9': {" ### ", "#   #", "#   #", " ####", "    #", "    #", " ### "},
	'A': {" ### ", "#   #", "#   #", "#####", "#   #", "#   #", "#   #"},
	'B': {"#### ", "#   #", "#   #", "#### ", "#   #", "#   #", "#### "},
	'C': {" ### ", "#   #", "#    ", "#    ", "#    ", "#   #", " ### "},
	'D': {"#### ", "#   #", "#   #", "#   #", "#   #", "#   #", "#### "},
	'E': {"#####", "#    ", "#    ", "#### ", "#    ", "#    ", "#####"},
	'F': {"#####", "#    ", "#    ", "#### ", "#    ", "#    ", "#    "},
	'G': {" ### ", "#   #", "#    ", "# ###", "#   #", "#   #", " ### "},
	'H': {"#   #", "#   #", "#   #", "#####", "#   #", "#   #", "#   #"},
	'I': {" ### ", "  #  ", "  #  ", "  #  ", "  #  ", "  #  ", " ### "},
	'J': {"    #", "    #", "    #", "    #", "    #", "#   #", " ### "},
	'K': {"#   #", "#  # ", "# #  ", "##   ", "# #  ", "#  # ", "#   #"},
	'L': {"#    ", "#    ", "#    ", "#    ", "#    ", "#    ", "#####"},
	'M': {"#   #", "## ##", "# # #", "# # #", "#   #", "#   #", "#   #"},
	'N': {"#   #", "##  #", "# # #", "#  ##", "#   #", "#   #", "#   #"},
	'O': {" ### ", "#   #", "#   #", "#   #", "#   #", "#   #", " ### "},
	'P': {"#### ", "#   #", "#   #", "#### ", "#    ", "#    ", "#    "},
	'Q': {" ### ", "#   #", "#   #", "#   #", "# # #", "#  # ", " ## #"},
	'R': {"#### ", "#   #", "#   #", "#### ", "# #  ", "#  # ", "#   #"},
	'S': {" ####", "#    ", "#    ", " ### ", "    #", "    #", "#### "},
	'T': {"#####", "  #  ", "  #  ", "  #  ", "  #  ", "  #  ", "  #  "},
	'U': {"#   #", "#   #", "#   #", "#   #", "#   #", "#   #", " ### "},
	'V': {"#   #", "#   #", "#   #", "#   #", "#   #", " # # ", "  #  "},
	'W': {"#   #", "#   #", "#   #", "# # #", "# # #", "## ##", "#   #"},
	'X': {"#   #", "#   #", " # # ", "  #  ", " # # ", "#   #", "#   #"},
	'Y': {"#   #", "#   #", " # # ", "  #  ", "  #  ", "  #  ", "  #  "},
	'Z': {"#####", "    #", "   # ", "  #  ", " #   ", "#    ", "#####"},
	'+': {"     ", "  #  ", "  #  ", "#####", "  #  ", "  #  ", "     "},
	'-': {"     ", "     ", "     ", "#####", "     ", "     ", "     "},
	'.': {"     ", "     ", "     ", "     ", "     ", " ##  ", " ##  "},
	',': {"     ", "     ", "     ", "     ", " ##  ", " ##  ", " #   "},
	'%': {"##  #", "## # ", "  #  ", "  #  ", " #   ", "# ## ", "#  ##"},
	'&': {" ##  ", "#  # ", "#  # ", " ##  ", "# # #", "#  # ", " ## #"},
	'(': {"   # ", "  #  ", " #   ", " #   ", " #   ", "  #  ", "   # "},
	')': {" #   ", "  #  ", "   # ", "   # ", "   # ", "  #  ", " #   "},
	'/': {"    #", "    #", "   # ", "  #  ", " #   ", "#    ", "#    "},
	':': {"     ", " ##  ", " ##  ", "     ", " ##  ", " ##  ", "     "},
	'…': {"     ", "     ", "     ", "     ", "     ", "     ", "# # #"},
}
