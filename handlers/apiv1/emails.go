package apiv1

import (
	"bottrade/database"
	"bottrade/models"
	"fmt"
	"log"
	"strings"
	"time"
)

// sendEmailOnce records (account, kind, period) in email_log and, when this is
// the first occurrence, delivers the email in the background. A row already
// present means it was sent this period — silent skip. Insert failures skip
// the send too: no dedupe guarantee, no email.
func (h *handlers) sendEmailOnce(accountID, kind, period, to, subject, text, html string) {
	if h.Mailer == nil || to == "" {
		return
	}
	res, err := database.DB.Exec(
		`INSERT OR IGNORE INTO email_log (account_id, kind, period) VALUES (?1, ?2, ?3)`,
		accountID, kind, period,
	)
	if err != nil {
		log.Printf("email_log insert failed (%s, account %s): %v", kind, accountID, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	go func() {
		if err := h.Mailer.Send(to, subject, text, html); err != nil {
			log.Printf("email send failed (%s to %s): %v", kind, to, err)
		}
	}()
}

// accountEmail returns the best address on file for an account.
func accountEmail(accountID string) string {
	var email string
	_ = database.DB.QueryRow(
		`SELECT COALESCE(NULLIF(email, ''), COALESCE(billing_email, '')) FROM accounts WHERE id = ?1`,
		accountID,
	).Scan(&email)
	return email
}

// emailHTML wraps paragraphs in a minimal readable layout. Paragraphs may
// contain inline HTML (links); plain-text lines are the caller's job.
func emailHTML(paragraphs ...string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family: -apple-system, Segoe UI, Helvetica, Arial, sans-serif; font-size: 15px; line-height: 1.6; color: #1a1a1a; max-width: 560px;">`)
	for _, p := range paragraphs {
		b.WriteString(`<p style="margin: 0 0 14px;">` + p + `</p>`)
	}
	b.WriteString(`<p style="margin: 18px 0 0; color: #8a8a88; font-size: 12px;">BotTrade · a reproducible test bench for AI trading agents · <a href="https://bot-trade.org" style="color: #ff6a00;">bot-trade.org</a> · <a href="https://bot-trade.org/contact" style="color: #ff6a00;">Contact</a></p></div>`)
	return b.String()
}

// sendWelcomeEmail fires once per account, on first OAuth signup.
func (h *handlers) sendWelcomeEmail(accountID, email, name string) {
	greet := "Welcome"
	if fields := strings.Fields(strings.TrimSpace(name)); len(fields) > 0 && !strings.Contains(fields[0], "@") {
		greet = "Welcome, " + fields[0]
	}
	base := h.AppBaseURL
	subject := "Your BotTrade account is live — 25 free runs"

	text := greet + ` — your BotTrade account is ready.

You have 25 free benchmark runs this month. The fastest first score:

1. Grab your API key: ` + base + `/account
2. Run the reference bot:

   export BOT_API_KEY=<your key>
   curl -sO ` + base + `/api/test_bot.py
   python test_bot.py --strategy momentum --publish

That gets you a scored public run on a historic market scenario — same data, same rules every agent gets.

Using Claude or another MCP client? Point it at https://mcp.bot-trade.org/mcp and ask it to run a benchmark.

Watch a run first: ` + base + `/demo
Beat the baseline: ` + base + `/challenge

Reply to this email if you get stuck — a human reads it.`

	html := emailHTML(
		greet+" — your BotTrade account is ready.",
		"You have <b>25 free benchmark runs</b> this month. The fastest first score:",
		`1. Grab your API key: <a href="`+base+`/account" style="color: #ff6a00;">`+base+`/account</a><br>2. Run the reference bot:`,
		`<code style="display:block; background:#f4f4f2; padding:12px; border-radius:6px; font-size:13px;">export BOT_API_KEY=&lt;your key&gt;<br>curl -sO `+base+`/api/test_bot.py<br>python test_bot.py --strategy momentum --publish</code>`,
		"That gets you a scored public run on a historic market scenario — same data, same rules every agent gets.",
		`Using Claude or another MCP client? Point it at <code>https://mcp.bot-trade.org/mcp</code> and ask it to run a benchmark.`,
		`Watch a run first: <a href="`+base+`/demo" style="color: #ff6a00;">`+base+`/demo</a><br>Beat the baseline: <a href="`+base+`/challenge" style="color: #ff6a00;">`+base+`/challenge</a>`,
		"Reply to this email if you get stuck — a human reads it.",
	)

	h.sendEmailOnce(accountID, "welcome", "", email, subject, text, html)
}

// sendQuotaUpgradeEmail fires when a free or pro account exhausts its monthly
// quota. At most one per kind per UTC month.
func (h *handlers) sendQuotaUpgradeEmail(key models.APIKey, runsUsed, limit int, resetsAt time.Time) {
	email := accountEmail(key.AccountID.String())
	if email == "" {
		return
	}
	base := h.AppBaseURL
	reset := resetsAt.Format("January 2")
	period := time.Now().UTC().Format("2006-01")

	var kind, subject, text, html string
	switch key.Plan {
	case "pro":
		kind = "quota_pro"
		subject = "You've used all 200 Pro runs this month"
		text = fmt.Sprintf(`That's some volume — %d of %d Pro runs used this month.

Your quota resets on %s. If you don't want to wait, Max is 1000 runs a month for $69.99:

%s/account

Reply if you need something bigger than Max — a human reads this.`, runsUsed, limit, reset, base)
		html = emailHTML(
			fmt.Sprintf("That's some volume — <b>%d of %d</b> Pro runs used this month.", runsUsed, limit),
			fmt.Sprintf(`Your quota resets on %s. If you don't want to wait, <b>Max is 1000 runs a month for $69.99</b>:`, reset),
			`<a href="`+base+`/account" style="color: #ff6a00; font-weight: 600;">Upgrade to Max →</a>`,
			"Reply if you need something bigger than Max — a human reads this.",
		)
	default:
		kind = "quota_free"
		subject = "You've used all 25 free runs this month"
		text = fmt.Sprintf(`Your BotTrade account hit its free limit: %d of %d runs used this month.

Your quota resets on %s. If you don't want to wait, Pro is 200 runs a month for $29.99:

%s/account

Your runs, results, and leaderboard entries stay where they are — upgrading only raises the ceiling.`, runsUsed, limit, reset, base)
		html = emailHTML(
			fmt.Sprintf("Your BotTrade account hit its free limit: <b>%d of %d</b> runs used this month.", runsUsed, limit),
			fmt.Sprintf(`Your quota resets on %s. If you don't want to wait, <b>Pro is 200 runs a month for $29.99</b>:`, reset),
			`<a href="`+base+`/account" style="color: #ff6a00; font-weight: 600;">Upgrade to Pro →</a>`,
			"Your runs, results, and leaderboard entries stay where they are — upgrading only raises the ceiling.",
		)
	}

	h.sendEmailOnce(key.AccountID.String(), kind, period, email, subject, text, html)
}
